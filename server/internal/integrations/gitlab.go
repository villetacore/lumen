package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GitLab turns issues and, optionally, merge requests into tasks. It reads a
// project, a whole group, or whatever is assigned to the token's owner. It only
// reads: nothing is ever written back to GitLab.
type GitLab struct{}

func (*GitLab) Kind() string { return "gitlab" }

func (*GitLab) Title() string { return "GitLab — задачи и merge requests" }

func (*GitLab) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Адрес GitLab", Type: FieldURL, Required: true,
			Placeholder: "https://gitlab.example.com"},
		{Key: "token", Label: "Токен", Type: FieldSecret, Required: true,
			Placeholder: "env:GITLAB_TOKEN",
			Help:        "Ссылка на секрет, а не сам токен. Достаточно прав read_api"},
		{Key: "project", Label: "Проект", Type: FieldText,
			Placeholder: "group/project",
			Help:        "Пусто вместе с группой — всё, что назначено владельцу токена"},
		{Key: "group", Label: "Группа", Type: FieldText,
			Placeholder: "group/subgroup", Help: "Вместо проекта: все проекты группы"},
		{Key: "state", Label: "Какие брать", Type: FieldText,
			Placeholder: "opened", Help: "opened, closed или all"},
		{Key: "assignee", Label: "Только назначенные на", Type: FieldText,
			Placeholder: "имя пользователя"},
		{Key: "labels", Label: "Только с метками", Type: FieldText,
			Placeholder: "bug,urgent"},
		{Key: "include_mrs", Label: "Включать merge requests", Type: FieldBool},
		{Key: "include_confidential", Label: "Включать конфиденциальные задачи", Type: FieldBool,
			Help: "Их текст попадёт в vault и станет виден всем, кому виден vault"},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для GitLab во внутренней сети"},
	}
}

func (*GitLab) Validate(c Config) error {
	if err := requireValue(c, "url", "адрес GitLab"); err != nil {
		return err
	}
	u, err := url.Parse(c.Value("url", ""))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("адрес указывается как https://gitlab.example.com")
	}
	if err := requireValue(c, "token", "ссылка на токен"); err != nil {
		return err
	}
	if c.Value("project", "") != "" && c.Value("group", "") != "" {
		return fmt.Errorf("укажите либо проект, либо группу")
	}
	switch c.Value("state", "opened") {
	case "opened", "closed", "all":
	default:
		return fmt.Errorf("state должен быть opened, closed или all")
	}
	return nil
}

