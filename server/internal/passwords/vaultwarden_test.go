package passwords

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// vwEncrypt writes a type 2 string, the way a Bitwarden client does.
func vwEncrypt(t *testing.T, plain []byte, key symKey) string {
	t.Helper()
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	data := append(append([]byte{}, plain...), make([]byte, pad)...)
	for i := len(plain); i < len(data); i++ {
		data[i] = byte(pad)
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key.enc)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
	mac := hmac.New(sha256.New, key.mac)
	mac.Write(iv)
	mac.Write(out)
	enc := base64.StdEncoding.EncodeToString
	return "2." + enc(iv) + "|" + enc(out) + "|" + enc(mac.Sum(nil))
}

func randomKey(t *testing.T) ([]byte, symKey) {
	t.Helper()
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	key, err := newSymKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw, key
}

// fakeVault is a test double of the server: it knows only ciphertext and the
// password hash, like the real one.
type fakeVault struct {
	t            *testing.T
	email        string
	kdf          vwKDF
	passwordHash string
	sync         map[string]any
	tokens       int
	syncs        int
	expireToken  bool
	forms        []map[string]string
	twoFactor    bool
}

func (f *fakeVault) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{
			// PascalCase, as older servers send it.
			"Kdf": f.kdf.Kdf, "KdfIterations": f.kdf.KdfIterations,
			"KdfMemory": f.kdf.KdfMemory, "KdfParallelism": f.kdf.KdfParallelism,
		})
	})
	mux.HandleFunc("POST /identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			f.t.Error(err)
		}
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		f.forms = append(f.forms, form)
		if f.twoFactor {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"TwoFactorProviders": []string{"0"}})
			return
		}
		ok := false
		switch form["grant_type"] {
		case "password":
			ok = form["username"] == f.email && form["password"] == f.passwordHash
		case "client_credentials":
			ok = form["client_id"] == "user.1" && form["client_secret"] == "api-secret"
		case "refresh_token":
			ok = form["refresh_token"] == "refresh-1"
		}
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error_description": "invalid"})
			return
		}
		f.tokens++
		f.expireToken = false
		json.NewEncoder(w).Encode(map[string]any{"access_token": "token-1", "refresh_token": "refresh-1", "expires_in": 3600})
	})
	mux.HandleFunc("GET /api/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" || f.expireToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.syncs++
		json.NewEncoder(w).Encode(f.sync)
	})
	srv := httptest.NewServer(mux)
	f.t.Cleanup(srv.Close)
	return srv
}

const testMaster = "correct horse battery"

// newFakeVault builds an account with a personal login, a card, an organization
// item, an item with its own key, a trashed item and a foreign-key item.
func newFakeVault(t *testing.T, kdf vwKDF) *fakeVault {
	t.Helper()
	email := "owner@example.com"
	master, err := deriveMasterKey([]byte(testMaster), email, kdf)
	if err != nil {
		t.Fatal(err)
	}
	stretched, err := stretchMasterKey(master)
	if err != nil {
		t.Fatal(err)
	}
	userRaw, user := randomKey(t)
	orgRaw, org := randomKey(t)
	itemRaw, item := randomKey(t)
	_, stranger := randomKey(t)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, &rsaKey.PublicKey, orgRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := func(s string, k symKey) string { return vwEncrypt(t, []byte(s), k) }

	return &fakeVault{
		t: t, email: email, kdf: kdf, passwordHash: masterPasswordHash(master, []byte(testMaster)),
		sync: map[string]any{
			"profile": map[string]any{
				"key":        vwEncrypt(t, userRaw, stretched),
				"privateKey": vwEncrypt(t, der, user),
				"organizations": []map[string]any{
					{"id": "org-1", "key": "4." + base64.StdEncoding.EncodeToString(wrapped)},
					{"id": "org-broken", "key": "4.AAAA"},
				},
			},
			"folders": []map[string]any{{"id": "f1", "name": e("Работа", user)}},
			"ciphers": []map[string]any{
				{"id": "login", "type": 1, "folderId": "f1", "name": e("GitLab", user), "notes": e("заметка", user),
					"login": map[string]any{
						"username": e("alice", user), "password": e("s3cret", user),
						"totp": e("JBSWY3DPEHPK3PXP", user),
						"uris": []map[string]any{{"uri": e("https://gitlab.example.com", user)}},
					},
					"fields": []map[string]any{{"name": e("PIN", user), "value": e("4242", user), "type": 1}},
				},
				{"id": "card", "type": 3, "name": e("Карта", user),
					"card": map[string]any{"number": e("4111", user), "code": e("123", user)}},
				{"id": "shared", "type": 1, "organizationId": "org-1", "name": e("Общий", org),
					"login": map[string]any{"password": e("org-pass", org)}},
				{"id": "own-key", "type": 2, "key": vwEncrypt(t, itemRaw, user), "name": e("Своя запись", item), "notes": e("текст", item)},
				{"id": "trash", "type": 1, "name": e("Удалённая", user), "deletedDate": "2026-01-01T00:00:00Z"},
				{"id": "broken", "type": 1, "organizationId": "org-broken", "name": e("Чужая", stranger),
					"login": map[string]any{"password": e("x", stranger)}},
			},
		},
	}
}

