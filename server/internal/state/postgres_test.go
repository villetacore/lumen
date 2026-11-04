package state_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/villetacore/lumen/server/internal/state"
	"github.com/villetacore/lumen/server/internal/state/statetest"
)

func needPostgres(t *testing.T) string {
	t.Helper()
	if os.Getenv(statetest.URLEnv) == "" {
		t.Skip("нужен " + statetest.URLEnv)
	}
	return statetest.NewSchema(t)
}

func TestPostgresMigratesLocksAndSurvivesRestart(t *testing.T) {
	url := needPostgres(t)
	ctx := context.Background()
	st, err := state.OpenPostgres(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version() != state.Latest() {
		t.Fatalf("версия %d, ждём %d", st.Version(), state.Latest())
	}
	id, err := st.InstanceID(ctx)
	if err != nil || len(id) != 32 {
		t.Fatalf("экземпляр %q, %v", id, err)
	}
	if err := st.Backup(ctx, filepath.Join(t.TempDir(), "x.db")); !errors.Is(err, state.ErrBackupByServer) {
		t.Errorf("копия средствами Lumen: %v", err)
	}

	// A second instance on the same schema waits for the lock, then gives up.
	short, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if other, err := state.OpenPostgres(short, url, nil); err == nil {
		other.Close()
		t.Fatal("второй экземпляр запустился")
	}

	if _, err := st.DB().ExecContext(ctx, `INSERT INTO users(id, username, first_seen, last_seen) VALUES (?, ?, ?, ?)`, "s1", "alice", "t", "t"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st, err = state.OpenPostgres(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if again, _ := st.InstanceID(ctx); again != id {
		t.Errorf("экземпляр сменился: %s → %s", id, again)
	}
	var name string
	if err := st.DB().QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, "s1").Scan(&name); err != nil || name != "alice" {
		t.Errorf("после рестарта: %q, %v", name, err)
	}
	info, err := state.InspectPostgres(ctx, url)
	if err != nil || info.Version != state.Latest() || info.Instance != id {
		t.Errorf("inspect: %+v, %v", info, err)
	}
}

// The real path of a migration: rows written through SQLite, read in PostgreSQL.
func TestImportFromSQLite(t *testing.T) {
	url := needPostgres(t)
	ctx := context.Background()
	dir := t.TempDir()
	old, err := state.Open(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := old.DB()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id, username, first_seen, last_seen) VALUES (?, ?, ?, ?)`, []any{"s1", "alice", "t", "t"}},
		{`INSERT INTO user_groups(user_id, group_name) VALUES (?, ?)`, []any{"s1", "dev"}},
		{`INSERT INTO project_members(project, kind, name, role, added_at) VALUES (?, ?, ?, ?, ?)`, []any{"SEC", "user", "alice", "admin", "t"}},
		{`INSERT INTO notifications(recipient, kind, issue, dedup, created_at) VALUES (?, ?, ?, ?, ?)`, []any{"alice", "assigned", "WEB-1", "d1", "t"}},
		{`INSERT INTO issue_snapshots(issue, path, hash, fields, seen) VALUES (?, ?, ?, ?, ?)`, []any{"WEB-1", "p.md", "h", []byte{0, 1, 2}, "t"}},
		{`INSERT INTO issue_events(issue, kind, old, source, at) VALUES (?, ?, ?, ?, ?)`, []any{"WEB-1", "changed", []byte("было"), "lumen", "t"}},
		{`INSERT INTO issue_events(issue, kind, source, at) VALUES (?, ?, ?, ?)`, []any{"WEB-1", "deleted", "lumen", "t"}},
		{`INSERT INTO proposals(id, requester, principal_id, kind, args, preview, state, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"p1", "alice", "s1", "add_comment", []byte("{}"), []byte("x"), "pending", "t", "t"}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	wantID, _ := old.InstanceID(ctx)
	old.Close()

	st, err := state.OpenPostgres(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	counts, err := st.ImportSQLite(ctx, state.DBPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if counts["issue_events"] != 2 || counts["users"] != 1 || counts["proposals"] != 1 {
		t.Errorf("перенесено: %v", counts)
	}
	if id, _ := st.InstanceID(ctx); id != wantID {
		t.Errorf("экземпляр %s, ждём %s", id, wantID)
	}
	var old2 []byte
	var fields []byte
	if err := st.DB().QueryRowContext(ctx, `SELECT old FROM issue_events WHERE id = 1`).Scan(&old2); err != nil || string(old2) != "было" {
		t.Errorf("значение: %q, %v", old2, err)
	}
	if err := st.DB().QueryRowContext(ctx, `SELECT fields FROM issue_snapshots`).Scan(&fields); err != nil || len(fields) != 3 || fields[0] != 0 {
		t.Errorf("снимок: %v, %v", fields, err)
	}
	// New rows continue the numbering instead of colliding with imported ids.
	var next int64
	if err := st.DB().QueryRowContext(ctx, `INSERT INTO issue_events(issue, kind, source, at) VALUES (?, ?, ?, ?) RETURNING id`, "WEB-1", "changed", "lumen", "t").Scan(&next); err != nil || next != 3 {
		t.Errorf("следующий id: %d, %v", next, err)
	}
	// A second import must not mix two histories.
	if _, err := st.ImportSQLite(ctx, state.DBPath(dir)); err == nil {
		t.Error("повторный импорт принят")
	}
}
