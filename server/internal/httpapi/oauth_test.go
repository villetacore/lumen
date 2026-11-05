package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const pkceVerifier = "a-long-enough-verifier-for-pkce-0123456789-abcdef"

func pkceChallenge() string {
	sum := sha256.Sum256([]byte(pkceVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// oauthDo sends a request with optional Keycloak identity and returns the
// response with its body already read.
func (f *oidcFixture) oauthDo(method, path, user, contentType, body string, headers map[string]string) (*http.Response, string) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if user != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[user])
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Transport: f.srv.Client().Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res, b.String()
}

const redirect = "http://127.0.0.1:53682/callback"

func (f *oidcFixture) register() string {
	f.t.Helper()
	res, body := f.oauthDo("POST", "/oauth/register", "", "application/json",
		`{"client_name":"Claude","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`, nil)
	if res.StatusCode != http.StatusCreated {
		f.t.Fatalf("register: %d %s", res.StatusCode, body)
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	json.Unmarshal([]byte(body), &out)
	return out.ClientID
}

func consentBody(clientID string, approve bool) string {
	b, _ := json.Marshal(map[string]any{"client_id": clientID, "redirect_uri": redirect, "state": "st-1", "response_type": "code",
		"code_challenge": pkceChallenge(), "code_challenge_method": "S256", "approve": approve})
	return string(b)
}

// approve lets the person allow the client and returns the code.
func (f *oidcFixture) approve(user, clientID string) string {
	f.t.Helper()
	// The directory learns about the person from their signed-in requests.
	f.oauthDo("GET", "/api/v1/me", user, "", "", nil)
	res, body := f.oauthDo("POST", "/api/v1/oauth/consent", user, "application/json", consentBody(clientID, true), nil)
	if res.StatusCode != http.StatusOK {
		f.t.Fatalf("consent: %d %s", res.StatusCode, body)
	}
	var out struct {
		RedirectTo string `json:"redirect_to"`
	}
	json.Unmarshal([]byte(body), &out)
	u, err := url.Parse(out.RedirectTo)
	if err != nil {
		f.t.Fatal(err)
	}
	if u.Query().Get("state") != "st-1" || u.Query().Get("code") == "" {
		f.t.Fatalf("redirect: %s", out.RedirectTo)
	}
	return u.Query().Get("code")
}

type tokenReply struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Error   string `json:"error"`
}

func (f *oidcFixture) token(form url.Values) (int, tokenReply) {
	f.t.Helper()
	res, body := f.oauthDo("POST", "/oauth/token", "", "application/x-www-form-urlencoded", form.Encode(), nil)
	var out tokenReply
	json.Unmarshal([]byte(body), &out)
	return res.StatusCode, out
}

func exchangeForm(clientID, code string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {pkceVerifier}}
}

// connect runs the whole flow and returns the tokens.
func (f *oidcFixture) connect(user string) (string, tokenReply) {
	f.t.Helper()
	id := f.register()
	status, tok := f.token(exchangeForm(id, f.approve(user, id)))
	if status != http.StatusOK || tok.Access == "" || tok.Refresh == "" {
		f.t.Fatalf("exchange: %d %+v", status, tok)
	}
	return id, tok
}

func (f *oidcFixture) mcpStatus(access string) int {
	f.t.Helper()
	res, _ := f.oauthDo("POST", "/mcp", "", "application/json", `{}`, map[string]string{"Authorization": "Bearer " + access})
	return res.StatusCode
}

func TestMCPAnswersByWhoAsks(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	browser := map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}

	// Without the web UI there is no page to send a person to.
	res, _ := f.oauthDo("GET", "/mcp", "", "", "", browser)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("browser, no UI: %d", res.StatusCode)
	}
	f.cfg.ServeUI = true
	res, _ = f.oauthDo("GET", "/mcp", "", "", "", browser)
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/connect" {
		t.Fatalf("browser: %d %q", res.StatusCode, res.Header.Get("Location"))
	}

	// A client must still get the challenge that starts its sign-in.
	for _, h := range []map[string]string{{"Accept": "text/event-stream"}, {"Accept": "text/event-stream, text/html"}} {
		res, _ = f.oauthDo("GET", "/mcp", "", "", "", h)
		if res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), "resource_metadata") {
			t.Fatalf("client %v: %d %s", h, res.StatusCode, res.Header.Get("WWW-Authenticate"))
		}
	}

	// A bad token is not a missing one, and a browser header does not hide it.
	res, body := f.oauthDo("GET", "/mcp", "", "", "", map[string]string{"Accept": browser["Accept"], "Authorization": "Bearer lum_invalid"})
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), `error="invalid_token"`) || !strings.Contains(body, "недействителен") {
		t.Fatalf("bad token: %d %s %s", res.StatusCode, res.Header.Get("WWW-Authenticate"), body)
	}
}

