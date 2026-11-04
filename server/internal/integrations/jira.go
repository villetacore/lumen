package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Jira imports issues into tasks. Cloud and Data Center differ in the search
// endpoint, its paging and the description format; the address tells which it is.
type Jira struct{}

func (*Jira) Kind() string { return "jira" }

func (*Jira) Title() string { return "Jira" }

func (*Jira) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Адрес Jira", Type: FieldURL, Required: true,
			Placeholder: "https://company.atlassian.net"},
		{Key: "email", Label: "Почта", Type: FieldText,
			Placeholder: "you@example.com", Help: "Для Jira Cloud. На своём сервере оставьте пустым"},
		{Key: "token", Label: "API-токен", Type: FieldSecret, Required: true, Help: secretHelp},
		{Key: "jql", Label: "Какие задачи брать (JQL)", Type: FieldText,
			Placeholder: "assignee = currentUser()", Help: "Пусто — назначенные вам"},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для Jira во внутренней сети"},
	}
}

func (*Jira) Validate(c Config) error {
	u, err := url.Parse(c.Value("url", ""))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("адрес указывается как https://company.atlassian.net")
	}
	return requireValue(c, "token", "API-токен")
}

type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		DueDate     string          `json:"duedate"`
		Updated     string          `json:"updated"`
		Labels      []string        `json:"labels"`
		Status      struct {
			Category struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
	} `json:"fields"`
}

type jiraPage struct {
	Issues        []jiraIssue `json:"issues"`
	Total         int         `json:"total"`
	NextPageToken string      `json:"nextPageToken"`
}

const jiraPageSize, jiraMaxPages = 100, 100

func (j *Jira) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}
	cfg := in.Config
	token, err := in.Secret(ctx, cfg.Value("token", ""))
	if err != nil {
		return out, fmt.Errorf("токен: %w", err)
	}
	headers := map[string]string{"Accept": "application/json"}
	secret := strings.TrimSpace(string(token))
	if email := cfg.Value("email", ""); email != "" {
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+secret))
	} else {
		headers["Authorization"] = "Bearer " + secret
	}

	jql := cfg.Value("jql", "assignee = currentUser()")
	since := in.State["since"]
	if since != "" {
		// Minute precision on purpose: a repeat is skipped by the item hash.
		jql = "(" + jql + `) AND updated >= "` + since + `"`
	}
	jql += " ORDER BY updated ASC"

	base := strings.TrimRight(cfg.Value("url", ""), "/")
	cloud := strings.HasSuffix(mustHost(base), ".atlassian.net")
	newest := since
	startAt, pageToken := 0, ""
	for page := 0; page < jiraMaxPages; page++ {
		q := url.Values{"jql": {jql}, "maxResults": {fmt.Sprint(jiraPageSize)}, "fields": {"summary,description,duedate,updated,labels,status"}}
		endpoint := base + "/rest/api/2/search"
		if cloud {
			endpoint = base + "/rest/api/3/search/jql"
			if pageToken != "" {
				q.Set("nextPageToken", pageToken)
			}
		} else {
			q.Set("startAt", fmt.Sprint(startAt))
		}
		body, resp, err := Fetch(ctx, in.HTTP, endpoint+"?"+q.Encode(), headers)
		if err != nil {
			if resp != nil && resp.StatusCode == 401 {
				return SyncOutput{State: in.State}, fmt.Errorf("Jira отклонила токен: для Cloud нужна и почта")
			}
			return SyncOutput{State: in.State}, err
		}
		var res jiraPage
		if err := json.Unmarshal(body, &res); err != nil {
			return SyncOutput{State: in.State}, fmt.Errorf("не удалось разобрать ответ Jira: %w", err)
		}
		for _, is := range res.Issues {
			out.Items = append(out.Items, jiraTask(base, is))
			if ts := jiraCursor(is.Fields.Updated); ts > newest {
				newest = ts
			}
		}
		startAt += len(res.Issues)
		pageToken = res.NextPageToken
		if len(res.Issues) == 0 || (cloud && pageToken == "") || (!cloud && startAt >= res.Total) {
			break
		}
	}
	if newest != "" {
		out.State["since"] = newest
	}
	return out, nil
}

func mustHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// jiraCursor formats a time the way JQL takes it.
func jiraCursor(s string) string {
	t := parseAnyDate(s)
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func jiraTask(base string, is jiraIssue) ExternalItem {
	item := ExternalItem{
		ExternalID: is.Key,
		Kind:       ItemTask,
		Title:      is.Key + " " + is.Fields.Summary,
		Body:       jiraDescription(is.Fields.Description),
		URL:        base + "/browse/" + is.Key,
		Done:       is.Fields.Status.Category.Key == "done",
	}
	for _, l := range is.Fields.Labels {
		item.Labels = append(item.Labels, slugTag(l))
	}
	if is.Fields.DueDate != "" {
		item.Due = parseAnyDate(is.Fields.DueDate)
	}
	item.Hash = hashItem(item) + boolMark(item.Done)
	return item
}

// jiraDescription takes plain text (Data Center) or a document tree (Cloud),
// keeping the text and paragraph breaks.
func jiraDescription(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		node, ok := v.(map[string]any)
		if !ok {
			return
		}
		if t, ok := node["text"].(string); ok {
			b.WriteString(t)
		}
		if list, ok := node["content"].([]any); ok {
			for _, child := range list {
				walk(child)
			}
		}
		switch node["type"] {
		case "paragraph", "heading", "listItem", "codeBlock":
			b.WriteString("\n\n")
		}
	}
	walk(doc)
	return strings.TrimSpace(b.String())
}
