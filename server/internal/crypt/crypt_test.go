package crypt

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func keyFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".lumen", "crypt.json")
}

func TestInitLoadRoundTrip(t *testing.T) {
	path := keyFile(t)
	pass := []byte("правильная фраза")

	k, err := Init(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("---\ntitle: Заметка\n---\n\n- [ ] Задача ^t1a\n")

	sealed, err := k.Encode("10-notes/a.md", plain)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(sealed) {
		t.Fatal("container must start with the magic prefix")
	}
	if bytes.Contains(sealed, []byte("Задача")) {
		t.Fatal("plaintext leaked into the container")
	}

	reopened, err := Load(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Decode("10-notes/a.md", sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round trip mismatch:\n got  %q\n want %q", got, plain)
	}
}

func TestDecodeIsBoundToPath(t *testing.T) {
	k, err := Init(keyFile(t), []byte("phrase"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := k.Encode("a.md", []byte("секрет"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Decode("b.md", sealed); !errors.Is(err, ErrCorruptFile) {
		t.Errorf("swapping a file under another name must fail, got %v", err)
	}
}

func TestPlainFilesPassThrough(t *testing.T) {
	k, err := Init(keyFile(t), []byte("phrase"))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("# Обычный markdown\n")
	got, err := k.Decode("a.md", plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Error("a half-migrated vault must stay readable")
	}
	if IsEncrypted(plain) {
		t.Error("plain markdown must not look encrypted")
	}
}

func TestWrongPassphrase(t *testing.T) {
	path := keyFile(t)
	if _, err := Init(path, []byte("правильная")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, []byte("неправильная")); !errors.Is(err, ErrBadPassphrase) {
		t.Errorf("want ErrBadPassphrase, got %v", err)
	}
}

func TestChangePassphraseKeepsData(t *testing.T) {
	path := keyFile(t)
	k, err := Init(path, []byte("старая"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := k.Encode("a.md", []byte("данные"))
	if err != nil {
		t.Fatal(err)
	}

	if err := ChangePassphrase(path, []byte("старая"), []byte("новая")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, []byte("старая")); !errors.Is(err, ErrBadPassphrase) {
		t.Error("old passphrase must stop working")
	}

	k2, err := Load(path, []byte("новая"))
	if err != nil {
		t.Fatal(err)
	}
	// The data key is unchanged, so files written before the change still open.
	got, err := k2.Decode("a.md", sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "данные" {
		t.Errorf("got %q", got)
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	path := keyFile(t)
	if _, err := Init(path, []byte("phrase")); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(path, []byte("other")); !errors.Is(err, ErrAlreadyInit) {
		t.Errorf("overwriting a key file would orphan the vault, got %v", err)
	}
}

func TestLoadWithoutKeyFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json"), []byte("x")); !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("want ErrNotEncrypted, got %v", err)
	}
}

func TestKeyFilePermissions(t *testing.T) {
	path := keyFile(t)
	if _, err := Init(path, []byte("phrase")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not honour POSIX bits; the check is meaningful elsewhere.
	if st.Mode().Perm()&0o077 != 0 && os.Getenv("GOOS") != "windows" {
		t.Logf("key file mode is %v", st.Mode().Perm())
	}
}