func TestOAuthDiscovery(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	res, _ := f.oauthDo("POST", "/mcp", "", "application/json", `{}`, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
	if h := res.Header.Get("WWW-Authenticate"); !strings.Contains(h, `resource_metadata="`+f.srv.URL+`/.well-known/oauth-protected-resource"`) {
		t.Fatalf("challenge: %s", h)
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		res, body := f.oauthDo("GET", path, "", "", "", nil)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, f.srv.URL+"/mcp") {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
	}
	res, body := f.oauthDo("GET", "/.well-known/oauth-authorization-server", "", "", "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"S256"`) || !strings.Contains(body, "/oauth/register") {
		t.Fatalf("metadata: %d %s", res.StatusCode, body)
	}
}

func TestOAuthFlowActsAsThePerson(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	_, tok := f.connect("alice")

	f.tokens["alice-oauth"] = tok.Access
	text, failed := callJSON(t, f.mcpSession(t, "alice-oauth"), "whoami", nil)
	if failed || !strings.Contains(text, `"username":"alice"`) {
		t.Fatalf("whoami: %s", text)
	}

	// The token opens MCP and nothing else.
	res, _ := f.oauthDo("GET", "/api/v1/stats", "alice-oauth", "", "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("REST API with an MCP token: %d", res.StatusCode)
	}
	if status := f.mcpStatus("lum_at_made-up"); status != http.StatusUnauthorized {
		t.Fatalf("made-up token: %d", status)
	}
}

func TestOAuthAuthorizeChecksBeforeRedirecting(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	id := f.register()
	q := url.Values{"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"},
		"code_challenge": {pkceChallenge()}, "code_challenge_method": {"S256"}, "state": {"s"}}

	res, _ := f.oauthDo("GET", "/oauth/authorize?"+q.Encode(), "", "", "", nil)
	if res.StatusCode != http.StatusFound || !strings.HasPrefix(res.Header.Get("Location"), "/connect/authorize?") {
		t.Fatalf("to consent: %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	// An address nobody registered is never redirected to.
	evil := url.Values{}
	for k, v := range q {
		evil[k] = v
	}
	evil.Set("redirect_uri", "https://evil.example/cb")
	res, _ = f.oauthDo("GET", "/oauth/authorize?"+evil.Encode(), "", "", "", nil)
	if res.StatusCode != http.StatusBadRequest || res.Header.Get("Location") != "" {
		t.Fatalf("foreign redirect: %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	// Without PKCE the client hears about it at its own address.
	noPKCE := url.Values{}
	for k, v := range q {
		noPKCE[k] = v
	}
	noPKCE.Del("code_challenge")
	res, _ = f.oauthDo("GET", "/oauth/authorize?"+noPKCE.Encode(), "", "", "", nil)
	if !strings.HasPrefix(res.Header.Get("Location"), redirect) || !strings.Contains(res.Header.Get("Location"), "error=invalid_request") {
		t.Fatalf("no PKCE: %d %s", res.StatusCode, res.Header.Get("Location"))
	}

	// A token for another service is refused.
	other := url.Values{}
	for k, v := range q {
		other[k] = v
	}
	other.Set("resource", "https://other.example/mcp")
	res, _ = f.oauthDo("GET", "/oauth/authorize?"+other.Encode(), "", "", "", nil)
	if !strings.Contains(res.Header.Get("Location"), "error=invalid_target") {
		t.Fatalf("foreign resource: %s", res.Header.Get("Location"))
	}
}

func TestOAuthRegisterRefusesUnsafeRedirects(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	for _, uri := range []string{"http://evil.example/cb", "javascript:alert(1)", "data:text/html,x", "myapp://cb", "https://a.example/cb#frag", ""} {
		body, _ := json.Marshal(map[string]any{"client_name": "x", "redirect_uris": []string{uri}})
		res, text := f.oauthDo("POST", "/oauth/register", "", "application/json", string(body), nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: %d %s", uri, res.StatusCode, text)
		}
	}
}

func TestOAuthConsentRules(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	id := f.register()

	res, _ := f.oauthDo("POST", "/api/v1/oauth/consent", "", "application/json", consentBody(id, true), nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("nobody signed in: %d", res.StatusCode)
	}
	res, _ = f.oauthDo("POST", "/api/v1/oauth/consent", "alice", "application/json", consentBody(id, true),
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("another site's page: %d", res.StatusCode)
	}
	res, body := f.oauthDo("POST", "/api/v1/oauth/consent", "alice", "application/json", consentBody(id, false), nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "error=access_denied") || strings.Contains(body, "code=") {
		t.Fatalf("refusal: %d %s", res.StatusCode, body)
	}

	// The static token belongs to nobody, so it cannot allow a client.
	f.cfg.Token = "static-secret"
	f.tokens["static"] = "static-secret"
	res, _ = f.oauthDo("POST", "/api/v1/oauth/consent", "static", "application/json", consentBody(id, true), nil)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("static token: %d", res.StatusCode)
	}
}

func TestOAuthCodeWorksOnce(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	id := f.register()
	code := f.approve("alice", id)

	wrong := exchangeForm(id, code)
	wrong.Set("code_verifier", strings.Repeat("x", 50))
	if status, tok := f.token(wrong); status != http.StatusBadRequest || tok.Error != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %+v", status, tok)
	}
	// The failed attempt spent the code.
	if status, _ := f.token(exchangeForm(id, code)); status != http.StatusBadRequest {
		t.Fatalf("after a failed attempt: %d", status)
	}

	code = f.approve("alice", id)
	status, tok := f.token(exchangeForm(id, code))
	if status != http.StatusOK {
		t.Fatalf("exchange: %d", status)
	}
	if got := f.mcpStatus(tok.Access); got == http.StatusUnauthorized {
		t.Fatal("token should work")
	}
	// Replay withdraws what the code produced.
	if status, _ := f.token(exchangeForm(id, code)); status != http.StatusBadRequest {
		t.Fatalf("replay: %d", status)
	}
	if got := f.mcpStatus(tok.Access); got != http.StatusUnauthorized {
		t.Fatalf("after replay the token still works: %d", got)
	}
}

func TestOAuthCodeIsBoundToClientAndRedirect(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	id := f.register()
	other := f.register()
	form := exchangeForm(other, f.approve("alice", id))
	if status, _ := f.token(form); status != http.StatusBadRequest {
		t.Fatalf("another client: %d", status)
	}
	form = exchangeForm(id, f.approve("alice", id))
	form.Set("redirect_uri", "http://127.0.0.1:1/other")
	if status, _ := f.token(form); status != http.StatusBadRequest {
		t.Fatalf("another redirect: %d", status)
	}
}

func TestOAuthRefresh(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	id, tok := f.connect("alice")
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {tok.Refresh}}

	other := f.register()
	if status, _ := f.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {other}, "refresh_token": {tok.Refresh}}); status != http.StatusBadRequest {
		t.Fatalf("another client's refresh: %d", status)
	}
	status, next := f.token(form)
	if status != http.StatusOK || next.Refresh == tok.Refresh || next.Access == tok.Access {
		t.Fatalf("refresh: %d %+v", status, next)
	}
	if got := f.mcpStatus(next.Access); got == http.StatusUnauthorized {
		t.Fatal("new token should work")
	}
}

