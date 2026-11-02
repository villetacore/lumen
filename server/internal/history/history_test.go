package history

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/markdown"
	"github.com/alexpyslar/lumen/server/internal/policy"
	"github.com/alexpyslar/lumen/server/internal/state"
)

func note(t *testing.T, src string) *domain.Note {
	t.Helper()
	n, err := markdown.ParseNote("20-projects/WEB/issues/WEB-1.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

const v1 = "---\nid: WEB-1\ntitle: Макет\ntype: issue\nproject: WEB\nstatus: todo\n---\nОписание\n"

func summary(events []Event) string {
	var out []string
	for _, e := range events {
		s := e.Kind
		if e.Field != "" {
			s += ":" + e.Field + "=" + e.Old + "→" + e.New
		}
		out = append(out, s+"@"+e.Source+"/"+e.Actor)
	}
	return strings.Join(out, " ")
}

func TestJournalRecordsOnlyRealChanges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := state.Open(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	j := New(st.DB(), st)
	alice := policy.With(ctx, &policy.Principal{ID: "s-a", Username: "alice", Role: policy.RoleMember})

	// First sight during the startup sweep: no invented "created".
	j.Observe(ctx, note(t, v1), true)
	if ev, _ := j.Events(ctx, "WEB-1"); len(ev) != 0 {
		t.Fatalf("baseline recorded %s", summary(ev))
	}
	// A change made in Lumen carries its author.
	v2 := strings.Replace(v1, "status: todo", "status: doing", 1)
	j.Observe(alice, note(t, v2), false)
	// The same file seen again — watcher, rescan — records nothing.
	j.Observe(ctx, note(t, v2), false)
	j.Observe(ctx, note(t, v2), true)
	// An edit in Obsidian has no author.
	v3 := strings.Replace(strings.Replace(v2, "title: Макет", "title: Макет главной", 1), "Описание", "Описание\n\n## Комментарии\n\n### 2026-11-02 10:00 · bob ^c-1\nок", 1)
	j.Observe(ctx, note(t, v3), false)
	st.Close()

	// Restart: a new journal over the same database, the same file.
	st, err = state.Open(ctx, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	j = New(st.DB(), st)
	j.Observe(ctx, note(t, v3), true)
	ev, _ := j.Events(ctx, "WEB-1")
	want := "changed:status=todo→doing@lumen/alice changed:comments=0→1@external/ changed:title=Макет→Макет главной@external/"
	if got := summary(ev); got != want {
		t.Fatalf("history\n got %s\nwant %s", got, want)
	}

	// Removal, then the file coming back counts as created.
	j.Removed(alice, "20-projects/WEB/issues/WEB-1.md")
	j.Removed(alice, "20-projects/WEB/issues/WEB-1.md") // twice: still one event
	j.Observe(ctx, note(t, v3), false)
	ev, _ = j.Events(ctx, "WEB-1")
	if got := summary(ev[3:]); got != "deleted@lumen/alice created@external/" {
		t.Fatalf("tail = %s", got)
	}
	if pe, _ := j.ProjectEvents(ctx, "WEB", nowFunc().AddDate(0, 0, -1), nowFunc().AddDate(0, 0, 1)); len(pe) != 5 {
		t.Fatalf("project events = %d", len(pe))
	}
	// Plain notes are not issues.
	if err := j.Observe(ctx, &domain.Note{Path: "x.md", Type: domain.TypeNote}, false); err != nil {
		t.Fatal(err)
	}
}

type xor struct{}

func (xor) Seal(ref string, p []byte) ([]byte, error) {
	return append([]byte(ref+"|"), flip(p)...), nil
}
func (xor) Unseal(ref string, s []byte) ([]byte, error) {
	if !bytes.HasPrefix(s, []byte(ref+"|")) {
		return nil, errors.New("wrong ref")
	}
	return flip(s[len(ref)+1:]), nil
}
func flip(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[i] ^ 0x5a
	}
	return out
}

func TestJournalSealsValues(t *testing.T) {
	ctx := context.Background()
	st, err := state.Open(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	j := New(st.DB(), xor{})
	j.Observe(ctx, note(t, v1), true)
	j.Observe(ctx, note(t, strings.Replace(v1, "Макет", "Секретный макет", 1)), false)
	var raw []byte
	st.DB().QueryRow(`SELECT new FROM issue_events`).Scan(&raw)
	var snap []byte
	st.DB().QueryRow(`SELECT fields FROM issue_snapshots`).Scan(&snap)
	if strings.Contains(string(raw), "Секретный") || strings.Contains(string(snap), "Секретный") {
		t.Fatal("content stored in the clear")
	}
	if ev, _ := j.Events(ctx, "WEB-1"); len(ev) != 1 || ev[0].New != "Секретный макет" {
		t.Fatalf("unsealed = %+v", ev)
	}
}
