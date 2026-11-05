package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/villetacore/lumen/server/internal/oauth"
	"github.com/villetacore/lumen/server/internal/policy"
)

// MCP clients connect through OAuth with Lumen as the authorization server
// (ADR-0019). The person signs in as usual and allows the client on a screen in
// Lumen; the resulting token opens /mcp and nothing else.

func (a *API) baseURL(r *http.Request) string {
	if a.cfg.PublicURL != "" {
		return strings.TrimRight(a.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// oauthPrincipal identifies a request to /mcp by an OAuth token. Name, groups
// and role come from the directory, which follows Keycloak, so a change there
// reaches the client without reconnecting.
func (a *API) oauthPrincipal(r *http.Request) *policy.Principal {
	if a.oauth == nil || r.URL.Path != "/mcp" {
		return nil
	}
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(raw, oauth.TokenPrefix) {
		return nil
	}
	id, err := a.oauth.Verify(r.Context(), raw)
	if err != nil {
		if !errors.Is(err, oauth.ErrInvalidGrant) {
			a.log.Warn("токен MCP не проверен", "err", err)
		}
		return nil
	}
	p, err := a.people.Principal(r.Context(), id)
	if err != nil {
		a.log.Warn("пользователь токена MCP не найден", "err", err)
		return nil
	}
	return p
}

func (a *API) oauthEnabled(w http.ResponseWriter) bool {
	if a.oauth == nil {
		writeError(w, http.StatusNotFound, "not_found", "OAuth доступен только при входе через Keycloak", nil)
		return false
	}
	return true
}

func (a *API) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	base := a.baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Lumen",
	})
}

func (a *API) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	base := a.baseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func (a *API) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	var req struct {
		Name         string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "неверный JSON")
		return
	}
	c, err := a.oauth.Register(r.Context(), req.Name, req.RedirectURIs)
	switch {
	case errors.Is(err, oauth.ErrRedirectURI):
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "допустимы https, http на этом устройстве и схемы известных приложений")
		return
	case errors.Is(err, oauth.ErrTooMany):
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", err.Error())
		return
	case err != nil:
		a.log.Error("регистрация клиента OAuth", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "не удалось зарегистрировать клиента")
		return
	}
	// The next request from this client is /oauth/authorize in a browser: without
	// it in the log, the sign-in stopped before reaching Lumen.
	a.log.Info("клиент OAuth зарегистрирован", "client", c.ID, "name", c.Name, "redirects", redirectLabels(c.RedirectURIs))
	w.Header().Set("Cache-Control", "no-store")
	// Public clients with PKCE: no secret, whatever the request asked for.
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

func redirectLabels(uris []string) []string {
	labels := make([]string, len(uris))
	for i, uri := range uris {
		labels[i] = oauth.RedirectLabel(uri)
	}
	return labels
}

// authorizeParams is the authorization request, from the query string and again
// from the consent form: the form is checked as strictly as the original.
type authorizeParams struct {
	ClientID        string `json:"client_id"`
	RedirectURI     string `json:"redirect_uri"`
	State           string `json:"state"`
	ResponseType    string `json:"response_type"`
	Challenge       string `json:"code_challenge"`
	ChallengeMethod string `json:"code_challenge_method"`
	Resource        string `json:"resource"`
	Scope           string `json:"scope"`
}

func paramsFrom(q url.Values) authorizeParams {
	return authorizeParams{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), State: q.Get("state"),
		ResponseType: q.Get("response_type"), Challenge: q.Get("code_challenge"),
		ChallengeMethod: q.Get("code_challenge_method"), Resource: q.Get("resource"), Scope: q.Get("scope")}
}

// client checks what decides whether redirecting back is safe at all. On a
// failure here the error is shown, never sent to the unverified address.
func (a *API) authorizeClient(r *http.Request, p authorizeParams) (oauth.Client, error) {
	c, err := a.oauth.Client(r.Context(), p.ClientID)
	if err != nil {
		return oauth.Client{}, err
	}
	if !c.Allows(p.RedirectURI) {
		return oauth.Client{}, oauth.ErrRedirectURI
	}
	return c, nil
}

// requestError reports what the client is told through the redirect.
func (a *API) requestError(r *http.Request, p authorizeParams) (string, string) {
	switch {
	case p.ResponseType != "code":
		return "unsupported_response_type", "поддерживается только code"
	case p.Challenge == "" || p.ChallengeMethod != "S256":
		return "invalid_request", "нужен PKCE с S256"
	case p.Resource != "" && strings.TrimRight(p.Resource, "/") != a.baseURL(r)+"/mcp":
		return "invalid_target", "этот сервер выдаёт токены только для " + a.baseURL(r) + "/mcp"
	}
	return "", ""
}

