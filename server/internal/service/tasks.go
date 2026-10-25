package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/markdown"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

var (
	ErrTaskGone  = errors.New("task no longer matches the file")
	ErrConflict  = errors.New("revision mismatch")
	ErrNoteEmpty = errors.New("note has no content")
)

// vaultWrites serialises every read-modify-write of a vault file made through
// Notes and Tasks. Without it two requests that touch the same note (two quick
// adds into today's daily note, say) both read the old file and the second
// write silently drops the first one's change.
var vaultWrites sync.Mutex

// Tasks implements the task use cases. Every write goes through
// markdown.ApplyTaskPatch so only the task's own line is rewritten (ADR-0003).
type Tasks struct {
	vault *vault.Vault
	store index.Store
	ix    *Indexer
	bus   *Bus
	log   *slog.Logger
}

func NewTasks(v *vault.Vault, st index.Store, ix *Indexer, bus *Bus, log *slog.Logger) *Tasks {
	return &Tasks{vault: v, store: st, ix: ix, bus: bus, log: log}
}

// TaskUpdate carries the fields the client wants changed. A nil pointer means
// "leave alone"; a pointer to an empty string clears the field.
type TaskUpdate struct {
	Status    *domain.TaskStatus
	Text      *string
	Due       *string
	Scheduled *string
	Start     *string
	Priority  *string
	Estimate  *string
	Project   *string
	Revision  int64 // 0 skips the optimistic check
}

func (s *Tasks) Get(ctx context.Context, id string) (*domain.Task, error) {
	return s.store.Task(ctx, id)
}

func (s *Tasks) Query(ctx context.Context, q domain.TaskQuery) ([]*domain.Task, int, error) {
	return s.store.Tasks(ctx, q)
}

// Update rewrites one task line in its note and reindexes the file.
func (s *Tasks) Update(ctx context.Context, id string, up TaskUpdate) (*domain.Task, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	t, err := s.store.Task(ctx, id)
	if err != nil {
		return nil, err
	}
	note, err := s.store.Note(ctx, t.NotePath)
	if err != nil {
		return nil, err
	}
	if up.Revision != 0 && up.Revision != note.Revision {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrConflict, note.Revision, up.Revision)
	}

	data, err := s.vault.Read(t.NotePath)
	if err != nil {
		return nil, err
	}
	eol, lines := splitLines(data)

	lineNo, err := locateTask(lines, t)
	if err != nil {
		return nil, err
	}

	patch := markdown.TaskPatch{Set: map[string]string{}}
	if up.Status != nil {
		patch.Status = up.Status
		// Completing a task stamps the completion date; reopening clears it.
		switch {
		case *up.Status == domain.StatusDone && t.DoneAt.IsZero():
			patch.Set["done"] = domain.Today().String()
		case *up.Status != domain.StatusDone && !t.DoneAt.IsZero():
			patch.Set["done"] = ""
		}
	}
	if up.Text != nil {
		patch.Text = up.Text
	}
	for key, v := range map[string]*string{
		"due": up.Due, "scheduled": up.Scheduled, "start": up.Start,
		"prio": up.Priority, "est": up.Estimate, "project": up.Project,
	} {
		if v == nil {
			continue
		}
		val := strings.TrimSpace(*v)
		if val != "" && (key == "due" || key == "scheduled" || key == "start") {
			d, err := parseDateInput(val)
			if err != nil {
				return nil, fmt.Errorf("bad %s: %w", key, err)
			}
			val = d.String()
		}
		patch.Set[key] = val
	}

	// Give the task a stable block id the first time it is written through the API.
	if t.BlockID == "" {
		patch.BlockID = newBlockID()
	}

	newLine, err := markdown.ApplyTaskPatch(lines[lineNo], patch)
	if err != nil {
		return nil, err
	}
	if newLine == lines[lineNo] {
		return t, nil
	}
	lines[lineNo] = newLine

	if err := s.vault.Write(t.NotePath, []byte(strings.Join(lines, eol))); err != nil {
		return nil, err
	}
	s.ix.Forget(t.NotePath)
	if err := s.ix.ReindexPath(ctx, t.NotePath); err != nil {
		return nil, err
	}

	newID := t.BlockID
	if newID == "" {
		newID = patch.BlockID
	}
	updated, err := s.store.Task(ctx, newID)
	if err != nil {
		return nil, err
	}
	s.bus.Publish(Event{Type: "task.changed", ID: updated.ID, Status: string(updated.Status), Path: t.NotePath})
	return updated, nil
}

