package policy

import (
	"context"
	"sort"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
)

// Scoped is the index as the caller in ctx may see it (ADR-0012 §6): lists
// leave out what is hidden, and a direct lookup of a hidden object is "not
// found". Writes pass through; services check them with Policy.
type Scoped struct {
	index.Store
	p *Policy
}

// Scope wraps raw for request handling. Background jobs keep using raw.
func Scope(raw index.Store, p *Policy) *Scoped { return &Scoped{Store: raw, p: p} }

func (s *Scoped) reader(ctx context.Context) (*reader, error) { return s.p.reader(ctx) }

func (s *Scoped) visible(ctx context.Context, n *domain.Note) bool {
	r, err := s.reader(ctx)
	return err == nil && r.canSee(n)
}

func (s *Scoped) Note(ctx context.Context, idOrPath string) (*domain.Note, error) {
	n, err := s.Store.Note(ctx, idOrPath)
	if err != nil {
		return nil, err
	}
	if !s.visible(ctx, n) {
		return nil, index.ErrNotFound
	}
	return n, nil
}

func (s *Scoped) NoteBySource(ctx context.Context, owner, externalID string) (*domain.Note, error) {
	n, err := s.Store.NoteBySource(ctx, owner, externalID)
	if err != nil {
		return nil, err
	}
	if !s.visible(ctx, n) {
		return nil, index.ErrNotFound
	}
	return n, nil
}

func (s *Scoped) Notes(ctx context.Context, q domain.NoteQuery) ([]*domain.Note, int, error) {
	r, err := s.reader(ctx)
	if err != nil {
		return nil, 0, err
	}
	if r.all {
		return s.Store.Notes(ctx, q)
	}
	limit, offset := q.Limit, q.Offset
	q.Limit, q.Offset = 0, 0
	all, _, err := s.Store.Notes(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := all[:0:0]
	for _, n := range all {
		if r.canSee(n) {
			out = append(out, n)
		}
	}
	return page(out, limit, offset), len(out), nil
}

func (s *Scoped) Backlinks(ctx context.Context, idOrPath string) ([]*domain.Note, error) {
	if _, err := s.Note(ctx, idOrPath); err != nil {
		return nil, err
	}
	list, err := s.Store.Backlinks(ctx, idOrPath)
	if err != nil {
		return nil, err
	}
	r, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	out := list[:0:0]
	for _, n := range list {
		if r.canSee(n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// noteVisible looks a task's note up in the raw index.
func (s *Scoped) noteVisible(ctx context.Context, r *reader, notePath string) bool {
	if r.all {
		return true
	}
	n, err := s.Store.Note(ctx, notePath)
	return err == nil && r.canSee(n)
}

func (s *Scoped) Task(ctx context.Context, id string) (*domain.Task, error) {
	t, err := s.Store.Task(ctx, id)
	if err != nil {
		return nil, err
	}
	r, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if !s.noteVisible(ctx, r, t.NotePath) {
		return nil, index.ErrNotFound
	}
	return t, nil
}

func (s *Scoped) Tasks(ctx context.Context, q domain.TaskQuery) ([]*domain.Task, int, error) {
	r, err := s.reader(ctx)
	if err != nil {
		return nil, 0, err
	}
	if r.all {
		return s.Store.Tasks(ctx, q)
	}
	limit, offset := q.Limit, q.Offset
	q.Limit, q.Offset = 0, 0
	all, _, err := s.Store.Tasks(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	seen := map[string]bool{}
	out := all[:0:0]
	for _, t := range all {
		ok, known := seen[t.NotePath]
		if !known {
			ok = s.noteVisible(ctx, r, t.NotePath)
			seen[t.NotePath] = ok
		}
		if ok {
			out = append(out, t)
		}
	}
	return page(out, limit, offset), len(out), nil
}

func (s *Scoped) Events(ctx context.Context, from, to time.Time) ([]domain.Event, error) {
	list, err := s.Store.Events(ctx, from, to)
	if err != nil {
		return nil, err
	}
	r, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if r.all {
		return list, nil
	}
	out := list[:0:0]
	for _, e := range list {
		// Integration events have no path: they belong to the workspace.
		workspace := e.Path == "" && r.pr != nil && r.pr.Role >= RoleViewer
		if workspace || (e.Path != "" && s.noteVisible(ctx, r, e.Path)) {
			out = append(out, e)
		}
	}
	return out, nil
}

// searchWindow is how many raw hits are scanned to fill a filtered page.
const searchWindow = 1000

func (s *Scoped) Search(ctx context.Context, q string, limit int) ([]domain.SearchHit, error) {
	r, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if r.all {
		return s.Store.Search(ctx, q, limit)
	}
	if limit <= 0 {
		limit = 30
	}
	hits, err := s.Store.Search(ctx, q, searchWindow)
	if err != nil {
		return nil, err
	}
	out := hits[:0:0]
	for _, h := range hits {
		if s.noteVisible(ctx, r, h.Path) {
			out = append(out, h)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (s *Scoped) Tags(ctx context.Context) ([]domain.TagCount, error) {
	r, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if r.all {
		return s.Store.Tags(ctx)
	}
	notes, _, err := s.Notes(ctx, domain.NoteQuery{})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	var order []string
	for _, n := range notes {
		for _, t := range n.Tags {
			if counts[t] == 0 {
				order = append(order, t)
			}
			counts[t]++
		}
	}
	out := make([]domain.TagCount, 0, len(order))
	for _, t := range order {
		out = append(out, domain.TagCount{Tag: t, Count: counts[t]})
	}
	sortTags(out)
	return out, nil
}

func (s *Scoped) Stats(ctx context.Context) (domain.Stats, error) {
	st, err := s.Store.Stats(ctx)
	if err != nil {
		return st, err
	}
	r, err := s.reader(ctx)
	if err != nil {
		return st, err
	}
	if r.all {
		return st, nil
	}
	notes, _, err := s.Notes(ctx, domain.NoteQuery{})
	if err != nil {
		return st, err
	}
	tasks, _, err := s.Tasks(ctx, domain.TaskQuery{})
	if err != nil {
		return st, err
	}
	tags, _ := s.Tags(ctx)
	st.Notes, st.Tasks, st.Tags, st.OpenTasks = len(notes), len(tasks), len(tags), 0
	for _, t := range tasks {
		if t.Status.Open() {
			st.OpenTasks++
		}
	}
	return st, nil
}

// sortTags matches the index: most used first, then by name.
func sortTags(out []domain.TagCount) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Tag < out[j].Tag
		}
		return out[i].Count > out[j].Count
	})
}

func page[T any](in []T, limit, offset int) []T {
	if offset > 0 {
		if offset >= len(in) {
			return nil
		}
		in = in[offset:]
	}
	if limit > 0 && limit < len(in) {
		in = in[:limit]
	}
	return in
}
