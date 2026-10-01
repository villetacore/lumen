package service

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

func newTestVault(t *testing.T, files map[string]string) (*Tasks, *Notes, index.Store, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
	return NewTasks(v, store, ix, bus, log), NewNotes(v, store, ix, bus, log), store, dir
}

func TestUpdateWritesOnlyTheTaskLine(t *testing.T) {
	const src = "---\ntitle: День\n---\n\n# День\n\nПроизвольный текст.\n\n" +
		"- [ ] Первая ^t1a\n- [ ] Вторая ^t2b\n\nХвост файла.\n"
	tasks, _, _, dir := newTestVault(t, map[string]string{"day.md": src})

	done := domain.StatusDone
	if _, err := tasks.Update(context.Background(), "t2b", TaskUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "day.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntitle: День\n---\n\n# День\n\nПроизвольный текст.\n\n" +
		"- [ ] Первая ^t1a\n- [x] Вторая [done:: " + domain.Today().String() + "] ^t2b\n\nХвост файла.\n"
	if string(got) != want {
		t.Errorf("file changed beyond the task line:\n got  %q\n want %q", got, want)
	}
}

func TestUpdateAssignsBlockID(t *testing.T) {
	tasks, _, store, _ := newTestVault(t, map[string]string{"a.md": "- [ ] Без идентификатора\n"})
	ctx := context.Background()

	list, _, err := store.Tasks(ctx, domain.TaskQuery{})
	if err != nil || len(list) != 1 {
		t.Fatalf("tasks=%d err=%v", len(list), err)
	}
	if list[0].BlockID != "" {
		t.Fatalf("unexpected block id %q", list[0].BlockID)
	}

	due := "2026-09-10"
	updated, err := tasks.Update(ctx, list[0].ID, TaskUpdate{Due: &due})
	if err != nil {
		t.Fatal(err)
	}
	if updated.BlockID == "" {
		t.Error("expected a block id to be assigned on first write")
	}
	if updated.Due.String() != due {
		t.Errorf("due = %q", updated.Due.String())
	}
}

func TestUpdateRejectsStaleRevision(t *testing.T) {
	tasks, _, _, _ := newTestVault(t, map[string]string{"a.md": "- [ ] Задача ^t1a\n"})
	done := domain.StatusDone
	_, err := tasks.Update(context.Background(), "t1a", TaskUpdate{Status: &done, Revision: 999})
	if err == nil {
		t.Fatal("expected a conflict")
	}
}

func TestCreateTaskUnderSection(t *testing.T) {
	const src = "# День\n\n## Задачи\n\n- [ ] Существующая ^t1a\n\n## Заметки\n\nтекст\n"
	tasks, _, _, dir := newTestVault(t, map[string]string{"day.md": src})

	if _, err := tasks.Create(context.Background(), "day.md", "Новая задача", "Задачи", TaskUpdate{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "day.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# День\n\n## Задачи\n\n- [ ] Существующая ^t1a\n- [ ] Новая задача ^"
	if len(got) < len(want) || string(got[:len(want)]) != want {
		t.Errorf("task not inserted under the section:\n%s", got)
	}
}

func TestDailyNoteIsCreatedOnDemand(t *testing.T) {
	_, notes, _, dir := newTestVault(t, map[string]string{"seed.md": "# seed\n"})
	n, err := notes.Daily(context.Background(), domain.Today(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(n.Path))); err != nil {
		t.Errorf("daily note not written: %v", err)
	}
	if n.Type != domain.TypeDaily {
		t.Errorf("type = %q", n.Type)
	}
}
