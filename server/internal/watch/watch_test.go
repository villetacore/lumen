package watch

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestWatcher(t *testing.T, root string) (*Watcher, context.CancelFunc) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ignore := func(rel string) bool {
		for _, part := range strings.Split(rel, "/") {
			if part != "" && strings.HasPrefix(part, ".") {
				return true
			}
		}
		return false
	}
	w, err := New(root, ignore, 40*time.Millisecond, log)
	if err != nil {
		t.Skipf("fsnotify unavailable here: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)
	t.Cleanup(func() {
		cancel()
		w.Close()
	})
	return w, cancel
}

// waitFor collects changes until one matches, or the deadline passes.
func waitFor(t *testing.T, w *Watcher, want string, removed bool) {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		select {
		case c := <-w.Changes():
			if c.Path == want && c.Removed == removed {
				return
			}
		case <-deadline:
			t.Fatalf("no change for %q (removed=%v) within the deadline", want, removed)
		}
	}
}

func TestDetectsWriteAndDelete(t *testing.T) {
	root := t.TempDir()
	w, _ := newTestWatcher(t, root)

	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# первая\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "note.md", false)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "note.md", true)
}

func TestDetectsFilesInNewDirectories(t *testing.T) {
	root := t.TempDir()
	w, _ := newTestWatcher(t, root)

	dir := filepath.Join(root, "20-projects", "sub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The watcher has to notice the directory before the file lands in it.
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "20-projects/sub/x.md", false)
}

func TestIgnoresHiddenAndNonMarkdown(t *testing.T) {
	root := t.TempDir()
	w, _ := newTestWatcher(t, root)

	if err := os.MkdirAll(filepath.Join(root, ".lumen"), 0o755); err != nil {
		t.Fatal(err)
	}
	writes := map[string]string{
		filepath.Join(root, ".lumen", "cache.md"): "скрытое",
		filepath.Join(root, "image.png"):          "не markdown",
		filepath.Join(root, "real.md"):            "# настоящая\n",
	}
	for p, body := range writes {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Only real.md may show up; anything before it is a failure.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case c := <-w.Changes():
			if c.Path != "real.md" {
				t.Fatalf("unexpected change for %q", c.Path)
			}
			return
		case <-deadline:
			t.Fatal("real.md was never reported")
		}
	}
}

func TestDebouncesRapidWrites(t *testing.T) {
	root := t.TempDir()
	w, _ := newTestWatcher(t, root)

	path := filepath.Join(root, "busy.md")
	for i := 0; i < 12; i++ {
		if err := os.WriteFile(path, []byte("# "+time.Now().String()+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Millisecond)
	}
	waitFor(t, w, "busy.md", false)

	// A burst of writes must collapse into one, maybe two, notifications.
	extra := 0
	timeout := time.After(400 * time.Millisecond)
	for {
		select {
		case <-w.Changes():
			extra++
			if extra > 2 {
				t.Fatalf("burst produced %d extra notifications", extra)
			}
		case <-timeout:
			return
		}
	}
}
