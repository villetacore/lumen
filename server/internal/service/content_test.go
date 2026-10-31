package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

func TestProjectDocumentsAndNestedEntriesSurviveReindex(t *testing.T) {
	svc, store, _, _ := newIntegrationsFixture(t)
	ctx := context.Background()
	project, err := svc.notes.Create(ctx, CreateInput{Title: "Product", Type: domain.TypeProject, Body: "Detailed project description"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.notes.Create(ctx, CreateInput{Title: "Setup", Parent: "note:" + project.ID, ContentKind: "document", Body: "# Setup\n\nInstructions"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.notes.Create(ctx, CreateInput{Title: "Decision", Parent: "note:" + doc.ID, ContentKind: "entry", Body: "We chose this approach"})
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild all projections entirely from persisted Markdown.
	if err := store.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ix.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	docs, total, err := store.Notes(ctx, domain.NoteQuery{Parent: "note:" + project.ID, ContentKind: "document"})
	if err != nil || total != 1 || docs[0].ID != doc.ID {
		t.Fatalf("docs=%+v total=%d err=%v", docs, total, err)
	}
	nested, total, err := store.Notes(ctx, domain.NoteQuery{Parent: "note:" + doc.ID})
	if err != nil || total != 1 || nested[0].ID != child.ID || nested[0].Body != child.Body {
		t.Fatalf("nested=%+v total=%d err=%v", nested, total, err)
	}
	other, err := svc.notes.Create(ctx, CreateInput{Title: "Other", Type: domain.TypeProject})
	if err != nil {
		t.Fatal(err)
	}
	_, total, err = store.Notes(ctx, domain.NoteQuery{Parent: "note:" + other.ID})
	if err != nil || total != 0 {
		t.Fatalf("unrelated project included children: %d %v", total, err)
	}
}

func TestTaskDescriptionCreationIsUniqueAndConflictsPreserveText(t *testing.T) {
	svc, store, _, _ := newIntegrationsFixture(t)
	ctx := context.Background()
	n, err := svc.notes.Create(ctx, CreateInput{Title: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, n.Path, "Do work", "", TaskUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	parent := "task:" + task.ID
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := svc.notes.Create(ctx, CreateInput{Title: "Description", Parent: parent, ContentKind: "description", Body: "Requirements"})
			outcomes <- err
		})
	}
	wg.Wait()
	close(outcomes)
	success, conflict := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, ErrAlreadyExists()) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	docs, total, err := store.Notes(ctx, domain.NoteQuery{Parent: parent, ContentKind: "description"})
	if err != nil || total != 1 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	original := docs[0]
	if _, err := svc.notes.ReplaceBody(ctx, original.ID, "New requirements", original.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.notes.ReplaceBody(ctx, original.ID, "Stale draft", original.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	got, _ := store.Note(ctx, original.ID)
	if got.Body != "New requirements" {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestContentRejectsMissingParents(t *testing.T) {
	svc, _, _, _ := newIntegrationsFixture(t)
	for _, parent := range []string{"note:missing", "task:missing", "garbage", "other:id"} {
		if _, err := svc.notes.Create(context.Background(), CreateInput{Title: "Child", Parent: parent, ContentKind: "entry"}); err == nil {
			t.Fatalf("accepted %q", parent)
		}
	}
	if _, err := svc.notes.Create(context.Background(), CreateInput{ContentKind: "description"}); err == nil {
		t.Fatal("accepted orphan description")
	}
}
