package automation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/history"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/notify"
	"github.com/villetacore/lumen/server/internal/people"
	"github.com/villetacore/lumen/server/internal/policy"
	"github.com/villetacore/lumen/server/internal/service"
	"github.com/villetacore/lumen/server/internal/state"
	"github.com/villetacore/lumen/server/internal/state/statetest"
	"github.com/villetacore/lumen/server/internal/vault"
)

const project = `---
title: Сайт
type: project
key: WEB
workflow:
  - id: todo
    name: К работе
    category: todo
  - id: doing
    name: В работе
    category: in_progress
  - id: done
    name: Готово
    category: done
transitions:
  todo: [doing]
  doing: [todo, done]
---
`

type rig struct {
	t       *testing.T
	dir     string
	v       *vault.Vault
	ix      *service.Indexer
	st      *state.Store
	journal *history.Journal
	pol     *policy.Policy
	issues  *service.Issues
	dir2    *people.Directory
	notify  *notify.Store
	m       *Manager
	bus     *service.Bus
	log     *slog.Logger
	store   index.Store
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "20-projects"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "20-projects/web.md"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	raw := index.NewMemory(v.Root)
	bus := service.NewBus()
	ix := service.NewIndexer(v, raw, bus, log)
	st := statetest.Open(t)
	journal := history.New(st.DB(), st)
	ix.SetObserver(journal)
	ctx := context.Background()
	if _, err := ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	pol := policy.New(raw, policy.NewMembers(st.DB()))
	r := &rig{t: t, dir: dir, v: v, ix: ix, st: st, journal: journal, pol: pol, bus: bus, log: log, store: raw}
	r.dir2 = people.New(st.DB())
	r.notify = notify.New(st.DB(), nil)
	r.issues = service.NewIssues(v, policy.Scope(raw, pol), ix, bus, log).WithPolicy(pol).WithNotify(r.notify)
	r.m = r.manager()
	if err := r.m.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) manager() *Manager {
	m := NewManager(NewStore(r.st.DB(), r.st), r.issues, r.pol, r.dir2, r.notify, r.bus, r.log)
	m.UseJournal(r.journal)
	return m
}

