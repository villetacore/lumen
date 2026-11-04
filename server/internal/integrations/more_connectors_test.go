package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func input(srv *httptest.Server, kind string, values map[string]string, state map[string]string) SyncInput {
	return SyncInput{
		Config: Config{ID: "x", Kind: kind, Values: values}, State: state, HTTP: srv.Client(),
		Secret: func(context.Context, string) ([]byte, error) { return []byte("tok-1\n"), nil },
	}
}

func TestRESTMapsAnyJSONAndFollowsPages(t *testing.T) {
	var headers []string
	var srv *httptest.Server
	srv = serve(t, func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("X-Api-Key"))
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/list?page=2>; rel="next"`, srv.URL))
			fmt.Fprint(w, `{"data":{"rows":[{"key":17,"meta":{"name":"Первая"},"text":"Описание","link":"https://x/17","state":"open","deadline":"2026-11-20","tags":["Bug Fix"]},{"key":0},{"meta":{"name":"без id"}}]}}`)
			return
		}
		// A next link to another host must not get the token.
		w.Header().Set("Link", `<https://evil.example/list>; rel="next"`)
		fmt.Fprint(w, `{"data":{"rows":[{"key":"a-2","meta":{"name":"Вторая"},"state":"Closed"}]}}`)
	})
	in := input(srv, "rest", map[string]string{
		"url": srv.URL + "/list", "token": "env:T", "auth_header": "X-Api-Key", "items_path": "data.rows",
		"id_field": "key", "title_field": "meta.name", "body_field": "text", "url_field": "link", "due_field": "deadline",
		"labels_field": "tags", "done_field": "state", "done_value": "closed",
	}, nil)
	out, err := (&REST{}).Sync(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("элементы: %+v", out.Items)
	}
	first, second := out.Items[0], out.Items[1]
	if first.ExternalID != "17" || first.Title != "Первая" || first.Body != "Описание" || first.URL != "https://x/17" || first.Done ||
		first.Due.Format("2006-01-02") != "2026-11-20" || first.Labels[0] != "bug-fix" || first.Kind != ItemTask {
		t.Errorf("первая: %+v", first)
	}
	if second.ExternalID != "a-2" || !second.Done {
		t.Errorf("вторая: %+v", second)
	}
	if len(headers) != 2 || headers[0] != "tok-1" {
		t.Errorf("запросы: %v", headers)
	}
}

