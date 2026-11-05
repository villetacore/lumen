package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/markdown"
	"github.com/villetacore/lumen/server/internal/notify"
	"github.com/villetacore/lumen/server/internal/policy"
	"github.com/villetacore/lumen/server/internal/vault"
)

var (
	ErrBadTransition = errors.New("workflow does not allow this transition")
	ErrInvalid       = errors.New("invalid input")
)

// Issues implements the tracker use cases (ADR-0010). Issues are notes, so
// every write goes through the vault and the shared vaultWrites lock, and the
// index stays the only read model.
type Issues struct {
	vault  *vault.Vault
	store  index.Store
	ix     *Indexer
	bus    *Bus
	log    *slog.Logger
	policy *policy.Policy // nil: no checks (background jobs, tests)
	notify *notify.Store  // nil: no notifications
}

// WithNotify reports assignments, mentions, comments and status changes.
func (s *Issues) WithNotify(n *notify.Store) *Issues {
	s.notify = n
	return s
}

// WithPolicy makes every write check the caller's project role (ADR-0012).
func (s *Issues) WithPolicy(p *policy.Policy) *Issues {
	s.policy = p
	return s
}

func NewIssues(v *vault.Vault, st index.Store, ix *Indexer, bus *Bus, log *slog.Logger) *Issues {
	return &Issues{vault: v, store: st, ix: ix, bus: bus, log: log}
}

// Projects lists project notes that carry an issue key.
func (s *Issues) Projects(ctx context.Context) ([]*domain.TrackerProject, error) {
	notes, _, err := s.store.Notes(ctx, domain.NoteQuery{Type: domain.TypeProject})
	if err != nil {
		return nil, err
	}
	out := []*domain.TrackerProject{}
	for _, n := range notes {
		if p, ok := markdown.ParseProject(n); ok {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	// A parent the caller cannot see stays unnamed: the child shows as top-level.
	visible := make(map[string]bool, len(out))
	for _, p := range out {
		visible[p.Key] = true
	}
	for _, p := range out {
		if !visible[p.Parent] {
			p.Parent = ""
		}
	}
	return out, nil
}

// Project finds a tracker project by key.
func (s *Issues) Project(ctx context.Context, key string) (*domain.TrackerProject, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	all, err := s.Projects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.Key == key {
			return p, nil
		}
	}
	return nil, fmt.Errorf("project %s: %w", key, index.ErrNotFound)
}

// ProjectInput creates a tracker project, or turns an existing project note
// into one when Note is set.
type ProjectInput struct {
	Key    string
	Title  string
	Note   string
	Parent string // key of the parent project
}

func (s *Issues) CreateProject(ctx context.Context, in ProjectInput) (*domain.TrackerProject, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	key := strings.ToUpper(strings.TrimSpace(in.Key))
	if !markdown.ValidProjectKey(key) {
		return nil, fmt.Errorf("%w: key must be 1–10 latin capitals or digits, starting with a letter", ErrInvalid)
	}
	if err := s.policy.CheckWorkspace(ctx, policy.RoleMember); err != nil {
		return nil, err
	}
	if _, err := s.Project(ctx, key); err == nil {
		return nil, fmt.Errorf("%w: project key %s", errAlreadyExists, key)
	}
	parent := strings.ToUpper(strings.TrimSpace(in.Parent))
	if parent != "" {
		if _, err := s.Project(ctx, parent); err != nil {
			return nil, err
		}
		if err := s.policy.CheckProject(ctx, parent, policy.RoleMember); err != nil {
			return nil, err
		}
		if err := s.checkProjectParent(ctx, key, parent); err != nil {
			return nil, err
		}
	}

	var rel string
	var lines []string
	eol := "\n"
	if in.Note != "" {
		n, err := s.store.Note(ctx, in.Note)
		if err != nil {
			return nil, err
		}
		if n.Type != domain.TypeProject {
			return nil, fmt.Errorf("%w: %s is not a project", ErrInvalid, n.Path)
		}
		if err := s.policy.CheckNote(ctx, n, policy.RoleMember); err != nil {
			return nil, err
		}
		data, err := s.vault.Read(n.Path)
		if err != nil {
			return nil, err
		}
		rel = n.Path
		eol, lines = splitLines(data)
		lines = markdown.SetFrontmatterValue(lines, "key", key)
		if _, has := n.Frontmatter["workflow"]; !has {
			lines = markdown.SetFrontmatterValue(lines, "workflow", workflowYAML(domain.DefaultWorkflow()))
		}
	} else {
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: title is required", ErrInvalid)
		}
		rel = path.Join("20-projects", strings.ToLower(key)+".md")
		if s.vault.Exists(rel) {
			return nil, fmt.Errorf("%w: %s", errAlreadyExists, rel)
		}
		lines = []string{"---", "---", "", "# " + title, ""}
		lines = markdown.SetFrontmatterValue(lines, "title", title)
		lines = markdown.SetFrontmatterValue(lines, "type", "project")
		lines = markdown.SetFrontmatterValue(lines, "key", key)
		lines = markdown.SetFrontmatterValue(lines, "created", nowFunc().Format(time.RFC3339))
		lines = markdown.SetFrontmatterValue(lines, "workflow", workflowYAML(domain.DefaultWorkflow()))
	}
	lines = markdown.SetFrontmatterValue(lines, "parent_project", parent)
	if err := s.write(ctx, rel, eol, lines); err != nil {
		return nil, err
	}
	return s.Project(ctx, key)
}

