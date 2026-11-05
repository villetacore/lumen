package policy

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/markdown"
)

// Policy answers access questions. It reads the unfiltered index to work out
// which project a file belongs to (ADR-0012 §4) and the member lists to know
// who is in it. A nil *Policy allows everything: background jobs and tests
// that build services without one.
type Policy struct {
	raw     index.Store
	members *Members
}

func New(raw index.Store, members *Members) *Policy {
	if members == nil {
		members = NewMembers(nil)
	}
	return &Policy{raw: raw, members: members}
}

// Members exposes the access lists for the membership API.
func (p *Policy) Members() *Members { return p.members }

// projects is a per-call view of tracker projects: who owns which folder.
type projects struct {
	byPath   map[string]string // project note path → key
	prefixes []prefix
}

type prefix struct{ dir, key string }

func (p *Policy) projects(ctx context.Context) (*projects, error) {
	notes, _, err := p.raw.Notes(ctx, domain.NoteQuery{Type: domain.TypeProject})
	if err != nil {
		return nil, err
	}
	ps := &projects{byPath: map[string]string{}}
	for _, n := range notes {
		tp, ok := markdown.ParseProject(n)
		if !ok {
			continue
		}
		ps.byPath[n.Path] = tp.Key
		dir := path.Dir(n.Path)
		if dir == "." {
			dir = ""
		} else {
			dir += "/"
		}
		ps.prefixes = append(ps.prefixes, prefix{dir: dir + tp.Key + "/", key: tp.Key})
	}
	return ps, nil
}

func (ps *projects) ofPath(p string) string {
	for _, pr := range ps.prefixes {
		if strings.HasPrefix(p, pr.dir) {
			return pr.key
		}
	}
	return ""
}

// ofNote finds the project a note belongs to, or "" for workspace material.
func (p *Policy) ofNote(ctx context.Context, ps *projects, n *domain.Note, depth int) string {
	fm := &markdown.Frontmatter{Fields: n.Frontmatter}
	switch n.Type {
	case domain.TypeIssue:
		if k := strings.ToUpper(fm.Str("project")); k != "" {
			return k
		}
		if k, _, ok := markdown.SplitIssueKey(markdown.ParseIssue(n).Key); ok {
			return k
		}
	case domain.TypeSprint:
		if sp := markdown.ParseSprint(n); sp.Project != "" {
			return sp.Project
		}
	case domain.TypeProject:
		if k, ok := ps.byPath[n.Path]; ok {
			return k
		}
	}
	if k := ps.ofPath(n.Path); k != "" {
		return k
	}
	if depth <= 0 {
		return ""
	}
	kind, id, ok := strings.Cut(fm.Str("parent"), ":")
	if !ok || id == "" {
		return ""
	}
	switch kind {
	case "note":
		if parent, err := p.raw.Note(ctx, id); err == nil && parent.Path != n.Path {
			return p.ofNote(ctx, ps, parent, depth-1)
		}
	case "task":
		if t, err := p.raw.Task(ctx, id); err == nil {
			if parent, err := p.raw.Note(ctx, t.NotePath); err == nil && parent.Path != n.Path {
				return p.ofNote(ctx, ps, parent, depth-1)
			}
		}
	}
	return ""
}

// parentDepth bounds the walk up a page tree, so a cycle edited in by hand ends.
// It must exceed the deepest tree the service allows (service.maxPageDepth): a page
// beyond it would stop counting as part of its project.
const parentDepth = 64

// ProjectParents maps every tracker project to its parent's key, whatever the
// caller may see: access and cycle checks need the whole chain.
func (p *Policy) ProjectParents(ctx context.Context) (map[string]string, error) {
	notes, _, err := p.raw.Notes(ctx, domain.NoteQuery{Type: domain.TypeProject})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, n := range notes {
		if tp, ok := markdown.ParseProject(n); ok {
			out[tp.Key] = tp.Parent
		}
	}
	return out, nil
}

// accessList finds the members that govern a project: its own, else those of the
// nearest ancestor that has any, so a subproject of a closed project is closed
// too (ADR-0020 §2). source is the project the list belongs to, "" when open.
func (p *Policy) accessList(ctx context.Context, project string, parents map[string]string) ([]Member, string, error) {
	list, err := p.members.List(ctx, project)
	if err != nil || len(list) > 0 {
		return list, project, err
	}
	if parents == nil {
		if parents, err = p.ProjectParents(ctx); err != nil {
			return nil, "", err
		}
	}
	seen := map[string]bool{project: true}
	for k := parents[project]; k != "" && !seen[k]; k = parents[k] {
		seen[k] = true
		list, err := p.members.List(ctx, k)
		if err != nil {
			return nil, "", err
		}
		if len(list) > 0 {
			return list, k, nil
		}
	}
	return nil, "", nil
}

