package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

func TestParseTaskLine(t *testing.T) {
	cases := []struct {
		line   string
		ok     bool
		status domain.TaskStatus
		text   string
		block  string
	}{
		{"- [ ] Купить молоко", true, domain.StatusTodo, "Купить молоко", ""},
		{"  * [x] Готово ^abc123", true, domain.StatusDone, "Готово", "abc123"},
		{"\t- [/] В работе", true, domain.StatusDoing, "В работе", ""},
		{"1. [-] Отменено", true, domain.StatusCancelled, "Отменено", ""},
		{"- [>] Перенесено", true, domain.StatusDeferred, "Перенесено", ""},
		{"- обычный пункт", false, "", "", ""},
		{"# Заголовок", false, "", "", ""},
		{"-[ ] без пробела", false, "", "", ""},
	}
	for _, c := range cases {
		task := BuildTask("n.md", "n.md", 0, c.line)
		if (task != nil) != c.ok {
			t.Fatalf("%q: parsed=%v want %v", c.line, task != nil, c.ok)
		}
		if task == nil {
			continue
		}
		if task.Status != c.status || task.Text != c.text || task.BlockID != c.block {
			t.Errorf("%q: got status=%s text=%q block=%q", c.line, task.Status, task.Text, task.BlockID)
		}
	}
}

func TestInlineFields(t *testing.T) {
	line := "- [ ] Написать спец [due:: 2026-09-10] [prio:: A] [project:: [[Запуск Lumen]]] #work ^k5q9v3"
	task := BuildTask("n.md", "n.md", 3, line)
	if task == nil {
		t.Fatal("expected a task")
	}
	if got := task.Due.String(); got != "2026-09-10" {
		t.Errorf("due = %q", got)
	}
	if task.Priority != domain.PrioA {
		t.Errorf("priority = %q", task.Priority)
	}
	if task.Project != "Запуск Lumen" {
		t.Errorf("project = %q", task.Project)
	}
	if task.BlockID != "k5q9v3" {
		t.Errorf("block id = %q", task.BlockID)
	}
	if len(task.Tags) != 1 || task.Tags[0] != "work" {
		t.Errorf("tags = %v", task.Tags)
	}
	if task.Text != "Написать спец #work" {
		t.Errorf("text = %q", task.Text)
	}
}

func TestApplyTaskPatchKeepsTheRestOfTheLine(t *testing.T) {
	done := domain.StatusDone
	in := "  - [ ] Купить молоко #дом [due:: 2026-09-10] ^m1a2b4"
	out, err := ApplyTaskPatch(in, TaskPatch{
		Status: &done,
		Set:    map[string]string{"done": "2026-09-08"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "  - [x] Купить молоко #дом [due:: 2026-09-10] [done:: 2026-09-08] ^m1a2b4"
	if out != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

func TestApplyTaskPatchAppendsInCanonicalOrder(t *testing.T) {
	out, err := ApplyTaskPatch("- [ ] Прочитать статью", TaskPatch{
		Set:     map[string]string{"prio": "B", "due": "2026-09-09"},
		BlockID: "4fuxbusg",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "- [ ] Прочитать статью [due:: 2026-09-09] [prio:: B] ^4fuxbusg"
	if out != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

func TestApplyTaskPatchRemovesField(t *testing.T) {
	out, err := ApplyTaskPatch("- [ ] Задача [due:: 2026-09-10] [prio:: A] ^x1", TaskPatch{
		Set: map[string]string{"due": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "- [ ] Задача [prio:: A] ^x1"
	if out != want {
		t.Errorf("got  %q\nwant %q", out, want)
	}
}

func TestParseNote(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"id: 01K5Q9V3JX8ZQ2N6C1B7M4TDA0",
		"title: Запуск Lumen",
		"type: project",
		"tags: [work, lumen]",
		"---",
		"",
		"# Запуск Lumen",
		"",
		"Связано с [[Архитектура Lumen|архитектурой]] и #дизайн/формат.",
		"",
		"- [ ] Спецификация [due:: 2026-09-10] ^p1",
		"",
		"```",
		"- [ ] не задача #нетег",
		"```",
		"",
	}, "\n")

	n, err := ParseNote("20-projects/lumen.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "01K5Q9V3JX8ZQ2N6C1B7M4TDA0" {
		t.Errorf("id = %q", n.ID)
	}
	if n.Title != "Запуск Lumen" || n.Type != domain.TypeProject {
		t.Errorf("title/type = %q/%q", n.Title, n.Type)
	}
	if len(n.Tasks) != 1 {
		t.Fatalf("tasks = %d, fenced code must be ignored", len(n.Tasks))
	}
	if n.Tasks[0].Line != 11 {
		t.Errorf("task line = %d, want 11 (absolute in file)", n.Tasks[0].Line)
	}
	if len(n.Links) != 1 || n.Links[0].Target != "Архитектура Lumen" || n.Links[0].Alias != "архитектурой" {
		t.Errorf("links = %+v", n.Links)
	}
	want := map[string]bool{"work": true, "lumen": true, "дизайн/формат": true}
	for _, tag := range n.Tags {
		delete(want, tag)
	}
	if len(want) != 0 {
		t.Errorf("missing tags %v, got %v", want, n.Tags)
	}
}

func TestParseNoteDailyFallback(t *testing.T) {
	n, err := ParseNote("40-daily/2026/2026-09-08.md", []byte("# 2026-09-08\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n.Type != domain.TypeDaily {
		t.Errorf("type = %q, want daily", n.Type)
	}
	if n.Start.String() != "2026-09-08" {
		t.Errorf("start = %q", n.Start.String())
	}
}

// TestGoldenFixtures runs the shared spec fixtures. The Rust implementation in
// desktop/core must produce the same values (ADR-0004).
func TestGoldenFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "spec", "testdata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no fixtures: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		n, err := ParseNote(e.Name(), data)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		// Round-trip check: rewriting every task without changes must be a no-op.
		for _, task := range n.Tasks {
			out, err := ApplyTaskPatch(task.Raw, TaskPatch{})
			if err != nil {
				t.Errorf("%s:%d %v", e.Name(), task.Line, err)
				continue
			}
			if out != task.Raw {
				t.Errorf("%s:%d not lossless\n got  %q\n want %q", e.Name(), task.Line, out, task.Raw)
			}
		}
	}
}
