package domain

import "time"

type TaskStatus string

const (
	StatusTodo      TaskStatus = "todo"
	StatusDoing     TaskStatus = "doing"
	StatusDone      TaskStatus = "done"
	StatusCancelled TaskStatus = "cancelled"
	StatusDeferred  TaskStatus = "deferred"
)

// Open reports whether the task still needs attention.
func (s TaskStatus) Open() bool { return s == StatusTodo || s == StatusDoing }

// StatusFromMarker maps the character inside "[ ]" to a status. Unknown markers
// are treated as todo but their marker is preserved on write.
func StatusFromMarker(c byte) TaskStatus {
	switch c {
	case ' ', '\t':
		return StatusTodo
	case '/':
		return StatusDoing
	case 'x', 'X':
		return StatusDone
	case '-':
		return StatusCancelled
	case '>':
		return StatusDeferred
	default:
		return StatusTodo
	}
}

// MarkerFromStatus is the inverse used when writing a task back to the file.
func MarkerFromStatus(s TaskStatus) byte {
	switch s {
	case StatusDoing:
		return '/'
	case StatusDone:
		return 'x'
	case StatusCancelled:
		return '-'
	case StatusDeferred:
		return '>'
	default:
		return ' '
	}
}

type Priority string

const (
	PrioA Priority = "A"
	PrioB Priority = "B"
	PrioC Priority = "C"
	PrioD Priority = "D"
)

// Task is a checkbox line inside a note.
type Task struct {
	ID       string     `json:"id"`
	BlockID  string     `json:"block_id,omitempty"` // "^blockid" if present in the file
	NoteID   string     `json:"note_id"`
	NotePath string     `json:"note_path"`
	Line     int        `json:"line"` // 0-based, absolute in file
	Indent   int        `json:"indent"`
	Status   TaskStatus `json:"status"`
	Marker   string     `json:"marker"` // raw character inside the brackets
	Text     string     `json:"text"`   // without inline fields, tags and block id
	Raw      string     `json:"-"`      // original line, without EOL

	Tags      []string          `json:"tags,omitempty"`
	Due       Date              `json:"due,omitempty"`
	Scheduled Date              `json:"scheduled,omitempty"`
	Start     Date              `json:"start,omitempty"`
	DoneAt    Date              `json:"done,omitempty"`
	Priority  Priority          `json:"priority,omitempty"`
	Estimate  time.Duration     `json:"-"`
	EstimateS string            `json:"estimate,omitempty"`
	Project   string            `json:"project,omitempty"`
	Repeat    string            `json:"repeat,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// Overdue reports whether an open task's due date is in the past.
func (t *Task) Overdue(now Date) bool {
	return t.Status.Open() && !t.Due.IsZero() && t.Due.Day().Before(now.Day())
}

type SortKey struct {
	Field string // due | scheduled | priority | path | line | status | text
	Desc  bool
}

type TaskQuery struct {
	Status        []TaskStatus
	DueBefore     Date
	DueAfter      Date
	ScheduledFrom Date
	ScheduledTo   Date
	Tags          []string
	Project       string
	PathPrefix    string
	Text          string
	Overdue       bool
	HasDue        *bool
	Sort          []SortKey
	Limit, Offset int
}

type EventSource string

const (
	SourceNote  EventSource = "note"
	SourceTask  EventSource = "task"
	SourceDaily EventSource = "daily"
	SourceICS   EventSource = "ics"
)

// Event is a calendar entry derived from a note, a task or an external feed.
type Event struct {
	ID     string      `json:"id"`
	Source EventSource `json:"source"`
	RefID  string      `json:"ref_id"`
	Path   string      `json:"path,omitempty"`
	Title  string      `json:"title"`
	Start  time.Time   `json:"start"`
	End    time.Time   `json:"end"`
	AllDay bool        `json:"all_day"`
	Status TaskStatus  `json:"status,omitempty"`

	// Set for events that come from an integration rather than from a file.
	Calendar    string `json:"calendar,omitempty"`
	Location    string `json:"location,omitempty"`
	URL         string `json:"url,omitempty"`
	Integration string `json:"integration,omitempty"`
}