// CheckReparent decides whether a project may move under newParent. Whoever moves
// it needs write access there. When the move changes which list governs the
// project, it changes who can see it, so that takes project admin.
func (p *Policy) CheckReparent(ctx context.Context, project, newParent string) error {
	if p == nil {
		return nil
	}
	if newParent != "" {
		if err := p.CheckProject(ctx, newParent, RoleMember); err != nil {
			return err
		}
	}
	parents, err := p.ProjectParents(ctx)
	if err != nil {
		return err
	}
	_, before, err := p.accessList(ctx, project, parents)
	if err != nil {
		return err
	}
	parents[project] = newParent
	_, after, err := p.accessList(ctx, project, parents)
	if err != nil {
		return err
	}
	if before != after {
		return p.CheckProject(ctx, project, RoleAdmin)
	}
	return p.CheckProject(ctx, project, RoleMember)
}

// roleIn is the principal's effective role in a project ("" — workspace).
func (p *Policy) roleIn(ctx context.Context, pr *Principal, project string) (Role, error) {
	if pr == nil {
		return RoleNone, nil
	}
	if project == "" || pr.Role == RoleAdmin {
		return pr.Role, nil
	}
	list, _, err := p.accessList(ctx, project, nil)
	if err != nil {
		return RoleNone, err
	}
	if len(list) == 0 {
		return pr.Role, nil // open project
	}
	best := RoleNone
	groups := map[string]bool{}
	for _, g := range pr.Groups {
		groups[strings.TrimPrefix(g, "/")] = true
	}
	for _, m := range list {
		match := (m.Kind == "user" && (m.Name == pr.Username || m.Name == pr.ID)) ||
			(m.Kind == "group" && groups[m.Name])
		if r, _ := ParseRole(m.Role); match && r > best {
			best = r
		}
	}
	return best, nil
}

// ProjectRole is the caller's role in a tracker project.
func (p *Policy) ProjectRole(ctx context.Context, project string) Role {
	if p == nil {
		return RoleAdmin
	}
	r, err := p.roleIn(ctx, From(ctx), strings.ToUpper(project))
	if err != nil {
		return RoleNone
	}
	return r
}

// NoteRole is the caller's role for a note.
func (p *Policy) NoteRole(ctx context.Context, n *domain.Note) Role {
	if p == nil {
		return RoleAdmin
	}
	ps, err := p.projects(ctx)
	if err != nil {
		return RoleNone
	}
	r, err := p.roleIn(ctx, From(ctx), p.ofNote(ctx, ps, n, parentDepth))
	if err != nil {
		return RoleNone
	}
	return r
}

func deny(need Role, what string) error {
	return fmt.Errorf("%w: нужна роль %s для %s", ErrForbidden, need, what)
}

// CheckNote requires role need on a note.
func (p *Policy) CheckNote(ctx context.Context, n *domain.Note, need Role) error {
	if p.NoteRole(ctx, n) < need {
		return deny(need, n.Path)
	}
	return nil
}

// CheckProject requires role need in a tracker project.
func (p *Policy) CheckProject(ctx context.Context, project string, need Role) error {
	if p.ProjectRole(ctx, project) < need {
		return deny(need, "проекта "+project)
	}
	return nil
}

// CheckWorkspace requires a workspace role (integrations, passwords, admin).
func (p *Policy) CheckWorkspace(ctx context.Context, need Role) error {
	if p == nil {
		return nil
	}
	pr := From(ctx)
	if pr == nil || pr.Role < need {
		return deny(need, "пространства")
	}
	return nil
}

// CheckCreate requires write access where a new file will appear: the
// project owning its folder, else the project of its parent ("note:…" or
// "task:…"), else the workspace.
func (p *Policy) CheckCreate(ctx context.Context, rel, parent string) error {
	if p == nil {
		return nil
	}
	ps, err := p.projects(ctx)
	if err != nil {
		return err
	}
	project := ps.ofPath(rel)
	if project == "" && parent != "" {
		probe := &domain.Note{Path: rel, Frontmatter: map[string]any{"parent": parent}}
		project = p.ofNote(ctx, ps, probe, parentDepth)
	}
	r, err := p.roleIn(ctx, From(ctx), project)
	if err != nil {
		return err
	}
	if r < RoleMember {
		return deny(RoleMember, rel)
	}
	return nil
}

