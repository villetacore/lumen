// Package notify keeps in-app notifications (LUM-067). The tracker reports
// what happened; this package stores one row per recipient and event, and
// a repeated report of the same event is ignored.
package notify

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/state"
)

type Kind string

const (
	Assigned  Kind = "assigned"  // you became the assignee
	Mentioned Kind = "mentioned" // @you in a description or comment
	Commented Kind = "commented" // a comment on an issue you watch
	Status    Kind = "status"    // an issue you watch changed status
)

// Event is something that happened to an issue. Dedup must be the same for a
// retried request and different for a new occurrence.
type Event struct {
	Kind       Kind
	Issue      string
	Ref        string
	Actor      string
	Recipients []string
	Dedup      string
}

// Notification is one stored row.
type Notification struct {
	ID        int64  `json:"id"`
	Kind      Kind   `json:"kind"`
	Issue     string `json:"issue"`
	Ref       string `json:"ref,omitempty"`
	Actor     string `json:"actor,omitempty"`
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// Store writes and reads notifications. A nil *Store drops everything, so
// the tracker works without an operational database.
type Store struct {
	db      *state.DB
	publish func(recipient, issue string) // SSE nudge; optional
}

func New(db *state.DB, publish func(recipient, issue string)) *Store {
	return &Store{db: db, publish: publish}
}

var nowFunc = time.Now

// Emit stores the event for each recipient except the actor. It returns how
// many rows were new.
func (s *Store) Emit(ctx context.Context, e Event) (int, error) {
	if s == nil {
		return 0, nil
	}
	ts := nowFunc().UTC().Format(time.RFC3339Nano)
	added := 0
	for _, r := range unique(e.Recipients) {
		if r == "" || r == e.Actor {
			continue
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO notifications(recipient, kind, issue, ref, actor, dedup, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, r, string(e.Kind), e.Issue, e.Ref, e.Actor, e.Dedup+"|"+r, ts)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
			if s.publish != nil {
				s.publish(r, e.Issue)
			}
		}
	}
	return added, nil
}

// List returns a recipient's notifications, newest first.
func (s *Store) List(ctx context.Context, recipient string, unreadOnly bool, limit int) ([]Notification, error) {
	if s == nil || recipient == "" {
		return []Notification{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, kind, issue, ref, actor, created_at, read_at IS NOT NULL FROM notifications WHERE recipient = ?`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT ?`, recipient, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Kind, &n.Issue, &n.Ref, &n.Actor, &n.CreatedAt, &n.Read); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkRead marks the given ids (all when ids is empty) as read for recipient.
func (s *Store) MarkRead(ctx context.Context, recipient string, ids []int64) error {
	if s == nil || recipient == "" {
		return nil
	}
	ts := nowFunc().UTC().Format(time.RFC3339)
	if len(ids) == 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE recipient = ? AND read_at IS NULL`, ts, recipient)
		return err
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE id = ? AND recipient = ? AND read_at IS NULL`,
			ts, id, recipient); err != nil {
			return err
		}
	}
	return nil
}

var mentionRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_.@/-])@([\p{L}\p{N}_][\p{L}\p{N}_.-]*[\p{L}\p{N}_]|[\p{L}\p{N}_])`)

// Mentions finds @usernames in Markdown, skipping fenced and inline code and
// e-mail addresses.
func Mentions(text string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range mentionRe.FindAllStringSubmatch(stripCode(line), -1) {
			out = append(out, m[1])
		}
	}
	return unique(out)
}

func stripCode(line string) string {
	var b strings.Builder
	in := false
	for _, r := range line {
		if r == '`' {
			in = !in
			b.WriteRune(' ')
			continue
		}
		if in {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func unique(in []string) []string {
	set := map[string]bool{}
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
