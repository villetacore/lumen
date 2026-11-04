package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func redisStore(t *testing.T) (*RedisSessions, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	st, err := NewRedisSessions(context.Background(), "redis://"+srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, srv
}

func TestPasswordLoginCreatesSession(t *testing.T) {
	fk := newFakeKeycloak(t)
	p := newProvider(t, fk)
	s, err := p.PasswordLogin(context.Background(), "alex", "верный", "")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Direct || s.Claims.Username != "alex" {
		t.Errorf("сессия: %+v", s)
	}
	if got, ok := p.Session(s.ID); !ok || got.Claims.Username != "alex" {
		t.Error("сессия не найдена")
	}
	// The form has no Keycloak page to go back to; the session ends from the server.
	if to := p.EndSession(s.ID, "https://lumen/"); to != "https://lumen/" {
		t.Errorf("после выхода: %s", to)
	}
	if fk.revoked != "r-1" {
		t.Errorf("сессия Keycloak не завершена: %q", fk.revoked)
	}
	if _, ok := p.Session(s.ID); ok {
		t.Error("сессия пережила выход")
	}
}

func TestPasswordLoginFailures(t *testing.T) {
	fk := newFakeKeycloak(t)
	cases := map[string]error{
		"неверный": ErrBadCredentials,
		"setup":    ErrAccountSetup,
		"disabled": ErrPasswordLoginDisabled,
	}
	p := newProvider(t, fk)
	for password, want := range cases {
		if _, err := p.PasswordLogin(context.Background(), "alex", password, ""); !errors.Is(err, want) {
			t.Errorf("%s: %v", password, err)
		}
	}
	// A valid password without the required role opens nothing.
	strict := newProvider(t, fk, "admins")
	if _, err := strict.PasswordLogin(context.Background(), "alex", "верный", ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("без роли: %v", err)
	}
	if _, err := p.PasswordLogin(context.Background(), "alex", "нужен-код", "123456"); err != nil {
		t.Errorf("с кодом: %v", err)
	}
	if fk.lastOTP != "123456" {
		t.Errorf("код 2FA не передан: %q", fk.lastOTP)
	}
}

func TestSessionSurvivesRestartThroughRedis(t *testing.T) {
	fk := newFakeKeycloak(t)
	st, srv := redisStore(t)
	before := newProvider(t, fk)
	before.UseSessionStore(st)
	s, err := before.PasswordLogin(context.Background(), "alex", "верный", "")
	if err != nil {
		t.Fatal(err)
	}

	// A new process: nothing in memory.
	after := newProvider(t, fk)
	after.UseSessionStore(st)
	got, ok := after.Session(s.ID)
	if !ok || got.Claims.Username != "alex" || got.Claims.Email != "alex@example.com" || !got.Direct {
		t.Fatalf("после рестарта: %+v, %v", got, ok)
	}
	if roles := got.Claims.Roles("lumen"); len(roles) != 1 || roles[0] != "lumen-user" {
		t.Errorf("роли: %v", roles)
	}

	// What Redis holds must not open anything by itself.
	for _, key := range srv.Keys() {
		if strings.Contains(key, s.ID) {
			t.Errorf("значение cookie в ключе: %s", key)
		}
		value, _ := srv.Get(key)
		if strings.Contains(value, "r-1") || strings.Contains(value, s.AccessToken) {
			t.Errorf("токены в Redis: %s", value)
		}
		if ttl := srv.TTL(key); ttl <= 0 || ttl > 13*time.Hour {
			t.Errorf("срок ключа: %v", ttl)
		}
	}

	// Signing out in one process ends the session for the next.
	after.EndSession(s.ID, "/")
	third := newProvider(t, fk)
	third.UseSessionStore(st)
	if _, ok := third.Session(s.ID); ok {
		t.Error("сессия пережила выход")
	}
}

func TestSessionKeepsWorkingWhenRedisFails(t *testing.T) {
	fk := newFakeKeycloak(t)
	st, srv := redisStore(t)
	p := newProvider(t, fk)
	p.UseSessionStore(st)
	s, err := p.PasswordLogin(context.Background(), "alex", "верный", "")
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	if _, ok := p.Session(s.ID); !ok {
		t.Error("падение Redis разлогинило работающего пользователя")
	}
	if _, err := p.PasswordLogin(context.Background(), "alex", "верный", ""); err != nil {
		t.Errorf("вход при упавшем Redis: %v", err)
	}
	if _, ok := p.Session("чужой"); ok {
		t.Error("неизвестная сессия принята")
	}
}

func TestRedisURLErrorHidesPassword(t *testing.T) {
	_, err := NewRedisSessions(context.Background(), "ftp://user:s3cret@host")
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("ошибка: %v", err)
	}
}
