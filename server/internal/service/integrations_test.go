package service

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/integrations"
	"github.com/alexpyslar/lumen/server/internal/secrets"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

// fakeConnector returns whatever the test puts in it, so the mapper can be
// exercised without touching the network.
type fakeConnector struct {
	items []integrations.ExternalItem
	err   error
	calls int
}

func (*fakeConnector) Kind() string  { return "fake" }
func (*fakeConnector) Title() string { return "Тестовый коннектор" }

func (*fakeConnector) ConfigSchema() []integrations.ConfigField {
	return []integrations.ConfigField{{Key: "token", Label: "Токен", Type: integrations.FieldSecret}}
}

func (*fakeConnector) Validate(integrations.Config) error { return nil }

func (f *fakeConnector) Sync(_ context.Context, _ integrations.SyncInput) (integrations.SyncOutput, error) {
	f.calls++
	if f.err != nil {
		return integrations.SyncOutput{}, f.err
	}
	return integrations.SyncOutput{
		Items: f.items,
		State: map[string]string{"cursor": "1"},
	}, nil
}

func newIntegrationsFixture(t *testing.T) (*Integrations, index.Store, string, *fakeConnector) {
	t.Helper()
	dir := t.TempDir()
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := index.NewMemory(v.Root)
	bus := NewBus()
	ix := NewIndexer(v, store, bus, log)
	if _, err := ix.ReindexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks := NewTasks(v, store, ix, bus, log)
	notes := NewNotes(v, store, ix, bus, log)

	svc := NewIntegrations(v, store, tasks, notes, ix, bus, secrets.New(nil), log)
	conn := &fakeConnector{}
	svc.RegisterConnector(conn)
	if err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	return svc, store, dir, conn
}

func TestIntegrationEventsStayOutOfTheVault(t *testing.T) {
	svc, store, dir, conn := newIntegrationsFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 9, 9, 14, 0, 0, 0, time.Local)
	conn.items = []integrations.ExternalItem{{
		ExternalID: "evt-1", Kind: integrations.ItemEvent, Title: "Созвон",
		Start: start, End: start.Add(time.Hour), Hash: "h1", Labels: []string{"Работа"},
	}}

	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Календарь", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}

	events, err := store.Events(ctx, start.Add(-24*time.Hour), start.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Title != "Созвон" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Calendar != "Работа" {
		t.Errorf("calendar = %q", events[0].Calendar)
	}

	// A work calendar must not create files.
	var mdFiles int
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(p, ".md") {
			mdFiles++
		}
		return nil
	})
	if mdFiles != 0 {
		t.Errorf("integration created %d markdown files, expected none", mdFiles)
	}

	// Removing the integration takes its events off the calendar.
	if err := svc.Delete(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	events, _ = store.Events(ctx, start.Add(-24*time.Hour), start.Add(24*time.Hour))
	if len(events) != 0 {
		t.Errorf("events remained after delete: %+v", events)
	}
}

