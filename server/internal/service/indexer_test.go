package service

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

func newIndexer(t *testing.T) (*Indexer, index.Store, *Bus, string) {
	t.Helper()
	dir := t.TempDir()
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := index.NewMemory(v.Root)
	bus := NewBus()
	return NewIndexer(v, store, bus, log), store, bus, dir
}

// waitUntil polls a condition; the watcher is asynchronous by nature.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWatchPicksUpExternalEdits(t *testing.T) {
	ix, store, _, dir := newIndexer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	go ix.Watch(ctx, 40*time.Millisecond, 200*time.Millisecond)

	// A file written by another editor, exactly like Obsidian would.
	path := filepath.Join(dir, "outside.md")
	if err := os.WriteFile(path, []byte("# Снаружи\n\n- [ ] Извне ^x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the new note to be indexed", func() bool {
		_, err := store.Task(ctx, "x1a")
		return err == nil
	})

	if err := os.WriteFile(path, []byte("# Снаружи\n\n- [x] Извне ^x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the edit to be picked up", func() bool {
		task, err := store.Task(ctx, "x1a")
		return err == nil && task.Status == domain.StatusDone
	})

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the deletion to be picked up", func() bool {
		_, err := store.Note(ctx, "outside.md")
		return err != nil
	})
}

func TestWatchPublishesEvents(t *testing.T) {
	ix, _, bus, dir := newIndexer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	go ix.Watch(ctx, 40*time.Millisecond, 200*time.Millisecond)

	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Заметка\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Type == "note.changed" && e.Path == "note.md" {
				return
			}
		case <-deadline:
			t.Fatal("no note.changed event arrived")
		}
	}
}

func TestReindexPathIsQuietWhenNothingChanged(t *testing.T) {
	ix, _, bus, dir := newIndexer(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# А\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}

	events, unsubscribe := bus.Subscribe()
	defer unsubscribe()

	// Touching a file without changing its content must not wake up clients.
	if err := ix.ReindexPath(ctx, "a.md"); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(200 * time.Millisecond):
	}
}
