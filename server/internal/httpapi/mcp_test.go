package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

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
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must be marked read-only", tool.Name)
		}
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "get_issue,get_note,issue_history,list_issues,list_projects,list_sprints,search,whoami" {
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