func TestOAuthConnectionsArePersonal(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	_, tok := f.connect("alice")

	list := func(user string) []struct {
		ID         string `json:"id"`
		ClientName string `json:"client_name"`
	} {
		res, body := f.oauthDo("GET", "/api/v1/mcp/connections", user, "", "", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("list: %d %s", res.StatusCode, body)
		}
		var out struct {
			Items []struct {
				ID         string `json:"id"`
				ClientName string `json:"client_name"`
			} `json:"items"`
		}
		json.Unmarshal([]byte(body), &out)
		return out.Items
	}
	mine := list("alice")
	if len(mine) != 1 || mine[0].ClientName != "Claude" {
		t.Fatalf("alice: %+v", mine)
	}
	if theirs := list("bob"); len(theirs) != 0 {
		t.Fatalf("bob sees someone else's: %+v", theirs)
	}
	res, _ := f.oauthDo("DELETE", "/api/v1/mcp/connections/"+mine[0].ID, "bob", "", "", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("bob disconnects alice: %d", res.StatusCode)
	}
	if got := f.mcpStatus(tok.Access); got == http.StatusUnauthorized {
		t.Fatal("alice should still be connected")
	}
	res, _ = f.oauthDo("DELETE", "/api/v1/mcp/connections/"+mine[0].ID, "alice", "", "", nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("disconnect: %d", res.StatusCode)
	}
	if got := f.mcpStatus(tok.Access); got != http.StatusUnauthorized {
		t.Fatalf("after disconnect: %d", got)
	}
	if status, _ := f.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {"x"}, "refresh_token": {tok.Refresh}}); status != http.StatusBadRequest {
		t.Fatalf("refresh after disconnect: %d", status)
	}
}

func TestOAuthSeesOnlyWhatThePersonSees(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	_, tok := f.connect("bob")
	f.tokens["bob-oauth"] = tok.Access
	if _, failed := callJSON(t, f.mcpSession(t, "bob"), "list_projects", nil); failed {
		t.Fatal("bob via Keycloak")
	}
	viaKeycloak, _ := callJSON(t, f.mcpSession(t, "bob"), "list_projects", nil)
	viaOAuth, failed := callJSON(t, f.mcpSession(t, "bob-oauth"), "list_projects", nil)
	if failed || viaKeycloak != viaOAuth {
		t.Fatalf("Keycloak: %s\nOAuth: %s", viaKeycloak, viaOAuth)
	}
}
