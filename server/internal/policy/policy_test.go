package policy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/markdown"
	"github.com/villetacore/lumen/server/internal/state/statetest"
)

func TestRoleMapping(t *testing.T) {
	if (RoleMapping{}).WorkspaceRole(nil) != RoleAdmin {
		t.Error("without admin roles everyone admitted stays admin")
	}
	m := RoleMapping{Admin: []string{"lumen-admin"}, Viewer: []string{"guests"}}
	for have, want := range map[string]Role{"lumen-admin": RoleAdmin, "/guests": RoleViewer, "staff": RoleMember} {
		if got := m.WorkspaceRole([]string{have}); got != want {
			t.Errorf("%s → %s, want %s", have, got, want)
		}
	}
	if m.WorkspaceRole([]string{"guests", "lumen-admin"}) != RoleAdmin {
		t.Error("admin wins over viewer")
	}
}

func fixture(t *testing.T) (*Policy, index.Store) {
	t.Helper()
	st := index.NewMemory("")
	put := func(path, src string) {
		n, err := markdown.ParseNote(path, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		st.PutNote(context.Background(), n)
	}
	put("20-projects/sec.md", "---\ntype: project\nkey: SEC\n---\n")
	put("20-projects/SEC/issues/SEC-1.md", "---\nid: SEC-1\ntype: issue\nproject: SEC\n---\n- [ ] шаг ^step1\n")
	put("60-documents/spec.md", "---\nparent: task:step1\n---\nспецификация\n")
	put("10-notes/open.md", "# Открытая\n")
	p := New(st, nil)
	if _, err := p.members.Set(context.Background(), "SEC", "user", "alice", RoleMember, ""); err != nil {
		t.Fatal(err)
	}
	return p, st
}

func TestNoPrincipalSeesNothing(t *testing.T) {
	p, st := fixture(t)
	s := Scope(st, p)
	ctx := context.Background()
	if notes, total, _ := s.Notes(ctx, domain.NoteQuery{}); len(notes) != 0 || total != 0 {
		t.Fatalf("anonymous sees %d notes", total)
	}
	if _, err := s.Note(ctx, "10-notes/open.md"); !errors.Is(err, index.ErrNotFound) {
		t.Fatalf("anonymous note: %v", err)
	}
	if err := p.CheckWorkspace(ctx, RoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("anonymous workspace: %v", err)
	}
}

func TestParentChainAndDeletedPaths(t *testing.T) {
	p, st := fixture(t)
	s := Scope(st, p)
	bob := With(context.Background(), &Principal{ID: "b", Username: "bob", Role: RoleMember})
	alice := With(context.Background(), &Principal{ID: "a", Username: "alice", Role: RoleViewer})

	// The document hangs off a task of a SEC issue, so it belongs to SEC.
	if _, err := s.Note(bob, "60-documents/spec.md"); !errors.Is(err, index.ErrNotFound) {
		t.Fatalf("bob reads a SEC document via its parent task: %v", err)
	}
	if _, err := s.Note(alice, "60-documents/spec.md"); err != nil {
		t.Fatalf("alice: %v", err)
	}
	// A project role can be above the workspace role.
	if p.ProjectRole(alice, "SEC") != RoleMember || p.ProjectRole(alice, "") != RoleViewer {
		t.Fatalf("alice roles: %s / %s", p.ProjectRole(alice, "SEC"), p.ProjectRole(alice, ""))
	}
	if err := p.CheckCreate(alice, "10-notes/new.md", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a workspace viewer must not create workspace notes: %v", err)
	}
	if err := p.CheckCreate(alice, "20-projects/SEC/notes/new.md", ""); err != nil {
		t.Fatalf("alice writes in SEC: %v", err)
	}
	if err := p.CheckCreate(bob, "60-documents/x.md", "task:step1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("bob creates under a SEC task: %v", err)
	}
	// Deleted files are judged by their folder.
	if p.VisiblePath(bob, "20-projects/SEC/issues/SEC-9.md") || !p.VisiblePath(bob, "10-notes/gone.md") {
		t.Fatal("deleted path visibility")
	}
	tasks, _, _ := s.Tasks(bob, domain.TaskQuery{})
	if len(tasks) != 0 {
		t.Fatalf("bob sees SEC tasks: %v", tasks)
	}
}

func TestMembersSurviveRestart(t *testing.T) {
	ctx := context.Background()
	open := statetest.Opener(t)
	st := open()
	m := NewMembers(st.DB())
	if _, err := m.Set(ctx, "SEC", "group", "/designers", RoleViewer, "boss"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(ctx, "SEC", "group", "designers", RoleMember, "boss"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Set(ctx, "SEC", "robot", "x", RoleMember, ""); err == nil {
		t.Fatal("unknown member kind must be refused")
	}
	st.Close()

	st = open()
	defer st.Close()
	list, err := NewMembers(st.DB()).List(ctx, "SEC")
	if err != nil || len(list) != 1 || list[0].Name != "designers" || list[0].Role != "member" || list[0].AddedBy != "boss" {
		t.Fatalf("after restart: %+v %v", list, err)
	}
}

func treeFixture(t *testing.T) (*Policy, index.Store) {
	t.Helper()
	st := index.NewMemory("")
	put := func(path, src string) {
		n, err := markdown.ParseNote(path, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		st.PutNote(context.Background(), n)
	}
	put("20-projects/sec.md", "---\ntype: project\nkey: SEC\n---\n")
	put("20-projects/sub.md", "---\ntype: project\nkey: SUB\nparent_project: SEC\n---\n")
	put("20-projects/leaf.md", "---\ntype: project\nkey: LEAF\nparent_project: SUB\n---\n")
	put("20-projects/LEAF/issues/LEAF-1.md", "---\nid: LEAF-1\ntype: issue\nproject: LEAF\n---\n")
	put("20-projects/open.md", "---\ntype: project\nkey: OPEN\n---\n")
	p := New(st, nil)
	if _, err := p.members.Set(context.Background(), "SEC", "user", "alice", RoleAdmin, ""); err != nil {
		t.Fatal(err)
	}
	return p, st
}

func as(name string, role Role) context.Context {
	return With(context.Background(), &Principal{ID: name, Username: name, Role: role})
}

// A subproject of a closed project is closed with it, at any depth.
func TestSubprojectInheritsAccess(t *testing.T) {
	p, st := treeFixture(t)
	alice, bob := as("alice", RoleMember), as("bob", RoleMember)
	for _, key := range []string{"SEC", "SUB", "LEAF"} {
		if got := p.ProjectRole(bob, key); got != RoleNone {
			t.Errorf("outsider in %s: %v", key, got)
		}
		if got := p.ProjectRole(alice, key); got != RoleAdmin {
			t.Errorf("member in %s: %v", key, got)
		}
	}
	if got := p.ProjectRole(bob, "OPEN"); got != RoleMember {
		t.Errorf("open project: %v", got)
	}
	issue, err := st.Note(context.Background(), "20-projects/LEAF/issues/LEAF-1.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.NoteRole(bob, issue); got != RoleNone {
		t.Errorf("issue of a nested closed project: %v", got)
	}
	acc, err := p.ProjectAccess(alice, "LEAF")
	if err != nil {
		t.Fatal(err)
	}
	if acc.Open || acc.InheritedFrom != "SEC" || len(acc.Members) != 1 {
		t.Fatalf("access = %+v", acc)
	}
}

// The first edit of an inherited list starts from it: nobody is locked out.
func TestDetachingInheritedAccessKeepsPeople(t *testing.T) {
	p, _ := treeFixture(t)
	alice := as("alice", RoleMember)
	acc, err := p.SetMember(alice, "SUB", "user", "carol", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if acc.InheritedFrom != "" || len(acc.Members) != 2 {
		t.Fatalf("access = %+v", acc)
	}
	if p.ProjectRole(alice, "SUB") != RoleAdmin || p.ProjectRole(as("carol", RoleMember), "SUB") != RoleMember {
		t.Fatal("people lost with the first edit")
	}
	// Carol reached SUB, and LEAF under it, but not SEC.
	if p.ProjectRole(as("carol", RoleMember), "LEAF") != RoleMember || p.ProjectRole(as("carol", RoleMember), "SEC") != RoleNone {
		t.Fatal("wrong subtree")
	}
}

// A move that changes who can see a project takes its admin.
func TestReparentChecks(t *testing.T) {
	p, _ := treeFixture(t)
	insider := With(context.Background(), &Principal{ID: "m", Username: "m", Role: RoleMember})
	if _, err := p.members.Set(context.Background(), "SEC", "user", "m", RoleMember, ""); err != nil {
		t.Fatal(err)
	}
	// A plain member of SEC cannot carry SUB out into the open.
	if err := p.CheckReparent(insider, "SUB", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("taking a project out of a closed parent: %v", err)
	}
	if err := p.CheckReparent(as("alice", RoleMember), "SUB", ""); err != nil {
		t.Fatalf("project admin: %v", err)
	}
	// Moving within the same governing list needs write access only.
	if err := p.CheckReparent(insider, "LEAF", "SEC"); err != nil {
		t.Fatalf("within one list: %v", err)
	}
	// A member of an open project cannot put it under a project they are not in.
	if err := p.CheckReparent(as("bob", RoleMember), "OPEN", "SEC"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("into a foreign project: %v", err)
	}
}

// Regression: the walk up stopped after 5 parents, so a deep page of a closed
// project read as open workspace material.
func TestDeepPageStaysInItsProject(t *testing.T) {
	st := index.NewMemory("")
	put := func(path, src string) {
		n, err := markdown.ParseNote(path, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		st.PutNote(context.Background(), n)
	}
	put("20-projects/sec.md", "---\nid: sec\ntype: project\nkey: SEC\n---\n")
	parent := "sec"
	const levels = 32
	for i := 0; i < levels; i++ {
		id := fmt.Sprintf("p%d", i)
		put("60-documents/"+id+".md", fmt.Sprintf("---\nid: %s\nparent: \"note:%s\"\ncontent_kind: document\n---\n", id, parent))
		parent = id
	}
	p := New(st, nil)
	if _, err := p.members.Set(context.Background(), "SEC", "user", "alice", RoleMember, ""); err != nil {
		t.Fatal(err)
	}
	deep, err := st.Note(context.Background(), fmt.Sprintf("60-documents/p%d.md", levels-1))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.NoteRole(as("bob", RoleMember), deep); got != RoleNone {
		t.Fatalf("outsider on a deep page: %v", got)
	}
	if got := p.NoteRole(as("alice", RoleMember), deep); got != RoleMember {
		t.Fatalf("member on a deep page: %v", got)
	}
}
