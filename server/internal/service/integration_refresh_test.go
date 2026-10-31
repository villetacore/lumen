package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/integrations"
)

func TestImportedBodyRefreshKeepsLocalText(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Docs"})
	if err != nil {
		t.Fatal(err)
	}
	item := integrations.ExternalItem{Kind: integrations.ItemNote, ExternalID: "1", Title: "Guide", Body: "Old source", Hash: "h1"}
	conn.items = []integrations.ExternalItem{item}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, _ := store.NoteBySource(ctx, cfg.ID, "1")
	if _, err := svc.notes.ReplaceBody(ctx, n.Path, n.Body+"\nMy personal text\n", n.Revision); err != nil {
		t.Fatal(err)
	}
	conn.items[0].Body = "New source"
	conn.items[0].Hash = "h2"
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, _ = store.NoteBySource(ctx, cfg.ID, "1")
	if strings.Contains(n.Body, "Old source") || !strings.Contains(n.Body, "New source") || !strings.Contains(n.Body, "My personal text") {
		t.Fatalf("body=%s", n.Body)
	}
	before := n.Body
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, _ = store.NoteBySource(ctx, cfg.ID, "1")
	if n.Body != before {
		t.Fatal("retry changed body")
	}
}

func TestExternalTaskIDsAreConnectionScoped(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	a, err := svc.Add(integrations.Config{Kind: "fake", Name: "A", Folder: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Add(integrations.Config{Kind: "fake", Name: "B", Folder: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	conn.items = []integrations.ExternalItem{{Kind: integrations.ItemTask, ExternalID: "42", Title: "Issue", Body: "Original", Hash: "h1"}}
	for _, cfg := range []integrations.Config{a, b} {
		if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
			t.Fatal(err)
		}
	}
	na, _ := store.NoteBySource(ctx, a.ID, "42")
	nb, _ := store.NoteBySource(ctx, b.ID, "42")
	if na.Tasks[0].ID == nb.Tasks[0].ID {
		t.Fatal("task IDs collide")
	}
	bid := nb.Tasks[0].ID
	conn.items[0].Done = true
	conn.items[0].Body = "Changed"
	conn.items[0].Hash = "h2"
	if _, err := svc.SyncNow(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	na, _ = store.NoteBySource(ctx, a.ID, "42")
	nb, _ = store.NoteBySource(ctx, b.ID, "42")
	if na.Tasks[0].Status != domain.StatusDone || nb.Tasks[0].Status != domain.StatusTodo || nb.Tasks[0].ID != bid || strings.Contains(nb.Body, "Changed") {
		t.Fatalf("a=%+v b=%+v", na.Tasks[0], nb.Tasks[0])
	}
}

func TestLegacyRefreshPreservesContentAndTaskLinks(t *testing.T) {
	svc, store, dir, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	oldID := "x" + shortHash("42")
	body := "# Original\n\n- [ ] Original ^" + oldID + "\n\nLocal edits mixed into old imported text.\n\n- [ ] Personal task ^personal\n"
	n, err := svc.notes.Create(ctx, CreateInput{Path: "legacy.md", Title: "Original", Body: body, Extra: map[string]string{"source_integration": cfg.ID, "source_id": "42", "source_hash": "old"}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.vault.Read(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	conn.items = []integrations.ExternalItem{{Kind: integrations.ItemTask, ExternalID: "42", Title: "Updated", Body: "Fresh source", Done: true, Hash: "new"}}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Note(ctx, n.Path)
	if len(got.Tasks) != 2 || got.Tasks[0].ID != oldID || got.Tasks[0].Status != domain.StatusDone || got.Tasks[1].ID != "personal" {
		t.Fatalf("tasks=%+v", got.Tasks)
	}
	if !strings.Contains(got.Body, "Fresh source") || !strings.Contains(got.Body, "Local edits mixed") {
		t.Fatalf("body=%s", got.Body)
	}
	backup := filepath.Join(dir, ".lumen/backups/imports", sourceHash(string(before))+".md")
	saved, err := os.ReadFile(backup)
	if err != nil || string(saved) != string(before) {
		t.Fatalf("backup err=%v", err)
	}
	conn.items[0].Body = "Newest source"
	conn.items[0].Hash = "newer"
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Note(ctx, n.Path)
	if strings.Count(got.Body, "Сохранённые материалы") != 1 || !strings.Contains(got.Body, "Newest source") || got.Tasks[0].ID != oldID {
		t.Fatalf("body=%s", got.Body)
	}
}

func TestLocalSourceEditConflictsWithoutOverwriting(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Docs"})
	if err != nil {
		t.Fatal(err)
	}
	conn.items = []integrations.ExternalItem{{Kind: integrations.ItemNote, ExternalID: "1", Title: "Guide", Body: "Source", Hash: "h1"}}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, _ := store.NoteBySource(ctx, cfg.ID, "1")
	edited := strings.Replace(n.Body, "Source", "My own replacement", 1)
	if _, err := svc.notes.ReplaceBody(ctx, n.Path, edited, n.Revision); err != nil {
		t.Fatal(err)
	}
	conn.items[0].Body = "Remote replacement"
	conn.items[0].Hash = "h2"
	if _, err := svc.SyncNow(ctx, cfg.ID); err == nil {
		t.Fatal("expected conflict")
	}
	got, _ := store.Note(ctx, n.Path)
	if got.Body != edited || asStr(got.Frontmatter["source_hash"]) != "h1" || svc.List()[0].LastError == "" {
		t.Fatal("conflict lost local content or advanced source hash")
	}
}

func TestConnectorKindCannotOverrideExplicitConnectionOwner(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	a, err := svc.Add(integrations.Config{Kind: "fake", ID: "first", Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Add(integrations.Config{Kind: "fake", ID: "fake", Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	conn.items = []integrations.ExternalItem{{Kind: integrations.ItemNote, ExternalID: "42", Title: "Guide", Hash: "one", Body: "First body"}}
	if _, err := svc.SyncNow(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	conn.items[0].Body = "Second body"
	conn.items[0].Hash = "two"
	if _, err := svc.SyncNow(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	first, err := store.NoteBySource(ctx, a.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.NoteBySource(ctx, b.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || !strings.Contains(first.Body, "First body") || !strings.Contains(second.Body, "Second body") {
		t.Fatal("connection ownership leaked through connector kind")
	}
}

func TestImportedCRLFRefreshAndRestart(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Docs"})
	if err != nil {
		t.Fatal(err)
	}
	conn.items = []integrations.ExternalItem{{Kind: integrations.ItemNote, ExternalID: "42", Title: "Guide", Hash: "one", Body: "First body"}}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, err := store.NoteBySource(ctx, cfg.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := svc.vault.Read(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an older numeric ID plus Windows line endings on disk.
	converted := strings.Replace(string(raw), `source_id: "42"`, "source_id: 42", 1)
	converted = strings.ReplaceAll(converted, "\n", "\r\n")
	if err := svc.vault.Write(n.Path, []byte(converted)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	conn.items[0].Body = "Second body"
	conn.items[0].Hash = "two"
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	conn.items[0].Body = "Third body"
	conn.items[0].Hash = "three"
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.NoteBySource(ctx, cfg.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "Third body") || strings.Contains(got.Body, "Second body") {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestLocalTaskStatusDoesNotBlockSourceRefresh(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	conn.items = []integrations.ExternalItem{{Kind: integrations.ItemTask, ExternalID: "42", Title: "Issue", Body: "First body", Hash: "one"}}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, err := store.NoteBySource(ctx, cfg.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	status := domain.StatusDoing
	if _, err := svc.tasks.Update(ctx, n.Tasks[0].ID, TaskUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	conn.items[0].Body = "Second body"
	conn.items[0].Hash = "two"
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	n, err = store.NoteBySource(ctx, cfg.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	if n.Tasks[0].Status != domain.StatusDoing || !strings.Contains(n.Body, "Second body") {
		t.Fatalf("body=%s status=%s", n.Body, n.Tasks[0].Status)
	}
}
