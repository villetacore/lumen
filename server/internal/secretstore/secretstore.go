// Package secretstore keeps tokens that people paste into settings forms
// (ADR-0017). Values are encrypted in the operational database; settings hold
// only a "stored:<id>" reference, so files in the vault stay free of secrets.
package secretstore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/secrets"
	"github.com/villetacore/lumen/server/internal/state"
)

// KeyFile is created in the state directory when no key is configured.
const KeyFile = "secrets.key"

// ErrUnreadable means the stored value cannot be opened with the current key.
var ErrUnreadable = errors.New("сохранённый секрет не расшифровать: ключ сменился, введите значение заново")

// Store is the encrypted table of secrets.
type Store struct {
	db   *state.DB
	aead cipher.AEAD
}

// New opens the store. The key comes from configured (LUMEN_SECRETS_KEY) or from
// a file beside the database: a dump of the database alone opens nothing.
func New(db *state.DB, configured, stateDir string) (*Store, error) {
	key, err := loadKey(configured, stateDir)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, aead: aead}, nil
}

func loadKey(configured, stateDir string) ([]byte, error) {
	if configured != "" {
		sum := sha256.Sum256([]byte(configured))
		return sum[:], nil
	}
	path := filepath.Join(stateDir, KeyFile)
	if data, err := os.ReadFile(path); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("%s повреждён", path)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// O_EXCL: of two starts racing, the loser reads the winner's key next time.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		return nil, err
	}
	return key, f.Sync()
}

// Keep turns what a form received into what settings may hold: a reference stays
// as it is, anything else is encrypted and replaced by a new reference. owner
// names the setting, for people reading the table.
func (s *Store) Keep(ctx context.Context, owner, value string) (string, error) {
	if secrets.IsReference(value) {
		return value, nil
	}
	value = strings.TrimSpace(value)
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	// The id is authenticated: a value copied onto another row does not open.
	sealed := s.aead.Seal(nonce, nonce, []byte(value), []byte(id))
	if _, err := s.db.ExecContext(ctx, `INSERT INTO stored_secrets(id, owner, value, created_at) VALUES (?, ?, ?, ?)`,
		id, owner, sealed, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return "", err
	}
	return secrets.StoredScheme + ":" + id, nil
}

// Forget deletes a stored value. Other kinds of reference point outside and stay.
func (s *Store) Forget(ctx context.Context, ref string) error {
	id, ok := strings.CutPrefix(ref, secrets.StoredScheme+":")
	if !ok {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM stored_secrets WHERE id = ?`, id)
	return err
}

// Provider returns the "stored:" scheme for a secrets.Resolver.
func (s *Store) Provider() secrets.Provider { return provider{s} }

type provider struct{ s *Store }

func (provider) Scheme() string { return secrets.StoredScheme }

func (provider) Describe(secrets.Spec) string { return "сохранённый секрет" }

func (p provider) Resolve(ctx context.Context, spec secrets.Spec, _ *secrets.Resolver) ([]byte, error) {
	var sealed []byte
	err := p.s.db.QueryRowContext(ctx, `SELECT value FROM stored_secrets WHERE id = ?`, spec.Value).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("значение не найдено, введите его заново")
	}
	if err != nil {
		return nil, err
	}
	n := p.s.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrUnreadable
	}
	plain, err := p.s.aead.Open(nil, sealed[:n], sealed[n:], []byte(spec.Value))
	if err != nil {
		return nil, ErrUnreadable
	}
	return plain, nil
}
