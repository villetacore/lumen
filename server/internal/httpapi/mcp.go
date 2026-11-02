package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/history"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/policy"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP server (ADR-0013) lives at /mcp. Tools call the same services and
// the same policy-scoped index as the HTTP API, with the principal of the
// very HTTP request that carries the call.

const mcpInstructions = `Lumen — трекер задач и база знаний студии поверх Markdown.
Инструменты видят ровно то, что видит вошедший пользователь: закрытые проекты
других людей недоступны. Ключи тикетов выглядят как WEB-12, спринтов — WEB-S3.
Изменения через MCP не применяются сразу: инструменты записи создают
предложение, которое человек подтверждает в веб-интерфейсе Lumen.`

// maxText bounds note and description bodies returned to a model.
const maxText = 60_000

func (a *API) mcpHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "lumen", Title: "Lumen", Version: "1"},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	a.addMCPReadTools(server)
	a.addMCPWriteTools(server)

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, Logger: a.log,
			// MCP clients are not browsers; a browser page from another site
			// (Sec-Fetch-Site / Origin) must not reach the tools.
			CrossOriginProtection: http.NewCrossOriginProtection()})
	// The principal travels to tool handlers as TokenInfo, which the SDK
	// hands to every call of the request that carried it.
	verify := mcpauth.RequireBearerToken(func(ctx context.Context, _ string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		p := policy.From(ctx)
		if p == nil {
			return nil, mcpauth.ErrInvalidToken
		}
		return &mcpauth.TokenInfo{UserID: p.ID, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"principal": p}}, nil
	}, nil)(streamable)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// withAuth has identified the caller. A browser session cookie is not
		// enough here: only an Authorization header proves the request comes
		// from an MCP client and not from another site's page (ADR-0013 §3).
		auth := a.oidc != nil || a.cfg.Token != ""
		if auth && r.Header.Get("Authorization") == "" || policy.From(r.Context()) == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lumen"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "MCP требует заголовок Authorization: Bearer", nil)
			return
		}
		if !auth {
			// Local mode has no token; the SDK middleware still wants one.
			r.Header.Set("Authorization", "Bearer local")
		}
		verify.ServeHTTP(w, r)
	})
}

// mcpContext restores the request's principal inside a tool handler.
func mcpContext(ctx context.Context, req *mcp.CallToolRequest) (context.Context, *policy.Principal, error) {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return nil, nil, errors.New("нет данных о пользователе")
	}
	p, _ := req.Extra.TokenInfo.Extra["principal"].(*policy.Principal)
	if p == nil {
		return nil, nil, errors.New("нет данных о пользователе")
	}
	return policy.With(ctx, p), p, nil
}

// toolError turns service errors into messages a model can act on, without
// telling hidden from missing.
func toolError(err error) error {
	switch {
	case errors.Is(err, index.ErrNotFound):
		return errors.New("не найдено или нет доступа")
	case errors.Is(err, policy.ErrForbidden):
		return errors.New("недостаточно прав")
	}
	return err
}

func clip(s string) (string, bool) {
	if len(s) <= maxText {
		return s, false
	}
	cut := maxText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}