// maxProjectDepth keeps project trees readable in the menu.
const maxProjectDepth = 8

// projectParents is the whole project tree, hidden parts included: a cycle through
// a project the caller cannot see is still a cycle.
func (s *Issues) projectParents(ctx context.Context) (map[string]string, error) {
	if s.policy != nil {
		return s.policy.ProjectParents(ctx)
	}
	all, err := s.Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for _, p := range all {
		out[p.Key] = p.Parent
	}
	return out, nil
}

// checkProjectParent refuses a parent that would make a cycle or too deep a tree.
func (s *Issues) checkProjectParent(ctx context.Context, key, parent string) error {
	if parent == key {
		return fmt.Errorf("%w: проект не может быть своим родителем", ErrInvalid)
	}
	parents, err := s.projectParents(ctx)
	if err != nil {
		return err
	}
	depth := 1
	seen := map[string]bool{}
	for k := parent; k != "" && !seen[k]; k = parents[k] {
		if k == key {
			return fmt.Errorf("%w: %s уже вложен в %s", ErrInvalid, parent, key)
		}
		seen[k] = true
		depth++
	}
	var height func(k string, visited map[string]bool) int
	height = func(k string, visited map[string]bool) int {
		best := 0
		visited[k] = true
		for child, up := range parents {
			if up == k && !visited[child] {
				if h := height(child, visited) + 1; h > best {
					best = h
				}
			}
		}
		return best
	}
	if depth+height(key, map[string]bool{}) > maxProjectDepth {
		return fmt.Errorf("%w: вложенность проектов — не больше %d уровней", ErrInvalid, maxProjectDepth)
	}
	return nil
}

// ProjectUpdate changes the named project fields; nil leaves a field alone and
// an empty parent makes the project top-level.
type ProjectUpdate struct {
	Title    *string
	Parent   *string
	Revision int64
}

// UpdateProject renames a project or moves it in the tree. Files stay where they
// are: the tree lives in the "parent_project" key, so no link or issue path changes.
func (s *Issues) UpdateProject(ctx context.Context, key string, up ProjectUpdate) (*domain.TrackerProject, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	p, err := s.Project(ctx, key)
	if err != nil {
		return nil, err
	}
	if up.Revision != 0 && up.Revision != p.Revision {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrConflict, p.Revision, up.Revision)
	}
	if err := s.policy.CheckProject(ctx, p.Key, policy.RoleMember); err != nil {
		return nil, err
	}
	data, err := s.vault.Read(p.Path)
	if err != nil {
		return nil, err
	}
	eol, lines := splitLines(data)
	if up.Title != nil {
		t := strings.TrimSpace(*up.Title)
		if t == "" {
			return nil, fmt.Errorf("%w: title is required", ErrInvalid)
		}
		lines = markdown.SetFrontmatterValue(lines, "title", t)
	}
	if up.Parent != nil {
		parent := strings.ToUpper(strings.TrimSpace(*up.Parent))
		if parent != "" {
			if _, err := s.Project(ctx, parent); err != nil {
				return nil, err
			}
			if err := s.checkProjectParent(ctx, p.Key, parent); err != nil {
				return nil, err
			}
		}
		if err := s.policy.CheckReparent(ctx, p.Key, parent); err != nil {
			return nil, err
		}
		lines = markdown.SetFrontmatterValue(lines, "parent_project", parent)
	}
	if err := s.write(ctx, p.Path, eol, lines); err != nil {
		return nil, err
	}
	return s.Project(ctx, p.Key)
}

// workflowItem keeps the YAML keys in reading order: id, name, category.
type workflowItem struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Category string `yaml:"category"`
	WIP      int    `yaml:"wip,omitempty"`
}

