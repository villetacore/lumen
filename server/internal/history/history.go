// Package history records how issues change (LUM-068). It compares each new
// version of an issue file with the last recorded snapshot, so it sees edits
// made in Lumen and outside it (Obsidian, git, Nextcloud) alike, and a rescan
// or restart that finds the same file records nothing.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/markdown"
	"github.com/villetacore/lumen/server/internal/policy"

	"github.com/villetacore/lumen/server/internal/state"
)

// Sealer protects content-bearing values; *state.Store implements it.
type Sealer interface {
	Seal(ref string, plain []byte) ([]byte, error)
	Unseal(ref string, stored []byte) ([]byte, error)
}

// Event is one recorded change.
type Event struct {
	ID      int64  `json:"id"`
	Issue   string `json:"issue"`
	Project string `json:"project,omitempty"`
	Kind    string `json:"kind"` // created | changed | deleted
	Field   string `json:"field,omitempty"`
	Old     string `json:"old,omitempty"`
	New     string `json:"new,omitempty"`
	Actor   string `json:"actor,omitempty"`
	Source  string `json:"source"` // lumen | automation | external
	At      string `json:"at"`
}

// Journal writes issue history to the operational store.
type Journal struct {
	db   *state.DB
	seal Sealer
	mu   sync.Mutex // one comparison at a time: snapshot read and write stay together
}

func New(db *state.DB, seal Sealer) *Journal { return &Journal{db: db, seal: seal} }

var nowFunc = time.Now

// Fields are the tracked properties. Rank is left out: every drag on the
// board would flood the history without telling anyone anything.
func Fields(is *domain.Issue) map[string]string {
	f := map[string]string{
		"title": is.Title, "status": is.Status, "issue_type": is.Type, "priority": is.Priority,
		"assignee": is.Assignee, "reporter": is.Reporter, "epic": is.Epic, "parent": is.Parent,
		"sprint": is.Sprint, "estimate": is.Estimate, "description": is.Description,
		"watchers": strings.Join(is.Watchers, ", "), "labels": strings.Join(is.Labels, ", "),
	}
	if !is.Start.IsZero() {
		f["start"] = is.Start.String()
	}
	if !is.Due.IsZero() {
		f["due"] = is.Due.String()
	}
	if is.Points != 0 {
		f["points"] = strconv.FormatFloat(is.Points, 'f', -1, 64)
	}
	f["comments"] = strconv.Itoa(len(is.Comments))
	for k, v := range f {
		if v == "" {
			delete(f, k)
		}
	}
	return f
}

