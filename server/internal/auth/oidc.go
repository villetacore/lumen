package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	store    SessionStore // nil: sessions end with the process
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
	// Direct marks a sign-in through Lumen's own form: the browser has no
	// Keycloak page to return to on logout.
	Direct bool
}

var (
	// ErrBadCredentials is a wrong login, password or one-time code.
	ErrBadCredentials = errors.New("неверный логин или пароль")
	// ErrAccountSetup means Keycloak wants something done first, such as a new password.
	ErrAccountSetup = errors.New("учётная запись не настроена до конца: войдите через страницу Keycloak")
	// ErrPasswordLoginDisabled means the client has no Direct Access Grants.
	ErrPasswordLoginDisabled = errors.New("в Keycloak для этого клиента выключен вход по паролю (Direct access grants)")
)

const (
	pendingTTL        = 10 * time.Minute
	defaultSessionTTL = 12 * time.Hour
	storeTimeout      = 2 * time.Second
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

// UseSessionStore makes sessions outlive the process.
func (p *Provider) UseSessionStore(st SessionStore) { p.store = st }

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

	tr, err := p.tokenRequest(ctx, form)
	if err != nil {
		return nil, "", err
	}
	if tr.Error != "" {
		return nil, "", fmt.Errorf("%s: %s %s", p.discovery.TokenURL, tr.Error, tr.ErrorDesc)
	}
	session, err := p.newSession(ctx, tr, false)
	if err != nil {
		return nil, "", err
	}
	returnTo := pend.returnTo
	if returnTo == "" {
		returnTo = "/"
	}
	return session, returnTo, nil
}

// PasswordLogin signs in through Lumen's own form: Keycloak checks the password
// (Direct Access Grant) and the browser never leaves Lumen. The password is used
// for this one request and is not kept or logged.
func (p *Provider) PasswordLogin(ctx context.Context, username, password, otp string) (*Session, error) {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", p.cfg.ClientID)
	form.Set("username", username)
	form.Set("password", password)
	form.Set("scope", strings.Join(p.cfg.Scopes, " "))
	if otp != "" {
		form.Set("totp", otp)
	}
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	tr, err := p.tokenRequest(ctx, form)
	if err != nil {
		return nil, err
	}
	switch {
	case tr.Error == "":
	case tr.Error == "unauthorized_client" || tr.Error == "invalid_client" || strings.Contains(tr.ErrorDesc, "direct access"):
		return nil, ErrPasswordLoginDisabled
	case tr.Error == "invalid_grant" && strings.Contains(strings.ToLower(tr.ErrorDesc), "not fully set up"):
		return nil, ErrAccountSetup
	case tr.Error == "invalid_grant":
		return nil, ErrBadCredentials
	default:
		return nil, fmt.Errorf("вход отклонён: %s", tr.Error)
	}
	return p.newSession(ctx, tr, true)
}

func (p *Provider) tokenRequest(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.discovery.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("обмен на токен: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("ответ %s: %w", p.discovery.TokenURL, err)
	}
	return &tr, nil
}

// newSession checks the token and whether this user may enter, then records
// the sign-in.
func (p *Provider) newSession(ctx context.Context, tr *tokenResponse, direct bool) (*Session, error) {
	if tr.IDToken == "" {
		return nil, fmt.Errorf("%s не вернул id_token", p.discovery.TokenURL)
	}
	claims, err := p.verifier.Verify(ctx, tr.IDToken)
	if err != nil {
		return nil, err
	}
	if err := p.Authorize(claims); err != nil {
		return nil, err
	}
	id, err := randomString(32)
	if err != nil {
		return nil, err
	}
	session := &Session{
		ID: id, Claims: claims, AccessToken: tr.AccessToken,
		RefreshToken: tr.RefreshToken, Expiry: time.Now().Add(p.cfg.SessionTTL), Direct: direct,
	}
	p.mu.Lock()
	p.sessions[id] = session
	p.mu.Unlock()
	if p.store != nil {
		// Losing this write costs a re-login after a restart, not this sign-in.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
		defer cancel()
		if err := p.store.Save(saveCtx, session); err != nil {
			slog.Warn("сессия не сохранена в хранилище", "err", err)
		}
	}
	return session, nil
}

// Session looks up a live session: in memory, then in the store, so a sign-in
// survives a restart of lumend.
func (p *Provider) Session(id string) (*Session, bool) {
	p.mu.Lock()
	s, ok := p.sessions[id]
	if ok && time.Now().After(s.Expiry) {
		delete(p.sessions, id)
		p.mu.Unlock()
		return nil, false
	}
	p.mu.Unlock()
	if ok {
		return s, true
	}
	if p.store == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	s, err := p.store.Load(ctx, id)
	if err != nil {
		slog.Warn("сессия не прочитана из хранилища", "err", err)
		return nil, false
	}
	if s == nil || time.Now().After(s.Expiry) {
		return nil, false
	}
	p.mu.Lock()
	p.sessions[id] = s
	p.mu.Unlock()
	return s, true
}

// EndSession drops a session and returns where to send the browser next. A
// sign-in through Keycloak's page continues there, to end its browser session;
// a sign-in through Lumen's form ends the Keycloak session from the server.
func (p *Provider) EndSession(id, redirectTo string) string {
	p.mu.Lock()
	s := p.sessions[id]
	delete(p.sessions, id)
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if p.store != nil && id != "" {
		if s == nil {
			s, _ = p.store.Load(ctx, id)
		}
		if err := p.store.Delete(ctx, id); err != nil {
			slog.Warn("сессия не удалена из хранилища", "err", err)
		}
	}
	if s != nil && s.Direct {
		if s.RefreshToken != "" && p.discovery.EndSessionURL != "" {
			p.revoke(ctx, s.RefreshToken)
		}
		return redirectTo
	}
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

// revoke is best effort: the Lumen session is already gone either way.
func (p *Provider) revoke(ctx context.Context, refresh string) {
	form := url.Values{}
	form.Set("client_id", p.cfg.ClientID)
	form.Set("refresh_token", refresh)
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.discovery.EndSessionURL, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := p.client.Do(req); err == nil {
		resp.Body.Close()
	}
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
