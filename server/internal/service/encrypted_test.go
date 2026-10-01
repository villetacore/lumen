package service

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/crypt"
	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

// newEncryptedVault builds a vault, encrypts it in place and returns a wired-up
// service stack reading through the codec.
func newEncryptedVault(t *testing.T, files map[string]string) (*Tasks, *vault.Vault, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	keyring, err := crypt.Init(filepath.Join(dir, filepath.FromSlash(crypt.KeyFileName)), []byte("фраза"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncryptVault(v, keyring, log); err != nil {
		t.Fatal(err)
	}
	v.SetCodec(keyring)

	store := index.NewMemory(v.Root)
	bus := NewBus()
	ix := NewIndexer(v, store, bus, log)
	if _, err := ix.ReindexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewTasks(v, store, ix, bus, log), v, dir
}

func TestEncryptedVaultIsIndexedAndWritable(t *testing.T) {
	const src = "---\ntitle: День\n---\n\n# День\n\n- [ ] Секретная задача ^t1a\n"
	tasks, v, dir := newEncryptedVault(t, map[string]string{"day.md": src})
	ctx := context.Background()

	onDisk, err := os.ReadFile(filepath.Join(dir, "day.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !crypt.IsEncrypted(onDisk) {
		t.Fatal("file on disk must be a container")
	}
	if bytes.Contains(onDisk, []byte("Секретная")) {
		t.Fatal("plaintext leaked to disk")
	}

	// The index sees plaintext even though the disk does not.
	task, err := tasks.Get(ctx, "t1a")
	if err != nil {
		t.Fatal(err)
	}
	if task.Text != "Секретная задача" {
		t.Fatalf("text = %q", task.Text)
	}

	done := domain.StatusDone
	if _, err := tasks.Update(ctx, "t1a", TaskUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(filepath.Join(dir, "day.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !crypt.IsEncrypted(after) {
		t.Error("a write must not drop the file back to plaintext")
	}

	plain, err := v.Read("day.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "- [x] Секретная задача [done:: " + domain.Today().String() + "] ^t1a"
	if !bytes.Contains(plain, []byte(want)) {
		t.Errorf("decrypted content missing the edit:\n%s", plain)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const src = "# Заметка\n\n- [ ] Задача ^t1a\n"
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	keyring, err := crypt.Init(filepath.Join(dir, filepath.FromSlash(crypt.KeyFileName)), []byte("фраза"))
	if err != nil {
		t.Fatal(err)
	}

	res, err := EncryptVault(v, keyring, log)
	if err != nil || res.Converted != 1 {
		t.Fatalf("encrypt: converted=%d err=%v", res.Converted, err)
	}
	// Re-running must be a no-op, so an interrupted migration can be repeated.
	again, err := EncryptVault(v, keyring, log)
	if err != nil || again.Converted != 0 || again.Skipped != 1 {
		t.Fatalf("re-encrypt: %+v err=%v", again, err)
	}

	back, err := DecryptVault(v, keyring, log)
	if err != nil || back.Converted != 1 {
		t.Fatalf("decrypt: converted=%d err=%v", back.Converted, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != src {
		t.Errorf("round trip changed the file:\n got  %q\n want %q", got, src)
	}
}
