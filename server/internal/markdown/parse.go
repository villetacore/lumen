// Package markdown implements the Lumen file format described in spec/FORMAT.md.
// Both this package and desktop/core/src/md must satisfy the same golden fixtures.
package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

var dailyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ParseNote turns the raw bytes of a vault file into a domain note.
// Line numbers on tasks and links are absolute within the file.
func ParseNote(relPath string, data []byte) (*domain.Note, error) {
	src := string(data)
	eol := "\n"
	if strings.Contains(src, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	fm, fmErr := ParseFrontmatter(lines)
	_, bodyStart, hasFM := SplitFrontmatter(lines)
	if !hasFM {
		bodyStart = 0
	}

	sum := sha256.Sum256(data)
	n := &domain.Note{
		Path:           relPath,
		Frontmatter:    fm.Fields,
		EOL:            eol,
		FrontmatterEnd: fm.EndLine,
		Hash:           "sha256:" + hex.EncodeToString(sum[:]),
		Size:           int64(len(data)),
		Body:           strings.Join(lines[bodyStart:], eol),
	}

	n.ID = fm.Str("id")
	if n.ID == "" {
		n.ID = relPath
	}
	n.Type = noteType(fm.Str("type"), relPath)
	n.Title = fm.Str("title")
	n.Tags = fm.Strings("tags")
	n.AllDay = fm.Bool("allday")
	if s := fm.Str("start"); s != "" {
		n.Start, _ = domain.ParseDate(s)
	}
	if s := fm.Str("end"); s != "" {
		n.End, _ = domain.ParseDate(s)
	}
	if s := fm.Str("created"); s != "" {
		if d, err := domain.ParseDate(s); err == nil {
			n.Created = d.T
		}
	}
	if s := fm.Str("updated"); s != "" {
		if d, err := domain.ParseDate(s); err == nil {
			n.Updated = d.T
		}
	}

	seenTag := map[string]bool{}
	for _, t := range n.Tags {
		seenTag[t] = true
	}

	inFence := false
	var fence string
	for i := bodyStart; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if f := fenceMarker(trimmed); f != "" {
			if !inFence {
				inFence, fence = true, f
			} else if strings.HasPrefix(trimmed, fence) {
				inFence, fence = false, ""
			}
			continue
		}
		if inFence {
			continue
		}

		if n.Title == "" && strings.HasPrefix(trimmed, "# ") {
			n.Title = strings.TrimSpace(trimmed[2:])
		}

		if t := BuildTask(relPath, n.ID, i, line); t != nil {
			n.Tasks = append(n.Tasks, t)
			for _, tg := range t.Tags {
				if !seenTag[tg] {
					seenTag[tg] = true
					n.Tags = append(n.Tags, tg)
				}
			}
		} else {
			masked := MaskCodeSpans(line)
			for _, tg := range ExtractTags(masked) {
				if !seenTag[tg] {
					seenTag[tg] = true
					n.Tags = append(n.Tags, tg)
				}
			}
		}

		for _, l := range ExtractWikiLinks(MaskCodeSpans(line)) {
			n.Links = append(n.Links, domain.Link{
				Target: l.Target, Heading: l.Heading, Alias: l.Alias,
				Kind: domain.LinkWiki, Line: i,
			})
		}
	}

	if n.Title == "" {
		n.Title = TitleFromPath(relPath)
	}
	if n.Type == domain.TypeDaily && n.Start.IsZero() {
		if d, err := domain.ParseDate(strings.TrimSuffix(path.Base(relPath), ".md")); err == nil {
			n.Start = d
			n.AllDay = true
		}
	}
	return n, fmErr
}

// TitleFromPath falls back to the file name when a note has no title and no H1.
func TitleFromPath(relPath string) string {
	return strings.TrimSuffix(path.Base(relPath), path.Ext(relPath))
}

func noteType(v, relPath string) domain.NoteType {
	switch domain.NoteType(strings.ToLower(strings.TrimSpace(v))) {
	case domain.TypeProject:
		return domain.TypeProject
	case domain.TypeArea:
		return domain.TypeArea
	case domain.TypeDaily:
		return domain.TypeDaily
	case domain.TypeMeeting:
		return domain.TypeMeeting
	case domain.TypeEvent:
		return domain.TypeEvent
	case domain.TypeNote:
		return domain.TypeNote
	}
	if dailyRe.MatchString(TitleFromPath(relPath)) {
		return domain.TypeDaily
	}
	return domain.TypeNote
}

func fenceMarker(trimmed string) string {
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, m) {
			return m
		}
	}
	return ""
}

// Events derives every calendar entry a note contributes: the note itself when
// it carries start/end, plus each of its dated tasks.
func Events(n *domain.Note) []domain.Event {
	var out []domain.Event
	if n.IsEvent() {
		end := n.End
		if end.IsZero() {
			end = n.Start
		}
		src := domain.SourceNote
		if n.Type == domain.TypeDaily {
			src = domain.SourceDaily
		}
		out = append(out, domain.Event{
			ID: "note:" + n.ID, Source: src, RefID: n.ID, Path: n.Path,
			Title: n.Title, Start: n.Start.T, End: end.T,
			AllDay: n.AllDay || !n.Start.HasTime,
		})
	}
	for _, t := range n.Tasks {
		d := t.Scheduled
		if d.IsZero() {
			d = t.Due
		}
		if d.IsZero() {
			continue
		}
		end := d.T
		if d.HasTime {
			end = d.T.Add(time.Hour)
			if t.Estimate > 0 {
				end = d.T.Add(t.Estimate)
			}
		}
		out = append(out, domain.Event{
			ID: "task:" + t.ID, Source: domain.SourceTask, RefID: t.ID, Path: n.Path,
			Title: t.Text, Start: d.T, End: end, AllDay: !d.HasTime, Status: t.Status,
		})
	}
	return out
}
