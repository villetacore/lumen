package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type glCall struct {
	path  string
	query url.Values
	auth  string
}

// glServer answers like GitLab: lists in pages, with X-Next-Page.
func glServer(t *testing.T, routes map[string][][]map[string]any, status int) (*httptest.Server, *[]glCall) {
	t.Helper()
	var calls []glCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, glCall{r.URL.EscapedPath(), r.URL.Query(), r.Header.Get("Authorization")})
		if status != 0 {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"message":"нет"}`)
			return
		}
		pages := routes[r.URL.EscapedPath()]
		var page int
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		if page < 1 || page > len(pages) {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		if page < len(pages) {
			w.Header().Set("X-Next-Page", fmt.Sprint(page+1))
		}
		json.NewEncoder(w).Encode(pages[page-1])
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func glInput(srv *httptest.Server, values map[string]string, state map[string]string) SyncInput {
	cfg := Config{Kind: "gitlab", ID: "gl", Values: map[string]string{"url": srv.URL, "token": "env:T"}}
	for k, v := range values {
		cfg.Values[k] = v
	}
	return SyncInput{
		Config: cfg, State: state, HTTP: srv.Client(),
		Secret: func(context.Context, string) ([]byte, error) { return []byte("tok-1\n"), nil },
	}
}

func TestGitLabImportsIssuesWithPagesAndFields(t *testing.T) {
	srv, calls := glServer(t, map[string][][]map[string]any{
		"/api/v4/projects/group%2Fapp/issues": {
			{{"iid": 1, "title": "Первая", "description": "Текст", "state": "opened", "web_url": "https://g/1",
				"updated_at": "2026-11-01T10:00:00.000+03:00", "due_date": "2026-11-20", "labels": []string{"Bug Fix"},
				"references": map[string]string{"full": "group/app#1"}}},
			{{"iid": 2, "title": "Вторая", "state": "closed", "updated_at": "2026-11-02T10:00:00Z",
				"milestone":  map[string]string{"due_date": "2026-12-01"},
				"references": map[string]string{"full": "group/app#2"}}},
		},
	}, 0)
	out, err := (&GitLab{}).Sync(context.Background(), glInput(srv, map[string]string{"project": "group/app"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || len(*calls) != 2 {
		t.Fatalf("элементов %d, запросов %d", len(out.Items), len(*calls))
	}
	first, second := out.Items[0], out.Items[1]
	if first.ExternalID != "group/app#1" || first.Title != "group/app#1 Первая" || first.Body != "Текст" ||
		first.Kind != ItemTask || first.Done || first.URL != "https://g/1" {
		t.Errorf("первая: %+v", first)
	}
	if first.Due.Format("2006-01-02") != "2026-11-20" || first.Labels[0] != "bug-fix" {
		t.Errorf("срок/метка: %v %v", first.Due, first.Labels)
	}
	if !second.Done || second.Due.Format("2006-01-02") != "2026-12-01" {
		t.Errorf("вторая: %+v", second)
	}
	if got := out.State["since"]; got != "2026-11-02T10:00:00Z" {
		t.Errorf("cursor: %s", got)
	}
	c := (*calls)[0]
	if c.auth != "Bearer tok-1" {
		t.Errorf("заголовок: %q", c.auth)
	}
	if c.query.Get("state") != "opened" || c.query.Get("confidential") != "false" || c.query.Get("updated_after") != "" {
		t.Errorf("запрос: %v", c.query)
	}
}

// An issue closed in GitLab must close here too, though the config asks for
// open ones only.
func TestGitLabIncrementalRunAsksForClosedToo(t *testing.T) {
	srv, calls := glServer(t, map[string][][]map[string]any{
		"/api/v4/projects/g%2Fa/issues": {{{"iid": 1, "title": "Закрыли", "state": "closed", "updated_at": "2026-11-03T00:00:00Z",
			"references": map[string]string{"full": "g/a#1"}}}},
	}, 0)
	prev := map[string]string{"since": "2026-11-02T00:00:00Z"}
	out, err := (&GitLab{}).Sync(context.Background(), glInput(srv, map[string]string{"project": "g/a"}, prev))
	if err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.query.Get("state") != "all" || c.query.Get("updated_after") != prev["since"] {
		t.Errorf("запрос: %v", c.query)
	}
	if len(out.Items) != 1 || !out.Items[0].Done || out.State["since"] != "2026-11-03T00:00:00Z" {
		t.Errorf("результат: %+v %v", out.Items, out.State)
	}
}

func TestGitLabScopes(t *testing.T) {
	mr := map[string]any{"iid": 7, "title": "Правка", "state": "merged", "updated_at": "2026-11-03T00:00:00Z",
		"references": map[string]string{"full": "g/a!7"}}
	srv, calls := glServer(t, map[string][][]map[string]any{
		"/api/v4/groups/top%2Fsub/merge_requests": {{mr}},
	}, 0)
	out, err := (&GitLab{}).Sync(context.Background(), glInput(srv, map[string]string{"group": "top/sub", "include_mrs": "true"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].ExternalID != "g/a!7" || !out.Items[0].Done || !strings.Contains(out.Items[0].Title, "MR: Правка") {
		t.Errorf("MR: %+v", out.Items)
	}
	if (*calls)[0].query.Get("scope") != "all" || (*calls)[0].path != "/api/v4/groups/top%2Fsub/issues" {
		t.Errorf("группа: %+v", (*calls)[0])
	}
	if (*calls)[1].query.Get("confidential") != "" {
		t.Error("у MR нет параметра confidential")
	}

	mine, mineCalls := glServer(t, nil, 0)
	if _, err := (&GitLab{}).Sync(context.Background(), glInput(mine, nil, nil)); err != nil {
		t.Fatal(err)
	}
	if c := (*mineCalls)[0]; c.path != "/api/v4/issues" || c.query.Get("scope") != "assigned_to_me" {
		t.Errorf("мои: %+v", c)
	}
}

func TestGitLabExplainsFailuresAndKeepsCursor(t *testing.T) {
	prev := map[string]string{"since": "2026-11-02T00:00:00Z"}
	for status, want := range map[int]string{401: "токен", 404: "не нашёл", 500: "500"} {
		srv, _ := glServer(t, nil, status)
		out, err := (&GitLab{}).Sync(context.Background(), glInput(srv, map[string]string{"project": "g/a"}, prev))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%d: %v", status, err)
		}
		if out.State["since"] != prev["since"] || len(out.Items) != 0 {
			t.Errorf("%d: состояние %v", status, out.State)
		}
		if err != nil && strings.Contains(err.Error(), "tok-1") {
			t.Errorf("токен в ошибке: %v", err)
		}
	}
	srv, _ := glServer(t, nil, 0)
	in := glInput(srv, map[string]string{"project": "g/a"}, nil)
	in.Secret = func(context.Context, string) ([]byte, error) { return nil, errors.New("нет секрета") }
	if _, err := (&GitLab{}).Sync(context.Background(), in); err == nil || !strings.Contains(err.Error(), "токен") {
		t.Errorf("секрет: %v", err)
	}
}

func TestGitLabValidate(t *testing.T) {
	base := map[string]string{"url": "https://g.example.com", "token": "env:T"}
	with := func(k, v string) map[string]string {
		m := map[string]string{k: v}
		for a, b := range base {
			if _, set := m[a]; !set {
				m[a] = b
			}
		}
		return m
	}
	if err := (&GitLab{}).Validate(Config{Values: base}); err != nil {
		t.Errorf("хорошая настройка: %v", err)
	}
	for _, bad := range []map[string]string{
		with("url", ""), with("url", "g.example.com"), with("token", ""), with("state", "open"),
		{"url": "https://g", "token": "env:T", "project": "a/b", "group": "a"},
	} {
		if err := (&GitLab{}).Validate(Config{Values: bad}); err == nil {
			t.Errorf("принято: %v", bad)
		}
	}
}

func TestGitLabTimeIsComparable(t *testing.T) {
	if got := glTime("2026-11-01T10:00:00.000+03:00"); got != "2026-11-01T07:00:00Z" {
		t.Errorf("время: %s", got)
	}
	if got := glTime("вчера"); got != "" {
		t.Errorf("мусор: %s", got)
	}
}