func workflowYAML(wf []domain.WorkflowStatus) []workflowItem {
	out := make([]workflowItem, len(wf))
	for i, st := range wf {
		out[i] = workflowItem{ID: st.ID, Name: st.Name, Category: string(st.Category), WIP: st.WIP}
	}
	return out
}

// List returns issues in board order: rank, then key number.
func (s *Issues) List(ctx context.Context, q domain.IssueQuery) ([]*domain.Issue, int, error) {
	all, err := s.all(ctx)
	if err != nil {
		return nil, 0, err
	}
	projects := map[string]*domain.TrackerProject{}
	if ps, err := s.Projects(ctx); err == nil {
		for _, p := range ps {
			projects[p.Key] = p
		}
	}
	var inTree map[string]bool
	if q.Subprojects && q.Project != "" {
		inTree = subtree(projects, strings.ToUpper(q.Project))
		q.Project = ""
	}
	var out []*domain.Issue
	for _, is := range all {
		if p := projects[is.Project]; p != nil {
			if st, ok := p.Status(is.Status); ok {
				is.StatusCategory = st.Category
			}
		}
		if inTree != nil && !inTree[is.Project] {
			continue
		}
		if matchIssue(is, q) {
			out = append(out, is)
		}
	}
	sortIssues(out)
	total := len(out)
	if q.Offset > 0 {
		if q.Offset >= len(out) {
			out = nil
		} else {
			out = out[q.Offset:]
		}
	}
	if q.Limit > 0 && q.Limit < len(out) {
		out = out[:q.Limit]
	}
	return out, total, nil
}

// subtree is a project and everything nested under it.
func subtree(projects map[string]*domain.TrackerProject, root string) map[string]bool {
	out := map[string]bool{root: true}
	for grew := true; grew; {
		grew = false
		for k, p := range projects {
			if out[p.Parent] && !out[k] {
				out[k], grew = true, true
			}
		}
	}
	return out
}

func (s *Issues) all(ctx context.Context) ([]*domain.Issue, error) {
	notes, _, err := s.store.Notes(ctx, domain.NoteQuery{Type: domain.TypeIssue})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Issue, 0, len(notes))
	for _, n := range notes {
		if is := markdown.ParseIssue(n); is != nil {
			out = append(out, is)
		}
	}
	return out, nil
}

func matchIssue(is *domain.Issue, q domain.IssueQuery) bool {
	if q.Project != "" && !strings.EqualFold(is.Project, q.Project) {
		return false
	}
	if len(q.Status) > 0 && !contains(q.Status, is.Status) {
		return false
	}
	if q.Category != "" && is.StatusCategory != q.Category {
		return false
	}
	if q.Assignee != "" && is.Assignee != q.Assignee && !(q.Assignee == "none" && is.Assignee == "") {
		return false
	}
	if q.Watcher != "" && !contains(watchersOf(is), q.Watcher) {
		return false
	}
	if q.Type != "" && is.Type != q.Type {
		return false
	}
	if q.Sprint != "" && is.Sprint != q.Sprint {
		return false
	}
	if q.Epic != "" && is.Epic != q.Epic {
		return false
	}
	if q.Parent != "" && is.Parent != q.Parent {
		return false
	}
	for _, l := range q.Labels {
		if !contains(is.Labels, l) {
			return false
		}
	}
	if t := strings.ToLower(strings.TrimSpace(q.Text)); t != "" &&
		!strings.Contains(strings.ToLower(is.Title), t) && !strings.EqualFold(is.Key, t) {
		return false
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func sortIssues(list []*domain.Issue) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		// Unranked issues (created outside Lumen) go last, in key order.
		if (a.Rank == "") != (b.Rank == "") {
			return a.Rank != ""
		}
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		pa, na, _ := markdown.SplitIssueKey(a.Key)
		pb, nb, _ := markdown.SplitIssueKey(b.Key)
		if pa != pb {
			return pa < pb
		}
		return na < nb
	})
}

// Get returns one issue with its description, checklist and comments.
func (s *Issues) Get(ctx context.Context, key string) (*domain.Issue, error) {
	n, err := s.note(ctx, key)
	if err != nil {
		return nil, err
	}
	is := markdown.ParseIssue(n)
	if p, err := s.Project(ctx, is.Project); err == nil {
		if st, ok := p.Status(is.Status); ok {
			is.StatusCategory = st.Category
		}
	}
	return is, nil
}

func (s *Issues) note(ctx context.Context, key string) (*domain.Note, error) {
	key = strings.TrimSpace(key)
	n, err := s.store.Note(ctx, key)
	if err != nil || n.Type != domain.TypeIssue {
		return nil, fmt.Errorf("issue %s: %w", key, index.ErrNotFound)
	}
	return n, nil
}

