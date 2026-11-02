package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStateDirDefaultsNextToVault(t *testing.T) {
	root := t.TempDir()
	c, err := Load([]string{"--vault", filepath.Join(root, "vault")})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "lumen-state"); c.StateDir != want {
		t.Fatalf("state = %q, want %q", c.StateDir, want)
	}
}

func TestStateDirInsideVaultIsRefused(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	for _, dir := range []string{vault, filepath.Join(vault, ".lumen", "state")} {
		if _, err := Load([]string{"--vault", vault, "--state", dir}); err == nil || !strings.Contains(err.Error(), "outside the vault") {
			t.Errorf("--state %s: %v", dir, err)
		}
	}
	// A sibling whose name merely starts with the vault's name is fine.
	if _, err := Load([]string{"--vault", vault, "--state", vault + "-state"}); err != nil {
		t.Errorf("sibling state dir: %v", err)
	}
}
