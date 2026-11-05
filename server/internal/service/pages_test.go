package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/markdown"
)

func page(t *testing.T, n *Notes, title, parent string) *domain.Note {
	t.Helper()
	p, err := n.Create(context.Background(), CreateInput{Title: title, Parent: parent, ContentKind: "document"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func titles(t *testing.T, n *Notes, parent string) string {
	t.Helper()
	list, err := n.pages(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = p.Title
	}
	return strings.Join(out, " ")
}

func TestPageTreeCreateMoveReorder(t *testing.T) {
	_, notes, _, dir := newTestVault(t, nil)
	ctx := context.Background()
	a := page(t, notes, "A", "")
	b := page(t, notes, "B", "")
	c := page(t, notes, "C", "")
	if got := titles(t, notes, ""); got != "A B C" {
		t.Fatalf("new pages go last: %s", got)
	}

	// Between two siblings: only the moved file changes.
	before, _ := os.ReadFile(filepath.Join(dir, a.Path))
	if _, err := notes.MovePage(ctx, c.ID, PageMove{Before: a.ID, After: b.ID}); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, notes, ""); got != "A C B" {
		t.Fatalf("order = %s", got)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, a.Path)); string(after) != string(before) {
		t.Fatal("a sibling's file changed")
	}

	// Nesting keeps the file where it is, so paths and wikilinks hold.
	moved, err := notes.MovePage(ctx, b.ID, PageMove{Parent: "note:" + a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Path != b.Path || moved.Frontmatter["parent"] != "note:"+a.ID {
		t.Fatalf("moved = %+v", moved)
	}
	if got := titles(t, notes, "note:"+a.ID); got != "B" {
		t.Fatalf("children = %s", got)
	}

	// Out again, to the top.
	if _, err := notes.MovePage(ctx, b.ID, PageMove{After: a.ID}); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, notes, ""); got != "B A C" {
		t.Fatalf("order = %s", got)
	}
}

func TestPageTreeRefusesCyclesAndConflicts(t *testing.T) {
	_, notes, _, _ := newTestVault(t, nil)
	ctx := context.Background()
	a := page(t, notes, "A", "")
	b := page(t, notes, "B", "note:"+a.ID)
	c := page(t, notes, "C", "note:"+b.ID)

	if _, err := notes.MovePage(ctx, a.ID, PageMove{Parent: "note:" + c.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("into its own descendant: %v", err)
	}
	if _, err := notes.MovePage(ctx, a.ID, PageMove{Parent: "note:" + a.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("into itself: %v", err)
	}
	if _, err := notes.MovePage(ctx, a.ID, PageMove{Revision: a.Revision + 100}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	plain, err := notes.Create(ctx, CreateInput{Title: "Заметка", Path: "10-notes/n.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := notes.MovePage(ctx, plain.ID, PageMove{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("plain note in the tree: %v", err)
	}
}

func TestPageTreeDepthLimit(t *testing.T) {
	_, notes, _, _ := newTestVault(t, nil)
	ctx := context.Background()
	prev := page(t, notes, "корень", "")
	root := prev
	var last *domain.Note
	for i := 0; i < maxPageDepth; i++ {
		last = page(t, notes, "уровень", "note:"+prev.ID)
		prev = last
	}
	other := page(t, notes, "другая", "")
	if _, err := notes.MovePage(ctx, other.ID, PageMove{Parent: "note:" + last.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too deep: %v", err)
	}
	_ = root
}

// Older documents have no rank: the first move among them gives the level one.
func TestPageTreeRanksUnrankedSiblings(t *testing.T) {
	_, notes, _, _ := newTestVault(t, map[string]string{
		"60-documents/a.md": "---\ntitle: Альфа\ncontent_kind: document\n---\n",
		"60-documents/b.md": "---\ntitle: Бета\ncontent_kind: document\n---\n",
		"60-documents/c.md": "---\ntitle: Гамма\ncontent_kind: document\n---\n",
	})
	ctx := context.Background()
	if got := titles(t, notes, ""); got != "Альфа Бета Гамма" {
		t.Fatalf("unranked read by title: %s", got)
	}
	if _, err := notes.MovePage(ctx, "60-documents/c.md", PageMove{Before: "", After: mustID(t, notes, "60-documents/b.md")}); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, notes, ""); got != "Альфа Гамма Бета" {
		t.Fatalf("order = %s", got)
	}
	// And the order holds for a new page, which goes last.
	page(t, notes, "Дельта", "")
	if got := titles(t, notes, ""); got != "Альфа Гамма Бета Дельта" {
		t.Fatalf("order = %s", got)
	}
}

func mustID(t *testing.T, n *Notes, p string) string {
	t.Helper()
	note, err := n.Get(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return note.ID
}

func TestDeletingAPageKeepsItsChildrenInTheTree(t *testing.T) {
	_, notes, _, _ := newTestVault(t, nil)
	ctx := context.Background()
	a := page(t, notes, "A", "")
	b := page(t, notes, "B", "note:"+a.ID)
	c := page(t, notes, "C", "note:"+b.ID)
	if err := notes.Delete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := notes.Get(ctx, c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Frontmatter["parent"] != "note:"+a.ID {
		t.Fatalf("child's parent = %v", got.Frontmatter["parent"])
	}
}

func TestProjectTree(t *testing.T) {
	s, dir := trackerVault(t)
	ctx := context.Background()
	mob, err := s.CreateProject(ctx, ProjectInput{Key: "MOB", Title: "Мобильное", Parent: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if mob.Parent != "WEB" {
		t.Fatalf("parent = %q", mob.Parent)
	}
	ios, err := s.CreateProject(ctx, ProjectInput{Key: "IOS", Title: "iOS", Parent: "MOB"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(ctx, ProjectInput{Key: "BAD", Title: "x", Parent: "NOPE"}); err == nil {
		t.Fatal("unknown parent accepted")
	}

	// Cycles, directly and through a descendant.
	for _, target := range []string{"WEB", "IOS"} {
		parent := target
		if _, err := s.UpdateProject(ctx, map[string]string{"WEB": "WEB", "IOS": "WEB"}[target], ProjectUpdate{Parent: &parent}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cycle via %s: %v", target, err)
		}
	}
	// A move does not touch files of the project.
	if _, err := s.Create(ctx, IssueInput{Project: "IOS", Title: "Кнопка"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "20-projects/IOS/issues/IOS-1.md")); err != nil {
		t.Fatal(err)
	}
	web := "WEB"
	moved, err := s.UpdateProject(ctx, "IOS", ProjectUpdate{Parent: &web})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Parent != "WEB" || moved.Path != ios.Path {
		t.Fatalf("moved = %+v", moved)
	}
	if _, err := s.Get(ctx, "IOS-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProject(ctx, "IOS", ProjectUpdate{Revision: moved.Revision + 100}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}

	// Issues of the subtree, on request only.
	if _, err := s.Create(ctx, IssueInput{Project: "WEB", Title: "Сайт"}, ""); err != nil {
		t.Fatal(err)
	}
	if list, _, _ := s.List(ctx, domain.IssueQuery{Project: "WEB"}); len(list) != 1 {
		t.Fatalf("own issues = %d", len(list))
	}
	if list, _, _ := s.List(ctx, domain.IssueQuery{Project: "WEB", Subprojects: true}); len(list) != 2 {
		t.Fatalf("subtree issues = %d", len(list))
	}

	// Removing the parent would strand the subtree (and open it if it was closed).
	notes := NewNotes(s.vault, s.store, s.ix, s.bus, s.log)
	if err := notes.Delete(ctx, "20-projects/web.md"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("delete of a project with subprojects: %v", err)
	}
	// An edit in Obsidian can make a cycle; reading it must not hang.
	writeParent(t, dir, "20-projects/web.md", "IOS")
	if err := s.ix.ReindexPath(ctx, "20-projects/web.md"); err != nil {
		t.Fatal(err)
	}
	if list, _, err := s.List(ctx, domain.IssueQuery{Project: "WEB", Subprojects: true}); err != nil || len(list) != 2 {
		t.Fatalf("cycle on disk: %d %v", len(list), err)
	}
}

func writeParent(t *testing.T, dir, rel, parent string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	_, lines := splitLines(data)
	lines = markdown.SetFrontmatterValue(lines, "parent_project", parent)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubIssues(t *testing.T) {
	s, _ := trackerVault(t)
	ctx := context.Background()
	if _, err := s.CreateProject(ctx, ProjectInput{Key: "OTH", Title: "Другой"}); err != nil {
		t.Fatal(err)
	}
	top, _ := s.Create(ctx, IssueInput{Project: "WEB", Title: "Большая"}, "")
	mid, err := s.Create(ctx, IssueInput{Project: "WEB", Title: "Средняя", Parent: top.Key}, "")
	if err != nil {
		t.Fatal(err)
	}
	low, err := s.Create(ctx, IssueInput{Project: "WEB", Title: "Малая", Parent: mid.Key}, "")
	if err != nil {
		t.Fatal(err)
	}
	if list, _, _ := s.List(ctx, domain.IssueQuery{Parent: top.Key}); len(list) != 1 || list[0].Key != mid.Key {
		t.Fatalf("children = %+v", list)
	}
	for _, bad := range []string{low.Key, top.Key} {
		if _, err := s.Update(ctx, top.Key, IssueUpdate{Parent: strp(bad)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cycle via %s: %v", bad, err)
		}
	}
	if _, err := s.Create(ctx, IssueInput{Project: "OTH", Title: "x", Parent: top.Key}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("parent from another project: %v", err)
	}
	if _, err := s.Create(ctx, IssueInput{Project: "WEB", Title: "x", Parent: "WEB-99"}, ""); err == nil {
		t.Fatal("unknown parent accepted")
	}
	// Deleting the middle hands the child up.
	if err := s.Delete(ctx, mid.Key); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, low.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent != top.Key {
		t.Fatalf("parent after delete = %q", got.Parent)
	}
}
