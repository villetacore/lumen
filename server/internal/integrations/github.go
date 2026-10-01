package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GitHub turns issues and pull requests into tasks. The same connector serves
// GitHub Enterprise, Gitea and Forgejo, because all of them expose the same
// REST shape — only api_url differs.
type GitHub struct{}

func (*GitHub) Kind() string { return "github" }

func (*GitHub) Title() string { return "GitHub / Gitea — задачи из issues" }

func (*GitHub) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "repo", Label: "Репозиторий", Type: FieldText, Required: true,
			Placeholder: "owner/name"},
		{Key: "token", Label: "Токен", Type: FieldSecret,
			Placeholder: "bitwarden:github-token",
			Help:        "Ссылка на секрет, а не сам токен: env:, file:, command:, bitwarden:, keepass:"},
		{Key: "api_url", Label: "Адрес API", Type: FieldURL,
			Placeholder: "https://api.github.com",
			Help:        "Для Gitea/Forgejo: https://git.example.com/api/v1"},
		{Key: "state", Label: "Какие issues брать", Type: FieldText,
			Placeholder: "open", Help: "open, closed или all"},
		{Key: "assignee", Label: "Только назначенные на", Type: FieldText,
			Placeholder: "имя пользователя"},
		{Key: "labels", Label: "Только с метками", Type: FieldText,
			Placeholder: "bug,urgent"},
		{Key: "include_pulls", Label: "Включать pull requests", Type: FieldBool},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для Gitea во внутренней сети"},
	}
}

func (*GitHub) Validate(c Config) error {
	if err := requireValue(c, "repo", "owner/name"); err != nil {
		return err
	}
	if parts := strings.Split(c.Value("repo", ""), "/"); len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("репозиторий указывается как owner/name")
	}
	switch c.Value("state", "open") {
	case "open", "closed", "all":
	default:
		return fmt.Errorf("state должен быть open, closed или all")
	}
	return nil
}

// ghIssue is the subset of the API response Lumen uses.
type ghIssue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	HTMLURL   string     `json:"html_url"`
	UpdatedAt time.Time  `json:"updated_at"`
	Labels    []ghLabel  `json:"labels"`
	Milestone *ghMilest  `json:"milestone"`
	PullReq   *ghPullRef `json:"pull_request"`
	User      *ghUser    `json:"user"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghMilest struct {
	Title string     `json:"title"`
	DueOn *time.Time `json:"due_on"`
}

type ghPullRef struct {
	URL string `json:"url"`
}

type ghUser struct {
	Login string `json:"login"`
}

func (gh *GitHub) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}

	apiURL := strings.TrimSuffix(in.Config.Value("api_url", "https://api.github.com"), "/")
	repo := in.Config.Value("repo", "")

	query := url.Values{}
	query.Set("state", in.Config.Value("state", "open"))
	query.Set("per_page", "100")
	query.Set("sort", "updated")
	query.Set("direction", "desc")
	if v := in.Config.Value("assignee", ""); v != "" {
		query.Set("assignee", v)
	}
	if v := in.Config.Value("labels", ""); v != "" {
		query.Set("labels", v)
	}
	// Ask only for what changed; the first run has no cursor and takes everything.
	if since := in.State["since"]; since != "" {
		query.Set("since", since)
	}

	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if ref := in.Config.Value("token", ""); ref != "" {
		token, err := in.Secret(ctx, ref)
		if err != nil {
			return out, fmt.Errorf("токен: %w", err)
		}
		headers["Authorization"] = "Bearer " + string(token)
	}

	endpoint := fmt.Sprintf("%s/repos/%s/issues?%s", apiURL, repo, query.Encode())
	body, _, err := Fetch(ctx, in.HTTP, endpoint, headers)
	if err != nil {
		return out, err
	}

	var issues []ghIssue
	if err := json.Unmarshal(body, &issues); err != nil {
		return out, fmt.Errorf("не удалось разобрать ответ %s: %w", apiURL, err)
	}

	includePulls := in.Config.Bool("include_pulls")
	newest := in.State["since"]

	for _, issue := range issues {
		if issue.PullReq != nil && !includePulls {
			continue
		}
		item := ExternalItem{
			ExternalID: fmt.Sprintf("%s#%d", repo, issue.Number),
			Kind:       ItemTask,
			Title:      fmt.Sprintf("%s#%d %s", repo, issue.Number, issue.Title),
			Body:       issue.Body,
			URL:        issue.HTMLURL,
			Done:       issue.State == "closed",
		}
		for _, l := range issue.Labels {
			item.Labels = append(item.Labels, slugTag(l.Name))
		}
		if issue.Milestone != nil && issue.Milestone.DueOn != nil {
			item.Due = issue.Milestone.DueOn.Local()
		}
		item.Hash = hashItem(item) + boolMark(item.Done)
		out.Items = append(out.Items, item)

		if issue.UpdatedAt.Format(time.RFC3339) > newest {
			newest = issue.UpdatedAt.Format(time.RFC3339)
		}
	}

	if newest != "" {
		out.State["since"] = newest
	}
	return out, nil
}

func boolMark(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// slugTag makes a GitHub label usable as a Lumen tag: no spaces, no '#'.
func slugTag(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	repl := strings.NewReplacer(" ", "-", "#", "", ",", "", ":", "-", "/", "-")
	return strings.Trim(repl.Replace(s), "-")
}