type (
	noArgs struct{}
	whoami struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Role     string `json:"role" jsonschema:"роль в пространстве: viewer, member или admin"`
	}
	searchIn struct {
		Query string `json:"query" jsonschema:"что искать в названиях и тексте"`
		Limit int    `json:"limit,omitempty" jsonschema:"сколько результатов, по умолчанию 20"`
	}
	searchOut struct {
		Hits []domain.SearchHit `json:"hits"`
	}
	noteIn struct {
		Path string `json:"path" jsonschema:"путь в vault (20-projects/web.md) или id заметки"`
	}
	noteOut struct {
		Path      string   `json:"path"`
		Title     string   `json:"title"`
		Type      string   `json:"type"`
		Tags      []string `json:"tags"`
		Body      string   `json:"body"`
		Truncated bool     `json:"truncated,omitempty"`
	}
	projectOut struct {
		Key      string   `json:"key"`
		Title    string   `json:"title"`
		Statuses []string `json:"statuses" jsonschema:"колонки workflow по порядку, id: название"`
		Types    []string `json:"issue_types"`
	}
	projectsOut struct {
		Projects []projectOut `json:"projects"`
	}
	issuesIn struct {
		Project  string `json:"project,omitempty" jsonschema:"ключ проекта, например WEB"`
		Status   string `json:"status,omitempty" jsonschema:"id статуса"`
		Category string `json:"category,omitempty" jsonschema:"todo, in_progress или done"`
		Assignee string `json:"assignee,omitempty" jsonschema:"логин исполнителя; me — вы; none — без исполнителя"`
		Watcher  string `json:"watcher,omitempty" jsonschema:"логин автора, исполнителя или наблюдателя; me — вы"`
		Sprint   string `json:"sprint,omitempty" jsonschema:"id спринта, например WEB-S3"`
		Text     string `json:"text,omitempty" jsonschema:"подстрока названия или ключ"`
		Limit    int    `json:"limit,omitempty" jsonschema:"по умолчанию 50, не больше 200"`
	}
	issueSummary struct {
		Key      string   `json:"key"`
		Title    string   `json:"title"`
		Type     string   `json:"type"`
		Status   string   `json:"status"`
		Category string   `json:"category,omitempty"`
		Priority string   `json:"priority,omitempty"`
		Assignee string   `json:"assignee,omitempty"`
		Sprint   string   `json:"sprint,omitempty"`
		Due      string   `json:"due,omitempty"`
		Labels   []string `json:"labels,omitempty"`
	}
	issuesOut struct {
		Total  int            `json:"total"`
		Issues []issueSummary `json:"issues"`
	}
	keyIn struct {
		Key string `json:"key" jsonschema:"ключ тикета, например WEB-12"`
	}
	commentOut struct {
		ID     string `json:"id"`
		Author string `json:"author,omitempty"`
		At     string `json:"at"`
		Body   string `json:"body"`
	}
	issueOut struct {
		issueSummary
		Project     string       `json:"project"`
		Reporter    string       `json:"reporter,omitempty"`
		Watchers    []string     `json:"watchers,omitempty"`
		Epic        string       `json:"epic,omitempty"`
		Start       string       `json:"start,omitempty"`
		Estimate    string       `json:"estimate,omitempty"`
		Points      float64      `json:"points,omitempty"`
		Description string       `json:"description"`
		Truncated   bool         `json:"truncated,omitempty"`
		Checklist   []string     `json:"checklist,omitempty" jsonschema:"пункты чек-листа: [x] или [ ] и текст"`
		Comments    []commentOut `json:"comments,omitempty"`
		Path        string       `json:"path"`
	}
	historyOut struct {
		Events []history.Event `json:"events"`
	}
	projectIn struct {
		Project string `json:"project" jsonschema:"ключ проекта"`
	}
	sprintOut struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		State     string `json:"state" jsonschema:"planned, active или closed"`
		Goal      string `json:"goal,omitempty"`
		Start     string `json:"start,omitempty"`
		End       string `json:"end,omitempty"`
		Completed string `json:"completed,omitempty"`
	}
	sprintsOut struct {
		Sprints []sprintOut `json:"sprints"`
	}
)

func summarizeIssue(is *domain.Issue) issueSummary {
	s := issueSummary{Key: is.Key, Title: is.Title, Type: is.Type, Status: is.Status, Category: string(is.StatusCategory),
		Priority: is.Priority, Assignee: is.Assignee, Sprint: is.Sprint, Labels: is.Labels}
	if !is.Due.IsZero() {
		s.Due = is.Due.String()
	}
	return s
}

