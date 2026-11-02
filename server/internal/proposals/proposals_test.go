package proposals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexpyslar/lumen/server/internal/state"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := state.Open(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st.DB(), st)
}

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	p, err := s.Create(ctx, Proposal{Requester: "alice", PrincipalID: "s-a", Kind: "add_comment", Target: "WEB-1",
		Args: []byte(`{"key":"WEB-1","body":"x"}`), Preview: "Комментарий", RequestID: "r1"})
	if err != nil || p.State != Pending {
		t.Fatalf("create = %+v %v", p, err)
	}
	// A retried call with the same request id returns the same proposal…
	again, err := s.Create(ctx, Proposal{Requester: "alice", PrincipalID: "s-a", Kind: "add_comment", Target: "WEB-1",
		Args: []byte(`{"key":"WEB-1","body":"x"}`), Preview: "Комментарий", RequestID: "r1"})
	if err != nil || again.ID != p.ID {
		t.Fatalf("retry = %+v %v", again, err)
	}
	// …and cannot smuggle in a different change.
	if _, err := s.Create(ctx, Proposal{Requester: "alice", PrincipalID: "s-a", Kind: "add_comment", Target: "WEB-1",
		Args: []byte(`{"key":"WEB-1","body":"другое"}`), RequestID: "r1"}); !errors.Is(err, ErrState) {
		t.Fatalf("reused request id: %v", err)
	}
	// Somebody else neither sees nor applies it.
	if _, err := s.Get(ctx, "bob", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob get: %v", err)
	}
	if _, err := s.Begin(ctx, "alice", "s-other", p.ID); !errors.Is(err, ErrState) {
		t.Fatalf("other subject with the same username: %v", err)
	}
	if _, err := s.Begin(ctx, "alice", "s-a", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(ctx, "alice", "s-a", p.ID); !errors.Is(err, ErrState) {
		t.Fatalf("second begin must lose: %v", err)
	}
	if err := s.Finish(ctx, p.ID, Applied, "WEB-1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, "alice", p.ID); got.State != Applied || got.Result != "WEB-1" {
		t.Fatalf("after apply = %+v", got)
	}
	if _, err := s.Reject(ctx, "alice", "s-a", p.ID); !errors.Is(err, ErrState) {
		t.Fatalf("reject after apply: %v", err)
	}
}

func TestExpiryAndReject(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return start }
	t.Cleanup(func() { nowFunc = time.Now })
	old, _ := s.Create(ctx, Proposal{Requester: "alice", PrincipalID: "s-a", Kind: "k", Args: []byte(`{}`)})
	keep, _ := s.Create(ctx, Proposal{Requester: "alice", PrincipalID: "s-a", Kind: "k", Args: []byte(`{}`)})
	if _, err := s.Reject(ctx, "alice", "s-a", keep.ID); err != nil {
		t.Fatal(err)
	}
	nowFunc = func() time.Time { return start.Add(TTL + time.Minute) }
	if got, _ := s.Get(ctx, "alice", old.ID); got.State != Expired {
		t.Fatalf("state after TTL = %s", got.State)
	}
	if _, err := s.Begin(ctx, "alice", "s-a", old.ID); !errors.Is(err, ErrState) {
		t.Fatalf("expired begin: %v", err)
	}
	if list, _ := s.List(ctx, "alice", true); len(list) != 0 {
		t.Fatalf("pending list = %+v", list)
	}
}