func (r *rig) step() {
	r.t.Helper()
	if err := r.m.Step(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

var admin = policy.With(context.Background(), &policy.Principal{ID: "sub-ann", Username: "ann", Role: policy.RoleAdmin})

// person registers someone in the directory, as signing in would.
func (r *rig) person(name string, role policy.Role, groups ...string) context.Context {
	r.t.Helper()
	p := &policy.Principal{ID: "sub-" + name, Username: name, Role: role, Groups: groups}
	if err := r.dir2.Touch(context.Background(), p, ""); err != nil {
		r.t.Fatal(err)
	}
	return policy.With(context.Background(), p)
}

func (r *rig) issue(ctx context.Context, title string) *domain.Issue {
	r.t.Helper()
	is, err := r.issues.Create(ctx, service.IssueInput{Project: "WEB", Title: title}, policy.From(ctx).Username)
	if err != nil {
		r.t.Fatal(err)
	}
	return is
}

func (r *rig) get(ctx context.Context, key string) *domain.Issue {
	r.t.Helper()
	is, err := r.issues.Get(ctx, key)
	if err != nil {
		r.t.Fatal(err)
	}
	return is
}

func (r *rig) move(ctx context.Context, key, status string) {
	r.t.Helper()
	if _, err := r.issues.Update(ctx, key, service.IssueUpdate{Status: &status}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) runs() []Run {
	r.t.Helper()
	runs, err := r.m.store.Runs(context.Background(), "WEB", 100)
	if err != nil {
		r.t.Fatal(err)
	}
	return runs
}

func onDone(actions ...Action) Definition {
	return Definition{Name: "Готово", Trigger: Trigger{Type: TriggerStatusChanged, To: "done"}, Actions: actions}
}

func TestStatusRuleRunsOnceAndDoesNotFireItself(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	rule, err := r.m.Create(ann, "WEB", onDone(
		Action{Type: ActionAddLabel, Value: "закрыто"},
		Action{Type: ActionAddComment, Value: "{{key}} «{{title}}» закрыт"},
		Action{Type: ActionNotify, Value: "reporter"},
	))
	if err != nil {
		t.Fatal(err)
	}
	vic := r.person("vic", policy.RoleMember)
	is := r.issue(vic, "Первый")
	r.move(ann, is.Key, "doing")
	r.step()
	if got := r.get(ann, is.Key); len(got.Labels) != 0 {
		t.Fatalf("rule fired before done: %v", got.Labels)
	}
	r.move(ann, is.Key, "done")
	r.step()
	r.step() // the rule's own writes are in the journal now; they must not fire it

	got := r.get(ann, is.Key)
	if len(got.Labels) != 1 || got.Labels[0] != "закрыто" || len(got.Comments) != 1 {
		t.Fatalf("issue = %+v", got)
	}
	if !strings.Contains(got.Comments[0].Body, "WEB-1 «Первый» закрыт") || got.Comments[0].Author != service.AutomationActor {
		t.Fatalf("comment = %+v", got.Comments[0])
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != StatusDone || runs[0].Actions != 3 || runs[0].RuleID != rule.ID {
		t.Fatalf("runs = %+v", runs)
	}
	// The reporter hears about it even though the owner's name is on the rule.
	list, err := r.notify.List(context.Background(), "vic", false, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range list {
		found = found || (n.Kind == notify.Automation && n.Ref == rule.ID)
	}
	if !found {
		t.Fatalf("no automation notification in %+v", list)
	}
	// The journal tells a rule's edit from a person's.
	events, _ := r.journal.Events(context.Background(), is.Key)
	sawAutomation := false
	for _, e := range events {
		if e.Field == "labels" {
			sawAutomation = e.Source == "automation" && e.Actor == "ann"
		}
	}
	if !sawAutomation {
		t.Fatalf("label event not attributed: %+v", events)
	}
}

func TestOnlyEventsAfterSavingFire(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	before := r.issue(ann, "Старый")
	r.move(ann, before.Key, "doing")
	r.move(ann, before.Key, "done")
	rule, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddLabel, Value: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, before.Key); len(got.Labels) != 0 {
		t.Fatal("a rule fired on what happened before it was saved")
	}

	// Switched off: events pass by, and switching on does not replay them.
	off := false
	if rule, err = r.m.Update(ann, rule.ID, nil, &off, rule.Revision); err != nil {
		t.Fatal(err)
	}
	second := r.issue(ann, "Второй")
	r.move(ann, second.Key, "doing")
	r.move(ann, second.Key, "done")
	r.step()
	on := true
	if _, err = r.m.Update(ann, rule.ID, nil, &on, rule.Revision); err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, second.Key); len(got.Labels) != 0 {
		t.Fatal("events from while the rule was off were replayed")
	}
}

func TestExternalEditFiresRule(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	if _, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddLabel, Value: "снаружи"})); err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Правка в Obsidian")
	r.move(ann, is.Key, "doing")
	data, err := os.ReadFile(filepath.Join(r.dir, is.Path))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "status: doing", "status: done", 1)
	if err := os.WriteFile(filepath.Join(r.dir, is.Path), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.ix.ReindexPath(context.Background(), is.Path); err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, is.Key); len(got.Labels) != 1 {
		t.Fatalf("labels = %v", got.Labels)
	}
}

// A crash after the actions and before the cursor moved repeats the batch; the
// claim keeps the comment from being written twice.
func TestRestartDoesNotRepeatActions(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	if _, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddComment, Value: "однажды"})); err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Тикет")
	cursor, _, _ := r.m.store.Cursor(context.Background())
	r.move(ann, is.Key, "doing")
	r.move(ann, is.Key, "done")
	r.step()
	if err := r.m.store.SetCursor(context.Background(), cursor); err != nil {
		t.Fatal(err)
	}
	r.m = r.manager()
	if err := r.m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, is.Key); len(got.Comments) != 1 {
		t.Fatalf("comments = %d", len(got.Comments))
	}
}