// Create appends a task to a note, optionally under a "## Section" heading.
func (s *Tasks) Create(ctx context.Context, notePath, text, section string, up TaskUpdate) (*domain.Task, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty task text")
	}
	if !strings.HasSuffix(notePath, ".md") {
		notePath += ".md"
	}
	data, err := s.vault.Read(notePath)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		return nil, err
	}
	eol, lines := splitLines(data)

	blockID := newBlockID()
	line := "- [ ] " + strings.TrimSpace(text)
	fields := map[string]string{}
	if up.Due != nil && *up.Due != "" {
		d, err := parseDateInput(*up.Due)
		if err != nil {
			return nil, err
		}
		fields["due"] = d.String()
	}
	if up.Scheduled != nil && *up.Scheduled != "" {
		d, err := parseDateInput(*up.Scheduled)
		if err != nil {
			return nil, err
		}
		fields["scheduled"] = d.String()
	}
	if up.Priority != nil && *up.Priority != "" {
		fields["prio"] = *up.Priority
	}
	if up.Project != nil && *up.Project != "" {
		fields["project"] = *up.Project
	}
	line, err = markdown.ApplyTaskPatch(line, markdown.TaskPatch{Set: fields, BlockID: blockID})
	if err != nil {
		return nil, err
	}

	lines = insertTask(lines, line, section)
	if err := s.vault.Write(notePath, []byte(strings.Join(lines, eol))); err != nil {
		return nil, err
	}
	s.ix.Forget(notePath)
	if err := s.ix.ReindexPath(ctx, notePath); err != nil {
		return nil, err
	}
	return s.store.Task(ctx, blockID)
}

// Delete removes a task's line from its note.
func (s *Tasks) Delete(ctx context.Context, id string) error {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	t, err := s.store.Task(ctx, id)
	if err != nil {
		return err
	}
	data, err := s.vault.Read(t.NotePath)
	if err != nil {
		return err
	}
	eol, lines := splitLines(data)
	lineNo, err := locateTask(lines, t)
	if err != nil {
		return err
	}
	lines = append(lines[:lineNo], lines[lineNo+1:]...)
	if err := s.vault.Write(t.NotePath, []byte(strings.Join(lines, eol))); err != nil {
		return err
	}
	s.ix.Forget(t.NotePath)
	if err := s.ix.ReindexPath(ctx, t.NotePath); err != nil {
		return err
	}
	s.bus.Publish(Event{Type: "task.changed", ID: t.ID, Path: t.NotePath})
	return nil
}

// locateTask finds the task's line, tolerating edits made to the file elsewhere
// since it was indexed.
func locateTask(lines []string, t *domain.Task) (int, error) {
	if t.Line < len(lines) && sameTask(lines[t.Line], t) {
		return t.Line, nil
	}
	for i, l := range lines {
		if sameTask(l, t) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: %s in %s", ErrTaskGone, t.ID, t.NotePath)
}

func sameTask(line string, t *domain.Task) bool {
	parsed := markdown.BuildTask(t.NotePath, t.NoteID, t.Line, line)
	if parsed == nil {
		return false
	}
	if t.BlockID != "" {
		return parsed.BlockID == t.BlockID
	}
	return parsed.Text == t.Text
}

// insertTask places a new task under the given heading, or at the end of the file.
func insertTask(lines []string, taskLine, section string) []string {
	if section != "" {
		want := strings.ToLower(strings.TrimSpace(section))
		for i, l := range lines {
			trimmed := strings.TrimSpace(l)
			if !strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "# "))) != want {
				continue
			}
			// Insert after the last consecutive list item of that section.
			j := i + 1
			last := j
			for ; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if strings.HasPrefix(t, "#") {
					break
				}
				if t != "" {
					last = j + 1
				}
			}
			out := make([]string, 0, len(lines)+1)
			out = append(out, lines[:last]...)
			out = append(out, taskLine)
			return append(out, lines[last:]...)
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return append(lines, taskLine, "")
}

// splitLines keeps the file's original line ending so writes stay byte-faithful.
func splitLines(data []byte) (eol string, lines []string) {
	s := string(data)
	eol = "\n"
	if strings.Contains(s, "\r\n") {
		eol = "\r\n"
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	return eol, strings.Split(s, "\n")
}

func parseDateInput(v string) (domain.Date, error) {
	if d, ok := domain.RelativeInput(v, nowFunc()); ok {
		return d, nil
	}
	return domain.ParseDate(v)
}

// newBlockID returns a short, file-safe identifier for "^blockid".
func newBlockID() string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "l" + strings.ToLower(base32.StdEncoding.EncodeToString([]byte("fallback"))[:7])
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}
