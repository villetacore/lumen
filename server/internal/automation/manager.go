package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/history"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/notify"
	"github.com/villetacore/lumen/server/internal/people"
	"github.com/villetacore/lumen/server/internal/policy"
	"github.com/villetacore/lumen/server/internal/service"
)

const (
	// maxScheduledIssues bounds one scheduled firing: a broad rule over a big project
	// should not rewrite everything at once. The summary row says when it was cut.
	maxScheduledIssues = 200
	// fuseRuns per fuseWindow on one issue trips the fuse. Rule writes do not fire
	// rules, so this only catches loops through something outside Lumen (a sync tool
	// reacting to the file) or a rule edited in a burst of events.
	fuseRuns   = 10
	fuseWindow = time.Minute
	batchSize  = 200
	tickEvery  = 15 * time.Second
)

// Manager is the rule service: requests edit rules through it, and Run works them off.
type Manager struct {
	store   *Store
	issues  *service.Issues
	policy  *policy.Policy
	people  *people.Directory
	notify  *notify.Store
	bus     *service.Bus
	log     *slog.Logger
	journal *history.Journal
}

func NewManager(st *Store, issues *service.Issues, p *policy.Policy, dir *people.Directory, n *notify.Store, bus *service.Bus, log *slog.Logger) *Manager {
	return &Manager{store: st, issues: issues, policy: p, people: dir, notify: n, bus: bus, log: log}
}

// UseJournal attaches the event feed.
func (m *Manager) UseJournal(j *history.Journal) { m.journal = j }

// List returns a project's rules; the project must be visible to the caller.
func (m *Manager) List(ctx context.Context, project string) ([]*Rule, bool, error) {
	p, err := m.issues.Project(ctx, project)
	if err != nil {
		return nil, false, err
	}
	rules, err := m.store.Project(ctx, p.Key)
	if err != nil {
		return nil, false, err
	}
	return rules, m.policy.ProjectRole(ctx, p.Key) >= policy.RoleAdmin, nil
}

// Runs returns a project's log with rule names.
func (m *Manager) Runs(ctx context.Context, project string, limit int) ([]Run, error) {
	p, err := m.issues.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	runs, err := m.store.Runs(ctx, p.Key, limit)
	if err != nil {
		return nil, err
	}
	rules, err := m.store.Project(ctx, p.Key)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rules))
	for _, r := range rules {
		names[r.ID] = r.Name
	}
	for i := range runs {
		runs[i].RuleName = names[runs[i].RuleID]
	}
	return runs, nil
}

// RuleName is what a notification calls its rule; "" for a deleted one.
func (m *Manager) RuleName(ctx context.Context, id string) string {
	r, err := m.store.Get(ctx, id)
	if err != nil {
		return ""
	}
	return r.Name
}

// owner is the person a saved rule will act for. Saving takes project admin: the
// rule goes on writing to the project without anyone watching.
func (m *Manager) owner(ctx context.Context, project string) (*policy.Principal, error) {
	if err := m.policy.CheckProject(ctx, project, policy.RoleAdmin); err != nil {
		return nil, err
	}
	pr := policy.From(ctx)
	if pr == nil || pr.ID == "" {
		return nil, fmt.Errorf("%w: правило должно быть чьим-то", policy.ErrForbidden)
	}
	return pr, nil
}

// arm sets where a saved rule starts from: only events after this moment, and the
// next firing of its schedule.
func (m *Manager) arm(ctx context.Context, r *Rule) error {
	last, err := m.journal.LastID(ctx)
	if err != nil {
		return err
	}
	r.afterEvent = last
	r.NextRun = ""
	if s := r.Trigger.Schedule; s != nil && r.Enabled {
		next, err := s.Next(nowFunc())
		if err != nil {
			return err
		}
		r.NextRun = stamp(next)
	}
	return nil
}

