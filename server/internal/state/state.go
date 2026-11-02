// Package state is Lumen's operational store (ADR-0011): data that cannot be
// rebuilt from the vault — users, membership, history, jobs. It lives in its
// own directory outside the vault and has its own backup.
package state

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver; pure Go, no cgo
)

const (
	dbFile     = "lumen.db"
	lockFile   = "lumen.lock"
	backupsDir = "backups"
)

var (
	// ErrLocked means another process holds the state directory.
	ErrLocked = errors.New("state directory is in use by another lumend")
	// ErrTooNew means the database was migrated by a newer lumend (ADR-0011 §5).
	ErrTooNew = errors.New("state database is newer than this lumend")
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationSource is the set of migrations; tests swap in their own.
var migrationSource fs.FS = mustSub(migrationFS, "migrations")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// Codec seals content-bearing values when the vault is encrypted (ADR-0011 §7).
// vault.Codec satisfies it.
type Codec interface {
	Encode(rel string, plain []byte) ([]byte, error)
	Decode(rel string, stored []byte) ([]byte, error)
}

// Store is an open operational database.
type Store struct {
	Dir     string
	db      *sql.DB
	codec   Codec
	unlock  func()
	version int
	backup  string // pre-migration copy made by this Open, if any
}

type migration struct {
	version int
	name    string
	sql     string
}

func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationSource, ".")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		num, name, ok := strings.Cut(strings.TrimSuffix(e.Name(), ".sql"), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("bad migration file name %q", e.Name())
		}
		body, err := fs.ReadFile(migrationSource, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migrations must be numbered 1..n without gaps, got %d at %d", m.version, i+1)
		}
	}
	return out, nil
}

// Latest is the schema version this binary knows.
func Latest() int {
	ms, err := migrations()
	if err != nil {
		return 0
	}
	return len(ms)
}

// Open locks dir, opens (or creates) the database and applies pending
// migrations. A non-empty database is copied to backups/ before migrating.
func Open(ctx context.Context, dir string, codec Codec) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, backupsDir), 0o700); err != nil {
		return nil, err
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	db, err := openDB(filepath.Join(dir, dbFile), false)
	if err != nil {
		unlock()
		return nil, err
	}
	s := &Store{Dir: dir, db: db, codec: codec, unlock: unlock}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func openDB(path string, readOnly bool) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if readOnly {
		dsn += "&mode=ro"
	} else {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps writes serial and makes transactions simple; the
	// pilot is one process with modest load (ADR-0011 §3).
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return 0, err
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

func (s *Store) migrate(ctx context.Context) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	cur, err := schemaVersion(ctx, s.db)
	if err != nil {
		return err
	}
	if cur > len(ms) {
		return fmt.Errorf("%w: schema %d, this binary knows %d — run the newer lumend or restore a backup", ErrTooNew, cur, len(ms))
	}
	if cur > 0 && cur < len(ms) {
		name := fmt.Sprintf("pre-%04d-%s.db", ms[cur].version, time.Now().UTC().Format("20060102T150405Z"))
		s.backup = filepath.Join(s.Dir, backupsDir, name)
		if err := s.Backup(ctx, s.backup); err != nil {
			return fmt.Errorf("backup before migration: %w", err)
		}
	}
	for _, m := range ms[cur:] {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	s.version = len(ms)
	return nil
}

// DB is the database handle for feature packages.
func (s *Store) DB() *sql.DB { return s.db }

// Version is the applied schema version.
func (s *Store) Version() int { return s.version }

// MigrationBackup is the copy made before this Open migrated, or "".
func (s *Store) MigrationBackup() string { return s.backup }

// InstanceID names this database; it survives backup and restore.
func (s *Store) InstanceID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM instance LIMIT 1`).Scan(&id)
	return id, err
}

// Ping is the readiness check.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close releases the database and the directory lock.
func (s *Store) Close() error {
	err := s.db.Close()
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
	return err
}

// Seal prepares a content-bearing value for storage. ref names the value
// ("issue_events:42:old") and is bound to it by the codec.
func (s *Store) Seal(ref string, plain []byte) ([]byte, error) {
	if s.codec == nil {
		return plain, nil
	}
	return s.codec.Encode("state/"+ref, plain)
}

// Unseal reverses Seal.
func (s *Store) Unseal(ref string, stored []byte) ([]byte, error) {
	if s.codec == nil {
		return stored, nil
	}
	return s.codec.Decode("state/"+ref, stored)
}

// Backup writes a consistent copy of the live database to dst, which must
// not exist yet.
func (s *Store) Backup(ctx context.Context, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dst)
	return err
}

// BackupDir copies the database in dir to dst through a read-only
// connection, so it works while lumend is running (WAL readers do not block
// the writer).
func BackupDir(ctx context.Context, dir, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	live := filepath.Join(dir, dbFile)
	if _, err := os.Stat(live); err != nil {
		return err
	}
	db, err := openDB(live, true)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO ?`, dst)
	return err
}

// DBPath is the database file inside a state directory.
func DBPath(dir string) string { return filepath.Join(dir, dbFile) }

// Info describes a database file without opening it for writing.
type Info struct {
	Path     string
	Version  int
	Latest   int
	Instance string
	Replaced string // restore only: where the previous database was kept
}

// Inspect checks a database file: integrity, schema version and instance id.
func Inspect(ctx context.Context, path string) (Info, error) {
	info := Info{Path: path, Latest: Latest()}
	if _, err := os.Stat(path); err != nil {
		return info, err
	}
	db, err := openDB(path, true)
	if err != nil {
		return info, err
	}
	defer db.Close()
	var check string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&check); err != nil {
		return info, err
	}
	if check != "ok" {
		return info, fmt.Errorf("integrity check failed: %s", check)
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return info, fmt.Errorf("not a Lumen state database: %w", err)
	}
	info.Version = int(v.Int64)
	_ = db.QueryRowContext(ctx, `SELECT id FROM instance LIMIT 1`).Scan(&info.Instance)
	return info, nil
}

// Restore replaces the database in dir with src. The server must be stopped:
// Restore takes the same lock. The current database is kept in backups/.
func Restore(ctx context.Context, dir, src string) (Info, error) {
	info, err := Inspect(ctx, src)
	if err != nil {
		return info, err
	}
	if info.Version > info.Latest {
		return info, fmt.Errorf("%w: backup has schema %d, this binary knows %d", ErrTooNew, info.Version, info.Latest)
	}
	if err := os.MkdirAll(filepath.Join(dir, backupsDir), 0o700); err != nil {
		return info, err
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return info, err
	}
	defer unlock()

	live := filepath.Join(dir, dbFile)
	if _, err := os.Stat(live); err == nil {
		keep := filepath.Join(dir, backupsDir, "pre-restore-"+time.Now().UTC().Format("20060102T150405Z")+".db")
		// Checkpoint the WAL into the main file so the kept copy is complete.
		if db, err := openDB(live, false); err == nil {
			_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
			db.Close()
		}
		if err := os.Rename(live, keep); err != nil {
			return info, err
		}
		info.Replaced = keep
		for _, suffix := range []string{"-wal", "-shm"} {
			_ = os.Remove(live + suffix)
		}
	}
	tmp := live + ".restore"
	if err := copyFile(src, tmp); err != nil {
		return info, err
	}
	if err := os.Rename(tmp, live); err != nil {
		return info, err
	}
	info.Path = live
	return info, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func lockPath(dir string) string { return filepath.Join(dir, lockFile) }
