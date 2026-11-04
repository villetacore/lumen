package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/markdown"
	"github.com/villetacore/lumen/server/internal/policy"
)

// ErrSprintState is returned for a start or completion that the sprint's
// state does not allow, such as a second active sprint on one board.
var ErrSprintState = errors.New("sprint state does not allow this")

// Sprints lists a project's sprints: active first, then planned by start,
// then closed with the newest first.
func (s *Issues) Sprints(ctx context.Context, project string) ([]*domain.Sprint, error) {
	project = strings.ToUpper(strings.TrimSpace(project))
	notes, _, err := s.store.Notes(ctx, domain.NoteQuery{Type: domain.TypeSprint})
	if err != nil {
		return nil, err
	}
	out := []*domain.Sprint{}
	for _, n := range notes {
		if sp := markdown.ParseSprint(n); sp != nil && (project == "" || sp.Project == project) {
			out = append(out, sp)
		}
	}
	rank := map[domain.SprintState]int{domain.SprintActive: 0, domain.SprintPlanned: 1, domain.SprintClosed: 2}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank[a.State] != rank[b.State] {
			return rank[a.State] < rank[b.State]
		}
		_, na, _ := markdown.SplitSprintID(a.ID)
		_, nb, _ := markdown.SplitSprintID(b.ID)
		if a.State == domain.SprintClosed {
			return na > nb
		}
		return na < nb
	})
	return out, nil
}

// Sprint returns one sprint by id ("WEB-S3").
func (s *Issues) Sprint(ctx context.Context, id string) (*domain.Sprint, error) {
	n, err := s.store.Note(ctx, strings.TrimSpace(id))
	if err != nil || n.Type != domain.TypeSprint {
		return nil, fmt.Errorf("sprint %s: %w", id, index.ErrNotFound)
	}
	return markdown.ParseSprint(n), nil
}

// SprintInput describes a new sprint or changed sprint fields; nil leaves a
// field alone.
type SprintInput struct {
	Name  *string
	Goal  *string
	Start *string
	End   *string
}

// CreateSprint allocates the next "<KEY>-S<n>" under the write lock, from the
// sprints on disk, so ids are never reused.
func (s *Issues) CreateSprint(ctx context.Context, project string, in SprintInput) (*domain.Sprint, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	p, err := s.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	if err := s.policy.CheckProject(ctx, p.Key, policy.RoleMember); err != nil {
		return nil, err
	}
	all, err := s.Sprints(ctx, p.Key)
	if err != nil {
		return nil, err
	}
	next := 0
	for _, sp := range all {
		if _, n, ok := markdown.SplitSprintID(sp.ID); ok && n > next {
			next = n
		}
	}
	var id, rel string
	for {
		next++
		id = fmt.Sprintf("%s-S%d", p.Key, next)
		rel = markdown.SprintPath(p, id)
		if !s.vault.Exists(rel) {
			break
		}
	}
	name := fmt.Sprintf("Спринт %d", next)
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = strings.TrimSpace(*in.Name)
	}
	lines := []string{"---", "---", ""}
	set := func(k string, v any) { lines = markdown.SetFrontmatterValue(lines, k, v) }
	set("id", id)
	set("title", name)
	set("type", "sprint")
	set("project", p.Key)
	set("state", string(domain.SprintPlanned))
	if err := setSprintFields(set, in); err != nil {
		return nil, err
	}
	set("created", nowFunc().UTC().Format(time.RFC3339))
	if err := s.write(ctx, rel, "\n", lines); err != nil {
		return nil, err
	}
	s.bus.Publish(Event{Type: "sprint.changed", ID: id, Path: rel})
	return s.Sprint(ctx, id)
}

func setSprintFields(set func(string, any), in SprintInput) error {
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return fmt.Errorf("%w: sprint name is required", ErrInvalid)
		}
		set("title", strings.TrimSpace(*in.Name))
	}
	if in.Goal != nil {
		set("goal", strings.TrimSpace(*in.Goal))
	}
	for k, v := range map[string]*string{"start": in.Start, "end": in.End} {
		if v == nil {
			continue
		}
		val := strings.TrimSpace(*v)
		if val != "" {
			d, err := parseDateInput(val)
			if err != nil {
				return fmt.Errorf("%w: bad %s: %v", ErrInvalid, k, err)
			}
			val = d.String()
		}
		set(k, val)
	}
	return nil
}

