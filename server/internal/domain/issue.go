package domain

import "time"

// StatusCategory groups workflow columns so that boards with different
// column names still answer "is this open?" the same way.
type StatusCategory string

const (
	CategoryTodo       StatusCategory = "todo"
	CategoryInProgress StatusCategory = "in_progress"
	CategoryDone       StatusCategory = "done"
)

// WorkflowStatus is one column of a project board.
type WorkflowStatus struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Category StatusCategory `json:"category"`
	WIP      int            `json:"wip,omitempty"`
}

// DefaultWorkflow is used by projects that do not declare their own.
func DefaultWorkflow() []WorkflowStatus {
	return []WorkflowStatus{
		{ID: "todo", Name: "К работе", Category: CategoryTodo},
		{ID: "in-progress", Name: "В работе", Category: CategoryInProgress},
		{ID: "done", Name: "Готово", Category: CategoryDone},
	}
}

// DefaultIssueTypes is used by projects that do not declare their own.
func DefaultIssueTypes() []string { return []string{"task", "bug", "story", "epic"} }

// TrackerProject is a project note that carries an issue key (spec §7).
type TrackerProject struct {
	Key         string              `json:"key"`
	Title       string              `json:"title"`
	Parent      string              `json:"parent,omitempty"` // key of the parent project
	NoteID      string              `json:"note_id"`
	Path        string              `json:"path"`
	Workflow    []WorkflowStatus    `json:"workflow"`
	IssueTypes  []string            `json:"issue_types"`
	Transitions map[string][]string `json:"transitions,omitempty"` // empty: any move is allowed
	Seq         int                 `json:"issue_seq"`
	Revision    int64               `json:"revision"`
}

// Status finds a workflow column by id.
func (p *TrackerProject) Status(id string) (WorkflowStatus, bool) {
	for _, s := range p.Workflow {
		if s.ID == id {
			return s, true
		}
	}
	return WorkflowStatus{}, false
}

// CanMove reports whether the workflow allows from → to.
func (p *TrackerProject) CanMove(from, to string) bool {
	if from == to || len(p.Transitions) == 0 {
		return true
	}
	allowed, ok := p.Transitions[from]
	if !ok {
		return true
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// IssueLink is a typed relation to another issue ("blocks: WEB-14").
type IssueLink struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// Comment is one "### <time> · <author> ^c-<id>" section of an issue file.
type Comment struct {
	ID     string    `json:"id"`
	Author string    `json:"author,omitempty"`
	At     time.Time `json:"at"`
	Body   string    `json:"body"`
	Line   int       `json:"line"`
}

// Issue is a tracker ticket stored as its own Markdown file (ADR-0010).
type Issue struct {
	Key            string         `json:"key"`
	Project        string         `json:"project"`
	Path           string         `json:"path"`
	Title          string         `json:"title"`
	Type           string         `json:"issue_type"`
	Status         string         `json:"status"`
	StatusCategory StatusCategory `json:"status_category,omitempty"`
	Priority       string         `json:"priority,omitempty"`
	Assignee       string         `json:"assignee,omitempty"`
	Reporter       string         `json:"reporter,omitempty"`
	Watchers       []string       `json:"watchers,omitempty"`
	Labels         []string       `json:"labels,omitempty"`
	Epic           string         `json:"epic,omitempty"`
	Parent         string         `json:"parent,omitempty"`
	Sprint         string         `json:"sprint,omitempty"`
	Start          Date           `json:"start,omitempty"`
	Due            Date           `json:"due,omitempty"`
	Estimate       string         `json:"estimate,omitempty"`
	Points         float64        `json:"points,omitempty"`
	Rank           string         `json:"rank,omitempty"`
	Links          []IssueLink    `json:"links,omitempty"`
	Created        time.Time      `json:"created,omitempty"`
	Updated        time.Time      `json:"updated,omitempty"`
	Revision       int64          `json:"revision"`

	Description string     `json:"description,omitempty"`
	Comments    []*Comment `json:"comments,omitempty"`
	Checklist   []*Task    `json:"checklist,omitempty"`
}

type IssueQuery struct {
	Project string
	// Subprojects widens Project to the projects nested under it.
	Subprojects bool
	Parent      string // issues whose parent is this key
	Status      []string
	Category    StatusCategory
	Assignee    string
	Watcher     string // reporter, assignee or explicit watcher
	Type        string
	Labels      []string
	Sprint      string
	Epic        string
	Text        string
	Limit       int
	Offset      int
}

type SprintState string

const (
	SprintPlanned SprintState = "planned"
	SprintActive  SprintState = "active"
	SprintClosed  SprintState = "closed"
)

// Sprint is a "type: sprint" note of a tracker project (spec §7).
type Sprint struct {
	ID        string      `json:"id"`
	Project   string      `json:"project"`
	Path      string      `json:"path"`
	Name      string      `json:"name"`
	Goal      string      `json:"goal,omitempty"`
	State     SprintState `json:"state"`
	Start     Date        `json:"start,omitempty"`
	End       Date        `json:"end,omitempty"`
	Completed Date        `json:"completed,omitempty"`
	Revision  int64       `json:"revision"`
}
