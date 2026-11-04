package state

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenMigratesAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	if s.Version() != Latest() || s.Version() < 1 {
		t.Fatalf("version = %d, latest = %d", s.Version(), Latest())
	}
	id, err := s.InstanceID(ctx)
	if err != nil || len(id) != 32 {
		t.Fatalf("instance = %q, %v", id, err)
	}
	if _, err := s.DB().ExecContext(ctx, `CREATE TABLE probe(v TEXT); INSERT INTO probe VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	if s.MigrationBackup() != "" {
		t.Errorf("a fresh database needs no pre-migration copy")
	}
	s.Close()

	s = open(t, dir)
	defer s.Close()
	again, _ := s.InstanceID(ctx)
	var v string
	if err := s.DB().QueryRowContext(ctx, `SELECT v FROM probe`).Scan(&v); err != nil || v != "kept" || again != id {
		t.Fatalf("after restart: probe=%q id=%q err=%v", v, again, err)
	}
}

func TestSecondProcessIsRefused(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	defer s.Close()
	if _, err := Open(context.Background(), dir, nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("second open: %v", err)
	}
	if _, err := Restore(context.Background(), dir, filepath.Join(dir, dbFile)); !errors.Is(err, ErrLocked) {
		t.Fatalf("restore while running: %v", err)
	}
}

// A database migrated by a newer binary must not be touched by an older one.
func TestNewerSchemaIsRefused(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO schema_migrations VALUES (999, 'future', 'x')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, dir, nil); !errors.Is(err, ErrTooNew) {
		t.Fatalf("open newer schema: %v", err)
	}
}

// An older database gets a copy in backups/ before new migrations run, and a
// failing migration leaves the schema at the last good version.
func TestMigrationUpgradeBackupAndFailure(t *testing.T) {
	ctx := context.Background()
	orig := migrationSource
	t.Cleanup(func() { migrationSource = orig })
	base, _ := fs.ReadFile(orig, "0001_instance.sql")
	set := fstest.MapFS{"0001_instance.sql": {Data: base}}
	migrationSource = set

	dir := t.TempDir()
	s := open(t, dir)
	s.Close()

	set["0002_notes.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE notes(v TEXT); INSERT INTO notes VALUES ('x');`)}
	s = open(t, dir)
	if s.Version() != 2 || s.MigrationBackup() == "" {
		t.Fatalf("version %d, backup %q", s.Version(), s.MigrationBackup())
	}
	if info, err := Inspect(ctx, s.MigrationBackup()); err != nil || info.Version != 1 {
		t.Fatalf("pre-migration copy: %+v %v", info, err)
	}
	s.Close()

	set["0003_broken.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE half(v TEXT); INSERT INTO nowhere VALUES (1);`)}
	if _, err := Open(ctx, dir, nil); err == nil || !strings.Contains(err.Error(), "0003_broken") {
		t.Fatalf("broken migration: %v", err)
	}
	delete(set, "0003_broken.sql")
	s = open(t, dir)
	defer s.Close()
	var n int
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'half'`).Scan(&n)
	if s.Version() != 2 || n != 0 {
		t.Fatalf("failed migration left traces: version %d, half=%d", s.Version(), n)
	}

	set["0005_gap.sql"] = &fstest.MapFile{Data: []byte(`SELECT 1;`)}
	if Latest() != 0 {
		t.Fatal("a numbering gap must be rejected")
	}
}

func TestBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	id, _ := s.InstanceID(ctx)
	if _, err := s.DB().ExecContext(ctx, `CREATE TABLE probe(v TEXT); INSERT INTO probe VALUES ('before')`); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := s.Backup(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, snap); err == nil {
		t.Fatal("backup must not overwrite an existing file")
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE probe SET v = 'after'`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	info, err := Restore(ctx, dir, snap)
	if err != nil {
		t.Fatal(err)
	}
	if info.Instance != id || info.Version != Latest() {
		t.Fatalf("restored info = %+v", info)
	}
	kept, _ := filepath.Glob(filepath.Join(dir, backupsDir, "pre-restore-*.db"))
	if len(kept) != 1 || info.Replaced != kept[0] {
		t.Fatalf("the replaced database must be kept, got %v", kept)
	}

	s = open(t, dir)
	defer s.Close()
	var v string
	if err := s.DB().QueryRowContext(ctx, `SELECT v FROM probe`).Scan(&v); err != nil || v != "before" {
		t.Fatalf("restored probe = %q, %v", v, err)
	}
	// The kept copy holds the state just before the restore.
	old, err := Inspect(ctx, kept[0])
	if err != nil || old.Instance != id {
		t.Fatalf("kept copy: %+v %v", old, err)
	}
}

func TestRestoreRejectsBadFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	junk := filepath.Join(t.TempDir(), "junk.db")
	os.WriteFile(junk, []byte("not a database at all, just text that is long enough"), 0o600)
	if _, err := Restore(ctx, dir, junk); err == nil {
		t.Fatal("junk must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, dbFile)); err == nil {
		t.Fatal("a refused restore must not create a database")
	}
}

type xorCodec struct{}

func (xorCodec) Encode(rel string, p []byte) ([]byte, error) {
	return append([]byte(rel+"|"), xor(p)...), nil
}
func (xorCodec) Decode(rel string, s []byte) ([]byte, error) {
	if !bytes.HasPrefix(s, []byte(rel+"|")) {
		return nil, errors.New("wrong ref")
	}
	return xor(s[len(rel)+1:]), nil
}
func xor(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[i] ^ 0x5a
	}
	return out
}

func TestSealUsesCodecAndBindsRef(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(context.Background(), dir, xorCodec{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sealed, _ := s.Seal("issue_events:1:old", []byte("секрет"))
	if strings.Contains(string(sealed), "секрет") {
		t.Fatal("value stored in the clear")
	}
	if plain, err := s.Unseal("issue_events:1:old", sealed); err != nil || string(plain) != "секрет" {
		t.Fatalf("unseal = %q, %v", plain, err)
	}
	if _, err := s.Unseal("issue_events:2:old", sealed); err == nil {
		t.Fatal("a value must not open under another ref")
	}
	plain := &Store{}
	if v, _ := plain.Seal("x", []byte("открыто")); string(v) != "открыто" {
		t.Fatal("without a codec values are stored as is")
	}
}

func TestBackupDirWhileRunning(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := open(t, dir)
	defer s.Close()
	if _, err := s.DB().ExecContext(ctx, `CREATE TABLE probe(v TEXT); INSERT INTO probe VALUES ('live')`); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "online.db")
	if err := BackupDir(ctx, dir, dst); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(ctx, dst)
	if err != nil || info.Version != Latest() {
		t.Fatalf("online backup: %+v %v", info, err)
	}
	// The server keeps writing after the backup.
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO probe VALUES ('later')`); err != nil {
		t.Fatal(err)
	}
}

// Both engines must describe the same schema history (ADR-0015).
func TestEnginesHaveTheSameMigrations(t *testing.T) {
	sqlite, err := migrations(migrationSource)
	if err != nil {
		t.Fatal(err)
	}
	postgres, err := migrations(postgresMigration)
	if err != nil {
		t.Fatal(err)
	}
	if len(sqlite) != len(postgres) {
		t.Fatalf("SQLite %d, PostgreSQL %d", len(sqlite), len(postgres))
	}
	for i := range sqlite {
		if sqlite[i].name != postgres[i].name {
			t.Errorf("%d: %s и %s", sqlite[i].version, sqlite[i].name, postgres[i].name)
		}
	}
}

func TestRebindLeavesLiteralsAlone(t *testing.T) {
	got := rebind(`SELECT '?', x FROM t WHERE a = ? AND b = 'да?' AND c = ?`)
	want := `SELECT '?', x FROM t WHERE a = $1 AND b = 'да?' AND c = $2`
	if got != want {
		t.Errorf("%s", got)
	}
}