func openVault(t *testing.T, f *fakeVault, extra map[string]string) (*Manager, string) {
	t.Helper()
	srv := f.server()
	m := NewManager(t.TempDir(), &fakeRunner{}, func(_ context.Context, ref string) ([]byte, error) {
		if ref == "env:SECRET" {
			return []byte("api-secret\n"), nil
		}
		return nil, errors.New("неизвестная ссылка")
	})
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"server": srv.URL, "email": "Owner@Example.com"}
	for k, v := range extra {
		values[k] = v
	}
	cfg, err := m.Add(Config{Kind: "vaultwarden", Name: "Сейф", Values: values})
	if err != nil {
		t.Fatal(err)
	}
	return m, cfg.ID
}

func TestVaultwardenReadsEveryKindOfItem(t *testing.T) {
	kdfs := map[string]vwKDF{
		"pbkdf2": {Kdf: 0, KdfIterations: 1000},
		"argon2": {Kdf: 1, KdfIterations: 2, KdfMemory: 8, KdfParallelism: 1},
	}
	for name, kdf := range kdfs {
		t.Run(name, func(t *testing.T) {
			f := newFakeVault(t, kdf)
			m, id := openVault(t, f, nil)
			ctx := context.Background()

			if _, err := m.Items(ctx, id, ""); !errors.Is(err, ErrLocked) {
				t.Fatalf("закрытое хранилище: %v", err)
			}
			if err := m.Unlock(ctx, id, []byte(testMaster)); err != nil {
				t.Fatal(err)
			}
			// The password itself must never reach the server, only its hash.
			for k, v := range f.forms[0] {
				if strings.Contains(v, testMaster) {
					t.Errorf("мастер-пароль ушёл на сервер в поле %s", k)
				}
			}

			items, err := m.Items(ctx, id, "")
			if err != nil {
				t.Fatal(err)
			}
			byID := map[string]Item{}
			for _, it := range items {
				byID[it.ID] = it
			}
			if _, ok := byID["trash"]; ok {
				t.Error("запись из корзины в списке")
			}
			login := byID["login"]
			if login.Name != "GitLab" || login.Username != "alice" || login.URI != "https://gitlab.example.com" || login.Folder != "Работа" || login.Type != "login" {
				t.Errorf("логин: %+v", login)
			}
			if got := strings.Join(login.Fields, ","); got != "username,password,totp,uri,notes,field:PIN" {
				t.Errorf("поля: %s", got)
			}
			if blob, _ := json.Marshal(items); strings.Contains(string(blob), "s3cret") || strings.Contains(string(blob), "org-pass") {
				t.Errorf("значения секретов в списке: %s", blob)
			}
			if byID["shared"].Name != "Общий" || byID["own-key"].Name != "Своя запись" {
				t.Errorf("общая/со своим ключом: %+v %+v", byID["shared"], byID["own-key"])
			}
			// One unreadable organization must not break the others.
			if b := byID["broken"]; b.Name != "(не удалось расшифровать)" || len(b.Fields) != 0 {
				t.Errorf("нечитаемая запись: %+v", b)
			}

			for _, c := range []struct{ item, field, want string }{
				{"login", "password", "s3cret"}, {"login", "notes", "заметка"}, {"login", "field:PIN", "4242"},
				{"card", "number", "4111"}, {"shared", "password", "org-pass"}, {"own-key", "notes", "текст"},
			} {
				if got, err := m.Reveal(ctx, id, c.item, c.field); err != nil || got != c.want {
					t.Errorf("%s/%s = %q, %v", c.item, c.field, got, err)
				}
			}
			if code, err := m.Reveal(ctx, id, "login", "totp"); err != nil || len(code) != 6 {
				t.Errorf("код: %q, %v", code, err)
			}
			if _, err := m.Reveal(ctx, id, "login", "number"); !errors.Is(err, ErrNoField) {
				t.Errorf("чужое поле: %v", err)
			}
			if _, err := m.Reveal(ctx, id, "broken", "password"); err == nil {
				t.Error("запись без ключа раскрылась")
			}

			found, err := m.Items(ctx, id, "gitlab.EXAMPLE")
			if err != nil || len(found) != 1 || found[0].ID != "login" {
				t.Errorf("поиск: %+v, %v", found, err)
			}

			m.Lock(id)
			if _, err := m.Reveal(ctx, id, "login", "password"); !errors.Is(err, ErrLocked) {
				t.Errorf("после закрытия: %v", err)
			}
		})
	}
}