// IssueInput is a new issue. Empty fields take project defaults.
type IssueInput struct {
	Project     string
	Title       string
	Type        string
	Status      string
	Priority    string
	Assignee    string
	Labels      []string
	Epic        string
	Parent      string
	Sprint      string
	Start       string
	Due         string
	Estimate    string
	Points      float64
	Description string
}

// Create allocates the next key under the write lock. The number is the
// highest of the project's counter, the indexed issues and the files on disk,
// so neither a lost index nor a restart can hand out a key twice.
func (s *Issues) Create(ctx context.Context, in IssueInput, actor string) (*domain.Issue, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	p, err := s.Project(ctx, in.Project)
	if err != nil {
		return nil, err
	}
	if err := s.policy.CheckProject(ctx, p.Key, policy.RoleMember); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	if in.Type == "" {
		in.Type = p.IssueTypes[0]
	} else if !contains(p.IssueTypes, in.Type) {
		return nil, fmt.Errorf("%w: unknown issue type %q", ErrInvalid, in.Type)
	}
	if in.Status == "" {
		in.Status = p.Workflow[0].ID
	} else if _, ok := p.Status(in.Status); !ok {
		return nil, fmt.Errorf("%w: unknown status %q", ErrInvalid, in.Status)
	}
	if err := s.checkSprint(ctx, p, strings.TrimSpace(in.Sprint)); err != nil {
		return nil, err
	}
	in.Parent = strings.TrimSpace(in.Parent)
	if err := s.checkIssueParent(ctx, p, "", in.Parent); err != nil {
		return nil, err
	}

	all, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	next, lastRank := p.Seq, ""
	for _, is := range all {
		if pk, num, ok := markdown.SplitIssueKey(is.Key); ok && pk == p.Key && num > next {
			next = num
		}
		if is.Project == p.Key && is.Rank > lastRank {
			lastRank = is.Rank
		}
	}
	var key, rel string
	for {
		next++
		key = fmt.Sprintf("%s-%d", p.Key, next)
		rel = markdown.IssuePath(p, key)
		if !s.vault.Exists(rel) {
			break
		}
	}

	now := nowFunc()
	lines := []string{"---", "---", ""}
	set := func(k string, v any) { lines = markdown.SetFrontmatterValue(lines, k, v) }
	set("id", key)
	set("title", title)
	set("type", "issue")
	set("project", p.Key)
	set("issue_type", in.Type)
	set("status", in.Status)
	set("priority", in.Priority)
	set("assignee", in.Assignee)
	set("reporter", actor)
	set("labels", in.Labels)
	set("epic", in.Epic)
	set("parent", in.Parent)
	set("sprint", in.Sprint)
	for k, v := range map[string]string{"start": in.Start, "due": in.Due} {
		if v = strings.TrimSpace(v); v != "" {
			d, err := parseDateInput(v)
			if err != nil {
				return nil, fmt.Errorf("%w: bad %s: %v", ErrInvalid, k, err)
			}
			v = d.String()
		}
		set(k, v)
	}
	set("estimate", in.Estimate)
	if in.Points > 0 {
		set("points", in.Points)
	}
	set("rank", markdown.RankBetween(lastRank, ""))
	set("created", now.UTC().Format(time.RFC3339))
	set("updated", now.UTC().Format(time.RFC3339))
	if d := strings.TrimSpace(in.Description); d != "" {
		lines = append(lines, strings.Split(strings.ReplaceAll(d, "\r\n", "\n"), "\n")...)
		lines = append(lines, "")
	}

	if err := s.write(ctx, rel, "\n", lines); err != nil {
		return nil, err
	}
	// The counter is a guard against reusing keys of deleted issues; the file
	// written above already makes the number taken.
	if err := s.setProjectSeq(ctx, p, next); err != nil {
		s.log.Warn("issue counter not saved", "project", p.Key, "err", err)
	}
	s.bus.Publish(Event{Type: "issue.changed", ID: key, Path: rel})
	created, err := s.Get(ctx, key)
	if err == nil {
		s.notifyChange(ctx, &domain.Issue{Key: key}, created, actor)
	}
	return created, err
}

func (s *Issues) setProjectSeq(ctx context.Context, p *domain.TrackerProject, seq int) error {
	data, err := s.vault.Read(p.Path)
	if err != nil {
		return err
	}
	eol, lines := splitLines(data)
	lines = markdown.SetFrontmatterValue(lines, "issue_seq", seq)
	return s.write(ctx, p.Path, eol, lines)
}

