package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/villetacore/lumen/server/internal/state/statetest"
)

const verifier = "a-long-enough-verifier-for-pkce-0123456789-abcdef"
const redirect = "http://127.0.0.1:4000/cb"

func challenge() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func setClock(t *testing.T, at *time.Time) {
	t.Helper()
	nowFunc = func() time.Time { return *at }
	t.Cleanup(func() { nowFunc = time.Now })
}

func connected(t *testing.T, s *Store) (Client, Tokens) {
	t.Helper()
	ctx := context.Background()
	c, err := s.Register(ctx, "Claude", []string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.Approve(ctx, c.ID, "sub-1", redirect, challenge())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.Exchange(ctx, code, c.ID, redirect, verifier)
	if err != nil {
		t.Fatal(err)
	}
	return c, tok
}

func TestLoopbackRedirectMayChangePort(t *testing.T) {
	c := Client{RedirectURIs: []string{"http://127.0.0.1:4000/cb", "https://claude.ai/api/mcp/auth_callback"}}
	for uri, want := range map[string]bool{
		"http://127.0.0.1:5999/cb":                     true,
		"http://127.0.0.1:5999/other":                  false,
		"http://localhost:4000/cb":                     false,
		"https://claude.ai/api/mcp/auth_callback":      true,
		"https://claude.ai:8443/api/mcp/auth_callback": false,
		"https://evil.example/api/mcp/auth_callback":   false,
	} {
		if got := c.Allows(uri); got != want {
			t.Errorf("%s: %v", uri, got)
		}
	}
}

func TestTokensSurviveRestart(t *testing.T) {
	open := statetest.Opener(t)
	st := open()
	c, tok := connected(t, New(st.DB()))
	st.Close()

	s := New(open().DB())
	ctx := context.Background()
	if got, err := s.Verify(ctx, tok.Access); err != nil || got != "sub-1" {
		t.Fatalf("access after restart: %q %v", got, err)
	}
	if _, err := s.Refresh(ctx, tok.Refresh, c.ID); err != nil {
		t.Fatalf("refresh after restart: %v", err)
	}
}

func TestExpiry(t *testing.T) {
	st := statetest.Open(t)
	defer st.Close()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	setClock(t, &now)
	s := New(st.DB())
	ctx := context.Background()

	c, tok := connected(t, s)

	code, err := s.Approve(ctx, c.ID, "sub-2", redirect, challenge())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(CodeTTL + time.Second)
	if _, err := s.Exchange(ctx, code, c.ID, redirect, verifier); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expired code: %v", err)
	}

	now = now.Add(AccessTTL)
	if _, err := s.Verify(ctx, tok.Access); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expired access token: %v", err)
	}
	next, err := s.Refresh(ctx, tok.Refresh, c.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := s.Verify(ctx, next.Access); err != nil {
		t.Fatalf("refreshed: %v", err)
	}

	// Refreshing does not extend the grant.
	now = now.Add(GrantTTL)
	if _, err := s.Refresh(ctx, next.Refresh, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("after the grant ended: %v", err)
	}
}

func TestRefreshReuse(t *testing.T) {
	st := statetest.Open(t)
	defer st.Close()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	setClock(t, &now)
	s := New(st.DB())
	ctx := context.Background()
	c, tok := connected(t, s)

	first, err := s.Refresh(ctx, tok.Refresh, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Two sessions refreshing at once, or a retry: both keep working.
	second, err := s.Refresh(ctx, tok.Refresh, c.ID)
	if err != nil {
		t.Fatalf("within the leeway: %v", err)
	}
	for _, access := range []string{first.Access, second.Access} {
		if _, err := s.Verify(ctx, access); err != nil {
			t.Fatalf("both tokens should work: %v", err)
		}
	}
	// Later reuse means the token leaked: the whole grant goes.
	now = now.Add(refreshLeeway + time.Second)
	if _, err := s.Refresh(ctx, tok.Refresh, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("reuse: %v", err)
	}
	if _, err := s.Verify(ctx, first.Access); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("grant should be gone: %v", err)
	}
	if _, err := s.Refresh(ctx, first.Refresh, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("fresh refresh token should be gone: %v", err)
	}
}

func TestReconnectReplacesTheConnection(t *testing.T) {
	st := statetest.Open(t)
	defer st.Close()
	s := New(st.DB())
	ctx := context.Background()
	c, tok := connected(t, s)
	code, err := s.Approve(ctx, c.ID, "sub-1", redirect, challenge())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exchange(ctx, code, c.ID, redirect, verifier); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, tok.Access); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("old token: %v", err)
	}
	list, err := s.Connections(ctx, "sub-1")
	if err != nil || len(list) != 1 || list[0].RedirectHost != "это устройство" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestUnusedClientsArePrunedAndCapped(t *testing.T) {
	st := statetest.Open(t)
	defer st.Close()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	setClock(t, &now)
	s := New(st.DB())
	ctx := context.Background()
	keep, _ := connected(t, s)
	abandoned, err := s.Register(ctx, "x", []string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(unusedClientTTL + time.Second)
	if _, err := s.Register(ctx, "y", []string{redirect}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Client(ctx, abandoned.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("abandoned: %v", err)
	}
	if _, err := s.Client(ctx, keep.ID); err != nil {
		t.Fatalf("connected client: %v", err)
	}
	for i := 0; i < maxUnusedClients; i++ {
		if _, err := s.Register(ctx, "z", []string{redirect}); err != nil {
			if !errors.Is(err, ErrTooMany) {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("no cap")
}
