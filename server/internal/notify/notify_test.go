package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/state"
)

func TestMentions(t *testing.T) {
	text := "Привет @alice и @bob.smith, @Алёна!\nпочта a@b.com, путь /x/@y\n`@code` не считается\n```\n@fenced\n```\n@alice ещё раз, конец предложения @carol."
	if got := strings.Join(Mentions(text), ","); got != "alice,bob.smith,carol,Алёна" {
		t.Fatalf("mentions = %q", got)
	}
}

func TestEmitDedupAndRead(t *testing.T) {
	ctx := context.Background()
	st, err := state.Open(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var pushed []string
	s := New(st.DB(), func(r, issue string) { pushed = append(pushed, r+":"+issue) })

	e := Event{Kind: Mentioned, Issue: "WEB-1", Ref: "c-1", Actor: "bob", Recipients: []string{"alice", "bob", "carol", "alice"}, Dedup: "mention:WEB-1:c-1"}
	if n, err := s.Emit(ctx, e); err != nil || n != 2 {
		t.Fatalf("first emit = %d, %v", n, err)
	}
	// A retried request reports the same event: nothing new, nothing pushed.
	if n, _ := s.Emit(ctx, e); n != 0 || len(pushed) != 2 {
		t.Fatalf("retry added %d, pushed %v", n, pushed)
	}
	if list, _ := s.List(ctx, "bob", false, 0); len(list) != 0 {
		t.Fatal("the actor is never notified")
	}
	s.Emit(ctx, Event{Kind: Assigned, Issue: "WEB-2", Actor: "bob", Recipients: []string{"alice"}, Dedup: "assigned:WEB-2:t1"})
	list, _ := s.List(ctx, "alice", false, 0)
	if len(list) != 2 || list[0].Issue != "WEB-2" || list[0].Read {
		t.Fatalf("alice = %+v", list)
	}
	if err := s.MarkRead(ctx, "carol", []int64{list[0].ID}); err != nil {
		t.Fatal(err)
	}
	if unread, _ := s.List(ctx, "alice", true, 0); len(unread) != 2 {
		t.Fatal("someone else cannot mark alice's notifications read")
	}
	s.MarkRead(ctx, "alice", []int64{list[0].ID})
	if unread, _ := s.List(ctx, "alice", true, 0); len(unread) != 1 || unread[0].Issue != "WEB-1" {
		t.Fatalf("after one read = %+v", unread)
	}
	s.MarkRead(ctx, "alice", nil)
	if unread, _ := s.List(ctx, "alice", true, 0); len(unread) != 0 {
		t.Fatal("mark all")
	}
	var nilStore *Store
	if n, err := nilStore.Emit(ctx, e); n != 0 || err != nil {
		t.Fatal("nil store drops events")
	}
}
