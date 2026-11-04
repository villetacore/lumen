package secretstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/villetacore/lumen/server/internal/secrets"
	"github.com/villetacore/lumen/server/internal/state/statetest"
)

const token = "glpat-не-настоящий-токен"

func TestPastedValueIsEncryptedAndResolves(t *testing.T) {
	ctx := context.Background()
	st := statetest.Open(t)
	dir := t.TempDir()
	s, err := New(st.DB(), "", dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.Keep(ctx, "integration:gitlab:token", "  "+token+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "stored:") || strings.Contains(ref, token) {
		t.Fatalf("ссылка: %s", ref)
	}
	var raw []byte
	if err := st.DB().QueryRow(`SELECT value FROM stored_secrets`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token)) {
		t.Error("значение лежит открытым текстом")
	}

	r := secrets.New(nil)
	r.Register(s.Provider())
	got, err := r.Resolve(ctx, ref)
	if err != nil || string(got) != token {
		t.Fatalf("%q, %v", got, err)
	}

	// The same key file opens it after a restart.
	again, err := New(st.DB(), "", dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Register(again.Provider())
	if got, err := r.Resolve(ctx, ref); err != nil || string(got) != token {
		t.Errorf("после рестарта: %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(dir, KeyFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("файл ключа: %v %v", info, err)
	}

	// Another key opens nothing, and the error names no value.
	other, err := New(st.DB(), "другой ключ", dir)
	if err != nil {
		t.Fatal(err)
	}
	r.Register(other.Provider())
	if _, err := r.Resolve(ctx, ref); !errors.Is(err, ErrUnreadable) || strings.Contains(err.Error(), token) {
		t.Errorf("чужой ключ: %v", err)
	}

	r.Register(s.Provider())
	if err := s.Forget(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(ctx, ref); err == nil {
		t.Error("забытое значение осталось")
	}
}

func TestReferencesStayReferences(t *testing.T) {
	ctx := context.Background()
	s, err := New(statetest.Open(t).DB(), "ключ", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"env:GITLAB_TOKEN", "file:/run/secrets/x", "/run/secrets/x", "stored:abc", "command:pass show x"} {
		got, err := s.Keep(ctx, "o", ref)
		if err != nil || got != ref {
			t.Errorf("%s → %s, %v", ref, got, err)
		}
		if ref != "stored:abc" {
			if err := s.Forget(ctx, ref); err != nil {
				t.Errorf("Forget %s: %v", ref, err)
			}
		}
	}
	// The value from the bug report: no scheme, so it is a value, not a file name.
	got, err := s.Keep(ctx, "o", "ValueWithoutAnySchemeNotReal")
	if err != nil || !strings.HasPrefix(got, "stored:") {
		t.Errorf("значение без схемы: %s, %v", got, err)
	}
}

func TestDamagedKeyFileIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("мусор"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(statetest.Open(t).DB(), "", dir); err == nil {
		t.Error("повреждённый файл ключа принят")
	}
}
