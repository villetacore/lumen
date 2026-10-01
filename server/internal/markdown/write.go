package markdown

import (
	"errors"
	"sort"
	"strings"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

// TaskPatch describes a change to a single checkbox line. Only the parts named
// here are touched; everything else in the line is preserved byte for byte.
type TaskPatch struct {
	Status  *domain.TaskStatus
	Text    *string
	Set     map[string]string // "" as value removes the field
	BlockID string            // assigned when the line has none
}

// fieldOrder is the order in which newly added inline fields are appended.
var fieldOrder = []string{"due", "scheduled", "start", "done", "prio", "est", "project", "repeat"}

// ApplyTaskPatch rewrites one checkbox line. See ADR-0003 (lossless edits).
func ApplyTaskPatch(line string, p TaskPatch) (string, error) {
	tl, ok := ParseTaskLine(line)
	if !ok {
		return "", errors.New("not a task line")
	}

	if p.Status != nil {
		cur := domain.StatusFromMarker(tl.Marker[0])
		if cur != *p.Status {
			tl.Marker = string(domain.MarkerFromStatus(*p.Status))
		}
	}

	rest := tl.Rest
	trailing := rest[len(strings.TrimRight(rest, " \t")):]
	rest = strings.TrimRight(rest, " \t")

	body, blockID := TrailingBlockID(rest)
	if p.BlockID != "" && blockID == "" {
		blockID = p.BlockID
	}

	if p.Text != nil {
		body = replaceText(body, *p.Text)
	}

	body = applyFields(body, p.Set)

	out := tl.Indent + tl.Bullet + tl.Gap1 + "[" + tl.Marker + "]" + tl.Gap2 + body
	if blockID != "" {
		out += " ^" + blockID
	}
	return out + trailing, nil
}

// applyFields edits inline fields in place and appends the ones that are new.
func applyFields(body string, set map[string]string) string {
	if len(set) == 0 {
		return body
	}
	existing := ExtractFields(body)
	byKey := map[string]InlineField{}
	for _, f := range existing {
		byKey[f.Key] = f
	}

	// Edits are applied back to front so that earlier offsets stay valid.
	type edit struct {
		start, end int
		repl       string
	}
	var edits []edit
	var added []string
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Slice(keys, func(i, j int) bool { return orderOf(keys[i]) < orderOf(keys[j]) })

	for _, k := range keys {
		v := strings.TrimSpace(set[k])
		f, has := byKey[k]
		switch {
		case has && v == "":
			edits = append(edits, edit{f.Start, f.End, ""})
		case has:
			edits = append(edits, edit{f.Start, f.End, "[" + k + ":: " + v + "]"})
		case v != "":
			added = append(added, "["+k+":: "+v+"]")
		}
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		body = body[:e.start] + e.repl + body[e.end:]
	}
	body = strings.TrimRight(collapseSpaces(body), " \t")
	for _, a := range added {
		body += " " + a
	}
	return body
}

func orderOf(k string) int {
	for i, f := range fieldOrder {
		if f == k {
			return i
		}
	}
	return len(fieldOrder)
}

// collapseSpaces squeezes runs of spaces left behind by removed fields, without
// touching leading indentation.
func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// replaceText swaps the human-readable part of a task line, keeping its inline
// fields in their original order at the end.
func replaceText(body, newText string) string {
	fields := ExtractFields(body)
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		kept = append(kept, body[f.Start:f.End])
	}
	out := strings.TrimSpace(newText)
	for _, f := range kept {
		out += " " + f
	}
	return out
}

// SetFrontmatterKey inserts or replaces a scalar key in a note's YAML header,
// creating the header when the file has none. Key order is preserved.
func SetFrontmatterKey(lines []string, key, value string) []string {
	fmLines, bodyStart, ok := SplitFrontmatter(lines)
	if !ok {
		header := []string{"---", key + ": " + value, "---", ""}
		return append(header, lines...)
	}
	prefix := key + ":"
	for i, l := range fmLines {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			fmLines[i] = key + ": " + value
			out := append([]string{"---"}, fmLines...)
			out = append(out, "---")
			return append(out, lines[bodyStart:]...)
		}
	}
	fmLines = append(fmLines, key+": "+value)
	out := append([]string{"---"}, fmLines...)
	out = append(out, "---")
	return append(out, lines[bodyStart:]...)
}