// UpdateSprint changes name, goal or dates.
func (s *Issues) UpdateSprint(ctx context.Context, id string, in SprintInput) (*domain.Sprint, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	return s.editSprint(ctx, id, func(_ *domain.Sprint, set func(string, any)) error {
		return setSprintFields(set, in)
	})
}

// StartSprint makes a planned sprint the board's active one. A project has at
// most one active sprint; dates default to today and two weeks later.
func (s *Issues) StartSprint(ctx context.Context, id string, in SprintInput) (*domain.Sprint, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	sp, err := s.Sprint(ctx, id)
	if err != nil {
		return nil, err
	}
	all, err := s.Sprints(ctx, sp.Project)
	if err != nil {
		return nil, err
	}
	for _, other := range all {
		if other.State == domain.SprintActive && other.ID != sp.ID {
			return nil, fmt.Errorf("%w: %s is already active", ErrSprintState, other.ID)
		}
	}
	if sp.State != domain.SprintPlanned {
		return nil, fmt.Errorf("%w: %s is %s", ErrSprintState, sp.ID, sp.State)
	}
	today := domain.Date{T: nowFunc()}
	if in.Start == nil && sp.Start.IsZero() {
		v := today.Day().Format("2006-01-02")
		in.Start = &v
	}
	if in.End == nil && sp.End.IsZero() {
		v := today.Day().AddDate(0, 0, 14).Format("2006-01-02")
		in.End = &v
	}
	return s.editSprint(ctx, id, func(_ *domain.Sprint, set func(string, any)) error {
		set("state", string(domain.SprintActive))
		return setSprintFields(set, in)
	})
}

// CompleteSprint closes the active sprint. Unfinished issues move to another
// planned sprint or, with an empty moveTo, back to the backlog; finished ones
// keep the sprint for reports. Issues are moved before the sprint is marked
// closed, so repeating the call after a crash finishes the job.
func (s *Issues) CompleteSprint(ctx context.Context, id, moveTo string) (*domain.Sprint, int, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	sp, err := s.Sprint(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if err := s.policy.CheckProject(ctx, sp.Project, policy.RoleMember); err != nil {
		return nil, 0, err
	}
	if sp.State != domain.SprintActive {
		return nil, 0, fmt.Errorf("%w: %s is %s", ErrSprintState, sp.ID, sp.State)
	}
	if moveTo != "" {
		dst, err := s.Sprint(ctx, moveTo)
		if err != nil {
			return nil, 0, err
		}
		if dst.Project != sp.Project || dst.State != domain.SprintPlanned {
			return nil, 0, fmt.Errorf("%w: unfinished issues can go only to a planned sprint of %s", ErrSprintState, sp.Project)
		}
	}
	issues, _, err := s.List(ctx, domain.IssueQuery{Project: sp.Project, Sprint: sp.ID})
	if err != nil {
		return nil, 0, err
	}
	moved := 0
	for _, is := range issues {
		if is.StatusCategory == domain.CategoryDone {
			continue
		}
		dst := moveTo
		if _, err := s.edit(ctx, is.Key, 0, func(_ *domain.Issue, _ *domain.TrackerProject, lines []string, _ int) ([]string, error) {
			return markdown.SetFrontmatterValue(lines, "sprint", dst), nil
		}); err != nil {
			return nil, moved, fmt.Errorf("move %s: %w", is.Key, err)
		}
		moved++
	}
	closed, err := s.editSprint(ctx, id, func(_ *domain.Sprint, set func(string, any)) error {
		set("state", string(domain.SprintClosed))
		set("completed", nowFunc().Format("2006-01-02"))
		return nil
	})
	return closed, moved, err
}

func (s *Issues) editSprint(ctx context.Context, id string, fn func(*domain.Sprint, func(string, any)) error) (*domain.Sprint, error) {
	sp, err := s.Sprint(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.policy.CheckProject(ctx, sp.Project, policy.RoleMember); err != nil {
		return nil, err
	}
	data, err := s.vault.Read(sp.Path)
	if err != nil {
		return nil, err
	}
	eol, lines := splitLines(data)
	if err := fn(sp, func(k string, v any) { lines = markdown.SetFrontmatterValue(lines, k, v) }); err != nil {
		return nil, err
	}
	if err := s.write(ctx, sp.Path, eol, lines); err != nil {
		return nil, err
	}
	s.bus.Publish(Event{Type: "sprint.changed", ID: sp.ID, Path: sp.Path})
	return s.Sprint(ctx, sp.ID)
}