// IssueUpdate changes the named fields. nil leaves a field alone; an empty
// string or list clears it.
type IssueUpdate struct {
	Title       *string
	Type        *string
	Status      *string
	Priority    *string
	Assignee    *string
	Labels      *[]string
	Watchers    *[]string
	Epic        *string
	Parent      *string
	Sprint      *string
	Start       *string
	Due         *string
	Estimate    *string
	Points      *float64
	Description *string
	Revision    int64
}

func (s *Issues) Update(ctx context.Context, key string, up IssueUpdate) (*domain.Issue, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	return s.edit(ctx, key, up.Revision, func(is *domain.Issue, p *domain.TrackerProject, lines []string, bodyStart int) ([]string, error) {
		set := func(k string, v any) { lines = markdown.SetFrontmatterValue(lines, k, v) }
		if up.Title != nil {
			t := strings.TrimSpace(*up.Title)
			if t == "" {
				return nil, fmt.Errorf("%w: title is required", ErrInvalid)
			}
			set("title", t)
		}
		if up.Type != nil {
			if !contains(p.IssueTypes, *up.Type) {
				return nil, fmt.Errorf("%w: unknown issue type %q", ErrInvalid, *up.Type)
			}
			set("issue_type", *up.Type)
		}
		if up.Status != nil {
			if err := checkMove(p, is.Status, *up.Status); err != nil {
				return nil, err
			}
			set("status", *up.Status)
		}
		if up.Sprint != nil && strings.TrimSpace(*up.Sprint) != is.Sprint {
			if err := s.checkSprint(ctx, p, strings.TrimSpace(*up.Sprint)); err != nil {
				return nil, err
			}
			set("sprint", strings.TrimSpace(*up.Sprint))
		}
		if up.Parent != nil && strings.TrimSpace(*up.Parent) != is.Parent {
			parent := strings.TrimSpace(*up.Parent)
			if err := s.checkIssueParent(ctx, p, is.Key, parent); err != nil {
				return nil, err
			}
			set("parent", parent)
		}
		for k, v := range map[string]*string{
			"priority": up.Priority, "assignee": up.Assignee, "epic": up.Epic, "estimate": up.Estimate,
		} {
			if v != nil {
				set(k, strings.TrimSpace(*v))
			}
		}
		for k, v := range map[string]*string{"start": up.Start, "due": up.Due} {
			if v == nil {
				continue
			}
			val := strings.TrimSpace(*v)
			if val != "" {
				d, err := parseDateInput(val)
				if err != nil {
					return nil, fmt.Errorf("%w: bad %s: %v", ErrInvalid, k, err)
				}
				val = d.String()
			}
			set(k, val)
		}
		if up.Labels != nil {
			set("labels", *up.Labels)
		}
		if up.Watchers != nil {
			set("watchers", *up.Watchers)
		}
		if up.Points != nil {
			if *up.Points > 0 {
				set("points", *up.Points)
			} else {
				set("points", nil)
			}
		}
		if up.Description != nil {
			// Frontmatter edits above may have changed its length.
			_, bodyStart, _ = markdown.SplitFrontmatter(lines)
			lines = replaceDescription(lines, bodyStart, *up.Description)
		}
		return lines, nil
	})
}

// checkIssueParent accepts no parent or an issue of the same project that is not
// the issue itself or nested under it. key is "" for an issue not created yet.
func (s *Issues) checkIssueParent(ctx context.Context, p *domain.TrackerProject, key, parent string) error {
	if parent == "" {
		return nil
	}
	seen := map[string]bool{}
	for k := parent; k != "" && !seen[k]; {
		if k == key {
			return fmt.Errorf("%w: подзадача не может быть родителем своего тикета", ErrInvalid)
		}
		seen[k] = true
		n, err := s.note(ctx, k)
		if err != nil {
			if k == parent {
				return err
			}
			break // a dangling link further up cannot make a cycle
		}
		up := markdown.ParseIssue(n)
		if k == parent && up.Project != p.Key {
			return fmt.Errorf("%w: родитель %s из другого проекта", ErrInvalid, parent)
		}
		k = up.Parent
	}
	return nil
}

// checkSprint accepts the backlog ("") or an open sprint of the same project.
func (s *Issues) checkSprint(ctx context.Context, p *domain.TrackerProject, id string) error {
	if id == "" {
		return nil
	}
	sp, err := s.Sprint(ctx, id)
	if err != nil {
		return err
	}
	if sp.Project != p.Key {
		return fmt.Errorf("%w: sprint %s belongs to %s", ErrInvalid, sp.ID, sp.Project)
	}
	if sp.State == domain.SprintClosed {
		return fmt.Errorf("%w: sprint %s is closed", ErrSprintState, sp.ID)
	}
	return nil
}

