// Package statetest opens an operational store for tests. With
// LUMEN_TEST_DATABASE_URL set, every test gets its own PostgreSQL schema, so the
// same tests cover both engines; without it they use SQLite in a temp directory.
package statetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/villetacore/lumen/server/internal/state"
)

// URLEnv names the variable with the address of a throwaway PostgreSQL.
const URLEnv = "LUMEN_TEST_DATABASE_URL"

// Opener returns a function that opens the test's store. Calling it again after
// Close is a restart over the same data.
func Opener(t *testing.T) func() *state.Store {
	t.Helper()
	ctx := context.Background()
	base := os.Getenv(URLEnv)
	var open func() (*state.Store, error)
	if base == "" {
		dir := t.TempDir()
		open = func() (*state.Store, error) { return state.Open(ctx, dir, nil) }
	} else {
		schemaURL := NewSchema(t)
		open = func() (*state.Store, error) { return state.OpenPostgres(ctx, schemaURL, nil) }
	}
	return func() *state.Store {
		t.Helper()
		st, err := open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		return st
	}
}

// Open opens a fresh store for the test.
func Open(t *testing.T) *state.Store {
	t.Helper()
	return Opener(t)()
}

// NewSchema creates an empty schema in the test PostgreSQL, drops it after the
// test and returns a URL pointing at it.
func NewSchema(t *testing.T) string {
	t.Helper()
	base := os.Getenv(URLEnv)
	var b [8]byte
	_, _ = rand.Read(b[:])
	schema := "t_" + hex.EncodeToString(b[:])
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		admin.Close()
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
