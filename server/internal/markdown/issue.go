package markdown

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"gopkg.in/yaml.v3"
)

// Tracker files (spec/FORMAT.md §7) are ordinary notes: the desktop parser
// reads them as notes, only the server projects them into projects and issues.

// CommentsHeading starts the comments section of an issue file.
const CommentsHeading = "## Комментарии"

var (
	commentsHeadingRe = regexp.MustCompile(`^##\s+(Комментарии|Comments)\s*$`)
	issueKeyRe        = regexp.MustCompile(`^([A-Z][A-Z0-9]{0,9})-([1-9][0-9]*)$`)
	projectKeyRe      = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}$`)
)

// ValidProjectKey reports whether k can prefix issue keys ("WEB", "LUM2").
func ValidProjectKey(k string) bool { return projectKeyRe.MatchString(k) }

// SplitIssueKey parses "WEB-12" into its project key and number.
func SplitIssueKey(key string) (project string, n int, ok bool) {
	m := issueKeyRe.FindStringSubmatch(key)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// ParseProject projects a "type: project" note with a "key" into a tracker
// project. Notes without a key are plain projects and return false.
func ParseProject(n *domain.Note) (*domain.TrackerProject, bool) {
	if n == nil || n.Type != domain.TypeProject {
		return nil, false
	}
	fm := &Frontmatter{Fields: n.Frontmatter}
	key := strings.ToUpper(fm.Str("key"))
	if !ValidProjectKey(key) {
		return nil, false
	}
	p := &domain.TrackerProject{
		Key: key, Title: n.Title, NoteID: n.ID, Path: n.Path,
		Parent:     strings.ToUpper(fm.Str("parent_project")),
		IssueTypes: fm.Strings("issue_types"), Revision: n.Revision,
	}
	p.Seq, _ = strconv.Atoi(fm.Str("issue_seq"))
	if list, ok := n.Frontmatter["workflow"].([]any); ok {
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sub := &Frontmatter{Fields: m}
			st := domain.WorkflowStatus{
				ID: sub.Str("id"), Name: sub.Str("name"),
				Category: domain.StatusCategory(sub.Str("category")),
			}
			if st.ID == "" {
				continue
			}
			if st.Name == "" {
				st.Name = st.ID
			}
			switch st.Category {
			case domain.CategoryTodo, domain.CategoryInProgress, domain.CategoryDone:
			default:
				st.Category = domain.CategoryTodo
			}
			st.WIP, _ = strconv.Atoi(sub.Str("wip"))
			p.Workflow = append(p.Workflow, st)
		}
	}
	if len(p.Workflow) == 0 {
		p.Workflow = domain.DefaultWorkflow()
	}
	if len(p.IssueTypes) == 0 {
		p.IssueTypes = domain.DefaultIssueTypes()
	}
	if m, ok := n.Frontmatter["transitions"].(map[string]any); ok {
		p.Transitions = map[string][]string{}
		for from := range m {
			p.Transitions[from] = (&Frontmatter{Fields: m}).Strings(from)
		}
	}
	return p, true
}

// ParseIssue projects a "type: issue" note into an issue. The key comes from
// "id" and falls back to the file name, so a renamed title never breaks links.
func ParseIssue(n *domain.Note) *domain.Issue {
	if n == nil || n.Type != domain.TypeIssue {
		return nil
	}
	fm := &Frontmatter{Fields: n.Frontmatter}
	key := fm.Str("id")
	if key == "" {
		key = TitleFromPath(n.Path)
	}
	is := &domain.Issue{
		Key: key, Path: n.Path, Title: n.Title, Revision: n.Revision,
		Project:  strings.ToUpper(fm.Str("project")),
		Type:     fm.Str("issue_type"),
		Status:   fm.Str("status"),
		Priority: fm.Str("priority"),
		Assignee: fm.Str("assignee"),
		Reporter: fm.Str("reporter"),
		Watchers: fm.Strings("watchers"),
		Labels:   fm.Strings("labels"),
		Epic:     fm.Str("epic"),
		Parent:   fm.Str("parent"),
		Sprint:   fm.Str("sprint"),
		Estimate: fm.Str("estimate"),
		Rank:     fm.Str("rank"),
		Created:  n.Created,
		Updated:  n.Updated,
	}
	if is.Project == "" {
		is.Project, _, _ = SplitIssueKey(key)
	}
	if is.Type == "" {
		is.Type = "task"
	}
	if is.Status == "" {
		is.Status = "todo"
	}
	if s := fm.Str("start"); s != "" {
		is.Start, _ = domain.ParseDate(s)
	}
	if s := fm.Str("due"); s != "" {
		is.Due, _ = domain.ParseDate(s)
	}
	if s := fm.Str("points"); s != "" {
		is.Points, _ = strconv.ParseFloat(s, 64)
	}
	if list, ok := n.Frontmatter["links"].([]any); ok {
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for typ := range m {
				sub := &Frontmatter{Fields: m}
				for _, target := range sub.Strings(typ) {
					is.Links = append(is.Links, domain.IssueLink{Type: typ, Target: target})
				}
			}
		}
	}

	bodyStart := n.FrontmatterEnd + 1
	lines := strings.Split(strings.ReplaceAll(n.Body, "\r\n", "\n"), "\n")
	desc, comments := SplitIssueBody(lines)
	is.Description = strings.TrimSpace(strings.Join(lines[:desc], "\n"))
	is.Comments = parseComments(lines, comments, bodyStart)
	for _, t := range n.Tasks {
		if t.Line-bodyStart < desc {
			is.Checklist = append(is.Checklist, t)
		}
	}
	return is
}

// SplitIssueBody returns where the description ends and where comments start
// (-1 without a comments section). Lines are the body, without frontmatter.
func SplitIssueBody(lines []string) (descEnd, commentsStart int) {
	inFence, fence := false, ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if f := fenceMarker(t); f != "" {
			if !inFence {
				inFence, fence = true, f
			} else if strings.HasPrefix(t, fence) {
				inFence = false
			}
			continue
		}
		if !inFence && commentsHeadingRe.MatchString(t) {
			return i, i + 1
		}
	}
	return len(lines), -1
}

func parseComments(lines []string, start, offset int) []*domain.Comment {
	if start < 0 {
		return nil
	}
	var out []*domain.Comment
	var cur *domain.Comment
	var body []string
	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
			out = append(out, cur)
		}
		cur, body = nil, nil
	}
	inFence, fence := false, ""
	for i := start; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		if f := fenceMarker(t); f != "" {
			if !inFence {
				inFence, fence = true, f
			} else if strings.HasPrefix(t, fence) {
				inFence = false
			}
		} else if !inFence && strings.HasPrefix(t, "### ") {
			flush()
			cur = parseCommentHeading(strings.TrimSpace(t[4:]), i+offset)
			continue
		} else if !inFence && strings.HasPrefix(t, "## ") {
			break
		}
		if cur != nil {
			body = append(body, l)
		}
	}
	flush()
	return out
}

// parseCommentHeading reads "2026-11-02 12:30 · alice ^c-7f3a".
func parseCommentHeading(h string, line int) *domain.Comment {
	c := &domain.Comment{Line: line}
	h, c.ID = TrailingBlockID(h)
	if c.ID == "" {
		c.ID = fmt.Sprintf("line-%d", line)
	}
	when, who, _ := strings.Cut(h, "·")
	c.Author = strings.TrimSpace(who)
	when = strings.TrimSpace(when)
	for _, layout := range []string{"2006-01-02 15:04", time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, when, time.Local); err == nil {
			c.At = t
			break
		}
	}
	return c
}

// CommentHeading renders the heading line of a new comment.
func CommentHeading(at time.Time, author, id string) string {
	h := "### " + at.Format("2006-01-02 15:04")
	if author != "" {
		h += " · " + author
	}
	return h + " ^" + id
}

// SetFrontmatterValue replaces one top-level key, including its nested lines,
// and leaves every other line of the header untouched (ADR-0003). A nil value
// or an empty string/list removes the key.
func SetFrontmatterValue(lines []string, key string, value any) []string {
	rendered := renderField(key, value)
	fmLines, bodyStart, ok := SplitFrontmatter(lines)
	if !ok {
		if rendered == nil {
			return lines
		}
		header := append([]string{"---"}, rendered...)
		header = append(header, "---", "")
		return append(header, lines...)
	}
	fm := append([]string{}, fmLines...)
	start, end := -1, -1
	for i, l := range fm {
		if strings.HasPrefix(l, key+":") {
			start, end = i, i+1
			for end < len(fm) && (strings.HasPrefix(fm[end], " ") || strings.HasPrefix(fm[end], "\t") ||
				strings.HasPrefix(fm[end], "- ") || fm[end] == "-") {
				end++
			}
			break
		}
	}
	if start >= 0 {
		fm = append(fm[:start], append(rendered, fm[end:]...)...)
	} else {
		fm = append(fm, rendered...)
	}
	out := append([]string{"---"}, fm...)
	out = append(out, "---")
	return append(out, lines[bodyStart:]...)
}

var plainScalarRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _.\-/]*$`)
var dateScalarRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T[\d:]+(Z|[+-]\d{2}:\d{2})?)?$`)

func renderField(key string, value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []string{key + ": " + scalar(v)}
	case int:
		return []string{key + ": " + strconv.Itoa(v)}
	case float64:
		return []string{key + ": " + strconv.FormatFloat(v, 'f', -1, 64)}
	case []string:
		if len(v) == 0 {
			return nil
		}
		parts := make([]string, len(v))
		for i, s := range v {
			parts[i] = scalar(s)
		}
		return []string{key + ": [" + strings.Join(parts, ", ") + "]"}
	default:
		var b strings.Builder
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		if err := enc.Encode(map[string]any{key: v}); err != nil {
			return nil
		}
		_ = enc.Close()
		return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	}
}

// scalar writes a string the way a person would, quoting only when YAML would
// read it as something else.
func scalar(s string) string {
	if dateScalarRe.MatchString(s) {
		return s
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~":
		return quoted(s)
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return quoted(s)
	}
	if plainScalarRe.MatchString(s) && !strings.HasSuffix(s, " ") {
		return s
	}
	return quoted(s)
}

// quoted uses JSON escaping: a JSON string is a valid double-quoted YAML scalar.
func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// IssuePath is where a new issue of a project is created.
func IssuePath(p *domain.TrackerProject, key string) string {
	return path.Join(path.Dir(p.Path), p.Key, "issues", key+".md")
}

var sprintIDRe = regexp.MustCompile(`^([A-Z][A-Z0-9]{0,9})-S([1-9][0-9]*)$`)

// SplitSprintID parses "WEB-S3" into its project key and number.
func SplitSprintID(id string) (project string, n int, ok bool) {
	m := sprintIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	return m[1], n, err == nil
}

// ParseSprint projects a "type: sprint" note. The id falls back to the file
// name, the project to the id prefix, the state to "planned".
func ParseSprint(n *domain.Note) *domain.Sprint {
	if n == nil || n.Type != domain.TypeSprint {
		return nil
	}
	fm := &Frontmatter{Fields: n.Frontmatter}
	sp := &domain.Sprint{
		ID: fm.Str("id"), Path: n.Path, Name: n.Title, Goal: fm.Str("goal"),
		Project: strings.ToUpper(fm.Str("project")), State: domain.SprintState(fm.Str("state")),
		Revision: n.Revision,
	}
	if sp.ID == "" {
		sp.ID = TitleFromPath(n.Path)
	}
	if sp.Project == "" {
		sp.Project, _, _ = SplitSprintID(sp.ID)
	}
	switch sp.State {
	case domain.SprintActive, domain.SprintClosed:
	default:
		sp.State = domain.SprintPlanned
	}
	sp.Start, sp.End = n.Start, n.End
	if s := fm.Str("completed"); s != "" {
		sp.Completed, _ = domain.ParseDate(s)
	}
	return sp
}

// SprintPath is where a new sprint of a project is created.
func SprintPath(p *domain.TrackerProject, id string) string {
	return path.Join(path.Dir(p.Path), p.Key, "sprints", id+".md")
}