func checkMove(p *domain.TrackerProject, from, to string) error {
	if _, ok := p.Status(to); !ok {
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, to)
	}
	if !p.CanMove(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrBadTransition, from, to)
	}
	return nil
}

// replaceDescription swaps everything before "## Комментарии", keeping the
// comments section byte for byte.
func replaceDescription(lines []string, bodyStart int, desc string) []string {
	body := lines[bodyStart:]
	descEnd, _ := markdown.SplitIssueBody(body)
	out := append([]string{}, lines[:bodyStart]...)
	out = append(out, "")
	if d := strings.TrimSpace(strings.ReplaceAll(desc, "\r\n", "\n")); d != "" {
		out = append(out, strings.Split(d, "\n")...)
		out = append(out, "")
	}
	if descEnd < len(body) {
		out = append(out, body[descEnd:]...)
	}
	return out
}

// MoveInput places an issue on the board: a new status and the neighbours it
// was dropped between (empty: top or bottom of the column).
type MoveInput struct {
	Status   string
	Before   string  // key of the issue now directly above
	After    string  // key of the issue now directly below
	Sprint   *string // set: move to this sprint; "" is the backlog
	Revision int64
}

func (s *Issues) Move(ctx context.Context, key string, in MoveInput) (*domain.Issue, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	rankOf := func(k string) (string, error) {
		if k == "" {
			return "", nil
		}
		n, err := s.note(ctx, k)
		if err != nil {
			return "", err
		}
		return markdown.ParseIssue(n).Rank, nil
	}
	prev, err := rankOf(in.Before)
	if err != nil {
		return nil, err
	}
	next, err := rankOf(in.After)
	if err != nil {
		return nil, err
	}
	return s.edit(ctx, key, in.Revision, func(is *domain.Issue, p *domain.TrackerProject, lines []string, _ int) ([]string, error) {
		if in.Status != "" && in.Status != is.Status {
			if err := checkMove(p, is.Status, in.Status); err != nil {
				return nil, err
			}
			lines = markdown.SetFrontmatterValue(lines, "status", in.Status)
		}
		if in.Sprint != nil && *in.Sprint != is.Sprint {
			if err := s.checkSprint(ctx, p, *in.Sprint); err != nil {
				return nil, err
			}
			lines = markdown.SetFrontmatterValue(lines, "sprint", *in.Sprint)
		}
		if in.Before != "" || in.After != "" {
			lines = markdown.SetFrontmatterValue(lines, "rank", markdown.RankBetween(prev, next))
		}
		return lines, nil
	})
}

// AddComment appends a comment section, creating "## Комментарии" if needed.
func (s *Issues) AddComment(ctx context.Context, key, body, actor string) (*domain.Issue, *domain.Comment, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return nil, nil, fmt.Errorf("%w: empty comment", ErrInvalid)
	}
	id := "c-" + newBlockID()
	is, err := s.edit(ctx, key, 0, func(_ *domain.Issue, _ *domain.TrackerProject, lines []string, bodyStart int) ([]string, error) {
		for len(lines) > bodyStart && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if _, start := markdown.SplitIssueBody(lines[bodyStart:]); start < 0 {
			lines = append(lines, "", markdown.CommentsHeading)
		}
		lines = append(lines, "", markdown.CommentHeading(nowFunc(), actor, id))
		lines = append(lines, strings.Split(body, "\n")...)
		return append(lines, ""), nil
	})
	if err != nil {
		return nil, nil, err
	}
	// Mentioned people hear about the mention; other watchers about the comment.
	if s.notify != nil {
		mentioned := notify.Mentions(body)
		skip := map[string]bool{}
		for _, m := range mentioned {
			skip[m] = true
		}
		var watchers []string
		for _, w := range watchersOf(is) {
			if !skip[w] {
				watchers = append(watchers, w)
			}
		}
		s.emit(ctx,
			notify.Event{Kind: notify.Mentioned, Issue: is.Key, Ref: id, Actor: actor, Recipients: mentioned, Dedup: "mention:" + is.Key + ":" + id},
			notify.Event{Kind: notify.Commented, Issue: is.Key, Ref: id, Actor: actor, Recipients: watchers, Dedup: "comment:" + is.Key + ":" + id})
	}
	for _, c := range is.Comments {
		if c.ID == id {
			return is, c, nil
		}
	}
	return is, nil, nil
}

