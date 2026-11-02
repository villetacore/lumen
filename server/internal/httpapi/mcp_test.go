package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearer adds an Authorization header to every request of the MCP client.
type withBearer struct {
	token string
	next  http.RoundTripper
}

func (b withBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func (f *oidcFixture) mcpSession(t *testing.T, user string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "lumen-test", Version: "1"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:   f.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: withBearer{token: f.tokens[user], next: f.srv.Client().Transport}},
		MaxRetries: -1, DisableStandaloneSSE: true,
	}
	cs, err := client.Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatalf("%s connect: %v", user, err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// callJSON calls a tool and returns its structured result as JSON text, or
// the error text when the tool failed.
func callJSON(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		var msg []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg = append(msg, tc.Text)
			}
		}
		return strings.Join(msg, " "), true
	}
	b, _ := json.Marshal(res.StructuredContent)
	return string(b), false
}

func TestMCPReadToolsFollowAccessPolicy(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	f.expect("boss", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, 200)
	f.expect("alice", "PATCH", "/api/v1/issues/WEB-1", map[string]any{"assignee": "bob"}, 200)

	bob := f.mcpSession(t, "bob")
	alice := f.mcpSession(t, "alice")
	if v := bob.InitializeResult().ProtocolVersion; v == "" {
		t.Fatal("no protocol version negotiated")
	}

	tools, err := bob.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		propose := strings.HasPrefix(tool.Name, "propose_")
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint == propose {
			t.Errorf("%s: read-only hint must be %v", tool.Name, !propose)
		}
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "get_issue,get_note,get_proposal,issue_history,list_issues,list_projects,list_sprints,"+
		"propose_change_status,propose_comment,propose_create_issue,propose_update_issue,search,whoami" {
		t.Fatalf("tools = %s", got)
	}

	if out, _ := callJSON(t, bob, "whoami", nil); !strings.Contains(out, `"username":"bob"`) || !strings.Contains(out, `"role":"member"`) {
		t.Fatalf("whoami = %s", out)
	}
	out, _ := callJSON(t, bob, "list_issues", map[string]any{"assignee": "me"})
	if !strings.Contains(out, `"key":"WEB-1"`) || !strings.Contains(out, `"total":1`) {
		t.Fatalf("bob's issues = %s", out)
	}

	// bob sees nothing of SEC; alice does.
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"get_issue", map[string]any{"key": "SEC-1"}},
		{"issue_history", map[string]any{"key": "SEC-1"}},
		{"get_note", map[string]any{"path": "60-documents/sec-doc.md"}},
		{"list_sprints", map[string]any{"project": "SEC"}},
	} {
		if msg, isErr := callJSON(t, bob, c.tool, c.args); !isErr || !strings.Contains(msg, "не найдено или нет доступа") {
			t.Errorf("bob %s: %v %s", c.tool, isErr, msg)
		}
		if msg, isErr := callJSON(t, alice, c.tool, c.args); isErr {
			t.Errorf("alice %s: %s", c.tool, msg)
		}
	}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"search", map[string]any{"query": "секрет"}},
		{"list_projects", nil},
		{"list_issues", map[string]any{"project": "SEC"}},
		{"list_issues", nil},
	} {
		out, isErr := callJSON(t, bob, c.tool, c.args)
		if isErr || strings.Contains(out, "SEC") || strings.Contains(out, "секрет") {
			t.Errorf("bob %s leaks: %s", c.tool, out)
		}
	}
	if out, _ := callJSON(t, alice, "get_issue", map[string]any{"key": "SEC-1"}); !strings.Contains(out, "Код запуска") || !strings.Contains(out, "[ ] секретная задача") {
		t.Fatalf("alice SEC-1 = %s", out)
	}
}

