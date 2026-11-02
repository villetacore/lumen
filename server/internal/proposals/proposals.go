// Package proposals keeps changes an AI client asked for until the person
// confirms or rejects them in Lumen (ADR-0013 §6). The state machine is
// pending → applying → applied | failed, or pending → rejected | expired |
// stale. Only the requester can move a proposal on, and only once.
package proposals

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound = errors.New("proposal not found")
	// ErrState covers every refused transition: not yours, already decided,
	// expired, or replayed with different arguments.
	ErrState = errors.New("proposal cannot be used")
)

// TTL is how long a proposal waits for a decision.
const TTL = 24 * time.Hour

type State string

const (
	Pending  State = "pending"
	Applying State = "applying"
	Applied  State = "applied"
	Rejected State = "rejected"
	Expired  State = "expired"
	Stale    State = "stale"
	Failed   State = "failed"
)

// Proposal is one requested change.
type Proposal struct {
	ID          string          `json:"id"`
	Requester   string          `json:"requester"`
	PrincipalID string          `json:"-"`
	Kind        string          `json:"kind"`
	Target      string          `json:"target,omitempty"`
	BaseHash    string          `json:"-"`
	Args        json.RawMessage `json:"args"`
	Preview     string          `json:"preview"`
	State       State           `json:"state"`
	Result      string          `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	RequestID   string          `json:"request_id,omitempty"`
	CreatedAt   string          `json:"created_at"`
	ExpiresAt   string          `json:"expires_at"`
	DecidedAt   string          `json:"decided_at,omitempty"`
}

// Sealer protects arguments and previews; *state.Store implements it.
type Sealer interface {
	Seal(ref string, plain []byte) ([]byte, error)
	Unseal(ref string, stored []byte) ([]byte, error)
}

type Store struct {
	db   *sql.DB
	seal Sealer
}

func New(db *sql.DB, seal Sealer) *Store { return &Store{db: db, seal: seal} }

var nowFunc = time.Now

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "p-" + hex.EncodeToString(b[:])
}

// Create stores a pending proposal. With a request_id already used by the
// same requester it returns the earlier proposal when the arguments match,
// and ErrState when they do not — a retry must not turn into another change.
func (s *Store) Create(ctx context.Context, p Proposal) (Proposal, error) {
	if p.RequestID != "" {
		if prev, err := s.byRequest(ctx, p.Requester, p.RequestID); err == nil {
			if prev.Kind != p.Kind || prev.Target != p.Target || string(prev.Args) != string(p.Args) {
				return Proposal{}, fmt.Errorf("%w: request_id %q was used for another change", ErrState, p.RequestID)
			}
			return prev, nil
		} else if !errors.Is(err, ErrNotFound) {
			return Proposal{}, err
		}
	}
	now := nowFunc().UTC()
	p.ID, p.State = newID(), Pending
	p.CreatedAt, p.ExpiresAt = now.Format(time.RFC3339), now.Add(TTL).Format(time.RFC3339)
	args, err := s.sealed(p.ID, "args", p.Args)
	if err != nil {
		return Proposal{}, err
	}
	preview, err := s.sealed(p.ID, "preview", []byte(p.Preview))
	if err != nil {
		return Proposal{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO proposals(id, requester, principal_id, kind, target, base_hash, args, preview,
		state, request_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Requester, p.PrincipalID, p.Kind, p.Target, p.BaseHash, args, preview, string(p.State), p.RequestID,
		p.CreatedAt, p.ExpiresAt); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

const columns = `id, requester, principal_id, kind, target, base_hash, args, preview, state, result, error, request_id,
	created_at, expires_at, decided_at`

func (s *Store) scan(row interface{ Scan(...any) error }) (Proposal, error) {
	var p Proposal
	var args, preview []byte
	var state string
	if err := row.Scan(&p.ID, &p.Requester, &p.PrincipalID, &p.Kind, &p.Target, &p.BaseHash, &args, &preview, &state,
		&p.Result, &p.Error, &p.RequestID, &p.CreatedAt, &p.ExpiresAt, &p.DecidedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, ErrNotFound
		}
		return p, err
	}
	p.State = State(state)
	a, err := s.opened(p.ID, "args", args)
	if err != nil {
		return p, err
	}
	pv, err := s.opened(p.ID, "preview", preview)
	if err != nil {
		return p, err
	}
	p.Args, p.Preview = a, string(pv)
	if p.State == Pending && p.ExpiresAt < nowFunc().UTC().Format(time.RFC3339) {
		p.State = Expired // reported as expired; stored on the next transition attempt
	}
	return p, nil
}

func (s *Store) byRequest(ctx context.Context, requester, requestID string) (Proposal, error) {
	return s.scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM proposals WHERE requester = ? AND request_id = ?`, requester, requestID))
}