func TestRESTExplainsWrongPath(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"data":{}}`) })
	_, err := (&REST{}).Sync(context.Background(), input(srv, "rest", map[string]string{"url": srv.URL, "items_path": "data.rows", "id_field": "id", "title_field": "t"}, nil))
	if err == nil || !strings.Contains(err.Error(), "data.rows") {
		t.Errorf("ошибка: %v", err)
	}
	for _, bad := range []map[string]string{{"url": "nope", "id_field": "i", "title_field": "t"}, {"url": "https://x"}, {"url": "https://x", "id_field": "i"}} {
		if err := (&REST{}).Validate(Config{Values: bad}); err == nil {
			t.Errorf("принято: %v", bad)
		}
	}
}

func TestFeedReadsRSSAndAtom(t *testing.T) {
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>Лента</title>
	  <item><title>Релиз 1.2</title><link>https://x/1</link><guid>g1</guid><description>Что нового</description><pubDate>Mon, 02 Nov 2026 10:00:00 +0300</pubDate></item>
	  <item><title>Без id</title><link>https://x/2</link></item>
	  <item><description>без названия</description></item></channel></rss>`
	atom := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>A</title>
	  <entry><id>tag:1</id><title>Запись</title><link rel="self" href="https://x/self"/><link rel="alternate" href="https://x/a"/><summary>Кратко</summary><updated>2026-11-02T10:00:00Z</updated></entry></feed>`
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/atom" {
			fmt.Fprint(w, atom)
		} else if r.URL.Path == "/html" {
			fmt.Fprint(w, "<html><body><p>не лента")
		} else {
			fmt.Fprint(w, rss)
		}
	})
	out, err := (&Feed{}).Sync(context.Background(), input(srv, "feed", map[string]string{"url": srv.URL + "/rss"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || out.Items[0].ExternalID != "g1" || out.Items[0].Kind != ItemNote || out.Items[0].Body != "Что нового" || out.Items[0].URL != "https://x/1" {
		t.Errorf("RSS: %+v", out.Items)
	}
	if out.Items[1].ExternalID != "https://x/2" {
		t.Errorf("id из ссылки: %+v", out.Items[1])
	}
	out, err = (&Feed{}).Sync(context.Background(), input(srv, "feed", map[string]string{"url": srv.URL + "/atom", "as_tasks": "true"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].URL != "https://x/a" || out.Items[0].Kind != ItemTask || out.Items[0].Body != "Кратко" {
		t.Errorf("Atom: %+v", out.Items)
	}
	if out, err := (&Feed{}).Sync(context.Background(), input(srv, "feed", map[string]string{"url": srv.URL + "/html"}, nil)); err == nil && len(out.Items) > 0 {
		t.Errorf("страница принята за ленту: %+v", out.Items)
	}
}

func TestJiraServerPagesAndCursor(t *testing.T) {
	var jqls, auth []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/search" {
			t.Errorf("путь: %s", r.URL.Path)
		}
		jqls = append(jqls, r.URL.Query().Get("jql"))
		auth = append(auth, r.Header.Get("Authorization"))
		page := map[string]any{"total": 2, "issues": []map[string]any{{"key": "WEB-1", "fields": map[string]any{
			"summary": "Первая", "description": "Текст", "duedate": "2026-11-20", "updated": "2026-11-01T10:00:00.000+0000",
			"labels": []string{"ui"}, "status": map[string]any{"statusCategory": map[string]any{"key": "indeterminate"}},
		}}}}
		if r.URL.Query().Get("startAt") == "1" {
			page["issues"] = []map[string]any{{"key": "WEB-2", "fields": map[string]any{
				"summary": "Вторая", "updated": "2026-11-02T10:00:00.000+0000",
				"status": map[string]any{"statusCategory": map[string]any{"key": "done"}},
			}}}
		}
		json.NewEncoder(w).Encode(page)
	})
	out, err := (&Jira{}).Sync(context.Background(), input(srv, "jira", map[string]string{"url": srv.URL, "token": "env:T"}, map[string]string{"since": "2026-10-01 00:00"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || out.Items[0].Title != "WEB-1 Первая" || out.Items[0].Done || !out.Items[1].Done || out.Items[0].URL != srv.URL+"/browse/WEB-1" || out.Items[0].Body != "Текст" {
		t.Fatalf("элементы: %+v", out.Items)
	}
	if auth[0] != "Bearer tok-1" {
		t.Errorf("вход: %s", auth[0])
	}
	if !strings.Contains(jqls[0], `updated >= "2026-10-01 00:00"`) || !strings.HasSuffix(jqls[0], "ORDER BY updated ASC") {
		t.Errorf("jql: %s", jqls[0])
	}
	if out.State["since"] == "" || out.State["since"] <= "2026-10-01 00:00" {
		t.Errorf("cursor: %v", out.State)
	}
}

func TestJiraCloudDocumentDescription(t *testing.T) {
	got := jiraDescription(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Раз"}]},{"type":"paragraph","content":[{"type":"text","text":"Два"}]}]}`))
	if got != "Раз\n\nДва" {
		t.Errorf("%q", got)
	}
	if jiraDescription(nil) != "" || jiraDescription(json.RawMessage(`null`)) != "" {
		t.Error("пустое описание")
	}
}

// A closed issue must close here too, though the config asks for open ones.
func TestGitHubIncrementalRunAsksForClosedToo(t *testing.T) {
	var states []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		states = append(states, r.URL.Query().Get("state"))
		fmt.Fprint(w, `[{"number":1,"title":"x","state":"closed","updated_at":"2026-11-03T00:00:00Z"}]`)
	})
	values := map[string]string{"repo": "a/b", "api_url": srv.URL}
	if _, err := (&GitHub{}).Sync(context.Background(), input(srv, "github", values, nil)); err != nil {
		t.Fatal(err)
	}
	out, err := (&GitHub{}).Sync(context.Background(), input(srv, "github", values, map[string]string{"since": "2026-11-01T00:00:00Z"}))
	if err != nil {
		t.Fatal(err)
	}
	if states[0] != "open" || states[1] != "all" || !out.Items[0].Done {
		t.Errorf("запросы %v, %+v", states, out.Items)
	}
}
