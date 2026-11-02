package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexpyslar/lumen/server/internal/auth"
	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/state"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

// idp is a minimal Keycloak: discovery and keys, so tokens signed here pass
// the real verification path in identify.
type idp struct {
	*httptest.Server
	key   *rsa.PrivateKey
	realm string
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	i := &idp{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/main/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(auth.Discovery{
			Issuer: i.realm, AuthURL: i.realm + "/auth", TokenURL: i.realm + "/token",
			JWKSURL: i.realm + "/certs", EndSessionURL: i.realm + "/logout",
		})
	})
	mux.HandleFunc("/realms/main/certs", func(w http.ResponseWriter, _ *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
		fmt.Fprintf(w, `{"keys":[{"kid":"k1","kty":"RSA","alg":"RS256","use":"sig","n":%q,"e":%q}]}`, n, e)
	})
	i.Server = httptest.NewServer(mux)
	i.realm = i.Server.URL + "/realms/main"
	t.Cleanup(i.Close)
	return i
}

func (i *idp) token(t *testing.T, user string, roles, groups []string) string {
	t.Helper()
	claims := map[string]any{
		"sub": "sub-" + user, "iss": i.realm, "aud": "lumen", "preferred_username": user,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"realm_access": map[string]any{"roles": roles}, "groups": groups, "email": user + "@studio.test",
	}
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	payload, _ := json.Marshal(claims)
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + enc.EncodeToString(sig)
}

var policyVault = map[string]string{
	"20-projects/web.md":              "---\ntitle: Сайт\ntype: project\nkey: WEB\n---\n# Сайт\n",
	"20-projects/WEB/issues/WEB-1.md": "---\nid: WEB-1\ntitle: Открытый тикет\ntype: issue\nproject: WEB\nstatus: todo\n---\nОписание\n",
	"20-projects/sec.md":              "---\ntitle: Секретный\ntype: project\nkey: SEC\n---\n# Секретный\n",
	"20-projects/SEC/issues/SEC-1.md": "---\nid: SEC-1\ntitle: Закрытый тикет\ntype: issue\nproject: SEC\nstatus: todo\n---\n" +
		"Код запуска #секрет\n\n- [ ] секретная задача [due:: 2026-11-05] ^sectask\n",
	"20-projects/SEC/sprints/SEC-S1.md": "---\nid: SEC-S1\ntitle: Спринт тайный\ntype: sprint\nproject: SEC\n---\n",
	"60-documents/sec-doc.md":           "---\ntitle: Документ тайного проекта\nparent: note:20-projects/sec.md\n---\nсекретное содержание\n",
	"10-notes/common.md":                "---\ntitle: Общая\n---\nОбщая заметка #общий про [[sec-doc]]\n",
}

// oidcFixture runs the API over a vault of files with a fake Keycloak and a
// state store; call makes a request as one of the named users.
type oidcFixture struct {
	srv    *httptest.Server
	tokens map[string]string
	t      *testing.T
}

