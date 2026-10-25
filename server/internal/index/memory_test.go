package index

import (
	"context"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

func TestNoteResolvesLikeWikilink(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(t.TempDir())
	for _, n := range []*domain.Note{
		{ID: "a", Path: "10-notes/architecture.md", Title: "Архитектура Lumen", Hash: "1"},
		{ID: "b", Path: "10-notes/Интеграции.md", Title: "Интеграции и сервисы", Hash: "2"},
	} {
		if err := m.PutNote(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]string{
		"10-notes/architecture.md": "a", // path
		"10-notes/architecture":    "a", // path without extension
		"architecture":             "a", // file name
		"архитектура lumen":        "a", // title, any case
		"Интеграции":               "b", // file name wins over another note's title
		"Интеграции.md":            "b",
	}
	for in, want := range cases {
		n, err := m.Note(ctx, in)
		if err != nil || n.ID != want {
			t.Errorf("Note(%q) = %v, %v; want %s", in, n, err, want)
		}
	}
	if _, err := m.Note(ctx, "нет такой"); err != ErrNotFound {
		t.Errorf("missing note: err = %v", err)
	}
}