// A run cut off mid-way is reported and not retried: its actions may have happened.
func TestInterruptedRunIsVisibleAndNotRetried(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	rule, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddComment, Value: "текст"}))
	if err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Тикет")
	r.move(ann, is.Key, "doing")
	cursor, _, _ := r.m.store.Cursor(context.Background())
	r.move(ann, is.Key, "done")
	last, _ := r.journal.LastID(context.Background())
	// What a process leaves behind if it dies right after the claim.
	if _, _, err := r.m.store.Claim(context.Background(), rule, "event:"+strconv.FormatInt(last, 10), is.Key); err != nil {
		t.Fatal(err)
	}
	if err := r.m.store.SetCursor(context.Background(), cursor); err != nil {
		t.Fatal(err)
	}
	r.m = r.manager()
	if err := r.m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, is.Key); len(got.Comments) != 0 {
		t.Fatal("an interrupted run was repeated")
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != StatusUnknown || runs[0].Error == "" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestScheduleFiresOnceAfterMissedDaysAndKeepsIssuesApart(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = time.Now })

	late := r.issue(ann, "Просрочен")
	if _, err := r.issues.Update(ann, late.Key, service.IssueUpdate{Due: ptr("2026-11-01")}); err != nil {
		t.Fatal(err)
	}
	finished := r.issue(ann, "Закрыт, но просрочен")
	if _, err := r.issues.Update(ann, finished.Key, service.IssueUpdate{Due: ptr("2026-11-01")}); err != nil {
		t.Fatal(err)
	}
	r.move(ann, finished.Key, "doing")
	r.move(ann, finished.Key, "done")
	r.issue(ann, "Без срока")

	rule, err := r.m.Create(ann, "WEB", Definition{
		Name:       "Утренняя проверка",
		Trigger:    Trigger{Type: TriggerSchedule, Schedule: &Schedule{At: "09:00", TZ: "UTC"}},
		Conditions: []Condition{{Field: "due", Op: "overdue"}},
		Actions:    []Action{{Type: ActionAddComment, Value: "Срок прошёл"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule.NextRun != "2026-11-06T09:00:00Z" {
		t.Fatalf("next run = %s", rule.NextRun)
	}
	r.step()
	if got := r.get(ann, late.Key); len(got.Comments) != 0 {
		t.Fatal("fired before its time")
	}

	// The server was down for three firings.
	now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	r.step()
	r.step()
	if got := r.get(ann, late.Key); len(got.Comments) != 1 {
		t.Fatalf("comments = %d, want one for all the missed days", len(got.Comments))
	}
	if got := r.get(ann, finished.Key); len(got.Comments) != 0 {
		t.Fatal("a finished issue counted as overdue")
	}
	stored, _ := r.m.store.Get(context.Background(), rule.ID)
	if stored.NextRun != "2026-11-10T09:00:00Z" {
		t.Fatalf("next run = %s", stored.NextRun)
	}
	summary := 0
	for _, run := range r.runs() {
		if run.Issue == "" {
			summary++
			if run.Matched != 1 || run.Status != StatusDone {
				t.Fatalf("summary = %+v", run)
			}
		}
	}
	if summary != 1 {
		t.Fatalf("summaries = %d", summary)
	}

	// Run now: due at once, then back on the timetable.
	if err := r.m.RunNow(ann, rule.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	r.step()
	if got := r.get(ann, late.Key); len(got.Comments) != 2 {
		t.Fatalf("comments after run now = %d", len(got.Comments))
	}
	stored, _ = r.m.store.Get(context.Background(), rule.ID)
	if stored.NextRun != "2026-11-10T09:00:00Z" {
		t.Fatalf("next run after run now = %s", stored.NextRun)
	}
}

func ptr(s string) *string { return &s }

func TestScheduleNextAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-08: 02:30 does not exist in New York.
	s := Schedule{At: "02:30", TZ: "America/New_York"}
	next, err := s.Next(time.Date(2026, 3, 8, 0, 0, 0, 0, ny))
	if err != nil {
		t.Fatal(err)
	}
	if next.Day() != 8 || next.Before(time.Date(2026, 3, 8, 0, 0, 0, 0, ny)) {
		t.Fatalf("spring forward: %v", next)
	}
	// 2026-11-01: 01:30 happens twice; the day fires once.
	s = Schedule{At: "01:30", TZ: "America/New_York"}
	first, err := s.Next(time.Date(2026, 11, 1, 0, 0, 0, 0, ny))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Next(first)
	if err != nil {
		t.Fatal(err)
	}
	if first.In(ny).Day() != 1 || second.In(ny).Day() != 2 {
		t.Fatalf("fall back: %v then %v", first, second)
	}
	// Weekdays: Monday only.
	s = Schedule{At: "09:00", TZ: "UTC", Weekdays: []int{1}}
	next, err = s.Next(time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)) // a Thursday
	if err != nil {
		t.Fatal(err)
	}
	if next.Weekday() != time.Monday || next.Day() != 9 {
		t.Fatalf("weekday: %v", next)
	}
}

// A rule acts with its owner's rights as of now.
func TestOwnerWithoutRightsBlocksRule(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	if _, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddLabel, Value: "x"})); err != nil {
		t.Fatal(err)
	}
	// Ann loses the workspace role (the next sign-in would record this).
	r.person("ann", policy.RoleViewer)
	is := r.issue(admin, "Тикет")
	r.move(admin, is.Key, "doing")
	r.move(admin, is.Key, "done")
	r.step()
	if got := r.get(admin, is.Key); len(got.Labels) != 0 {
		t.Fatal("a rule wrote with rights its owner no longer has")
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != StatusBlocked {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestClosingProjectToOthersBlocksTheirRule(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	bob := r.person("bob", policy.RoleAdmin)
	if _, err := r.m.Create(bob, "WEB", onDone(Action{Type: ActionAddLabel, Value: "x"})); err != nil {
		t.Fatal(err)
	}
	// Bob is demoted, then the project closes to Ann only.
	bob = r.person("bob", policy.RoleMember)
	if _, err := r.pol.SetMember(ann, "WEB", "user", "ann", policy.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Тикет")
	r.move(ann, is.Key, "doing")
	r.move(ann, is.Key, "done")
	r.step()
	if got := r.get(ann, is.Key); len(got.Labels) != 0 {
		t.Fatal("rule of an outsider wrote to a closed project")
	}
	// And the outsider can see neither the rules nor the log.
	if _, _, err := r.m.List(bob, "WEB"); !errors.Is(err, index.ErrNotFound) {
		t.Fatalf("list as outsider: %v", err)
	}
	if _, err := r.m.Runs(bob, "WEB", 10); !errors.Is(err, index.ErrNotFound) {
		t.Fatalf("runs as outsider: %v", err)
	}
}

func TestOnlyProjectAdminEditsRules(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	member := r.person("mia", policy.RoleMember)
	if _, err := r.m.Create(member, "WEB", onDone(Action{Type: ActionAddLabel, Value: "x"})); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("member creating a rule: %v", err)
	}
	rule, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddLabel, Value: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, can, err := r.m.List(member, "WEB"); err != nil || can {
		t.Fatalf("member reading rules: can=%v err=%v", can, err)
	}
	if err := r.m.Delete(member, rule.ID); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("member deleting: %v", err)
	}
	d := rule.Definition
	if _, err := r.m.Update(member, rule.ID, &d, nil, rule.Revision); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("member updating: %v", err)
	}
	if err := r.m.RunNow(member, rule.ID); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("member running: %v", err)
	}
}