// Delete moves the issue file to the vault trash. Its number stays taken.
func (s *Issues) Delete(ctx context.Context, key string) error {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	n, err := s.note(ctx, key)
	if err != nil {
		return err
	}
	if err := s.policy.CheckNote(ctx, n, policy.RoleMember); err != nil {
		return err
	}
	// What hung on this issue stays in the same place of the tree and the same
	// project: sub-issues move up, documents go to the project.
	is := markdown.ParseIssue(n)
	children, err := s.all(ctx)
	if err != nil {
		return err
	}
	for _, c := range children {
		if c.Parent != is.Key {
			continue
		}
		parent := is.Parent
		if _, err := s.edit(ctx, c.Key, 0, func(_ *domain.Issue, _ *domain.TrackerProject, lines []string, _ int) ([]string, error) {
			return markdown.SetFrontmatterValue(lines, "parent", parent), nil
		}); err != nil {
			return err
		}
	}
	if p, err := s.Project(ctx, is.Project); err == nil {
		if err := reparentChildren(ctx, s.vault, s.ix, s.store, n.ID, "note:"+p.NoteID); err != nil {
			return err
		}
	}
	if err := s.vault.Trash(n.Path); err != nil {
		return err
	}
	s.ix.Forget(n.Path)
	if err := s.ix.ReindexPath(ctx, n.Path); err != nil {
		return err
	}
	s.bus.Publish(Event{Type: "issue.deleted", ID: n.ID, Path: n.Path})
	return nil
}

type editFunc func(is *domain.Issue, p *domain.TrackerProject, lines []string, bodyStart int) ([]string, error)

// edit is the read-modify-write cycle shared by every issue change. Callers
// hold vaultWrites. The file on disk, not the index, is the base of the edit.
func (s *Issues) edit(ctx context.Context, key string, revision int64, fn editFunc) (*domain.Issue, error) {
	n, err := s.note(ctx, key)
	if err != nil {
		return nil, err
	}
	if revision != 0 && revision != n.Revision {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrConflict, n.Revision, revision)
	}
	is := markdown.ParseIssue(n)
	p, err := s.Project(ctx, is.Project)
	if err != nil {
		return nil, err
	}
	if err := s.policy.CheckProject(ctx, p.Key, policy.RoleMember); err != nil {
		return nil, err
	}
	data, err := s.vault.Read(n.Path)
	if err != nil {
		return nil, err
	}
	eol, lines := splitLines(data)
	_, bodyStart, ok := markdown.SplitFrontmatter(lines)
	if !ok {
		bodyStart = 0
	}
	before := strings.Join(lines, "\n")
	lines, err = fn(is, p, lines, bodyStart)
	if err != nil {
		return nil, err
	}
	if strings.Join(lines, "\n") == before {
		return s.Get(ctx, n.ID)
	}
	lines = markdown.SetFrontmatterValue(lines, "updated", nowFunc().UTC().Format(time.RFC3339))
	if err := s.write(ctx, n.Path, eol, lines); err != nil {
		return nil, err
	}
	s.bus.Publish(Event{Type: "issue.changed", ID: n.ID, Path: n.Path})
	after, err := s.Get(ctx, n.ID)
	if err == nil {
		s.notifyChange(ctx, is, after, actorOf(ctx))
	}
	return after, err
}

func (s *Issues) write(ctx context.Context, rel, eol string, lines []string) error {
	if err := s.vault.Write(rel, []byte(strings.Join(lines, eol))); err != nil {
		return err
	}
	s.ix.Forget(rel)
	return s.ix.ReindexPath(ctx, rel)
}

// actorOf is the username of the caller, "" for background work.
func actorOf(ctx context.Context) string {
	if p := policy.From(ctx); p != nil {
		if p.Via != "" {
			return AutomationActor
		}
		return p.Username
	}
	return ""
}

// AutomationActor signs what a rule writes, so the rule's owner still hears about
// an assignment or comment a person would otherwise be told of.
const AutomationActor = "automation"

// watchersOf are the people who follow an issue: its reporter, assignee and
// explicit watchers.
func watchersOf(is *domain.Issue) []string {
	return append([]string{is.Reporter, is.Assignee}, is.Watchers...)
}

