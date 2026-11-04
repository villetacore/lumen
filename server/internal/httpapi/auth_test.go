package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/villetacore/lumen/server/internal/auth"
	"github.com/villetacore/lumen/server/internal/config"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/service"
	"github.com/villetacore/lumen/server/internal/vault"
)

// С включённым SSO статический токен остаётся запасным входом: его пускает
// API, и /api/v1/me должен считать такого клиента вошедшим.
func TestStaticTokenAlongsideOIDC(t *testing.T) {
	var issuer string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/certs",
		})
	}))
	defer idp.Close()
	issuer = idp.URL

	provider, err := auth.NewProvider(context.Background(), auth.Config{Issuer: issuer, ClientID: "lumen"}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := index.NewMemory(v.Root)
	bus := service.NewBus()
	ix := service.NewIndexer(v, store, bus, log)
	handler := New(&config.Config{Token: "s3cret"}, store, v, ix, bus, nil, provider, nil, log).Handler()

	call := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	me := func(token string) identity {
		t.Helper()
		var id identity
		if err := json.Unmarshal(call("/api/v1/me", token).Body.Bytes(), &id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	if id := me("s3cret"); id.Mode != "oidc" || !id.Authenticated {
		t.Fatalf("с токеном: %+v", id)
	}
	if id := me("wrong"); id.Authenticated {
		t.Fatalf("с чужим токеном: %+v", id)
	}
	if code := call("/api/v1/stats", "s3cret").Code; code != http.StatusOK {
		t.Fatalf("stats с токеном: %d", code)
	}
	if code := call("/api/v1/stats", "").Code; code != http.StatusUnauthorized {
		t.Fatalf("stats без токена: %d", code)
	}
}

// A failed login must lead a person to an explanation, not to raw JSON, and the
// reason must not echo what the provider or the query string said.
func TestCallbackFailureRedirectsToExplanation(t *testing.T) {
	f := newOIDCFixture(t, map[string]string{"a.md": "# a\n"})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, query := range []string{"error=access_denied&error_description=%3Cscript%3E", "state=forged&code=x"} {
		resp, err := client.Get(f.srv.URL + "/auth/callback?" + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/?auth_error=failed" {
			t.Errorf("%s: %d → %q", query, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

func postLogin(t *testing.T, base, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/auth/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func TestPasswordLoginThroughTheForm(t *testing.T) {
	f := newOIDCFixture(t, map[string]string{"a.md": "# a\n"})

	// Nothing is visible before signing in.
	resp, err := http.Get(f.srv.URL + "/api/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("без входа: %d", resp.StatusCode)
	}

	resp, body := postLogin(t, f.srv.URL, `{"username":"boss","password":"не тот"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "invalid_credentials") || len(resp.Cookies()) != 0 {
		t.Fatalf("неверный пароль: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(body, "не тот") {
		t.Error("пароль в ответе")
	}

	resp, body = postLogin(t, f.srv.URL, `{"username":"boss","password":"верный"}`, nil)
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 1 {
		t.Fatalf("вход: %d %s", resp.StatusCode, body)
	}
	cookie := resp.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie: %+v", cookie)
	}
	req, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/api/v1/me", nil)
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		PasswordLogin bool   `json:"password_login"`
	}
	json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if !me.Authenticated || me.Username != "boss" || !me.PasswordLogin {
		t.Errorf("me: %+v", me)
	}

	// A form on another site must not sign anyone in.
	resp, _ = postLogin(t, f.srv.URL, `{"username":"boss","password":"верный"}`, map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 {
		t.Errorf("с чужого сайта: %d", resp.StatusCode)
	}
	if resp, _ = postLogin(t, f.srv.URL, `{"username":"","password":""}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("пустой: %d", resp.StatusCode)
	}
}

func TestPasswordGuessingIsLimitedPerLogin(t *testing.T) {
	f := newOIDCFixture(t, map[string]string{"a.md": "# a\n"})
	for i := range loginMaxFails {
		if resp, _ := postLogin(t, f.srv.URL, `{"username":"Boss","password":"нет"}`, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("попытка %d: %d", i, resp.StatusCode)
		}
	}
	// Even the right password waits now, and case does not make a new login.
	if resp, _ := postLogin(t, f.srv.URL, `{"username":"boss","password":"верный"}`, nil); resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("сверх лимита: %d", resp.StatusCode)
	}
	if resp, _ := postLogin(t, f.srv.URL, `{"username":"alice","password":"верный"}`, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("другой логин: %d", resp.StatusCode)
	}
	l := newLoginLimiter()
	start := time.Now()
	l.now = func() time.Time { return start }
	for range loginMaxFails {
		l.fail("x")
	}
	if l.allow("x") {
		t.Error("лимит не сработал")
	}
	l.now = func() time.Time { return start.Add(loginWindow + time.Second) }
	if !l.allow("x") {
		t.Error("окно не прошло")
	}
}