// glItem covers both issues and merge requests.
type glItem struct {
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"`
	WebURL      string   `json:"web_url"`
	UpdatedAt   string   `json:"updated_at"`
	DueDate     string   `json:"due_date"`
	Labels      []string `json:"labels"`
	Draft       bool     `json:"draft"`
	Milestone   *struct {
		DueDate string `json:"due_date"`
	} `json:"milestone"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
}

// maxPages bounds one run: 100 × 100 items. Anything beyond arrives on later
// runs, because the cursor only advances to what was actually read.
const glMaxPages = 100

// glBase is where lists live for the configured scope. Paths are URL-encoded
// whole: GitLab takes "group%2Fproject" as one id.
func glBase(c Config) string {
	api := strings.TrimRight(c.Value("url", ""), "/") + "/api/v4"
	if p := c.Value("project", ""); p != "" {
		return api + "/projects/" + url.PathEscape(strings.Trim(p, "/"))
	}
	if g := c.Value("group", ""); g != "" {
		return api + "/groups/" + url.PathEscape(strings.Trim(g, "/"))
	}
	return api
}

func (gl *GitLab) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}
	token, err := in.Secret(ctx, in.Config.Value("token", ""))
	if err != nil {
		return out, fmt.Errorf("токен: %w", err)
	}
	// Bearer, not PRIVATE-TOKEN: the HTTP client drops Authorization on a
	// redirect to another host, and would forward a custom header.
	headers := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(string(token))}

	newest := in.State["since"]
	for _, kind := range []struct {
		path, mark string
		enabled    bool
	}{
		{"issues", "#", true},
		{"merge_requests", "!", in.Config.Bool("include_mrs")},
	} {
		if !kind.enabled {
			continue
		}
		seen, err := gl.fetch(ctx, in, headers, kind.path, kind.mark, &out)
		if err != nil {
			return SyncOutput{State: in.State}, err
		}
		if seen > newest {
			newest = seen
		}
	}
	if newest != "" {
		out.State["since"] = newest
	}
	return out, nil
}

func (gl *GitLab) fetch(ctx context.Context, in SyncInput, headers map[string]string, path, mark string, out *SyncOutput) (string, error) {
	cfg := in.Config
	q := url.Values{}
	q.Set("per_page", "100")
	q.Set("order_by", "updated_at")
	q.Set("sort", "asc")
	state := cfg.Value("state", "opened")
	since := in.State["since"]
	if since != "" {
		// After the first run also ask for closed ones: otherwise an issue closed
		// in GitLab would never be returned and would stay open here forever.
		q.Set("updated_after", since)
		if state == "opened" {
			state = "all"
		}
	}
	q.Set("state", state)
	if v := cfg.Value("assignee", ""); v != "" {
		q.Set("assignee_username", v)
	}
	if v := cfg.Value("labels", ""); v != "" {
		q.Set("labels", v)
	}
	if path == "issues" && !cfg.Bool("include_confidential") {
		q.Set("confidential", "false")
	}
	if cfg.Value("project", "") == "" {
		// The instance-wide list defaults to "created by me"; a group means everything.
		scope := "assigned_to_me"
		if cfg.Value("group", "") != "" || cfg.Value("assignee", "") != "" {
			scope = "all"
		}
		q.Set("scope", scope)
	}

	newest := ""
	for page := 1; page <= glMaxPages; page++ {
		q.Set("page", fmt.Sprint(page))
		endpoint := fmt.Sprintf("%s/%s?%s", glBase(cfg), path, q.Encode())
		body, resp, err := Fetch(ctx, in.HTTP, endpoint, headers)
		if err != nil {
			return "", glError(err, resp != nil, resp != nil && resp.StatusCode == 401, resp != nil && resp.StatusCode == 404)
		}
		var items []glItem
		if err := json.Unmarshal(body, &items); err != nil {
			return "", fmt.Errorf("не удалось разобрать ответ GitLab: %w", err)
		}
		for _, it := range items {
			out.Items = append(out.Items, glTask(it, mark))
			if ts := glTime(it.UpdatedAt); ts > newest {
				newest = ts
			}
		}
		if next := resp.Header.Get("X-Next-Page"); next == "" || len(items) == 0 {
			break
		}
	}
	return newest, nil
}

func glError(err error, answered, unauthorized, missing bool) error {
	switch {
	case unauthorized:
		return fmt.Errorf("GitLab отклонил токен: проверьте его срок и права read_api")
	case missing:
		return fmt.Errorf("GitLab не нашёл проект или группу, либо у токена нет к ним доступа")
	case answered:
		return err
	}
	return fmt.Errorf("GitLab недоступен: %w", err)
}

// glTime normalises to UTC so the cursor compares as a string, whichever zone
// the instance answers in.
func glTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func glTask(it glItem, mark string) ExternalItem {
	ref := it.References.Full
	if ref == "" {
		ref = fmt.Sprintf("%s%d", mark, it.IID)
	}
	title := it.Title
	if mark == "!" {
		title = "MR: " + title
	}
	item := ExternalItem{
		ExternalID: ref,
		Kind:       ItemTask,
		Title:      ref + " " + title,
		Body:       it.Description,
		URL:        it.WebURL,
		Done:       it.State == "closed" || it.State == "merged",
	}
	for _, l := range it.Labels {
		item.Labels = append(item.Labels, slugTag(l))
	}
	due := it.DueDate
	if due == "" && it.Milestone != nil {
		due = it.Milestone.DueDate
	}
	if due != "" {
		if d, err := time.ParseInLocation("2006-01-02", due, time.Local); err == nil {
			item.Due = d
		}
	}
	item.Hash = hashItem(item) + boolMark(item.Done)
	return item
}
