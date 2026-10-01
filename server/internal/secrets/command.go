package secrets

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// commandTimeout bounds an external helper; an interactive prompt would
// otherwise hang the server on start.
const commandTimeout = 30 * time.Second

// Runner executes an external command. It is an interface so tests never touch
// a real password manager.
type Runner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands through os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	} else {
		// Nothing to type: make sure a prompting CLI fails fast instead of
		// blocking on a terminal that no longer exists.
		cmd.Stdin = strings.NewReader("")
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: превышено время ожидания (%s)", name, commandTimeout)
		}
		if msg != "" {
			return nil, fmt.Errorf("%s: %s", name, firstLines(msg, 3))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out.Bytes(), nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, "; "))
}

// splitArgs splits a command line on spaces, honouring single and double quotes
// so that paths with spaces survive. It is not a shell: no globbing, no pipes.
func splitArgs(s string) []string {
	var (
		args []string
		cur  strings.Builder
		quot rune
	)
	flush := func() {
		if cur.Len() > 0 {
			args = append(args, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quot != 0:
			if r == quot {
				quot = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quot = r
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return args
}

// commandProvider is the escape hatch for any password manager with a CLI:
// pass, gopass, 1Password (op), age, gpg, a custom script.
type commandProvider struct{}

func (commandProvider) Scheme() string { return "command" }

func (commandProvider) Describe(s Spec) string {
	args := splitArgs(s.Value)
	if len(args) == 0 {
		return "команда"
	}
	// Only the program name goes into logs; arguments may name an item.
	return "команда " + args[0]
}

func (commandProvider) Resolve(ctx context.Context, s Spec, r *Resolver) ([]byte, error) {
	args := splitArgs(s.Value)
	if len(args) == 0 {
		return nil, fmt.Errorf("пустая команда")
	}
	var stdin []byte
	if ref := s.Params.Get("stdin"); ref != "" {
		v, err := r.Resolve(ctx, ref)
		if err != nil {
			return nil, err
		}
		stdin = v
	}
	return r.Runner().Run(ctx, stdin, args[0], args[1:]...)
}

// bitwardenProvider reads an item through the Bitwarden CLI, which also talks
// to a self-hosted Vaultwarden — point it there once with
// `bw config server https://vault.example.com`.
//
//	bitwarden:lumen-vault
//	bitwarden:lumen-vault?field=password&session=env:BW_SESSION
//	bitwarden:e1b0…?field=notes&bw=/opt/bin/bw
type bitwardenProvider struct{}

func (bitwardenProvider) Scheme() string { return "bitwarden" }

func (bitwardenProvider) Describe(s Spec) string {
	return fmt.Sprintf("Bitwarden/Vaultwarden, запись %q", s.Value)
}

func (bitwardenProvider) Resolve(ctx context.Context, s Spec, r *Resolver) ([]byte, error) {
	if strings.TrimSpace(s.Value) == "" {
		return nil, fmt.Errorf("не указана запись (bitwarden:<имя или id>)")
	}
	bin := s.param("bw", "bw")
	field := s.param("field", "password")

	args := []string{"get", field, s.Value, "--nointeraction", "--raw"}

	// bw needs an unlocked session: `export BW_SESSION=$(bw unlock --raw)`.
	sessionRef := s.param("session", "env:BW_SESSION")
	session, err := r.Resolve(ctx, sessionRef)
	if err != nil {
		return nil, fmt.Errorf("нет сессии Bitwarden (%s): %w; выполните `bw unlock --raw`", sessionRef, err)
	}
	args = append(args, "--session", string(session))

	return r.Runner().Run(ctx, nil, bin, args...)
}

// keepassProvider reads an entry from a .kdbx database through keepassxc-cli.
// Shelling out to KeePassXC is deliberate: a hand-written KDBX4 reader would be
// a large amount of crypto to get wrong, and every KeePass user already has the
// tool (see ADR-0007).
//
//	keepass:D:/keys/personal.kdbx#Lumen/vault?password=env:KDBX_PASSWORD
//	keepass:/home/a/db.kdbx#Lumen/vault?key-file=/home/a/db.keyx&attribute=Password
type keepassProvider struct{}

func (keepassProvider) Scheme() string { return "keepass" }

func (keepassProvider) Describe(s Spec) string {
	return fmt.Sprintf("KeePass %s, запись %q", s.Value, s.Fragment)
}

func (keepassProvider) Resolve(ctx context.Context, s Spec, r *Resolver) ([]byte, error) {
	if strings.TrimSpace(s.Value) == "" {
		return nil, fmt.Errorf("не указан файл базы (keepass:<путь.kdbx>#<запись>)")
	}
	if strings.TrimSpace(s.Fragment) == "" {
		return nil, fmt.Errorf("не указана запись: keepass:%s#Папка/Запись", s.Value)
	}
	bin := s.param("cli", "keepassxc-cli")
	attr := s.param("attribute", "Password")

	args := []string{"show", "--quiet", "--show-protected", "--attributes", attr}
	if kf := s.Params.Get("key-file"); kf != "" {
		args = append(args, "--key-file", kf)
	}
	if s.Params.Get("no-password") == "true" {
		args = append(args, "--no-password")
	}
	args = append(args, s.Value, s.Fragment)

	// keepassxc-cli reads the database password from stdin when it is not a TTY.
	var stdin []byte
	if ref := s.param("password", ""); ref != "" {
		v, err := r.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("пароль базы (%s): %w", ref, err)
		}
		stdin = append(v, '\n')
	} else if s.Params.Get("no-password") != "true" {
		return nil, fmt.Errorf(
			"нужен пароль базы: keepass:%s#%s?password=env:KDBX_PASSWORD (или ?no-password=true с key-file)",
			s.Value, s.Fragment)
	}

	return r.Runner().Run(ctx, stdin, bin, args...)
}