// VisiblePath reports whether the caller may learn about a change at path.
// A deleted file is judged by its folder alone.
func (p *Policy) VisiblePath(ctx context.Context, rel string) bool {
	if p == nil || rel == "" {
		return true
	}
	if n, err := p.raw.Note(ctx, rel); err == nil {
		return p.NoteRole(ctx, n) >= RoleViewer
	}
	ps, err := p.projects(ctx)
	if err != nil {
		return false
	}
	r, err := p.roleIn(ctx, From(ctx), ps.ofPath(rel))
	return err == nil && r >= RoleViewer
}

// reader decides visibility for many notes in one request.
type reader struct {
	p     *Policy
	ctx   context.Context
	pr    *Principal
	ps    *projects
	roles map[string]Role
	all   bool // fast path: nothing is hidden from this caller
}

func (p *Policy) reader(ctx context.Context) (*reader, error) {
	pr := From(ctx)
	r := &reader{p: p, ctx: ctx, pr: pr, roles: map[string]Role{}}
	if pr == nil {
		return r, nil
	}
	if pr.Role == RoleAdmin {
		r.all = true
		return r, nil
	}
	ps, err := p.projects(ctx)
	if err != nil {
		return nil, err
	}
	r.ps = ps
	if pr.Role >= RoleViewer {
		closed := false
		for _, pf := range ps.prefixes {
			if list, err := p.members.List(ctx, pf.key); err != nil {
				return nil, err
			} else if len(list) > 0 {
				closed = true
				break
			}
		}
		r.all = !closed
	}
	return r, nil
}

func (r *reader) canSee(n *domain.Note) bool {
	if r.all {
		return true
	}
	if r.pr == nil || r.ps == nil {
		return false
	}
	project := r.p.ofNote(r.ctx, r.ps, n, parentDepth)
	role, ok := r.roles[project]
	if !ok {
		role, _ = r.p.roleIn(r.ctx, r.pr, project)
		r.roles[project] = role
	}
	return role >= RoleViewer
}

// Access describes a project's access list as the caller may see it.
type Access struct {
	Open    bool     `json:"open"` // no members: the workspace role applies
	MyRole  string   `json:"my_role"`
	Members []Member `json:"items"`
	// InheritedFrom is the ancestor whose list applies; empty for the project's own.
	InheritedFrom string `json:"inherited_from,omitempty"`
}

// ProjectAccess lists members; any reader of the project may see who is in it.
func (p *Policy) ProjectAccess(ctx context.Context, project string) (Access, error) {
	if err := p.CheckProject(ctx, project, RoleViewer); err != nil {
		return Access{}, err
	}
	list, source, err := p.accessList(ctx, project, nil)
	if err != nil {
		return Access{}, err
	}
	acc := Access{Open: len(list) == 0, MyRole: p.ProjectRole(ctx, project).String(), Members: list}
	if source != project {
		acc.InheritedFrom = source
	}
	return acc, nil
}

// detach gives a project that inherits its access a list of its own, starting
// from the inherited one, so a first edit cannot lock out everyone else.
func (p *Policy) detach(ctx context.Context, project, by string) error {
	list, source, err := p.accessList(ctx, project, nil)
	if err != nil || source == project {
		return err
	}
	for _, m := range list {
		r, ok := ParseRole(m.Role)
		if !ok {
			continue
		}
		if _, err := p.members.Set(ctx, project, m.Kind, m.Name, r, by); err != nil {
			return err
		}
	}
	return nil
}

// SetMember adds or changes a member; it needs admin in the project. An open
// project has no project admins yet, so only a workspace admin can close it.
func (p *Policy) SetMember(ctx context.Context, project, kind, name string, role Role) (Access, error) {
	if err := p.CheckProject(ctx, project, RoleAdmin); err != nil {
		return Access{}, err
	}
	by := ""
	if pr := From(ctx); pr != nil {
		by = pr.Username
	}
	if err := p.detach(ctx, project, by); err != nil {
		return Access{}, err
	}
	if _, err := p.members.Set(ctx, project, kind, name, role, by); err != nil {
		return Access{}, err
	}
	return p.ProjectAccess(ctx, project)
}

// RemoveMember deletes a member; removing the last one reopens the project.
func (p *Policy) RemoveMember(ctx context.Context, project, kind, name string) (Access, error) {
	if err := p.CheckProject(ctx, project, RoleAdmin); err != nil {
		return Access{}, err
	}
	by := ""
	if pr := From(ctx); pr != nil {
		by = pr.Username
	}
	if err := p.detach(ctx, project, by); err != nil {
		return Access{}, err
	}
	if err := p.members.Remove(ctx, project, kind, name); err != nil {
		return Access{}, err
	}
	if p.ProjectRole(ctx, project) < RoleViewer {
		return Access{Open: false}, nil // the caller just removed themselves
	}
	return p.ProjectAccess(ctx, project)
}
