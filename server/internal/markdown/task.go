package markdown

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

// TaskLine is the lexical shape of a checkbox line, kept so that writers can
// rebuild the line without disturbing anything they did not change.
type TaskLine struct {
	Indent   string // leading whitespace
	Bullet   string // "-", "*", "+", "1." ...
	Gap1     string // whitespace between bullet and "["
	Marker   string // the character inside the brackets
	Gap2     string // whitespace after "]"
	Rest     string // everything after the checkbox
	RestFrom int    // byte offset of Rest within the original line
}

// ParseTaskLine recognises "- [ ] text" at any indentation. It returns false
// for ordinary list items and paragraphs.
func ParseTaskLine(line string) (TaskLine, bool) {
	var t TaskLine
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	t.Indent = line[:i]
	start := i
	switch {
	case i < len(line) && (line[i] == '-' || line[i] == '*' || line[i] == '+'):
		i++
	default: // ordered list: digits followed by '.' or ')'
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		if i == start || i >= len(line) || (line[i] != '.' && line[i] != ')') {
			return t, false
		}
		i++
	}
	t.Bullet = line[start:i]
	gap := i
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == gap { // a bullet must be followed by whitespace
		return t, false
	}
	t.Gap1 = line[gap:i]
	if i >= len(line) || line[i] != '[' {
		return t, false
	}
	close := strings.IndexByte(line[i:], ']')
	if close < 0 {
		return t, false
	}
	marker := line[i+1 : i+close]
	if len([]rune(marker)) != 1 {
		return t, false
	}
	t.Marker = marker
	i += close + 1
	gap = i
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	t.Gap2 = line[gap:i]
	t.Rest = line[i:]
	t.RestFrom = i
	return t, true
}

// String rebuilds the line from its parts.
func (t TaskLine) String() string {
	return t.Indent + t.Bullet + t.Gap1 + "[" + t.Marker + "]" + t.Gap2 + t.Rest
}

// IndentWidth counts the indentation in spaces (a tab is 4).
func (t TaskLine) IndentWidth() int {
	n := 0
	for _, r := range t.Indent {
		if r == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

// BuildTask turns a parsed checkbox line into a domain task.
func BuildTask(notePath, noteID string, lineNo int, line string) *domain.Task {
	tl, ok := ParseTaskLine(line)
	if !ok {
		return nil
	}
	rest, blockID := TrailingBlockID(tl.Rest)
	fields := ExtractFields(rest)

	t := &domain.Task{
		BlockID:  blockID,
		NoteID:   noteID,
		NotePath: notePath,
		Line:     lineNo,
		Indent:   tl.IndentWidth(),
		Status:   domain.StatusFromMarker(tl.Marker[0]),
		Marker:   tl.Marker,
		Raw:      line,
		Fields:   map[string]string{},
	}

	// Strip the field spans to get the plain task text.
	var b strings.Builder
	prev := 0
	for _, f := range fields {
		b.WriteString(rest[prev:f.Start])
		prev = f.End
	}
	b.WriteString(rest[prev:])
	text := strings.Join(strings.Fields(MaskNothing(b.String())), " ")
	t.Tags = ExtractTags(MaskCodeSpans(text))
	t.Text = text

	for _, f := range fields {
		switch f.Key {
		case "due":
			t.Due = parseDateField(f.Value)
		case "scheduled":
			t.Scheduled = parseDateField(f.Value)
		case "start":
			t.Start = parseDateField(f.Value)
		case "done", "completion":
			t.DoneAt = parseDateField(f.Value)
		case "prio", "priority":
			t.Priority = parsePriority(f.Value)
		case "est", "estimate":
			// Оценка показывается ровно так, как её написал пользователь.
			if d, err := time.ParseDuration(strings.TrimSpace(f.Value)); err == nil {
				t.Estimate, t.EstimateS = d, strings.TrimSpace(f.Value)
			}
		case "project":
			t.Project = strings.Trim(f.Value, "[] ")
		case "repeat", "recur":
			t.Repeat = f.Value
		default:
			t.Fields[f.Key] = f.Value
		}
	}

	t.ID = blockID
	if t.ID == "" {
		t.ID = DerivedTaskID(notePath, t.Text, lineNo)
	}
	return t
}

// MaskNothing exists to make the intent explicit where no masking is wanted.
func MaskNothing(s string) string { return s }

// DerivedTaskID is the fallback identity for a task without a "^blockid".
// It is stable while neither the path nor the text changes.
func DerivedTaskID(path, text string, line int) string {
	h := sha1.Sum([]byte(path + "\n" + text + "\n" + fmt.Sprint(line)))
	return "d" + hex.EncodeToString(h[:])[:11]
}

func parseDateField(v string) domain.Date {
	if d, err := domain.ParseDate(v); err == nil {
		return d
	}
	if d, ok := domain.Relative(v, time.Now()); ok {
		return d
	}
	return domain.Date{}
}

func parsePriority(v string) domain.Priority {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "a", "high", "highest", "1":
		return domain.PrioA
	case "b", "medium", "med", "2":
		return domain.PrioB
	case "c", "low", "3":
		return domain.PrioC
	case "d", "lowest", "4":
		return domain.PrioD
	}
	return ""
}