// Observe records a new version of a note. Notes that are not issues are
// ignored. baseline is set for the startup sweep: an issue seen for the first
// time is then taken as it is, without a "created" event, because its past is
// unknown. The actor comes from ctx: no principal means an edit outside Lumen.
func (j *Journal) Observe(ctx context.Context, n *domain.Note, baseline bool) error {
	if j == nil || n == nil || n.Type != domain.TypeIssue {
		return nil
	}
	is := markdown.ParseIssue(n)
	j.mu.Lock()
	defer j.mu.Unlock()

	prev, prevHash, err := j.snapshot(ctx, is.Key)
	if err != nil {
		return err
	}
	if prevHash == n.Hash {
		return nil // same file: a rescan, a restart, or our own write seen twice
	}
	actor, source := actorSource(ctx)
	now := nowFunc().UTC().Format(time.RFC3339Nano)
	cur := Fields(is)

	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	add := func(kind, field, old, new string) error {
		o, err := j.sealValue(is.Key, field, old)
		if err != nil {
			return err
		}
		nw, err := j.sealValue(is.Key, field, new)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO issue_events(issue, project, kind, field, old, new, actor, source, hash, at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, is.Key, is.Project, kind, field, o, nw, actor, source, n.Hash, now)
		return err
	}
	switch {
	case prev == nil && !baseline:
		if err := add("created", "", "", ""); err != nil {
			return err
		}
	case prev != nil:
		keys := make([]string, 0, len(cur)+len(prev))
		seen := map[string]bool{}
		for k := range cur {
			keys, seen[k] = append(keys, k), true
		}
		for k := range prev {
			if !seen[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if prev[k] != cur[k] {
				if err := add("changed", k, prev[k], cur[k]); err != nil {
					return err
				}
			}
		}
	}
	blob, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	if blob, err = j.sealBytes("issue_snapshots:"+is.Key, blob); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO issue_snapshots(issue, path, hash, fields, seen) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(issue) DO UPDATE SET path = excluded.path, hash = excluded.hash, fields = excluded.fields, seen = excluded.seen`,
		is.Key, n.Path, n.Hash, blob, now); err != nil {
		return err
	}
	return tx.Commit()
}

// Removed records that the issue file at path is gone (trash, deletion,
// rename). Its snapshot is dropped, so a file that reappears counts as created.
func (j *Journal) Removed(ctx context.Context, path string) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var key string
	err := j.db.QueryRowContext(ctx, `SELECT issue FROM issue_snapshots WHERE path = ?`, path).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	actor, source := actorSource(ctx)
	project, _, _ := markdown.SplitIssueKey(key)
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO issue_events(issue, project, kind, actor, source, at) VALUES (?, ?, 'deleted', ?, ?, ?)`,
		key, project, actor, source, nowFunc().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_snapshots WHERE issue = ?`, key); err != nil {
		return err
	}
	return tx.Commit()
}

// actorSource says who made a change: no principal means an edit outside Lumen.
func actorSource(ctx context.Context) (actor, source string) {
	p := policy.From(ctx)
	switch {
	case p == nil:
		return "", "external"
	case p.Via != "":
		return p.Username, "automation"
	}
	return p.Username, "lumen"
}

// After returns events newer than id, oldest first: the feed automations read.
func (j *Journal) After(ctx context.Context, id int64, limit int) ([]Event, error) {
	return j.query(ctx, `WHERE id > ? ORDER BY id LIMIT ?`, id, limit)
}

// LastID is the newest event id, 0 for an empty journal.
func (j *Journal) LastID(ctx context.Context) (int64, error) {
	if j == nil {
		return 0, nil
	}
	var id sql.NullInt64
	err := j.db.QueryRowContext(ctx, `SELECT MAX(id) FROM issue_events`).Scan(&id)
	return id.Int64, err
}

// Events returns an issue's history, oldest first.
func (j *Journal) Events(ctx context.Context, issue string) ([]Event, error) {
	return j.query(ctx, `WHERE issue = ? ORDER BY id`, issue)
}

// ProjectEvents returns a project's history in a time range (for reports).
func (j *Journal) ProjectEvents(ctx context.Context, project string, from, to time.Time) ([]Event, error) {
	return j.query(ctx, `WHERE project = ? AND at >= ? AND at < ? ORDER BY id`, project,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
}

func (j *Journal) query(ctx context.Context, where string, args ...any) ([]Event, error) {
	out := []Event{}
	if j == nil {
		return out, nil
	}
	rows, err := j.db.QueryContext(ctx, `SELECT id, issue, project, kind, field, old, new, actor, source, at FROM issue_events `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		var old, nw []byte
		if err := rows.Scan(&e.ID, &e.Issue, &e.Project, &e.Kind, &e.Field, &old, &nw, &e.Actor, &e.Source, &e.At); err != nil {
			return nil, err
		}
		if e.Old, err = j.unsealValue(e.Issue, e.Field, old); err != nil {
			return nil, err
		}
		if e.New, err = j.unsealValue(e.Issue, e.Field, nw); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (j *Journal) snapshot(ctx context.Context, key string) (map[string]string, string, error) {
	var hash string
	var blob []byte
	err := j.db.QueryRowContext(ctx, `SELECT hash, fields FROM issue_snapshots WHERE issue = ?`, key).Scan(&hash, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if blob, err = j.unsealBytes("issue_snapshots:"+key, blob); err != nil {
		return nil, "", err
	}
	fields := map[string]string{}
	if err := json.Unmarshal(blob, &fields); err != nil {
		return nil, "", fmt.Errorf("snapshot %s: %w", key, err)
	}
	return fields, hash, nil
}

// Values are bound to their issue and field, so a sealed value cannot be
// moved to another row and opened there.
func valueRef(issue, field string) string { return "issue_events:" + issue + ":" + field }

func (j *Journal) sealValue(issue, field, v string) ([]byte, error) {
	if v == "" {
		return nil, nil
	}
	return j.sealBytes(valueRef(issue, field), []byte(v))
}

func (j *Journal) unsealValue(issue, field string, b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	plain, err := j.unsealBytes(valueRef(issue, field), b)
	return string(plain), err
}

func (j *Journal) sealBytes(ref string, b []byte) ([]byte, error) {
	if j.seal == nil {
		return b, nil
	}
	return j.seal.Seal(ref, b)
}

func (j *Journal) unsealBytes(ref string, b []byte) ([]byte, error) {
	if j.seal == nil {
		return b, nil
	}
	return j.seal.Unseal(ref, b)
}
