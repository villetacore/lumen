package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alexpyslar/lumen/server/internal/secrets"
	"golang.org/x/term"
)

// errNoPassphrase is returned when no source of a passphrase is available and
// the process has no terminal to ask on.
var errNoPassphrase = errors.New(
	"нет парольной фразы: укажите --passphrase-from (env:, file:, command:, bitwarden:, keepass:), " +
		"задайте LUMEN_PASSPHRASE или запустите в терминале")

// passphraseSource describes every place a passphrase may come from.
type passphraseSource struct {
	Ref    string // --passphrase-from: a secrets reference
	File   string // --passphrase-file: the older, file-only form
	EnvKey string // usually LUMEN_PASSPHRASE
}

// readPassphrase resolves a passphrase from, in order: an explicit reference
// (which may reach into a password manager), a file, the environment, a pipe,
// and only then an interactive prompt. Non-interactive sources come first so a
// service or container never blocks waiting for input.
func readPassphrase(src passphraseSource, prompt string, confirm bool) ([]byte, error) {
	ctx := context.Background()

	if src.Ref != "" {
		resolver := secrets.New(nil)
		value, err := resolver.Resolve(ctx, src.Ref)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
	if src.File != "" {
		data, err := os.ReadFile(src.File)
		if err != nil {
			return nil, fmt.Errorf("read passphrase file: %w", err)
		}
		return firstLine(data), nil
	}
	if src.EnvKey != "" {
		if v, ok := os.LookupEnv(src.EnvKey); ok && v != "" {
			return []byte(v), nil
		}
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Still allow piping: `echo secret | lumend crypt init`.
		if stat, err := os.Stdin.Stat(); err == nil && stat.Mode()&os.ModeCharDevice == 0 {
			line, err := bufio.NewReader(os.Stdin).ReadBytes('\n')
			if len(line) > 0 {
				return firstLine(line), nil
			}
			if err != nil {
				return nil, errNoPassphrase
			}
		}
		return nil, errNoPassphrase
	}

	fmt.Fprint(os.Stderr, prompt)
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(first)) == 0 {
		return nil, errors.New("парольная фраза не может быть пустой")
	}
	if !confirm {
		return first, nil
	}

	fmt.Fprint(os.Stderr, "Повторите: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(first, second) {
		return nil, errors.New("парольные фразы не совпадают")
	}
	return first, nil
}

func firstLine(data []byte) []byte {
	s := string(data)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return []byte(strings.TrimSpace(s))
}
