package service

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/villetacore/lumen/server/internal/history"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/policy"
	"github.com/villetacore/lumen/server/internal/state/statetest"
	"github.com/villetacore/lumen/server/internal/vault"
)

// TestHistoryThroughIndexer follows an issue through Lumen writes, an edit
// made outside Lumen, a full reindex (what a restart does) and deletion.
func TestHistoryThroughIndexer(t *testing.T) {
	ctx := context.Background()
	_, _, _, dir := newTestVault(t, map[string]string{"20-projects/web.md": trackerProject})
	st := statetest.Open(t)
	defer st.Close()
	v, _ := vault.Open(dir)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := index.NewMemory(v.Root)
	ix := NewIndexer(v, store, NewBus(), log)
	j := history.New(st.DB(), st)
	ix.SetObserver(j)
	if _, err := ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	s := NewIssues(v, store, ix, NewBus(), log)
	alice := policy.With(ctx, &policy.Principal{ID: "s-a", Username: "alice", Role: policy.RoleMember})

	is, err := s.Create(alice, IssueInput{Project: "WEB", Title: "Макет", Assignee: "bob"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(alice, is.Key, MoveInput{Status: "doing"}); err != nil {
		t.Fatal(err)
	}
	// Someone edits the file in Obsidian; the watcher reindexes it.
	rel := "20-projects/WEB/issues/WEB-1.md"
	p := filepath.Join(dir, filepath.FromSlash(rel))
	data, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(data), "assignee: bob", "assignee: carol", 1)), 0o644)
	ix.Forget(rel)
	if err := ix.ReindexPath(ctx, rel); err != nil {
		t.Fatal(err)
	}
	// A restart reindexes everything; nothing new happened, nothing is added.
	if _, err := ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(alice, is.Key); err != nil {
		t.Fatal(err)
	}

	ev, err := j.Events(ctx, is.Key)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ev {
		got = append(got, e.Kind+":"+e.Field+":"+e.New+"@"+e.Source+"/"+e.Actor)
	}
	want := []string{
		"created::@lumen/alice",
		"changed:status:doing@lumen/alice",
		"changed:assignee:carol@external/",
		"deleted::@lumen/alice",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("history:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
