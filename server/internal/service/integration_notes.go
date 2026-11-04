package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/integrations"
	"github.com/villetacore/lumen/server/internal/markdown"
)

// upsertNote refreshes the owned source projection and retains local text.
// Legacy content is preserved in full, with an exact backup before migration.
func (s *Integrations) upsertNote(
	ctx context.Context, cfg integrations.Config, item integrations.ExternalItem,
) (created, changed bool, err error) {
	existing, lookupErr := s.store.NoteBySource(ctx, cfg.ID, item.ExternalID)
	if lookupErr == nil {
		if asStr(existing.Frontmatter["source_hash"]) == item.Hash {
			return false, false, nil
		}
		return false, true, s.refreshNote(ctx, cfg, item, existing)
	}

	if !errors.Is(lookupErr, index.ErrNotFound) {
		return false, false, lookupErr
	}

	source := renderItemBody(cfg, item)
	body := sourceBegin + "\n" + source + sourceEnd + "\n\n"
	relPath := path.Join(integrations.DefaultFolder(cfg), itemFileName(item)+".md")
	if s.vault.Exists(relPath) {
		// A file with that name exists but carries no marker: do not touch it.
		relPath = path.Join(integrations.DefaultFolder(cfg),
			itemFileName(item)+"-"+sourceHash(cfg.ID + "\x00" + item.ExternalID)[:16]+".md")
	}

	if _, err := s.notes.Create(ctx, CreateInput{
		Path:  relPath,
		Title: item.Title,
		Type:  domain.TypeNote,
		Tags:  append([]string{cfg.Kind}, item.Labels...),
		Body:  body,
		Extra: map[string]string{
			"source_connector":   cfg.Kind,
			"source_integration": cfg.ID,
			"source_id":          item.ExternalID,
			"source_url":         item.URL,
			"source_hash":        item.Hash,
			"source_block_hash":  projectionHash(source),
		},
	}); err != nil {
		return false, false, err
	}
	return true, false, nil
}

const sourceBegin = "<!-- lumen:source:begin -->"
const sourceEnd = "<!-- lumen:source:end -->"

func sourceHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", sum[:])
}

// Checkbox state is an operational field edited through the task API. It must
// not turn a safe source-text refresh into a local-content conflict.
func projectionHash(body string) string {
	parsed, err := markdown.ParseNote("source.md", []byte(body))
	if err != nil {
		return sourceHash(body)
	}
	lines := strings.Split(body, "\n")
	status := domain.StatusTodo
	for _, task := range parsed.Tasks {
		patched, err := markdown.ApplyTaskPatch(lines[task.Line], markdown.TaskPatch{Status: &status, Set: map[string]string{"done": ""}})
		if err == nil {
			lines[task.Line] = patched
		}
	}
	return sourceHash(strings.Join(lines, "\n"))
}

func patchSourceStatus(body, blockID string, status domain.TaskStatus) string {
	parsed, err := markdown.ParseNote("source.md", []byte(body))
	if err != nil {
		return body
	}
	lines := strings.Split(body, "\n")
	for _, task := range parsed.Tasks {
		if task.BlockID != blockID {
			continue
		}
		patched, err := markdown.ApplyTaskPatch(lines[task.Line], markdown.TaskPatch{Status: &status})
		if err == nil {
			lines[task.Line] = patched
		}
		break
	}
	return strings.Join(lines, "\n")
}

