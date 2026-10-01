package markdown

import (
	"strings"
	"unicode"
)

// InlineField is a "[key:: value]" pair with its byte range in the source line.
type InlineField struct {
	Key   string
	Value string
	Start int // byte offset of '['
	End   int // byte offset just past ']'
}

// scanBracket returns the offset just past the ']' matching the '[' at start,
// or -1. Nested brackets (as in "[project:: [[Note]]]") are handled.
func scanBracket(s string, start int) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// ExtractFields finds every inline field in s. Wikilinks are skipped because
// their content has no "::" separator at the top level.
func ExtractFields(s string) []InlineField {
	var out []InlineField
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' { // wikilink, not a field
			if end := scanBracket(s, i); end > 0 {
				i = end - 1
			}
			continue
		}
		end := scanBracket(s, i)
		if end < 0 {
			break
		}
		inner := s[i+1 : end-1]
		sep := topLevelSep(inner)
		if sep < 0 {
			i = end - 1
			continue
		}
		key := strings.TrimSpace(inner[:sep])
		if key == "" || !isFieldKey(key) {
			i = end - 1
			continue
		}
		out = append(out, InlineField{
			Key:   strings.ToLower(key),
			Value: strings.TrimSpace(inner[sep+2:]),
			Start: i,
			End:   end,
		})
		i = end - 1
	}
	return out
}

// topLevelSep returns the offset of the "::" separator outside of nested brackets.
func topLevelSep(s string) int {
	depth := 0
	for i := 0; i+1 < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
		case ':':
			if depth == 0 && s[i+1] == ':' {
				return i
			}
		}
	}
	return -1
}

func isFieldKey(k string) bool {
	for _, r := range k {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return unicode.IsLetter(rune(k[0]))
}

// TrailingBlockID returns the "^blockid" suffix of a line and the text before it.
func TrailingBlockID(s string) (text, blockID string) {
	trimmed := strings.TrimRight(s, " \t")
	i := strings.LastIndexByte(trimmed, '^')
	if i <= 0 || (trimmed[i-1] != ' ' && trimmed[i-1] != '\t') {
		return s, ""
	}
	id := trimmed[i+1:]
	if len(id) < 3 || len(id) > 32 {
		return s, ""
	}
	for _, r := range id {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '-' {
			return s, ""
		}
	}
	return strings.TrimRight(trimmed[:i], " \t"), id
}

// ExtractTags returns "#tag" values. Code spans must be masked by the caller.
func ExtractTags(s string) []string {
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '#' {
			continue
		}
		if i > 0 && !unicode.IsSpace(runes[i-1]) && runes[i-1] != '(' && runes[i-1] != '[' {
			continue
		}
		j := i + 1
		for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) ||
			runes[j] == '_' || runes[j] == '-' || runes[j] == '/') {
			j++
		}
		tag := string(runes[i+1 : j])
		// A bare "#" or a purely numeric "#123" is not a tag.
		if tag != "" && !allDigits(tag) {
			out = append(out, strings.Trim(tag, "/"))
		}
		i = j - 1
	}
	return out
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

// WikiLink is the parsed content of "[[target#heading|alias]]".
type WikiLink struct {
	Target, Heading, Alias string
}

// ExtractWikiLinks parses every "[[...]]" occurrence in a line.
func ExtractWikiLinks(s string) []WikiLink {
	var out []WikiLink
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '[' || s[i+1] != '[' {
			continue
		}
		end := strings.Index(s[i:], "]]")
		if end < 0 {
			break
		}
		inner := s[i+2 : i+end]
		i += end + 1
		if strings.TrimSpace(inner) == "" {
			continue
		}
		var l WikiLink
		if p := strings.Index(inner, "|"); p >= 0 {
			l.Alias = strings.TrimSpace(inner[p+1:])
			inner = inner[:p]
		}
		if p := strings.Index(inner, "#"); p >= 0 {
			l.Heading = strings.TrimSpace(inner[p+1:])
			inner = inner[:p]
		}
		l.Target = strings.TrimSpace(inner)
		if l.Target != "" || l.Heading != "" {
			out = append(out, l)
		}
	}
	return out
}

// MaskCodeSpans replaces the contents of inline code spans with spaces so that
// "#tag" and "[[link]]" inside `code` are not indexed. Offsets are preserved.
func MaskCodeSpans(s string) string {
	b := []byte(s)
	open := -1
	for i := 0; i < len(b); i++ {
		if b[i] != '`' {
			continue
		}
		if open < 0 {
			open = i
			continue
		}
		for j := open; j <= i; j++ {
			b[j] = ' '
		}
		open = -1
	}
	return string(b)
}