func TestVaultwardenRefusesWrongPassword(t *testing.T) {
	f := newFakeVault(t, vwKDF{Kdf: 0, KdfIterations: 1000})
	m, id := openVault(t, f, nil)
	if err := m.Unlock(context.Background(), id, []byte("не тот")); err == nil || !strings.Contains(err.Error(), "мастер-пароль") {
		t.Fatalf("ошибка: %v", err)
	}
	if _, err := m.Items(context.Background(), id, ""); !errors.Is(err, ErrLocked) {
		t.Errorf("после отказа: %v", err)
	}
}

// With an API key the server accepts any master password, so the only check is
// that the account key opens.
func TestVaultwardenAPIKeyStillChecksMasterPassword(t *testing.T) {
	f := newFakeVault(t, vwKDF{Kdf: 0, KdfIterations: 1000})
	m, id := openVault(t, f, map[string]string{"client_id": "user.1", "client_secret": "env:SECRET"})
	ctx := context.Background()
	if err := m.Unlock(ctx, id, []byte("не тот")); err == nil || !strings.Contains(err.Error(), "неверный мастер-пароль") {
		t.Fatalf("ошибка: %v", err)
	}
	if err := m.Unlock(ctx, id, []byte(testMaster)); err != nil {
		t.Fatal(err)
	}
	if got := f.forms[len(f.forms)-1]["grant_type"]; got != "client_credentials" {
		t.Errorf("grant: %s", got)
	}
	if _, has := f.forms[len(f.forms)-1]["password"]; has {
		t.Error("хеш пароля отправлен при входе по API-ключу")
	}
}

