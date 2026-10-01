// Package watch turns filesystem notifications into a debounced stream of
// vault-relative paths. It exists so the indexer never has to know whether the
// events came from fsnotify or from a polling fallback.
package watch

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Change names one file that needs re-indexing.
type Change struct {
	Path    string // vault-relative, slash separated
	Removed bool
}

// Watcher reports changes to markdown files under a root directory.
type Watcher struct {
	root     string
	ignore   func(rel string) bool
	debounce time.Duration
	log      *slog.Logger

	fsw     *fsnotify.Watcher
	changes chan Change
}

// New starts watching root recursively. Directories created later are picked up
// automatically. The caller must drain Changes and eventually call Close.
func New(root string, ignore func(rel string) bool, debounce time.Duration, log *slog.Logger) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		root:     root,
		ignore:   ignore,
		debounce: debounce,
		log:      log,
		fsw:      fsw,
		changes:  make(chan Change, 256),
	}
	if err := w.addTree(root); err != nil {
		fsw.Close()
		return nil, err
	}
	return w, nil
}

// Changes is the debounced stream of file changes.
func (w *Watcher) Changes() <-chan Change { return w.changes }

func (w *Watcher) Close() error { return w.fsw.Close() }

// Run pumps events until the context is cancelled or the watcher is closed.
func (w *Watcher) Run(ctx context.Context) {
	defer close(w.changes)

	// Pending collects paths seen since the last flush. Editors rename and
	// rewrite files several times per save, so nothing is emitted until the
	// filesystem has been quiet for `debounce`.
	pending := map[string]bool{} // path -> removed
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	armed := false

	flush := func() {
		for path, removed := range pending {
			select {
			case w.changes <- Change{Path: path, Removed: removed}:
			case <-ctx.Done():
				return
			}
		}
		pending = map[string]bool{}
		armed = false
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			rel, interesting := w.classify(ev)
			if !interesting {
				continue
			}
			removed := ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)
			// A rename away and a write back in the same window must not cancel
			// each other out: the last observation wins, and a missing file is
			// resolved as a deletion by the indexer anyway.
			pending[rel] = removed
			if !armed {
				armed = true
				timer.Reset(w.debounce)
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.log.Warn("watcher error", "err", err)

		case <-timer.C:
			flush()
		}
	}
}

// classify maps an fsnotify event onto a vault path, and adds watches for new
// directories along the way.
func (w *Watcher) classify(ev fsnotify.Event) (string, bool) {
	rel, err := filepath.Rel(w.root, ev.Name)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || w.ignore(rel) {
		return "", false
	}

	if ev.Has(fsnotify.Create) {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			// A whole directory can be created, or moved in, at once.
			if err := w.addTree(ev.Name); err != nil {
				w.log.Warn("watch new directory", "path", rel, "err", err)
			}
			return "", false
		}
	}
	if !strings.EqualFold(filepath.Ext(rel), ".md") {
		return "", false
	}
	// Chmod alone changes no content.
	if ev.Op == fsnotify.Chmod {
		return "", false
	}
	return rel, true
}

// addTree registers every non-hidden directory under root.
func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry must not abort setup
		}
		rel, rerr := filepath.Rel(w.root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && w.ignore(rel) {
			return fs.SkipDir
		}
		if err := w.fsw.Add(p); err != nil {
			w.log.Warn("watch directory", "path", rel, "err", err)
		}
		return nil
	})
}
