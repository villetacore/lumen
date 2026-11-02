package policy

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Member is one row of a closed project's access list.
type Member struct {
	Project string `json:"project"`
	Kind    string `json:"kind"` // user | group
	Name    string `json:"name"`
	Role    string `json:"role"`
	AddedAt string `json:"added_at"`
	AddedBy string `json:"added_by,omitempty"`
}

// Members keeps project access lists in the operational store and caches
// them: the table is small and read on every request.
type Members struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string][]Member // by project; nil until loaded
}

// NewMembers works on db; a nil db keeps lists in memory only (tests and
// installations without a state store — every project is then open).
func NewMembers(db *sql.DB) *Members { return &Members{db: db} }

func (m *Members) load(ctx context.Context) (map[string][]Member, error) {
	m.mu.RLock()
	c := m.cache
	m.mu.RUnlock()
	if c != nil {
		return c, nil
	}
	c = map[string][]Member{}
	if m.db != nil {
		rows, err := m.db.QueryContext(ctx, `SELECT project, kind, name, role, added_at, added_by FROM project_members`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r Member
			if err := rows.Scan(&r.Project, &r.Kind, &r.Name, &r.Role, &r.AddedAt, &r.AddedBy); err != nil {
				return nil, err
			}
			c[r.Project] = append(c[r.Project], r)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	m.cache = c
	m.mu.Unlock()
	return c, nil
}

// List returns a project's members sorted by kind and name.
func (m *Members) List(ctx context.Context, project string) ([]Member, error) {
	c, err := m.load(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]Member{}, c[project]...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind > out[j].Kind // users first
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Set adds a member or changes their role.
func (m *Members) Set(ctx context.Context, project, kind, name string, role Role, by string) (Member, error) {
	kind, name = strings.TrimSpace(kind), strings.TrimPrefix(strings.TrimSpace(name), "/")
	if (kind != "user" && kind != "group") || name == "" || role == RoleNone {
		return Member{}, fmt.Errorf("member must be user:<name> or group:<name> with a role")
	}
	r := Member{Project: project, Kind: kind, Name: name, Role: role.String(),
		AddedAt: time.Now().UTC().Format(time.RFC3339), AddedBy: by}
	if m.db != nil {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO project_members(project, kind, name, role, added_at, added_by)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(project, kind, name) DO UPDATE SET role = excluded.role`,
			r.Project, r.Kind, r.Name, r.Role, r.AddedAt, r.AddedBy); err != nil {
			return Member{}, err
		}
		m.invalidate()
		return r, nil
	}
	c, err := m.load(ctx)
	if err != nil {
		return Member{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := c[project][:0:0]
	for _, x := range c[project] {
		if !(x.Kind == kind && x.Name == name) {
			list = append(list, x)
		}
	}
	c[project] = append(list, r)
	return r, nil
}

// Remove deletes a member; removing the last one opens the project.
func (m *Members) Remove(ctx context.Context, project, kind, name string) error {
	if m.db != nil {
		if _, err := m.db.ExecContext(ctx, `DELETE FROM project_members WHERE project = ? AND kind = ? AND name = ?`,
			project, kind, name); err != nil {
			return err
		}
		m.invalidate()
		return nil
	}
	c, err := m.load(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []Member
	for _, x := range c[project] {
		if !(x.Kind == kind && x.Name == name) {
			list = append(list, x)
		}
	}
	c[project] = list
	return nil
}

func (m *Members) invalidate() {
	m.mu.Lock()
	m.cache = nil
	m.mu.Unlock()
}
