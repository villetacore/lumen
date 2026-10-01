package markdown

import (
	"bytes"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the parsed YAML header of a note plus the line offsets needed
// to patch it back without touching the rest of the file.
type Frontmatter struct {
	Fields    map[string]any
	Raw       string
	StartLine int // line index of the opening "---"
	EndLine   int // line index of the closing "---"; -1 when there is no frontmatter
}

// SplitFrontmatter separates the YAML header from the body. Lines are the file
// split by EOL with terminators removed.
func SplitFrontmatter(lines []string) (fmLines []string, bodyStart int, ok bool) {
	i := 0
	// A UTF-8 BOM is tolerated on the first line.
	if len(lines) > 0 && strings.HasPrefix(lines[0], "\ufeff") {
		lines = append([]string{strings.TrimPrefix(lines[0], "\ufeff")}, lines[1:]...)
	}
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return nil, 0, false
	}
	for i = 1; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if t == "---" || t == "..." {
			return lines[1:i], i + 1, true
		}
	}
	return nil, 0, false // unterminated: treat the whole file as body
}

// ParseFrontmatter parses the YAML header. Unknown keys are preserved in Fields
// so that writers can round-trip them.
func ParseFrontmatter(lines []string) (*Frontmatter, error) {
	fm := &Frontmatter{Fields: map[string]any{}, EndLine: -1}
	fmLines, _, ok := SplitFrontmatter(lines)
	if !ok {
		return fm, nil
	}
	fm.Raw = strings.Join(fmLines, "\n")
	fm.StartLine = 0
	fm.EndLine = len(fmLines) + 1
	if strings.TrimSpace(fm.Raw) == "" {
		return fm, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(fm.Raw), &m); err != nil {
		// A broken header must not make the note invisible; keep it as raw text.
		return fm, err
	}
	if m != nil {
		fm.Fields = m
	}
	return fm, nil
}

// Str returns a string-ish frontmatter value.
func (f *Frontmatter) Str(key string) string {
	v, ok := f.Fields[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case time.Time:
		// YAML turns "2026-09-08" into a timestamp; render it back as a date.
		if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
			return t.Format("2006-01-02")
		}
		return t.Format(time.RFC3339)
	default:
		return strings.TrimSpace(sprint(v))
	}
}

// Strings returns a list value, accepting both YAML sequences and
// comma/space separated scalars ("tags: work, home").
func (f *Frontmatter) Strings(key string) []string {
	v, ok := f.Fields[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := strings.TrimSpace(strings.TrimPrefix(sprint(e), "#")); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		var out []string
		for _, p := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' }) {
			if s := strings.TrimSpace(strings.TrimPrefix(p, "#")); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (f *Frontmatter) Bool(key string) bool {
	v, ok := f.Fields[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	if !b {
		s, _ := v.(string)
		return s == "true" || s == "yes"
	}
	return b
}

func sprint(v any) string {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	_ = enc.Encode(v)
	_ = enc.Close()
	return strings.TrimSpace(b.String())
}
