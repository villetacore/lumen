package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

// lockWait covers a rolling restart: the previous lumend is still closing.
const lockWait = 30 * time.Second

// ErrBackupByServer means this store's copies are made by the database server.
var ErrBackupByServer = errors.New("база в PostgreSQL: копии делаются средствами сервера (pg_dump)")

// OpenPostgres connects to PostgreSQL (ADR-0015), takes the instance lock and
// applies pending migrations. Copies before migrating are the server's job: a
// failed migration rolls back on its own.
func OpenPostgres(ctx context.Context, url string, codec Codec) (*Store, error) {
	pool, err := openPostgres(ctx, url)
	if err != nil {
		return nil, err
	}
	// One connection gives the same guarantee as SQLite: requests never overlap
	// inside a read-modify-write of the journal or the people directory.
	pool.SetMaxOpenConns(1)

	lockCtx, cancel := context.WithCancel(context.Background())
	unlockDB, err := holdInstanceLock(ctx, lockCtx, url)
	if err != nil {
		cancel()
		pool.Close()
		return nil, err
	}
	s := &Store{
		db: &DB{sql: pool, postgres: true}, codec: codec,
		unlock: func() { cancel(); unlockDB.Close() },
	}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func openPostgres(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, redactURL(err, url)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("подключение к PostgreSQL: %w", redactURL(err, url))
	}
	return db, nil
}

// redactURL keeps the password out of logs: driver errors may quote the address.
func redactURL(err error, url string) error {
	if err == nil || !strings.Contains(err.Error(), url) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), url, "<database-url>"))
}

// holdInstanceLock is the counterpart of the directory lock: two lumend over
// one schema would overwrite each other's vault. The advisory lock lives as
// long as its connection, so it gets a connection of its own, watched by a
// loop that takes the lock again if the server restarted.
func holdInstanceLock(ctx, life context.Context, url string) (*sql.DB, error) {
	db, err := openPostgres(ctx, url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := acquireLock(ctx, db)
	if err != nil {
		db.Close()
		return nil, err
	}
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-life.Done():
				conn.Close()
				return
			case <-tick.C:
			}
			if err := conn.PingContext(life); err == nil || life.Err() != nil {
				continue
			}
			slog.Warn("связь с PostgreSQL потеряна, блокировка экземпляра будет взята заново")
			conn.Close()
			next, err := acquireLock(life, db)
			for err != nil && life.Err() == nil {
				slog.Error("блокировка экземпляра не взята", "err", err)
				time.Sleep(10 * time.Second)
				next, err = acquireLock(life, db)
			}
			if err != nil {
				return
			}
			conn = next
		}
	}()
	return db, nil
}

func acquireLock(ctx context.Context, db *sql.DB) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	for {
		var got bool
		// Per schema: one server can hold several installations.
		err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(7236001, hashtext(current_schema()))`).Scan(&got)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if got {
			return conn, nil
		}
		if time.Now().After(deadline) {
			conn.Close()
			return nil, ErrLocked
		}
		select {
		case <-ctx.Done():
			conn.Close()
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// InspectPostgres reads the schema version and instance id without taking the
// lock, so it works next to a running server.
func InspectPostgres(ctx context.Context, url string) (Info, error) {
	info := Info{Path: "PostgreSQL", Latest: Latest()}
	db, err := openPostgres(ctx, url)
	if err != nil {
		return info, err
	}
	defer db.Close()
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return info, fmt.Errorf("схема Lumen ещё не создана: %w", err)
	}
	info.Version = int(v.Int64)
	_ = db.QueryRowContext(ctx, `SELECT id FROM instance LIMIT 1`).Scan(&info.Instance)
	return info, nil
}

// importTables lists what moves, parents first, with the columns that are BYTEA
// in PostgreSQL. The sealed values move as they are: their refs hold row ids,
// and the ids are preserved.
var importTables = []struct {
	name  string
	blobs []string
	seq   bool
}{
	{name: "project_members"},
	{name: "users"},
	{name: "user_groups"},
	{name: "notifications", seq: true},
	{name: "issue_snapshots", blobs: []string{"fields"}},
	{name: "issue_events", blobs: []string{"old", "new"}, seq: true},
	{name: "proposals", blobs: []string{"args", "preview"}},
}

// ImportSQLite copies an existing SQLite database into an empty PostgreSQL one,
// in one transaction, keeping the instance id. It reports rows per table.
func (s *Store) ImportSQLite(ctx context.Context, path string) (map[string]int, error) {
	if !s.db.postgres {
		return nil, errors.New("импорт выполняется в PostgreSQL")
	}
	info, err := Inspect(ctx, path)
	if err != nil {
		return nil, err
	}
	if info.Version != s.version {
		return nil, fmt.Errorf("версия схемы копии %d, этот lumend знает %d: сначала запустите его на этой копии, чтобы применить миграции",
			info.Version, s.version)
	}
	src, err := openDB(path, true)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	counts := map[string]int{}
	for _, t := range importTables {
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t.name).Scan(&existing); err != nil {
			return nil, err
		}
		if existing > 0 {
			return nil, fmt.Errorf("таблица %s уже не пуста: импорт выполняется только в чистую базу", t.name)
		}
		n, err := copyTable(ctx, src, tx, t.name, t.blobs)
		if err != nil {
			return nil, fmt.Errorf("таблица %s: %w", t.name, err)
		}
		counts[t.name] = n
		if t.seq && n > 0 {
			if _, err := tx.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('`+t.name+`', 'id'), (SELECT MAX(id) FROM `+t.name+`))`); err != nil {
				return nil, err
			}
		}
	}
	var id, created string
	if err := src.QueryRowContext(ctx, `SELECT id, created_at FROM instance LIMIT 1`).Scan(&id, &created); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE instance SET id = ?, created_at = ?`, id, created); err != nil {
		return nil, err
	}
	return counts, tx.Commit()
}

func copyTable(ctx context.Context, src *sql.DB, tx *Tx, table string, blobs []string) (int, error) {
	rows, err := src.QueryContext(ctx, `SELECT * FROM `+table)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	isBlob := make([]bool, len(cols))
	for i, c := range cols {
		for _, b := range blobs {
			isBlob[i] = isBlob[i] || c == b
		}
	}
	insert := `INSERT INTO ` + table + `(` + strings.Join(cols, ", ") + `) VALUES (` +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + `)`
	values, ptrs := make([]any, len(cols)), make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	n := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		for i, v := range values {
			// SQLite keeps whatever was written; a string must reach BYTEA as bytes.
			if text, ok := v.(string); ok && isBlob[i] {
				values[i] = []byte(text)
			}
		}
		if _, err := tx.ExecContext(ctx, insert, values...); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}
