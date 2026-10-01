package index

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/markdown"
)

// Memory is a straightforward in-memory index. A personal vault of tens of
// thousands of notes fits comfortably; it is replaced by SQLite in M1.
type Memory struct {
	mu       sync.RWMutex
	byPath   map[string]*domain.Note
	byID     map[string]*domain.Note
	tasks    map[string]*domain.Task
	external map[string][]domain.Event // события интеграций, по id интеграции
	rev      int64
	indexed  time.Time
	tookMS   int64
	root     string
}

func NewMemory(vaultPath string) *Memory {
	return &Memory{
		byPath:   map[string]*domain.Note{},
		byID:     map[string]*domain.Note{},
		tasks:    map[string]*domain.Task{},
		external: map[string][]domain.Event{},
		root:     vaultPath,
	}
}

func (m *Memory) PutNote(_ context.Context, n *domain.Note) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.byPath[n.Path]; ok {
		m.removeLocked(old)
		if old.Hash == n.Hash {
			n.Revision = old.Revision
		}
	}
	if n.Revision == 0 {
		m.rev++
		n.Revision = m.rev
	} else if n.Revision > m.rev {
		m.rev = n.Revision
	}
	m.byPath[n.Path] = n
	m.byID[n.ID] = n
	for _, t := range n.Tasks {
		m.tasks[t.ID] = t
	}
	return nil
}

func (m *Memory) removeLocked(n *domain.Note) {
	delete(m.byPath, n.Path)
	if cur, ok := m.byID[n.ID]; ok && cur == n {
		delete(m.byID, n.ID)
	}
	for _, t := range n.Tasks {
		if cur, ok := m.tasks[t.ID]; ok && cur == t {
			delete(m.tasks, t.ID)
		}
	}
}

func (m *Memory) DeleteNote(_ context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.byPath[p]; ok {
		m.removeLocked(n)
	}
	return nil
}

// Reset clears everything derived from files. External events survive: they
// come from integrations, not from the vault, and refetching them is expensive.
func (m *Memory) Reset(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byPath = map[string]*domain.Note{}
	m.byID = map[string]*domain.Note{}
	m.tasks = map[string]*domain.Task{}
	return nil
}

func (m *Memory) PutExternal(_ context.Context, integrationID string, events []domain.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(events) == 0 {
		delete(m.external, integrationID)
		return nil
	}
	m.external[integrationID] = events
	return nil
}

func (m *Memory) DeleteExternal(_ context.Context, integrationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.external, integrationID)
	return nil
}

// NoteBySource finds the note an integration created for an external item.
// The markers are flat frontmatter keys so they can be rewritten with the same
// lossless helper as any other scalar (ADR-0003).
func (m *Memory) NoteBySource(_ context.Context, owner, externalID string) (*domain.Note, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.byPath {
		if asString(n.Frontmatter["source_id"]) != externalID {
			continue
		}
		if asString(n.Frontmatter["source_integration"]) == owner ||
			asString(n.Frontmatter["source_connector"]) == owner {
			return n, nil
		}
	}
	return nil, ErrNotFound
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// NextRevision hands out the next global revision number.
func (m *Memory) NextRevision() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rev++
	return m.rev
}

