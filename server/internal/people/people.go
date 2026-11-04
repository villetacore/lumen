// Package people is the directory of users and groups Lumen has seen through
// the identity provider (LUM-066). Keycloak owns the accounts; Lumen keeps a
// copy so it can offer assignees and project members by name.
package people

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/villetacore/lumen/server/internal/policy"

	"github.com/villetacore/lumen/server/internal/state"
)

// User is a directory entry.
type User struct {
	ID        string   `json:"-"`
	Username  string   `json:"username"`
	Name      string   `json:"name"`
	Email     string   `json:"email,omitempty"`
	Groups    []string `json:"groups"`
	Role      string   `json:"role"`
	FirstSeen string   `json:"first_seen"`
	LastSeen  string   `json:"last_seen"`
}

// Group is a group name with how many known users are in it.
type Group struct {
	Name  string `json:"name"`
	Users int    `json:"users"`
}

// Directory reads and refreshes the users table.
type Directory struct {
	db    *state.DB
	every time.Duration

	mu   sync.Mutex
	seen map[string]seenEntry // by subject: skip writes for repeat requests
}

type seenEntry struct {
	at  time.Time
	sig string // what was written, so a changed group list is written at once
}

// refreshEvery limits writes: a user making many requests is recorded once in
// this period unless their name, role or groups change.
const refreshEvery = 5 * time.Minute

func New(db *state.DB) *Directory {
	return &Directory{db: db, every: refreshEvery, seen: map[string]seenEntry{}}
}

var nowFunc = time.Now

// Touch records a signed-in person. Service principals (local mode, static
// token) are not people and are skipped.
func (d *Directory) Touch(ctx context.Context, p *policy.Principal, email string) error {
	if d == nil || p == nil || p.ID == "" || p.Username == "" || p.ID == "local" || p.ID == "token" {
		return nil
	}
	groups := normalize(p.Groups)
	sig := strings.Join([]string{p.Username, p.Name, email, p.Role.String(), strings.Join(groups, ",")}, "\x00")
	now := nowFunc()
	d.mu.Lock()
	prev, ok := d.seen[p.ID]
	if ok && prev.sig == sig && now.Sub(prev.at) < d.every {
		d.mu.Unlock()
		return nil
	}
	d.seen[p.ID] = seenEntry{at: now, sig: sig}
	d.mu.Unlock()

	ts := now.UTC().Format(time.RFC3339)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A username moved to another subject (account recreated in Keycloak):
	// the newer subject takes it over.
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE username = ? AND id <> ?`, p.Username, p.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, username, name, email, role, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET username = excluded.username, name = excluded.name,
			email = excluded.email, role = excluded.role, last_seen = excluded.last_seen`,
		p.ID, p.Username, p.Name, email, p.Role.String(), ts, ts); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_groups WHERE user_id = ?`, p.ID); err != nil {
		return err
	}
	for _, g := range groups {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_groups(user_id, group_name) VALUES (?, ?)`, p.ID, g); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		d.mu.Lock()
		delete(d.seen, p.ID) // retry on the next request
		d.mu.Unlock()
		return err
	}
	return nil
}

func normalize(groups []string) []string {
	set := map[string]bool{}
	var out []string
	for _, g := range groups {
		g = strings.TrimPrefix(strings.TrimSpace(g), "/")
		if g != "" && !set[g] {
			set[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

// Users lists people, matching q against username, name and email. The
// directory is small, so matching happens here: SQLite's lower() and LIKE
// fold ASCII only, and names are often Cyrillic.
func (d *Directory) Users(ctx context.Context, q string) ([]User, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	rows, err := d.db.QueryContext(ctx, `SELECT id, username, name, email, role, first_seen, last_seen FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	byID := map[string]int{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.Role, &u.FirstSeen, &u.LastSeen); err != nil {
			return nil, err
		}
		if q != "" && !strings.Contains(strings.ToLower(u.Username+"\x00"+u.Name+"\x00"+u.Email), q) {
			continue
		}
		u.Groups = []string{}
		byID[u.ID] = len(out)
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(display(out[i])) < strings.ToLower(display(out[j])) })
	for i, u := range out {
		byID[u.ID] = i
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	g, err := d.db.QueryContext(ctx, `SELECT user_id, group_name FROM user_groups ORDER BY group_name`)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	for g.Next() {
		var id, name string
		if err := g.Scan(&id, &name); err != nil {
			return nil, err
		}
		if i, ok := byID[id]; ok {
			out[i].Groups = append(out[i].Groups, name)
		}
	}
	return out, g.Err()
}

// Groups lists groups of known users with their size.
func (d *Directory) Groups(ctx context.Context) ([]Group, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT group_name, COUNT(*) FROM user_groups GROUP BY group_name ORDER BY group_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Users); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func display(u User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}
