package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/auth"
	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
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