func TestVaultwardenExplainsTwoFactor(t *testing.T) {
	f := newFakeVault(t, vwKDF{Kdf: 0, KdfIterations: 1000})
	f.twoFactor = true
	m, id := openVault(t, f, nil)
	if err := m.Unlock(context.Background(), id, []byte(testMaster)); err == nil || !strings.Contains(err.Error(), "API-ключ") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestVaultwardenRenewsExpiredTokenAndRereads(t *testing.T) {
	f := newFakeVault(t, vwKDF{Kdf: 0, KdfIterations: 1000})
	m, id := openVault(t, f, nil)
	ctx := context.Background()
	if err := m.Unlock(ctx, id, []byte(testMaster)); err != nil {
		t.Fatal(err)
	}
	session, err := m.session(id)
	if err != nil {
		t.Fatal(err)
	}
	s := session.Data.(*vwSession)

	syncs := f.syncs
	if _, err := m.Items(ctx, id, ""); err != nil || f.syncs != syncs {
		t.Fatalf("свежий снимок перечитан: %v, %d → %d", err, syncs, f.syncs)
	}
	// The server revokes the token while the snapshot is old.
	s.syncedAt = time.Now().Add(-time.Hour)
	f.expireToken = true
	if _, err := m.Items(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	if f.tokens != 2 || f.syncs != syncs+1 {
		t.Errorf("токенов %d, синхронизаций %d", f.tokens, f.syncs-syncs)
	}
	if last := f.forms[len(f.forms)-1]; last["grant_type"] != "refresh_token" {
		t.Errorf("обновление: %v", last)
	}
}

func TestVaultwardenValidatesAddress(t *testing.T) {
	v := &Vaultwarden{}
	bad := []map[string]string{
		{"email": "a@b.c"},
		{"server": "ftp://x", "email": "a@b.c"},
		{"server": "http://vault.example.com", "email": "a@b.c"},
		{"server": "https://x.example.com", "email": "нет"},
		{"server": "https://x.example.com", "email": "a@b.c", "client_id": "user.1"},
	}
	for _, values := range bad {
		if err := v.Validate(Config{Values: values}); err == nil {
			t.Errorf("принято: %v", values)
		}
	}
	good := []map[string]string{
		{"server": "https://x.example.com/", "email": "a@b.c"},
		{"server": "http://127.0.0.1:8080", "email": "a@b.c"},
		{"server": "http://vaultwarden.lan", "email": "a@b.c", "allow_http": "true"},
	}
	for _, values := range good {
		if err := v.Validate(Config{Values: values}); err != nil {
			t.Errorf("отклонено %v: %v", values, err)
		}
	}
}

func TestVaultwardenDoesNotFollowRedirects(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked = true }))
	defer other.Close()
	f := newFakeVault(t, vwKDF{Kdf: 0, KdfIterations: 1000})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(f.kdf)
	})
	mux.HandleFunc("POST /identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := NewManager(t.TempDir(), &fakeRunner{}, nil)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	cfg, err := m.Add(Config{Kind: "vaultwarden", Values: map[string]string{"server": srv.URL, "email": f.email}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Unlock(context.Background(), cfg.ID, []byte(testMaster)); err == nil {
		t.Fatal("вход прошёл через перенаправление")
	}
	if leaked {
		t.Fatal("хеш пароля ушёл на адрес из перенаправления")
	}
}

// RFC 6238 Appendix B, SHA-1, with the 8-digit codes the RFC lists.
func TestTOTPKnownAnswers(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // base32("12345678901234567890")
	uri := "otpauth://totp/x?secret=" + secret + "&digits=8&period=30"
	for ts, want := range map[int64]string{59: "94287082", 1111111109: "07081804", 2000000000: "69279037"} {
		got, err := totpCode(uri, time.Unix(ts, 0))
		if err != nil || got != want {
			t.Errorf("t=%d: %q, %v, ждём %s", ts, got, err, want)
		}
	}
	if got, err := totpCode("gezd gnbv gy3t qojq gezd gnbv gy3t qojq", time.Unix(59, 0)); err != nil || got != "287082" {
		t.Errorf("голый секрет: %q, %v", got, err)
	}
	for _, bad := range []string{"", "steam://ABC", "!!!", "otpauth://totp/x?secret=GEZD&algorithm=MD5"} {
		if _, err := totpCode(bad, time.Unix(59, 0)); err == nil {
			t.Errorf("принято: %q", bad)
		}
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	_, key := randomKey(t)
	enc := vwEncrypt(t, []byte("данные"), key)
	if got, err := decryptString(enc, key); err != nil || string(got) != "данные" {
		t.Fatalf("%q, %v", got, err)
	}
	_, other := randomKey(t)
	if _, err := decryptString(enc, other); err == nil {
		t.Error("чужой ключ принят")
	}
	parts := strings.Split(enc, "|")
	if _, err := decryptString(parts[0]+"|"+parts[2]+"|"+parts[2], key); err == nil {
		t.Error("изменённые данные приняты")
	}
	for _, bad := range []string{"", "0.a|b", "2.a|b", "2.!|!|!"} {
		if _, err := decryptString(bad, key); err == nil {
			t.Errorf("принято: %q", bad)
		}
	}
}
