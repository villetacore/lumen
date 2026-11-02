package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

const trackerProject = `---
title: Сайт
type: project
key: WEB
workflow:
  - id: todo
    name: К работе
    category: todo
  - id: doing
    name: В работе
    category: in_progress
  - id: done
    name: Готово
    category: done
transitions:
  todo: [doing]
  doing: [todo, done]
---

# Сайт
`

func newIssues(t *testing.T, dir string) (*Issues, index.Store, *Bus) {
	t.Helper()
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
	return NewIssues(v, store, ix, bus, log), store, bus
}

func trackerVault(t *testing.T) (*Issues, string) {
	t.Helper()
	_, _, _, dir := newTestVault(t, map[string]string{"20-projects/web.md": trackerProject})
	s, _, _ := newIssues(t, dir)
	return s, dir
}

func TestIssueCreateAndRead(t *testing.T) {
	s, dir := trackerVault(t)
	ctx := context.Background()
	is, err := s.Create(ctx, IssueInput{
		Project: "web", Title: "Канбан: доска", Priority: "high", Assignee: "alice",
		Labels: []string{"ui"}, Due: "2026-11-10", Points: 3, Description: "Текст\n- [ ] шаг",
	}, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if is.Key != "WEB-1" || is.Status != "todo" || is.Type != "task" || is.Reporter != "bob" ||
		is.StatusCategory != domain.CategoryTodo || is.Title != "Канбан: доска" || len(is.Checklist) != 1 {
		t.Fatalf("issue = %+v", is)
	}
	data, err := os.ReadFile(filepath.Join(dir, "20-projects/WEB/issues/WEB-1.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"id: WEB-1\n", `title: "Канбан: доска"`, "due: 2026-11-10\n", "labels: [ui]\n", "points: 3\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file lacks %q:\n%s", want, data)
		}
	}
	p, err := s.Project(ctx, "WEB")
	if err != nil || p.Seq != 1 {
		t.Fatalf("project seq = %+v, %v", p, err)
	}
	if _, err := s.Create(ctx, IssueInput{Project: "WEB", Title: "x", Status: "nope"}, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown status: %v", err)
	}
}

// Keys must stay unique under concurrent requests and across a restart that
// rebuilds the index, including after the newest issue was deleted.
func TestIssueKeysAreUniqueAcrossConcurrencyAndRestart(t *testing.T) {
	s, dir := trackerVault(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	keys := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			is, err := s.Create(ctx, IssueInput{Project: "WEB", Title: fmt.Sprint("t", i)}, "")
			if err != nil {
				t.Error(err)
				return
			}
			keys <- is.Key
		}(i)
	}
	wg.Wait()
	close(keys)
	seen := map[string]bool{}
	for k := range keys {
		if seen[k] {
			t.Fatalf("duplicate key %s", k)
		}
		seen[k] = true
	}
	if len(seen) != 20 {
		t.Fatalf("created %d issues", len(seen))
	}
	if err := s.Delete(ctx, "WEB-20"); err != nil {
		t.Fatal(err)
	}

	restarted, _, _ := newIssues(t, dir)
	is, err := restarted.Create(ctx, IssueInput{Project: "WEB", Title: "после рестарта"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if is.Key != "WEB-21" {
		t.Fatalf("key after restart = %s, deleted WEB-20 must not be reused", is.Key)
	}
	list, total, err := restarted.List(ctx, domain.IssueQuery{Project: "WEB"})
	if err != nil || total != 20 || list[len(list)-1].Key != "WEB-21" {
		t.Fatalf("list total=%d err=%v", total, err)
	}
}

func TestIssueWorkflowAndMove(t *testing.T) {
	s, _ := trackerVault(t)
	ctx := context.Background()
	for _, title := range []string{"a", "b", "c"} {
		if _, err := s.Create(ctx, IssueInput{Project: "WEB", Title: title}, ""); err != nil {
			t.Fatal(err)
		}
	}
	done := "done"
	if _, err := s.Update(ctx, "WEB-1", IssueUpdate{Status: &done}); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("todo → done must be refused, got %v", err)
	}
	// Drop WEB-3 between WEB-1 and WEB-2 in "doing".
	is, err := s.Move(ctx, "WEB-3", MoveInput{Status: "doing", Before: "WEB-1", After: "WEB-2"})
	if err != nil {
		t.Fatal(err)
	}
	if is.Status != "doing" || is.StatusCategory != domain.CategoryInProgress {
		t.Fatalf("moved = %+v", is)
	}
	list, _, _ := s.List(ctx, domain.IssueQuery{Project: "WEB"})
	var order []string
	for _, i := range list {
		order = append(order, i.Key)
	}
	if strings.Join(order, ",") != "WEB-1,WEB-3,WEB-2" {
		t.Fatalf("order = %v", order)
	}
	doing, _, _ := s.List(ctx, domain.IssueQuery{Category: domain.CategoryInProgress})
	if len(doing) != 1 || doing[0].Key != "WEB-3" {
		t.Fatalf("category filter = %v", doing)
	}
}

func TestIssueUpdateKeepsUnknownFieldsAndComments(t *testing.T) {
	src := "---\nid: WEB-5\ntitle: Старое\ntype: issue\nproject: WEB\nstatus: todo\n" +
		"custom: значение # оставить\nlabels:\n  - a\n---\n\nОписание\n\n## Комментарии\n\n" +
		"### 2026-11-01 10:00 · bob ^c-1aa\nПервый\n"
	_, _, _, dir := newTestVault(t, map[string]string{
		"20-projects/web.md": trackerProject, "20-projects/WEB/issues/WEB-5.md": src,
	})
	s, _, _ := newIssues(t, dir)
	ctx := context.Background()
	title, desc := "Новое", "Новое описание"
	labels := []string{"b", "c"}
	is, err := s.Update(ctx, "WEB-5", IssueUpdate{Title: &title, Description: &desc, Labels: &labels})
	if err != nil {
		t.Fatal(err)
	}
	if is.Title != "Новое" || is.Description != "Новое описание" || strings.Join(is.Labels, ",") != "b,c" {
		t.Fatalf("issue = %+v", is)
	}
	is, c, err := s.AddComment(ctx, "WEB-5", "Второй\nс переносом", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Author != "alice" || c.Body != "Второй\nс переносом" || len(is.Comments) != 2 {
		t.Fatalf("comment = %+v, comments = %d", c, len(is.Comments))
	}
	data, _ := os.ReadFile(filepath.Join(dir, "20-projects/WEB/issues/WEB-5.md"))
	text := string(data)
	for _, want := range []string{"custom: значение # оставить\n", "### 2026-11-01 10:00 · bob ^c-1aa\nПервый\n", "labels: [b, c]\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("file lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "  - a") {
		t.Errorf("old list items left behind:\n%s", text)
	}
}

func TestIssueRevisionConflictAndExternalEdit(t *testing.T) {
	s, dir := trackerVault(t)
	ctx := context.Background()
	is, err := s.Create(ctx, IssueInput{Project: "WEB", Title: "a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Someone edits the file in Obsidian; the watcher reindexes it.
	p := filepath.Join(dir, "20-projects/WEB/issues/WEB-1.md")
	data, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), "status: todo", "status: doing", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	s.ix.Forget("20-projects/WEB/issues/WEB-1.md")
	if err := s.ix.ReindexPath(ctx, "20-projects/WEB/issues/WEB-1.md"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "WEB-1")
	if err != nil || got.Status != "doing" {
		t.Fatalf("external edit not visible: %+v %v", got, err)
	}
	title := "b"
	if _, err := s.Update(ctx, "WEB-1", IssueUpdate{Title: &title, Revision: is.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
}

func TestCreateTrackerProject(t *testing.T) {
	_, _, _, dir := newTestVault(t, map[string]string{
		"20-projects/old.md": "---\ntitle: Старый\ntype: project\ntags: [x]\n---\n\n# Старый\n",
	})
	s, _, _ := newIssues(t, dir)
	ctx := context.Background()
	p, err := s.CreateProject(ctx, ProjectInput{Key: "old", Note: "20-projects/old.md"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "OLD" || len(p.Workflow) != 3 || p.Title != "Старый" {
		t.Fatalf("project = %+v", p)
	}
	if _, err := s.CreateProject(ctx, ProjectInput{Key: "OLD", Title: "Ещё"}); !errors.Is(err, ErrAlreadyExists()) {
		t.Fatalf("duplicate key: %v", err)
	}
	p, err = s.CreateProject(ctx, ProjectInput{Key: "NEW", Title: "Новый"})
	if err != nil || p.Path != "20-projects/new.md" {
		t.Fatalf("new project = %+v %v", p, err)
	}
	if _, err := s.CreateProject(ctx, ProjectInput{Key: "1BAD", Title: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad key: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "20-projects/old.md"))
	if !strings.Contains(string(data), "tags: [x]\n") {
		t.Errorf("existing fields lost:\n%s", data)
	}
}