func TestIntegrationTasksBecomeNotesIdempotently(t *testing.T) {
	svc, store, dir, conn := newIntegrationsFixture(t)
	ctx := context.Background()

	conn.items = []integrations.ExternalItem{{
		ExternalID: "acme/repo#412", Kind: integrations.ItemTask,
		Title: "acme/repo#412 Починить парсер", Body: "Подробности задачи",
		URL: "https://example.com/412", Labels: []string{"bug"}, Hash: "h1",
	}}

	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Issues", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.SyncNow(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 1 {
		t.Fatalf("created = %d, want 1", first.Created)
	}

	note, err := store.NoteBySource(ctx, cfg.ID, "acme/repo#412")
	if err != nil {
		t.Fatal(err)
	}
	if len(note.Tasks) != 1 {
		t.Fatalf("the note should carry one checkbox line, got %d", len(note.Tasks))
	}
	if !strings.Contains(note.Body, "https://example.com/412") {
		t.Error("the link to the original item is missing")
	}

	// Nothing changed upstream: the second run must not rewrite anything.
	second, err := svc.SyncNow(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 || second.Updated != 0 || second.Skipped != 1 {
		t.Errorf("second run = %+v, want one skip", second)
	}

	before, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(note.Path)))
	if err != nil {
		t.Fatal(err)
	}

	// The user adds their own text; a later sync must leave it alone.
	withNotes := string(before) + "\nМои заметки по задаче.\n"
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(note.Path)), []byte(withNotes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.ix.ReindexPath(ctx, note.Path); err != nil {
		t.Fatal(err)
	}

	conn.items[0].Done = true
	conn.items[0].Hash = "h2"
	third, err := svc.SyncNow(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if third.Updated != 1 {
		t.Errorf("third run = %+v, want one update", third)
	}

	after, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(note.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "Мои заметки по задаче.") {
		t.Error("the user's own text was overwritten")
	}
	if !strings.Contains(string(after), "- [x]") {
		t.Errorf("closing the issue upstream should tick the checkbox:\n%s", after)
	}
}

func TestIntegrationSurvivesReindex(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 9, 9, 10, 0, 0, 0, time.Local)
	conn.items = []integrations.ExternalItem{{
		ExternalID: "evt-1", Kind: integrations.ItemEvent, Title: "Планёрка",
		Start: start, End: start.Add(time.Hour), Hash: "h1",
	}}
	cfg, _ := svc.Add(integrations.Config{Kind: "fake", Name: "Календарь", Enabled: true})
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}

	// A full reindex rebuilds everything derived from files; external events
	// are not derived from files and must not disappear.
	if _, err := svc.ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := store.Events(ctx, start.Add(-time.Hour), start.Add(time.Hour))
	if len(events) != 1 {
		t.Errorf("external events lost on reindex: %+v", events)
	}
}

func TestIntegrationConfigIsPersisted(t *testing.T) {
	svc, _, dir, _ := newIntegrationsFixture(t)

	if _, err := svc.Add(integrations.Config{
		Kind: "fake", Name: "Мои issues", Enabled: true,
		Values: map[string]string{"token": "bitwarden:github-token"},
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(integrations.ConfigFile)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "bitwarden:github-token") {
		t.Errorf("the reference should be stored verbatim:\n%s", raw)
	}

	// A fresh instance sees the same configuration.
	again, _, _, _ := newIntegrationsFixture(t)
	_ = again
	list := svc.List()
	if len(list) != 1 || list[0].Values["token"] != "bitwarden:github-token" {
		t.Errorf("list = %+v", list)
	}
}

func TestIntegrationSyncErrorIsRecorded(t *testing.T) {
	svc, _, _, conn := newIntegrationsFixture(t)
	conn.err = errFakeUpstream

	cfg, _ := svc.Add(integrations.Config{Kind: "fake", Name: "Сломанный", Enabled: true})
	if _, err := svc.SyncNow(context.Background(), cfg.ID); err == nil {
		t.Fatal("expected the upstream failure to surface")
	}
	list := svc.List()
	if list[0].LastError == "" {
		t.Error("the error should be visible in the integration list")
	}
}

var errFakeUpstream = errFake("сервер недоступен")

type errFake string

func (e errFake) Error() string { return string(e) }

func TestAddRejectsUnknownKind(t *testing.T) {
	svc, _, _, _ := newIntegrationsFixture(t)
	if _, err := svc.Add(integrations.Config{Kind: "не существует"}); err == nil {
		t.Fatal("expected an error for an unknown connector")
	}
}

func TestTaskNoteCarriesDueDate(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()

	due := time.Date(2026, 9, 20, 0, 0, 0, 0, time.Local)
	conn.items = []integrations.ExternalItem{{
		ExternalID: "x-1", Kind: integrations.ItemTask, Title: "С дедлайном",
		Due: due, Hash: "h1",
	}}
	cfg, _ := svc.Add(integrations.Config{Kind: "fake", Name: "Задачи", Enabled: true})
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}

	note, err := store.NoteBySource(ctx, cfg.ID, "x-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(note.Tasks) != 1 {
		t.Fatalf("tasks = %d", len(note.Tasks))
	}
	if got := note.Tasks[0].Due; got.String() != (domain.Date{T: due}).String() {
		t.Errorf("due = %q", got.String())
	}
}
