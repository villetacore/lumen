package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records what would have been executed and replies from a table.
type fakeRunner struct {
	calls   []call
	replies map[string]string
	fail    map[string]string
}

type call struct {
	name  string
	args  []string
	stdin string
}

func (f *fakeRunner) Run(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: args, stdin: string(stdin)})
	key := name + " " + strings.Join(args, " ")
	if msg, ok := f.fail[key]; ok {
		return nil, errors.New(msg)
	}
	if out, ok := f.replies[key]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("unexpected command: " + key)
}

func TestParse(t *testing.T) {
	cases := []struct {
		in       string
		scheme   string
		value    string
		fragment string
		param    string
	}{
		{"env:LUMEN_PASSPHRASE", "env", "LUMEN_PASSPHRASE", "", ""},
		{"file:/run/secrets/lumen", "file", "/run/secrets/lumen", "", ""},
		{"D:/keys/pass.txt", "file", "D:/keys/pass.txt", "", ""},
		{"bitwarden:lumen?field=notes", "bitwarden", "lumen", "", "notes"},
		{"keepass:D:/db.kdbx#Lumen/vault?attribute=Password", "keepass", "D:/db.kdbx", "Lumen/vault", "Password"},
	}
	for _, c := range cases {
		s, err := Parse(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if s.Scheme != c.scheme || s.Value != c.value || s.Fragment != c.fragment {
			t.Errorf("%s → scheme=%q value=%q fragment=%q", c.in, s.Scheme, s.Value, s.Fragment)
		}
		if c.param != "" {
			got := s.Params.Get("field") + s.Params.Get("attribute")
			if got != c.param {
				t.Errorf("%s → param %q, want %q", c.in, got, c.param)
			}
		}
	}
}

// A bare Windows path must not be read as scheme "d".
func TestParseKeepsWindowsDriveLetters(t *testing.T) {
	s, err := Parse(`keepass:C:/keys/db.kdbx#Lumen`)
	if err != nil {
		t.Fatal(err)
	}
	if s.Value != "C:/keys/db.kdbx" {
		t.Errorf("value = %q", s.Value)
	}
}

func TestEnvAndFile(t *testing.T) {
	r := New(&fakeRunner{})
	ctx := context.Background()

	t.Setenv("LUMEN_TEST_SECRET", "из окружения")
	got, err := r.Resolve(ctx, "env:LUMEN_TEST_SECRET")
	if err != nil || string(got) != "из окружения" {
		t.Fatalf("env: %q %v", got, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "pass.txt")
	// Only the first line counts; the rest may be a comment.
	if err := os.WriteFile(path, []byte("секрет\n# комментарий\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = r.Resolve(ctx, "file:"+path)
	if err != nil || string(got) != "секрет" {
		t.Fatalf("file: %q %v", got, err)
	}

	// A bare path is treated as a file reference.
	got, err = r.Resolve(ctx, path)
	if err != nil || string(got) != "секрет" {
		t.Fatalf("bare path: %q %v", got, err)
	}
}

func TestMissingEnvIsAnError(t *testing.T) {
	r := New(&fakeRunner{})
	if _, err := r.Resolve(context.Background(), "env:DEFINITELY_NOT_SET_12345"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestEmptySecretRejected(t *testing.T) {
	r := New(&fakeRunner{})
	t.Setenv("LUMEN_EMPTY", "")
	if _, err := r.Resolve(context.Background(), "env:LUMEN_EMPTY"); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("want ErrEmptySecret, got %v", err)
	}
}

func TestUnknownScheme(t *testing.T) {
	r := New(&fakeRunner{})
	if _, err := r.Resolve(context.Background(), "sorcery:thing"); !errors.Is(err, ErrUnknownScheme) {
		t.Fatalf("want ErrUnknownScheme, got %v", err)
	}
}

func TestCommandProvider(t *testing.T) {
	runner := &fakeRunner{replies: map[string]string{
		"pass show lumen/vault": "фраза из pass\n",
	}}
	r := New(runner)

	got, err := r.Resolve(context.Background(), "command:pass show lumen/vault")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "фраза из pass" {
		t.Errorf("got %q", got)
	}
}

func TestCommandProviderKeepsQuotedArguments(t *testing.T) {
	runner := &fakeRunner{replies: map[string]string{
		`helper --db C:/Program Files/db.kdbx`: "ok\n",
	}}
	r := New(runner)
	if _, err := r.Resolve(context.Background(), `command:helper --db "C:/Program Files/db.kdbx"`); err != nil {
		t.Fatal(err)
	}
	args := runner.calls[0].args
	if len(args) != 2 || args[1] != "C:/Program Files/db.kdbx" {
		t.Errorf("args = %q", args)
	}
}

func TestBitwardenUsesSession(t *testing.T) {
	t.Setenv("BW_SESSION", "сессия123")
	runner := &fakeRunner{replies: map[string]string{
		"bw get password lumen-vault --nointeraction --raw --session сессия123": "тайна\n",
	}}
	r := New(runner)

	got, err := r.Resolve(context.Background(), "bitwarden:lumen-vault")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "тайна" {
		t.Errorf("got %q", got)
	}
}

func TestBitwardenCustomFieldAndBinary(t *testing.T) {
	t.Setenv("BW_SESSION", "s")
	runner := &fakeRunner{replies: map[string]string{
		"/opt/bin/bw get notes item-id --nointeraction --raw --session s": "заметка\n",
	}}
	r := New(runner)
	if _, err := r.Resolve(context.Background(), "bitwarden:item-id?field=notes&bw=/opt/bin/bw"); err != nil {
		t.Fatal(err)
	}
}

func TestBitwardenWithoutSessionExplains(t *testing.T) {
	os.Unsetenv("BW_SESSION")
	r := New(&fakeRunner{})
	_, err := r.Resolve(context.Background(), "bitwarden:lumen-vault")
	if err == nil || !strings.Contains(err.Error(), "bw unlock") {
		t.Fatalf("error should point at `bw unlock`, got %v", err)
	}
}

func TestKeepassPassesDatabasePasswordOnStdin(t *testing.T) {
	t.Setenv("KDBX_PASSWORD", "пароль базы")
	runner := &fakeRunner{replies: map[string]string{
		"keepassxc-cli show --quiet --show-protected --attributes Password D:/db.kdbx Lumen/vault": "фраза\n",
	}}
	r := New(runner)

	got, err := r.Resolve(context.Background(), "keepass:D:/db.kdbx#Lumen/vault?password=env:KDBX_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "фраза" {
		t.Errorf("got %q", got)
	}
	if runner.calls[0].stdin != "пароль базы\n" {
		t.Errorf("database password must go to stdin, got %q", runner.calls[0].stdin)
	}
}

func TestKeepassKeyFileWithoutPassword(t *testing.T) {
	runner := &fakeRunner{replies: map[string]string{
		"keepassxc-cli show --quiet --show-protected --attributes Password --key-file /k/db.keyx --no-password /k/db.kdbx Lumen": "ф\n",
	}}
	r := New(runner)
	if _, err := r.Resolve(context.Background(),
		"keepass:/k/db.kdbx#Lumen?key-file=/k/db.keyx&no-password=true"); err != nil {
		t.Fatal(err)
	}
	if runner.calls[0].stdin != "" {
		t.Error("key-file mode must not send a password")
	}
}

func TestKeepassRequiresEntry(t *testing.T) {
	r := New(&fakeRunner{})
	_, err := r.Resolve(context.Background(), "keepass:/k/db.kdbx")
	if err == nil || !strings.Contains(err.Error(), "#") {
		t.Fatalf("error should show the expected syntax, got %v", err)
	}
}

func TestNestedReferencesAreBounded(t *testing.T) {
	r := New(&fakeRunner{})
	// stdin refers back to a command that refers to stdin again, and so on.
	_, err := r.Resolve(context.Background(), "command:a?stdin=command:b%3Fstdin%3Dcommand:c%253Fstdin%253Dcommand:d")
	if err == nil {
		t.Fatal("expected the depth limit to trip")
	}
}

func TestDescribeNeverLeaksTheSecret(t *testing.T) {
	r := New(&fakeRunner{})
	for _, ref := range []string{
		"env:LUMEN_PASSPHRASE",
		"bitwarden:lumen-vault",
		"keepass:D:/db.kdbx#Lumen/vault?password=env:KDBX_PASSWORD",
		"command:pass show lumen/vault",
	} {
		got := r.Describe(ref)
		if got == "" {
			t.Errorf("%s: empty description", ref)
		}
		if strings.Contains(got, "env:KDBX_PASSWORD") {
			t.Errorf("%s: description leaks nested reference: %s", ref, got)
		}
	}
	// A command description shows the program, not its arguments.
	if d := r.Describe("command:pass show lumen/vault"); strings.Contains(d, "lumen/vault") {
		t.Errorf("command description should not include arguments: %s", d)
	}
}
