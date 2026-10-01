// Package index holds the derived view of the vault. Everything here can be
// rebuilt from the files, so losing the index never loses data (ADR-0001).
package index

import (
	"context"
	"errors"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

var ErrNotFound = errors.New("not found")

// Store is the query surface used by services. The MVP ships an in-memory
// implementation; a SQLite one lands in M1 without touching callers.
type Store interface {
	PutNote(ctx context.Context, n *domain.Note) error
	DeleteNote(ctx context.Context, path string) error
	Reset(ctx context.Context) error
	Paths(ctx context.Context) ([]string, error)

	Note(ctx context.Context, idOrPath string) (*domain.Note, error)
	// NoteBySource finds a note previously created by an integration.
	NoteBySource(ctx context.Context, owner, externalID string) (*domain.Note, error)
	Notes(ctx context.Context, q domain.NoteQuery) ([]*domain.Note, int, error)
	Backlinks(ctx context.Context, idOrPath string) ([]*domain.Note, error)

	Task(ctx context.Context, id string) (*domain.Task, error)
	Tasks(ctx context.Context, q domain.TaskQuery) ([]*domain.Task, int, error)

	Events(ctx context.Context, from, to time.Time) ([]domain.Event, error)

	// PutExternal replaces the calendar entries an integration contributes.
	// They live only in the index: a work calendar has no business creating
	// thousands of files in the vault.
	PutExternal(ctx context.Context, integrationID string, events []domain.Event) error
	DeleteExternal(ctx context.Context, integrationID string) error
	Search(ctx context.Context, q string, limit int) ([]domain.SearchHit, error)
	Tags(ctx context.Context) ([]domain.TagCount, error)

	Stats(ctx context.Context) (domain.Stats, error)
	SetIndexed(ctx context.Context, at time.Time, tookMS int64)
}