// Get returns a proposal of requester; someone else's id is "not found".
func (s *Store) Get(ctx context.Context, requester, id string) (Proposal, error) {
	return s.scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM proposals WHERE id = ? AND requester = ?`, id, requester))
}

// List returns requester's proposals, newest first; pendingOnly hides decided ones.
func (s *Store) List(ctx context.Context, requester string, pendingOnly bool) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM proposals WHERE requester = ? ORDER BY created_at DESC, id DESC LIMIT 200`, requester)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proposal{}
	for rows.Next() {
		p, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		if !pendingOnly || p.State == Pending {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// Begin moves a pending proposal to applying for the person who asked for it.
// The conditional update makes a double click or a second tab lose the race
// instead of applying twice.
func (s *Store) Begin(ctx context.Context, requester, principalID, id string) (Proposal, error) {
	p, err := s.Get(ctx, requester, id)
	if err != nil {
		return p, err
	}
	if p.PrincipalID != principalID {
		return p, fmt.Errorf("%w: подтвердить может только тот, кто попросил", ErrState)
	}
	if p.State == Expired {
		s.finish(ctx, id, Pending, Expired, "", "")
		return p, fmt.Errorf("%w: срок подтверждения истёк", ErrState)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE proposals SET state = ?, decided_at = ? WHERE id = ? AND state = ?`,
		string(Applying), nowFunc().UTC().Format(time.RFC3339), id, string(Pending))
	if err != nil {
		return p, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return p, fmt.Errorf("%w: предложение уже %s", ErrState, p.State)
	}
	p.State = Applying
	return p, nil
}

// Finish records the outcome of an applying proposal.
func (s *Store) Finish(ctx context.Context, id string, state State, result, errText string) error {
	return s.finish(ctx, id, Applying, state, result, errText)
}

// Reject closes a pending proposal without applying it.
func (s *Store) Reject(ctx context.Context, requester, principalID, id string) (Proposal, error) {
	p, err := s.Get(ctx, requester, id)
	if err != nil {
		return p, err
	}
	if p.PrincipalID != principalID {
		return p, fmt.Errorf("%w: отклонить может только тот, кто попросил", ErrState)
	}
	if err := s.finish(ctx, id, Pending, Rejected, "", ""); err != nil {
		return p, err
	}
	return s.Get(ctx, requester, id)
}

func (s *Store) finish(ctx context.Context, id string, from, to State, result, errText string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE proposals SET state = ?, result = ?, error = ?, decided_at = ? WHERE id = ? AND state = ?`,
		string(to), result, errText, nowFunc().UTC().Format(time.RFC3339), id, string(from))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("%w: состояние изменилось", ErrState)
	}
	return nil
}

func (s *Store) sealed(id, part string, b []byte) ([]byte, error) {
	if s.seal == nil {
		return b, nil
	}
	return s.seal.Seal("proposals:"+id+":"+part, b)
}

func (s *Store) opened(id, part string, b []byte) ([]byte, error) {
	if s.seal == nil {
		return b, nil
	}
	return s.seal.Unseal("proposals:"+id+":"+part, b)
}
