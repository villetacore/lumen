// Package service wires the vault, the parser and the index into the use cases
// the HTTP layer exposes. It is the only place allowed to write to the vault.
package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/markdown"
	"github.com/alexpyslar/lumen/server/internal/vault"
	"github.com/alexpyslar/lumen/server/internal/watch"
)

// Indexer keeps the index in step with the files on disk.
type Indexer struct {
	vault *vault.Vault
	store index.Store
	log   *slog.Logger
	bus   *Bus

	// seen tracks what the index already holds per path, so repeated events for
	// an unchanged file cost nothing and publish nothing.
	mu   sync.Mutex
	seen map[string]fileStamp
}

type fileStamp struct {
	size int64
	mod  time.Time
	hash string
}

func NewIndexer(v *vault.Vault, st index.Store, bus *Bus, log *slog.Logger) *Indexer {
	return &Indexer{vault: v, store: st, log: log, seen: map[string]fileStamp{}, bus: bus}
}

// ReindexAll rebuilds the whole index from disk.
func (ix *Indexer) ReindexAll(ctx context.Context) (notes int, err error) {
	start := time.Now()
	if err := ix.store.Reset(ctx); err != nil {
		return 0, err
	}
	ix.mu.Lock()
	ix.seen = map[string]fileStamp{}
	ix.mu.Unlock()

	err = ix.vault.Walk(func(fi vault.FileInfo) error {
		if _, e := ix.indexFile(ctx, fi.Path, fi.Size, fi.ModTime); e != nil {
			ix.log.Warn("index file failed", "path", fi.Path, "err", e)
			return nil
		}
		notes++
		return nil
	})
	took := time.Since(start)
	ix.store.SetIndexed(ctx, time.Now(), took.Milliseconds())
	ix.log.Info("reindex finished", "notes", notes, "took", took)
	ix.publish(Event{Type: "reindex.finished", Notes: notes, TookMS: took.Milliseconds()})
	return notes, err
}

// indexFile parses one file into the index and reports whether its content
// actually differs from what was indexed before.
func (ix *Indexer) indexFile(ctx context.Context, rel string, size int64, mod time.Time) (bool, error) {
	data, err := ix.vault.Read(rel)
	if err != nil {
		return false, err
	}
	n, perr := markdown.ParseNote(rel, data)
	if perr != nil {
		// A malformed header is worth a warning, not a dropped note.
		ix.log.Warn("frontmatter parse error", "path", rel, "err", perr)
	}
	if n.Updated.IsZero() {
		n.Updated = mod
	}

	ix.mu.Lock()
	prev, had := ix.seen[rel]
	ix.seen[rel] = fileStamp{size: size, mod: mod, hash: n.Hash}
	ix.mu.Unlock()

	if err := ix.store.PutNote(ctx, n); err != nil {
		return false, err
	}
	return !had || prev.hash != n.Hash, nil
}

// ReindexPath refreshes a single file, removing it from the index if it is gone.
func (ix *Indexer) ReindexPath(ctx context.Context, rel string) error {
	if !ix.vault.Exists(rel) {
		ix.mu.Lock()
		_, had := ix.seen[rel]
		delete(ix.seen, rel)
		ix.mu.Unlock()
		if err := ix.store.DeleteNote(ctx, rel); err != nil {
			return err
		}
		if had {
			ix.publish(Event{Type: "note.deleted", Path: rel})
		}
		return nil
	}

	fi, err := ix.vault.Stat(rel)
	if err != nil {
		return err
	}
	changed, err := ix.indexFile(ctx, rel, fi.Size, fi.ModTime)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	n, err := ix.store.Note(ctx, rel)
	if err != nil {
		return err
	}
	ix.publish(Event{Type: "note.changed", Path: rel, Revision: n.Revision})
	return nil
}

// Watch keeps the index current. It prefers filesystem notifications and falls
// back to polling when they are unavailable (network shares, exhausted inotify
// watches, unsupported platforms).
func (ix *Indexer) Watch(ctx context.Context, debounce, pollEvery time.Duration) {
	w, err := watch.New(ix.vault.Root, vault.IsHidden, debounce, ix.log)
	if err != nil {
		ix.log.Warn("fsnotify unavailable, falling back to polling",
			"err", err, "interval", pollEvery)
		ix.poll(ctx, pollEvery)
		return
	}
	defer w.Close()

	ix.log.Info("watching vault", "root", ix.vault.Root, "debounce", debounce)
	go w.Run(ctx)

	// The watch is live before this sweep, so nothing can slip through the gap
	// between the initial index and the first notification.
	ix.scanOnce(ctx)

	// Filesystem notifications can be dropped (queue overflow, network shares),
	// so a slow sweep stays as a safety net.
	reconcile := time.NewTicker(reconcileEvery)
	defer reconcile.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcile.C:
			ix.scanOnce(ctx)
		case change, ok := <-w.Changes():
			if !ok {
				return
			}
			if err := ix.ReindexPath(ctx, change.Path); err != nil {
				ix.log.Warn("reindex failed", "path", change.Path, "err", err)
			}
		}
	}
}

// reconcileEvery is how often the watcher double-checks the whole vault.
const reconcileEvery = 5 * time.Minute

// poll is the fallback: a full stat sweep on a timer.
func (ix *Indexer) poll(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ix.scanOnce(ctx)
		}
	}
}

func (ix *Indexer) scanOnce(ctx context.Context) {
	present := map[string]bool{}
	_ = ix.vault.Walk(func(fi vault.FileInfo) error {
		present[fi.Path] = true

		ix.mu.Lock()
		s, known := ix.seen[fi.Path]
		ix.mu.Unlock()
		if known && s.size == fi.Size && s.mod.Equal(fi.ModTime) {
			return nil
		}

		changed, err := ix.indexFile(ctx, fi.Path, fi.Size, fi.ModTime)
		if err != nil {
			ix.log.Warn("reindex failed", "path", fi.Path, "err", err)
			return nil
		}
		if changed {
			ix.publish(Event{Type: "note.changed", Path: fi.Path})
		}
		return nil
	})

	paths, err := ix.store.Paths(ctx)
	if err != nil {
		return
	}
	for _, p := range paths {
		if present[p] {
			continue
		}
		_ = ix.store.DeleteNote(ctx, p)
		ix.mu.Lock()
		delete(ix.seen, p)
		ix.mu.Unlock()
		ix.publish(Event{Type: "note.deleted", Path: p})
	}
}

// Forget drops the cached stamp so the next reindex re-reads from disk.
func (ix *Indexer) Forget(path string) {
	ix.mu.Lock()
	delete(ix.seen, path)
	ix.mu.Unlock()
}

func (ix *Indexer) publish(e Event) {
	if ix.bus != nil {
		ix.bus.Publish(e)
	}
}
