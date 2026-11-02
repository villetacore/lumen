package markdown

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
)

func parseFixture(t *testing.T, name, rel string) *domain.Note {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	n, err := ParseNote(rel, data)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParseTrackerProjectFixture(t *testing.T) {
	n := parseFixture(t, "tracker-project.md", "20-projects/web.md")
	p, ok := ParseProject(n)
	if !ok {
		t.Fatal("project with key must be a tracker project")
	}
	if p.Key != "WEB" || p.Seq != 12 || p.Title != "Сайт студии" {
		t.Errorf("project = %+v", p)
	}
	if len(p.Workflow) != 4 || p.Workflow[1].ID != "in-progress" ||
		p.Workflow[1].Category != domain.CategoryInProgress || p.Workflow[1].WIP != 3 {
		t.Errorf("workflow = %+v", p.Workflow)
	}
	if !p.CanMove("todo", "in-progress") || p.CanMove("todo", "done") {
		t.Errorf("transitions not applied: %+v", p.Transitions)
	}
	if got := IssuePath(p, "WEB-13"); got != "20-projects/WEB/issues/WEB-13.md" {
		t.Errorf("issue path = %q", got)
	}
}

func TestPlainProjectIsNotTracker(t *testing.T) {
	n, _ := ParseNote("20-projects/x.md", []byte("---\ntype: project\n---\n# X\n"))
	if _, ok := ParseProject(n); ok {
		t.Fatal("project without key must stay a plain project")
	}
	n, _ = ParseNote("20-projects/x.md", []byte("---\ntype: project\nkey: web-1\n---\n"))
	if _, ok := ParseProject(n); ok {
		t.Fatal("invalid key must be ignored")
	}
}

func TestParseIssueFixture(t *testing.T) {
	n := parseFixture(t, "tracker-issue.md", "20-projects/WEB/issues/WEB-12.md")
	if n.Type != domain.TypeIssue || n.ID != "WEB-12" {
		t.Fatalf("type/id = %q/%q", n.Type, n.ID)
	}
	is := ParseIssue(n)
	if is.Key != "WEB-12" || is.Project != "WEB" || is.Status != "in-progress" || is.Type != "story" {
		t.Errorf("issue = %+v", is)
	}
	if is.Assignee != "alice" || is.Reporter != "bob" || is.Points != 3 || is.Due.String() != "2026-11-10" {
		t.Errorf("fields = %+v", is)
	}
	if strings.Join(is.Labels, ",") != "web,board" || strings.Join(is.Watchers, ",") != "carol" {
		t.Errorf("labels/watchers = %v / %v", is.Labels, is.Watchers)
	}
	var links []string
	for _, l := range is.Links {
		links = append(links, l.Type+":"+l.Target)
	}
	sort.Strings(links)
	if strings.Join(links, ",") != "blocks:WEB-14,relates:WEB-2,relates:WEB-3" {
		t.Errorf("links = %v", links)
	}
	if !strings.HasPrefix(is.Description, "Колонки строятся") || strings.Contains(is.Description, "Комментарии") {
		t.Errorf("description = %q", is.Description)
	}
	if len(is.Checklist) != 2 {
		t.Errorf("checklist = %d, tasks from comments must not count", len(is.Checklist))
	}
	if len(is.Comments) != 2 {
		t.Fatalf("comments = %d, a heading inside a fence is not a comment", len(is.Comments))
	}
	c := is.Comments[0]
	if c.ID != "c-7f3a" || c.Author != "alice" || c.At.Format("2006-01-02 15:04") != "2026-11-02 12:30" {
		t.Errorf("comment = %+v", c)
	}
	if !strings.Contains(c.Body, "### это не новый комментарий") {
		t.Errorf("comment body = %q", c.Body)
	}
}

func TestIssueKeyFallsBackToFileName(t *testing.T) {
	n, _ := ParseNote("x/WEB-7.md", []byte("---\ntype: issue\n---\n# Без id\n"))
	is := ParseIssue(n)
	if is.Key != "WEB-7" || is.Project != "WEB" || is.Status != "todo" || is.Type != "task" {
		t.Errorf("issue = %+v", is)
	}
}

func TestSetFrontmatterValueIsLossless(t *testing.T) {
	src := []string{
		"---",
		"id: WEB-1",
		"# комментарий в шапке",
		"labels:",
		"  - a",
		"  - b",
		"status: todo",
		"custom: {x: 1}",
		"---",
		"Тело",
	}
	out := SetFrontmatterValue(append([]string{}, src...), "labels", []string{"web", "true", "a b", "x:y"})
	out = SetFrontmatterValue(out, "status", "in-progress")
	out = SetFrontmatterValue(out, "due", "2026-11-10")
	out = SetFrontmatterValue(out, "custom", nil)
	out = SetFrontmatterValue(out, "title", "Запуск: этап 2")
	want := []string{
		"---",
		"id: WEB-1",
		"# комментарий в шапке",
		`labels: [web, "true", a b, "x:y"]`,
		"status: in-progress",
		"due: 2026-11-10",
		`title: "Запуск: этап 2"`,
		"---",
		"Тело",
	}
	if strings.Join(out, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(out, "\n"))
	}
	n, err := ParseNote("a.md", []byte(strings.Join(out, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	fm := &Frontmatter{Fields: n.Frontmatter}
	if got := strings.Join(fm.Strings("labels"), "|"); got != "web|true|a b|x:y" {
		// "a b" is one flow item: Strings splits scalars only, not sequences.
		t.Errorf("labels = %q", got)
	}
	if n.Title != "Запуск: этап 2" {
		t.Errorf("title = %q", n.Title)
	}
}

func TestRankBetween(t *testing.T) {
	cases := [][2]string{{"", ""}, {"", "i"}, {"i", ""}, {"a", "b"}, {"a", "a1"}, {"az", "b"}, {"zz", ""}, {"", "01"}, {"b", "a"}}
	for _, c := range cases {
		got := RankBetween(c[0], c[1])
		next := c[1]
		if next != "" && c[0] >= next {
			next = ""
		}
		if got <= c[0] || (next != "" && got >= next) || strings.HasSuffix(got, "0") {
			t.Errorf("RankBetween(%q, %q) = %q", c[0], c[1], got)
		}
	}
	// Repeated insertion at the same spot keeps producing ordered keys.
	lo, hi := "a", "b"
	for i := 0; i < 50; i++ {
		m := RankBetween(lo, hi)
		if !(lo < m && m < hi) {
			t.Fatalf("step %d: %q not in (%q, %q)", i, m, lo, hi)
		}
		hi = m
	}
}

func TestParseSprintFixture(t *testing.T) {
	n := parseFixture(t, "tracker-sprint.md", "20-projects/WEB/sprints/WEB-S3.md")
	sp := ParseSprint(n)
	if sp == nil || sp.ID != "WEB-S3" || sp.Project != "WEB" || sp.State != domain.SprintActive ||
		sp.Name != "Спринт 3" || sp.Goal != "Запустить главную страницу" ||
		sp.Start.String() != "2026-11-02" || sp.End.String() != "2026-11-16" {
		t.Fatalf("sprint = %+v", sp)
	}
	// A sprint with dates is a calendar entry like any dated note.
	if ev := Events(n); len(ev) != 1 || ev[0].Title != "Спринт 3" {
		t.Errorf("events = %+v", ev)
	}
	bare, _ := ParseNote("x/WEB-S1.md", []byte("---\ntype: sprint\nstate: odd\n---\n"))
	if sp := ParseSprint(bare); sp.ID != "WEB-S1" || sp.Project != "WEB" || sp.State != domain.SprintPlanned {
		t.Errorf("defaults = %+v", sp)
	}
}