func (m *Memory) Note(_ context.Context, idOrPath string) (*domain.Note, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if n, ok := m.byID[idOrPath]; ok {
		return n, nil
	}
	if n, ok := m.byPath[idOrPath]; ok {
		return n, nil
	}
	if n, ok := m.byPath[idOrPath+".md"]; ok {
		return n, nil
	}
	// Last resort: match by file name, the way a wikilink resolves.
	for _, n := range m.byPath {
		if strings.EqualFold(markdown.TitleFromPath(n.Path), idOrPath) {
			return n, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) Notes(_ context.Context, q domain.NoteQuery) ([]*domain.Note, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Note
	for _, n := range m.byPath {
		if q.Type != "" && n.Type != q.Type {
			continue
		}
		if q.PathPrefix != "" && !strings.HasPrefix(n.Path, q.PathPrefix) {
			continue
		}
		if !hasAllTags(n.Tags, q.Tags) {
			continue
		}
		if q.Text != "" && !strings.Contains(strings.ToLower(n.Title), strings.ToLower(q.Text)) {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	total := len(out)
	return page(out, q.Limit, q.Offset), total, nil
}

func (m *Memory) Backlinks(ctx context.Context, idOrPath string) ([]*domain.Note, error) {
	target, err := m.Note(ctx, idOrPath)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{
		strings.ToLower(target.ID):                              true,
		strings.ToLower(target.Path):                            true,
		strings.ToLower(strings.TrimSuffix(target.Path, ".md")): true,
		strings.ToLower(markdown.TitleFromPath(target.Path)):    true,
		strings.ToLower(target.Title):                           true,
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.Note
	for _, n := range m.byPath {
		if n.Path == target.Path {
			continue
		}
		for _, l := range n.Links {
			if names[strings.ToLower(strings.TrimSuffix(l.Target, ".md"))] {
				out = append(out, n)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (m *Memory) Task(_ context.Context, id string) (*domain.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.tasks[id]; ok {
		return t, nil
	}
	return nil, ErrNotFound
}

func (m *Memory) Tasks(_ context.Context, q domain.TaskQuery) ([]*domain.Task, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	today := domain.Today()
	var out []*domain.Task
	for _, t := range m.tasks {
		if !matchTask(t, q, today) {
			continue
		}
		out = append(out, t)
	}
	sortTasks(out, q.Sort)
	total := len(out)
	return page(out, q.Limit, q.Offset), total, nil
}

func matchTask(t *domain.Task, q domain.TaskQuery, today domain.Date) bool {
	if len(q.Status) > 0 && !containsStatus(q.Status, t.Status) {
		return false
	}
	if q.Overdue && !t.Overdue(today) {
		return false
	}
	if q.HasDue != nil && *q.HasDue == t.Due.IsZero() {
		return false
	}
	if !q.DueBefore.IsZero() && (t.Due.IsZero() || !t.Due.Day().Before(q.DueBefore.Day().AddDate(0, 0, 1))) {
		return false
	}
	if !q.DueAfter.IsZero() && (t.Due.IsZero() || t.Due.Day().Before(q.DueAfter.Day())) {
		return false
	}
	if !q.ScheduledFrom.IsZero() || !q.ScheduledTo.IsZero() {
		d := t.Scheduled
		if d.IsZero() {
			d = t.Due
		}
		if d.IsZero() {
			return false
		}
		if !q.ScheduledFrom.IsZero() && d.Day().Before(q.ScheduledFrom.Day()) {
			return false
		}
		if !q.ScheduledTo.IsZero() && q.ScheduledTo.Day().Before(d.Day()) {
			return false
		}
	}
	if q.Project != "" && !strings.EqualFold(t.Project, q.Project) {
		return false
	}
	if q.PathPrefix != "" && !strings.HasPrefix(t.NotePath, q.PathPrefix) {
		return false
	}
	if !hasAllTags(t.Tags, q.Tags) {
		return false
	}
	if q.Text != "" && !strings.Contains(strings.ToLower(t.Text), strings.ToLower(q.Text)) {
		return false
	}
	return true
}

func containsStatus(list []domain.TaskStatus, s domain.TaskStatus) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortTasks(ts []*domain.Task, keys []domain.SortKey) {
	if len(keys) == 0 {
		keys = []domain.SortKey{{Field: "due"}, {Field: "priority"}, {Field: "path"}}
	}
	sort.SliceStable(ts, func(i, j int) bool {
		for _, k := range keys {
			c := compareTasks(ts[i], ts[j], k.Field)
			if c == 0 {
				continue
			}
			if k.Desc {
				return c > 0
			}
			return c < 0
		}
		return ts[i].Line < ts[j].Line
	})
}

func compareTasks(a, b *domain.Task, field string) int {
	switch field {
	case "due", "scheduled":
		x, y := a.Due, b.Due
		if field == "scheduled" {
			x, y = a.Scheduled, b.Scheduled
		}
		// Undated tasks sort after dated ones.
		switch {
		case x.IsZero() && y.IsZero():
			return 0
		case x.IsZero():
			return 1
		case y.IsZero():
			return -1
		case x.T.Before(y.T):
			return -1
		case y.T.Before(x.T):
			return 1
		}
		return 0
	case "priority":
		return strings.Compare(prioRank(a.Priority), prioRank(b.Priority))
	case "status":
		return strings.Compare(string(a.Status), string(b.Status))
	case "text":
		return strings.Compare(strings.ToLower(a.Text), strings.ToLower(b.Text))
	case "path":
		if c := strings.Compare(a.NotePath, b.NotePath); c != 0 {
			return c
		}
		return a.Line - b.Line
	case "line":
		return a.Line - b.Line
	}
	return 0
}

// prioRank keeps unset priority last while A..D sort naturally.
func prioRank(p domain.Priority) string {
	if p == "" {
		return "Z"
	}
	return string(p)
}

func (m *Memory) Events(_ context.Context, from, to time.Time) ([]domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Event
	for _, n := range m.byPath {
		for _, e := range markdown.Events(n) {
			// [from, to) — the caller passes an exclusive end.
			if e.Start.Before(from) || !e.Start.Before(to) {
				continue
			}
			out = append(out, e)
		}
	}
	for _, events := range m.external {
		for _, e := range events {
			// [from, to) — the caller passes an exclusive end.
			if e.Start.Before(from) || !e.Start.Before(to) {
				continue
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].Title < out[j].Title
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out, nil
}

func (m *Memory) Search(_ context.Context, q string, limit int) ([]domain.SearchHit, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 30
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var hits []domain.SearchHit
	for _, n := range m.byPath {
		score := 0
		if strings.Contains(strings.ToLower(n.Title), q) {
			score += 10
		}
		if strings.Contains(strings.ToLower(n.Path), q) {
			score += 3
		}
		line, snippet := 0, ""
		for i, l := range strings.Split(n.Body, "\n") {
			if strings.Contains(strings.ToLower(l), q) {
				score += 2
				if snippet == "" {
					line, snippet = i, snip(l, q)
				}
			}
		}
		if score == 0 {
			continue
		}
		if snippet == "" {
			snippet = n.Title
		}
		hits = append(hits, domain.SearchHit{
			NoteID: n.ID, Path: n.Path, Title: n.Title,
			Snippet: snippet, Line: line, Score: score,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func snip(line, q string) string {
	l := strings.ToLower(line)
	i := strings.Index(l, q)
	start := max(0, i-40)
	end := min(len(line), i+len(q)+60)
	s := strings.TrimSpace(line[start:end])
	if start > 0 {
		s = "..." + s
	}
	if end < len(line) {
		s += "..."
	}
	return s
}

func (m *Memory) Tags(context.Context) ([]domain.TagCount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := map[string]int{}
	for _, n := range m.byPath {
		for _, t := range n.Tags {
			counts[t]++
		}
	}
	out := make([]domain.TagCount, 0, len(counts))
	for t, c := range counts {
		out = append(out, domain.TagCount{Tag: t, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Tag < out[j].Tag
		}
		return out[i].Count > out[j].Count
	})
	return out, nil
}

func (m *Memory) Stats(ctx context.Context) (domain.Stats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	open := 0
	for _, t := range m.tasks {
		if t.Status.Open() {
			open++
		}
	}
	tags := map[string]struct{}{}
	for _, n := range m.byPath {
		for _, t := range n.Tags {
			tags[t] = struct{}{}
		}
	}
	return domain.Stats{
		Notes: len(m.byPath), Tasks: len(m.tasks), OpenTasks: open, Tags: len(tags),
		LastIndexed: m.indexed, IndexTookMS: m.tookMS, VaultPath: m.root,
	}, nil
}

func (m *Memory) SetIndexed(_ context.Context, at time.Time, tookMS int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexed, m.tookMS = at, tookMS
}

// Paths returns every indexed note path; used by the reindexer to spot deletions.
func (m *Memory) Paths(context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.byPath))
	for p := range m.byPath {
		out = append(out, p)
	}
	return out, nil
}

func hasAllTags(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if strings.EqualFold(h, w) || strings.HasPrefix(strings.ToLower(h), strings.ToLower(w)+"/") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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