func TestSavingTakesOverOwnershipAndChecksRevision(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	bob := r.person("bob", policy.RoleAdmin)
	rule, err := r.m.Create(ann, "WEB", onDone(Action{Type: ActionAddLabel, Value: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	d := rule.Definition
	d.Name = "Новое имя"
	updated, err := r.m.Update(bob, rule.ID, &d, nil, rule.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Owner != "bob" || updated.Revision != rule.Revision+1 {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := r.m.Update(ann, rule.ID, &d, nil, rule.Revision); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
}

func TestFailedActionStopsAndIsReported(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	// The workflow has no todo → done.
	if _, err := r.m.Create(ann, "WEB", Definition{
		Name: "Сразу готово", Trigger: Trigger{Type: TriggerIssueCreated},
		Actions: []Action{
			{Type: ActionAddLabel, Value: "первое"},
			{Type: ActionSetStatus, Value: "done"},
			{Type: ActionAddLabel, Value: "третье"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Тикет")
	r.step()
	got := r.get(ann, is.Key)
	if got.Status != "todo" || len(got.Labels) != 1 || got.Labels[0] != "первое" {
		t.Fatalf("issue = %+v", got)
	}
	runs := r.runs()
	if len(runs) != 1 || runs[0].Status != StatusFailed || runs[0].Actions != 1 || !strings.Contains(runs[0].Error, "set_status") {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestConditionsAndOtherActions(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	vic := r.person("vic", policy.RoleMember)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = time.Now })
	if _, err := r.m.Create(ann, "WEB", Definition{
		Name:       "Разбор багов",
		Trigger:    Trigger{Type: TriggerIssueCreated},
		Conditions: []Condition{{Field: "issue_type", Op: "is", Value: "bug"}, {Field: "assignee", Op: "empty"}},
		Actions: []Action{
			{Type: ActionSetAssignee, Value: "@reporter"},
			{Type: ActionSetPriority, Value: "high"},
			{Type: ActionSetDue, Value: "2"},
			{Type: ActionCreateSubissue, Value: "Воспроизвести {{key}}"},
			{Type: ActionRemoveLabel, Value: "новый"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.issues.Create(vic, service.IssueInput{Project: "WEB", Title: "Задача", Type: "task"}, "vic"); err != nil {
		t.Fatal(err)
	}
	bug, err := r.issues.Create(vic, service.IssueInput{Project: "WEB", Title: "Баг", Type: "bug", Labels: []string{"новый", "ui"}}, "vic")
	if err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(vic, "WEB-1"); got.Assignee != "" {
		t.Fatalf("task matched a bug rule: %+v", got)
	}
	got := r.get(vic, bug.Key)
	if got.Assignee != "vic" || got.Priority != "high" || !strings.HasPrefix(got.Due.String(), now.AddDate(0, 0, 2).Format("2006-01-02")) ||
		len(got.Labels) != 1 || got.Labels[0] != "ui" {
		t.Fatalf("bug = %+v", got)
	}
	subs, _, err := r.issues.List(vic, domain.IssueQuery{Project: "WEB", Parent: bug.Key})
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Title != "Воспроизвести WEB-2" || subs[0].Reporter != service.AutomationActor {
		t.Fatalf("subissues = %+v", subs)
	}
	// The sub-issue was created by a rule: it does not start another round.
	all, _, _ := r.issues.List(vic, domain.IssueQuery{Project: "WEB"})
	if len(all) != 3 {
		t.Fatalf("issues = %d", len(all))
	}
}

func TestComment(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	if _, err := r.m.Create(ann, "WEB", Definition{
		Name: "Ответ", Trigger: Trigger{Type: TriggerCommentAdded},
		Conditions: []Condition{{Field: "status", Op: "is", Value: "todo"}},
		Actions:    []Action{{Type: ActionSetStatus, Value: "doing"}},
	}); err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Тикет")
	if _, err := r.issues.Update(ann, is.Key, service.IssueUpdate{Title: ptr("Другое имя")}); err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, is.Key); got.Status != "todo" {
		t.Fatal("a rename fired a comment rule")
	}
	if _, _, err := r.issues.AddComment(ann, is.Key, "вопрос", "ann"); err != nil {
		t.Fatal(err)
	}
	r.step()
	if got := r.get(ann, is.Key); got.Status != "doing" {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestFuseStopsARunawayRule(t *testing.T) {
	r := newRig(t)
	ann := r.person("ann", policy.RoleAdmin)
	if _, err := r.m.Create(ann, "WEB", Definition{
		Name: "Шумное", Trigger: Trigger{Type: TriggerFieldChanged, Field: "priority"},
		Actions: []Action{{Type: ActionAddComment, Value: "изменён приоритет"}},
	}); err != nil {
		t.Fatal(err)
	}
	is := r.issue(ann, "Тикет")
	for i := 0; i < fuseRuns+3; i++ {
		pri := []string{"high", "low"}[i%2]
		if _, err := r.issues.Update(ann, is.Key, service.IssueUpdate{Priority: &pri}); err != nil {
			t.Fatal(err)
		}
		r.step()
	}
	if got := r.get(ann, is.Key); len(got.Comments) != fuseRuns {
		t.Fatalf("comments = %d, want %d", len(got.Comments), fuseRuns)
	}
	throttled := 0
	for _, run := range r.runs() {
		if run.Status == StatusThrottled {
			throttled++
		}
	}
	if throttled != 3 {
		t.Fatalf("throttled = %d", throttled)
	}
}

func TestValidate(t *testing.T) {
	p := &domain.TrackerProject{Key: "WEB", Workflow: domain.DefaultWorkflow()}
	good := func() Definition {
		return Definition{Name: "x", Trigger: Trigger{Type: TriggerIssueCreated}, Actions: []Action{{Type: ActionAddLabel, Value: "a"}}}
	}
	for name, change := range map[string]func(*Definition){
		"no name":     func(d *Definition) { d.Name = " " },
		"no actions":  func(d *Definition) { d.Actions = nil },
		"bad trigger": func(d *Definition) { d.Trigger.Type = "webhook" },
		"bad status":  func(d *Definition) { d.Actions = []Action{{Type: ActionSetStatus, Value: "нет"}} },
		"bad to":      func(d *Definition) { d.Trigger = Trigger{Type: TriggerStatusChanged, To: "нет"} },
		"bad action":  func(d *Definition) { d.Actions = []Action{{Type: "http_post", Value: "https://x"}} },
		"bad field":   func(d *Definition) { d.Trigger = Trigger{Type: TriggerFieldChanged, Field: "description"} },
		"no schedule": func(d *Definition) { d.Trigger = Trigger{Type: TriggerSchedule} },
		"bad time": func(d *Definition) {
			d.Trigger = Trigger{Type: TriggerSchedule, Schedule: &Schedule{At: "25:00", TZ: "UTC"}}
		},
		"bad zone": func(d *Definition) {
			d.Trigger = Trigger{Type: TriggerSchedule, Schedule: &Schedule{At: "09:00", TZ: "Mars/Base"}}
		},
		"bad weekday": func(d *Definition) {
			d.Trigger = Trigger{Type: TriggerSchedule, Schedule: &Schedule{At: "09:00", TZ: "UTC", Weekdays: []int{8}}}
		},
		"bad cond":     func(d *Definition) { d.Conditions = []Condition{{Field: "path", Op: "is", Value: "x"}} },
		"bad op":       func(d *Definition) { d.Conditions = []Condition{{Field: "assignee", Op: "matches", Value: "x"}} },
		"bad due":      func(d *Definition) { d.Conditions = []Condition{{Field: "due", Op: "within", Value: "скоро"}} },
		"no value":     func(d *Definition) { d.Conditions = []Condition{{Field: "assignee", Op: "is"}} },
		"bad notify":   func(d *Definition) { d.Actions = []Action{{Type: ActionNotify, Value: "всем"}} },
		"bad assignee": func(d *Definition) { d.Actions = []Action{{Type: ActionSetAssignee, Value: "a b<c>"}} },
	} {
		d := good()
		change(&d)
		if err := d.Validate(p); !errors.Is(err, service.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	d := good()
	if err := d.Validate(p); err != nil {
		t.Fatal(err)
	}
}