// Create saves a new rule, switched on.
func (m *Manager) Create(ctx context.Context, project string, d Definition) (*Rule, error) {
	p, err := m.issues.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	pr, err := m.owner(ctx, p.Key)
	if err != nil {
		return nil, err
	}
	if err := d.Validate(p); err != nil {
		return nil, err
	}
	r := &Rule{Definition: d, Project: p.Key, Enabled: true, OwnerID: pr.ID, Owner: pr.Username}
	if err := m.arm(ctx, r); err != nil {
		return nil, err
	}
	if err := m.store.Insert(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// visible loads a rule whose project the caller can see.
func (m *Manager) visible(ctx context.Context, id string) (*Rule, *domain.TrackerProject, error) {
	r, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	p, err := m.issues.Project(ctx, r.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("automation: %w", index.ErrNotFound)
	}
	return r, p, nil
}

// Update replaces a rule. Whoever saves it becomes its owner: the rule must never
// act with rights of someone who did not write what it now does.
func (m *Manager) Update(ctx context.Context, id string, d *Definition, enabled *bool, revision int64) (*Rule, error) {
	r, p, err := m.visible(ctx, id)
	if err != nil {
		return nil, err
	}
	pr, err := m.owner(ctx, p.Key)
	if err != nil {
		return nil, err
	}
	if d != nil {
		if err := d.Validate(p); err != nil {
			return nil, err
		}
		r.Definition = *d
	}
	if enabled != nil {
		r.Enabled = *enabled
	}
	r.OwnerID, r.Owner = pr.ID, pr.Username
	if err := m.arm(ctx, r); err != nil {
		return nil, err
	}
	if err := m.store.Update(ctx, r, revision); err != nil {
		return nil, err
	}
	return r, nil
}

// Delete removes a rule and its log.
func (m *Manager) Delete(ctx context.Context, id string) error {
	r, p, err := m.visible(ctx, id)
	if err != nil {
		return err
	}
	if err := m.policy.CheckProject(ctx, p.Key, policy.RoleAdmin); err != nil {
		return err
	}
	return m.store.Delete(ctx, r.ID)
}

// RunNow makes a scheduled rule due at once; the worker picks it up.
func (m *Manager) RunNow(ctx context.Context, id string) error {
	r, p, err := m.visible(ctx, id)
	if err != nil {
		return err
	}
	if err := m.policy.CheckProject(ctx, p.Key, policy.RoleAdmin); err != nil {
		return err
	}
	if r.Trigger.Type != TriggerSchedule {
		return invalid("вручную запускаются только правила по расписанию")
	}
	if !r.Enabled {
		return invalid("правило выключено")
	}
	if err := m.store.SetNextRun(ctx, r.ID, r.NextRun, nowFunc().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	m.bus.Publish(service.Event{Type: "automation.wake"})
	return nil
}

// Run works rules off until ctx ends. The journal is the queue: its rows survive a
// restart, so what happened while this was busy or down is not lost.
func (m *Manager) Run(ctx context.Context) {
	if err := m.Prepare(ctx); err != nil {
		m.log.Error("автоматизации не запущены", "err", err)
		return
	}
	events, cancel := m.bus.Subscribe()
	defer cancel()
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	for {
		m.safeStep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case e, ok := <-events:
			if !ok {
				return
			}
			if !wakes(e.Type) {
				continue
			}
		}
	}
}

func wakes(eventType string) bool {
	switch eventType {
	case "issue.changed", "issue.deleted", "note.changed", "note.deleted", "automation.wake":
		return true
	}
	return false
}

func (m *Manager) safeStep(ctx context.Context) {
	defer func() {
		if p := recover(); p != nil {
			m.log.Error("сбой автоматизации", "panic", p)
		}
	}()
	if err := m.Step(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.log.Warn("шаг автоматизаций не завершён", "err", err)
	}
}

// Prepare settles what a previous process left: runs it cut short are marked, and
// on the very first start the journal so far counts as already seen.
func (m *Manager) Prepare(ctx context.Context) error {
	if err := m.store.Interrupted(ctx); err != nil {
		return err
	}
	_, known, err := m.store.Cursor(ctx)
	if err != nil || known {
		return err
	}
	last, err := m.journal.LastID(ctx)
	if err != nil {
		return err
	}
	return m.store.SetCursor(ctx, last)
}

// Step handles everything due now: new journal events, then schedules.
func (m *Manager) Step(ctx context.Context) error {
	err := m.stepEvents(ctx)
	if serr := m.stepSchedules(ctx); err == nil {
		err = serr
	}
	return err
}

func (m *Manager) stepEvents(ctx context.Context) error {
	cursor, _, err := m.store.Cursor(ctx)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := m.journal.After(ctx, cursor, batchSize)
		if err != nil || len(events) == 0 {
			return err
		}
		rules, err := m.store.Enabled(ctx, "event")
		if err != nil {
			return err
		}
		touched := map[string]bool{}
		for _, e := range events {
			cursor = e.ID
			if e.Source == "automation" {
				continue // rules do not fire rules: no cascades, no loops
			}
			for _, r := range rules {
				if r.Project != e.Project || e.ID <= r.afterEvent || !triggered(&r.Trigger, e) {
					continue
				}
				if err := m.apply(ctx, r, "event:"+strconv.FormatInt(e.ID, 10), e.Issue); err != nil {
					m.log.Warn("правило не применено", "rule", r.ID, "issue", e.Issue, "err", err)
				}
				touched[r.ID] = true
			}
		}
		// Crashing before this line repeats the batch; Claim makes that harmless.
		if err := m.store.SetCursor(ctx, cursor); err != nil {
			return err
		}
		for id := range touched {
			if err := m.store.Prune(ctx, id); err != nil {
				m.log.Warn("журнал правила не сокращён", "rule", id, "err", err)
			}
		}
		if len(events) < batchSize {
			return nil
		}
	}
}

func triggered(t *Trigger, e history.Event) bool {
	switch t.Type {
	case TriggerIssueCreated:
		return e.Kind == "created"
	case TriggerStatusChanged:
		return e.Kind == "changed" && e.Field == "status" && (t.From == "" || t.From == e.Old) && (t.To == "" || t.To == e.New)
	case TriggerFieldChanged:
		return e.Kind == "changed" && e.Field == t.Field && (t.To == "" || t.To == e.New)
	case TriggerCommentAdded:
		if e.Kind != "changed" || e.Field != "comments" {
			return false
		}
		before, _ := strconv.Atoi(e.Old)
		after, _ := strconv.Atoi(e.New)
		return after > before
	}
	return false
}

// principal rebuilds the owner as of now, so a demoted or removed owner stops the
// rule. Service owners (local mode, static token) have no directory entry.
func (m *Manager) principal(ctx context.Context, r *Rule) (*policy.Principal, error) {
	var pr *policy.Principal
	if r.OwnerID == "local" || r.OwnerID == "token" {
		pr = policy.Service(r.OwnerID)
	} else {
		var err error
		if pr, err = m.people.Principal(ctx, r.OwnerID); err != nil {
			return nil, fmt.Errorf("владельца правила %s нет в справочнике: %w", r.Owner, err)
		}
	}
	pr.Via = "automation:" + r.ID
	return pr, nil
}

// asOwner gives the context a rule works in, or says why it may not work.
func (m *Manager) asOwner(ctx context.Context, r *Rule) (context.Context, string) {
	pr, err := m.principal(ctx, r)
	if err != nil {
		return nil, err.Error()
	}
	octx := policy.With(ctx, pr)
	if m.policy.ProjectRole(octx, r.Project) < policy.RoleMember {
		return nil, "у владельца правила " + r.Owner + " больше нет права менять проект"
	}
	return octx, ""
}

// record writes a run that did not reach the actions.
func (m *Manager) record(ctx context.Context, r *Rule, occurrence, issue, status, why string, matched int) error {
	id, ok, err := m.store.Claim(ctx, r, occurrence, issue)
	if err != nil || !ok {
		return err
	}
	return m.store.Finish(ctx, id, status, why, 0, matched)
}

// apply runs a rule on one issue for one occurrence.
func (m *Manager) apply(ctx context.Context, r *Rule, occurrence, key string) error {
	octx, why := m.asOwner(ctx, r)
	if why != "" {
		return m.record(ctx, r, occurrence, key, StatusBlocked, why, 0)
	}
	is, err := m.issues.Get(octx, key)
	if err != nil {
		if errors.Is(err, index.ErrNotFound) {
			return nil // deleted meanwhile
		}
		return err
	}
	return m.applyTo(octx, ctx, r, occurrence, is)
}

// applyTo checks conditions and fuse, claims the run and performs the actions.
// A failed action ends the run; earlier ones stay done, and the log says how many.
func (m *Manager) applyTo(octx, ctx context.Context, r *Rule, occurrence string, is *domain.Issue) error {
	now := nowFunc().In(r.location())
	if !Matches(r.Conditions, is, now) {
		return nil
	}
	recent, err := m.store.RecentRuns(ctx, r.ID, is.Key, nowFunc().Add(-fuseWindow))
	if err != nil {
		return err
	}
	if recent >= fuseRuns {
		return m.record(ctx, r, occurrence, is.Key, StatusThrottled, "слишком часто на одном тикете: похоже на зацикливание", 0)
	}
	id, ok, err := m.store.Claim(ctx, r, occurrence, is.Key)
	if err != nil || !ok {
		return err
	}
	done, status, errText := 0, StatusDone, ""
	for _, a := range r.Actions {
		if err := m.perform(octx, r, a, is, occurrence, now); err != nil {
			status, errText = StatusFailed, fmt.Sprintf("действие %d (%s): %v", done+1, a.Type, err)
			if errors.Is(err, policy.ErrForbidden) {
				status = StatusBlocked
			}
			break
		}
		done++
	}
	if err := m.store.Finish(ctx, id, status, errText, done, 0); err != nil {
		return err
	}
	if p, err := m.issues.Project(octx, r.Project); err == nil {
		m.bus.Publish(service.Event{Type: "automation.run", ID: r.ID, Path: p.Path})
	}
	return nil
}

// perform runs one action through the tracker's own operations.
func (m *Manager) perform(ctx context.Context, r *Rule, a Action, is *domain.Issue, occurrence string, now time.Time) error {
	v := strings.TrimSpace(a.Value)
	switch a.Type {
	case ActionSetStatus:
		_, err := m.issues.Update(ctx, is.Key, service.IssueUpdate{Status: &v})
		return err
	case ActionSetPriority:
		_, err := m.issues.Update(ctx, is.Key, service.IssueUpdate{Priority: &v})
		return err
	case ActionSetAssignee:
		if v == "@reporter" {
			v = is.Reporter
		}
		_, err := m.issues.Update(ctx, is.Key, service.IssueUpdate{Assignee: &v})
		return err
	case ActionAddLabel, ActionRemoveLabel:
		cur, err := m.issues.Get(ctx, is.Key) // an earlier action may have changed them
		if err != nil {
			return err
		}
		labels := make([]string, 0, len(cur.Labels)+1)
		for _, l := range cur.Labels {
			if l != v {
				labels = append(labels, l)
			}
		}
		if a.Type == ActionAddLabel {
			labels = append(labels, v)
		}
		_, err = m.issues.Update(ctx, is.Key, service.IssueUpdate{Labels: &labels})
		return err
	case ActionSetDue:
		due := ""
		if v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			due = now.AddDate(0, 0, n).Format("2006-01-02")
		}
		_, err := m.issues.Update(ctx, is.Key, service.IssueUpdate{Due: &due})
		return err
	case ActionAddComment:
		_, _, err := m.issues.AddComment(ctx, is.Key, expand(a.Value, is), service.AutomationActor)
		return err
	case ActionCreateSubissue:
		_, err := m.issues.Create(ctx, service.IssueInput{
			Project: is.Project, Title: expand(a.Value, is), Parent: is.Key,
		}, service.AutomationActor)
		return err
	case ActionNotify:
		var to []string
		switch v {
		case "assignee":
			to = []string{is.Assignee}
		case "reporter":
			to = []string{is.Reporter}
		case "watchers":
			to = append([]string{is.Assignee, is.Reporter}, is.Watchers...)
		default:
			to = []string{strings.TrimPrefix(v, "@")}
		}
		_, err := m.notify.Emit(ctx, notify.Event{
			Kind: notify.Automation, Issue: is.Key, Ref: r.ID, Actor: service.AutomationActor, Recipients: to,
			Dedup: "automation:" + r.ID + ":" + occurrence + ":" + is.Key + ":" + v,
		})
		return err
	}
	return fmt.Errorf("неизвестное действие %q", a.Type)
}

