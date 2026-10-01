package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeKeycloak serves discovery, JWKS and the token endpoint.
type fakeKeycloak struct {
	*httptest.Server
	key   *rsa.PrivateKey
	kid   string
	realm string
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fk := &fakeKeycloak{key: key, kid: "test-key-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/realms/main/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(Discovery{
			Issuer:        fk.realm,
			AuthURL:       fk.realm + "/protocol/openid-connect/auth",
			TokenURL:      fk.realm + "/protocol/openid-connect/token",
			JWKSURL:       fk.realm + "/protocol/openid-connect/certs",
			EndSessionURL: fk.realm + "/protocol/openid-connect/logout",
		})
	})
	mux.HandleFunc("/realms/main/protocol/openid-connect/certs", func(w http.ResponseWriter, _ *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
		fmt.Fprintf(w, `{"keys":[{"kid":%q,"kty":"RSA","alg":"RS256","use":"sig","n":%q,"e":%q}]}`,
			fk.kid, n, e)
	})
	mux.HandleFunc("/realms/main/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_request","error_description":"PKCE required"}`, http.StatusBadRequest)
			return
		}
		idToken := fk.sign(t, map[string]any{
			"sub": "u-1", "iss": fk.realm, "aud": "lumen",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"preferred_username": "alex", "email": "alex@example.com",
			"realm_access": map[string]any{"roles": []string{"lumen-user"}},
		})
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": idToken, "id_token": idToken,
			"refresh_token": "r-1", "expires_in": 300, "token_type": "Bearer",
		})
	})

	fk.Server = httptest.NewServer(mux)
	fk.realm = fk.Server.URL + "/realms/main"
	t.Cleanup(fk.Close)
	return fk
}

func (fk *fakeKeycloak) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	return fk.signWithKey(t, fk.key, fk.kid, "RS256", claims)
}

func (fk *fakeKeycloak) signWithKey(t *testing.T, key *rsa.PrivateKey, kid, alg string, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT", "kid": kid})
	payload, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + enc.EncodeToString(sig)
}

func newProvider(t *testing.T, fk *fakeKeycloak, roles ...string) *Provider {
	t.Helper()
	p, err := NewProvider(context.Background(), Config{
		Issuer: fk.realm, ClientID: "lumen",
		RedirectURL:   "http://127.0.0.1:8787/auth/callback",
		RequiredRoles: roles, InsecureCookie: true,
	}, fk.Client())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDiscovery(t *testing.T) {
	fk := newFakeKeycloak(t)
	d, err := Discover(context.Background(), fk.realm, fk.Client())
	if err != nil {
		t.Fatal(err)
	}
	if d.TokenURL == "" || d.JWKSURL == "" {
		t.Fatalf("discovery = %+v", d)
	}
}

func TestVerifyGoodToken(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)

	token := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "lumen",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"preferred_username": "alex",
		"realm_access":       map[string]any{"roles": []string{"lumen-user", "offline_access"}},
	})

	claims, err := p.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "alex" || claims.Subject != "u-1" {
		t.Errorf("claims = %+v", claims)
	}
	if claims.DisplayName() != "alex" {
		t.Errorf("display name = %q", claims.DisplayName())
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	token := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "lumen",
		"exp": time.Now().Add(-2 * time.Hour).Unix(),
	})
	if _, err := p.Verify(context.Background(), token); !errors.Is(err, ErrExpired) {
		t.Fatalf("want ErrExpired, got %v", err)
	}
}

func TestVerifyRejectsWrongIssuer(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	token := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": "https://evil.example.com/realms/main", "aud": "lumen",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := p.Verify(context.Background(), token); !errors.Is(err, ErrWrongIssuer) {
		t.Fatalf("want ErrWrongIssuer, got %v", err)
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	token := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "grafana", "azp": "grafana",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := p.Verify(context.Background(), token); !errors.Is(err, ErrWrongAud) {
		t.Fatalf("токен другого сервиса не должен приниматься, got %v", err)
	}
}

// Keycloak кладёт client id в azp для access-токенов — это тоже валидно.
func TestVerifyAcceptsAzp(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	token := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "account", "azp": "lumen",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := p.Verify(context.Background(), token); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsForeignSignature(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	// Тот же kid, чужой ключ: подпись не должна пройти.
	token := fk.signWithKey(t, other, fk.kid, "RS256", map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "lumen",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := p.Verify(context.Background(), token); !errors.Is(err, ErrBadToken) {
		t.Fatalf("want ErrBadToken, got %v", err)
	}
}

// Классическая атака: alg=none. Должна отвергаться до всякой проверки подписи.
func TestVerifyRejectsAlgNone(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT", "kid": fk.kid})
	payload, _ := json.Marshal(map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "lumen",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token := enc.EncodeToString(header) + "." + enc.EncodeToString(payload) + "."
	if _, err := p.Verify(context.Background(), token); err == nil {
		t.Fatal("alg=none принят — это дыра")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	for _, bad := range []string{"", "abc", "a.b", "a.b.c.d", "not.a.jwt"} {
		if _, err := p.Verify(context.Background(), bad); err == nil {
			t.Errorf("%q принят", bad)
		}
	}
}

func TestRequiredRoles(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk, "lumen-user")

	ok := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "lumen",
		"exp":          time.Now().Add(time.Hour).Unix(),
		"realm_access": map[string]any{"roles": []string{"lumen-user"}},
	})
	if _, err := p.Verify(context.Background(), ok); err != nil {
		t.Fatalf("пользователь с ролью должен проходить: %v", err)
	}

	no := fk.sign(t, map[string]any{
		"sub": "u-2", "iss": fk.realm, "aud": "lumen",
		"exp":          time.Now().Add(time.Hour).Unix(),
		"realm_access": map[string]any{"roles": []string{"grafana-admin"}},
	})
	if _, err := p.Verify(context.Background(), no); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestRolesFromGroups(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk, "family")
	token := fk.sign(t, map[string]any{
		"sub": "u-1", "iss": fk.realm, "aud": "lumen",
		"exp":    time.Now().Add(time.Hour).Unix(),
		"groups": []string{"/family", "/admins"},
	})
	if _, err := p.Verify(context.Background(), token); err != nil {
		t.Fatalf("группа Keycloak должна работать как роль: %v", err)
	}
}

func TestAuthCodeURLUsesPKCE(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)

	raw, err := p.AuthCodeURL("/view/today")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "code_challenge=") || !strings.Contains(raw, "code_challenge_method=S256") {
		t.Errorf("в ссылке нет PKCE: %s", raw)
	}
	if !strings.Contains(raw, "client_id=lumen") {
		t.Errorf("нет client_id: %s", raw)
	}
}

func TestLoginRoundTrip(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk, "lumen-user")

	raw, err := p.AuthCodeURL("/calendar")
	if err != nil {
		t.Fatal(err)
	}
	state := urlParam(t, raw, "state")

	session, returnTo, err := p.Exchange(context.Background(), state, "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if session.Claims.Username != "alex" {
		t.Errorf("claims = %+v", session.Claims)
	}
	if returnTo != "/calendar" {
		t.Errorf("returnTo = %q", returnTo)
	}

	got, ok := p.Session(session.ID)
	if !ok || got.Claims.Subject != "u-1" {
		t.Fatal("сессия не сохранилась")
	}

	// Повторное использование state должно проваливаться.
	if _, _, err := p.Exchange(context.Background(), state, "the-code"); err == nil {
		t.Error("state одноразовый, повтор должен отвергаться")
	}

	p.EndSession(session.ID, "http://localhost/")
	if _, ok := p.Session(session.ID); ok {
		t.Error("сессия должна была исчезнуть после выхода")
	}
}

func TestExpiredSessionIsDropped(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	p.sessions["old"] = &Session{ID: "old", Expiry: time.Now().Add(-time.Minute)}
	if _, ok := p.Session("old"); ok {
		t.Fatal("просроченная сессия не должна отдаваться")
	}
}

func TestCookieFlags(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	c := p.Cookie("x", time.Now().Add(time.Hour))
	if !c.HttpOnly {
		t.Error("cookie обязана быть HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Error("нужен SameSite=Lax, иначе редирект с Keycloak не донесёт cookie")
	}
}

func urlParam(t *testing.T, raw, key string) string {
	t.Helper()
	i := strings.Index(raw, "?")
	if i < 0 {
		t.Fatalf("нет параметров в %s", raw)
	}
	for _, kv := range strings.Split(raw[i+1:], "&") {
		k, v, _ := strings.Cut(kv, "=")
		if k == key {
			return v
		}
	}
	t.Fatalf("нет параметра %s в %s", key, raw)
	return ""
}