func TestMCPRequiresBearerHeader(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`
	req, _ := http.NewRequest("POST", f.srv.URL+"/mcp", strings.NewReader(init))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("anonymous MCP: %d", res.StatusCode)
	}
	// A page of another site cannot use the tools, even with a valid token.
	req, _ = http.NewRequest("POST", f.srv.URL+"/mcp", strings.NewReader(init))
	req.Header.Set("Authorization", "Bearer "+f.tokens["bob"])
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if res, err = f.srv.Client().Do(req); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site request: %d", res.StatusCode)
	}
	// A forged token is refused too.
	req, _ = http.NewRequest("POST", f.srv.URL+"/mcp", strings.NewReader(init))
	req.Header.Set("Authorization", "Bearer not.a.token")
	req.Header.Set("Content-Type", "application/json")
	if res, err = f.srv.Client().Do(req); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged token: %d", res.StatusCode)
	}
}

func TestMCPWritesNeedHumanConfirmation(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	f.expect("boss", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, 200)
	alice := f.mcpSession(t, "alice")
	bob := f.mcpSession(t, "bob")
	vic := f.mcpSession(t, "vic")
	id := func(out string) string {
		var p struct {
			ID string `json:"proposal_id"`
		}
		json.Unmarshal([]byte(out), &p)
		return p.ID
	}

	// alice's agent proposes a comment and a status change.
	out, isErr := callJSON(t, alice, "propose_comment", map[string]any{"key": "WEB-1", "body": "Готово @bob", "request_id": "r-1"})
	if isErr || !strings.Contains(out, `"state":"pending"`) || !strings.Contains(out, "Готово @bob") || !strings.Contains(out, "/approvals") {
		t.Fatalf("propose_comment = %s", out)
	}
	comment := id(out)
	if again, _ := callJSON(t, alice, "propose_comment", map[string]any{"key": "WEB-1", "body": "Готово @bob", "request_id": "r-1"}); id(again) != comment {
		t.Fatalf("retry made another proposal: %s", again)
	}
	out, _ = callJSON(t, alice, "propose_change_status", map[string]any{"key": "WEB-1", "status": "in-progress"})
	status := id(out)
	if !strings.Contains(out, "Статус: К работе → В работе") {
		t.Fatalf("status preview = %s", out)
	}

	// Nothing has changed yet.
	var is domain.Issue
	json.Unmarshal(f.expect("alice", "GET", "/api/v1/issues/WEB-1", nil, 200), &is)
	if len(is.Comments) != 0 || is.Status != "todo" {
		t.Fatalf("applied without confirmation: %+v", is)
	}

	// The model cannot confirm: there is no such tool, and bob cannot either.
	f.expect("bob", "POST", "/api/v1/proposals/"+comment+"/approve", nil, 404)
	if out, isErr := callJSON(t, bob, "get_proposal", map[string]any{"proposal_id": comment}); !isErr {
		t.Fatalf("bob reads alice's proposal: %s", out)
	}

	// alice confirms in Lumen; a second click does nothing.
	var applied struct{ State, Result string }
	json.Unmarshal(f.expect("alice", "POST", "/api/v1/proposals/"+comment+"/approve", nil, 200), &applied)
	if applied.State != "applied" || applied.Result != "WEB-1" {
		t.Fatalf("approve = %+v", applied)
	}
	f.expect("alice", "POST", "/api/v1/proposals/"+comment+"/approve", nil, 409)
	json.Unmarshal(f.expect("alice", "GET", "/api/v1/issues/WEB-1", nil, 200), &is)
	if len(is.Comments) != 1 || is.Comments[0].Author != "alice" {
		t.Fatalf("comment = %+v", is.Comments)
	}
	if out, _ := callJSON(t, alice, "get_proposal", map[string]any{"proposal_id": comment}); !strings.Contains(out, `"state":"applied"`) {
		t.Fatalf("get_proposal = %s", out)
	}

	// Someone moves the issue first: the status proposal is stale, not applied.
	f.expect("bob", "POST", "/api/v1/issues/WEB-1/move", map[string]any{"status": "in-progress"}, 200)
	f.expect("bob", "POST", "/api/v1/issues/WEB-1/move", map[string]any{"status": "done"}, 200)
	json.Unmarshal(f.expect("alice", "POST", "/api/v1/proposals/"+status+"/approve", nil, 200), &applied)
	if applied.State != "stale" {
		t.Fatalf("status proposal = %+v", applied)
	}

	// An update proposal goes stale when the issue changes after the preview.
	out, _ = callJSON(t, alice, "propose_update_issue", map[string]any{"key": "WEB-1", "priority": "high", "assignee": "alice"})
	upd := id(out)
	if !strings.Contains(out, "Приоритет: — → high") || !strings.Contains(out, "Исполнитель: — → alice") {
		t.Fatalf("update preview = %s", out)
	}
	f.expect("bob", "PATCH", "/api/v1/issues/WEB-1", map[string]any{"title": "Изменено Бобом"}, 200)
	json.Unmarshal(f.expect("alice", "POST", "/api/v1/proposals/"+upd+"/approve", nil, 200), &applied)
	if applied.State != "stale" {
		t.Fatalf("update after foreign edit = %+v", applied)
	}

	// Creating an issue: proposed, rejected, proposed again and applied.
	out, _ = callJSON(t, alice, "propose_create_issue", map[string]any{"project": "SEC", "title": "Из агента", "priority": "low"})
	rej := id(out)
	f.expect("alice", "POST", "/api/v1/proposals/"+rej+"/reject", nil, 200)
	f.expect("alice", "POST", "/api/v1/proposals/"+rej+"/approve", nil, 409)
	out, _ = callJSON(t, alice, "propose_create_issue", map[string]any{"project": "SEC", "title": "Из агента", "priority": "low"})
	json.Unmarshal(f.expect("alice", "POST", "/api/v1/proposals/"+id(out)+"/approve", nil, 200), &applied)
	if applied.State != "applied" || !strings.HasPrefix(applied.Result, "SEC-") {
		t.Fatalf("create = %+v", applied)
	}

	// Rights apply at proposal time already: bob cannot target SEC, a viewer
	// cannot propose at all.
	if msg, isErr := callJSON(t, bob, "propose_comment", map[string]any{"key": "SEC-1", "body": "x"}); !isErr || !strings.Contains(msg, "не найдено") {
		t.Fatalf("bob on SEC: %s", msg)
	}
	if msg, isErr := callJSON(t, bob, "propose_create_issue", map[string]any{"project": "SEC", "title": "x"}); !isErr || strings.Contains(msg, "Секрет") {
		t.Fatalf("bob creates in SEC: %s", msg)
	}
	if msg, isErr := callJSON(t, vic, "propose_comment", map[string]any{"key": "WEB-1", "body": "x"}); !isErr || !strings.Contains(msg, "недостаточно прав") {
		t.Fatalf("viewer proposes: %s", msg)
	}

	// The person sees their pending proposals in Lumen.
	callJSON(t, alice, "propose_comment", map[string]any{"key": "WEB-1", "body": "ещё"})
	var pending listEnvelope[struct{ ID, State, Preview string }]
	json.Unmarshal(f.expect("alice", "GET", "/api/v1/proposals?pending=true", nil, 200), &pending)
	if pending.Total != 1 || !strings.Contains(pending.Items[0].Preview, "ещё") {
		t.Fatalf("pending = %+v", pending.Items)
	}
	json.Unmarshal(f.expect("bob", "GET", "/api/v1/proposals?pending=true", nil, 200), &pending)
	if pending.Total != 0 {
		t.Fatalf("bob sees alice's proposals: %+v", pending.Items)
	}
}
