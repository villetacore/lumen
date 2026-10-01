package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/markdown"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

// nowFunc is a seam for tests.
var nowFunc = time.Now

// Notes implements note-level use cases.
type Notes struct {
	vault *vault.Vault
	store index.Store
	ix    *Indexer
	bus   *Bus
	log   *slog.Logger
}

func NewNotes(v *vault.Vault, st index.Store, ix *Indexer, bus *Bus, log *slog.Logger) *Notes {
	return &Notes{vault: v, store: st, ix: ix, bus: bus, log: log}
}

func (s *Notes) Get(ctx context.Context, idOrPath string) (*domain.Note, error) {
	return s.store.Note(ctx, idOrPath)
}

func (s *Notes) List(ctx context.Context, q domain.NoteQuery) ([]*domain.Note, int, error) {
	return s.store.Notes(ctx, q)
}

func (s *Notes) Backlinks(ctx context.Context, idOrPath string) ([]*domain.Note, error) {
	return s.store.Backlinks(ctx, idOrPath)
}

// CreateInput describes a new note.
type CreateInput struct {
	Path  string
	Title string
	Type  domain.NoteType
	Tags  []string
	Body  string
	// Extra adds scalar frontmatter keys. Integrations use it for the
	// source_* markers that make their notes idempotent.
	Extra map[string]string
}

// Create writes a new note, refusing to overwrite an existing file.
func (s *Notes) Create(ctx context.Context, in CreateInput) (*domain.Note, error) {
	rel := strings.TrimPrefix(strings.TrimSpace(in.Path), "/")
	if rel == "" {
		return nil, errors.New("path is required")
	}
	if !strings.HasSuffix(rel, ".md") {
		rel += ".md"
	}
	if s.vault.Exists(rel) {
		return nil, fmt.Errorf("%w: %s", errAlreadyExists, rel)
	}
	title := in.Title
	if title == "" {
		title = markdown.TitleFromPath(rel)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + yamlScalar(title) + "\n")
	if in.Type != "" {
		b.WriteString("type: " + string(in.Type) + "\n")
	}
	if len(in.Tags) > 0 {
		b.WriteString("tags: [" + strings.Join(in.Tags, ", ") + "]\n")
	}
	b.WriteString("created: " + nowFunc().Format(time.RFC3339) + "\n")
	for _, k := range sortedKeys(in.Extra) {
		if v := in.Extra[k]; v != "" {
			b.WriteString(k + ": " + yamlScalar(v) + "\n")
		}
	}
	b.WriteString("---\n\n")
	if in.Body != "" {
		b.WriteString(strings.TrimRight(in.Body, "\n") + "\n")
	} else {
		b.WriteString("# " + title + "\n\n")
	}

	if err := s.vault.Write(rel, []byte(b.String())); err != nil {
		return nil, err
	}
	s.ix.Forget(rel)
	if err := s.ix.ReindexPath(ctx, rel); err != nil {
		return nil, err
	}
	return s.store.Note(ctx, rel)
}

var errAlreadyExists = errors.New("note already exists")

// ErrAlreadyExists is returned when Create targets an existing file.
func ErrAlreadyExists() error { return errAlreadyExists }

// ReplaceBody rewrites the body of a note, keeping its frontmatter intact.
func (s *Notes) ReplaceBody(ctx context.Context, idOrPath, body string, revision int64) (*domain.Note, error) {
	n, err := s.store.Note(ctx, idOrPath)
	if err != nil {
		return nil, err
	}
	if revision != 0 && revision != n.Revision {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrConflict, n.Revision, revision)
	}
	data, err := s.vault.Read(n.Path)
	if err != nil {
		return nil, err
	}
	eol, lines := splitLines(data)
	_, bodyStart, hasFM := markdown.SplitFrontmatter(lines)
	if !hasFM {
		bodyStart = 0
	}
	out := append([]string{}, lines[:bodyStart]...)
	out = append(out, strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")...)
	if err := s.vault.Write(n.Path, []byte(strings.Join(out, eol))); err != nil {
		return nil, err
	}
	s.ix.Forget(n.Path)
	if err := s.ix.ReindexPath(ctx, n.Path); err != nil {
		return nil, err
	}
	return s.store.Note(ctx, n.Path)
}

// SetFrontmatter updates scalar frontmatter keys, preserving unknown ones.
func (s *Notes) SetFrontmatter(ctx context.Context, idOrPath string, kv map[string]string, revision int64) (*domain.Note, error) {
	n, err := s.store.Note(ctx, idOrPath)
	if err != nil {
		return nil, err
	}
	if revision != 0 && revision != n.Revision {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrConflict, n.Revision, revision)
	}
	data, err := s.vault.Read(n.Path)
	if err != nil {
		return nil, err
	}
	eol, lines := splitLines(data)
	for k, v := range kv {
		lines = markdown.SetFrontmatterKey(lines, k, v)
	}
	if err := s.vault.Write(n.Path, []byte(strings.Join(lines, eol))); err != nil {
		return nil, err
	}
	s.ix.Forget(n.Path)
	if err := s.ix.ReindexPath(ctx, n.Path); err != nil {
		return nil, err
	}
	return s.store.Note(ctx, n.Path)
}

// Delete moves a note to the vault trash.
func (s *Notes) Delete(ctx context.Context, idOrPath string) error {
	n, err := s.store.Note(ctx, idOrPath)
	if err != nil {
		return err
	}
	if err := s.vault.Trash(n.Path); err != nil {
		return err
	}
	s.ix.Forget(n.Path)
	return s.ix.ReindexPath(ctx, n.Path)
}

// Daily returns the note for a given day, creating it if needed.
func (s *Notes) Daily(ctx context.Context, d domain.Date, create bool) (*domain.Note, error) {
	rel := path.Join("40-daily", d.T.Format("2006"), d.T.Format("2006-01-02")+".md")
	if n, err := s.store.Note(ctx, rel); err == nil {
		return n, nil
	}
	if !create {
		return nil, index.ErrNotFound
	}
	return s.Create(ctx, CreateInput{
		Path:  rel,
		Title: d.T.Format("2006-01-02"),
		Type:  domain.TypeDaily,
		Body:  "# " + d.T.Format("2006-01-02") + "\n\n## Задачи\n\n## Заметки\n",
	})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// yamlScalar quotes a value when YAML would otherwise misread it.
func yamlScalar(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ":#[]{}&*!|>'\"%@`,") || strings.TrimSpace(s) != s {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