func newOIDCFixture(t *testing.T, files map[string]string) *oidcFixture {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, "vault", filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	v, err := vault.Open(filepath.Join(dir, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := index.NewMemory(v.Root)
	bus := service.NewBus()
	ix := service.NewIndexer(v, store, bus, log)
	if _, err := ix.ReindexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(context.Background(), filepath.Join(dir, "state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	i := newIDP(t)
	provider, err := auth.NewProvider(context.Background(), auth.Config{
		Issuer: i.realm, ClientID: "lumen", RedirectURL: "http://127.0.0.1/auth/callback", InsecureCookie: true,
	}, i.Client())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AdminRoles: []string{"lumen-admin"}, ViewerRoles: []string{"guests"}}
	srv := httptest.NewServer(New(cfg, store, v, ix, bus, nil, provider, nil, log).WithState(st).Handler())
	t.Cleanup(srv.Close)
	return &oidcFixture{srv: srv, t: t, tokens: map[string]string{
		"boss":  i.token(t, "boss", []string{"lumen-admin"}, nil),
		"alice": i.token(t, "alice", nil, []string{"/designers"}),
		"bob":   i.token(t, "bob", nil, nil),
		"vic":   i.token(t, "vic", nil, []string{"/guests"}),
	}}
}

func (f *oidcFixture) call(user, method, path string, body any) (int, []byte) {
	f.t.Helper()
	var data io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		data = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, data)
	req.Header.Set("Authorization", "Bearer "+f.tokens[user])
	req.Header.Set("Content-Type", "application/json")
	res, err := f.srv.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func (f *oidcFixture) expect(user, method, path string, body any, want int) []byte {
	f.t.Helper()
	code, out := f.call(user, method, path, body)
	if code != want {
		f.t.Fatalf("%s %s %s: %d, want %d: %s", user, method, path, code, want, out)
	}
	return out
}

func TestAccessPolicyAcrossChannels(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	srv, tokens, expect := f.srv, f.tokens, f.expect

	// Roles from the identity provider.
	var me identity
	json.Unmarshal(expect("vic", "GET", "/api/v1/me", nil, 200), &me)
	if me.Role != "viewer" {
		t.Fatalf("vic role = %q", me.Role)
	}

	// Close SEC: alice personally, the designers group read-only. Only a
	// workspace admin may close an open project.
	expect("alice", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, 403)
	expect("boss", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, 200)
	expect("boss", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "group", "name": "/designers", "role": "viewer"}, 200)
	expect("alice", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "bob", "role": "admin"}, 403)

	// bob sees nothing of SEC through any channel.
	hidden := []string{
		"/api/v1/issues/SEC-1",
		"/api/v1/notes/20-projects/sec.md",
		"/api/v1/notes/20-projects/SEC/issues/SEC-1.md",
		"/api/v1/notes/60-documents/sec-doc.md",
		"/api/v1/backlinks/60-documents/sec-doc.md",
		"/api/v1/tasks/sectask",
		"/api/v1/tracker/projects/SEC",
		"/api/v1/tracker/projects/SEC/sprints",
		"/api/v1/tracker/projects/SEC/members",
		"/api/v1/sprints/SEC-S1",
	}
	for _, p := range hidden {
		expect("bob", "GET", p, nil, 404)
		expect("alice", "GET", p, nil, 200)
	}
	lists := []string{
		"/api/v1/issues", "/api/v1/issues?project=SEC", "/api/v1/notes", "/api/v1/tasks?view=all",
		"/api/v1/search?q=секрет", "/api/v1/search?q=тайн", "/api/v1/tags", "/api/v1/tracker/projects",
		"/api/v1/calendar?from=2026-11-01&to=2026-11-30",
	}
	for _, p := range lists {
		out := string(expect("bob", "GET", p, nil, 200))
		for _, leak := range []string{"SEC", "sec-doc", "секрет", "тайн", "sectask"} {
			if strings.Contains(out, leak) {
				t.Errorf("bob GET %s leaks %q: %s", p, leak, out)
			}
		}
	}
	for _, p := range []string{"/api/v1/issues", "/api/v1/search?q=секрет", "/api/v1/tags", "/api/v1/calendar?from=2026-11-01&to=2026-11-30"} {
		if out := string(expect("alice", "GET", p, nil, 200)); !strings.Contains(out, "SEC") && !strings.Contains(out, "секрет") {
			t.Errorf("alice GET %s misses SEC: %s", p, out)
		}
	}
	var bobStats, bossStats domain.Stats
	json.Unmarshal(expect("bob", "GET", "/api/v1/stats", nil, 200), &bobStats)
	json.Unmarshal(expect("boss", "GET", "/api/v1/stats", nil, 200), &bossStats)
	if bobStats.Notes != bossStats.Notes-4 || bobStats.Tasks != bossStats.Tasks-1 {
		t.Errorf("stats bob %+v, boss %+v", bobStats, bossStats)
	}

	// Writes into SEC are refused for bob, whatever the door.
	expect("bob", "PATCH", "/api/v1/issues/SEC-1", map[string]any{"title": "взлом"}, 404)
	expect("bob", "POST", "/api/v1/issues", map[string]any{"project": "SEC", "title": "x"}, 404)
	expect("bob", "POST", "/api/v1/tracker/projects/SEC/sprints", nil, 404)
	expect("bob", "PUT", "/api/v1/notes/60-documents/sec-doc.md", map[string]any{"body": "x"}, 404)
	expect("bob", "POST", "/api/v1/tasks", map[string]any{"note": "60-documents/sec-doc.md", "text": "x"}, 404)
	expect("bob", "POST", "/api/v1/notes", map[string]any{"path": "20-projects/SEC/new.md", "title": "x"}, 403)
	expect("bob", "POST", "/api/v1/notes", map[string]any{"title": "x", "parent": "note:20-projects/sec.md"}, 404)
	expect("bob", "PATCH", "/api/v1/tasks/sectask", map[string]any{"status": "done"}, 404)
	// …but the open project and the workspace stay writable for him.
	expect("bob", "PATCH", "/api/v1/issues/WEB-1", map[string]any{"title": "Открытый тикет 2"}, 200)
	expect("bob", "POST", "/api/v1/notes", map[string]any{"title": "Заметка Боба"}, 201)

	// The designers group reads SEC; alice herself may write.
	expect("alice", "PATCH", "/api/v1/issues/SEC-1", map[string]any{"title": "Закрытый тикет 2"}, 200)

	// A workspace viewer reads but never writes.
	expect("vic", "GET", "/api/v1/issues/WEB-1", nil, 200)
	expect("vic", "PATCH", "/api/v1/issues/WEB-1", map[string]any{"title": "x"}, 403)
	expect("vic", "POST", "/api/v1/notes", map[string]any{"title": "x"}, 403)
	expect("vic", "POST", "/api/v1/issues", map[string]any{"project": "WEB", "title": "x"}, 403)
	expect("vic", "POST", "/api/v1/tracker/projects", map[string]any{"key": "NEW", "title": "x"}, 403)
	expect("vic", "GET", "/api/v1/issues/SEC-1", nil, 404)

	// Administration is for workspace admins only.
	for _, p := range []struct{ method, path string }{
		{"GET", "/api/v1/integrations"}, {"GET", "/api/v1/passwords/stores"}, {"POST", "/api/v1/reindex"},
	} {
		expect("bob", p.method, p.path, nil, 403)
		expect("vic", p.method, p.path, nil, 403)
	}
	expect("boss", "POST", "/api/v1/reindex", nil, 200)

	// SSE: bob learns about WEB, not about SEC.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+tokens["bob"])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := srv.Client().Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	time.Sleep(100 * time.Millisecond) // let the subscription register
	expect("alice", "PATCH", "/api/v1/issues/SEC-1", map[string]any{"title": "Закрытый тикет 3"}, 200)
	expect("alice", "PATCH", "/api/v1/issues/WEB-1", map[string]any{"title": "Открытый тикет 3"}, 200)
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "SEC") {
			t.Fatalf("bob received a SEC event: %s", line)
		}
		if strings.Contains(line, "WEB-1") {
			break
		}
	}
	cancel()

	// The directory holds everyone who signed in, with groups from Keycloak;
	// e-mail is shown to admins only.
	if out := string(expect("boss", "GET", "/api/v1/users?q=alice", nil, 200)); !strings.Contains(out, "alice@studio.test") {
		t.Fatalf("admin sees e-mail: %s", out)
	}
	var users struct {
		Items []struct {
			Username string   `json:"username"`
			Email    string   `json:"email"`
			Groups   []string `json:"groups"`
			Role     string   `json:"role"`
		} `json:"items"`
	}
	json.Unmarshal(expect("bob", "GET", "/api/v1/users", nil, 200), &users)
	got := map[string]string{}
	for _, u := range users.Items {
		got[u.Username] = u.Role + "|" + strings.Join(u.Groups, ",") + "|" + u.Email
	}
	if got["alice"] != "member|designers|" || got["vic"] != "viewer|guests|" || got["boss"] != "admin||" || len(got) != 4 {
		t.Fatalf("directory as bob = %v", got)
	}
	if out := string(expect("vic", "GET", "/api/v1/groups", nil, 200)); !strings.Contains(out, `"designers"`) {
		t.Fatalf("groups = %s", out)
	}
	if out := string(expect("bob", "GET", "/api/v1/users?q=ALI", nil, 200)); !strings.Contains(out, "alice") || strings.Contains(out, "vic") {
		t.Fatalf("search = %s", out)
	}

	// Removing every member opens the project again.
	expect("boss", "DELETE", "/api/v1/tracker/projects/SEC/members/user/alice", nil, 200)
	expect("boss", "DELETE", "/api/v1/tracker/projects/SEC/members/group/designers", nil, 200)
	expect("bob", "GET", "/api/v1/issues/SEC-1", nil, 200)
}