// stepSchedules fires every rule that is due. A firing missed while the server was
// down runs once, not once per missed day. next_run moves only after the firing:
// a crash repeats it, and Claim keeps done issues from being touched again.
func (m *Manager) stepSchedules(ctx context.Context) error {
	rules, err := m.store.Enabled(ctx, "schedule")
	if err != nil {
		return err
	}
	now := nowFunc()
	for _, r := range rules {
		if r.NextRun == "" || r.Trigger.Schedule == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, r.NextRun)
		if err != nil {
			m.log.Warn("расписание правила не прочитано", "rule", r.ID, "err", err)
			continue
		}
		if at.After(now) {
			continue
		}
		if err := m.fire(ctx, r, now); err != nil {
			m.log.Warn("правило по расписанию не выполнено", "rule", r.ID, "err", err)
			continue
		}
		next, err := r.Trigger.Schedule.Next(now)
		if err != nil {
			return err
		}
		if err := m.store.SetNextRun(ctx, r.ID, r.NextRun, stamp(next)); err != nil {
			return err
		}
		if err := m.store.Prune(ctx, r.ID); err != nil {
			m.log.Warn("журнал правила не сокращён", "rule", r.ID, "err", err)
		}
	}
	return nil
}

func (m *Manager) fire(ctx context.Context, r *Rule, now time.Time) error {
	occurrence := "schedule:" + r.NextRun
	octx, why := m.asOwner(ctx, r)
	if why != "" {
		return m.record(ctx, r, occurrence, "", StatusBlocked, why, 0)
	}
	list, _, err := m.issues.List(octx, domain.IssueQuery{Project: r.Project})
	if err != nil {
		return err
	}
	today := now.In(r.location())
	var matched []*domain.Issue
	for _, is := range list {
		if Matches(r.Conditions, is, today) {
			matched = append(matched, is)
		}
	}
	note := ""
	if len(matched) > maxScheduledIssues {
		note = fmt.Sprintf("обработаны первые %d из %d тикетов", maxScheduledIssues, len(matched))
	}
	count := len(matched)
	if count > maxScheduledIssues {
		matched = matched[:maxScheduledIssues]
	}
	for _, is := range matched {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.applyTo(octx, ctx, r, occurrence, is); err != nil {
			m.log.Warn("правило не применено", "rule", r.ID, "issue", is.Key, "err", err)
		}
	}
	if err := m.record(ctx, r, occurrence, "", StatusDone, note, count); err != nil {
		return err
	}
	if p, err := m.issues.Project(octx, r.Project); err == nil {
		m.bus.Publish(service.Event{Type: "automation.run", ID: r.ID, Path: p.Path})
	}
	return nil
}