// notifyChange reports what changed between two states of an issue. Only real
// changes reach it (edit returns early when the file is unchanged), so a
// retried request does not notify twice; mentions are keyed by issue so a
// re-saved description does not repeat them.
func (s *Issues) notifyChange(ctx context.Context, before, after *domain.Issue, actor string) {
	if s.notify == nil {
		return
	}
	stamp := nowFunc().UTC().Format(time.RFC3339Nano)
	var events []notify.Event
	if after.Assignee != "" && after.Assignee != before.Assignee {
		events = append(events, notify.Event{Kind: notify.Assigned, Issue: after.Key, Actor: actor,
			Recipients: []string{after.Assignee}, Dedup: "assigned:" + after.Key + ":" + stamp})
	}
	if before.Status != "" && after.Status != before.Status {
		events = append(events, notify.Event{Kind: notify.Status, Issue: after.Key, Ref: after.Status, Actor: actor,
			Recipients: append(watchersOf(before), watchersOf(after)...), Dedup: "status:" + after.Key + ":" + stamp})
	}
	old := map[string]bool{}
	for _, m := range notify.Mentions(before.Description) {
		old[m] = true
	}
	var fresh []string
	for _, m := range notify.Mentions(after.Description) {
		if !old[m] {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) > 0 {
		events = append(events, notify.Event{Kind: notify.Mentioned, Issue: after.Key, Actor: actor,
			Recipients: fresh, Dedup: "mention:" + after.Key + ":description"})
	}
	s.emit(ctx, events...)
}

func (s *Issues) emit(ctx context.Context, events ...notify.Event) {
	for _, e := range events {
		if _, err := s.notify.Emit(ctx, e); err != nil {
			// The change itself is saved; a lost notification is not worth failing it.
			s.log.Warn("уведомление не сохранено", "issue", e.Issue, "kind", e.Kind, "err", err)
		}
	}
}

var taskPriorityToIssue = map[domain.Priority]string{"A": "highest", "B": "high", "C": "medium", "D": "low"}

// PromoteTask turns a checkbox into a ticket of a project. The ticket comes
// first: if anything fails before the line changes, the task is still there. The
// line then becomes a link to the ticket, keeping its text (LUM-065).
func (s *Issues) PromoteTask(ctx context.Context, taskID, project, actor string) (*domain.Issue, error) {
	t, err := s.store.Task(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status == domain.StatusDone || t.Status == domain.StatusCancelled {
		return nil, fmt.Errorf("%w: задача уже закрыта", ErrInvalid)
	}
	note, err := s.store.Note(ctx, t.NotePath)
	if err != nil {
		return nil, err
	}
	if note.Type == domain.TypeIssue {
		return nil, fmt.Errorf("%w: это пункт чек-листа тикета", ErrInvalid)
	}
	if err := s.policy.CheckNote(ctx, note, policy.RoleMember); err != nil {
		return nil, err
	}
	in := IssueInput{
		Project: project, Title: titleWithoutTags(t.Text, t.Tags), Priority: taskPriorityToIssue[t.Priority], Labels: t.Tags,
		Estimate: t.EstimateS, Description: "Из задачи в [[" + strings.TrimSuffix(path.Base(t.NotePath), ".md") + "]].",
	}
	if !t.Due.IsZero() {
		in.Due = t.Due.String()
	}
	if !t.Start.IsZero() {
		in.Start = t.Start.String()
	} else if !t.Scheduled.IsZero() {
		in.Start = t.Scheduled.String()
	}
	issue, err := s.Create(ctx, in, actor)
	if err != nil {
		return nil, err
	}

	if err := s.replaceTaskLine(ctx, t, issue); err != nil {
		// Two copies of one job are worse than a ticket that was not made.
		if derr := s.Delete(ctx, issue.Key); derr != nil {
			return nil, fmt.Errorf("%w; и тикет %s удалить не удалось: %v", err, issue.Key, derr)
		}
		return nil, err
	}
	return issue, nil
}

// titleWithoutTags removes "#tag" words: on a ticket they are labels.
func titleWithoutTags(text string, tags []string) string {
	for _, tag := range tags {
		text = strings.ReplaceAll(text, "#"+tag, "")
	}
	return strings.Join(strings.Fields(text), " ")
}

func (s *Issues) replaceTaskLine(ctx context.Context, t *domain.Task, issue *domain.Issue) error {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	data, err := s.vault.Read(t.NotePath)
	if err != nil {
		return err
	}
	eol, lines := splitLines(data)
	n, err := locateTask(lines, t)
	if err != nil {
		return err
	}
	indent := lines[n][:len(lines[n])-len(strings.TrimLeft(lines[n], " \t"))]
	lines[n] = indent + "- [[" + issue.Key + "]] " + t.Text
	if err := s.vault.Write(t.NotePath, []byte(strings.Join(lines, eol))); err != nil {
		return err
	}
	return s.ix.ReindexPath(ctx, t.NotePath)
}
