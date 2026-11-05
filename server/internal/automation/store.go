package automation

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/service"
	"github.com/villetacore/lumen/server/internal/state"
)

// Sealer protects rule definitions; *state.Store implements it.
type Sealer interface {
	Seal(ref string, plain []byte) ([]byte, error)
	Unseal(ref string, stored []byte) ([]byte, error)
}

// Store keeps rules, their log and the journal cursor in the operational database.
type Store struct {
	db   *state.DB
	seal Sealer
}

func NewStore(db *state.DB, seal Sealer) *Store { return &Store{db: db, seal: seal} }

var nowFunc = time.Now

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "a-" + hex.EncodeToString(b[:])
}

func kindOf(d *Definition) string {
	if d.Trigger.Type == TriggerSchedule {
		return "schedule"
	}
	return "event"
}

func (s *Store) sealed(id string, d *Definition) ([]byte, error) {
	blob, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	if s.seal == nil {
		return blob, nil
	}
	return s.seal.Seal("automation_rules:"+id, blob)
}

const ruleColumns = `id, project, enabled, definition, owner_id, owner, after_event, next_run, revision, created_at, updated_at`

func (s *Store) scan(row interface{ Scan(...any) error }) (*Rule, error) {
	r := &Rule{}
	var enabled int
	var blob []byte
	if err := row.Scan(&r.ID, &r.Project, &enabled, &blob, &r.OwnerID, &r.Owner, &r.afterEvent, &r.NextRun, &r.Revision, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("automation: %w", index.ErrNotFound)
		}
		return nil, err
	}
	r.Enabled = enabled != 0
	if s.seal != nil {
		var err error
		if blob, err = s.seal.Unseal("automation_rules:"+r.ID, blob); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(blob, &r.Definition); err != nil {
		return nil, fmt.Errorf("automation %s: %w", r.ID, err)
	}
	if r.Conditions == nil {
		r.Conditions = []Condition{}
	}
	return r, nil
}

func (s *Store) list(ctx context.Context, where string, args ...any) ([]*Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM automation_rules `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Rule{}
	for rows.Next() {
		r, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Project lists a project's rules, oldest first.
func (s *Store) Project(ctx context.Context, project string) ([]*Rule, error) {
	return s.list(ctx, `WHERE project = ? ORDER BY created_at, id`, project)
}

// Enabled lists switched-on rules of one kind.
func (s *Store) Enabled(ctx context.Context, kind string) ([]*Rule, error) {
	return s.list(ctx, `WHERE enabled = 1 AND kind = ? ORDER BY created_at, id`, kind)
}

// Get returns one rule.
func (s *Store) Get(ctx context.Context, id string) (*Rule, error) {
	return s.scan(s.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM automation_rules WHERE id = ?`, id))
}

// Insert stores a new rule.
func (s *Store) Insert(ctx context.Context, r *Rule) error {
	r.ID, r.Revision = newID(), 1
	r.CreatedAt = stamp(nowFunc())
	r.UpdatedAt = r.CreatedAt
	blob, err := s.sealed(r.ID, &r.Definition)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO automation_rules(id, project, enabled, kind, definition, owner_id, owner, after_event, next_run, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.Project, boolInt(r.Enabled), kindOf(&r.Definition), blob, r.OwnerID, r.Owner,
		r.afterEvent, r.NextRun, r.Revision, r.CreatedAt, r.UpdatedAt)
	return err
}

// Update saves an edited rule if nobody changed it since revision was read.
func (s *Store) Update(ctx context.Context, r *Rule, revision int64) error {
	blob, err := s.sealed(r.ID, &r.Definition)
	if err != nil {
		return err
	}
	r.UpdatedAt = stamp(nowFunc())
	res, err := s.db.ExecContext(ctx, `UPDATE automation_rules SET enabled = ?, kind = ?, definition = ?, owner_id = ?, owner = ?, after_event = ?,
		next_run = ?, revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?`,
		boolInt(r.Enabled), kindOf(&r.Definition), blob, r.OwnerID, r.Owner, r.afterEvent, r.NextRun, r.UpdatedAt, r.ID, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: правило изменили, обновите страницу", service.ErrConflict)
	}
	r.Revision = revision + 1
	return nil
}

// Delete removes a rule and its log.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM automation_runs WHERE rule_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM automation_rules WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetNextRun moves a schedule on, only from the value it was read with: a rule
// edited while it ran keeps the new schedule.
func (s *Store) SetNextRun(ctx context.Context, id, from, to string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE automation_rules SET next_run = ? WHERE id = ? AND next_run = ?`, to, id, from)
	return err
}

// Claim records that a rule starts on an issue for an occurrence. It reports false
// if that already happened: this is the guard against running actions twice.
func (s *Store) Claim(ctx context.Context, r *Rule, occurrence, issue string) (int64, bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO automation_runs(rule_id, project, occurrence, issue, status, started_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, r.ID, r.Project, occurrence, issue, StatusRunning, stamp(nowFunc()))
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, false, nil
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM automation_runs WHERE rule_id = ? AND occurrence = ? AND issue = ?`, r.ID, occurrence, issue).Scan(&id)
	return id, err == nil, err
}

// Finish closes a claimed run.
func (s *Store) Finish(ctx context.Context, id int64, status, errText string, actions, matched int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE automation_runs SET status = ?, error = ?, actions = ?, matched = ?, finished_at = ? WHERE id = ?`,
		status, errText, actions, matched, stamp(nowFunc()), id)
	return err
}

// Interrupted marks runs a restart cut short. Their actions may or may not have
// happened, so they are shown and never repeated blindly.
func (s *Store) Interrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE automation_runs SET status = ?, error = ?, finished_at = ? WHERE status = ?`,
		StatusUnknown, "сервер перезапустился во время выполнения; действия могли выполниться частично", stamp(nowFunc()), StatusRunning)
	return err
}

// RecentRuns counts a rule's runs on an issue since t, for the loop fuse.
func (s *Store) RecentRuns(ctx context.Context, ruleID, issue string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND issue = ? AND started_at >= ?`,
		ruleID, issue, stamp(since)).Scan(&n)
	return n, err
}

// Runs returns a project's log, newest first.
func (s *Store) Runs(ctx context.Context, project string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, rule_id, project, occurrence, issue, status, error, actions, matched, started_at, finished_at
		FROM automation_runs WHERE project = ? ORDER BY id DESC LIMIT ?`, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.RuleID, &r.Project, &r.Occurrence, &r.Issue, &r.Status, &r.Error, &r.Actions, &r.Matched, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// keepRuns is how much log a rule keeps. It is far above one scheduled run
// (maxScheduledIssues), so pruning cannot drop the guard of an unfinished one.
const keepRuns = 1000

// Prune trims a rule's log.
func (s *Store) Prune(ctx context.Context, ruleID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM automation_runs WHERE rule_id = ? AND id NOT IN
		(SELECT id FROM automation_runs WHERE rule_id = ? ORDER BY id DESC LIMIT ?)`, ruleID, ruleID, keepRuns)
	return err
}

const journalCursor = "journal"

// Cursor reads how far the journal has been worked off.
func (s *Store) Cursor(ctx context.Context) (int64, bool, error) {
	var v int64
	err := s.db.QueryRowContext(ctx, `SELECT value FROM automation_cursor WHERE name = ?`, journalCursor).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return v, err == nil, err
}

// SetCursor saves the position.
func (s *Store) SetCursor(ctx context.Context, v int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO automation_cursor(name, value) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value`, journalCursor, v)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
