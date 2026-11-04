package policy

import (
	"context"
	"errors"
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
