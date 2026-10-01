package domain

import "time"

type NoteType string

const (
	TypeNote    NoteType = "note"
	TypeProject NoteType = "project"
	TypeArea    NoteType = "area"
	TypeDaily   NoteType = "daily"
	TypeMeeting NoteType = "meeting"
	TypeEvent   NoteType = "event"
)

type LinkKind string

const (
	LinkWiki     LinkKind = "wiki"
	LinkMarkdown LinkKind = "markdown"
)

type Link struct {
	Target  string   `json:"target"`  // note name / path / id
	Heading string   `json:"heading"` // after '#', may be "^blockid"
	Alias   string   `json:"alias"`
	Kind    LinkKind `json:"kind"`
	Line    int      `json:"line"`
}

// Note is a single markdown file in the vault.
type Note struct {
	ID          string         `json:"id"`
	Path        string         `json:"path"` // slash-separated, relative to vault root
	Title       string         `json:"title"`
	Type        NoteType       `json:"type"`
	Tags        []string       `json:"tags"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Body        string         `json:"body,omitempty"`
	Links       []Link         `json:"links,omitempty"`
	Tasks       []*Task        `json:"tasks,omitempty"`
	Start       Date           `json:"start,omitempty"`
	End         Date           `json:"end,omitempty"`
	AllDay      bool           `json:"all_day,omitempty"`
	Created     time.Time      `json:"created,omitempty"`
	Updated     time.Time      `json:"updated,omitempty"`
	Revision    int64          `json:"revision"`
	Hash        string         `json:"hash"`
	Size        int64          `json:"size"`

	// EOL and FrontmatterEnd let writers patch the file without reformatting it.
	EOL            string `json:"-"`
	FrontmatterEnd int    `json:"-"` // line index of the closing "---", -1 if absent
}

// IsEvent reports whether the note itself occupies a slot on the calendar.
func (n *Note) IsEvent() bool { return !n.Start.IsZero() }

type NoteQuery struct {
	Type          NoteType
	Tags          []string
	PathPrefix    string
	Text          string
	Limit, Offset int
}

type SearchHit struct {
	NoteID  string `json:"note_id"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Line    int    `json:"line"`
	Score   int    `json:"score"`
}

type Stats struct {
	Notes        int       `json:"notes"`
	Tasks        int       `json:"tasks"`
	OpenTasks    int       `json:"open_tasks"`
	Tags         int       `json:"tags"`
	LastIndexed  time.Time `json:"last_indexed"`
	IndexTookMS  int64     `json:"index_took_ms"`
	VaultPath    string    `json:"vault_path"`
	Encrypted    bool      `json:"encrypted"`
	UIEmbedded   bool      `json:"ui_embedded"`
	AuthRequired bool      `json:"auth_required"`
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}