func (a *API) redirectBack(r *http.Request, p authorizeParams, values url.Values) (string, error) {
	u, err := url.Parse(p.RedirectURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range values {
		q[k] = v
	}
	if p.State != "" {
		q.Set("state", p.State)
	}
	q.Set("iss", a.baseURL(r))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (a *API) authorizeClientFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, oauth.ErrNotFound):
		writeError(w, http.StatusBadRequest, "invalid_client", "такой клиент не зарегистрирован: подключите его заново", nil)
	case errors.Is(err, oauth.ErrRedirectURI):
		writeError(w, http.StatusBadRequest, "invalid_redirect_uri", "этот адрес возврата не указан при регистрации клиента", nil)
	default:
		a.log.Error("проверка клиента OAuth", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось проверить клиента", nil)
	}
}

// handleOAuthAuthorize checks the request and hands over to the consent screen,
// which sits behind the usual sign-in.
func (a *API) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	p := paramsFrom(r.URL.Query())
	if _, err := a.authorizeClient(r, p); err != nil {
		a.authorizeClientFailed(w, err)
		return
	}
	if code, description := a.requestError(r, p); code != "" {
		to, err := a.redirectBack(r, p, url.Values{"error": {code}, "error_description": {description}})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_redirect_uri", "неверный адрес возврата", nil)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/connect/authorize?"+r.URL.RawQuery, http.StatusFound)
}

// handleConsentInfo tells the consent screen what it is asking about.
func (a *API) handleConsentInfo(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	p := paramsFrom(r.URL.Query())
	c, err := a.authorizeClient(r, p)
	if err != nil {
		a.authorizeClientFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"client_name": c.Name, "redirect_host": oauth.RedirectLabel(p.RedirectURI)})
}

// handleConsent is the person's answer. The session cookie is what a page on
// another site would be after, so a cross-site request is refused first, and
// only a real person can allow a client: the static token belongs to nobody.
func (a *API) handleConsent(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	if err := a.loginOrigin.Check(r); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "запрос с другого сайта", nil)
		return
	}
	who := policy.From(r.Context())
	if who == nil || who.ID == "token" || who.ID == "local" {
		writeError(w, http.StatusForbidden, "forbidden", "разрешить подключение может только вошедший человек", nil)
		return
	}
	var req struct {
		authorizeParams
		Approve bool `json:"approve"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "неверный запрос", nil)
		return
	}
	p := req.authorizeParams
	if _, err := a.authorizeClient(r, p); err != nil {
		a.authorizeClientFailed(w, err)
		return
	}
	values := url.Values{}
	if code, description := a.requestError(r, p); code != "" {
		values.Set("error", code)
		values.Set("error_description", description)
	} else if !req.Approve {
		values.Set("error", "access_denied")
		values.Set("error_description", "пользователь отказал")
	} else {
		code, err := a.oauth.Approve(r.Context(), p.ClientID, who.ID, p.RedirectURI, p.Challenge)
		if err != nil {
			a.log.Error("разрешение OAuth", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить разрешение", nil)
			return
		}
		values.Set("code", code)
	}
	to, err := a.redirectBack(r, p, values)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_redirect_uri", "неверный адрес возврата", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"redirect_to": to})
}

func (a *API) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "неверная форма")
		return
	}
	f := r.PostForm
	if res := f.Get("resource"); res != "" && strings.TrimRight(res, "/") != a.baseURL(r)+"/mcp" {
		oauthError(w, http.StatusBadRequest, "invalid_target", "неизвестный ресурс")
		return
	}
	var (
		tokens oauth.Tokens
		err    error
	)
	switch f.Get("grant_type") {
	case "authorization_code":
		tokens, err = a.oauth.Exchange(r.Context(), f.Get("code"), f.Get("client_id"), f.Get("redirect_uri"), f.Get("code_verifier"))
	case "refresh_token":
		tokens, err = a.oauth.Refresh(r.Context(), f.Get("refresh_token"), f.Get("client_id"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "поддерживаются authorization_code и refresh_token")
		return
	}
	switch {
	case errors.Is(err, oauth.ErrInvalidGrant):
		oauthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	case err != nil:
		a.log.Error("выдача токена OAuth", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "не удалось выдать токен")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.Access,
		"token_type":    "Bearer",
		"expires_in":    tokens.ExpiresIn,
		"refresh_token": tokens.Refresh,
	})
}

// handleMCPConnection says how to connect here, for the "AI clients" screen.
func (a *API) handleMCPConnection(w http.ResponseWriter, r *http.Request) {
	mode := "none"
	switch {
	case a.oauth != nil:
		mode = "oauth"
	case a.oidc != nil || a.cfg.Token != "":
		mode = "token"
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": a.baseURL(r) + "/mcp", "auth": mode})
}

func (a *API) handleListConnections(w http.ResponseWriter, r *http.Request) {
	who := policy.From(r.Context())
	if a.oauth == nil || who == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []oauth.Connection{}})
		return
	}
	items, err := a.oauth.Connections(r.Context(), who.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if !a.oauthEnabled(w) {
		return
	}
	who := policy.From(r.Context())
	if who == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "нужен вход", nil)
		return
	}
	switch err := a.oauth.Disconnect(r.Context(), r.PathValue("id"), who.ID); {
	case errors.Is(err, oauth.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "подключение не найдено", nil)
	case err != nil:
		fail(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// noFraming keeps the consent screen out of other sites' frames.
func noFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
