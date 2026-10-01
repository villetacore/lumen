package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config describes how Lumen talks to Keycloak (or any OIDC provider).
type Config struct {
	Issuer       string   // https://keycloak.example.com/realms/main
	ClientID     string   // lumen
	ClientSecret string   // пусто для публичного клиента с PKCE
	RedirectURL  string   // https://lumen.example.com/auth/callback
	Scopes       []string // openid profile email по умолчанию
	// RequiredRoles gates access: пользователь должен иметь хотя бы одну из них.
	RequiredRoles []string
	// SessionTTL ограничивает жизнь cookie-сессии.
	SessionTTL time.Duration
	// InsecureCookie снимает флаг Secure — только для http://localhost.
	InsecureCookie bool
}

func (c Config) Enabled() bool { return c.Issuer != "" && c.ClientID != "" }

// Provider runs the authorization-code flow and owns the sessions it creates.
type Provider struct {
	cfg       Config
	discovery Discovery
	verifier  *Verifier
	client    *http.Client

	mu       sync.Mutex
	pending  map[string]pendingAuth // state → PKCE
	sessions map[string]*Session
}

type pendingAuth struct {
	codeVerifier string
	returnTo     string
	created      time.Time
}

// Session is a logged-in browser.
type Session struct {
	ID           string
	Claims       *Claims
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

const (
	pendingTTL        = 10 * time.Minute
	defaultSessionTTL = 12 * time.Hour
	// SessionCookie is the name of the cookie holding the session id.
	SessionCookie = "lumen_session"
)

// NewProvider performs discovery and prepares the token verifier.
func NewProvider(ctx context.Context, cfg Config, client *http.Client) (*Provider, error) {
	if !cfg.Enabled() {
		return nil, ErrNotConfigured
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	d, err := Discover(ctx, cfg.Issuer, client)
	if err != nil {
		return nil, err
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = defaultSessionTTL
	}
	return &Provider{
		cfg:       cfg,
		discovery: d,
		verifier:  NewVerifier(d, cfg.ClientID, client),
		client:    client,
		pending:   map[string]pendingAuth{},
		sessions:  map[string]*Session{},
	}, nil
}

func (p *Provider) Config() Config       { return p.cfg }
func (p *Provider) Discovery() Discovery { return p.discovery }

// Verify validates a bearer token — used by scripts and by other services that
// already hold a Keycloak token.
func (p *Provider) Verify(ctx context.Context, raw string) (*Claims, error) {
	claims, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	if err := p.Authorize(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// Authorize applies the role requirement, if one is configured.
func (p *Provider) Authorize(claims *Claims) error {
	if len(p.cfg.RequiredRoles) == 0 {
		return nil
	}
	have := map[string]bool{}
	for _, r := range claims.Roles(p.cfg.ClientID) {
		have[r] = true
	}
	for _, g := range claims.Groups {
		have[strings.TrimPrefix(g, "/")] = true
	}
	for _, want := range p.cfg.RequiredRoles {
		if have[want] {
			return nil
		}
	}
	return fmt.Errorf("%w: нужна одна из %v", ErrForbidden, p.cfg.RequiredRoles)
}

// AuthCodeURL starts a login and returns where to send the browser.
func (p *Provider) AuthCodeURL(returnTo string) (string, error) {
	state, err := randomString(32)
	if err != nil {
		return "", err
	}
	codeVerifier, err := randomString(48)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	p.mu.Lock()
	p.sweepLocked()
	p.pending[state] = pendingAuth{codeVerifier: codeVerifier, returnTo: returnTo, created: time.Now()}
	p.mu.Unlock()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	q.Set("state", state)
	// PKCE закрывает перехват кода даже у конфиденциального клиента.
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	return p.discovery.AuthURL + "?" + q.Encode(), nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Exchange finishes the flow: swaps the code for tokens and creates a session.
func (p *Provider) Exchange(ctx context.Context, state, code string) (*Session, string, error) {
	p.mu.Lock()
	p.sweepLocked()
	pend, ok := p.pending[state]
	delete(p.pending, state)
	p.mu.Unlock()
	if !ok {
		return nil, "", fmt.Errorf("%w: неизвестный state, начните вход заново", ErrBadToken)
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", pend.codeVerifier)
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.discovery.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("обмен кода на токен: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, "", fmt.Errorf("ответ %s: %w", p.discovery.TokenURL, err)
	}
	if tr.Error != "" {
		return nil, "", fmt.Errorf("%s: %s %s", p.discovery.TokenURL, tr.Error, tr.ErrorDesc)
	}
	if tr.IDToken == "" {
		return nil, "", fmt.Errorf("%s не вернул id_token", p.discovery.TokenURL)
	}

	claims, err := p.verifier.Verify(ctx, tr.IDToken)
	if err != nil {
		return nil, "", err
	}
	if err := p.Authorize(claims); err != nil {
		return nil, "", err
	}

	id, err := randomString(32)
	if err != nil {
		return nil, "", err
	}
	expiry := time.Now().Add(p.cfg.SessionTTL)
	if tr.ExpiresIn > 0 {
		if tokenExpiry := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second); tokenExpiry.Before(expiry) {
			// Сессия не должна пережить сам токен больше, чем на TTL.
			expiry = time.Now().Add(p.cfg.SessionTTL)
		}
	}
	session := &Session{
		ID: id, Claims: claims, AccessToken: tr.AccessToken,
		RefreshToken: tr.RefreshToken, Expiry: expiry,
	}

	p.mu.Lock()
	p.sessions[id] = session
	p.mu.Unlock()

	returnTo := pend.returnTo
	if returnTo == "" {
		returnTo = "/"
	}
	return session, returnTo, nil
}

// Session looks up a live session.
func (p *Provider) Session(id string) (*Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(s.Expiry) {
		delete(p.sessions, id)
		return nil, false
	}
	return s, true
}

// EndSession drops a session and returns where to send the browser to also log
// out of Keycloak, if the provider supports it.
func (p *Provider) EndSession(id, redirectTo string) string {
	p.mu.Lock()
	delete(p.sessions, id)
	p.mu.Unlock()

	if p.discovery.EndSessionURL == "" {
		return redirectTo
	}
	q := url.Values{}
	q.Set("client_id", p.cfg.ClientID)
	if redirectTo != "" {
		q.Set("post_logout_redirect_uri", redirectTo)
	}
	return p.discovery.EndSessionURL + "?" + q.Encode()
}

// Cookie builds the session cookie with the right flags for the deployment.
func (p *Provider) Cookie(value string, expiry time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true, // токен недоступен из JavaScript
		Secure:   !p.cfg.InsecureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiry,
	}
}

// sweepLocked drops stale pending logins and expired sessions.
func (p *Provider) sweepLocked() {
	now := time.Now()
	for state, pend := range p.pending {
		if now.Sub(pend.created) > pendingTTL {
			delete(p.pending, state)
		}
	}
	for id, s := range p.sessions {
		if now.After(s.Expiry) {
			delete(p.sessions, id)
		}
	}
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
