package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

func strp(s string) *string { return &s }

func TestSprintLifecycle(t *testing.T) {
	s, dir := trackerVault(t)
	ctx := context.Background()
	s1, err := s.CreateSprint(ctx, "web", SprintInput{Goal: strp("Главная")})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := s.CreateSprint(ctx, "WEB", SprintInput{Name: strp("Полировка")})
	if err != nil {
		t.Fatal(err)
	}
	if s1.ID != "WEB-S1" || s1.Name != "Спринт 1" || s1.State != domain.SprintPlanned || s2.ID != "WEB-S2" {
		t.Fatalf("sprints = %+v / %+v", s1, s2)
	}
	if _, err := os.Stat(filepath.Join(dir, "20-projects/WEB/sprints/WEB-S1.md")); err != nil {
		t.Fatal(err)
	}

	// Plan: a, b, c into S1; d stays in the backlog.
	for _, title := range []string{"a", "b", "c", "d"} {
		in := IssueInput{Project: "WEB", Title: title}
		if title != "d" {
			in.Sprint = "WEB-S1"
		}
		if _, err := s.Create(ctx, in, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Moving into a sprint and reordering is one write.
	if _, err := s.Move(ctx, "WEB-4", MoveInput{Sprint: strp("WEB-S2"), After: "WEB-1"}); err != nil {
		t.Fatal(err)
	}
	if is, _ := s.Get(ctx, "WEB-4"); is.Sprint != "WEB-S2" {
		t.Fatalf("sprint after move = %q", is.Sprint)
	}

	if _, _, err := s.CompleteSprint(ctx, "WEB-S1", ""); !errors.Is(err, ErrSprintState) {
		t.Fatalf("completing a planned sprint: %v", err)
	}
	started, err := s.StartSprint(ctx, "WEB-S1", SprintInput{})
	if err != nil {
		t.Fatal(err)
	}
	if started.State != domain.SprintActive || started.Start.IsZero() || started.End.IsZero() {
		t.Fatalf("started = %+v", started)
	}
	if _, err := s.StartSprint(ctx, "WEB-S2", SprintInput{}); !errors.Is(err, ErrSprintState) {
		t.Fatalf("second active sprint must be refused: %v", err)
	}

	// Finish a, close the sprint moving the rest to S2.
	if _, err := s.Move(ctx, "WEB-1", MoveInput{Status: "doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(ctx, "WEB-1", MoveInput{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	closed, moved, err := s.CompleteSprint(ctx, "WEB-S1", "WEB-S2")
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != domain.SprintClosed || closed.Completed.IsZero() || moved != 2 {
		t.Fatalf("closed = %+v, moved = %d", closed, moved)
	}
	for key, want := range map[string]string{"WEB-1": "WEB-S1", "WEB-2": "WEB-S2", "WEB-3": "WEB-S2"} {
		if is, _ := s.Get(ctx, key); is.Sprint != want {
			t.Errorf("%s sprint = %q, want %q", key, is.Sprint, want)
		}
	}
	if _, err := s.Update(ctx, "WEB-4", IssueUpdate{Sprint: strp("WEB-S1")}); !errors.Is(err, ErrSprintState) {
		t.Fatalf("closed sprint must not take issues: %v", err)
	}
	if _, err := s.Update(ctx, "WEB-4", IssueUpdate{Sprint: strp("WEB-S9")}); err == nil {
		t.Fatal("unknown sprint must be refused")
	}
	if _, err := s.StartSprint(ctx, "WEB-S2", SprintInput{}); err != nil {
		t.Fatalf("after closing S1, S2 may start: %v", err)
	}

	list, err := s.Sprints(ctx, "WEB")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, sp := range list {
		order = append(order, sp.ID+":"+string(sp.State))
	}
	if strings.Join(order, ",") != "WEB-S2:active,WEB-S1:closed" {
		t.Fatalf("order = %v", order)
	}
}

// A crash after some issues moved but before the sprint was marked closed
// leaves it active; completing again moves the rest and closes it.
func TestCompleteSprintResumes(t *testing.T) {
	s, dir := trackerVault(t)
	ctx := context.Background()
	if _, err := s.CreateSprint(ctx, "WEB", SprintInput{}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"a", "b"} {
		if _, err := s.Create(ctx, IssueInput{Project: "WEB", Title: title, Sprint: "WEB-S1"}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.StartSprint(ctx, "WEB-S1", SprintInput{}); err != nil {
		t.Fatal(err)
	}
	// Simulate the half-done completion: WEB-1 already went to the backlog.
	if _, err := s.Update(ctx, "WEB-1", IssueUpdate{Sprint: strp("")}); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := newIssues(t, dir)
	sp, moved, err := restarted.CompleteSprint(ctx, "WEB-S1", "")
	if err != nil || moved != 1 || sp.State != domain.SprintClosed {
		t.Fatalf("resume: %+v moved=%d err=%v", sp, moved, err)
	}
	for _, key := range []string{"WEB-1", "WEB-2"} {
		if is, _ := restarted.Get(ctx, key); is.Sprint != "" {
			t.Errorf("%s still in sprint %q", key, is.Sprint)
		}
	}
}