// refreshNote commits the projection and its hash together, under the same
// lock as user edits. Cursor retry can never skip a half-refreshed document.
func (s *Integrations) refreshNote(ctx context.Context, cfg integrations.Config, item integrations.ExternalItem, note *domain.Note) error {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	data, err := s.vault.Read(note.Path)
	if err != nil {
		return err
	}
	current, err := markdown.ParseNote(note.Path, data)
	if err != nil {
		return err
	}
	if asStr(current.Frontmatter["source_integration"]) != cfg.ID || asStr(current.Frontmatter["source_id"]) != item.ExternalID {
		return fmt.Errorf("%w: material ownership changed", ErrConflict)
	}
	body := strings.ReplaceAll(current.Body, "\r\n", "\n")
	source := renderItemBody(cfg, item)
	if item.Kind == integrations.ItemTask && !item.Done {
		for _, task := range current.Tasks {
			if task.BlockID == taskBlockID(cfg, item) && task.Status != domain.StatusDone {
				source = patchSourceStatus(source, task.BlockID, task.Status)
				break
			}
		}
	}

	legacyTask := asStr(current.Frontmatter["source_legacy_task"])
	oldHash := asStr(current.Frontmatter["source_block_hash"])
	if oldHash == "" {
		// Legacy files have no reliable ownership boundary. Keep the whole body,
		// including any local tasks and their IDs, and save an exact backup first.
		backup := path.Join(".lumen/backups/imports", sourceHash(string(data))+".md")
		if !s.vault.Exists(backup) {
			if err := s.vault.Write(backup, data); err != nil {
				return err
			}
		}
		if item.Kind == integrations.ItemTask {
			for _, task := range current.Tasks {
				if task.BlockID == "x"+shortHash(item.ExternalID) {
					legacyTask = task.BlockID
					break
				}
			}
			source = renderSourceWithoutTask(cfg, item)
		}
		body = sourceBegin + "\n" + source + sourceEnd + "\n\n## Сохранённые материалы\n\n" + body
	} else {
		start := strings.Index(body, sourceBegin+"\n")
		end := strings.Index(body, sourceEnd)
		if start < 0 || end < start || strings.Count(body, sourceBegin) != 1 || strings.Count(body, sourceEnd) != 1 {
			return fmt.Errorf("%w: imported section changed locally", ErrConflict)
		}
		contentStart := start + len(sourceBegin) + 1
		if projectionHash(body[contentStart:end]) != oldHash {
			return fmt.Errorf("%w: imported section changed locally; your text is preserved", ErrConflict)
		}
		if asStr(current.Frontmatter["source_legacy"]) == "true" {
			source = renderSourceWithoutTask(cfg, item)
		}
		body = body[:contentStart] + source + body[end:]
	}
	// Legacy IDs remain intact so old links and task descriptions still resolve.
	// Find their line only inside the owning note, never via the global task map.
	if legacyTask != "" {
		_, lines := splitLines([]byte(body))
		parsed, parseErr := markdown.ParseNote(note.Path, []byte(body))
		if parseErr != nil {
			return parseErr
		}
		for _, task := range parsed.Tasks {
			if task.BlockID != legacyTask {
				continue
			}
			want := domain.StatusTodo
			if item.Done {
				want = domain.StatusDone
			}
			if task.Status == want || (!item.Done && task.Status != domain.StatusDone) {
				break
			}
			patched, patchErr := markdown.ApplyTaskPatch(lines[task.Line], markdown.TaskPatch{Status: &want})
			if patchErr != nil {
				return patchErr
			}
			lines[task.Line] = patched
			body = strings.Join(lines, "\n")
			break
		}
	}
	eol, lines := splitLines(data)
	_, bodyStart, hasFM := markdown.SplitFrontmatter(lines)
	if !hasFM {
		return fmt.Errorf("%w: missing source metadata", ErrConflict)
	}
	out := append([]string{}, lines[:bodyStart]...)
	out = append(out, strings.Split(body, "\n")...)
	fields := map[string]string{"source_hash": item.Hash, "source_url": item.URL, "source_block_hash": projectionHash(source)}
	if oldHash == "" {
		fields["source_legacy"] = "true"
	}
	if legacyTask != "" {
		fields["source_legacy_task"] = legacyTask
	}
	for _, key := range sortedKeys(fields) {
		out = markdown.SetFrontmatterKey(out, key, yamlScalar(fields[key]))
	}
	if err := s.vault.Write(note.Path, []byte(strings.Join(out, eol))); err != nil {
		return err
	}
	s.ix.Forget(note.Path)
	return s.ix.ReindexPath(ctx, note.Path)
}

func renderSourceWithoutTask(cfg integrations.Config, item integrations.ExternalItem) string {
	item.Kind = integrations.ItemNote
	return renderItemBody(cfg, item)
}

func renderItemBody(cfg integrations.Config, item integrations.ExternalItem) string {
	var b strings.Builder
	b.WriteString("# " + item.Title + "\n\n")

	if item.Kind == integrations.ItemTask {
		line := "- [ ] " + item.Title
		if item.Done {
			line = "- [x] " + item.Title
		}
		fields := map[string]string{}
		if !item.Due.IsZero() {
			fields["due"] = domain.Date{T: item.Due}.String()
		}
		patched, err := markdown.ApplyTaskPatch(line, markdown.TaskPatch{
			Set: fields, BlockID: taskBlockID(cfg, item),
		})
		if err == nil {
			line = patched
		}
		b.WriteString(line + "\n\n")
	}

	if item.URL != "" {
		b.WriteString("[Открыть в " + cfg.Kind + "](" + item.URL + ")\n\n")
	}
	if item.Location != "" {
		b.WriteString("Место: " + item.Location + "\n\n")
	}
	if body := strings.TrimSpace(item.Body); body != "" {
		b.WriteString("---\n\n" + body + "\n")
	}

	return b.String()
}

// taskBlockID includes the connection and external ID, so the checkbox stays
// findable across syncs without storing another mapping.
func taskBlockID(cfg integrations.Config, item integrations.ExternalItem) string {
	return "x" + sourceHash(cfg.ID + "\x00" + item.ExternalID)[:24]
}

func itemFileName(item integrations.ExternalItem) string {
	name := strings.ToLower(strings.TrimSpace(item.Title))
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "", "*", "", "?", "",
		`"`, "", "<", "", ">", "", "|", "", "#", "", "[", "", "]", "", "\n", " ")
	name = strings.Join(strings.Fields(repl.Replace(name)), "-")
	if len(name) > 60 {
		name = name[:60]
		name = strings.TrimRight(name, "-")
	}
	if name == "" {
		name = "item-" + shortHash(item.ExternalID)
	}
	return name
}

func shortHash(s string) string {
	return markdown.DerivedTaskID(s, "", 0)[1:9]
}

func asStr(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