func (a *API) addMCPReadTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Кто вы в Lumen и какая у вас роль.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, whoami, error) {
			_, p, err := mcpContext(ctx, req)
			if err != nil {
				return nil, whoami{}, err
			}
			return nil, whoami{Username: p.Username, Name: p.Name, Role: p.Role.String()}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "search", Description: "Поиск по заметкам, документам и тикетам доступных вам проектов.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, searchOut{}, err
			}
			limit := in.Limit
			if limit <= 0 || limit > 100 {
				limit = 20
			}
			hits, err := a.store.Search(ctx, in.Query, limit)
			if err != nil {
				return nil, searchOut{}, toolError(err)
			}
			if hits == nil {
				hits = []domain.SearchHit{}
			}
			return nil, searchOut{Hits: hits}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_note", Description: "Заметка, документ или страница целиком (Markdown).", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in noteIn) (*mcp.CallToolResult, noteOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, noteOut{}, err
			}
			n, err := a.notes.Get(ctx, strings.TrimPrefix(in.Path, "/"))
			if err != nil {
				return nil, noteOut{}, toolError(err)
			}
			body, cut := clip(n.Body)
			tags := n.Tags
			if tags == nil {
				tags = []string{}
			}
			return nil, noteOut{Path: n.Path, Title: n.Title, Type: string(n.Type), Tags: tags, Body: body, Truncated: cut}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "Проекты трекера, которые вам видны, с колонками workflow.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, projectsOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, projectsOut{}, err
			}
			list, err := a.issues.Projects(ctx)
			if err != nil {
				return nil, projectsOut{}, toolError(err)
			}
			out := projectsOut{Projects: []projectOut{}}
			for _, p := range list {
				po := projectOut{Key: p.Key, Title: p.Title, Types: p.IssueTypes}
				for _, st := range p.Workflow {
					po.Statuses = append(po.Statuses, st.ID+": "+st.Name)
				}
				out.Projects = append(out.Projects, po)
			}
			return nil, out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_issues", Description: "Тикеты с фильтрами в порядке доски. assignee=me — назначенные вам.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in issuesIn) (*mcp.CallToolResult, issuesOut, error) {
			ctx, p, err := mcpContext(ctx, req)
			if err != nil {
				return nil, issuesOut{}, err
			}
			me := func(v string) string {
				if v == "me" {
					return p.Username
				}
				return v
			}
			limit := in.Limit
			if limit <= 0 || limit > 200 {
				limit = 50
			}
			q := domain.IssueQuery{Project: in.Project, Category: domain.StatusCategory(in.Category),
				Assignee: me(in.Assignee), Watcher: me(in.Watcher), Sprint: in.Sprint, Text: in.Text, Limit: limit}
			if in.Status != "" {
				q.Status = []string{in.Status}
			}
			list, total, err := a.issues.List(ctx, q)
			if err != nil {
				return nil, issuesOut{}, toolError(err)
			}
			out := issuesOut{Total: total, Issues: []issueSummary{}}
			for _, is := range list {
				out.Issues = append(out.Issues, summarizeIssue(is))
			}
			return nil, out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "get_issue", Description: "Тикет целиком: поля, описание, чек-лист и комментарии.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in keyIn) (*mcp.CallToolResult, issueOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, issueOut{}, err
			}
			is, err := a.issues.Get(ctx, in.Key)
			if err != nil {
				return nil, issueOut{}, toolError(err)
			}
			desc, cut := clip(is.Description)
			out := issueOut{issueSummary: summarizeIssue(is), Project: is.Project, Reporter: is.Reporter, Watchers: is.Watchers,
				Epic: is.Epic, Estimate: is.Estimate, Points: is.Points, Description: desc, Truncated: cut, Path: is.Path}
			if !is.Start.IsZero() {
				out.Start = is.Start.String()
			}
			for _, t := range is.Checklist {
				mark := "[ ]"
				if t.Status == domain.StatusDone {
					mark = "[x]"
				}
				out.Checklist = append(out.Checklist, mark+" "+t.Text)
			}
			for _, c := range is.Comments {
				out.Comments = append(out.Comments, commentOut{ID: c.ID, Author: c.Author, At: c.At.Format(time.RFC3339), Body: c.Body})
			}
			return nil, out, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "issue_history", Description: "Кто и когда менял поля тикета, включая правки файла вне Lumen.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in keyIn) (*mcp.CallToolResult, historyOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, historyOut{}, err
			}
			is, err := a.issues.Get(ctx, in.Key)
			if err != nil {
				return nil, historyOut{}, toolError(err)
			}
			events, err := a.history.Events(ctx, is.Key)
			if err != nil {
				return nil, historyOut{}, err
			}
			return nil, historyOut{Events: events}, nil
		})

	mcp.AddTool(s, &mcp.Tool{Name: "list_sprints", Description: "Спринты проекта: активный, запланированные, закрытые.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, sprintsOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, sprintsOut{}, err
			}
			if _, err := a.issues.Project(ctx, in.Project); err != nil {
				return nil, sprintsOut{}, toolError(err)
			}
			list, err := a.issues.Sprints(ctx, in.Project)
			if err != nil {
				return nil, sprintsOut{}, toolError(err)
			}
			out := sprintsOut{Sprints: []sprintOut{}}
			date := func(d domain.Date) string {
				if d.IsZero() {
					return ""
				}
				return d.String()
			}
			for _, sp := range list {
				out.Sprints = append(out.Sprints, sprintOut{ID: sp.ID, Name: sp.Name, State: string(sp.State), Goal: sp.Goal,
					Start: date(sp.Start), End: date(sp.End), Completed: date(sp.Completed)})
			}
			return nil, out, nil
		})
}
