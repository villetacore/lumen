// Package oauth lets MCP clients connect to Lumen by asking the person for
// permission instead of being handed a token (ADR-0019). Keycloak still decides
// who the person is; this package only remembers which client they allowed.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/state"
)

const (
	CodeTTL   = time.Minute
	AccessTTL = time.Hour
	// GrantTTL is absolute, so someone removed from Keycloak loses access at
	// the latest this long after they last allowed the client.
	GrantTTL = 30 * 24 * time.Hour
	// refreshLeeway forgives two sessions of one client refreshing at once, and
	// a retry after a lost response. Later reuse of a rotated token means it
	// leaked, and the whole grant goes.
	refreshLeeway = 30 * time.Second

	unusedClientTTL  = 24 * time.Hour
	unusedGrantTTL   = time.Hour
	maxUnusedClients = 1000

	// TokenPrefix tells these tokens from Keycloak's JWTs and the static token.
	TokenPrefix = "lum_"
)

var (
	ErrNotFound     = errors.New("не найдено")
	ErrInvalidGrant = errors.New("код или токен недействителен")
	ErrTooMany      = errors.New("слишком много незавершённых регистраций")
	ErrRedirectURI  = errors.New("недопустимый redirect_uri")
)

var nowFunc = time.Now

// nativeSchemes are the private-use redirect schemes of known desktop clients.
// Any other scheme could hand the code to whichever app claimed it.
var nativeSchemes = map[string]bool{"cursor": true, "vscode": true, "vscode-insiders": true}

// Client is a registered MCP client. Anyone may register one, so its name is
// only a claim: the consent screen shows where the code would go as well.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	CreatedAt    string
}

// Allows applies RFC 8252: an exact match, except that a loopback redirect may
// use any port, because native clients pick one at run time.
func (c Client) Allows(uri string) bool {
	for _, known := range c.RedirectURIs {
		if known == uri {
			return true
		}
		a, errA := url.Parse(known)
		b, errB := url.Parse(uri)
		if errA == nil && errB == nil && isLoopback(a) && isLoopback(b) &&
			a.Scheme == b.Scheme && a.Hostname() == b.Hostname() && a.Path == b.Path && a.RawQuery == b.RawQuery {
			return true
		}
	}
	return false
}

func isLoopback(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// ValidRedirectURI accepts https, http on loopback and known native schemes.
func ValidRedirectURI(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Fragment != "" || u.User != nil || uri == "" {
		return false
	}
	switch {
	case u.Scheme == "https":
		return u.Host != ""
	case u.Scheme == "http":
		return isLoopback(u)
	}
	return nativeSchemes[u.Scheme]
}

// Connection is what a person sees about a client they allowed.
type Connection struct {
	ID           string `json:"id"`
	ClientName   string `json:"client_name"`
	RedirectHost string `json:"redirect_host"`
	ConnectedAt  string `json:"connected_at"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
	ExpiresAt    string `json:"expires_at"`
}

// Tokens is the result of a code exchange or a refresh.
type Tokens struct {
	Access    string
	Refresh   string
	ExpiresIn int
}

// Store keeps clients and grants in the operational database.
type Store struct{ db *state.DB }

func New(db *state.DB) *Store { return &Store{db: db} }

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func random(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// prune removes what was never finished or has expired.
func (s *Store) prune(ctx context.Context) error {
	now := nowFunc()
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM oauth_grants WHERE expires_at <= ? OR (connected_at = '' AND created_at <= ?)`,
			[]any{stamp(now), stamp(now.Add(-unusedGrantTTL))}},
		{`DELETE FROM oauth_tokens WHERE expires_at <= ?`, []any{stamp(now)}},
		{`DELETE FROM oauth_clients WHERE created_at <= ? AND id NOT IN (SELECT client_id FROM oauth_grants)`,
			[]any{stamp(now.Add(-unusedClientTTL))}},
	} {
		if _, err := s.db.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

// Register records a client (RFC 7591). Registration needs no sign-in, so the
// number of clients nobody has allowed is capped.
func (s *Store) Register(ctx context.Context, name string, redirectURIs []string) (Client, error) {
	if len(redirectURIs) == 0 || len(redirectURIs) > 10 {
		return Client{}, ErrRedirectURI
	}
	for _, uri := range redirectURIs {
		if !ValidRedirectURI(uri) {
			return Client{}, ErrRedirectURI
		}
	}
	if err := s.prune(ctx); err != nil {
		return Client{}, err
	}
	var unused int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM oauth_clients WHERE id NOT IN (SELECT client_id FROM oauth_grants)`).Scan(&unused); err != nil {
		return Client{}, err
	}
	if unused >= maxUnusedClients {
		return Client{}, ErrTooMany
	}
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 100 {
		name = string(r[:100])
	}
	if name == "" {
		name = "MCP-клиент"
	}
	id, err := random("mcp_")
	if err != nil {
		return Client{}, err
	}
	uris, err := json.Marshal(redirectURIs)
	if err != nil {
		return Client{}, err
	}
	c := Client{ID: id, Name: name, RedirectURIs: redirectURIs, CreatedAt: stamp(nowFunc())}
	_, err = s.db.ExecContext(ctx, `INSERT INTO oauth_clients(id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`,
		c.ID, c.Name, string(uris), c.CreatedAt)
	return c, err
}

// Client returns a registered client.
func (s *Store) Client(ctx context.Context, id string) (Client, error) {
	var c Client
	var uris string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &uris, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, ErrNotFound
	}
	if err != nil {
		return Client{}, err
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return Client{}, err
	}
	return c, nil
}

// Approve records the person's permission and returns a one-time code.
func (s *Store) Approve(ctx context.Context, clientID, principalID, redirectURI, challenge string) (string, error) {
	if err := s.prune(ctx); err != nil {
		return "", err
	}
	grantID, err := random("g_")
	if err != nil {
		return "", err
	}
	code, err := random(TokenPrefix + "c_")
	if err != nil {
		return "", err
	}
	now := nowFunc()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_grants(id, client_id, principal_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		grantID, clientID, principalID, stamp(now), stamp(now.Add(GrantTTL))); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_codes(hash, grant_id, redirect_uri, challenge, expires_at) VALUES (?, ?, ?, ?, ?)`,
		hash(code), grantID, redirectURI, challenge, stamp(now.Add(CodeTTL))); err != nil {
		return "", err
	}
	return code, tx.Commit()
}

// verifyPKCE compares the S256 hash of the verifier with the challenge.
func verifyPKCE(verifier, challenge string) bool {
	if n := len(verifier); n < 43 || n > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// Exchange turns a code into tokens. A code works once; presenting a spent one
// means it leaked, so everything issued through it is withdrawn.
func (s *Store) Exchange(ctx context.Context, code, clientID, redirectURI, verifier string) (Tokens, error) {
	h := hash(code)
	now := nowFunc()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback()

	// The update comes first: of two simultaneous exchanges exactly one wins.
	res, err := tx.ExecContext(ctx, `UPDATE oauth_codes SET used_at = ? WHERE hash = ? AND used_at = ''`, stamp(now), h)
	if err != nil {
		return Tokens{}, err
	}
	won, err := res.RowsAffected()
	if err != nil {
		return Tokens{}, err
	}
	var grantID, wantRedirect, challenge, expires, grantClient, principal string
	err = tx.QueryRowContext(ctx, `SELECT c.grant_id, c.redirect_uri, c.challenge, c.expires_at, g.client_id, g.principal_id
		FROM oauth_codes c JOIN oauth_grants g ON g.id = c.grant_id WHERE c.hash = ?`, h).
		Scan(&grantID, &wantRedirect, &challenge, &expires, &grantClient, &principal)
	if errors.Is(err, sql.ErrNoRows) {
		return Tokens{}, ErrInvalidGrant
	}
	if err != nil {
		return Tokens{}, err
	}
	if won == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ?`, grantID); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidGrant
	}
	// A failed attempt still spends the code: the transaction commits.
	if grantClient != clientID || wantRedirect != redirectURI || expires <= stamp(now) || !verifyPKCE(verifier, challenge) {
		if err := tx.Commit(); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidGrant
	}
	// Connecting again replaces the previous connection of this client.
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE client_id = ? AND principal_id = ? AND id <> ?`,
		grantClient, principal, grantID); err != nil {
		return Tokens{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_grants SET connected_at = ?, last_used_at = ? WHERE id = ?`,
		stamp(now), stamp(now), grantID); err != nil {
		return Tokens{}, err
	}
	var grantExpires string
	if err := tx.QueryRowContext(ctx, `SELECT expires_at FROM oauth_grants WHERE id = ?`, grantID).Scan(&grantExpires); err != nil {
		return Tokens{}, err
	}
	tokens, err := issue(ctx, tx, grantID, grantExpires, now)
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit()
}

// issue writes a new token pair. Neither token outlives the grant.
func issue(ctx context.Context, tx *state.Tx, grantID, grantExpires string, now time.Time) (Tokens, error) {
	end, err := time.Parse(time.RFC3339, grantExpires)
	if err != nil {
		return Tokens{}, err
	}
	accessEnd := now.Add(AccessTTL)
	if end.Before(accessEnd) {
		accessEnd = end
	}
	access, err := random(TokenPrefix + "at_")
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := random(TokenPrefix + "rt_")
	if err != nil {
		return Tokens{}, err
	}
	for _, t := range []struct{ secret, kind, expires string }{
		{access, "access", stamp(accessEnd)},
		{refresh, "refresh", grantExpires},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_tokens(hash, grant_id, kind, expires_at) VALUES (?, ?, ?, ?)`,
			hash(t.secret), grantID, t.kind, t.expires); err != nil {
			return Tokens{}, err
		}
	}
	return Tokens{Access: access, Refresh: refresh, ExpiresIn: int(accessEnd.Sub(now).Seconds())}, nil
}

// Refresh rotates a refresh token.
func (s *Store) Refresh(ctx context.Context, token, clientID string) (Tokens, error) {
	h := hash(token)
	now := nowFunc()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback()

	var grantID, usedAt, expires, grantClient string
	err = tx.QueryRowContext(ctx, `SELECT t.grant_id, t.used_at, t.expires_at, g.client_id
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.hash = ? AND t.kind = 'refresh'`, h).Scan(&grantID, &usedAt, &expires, &grantClient)
	if errors.Is(err, sql.ErrNoRows) {
		return Tokens{}, ErrInvalidGrant
	}
	if err != nil {
		return Tokens{}, err
	}
	if grantClient != clientID || expires <= stamp(now) {
		return Tokens{}, ErrInvalidGrant
	}
	if usedAt != "" {
		used, err := time.Parse(time.RFC3339, usedAt)
		if err != nil {
			return Tokens{}, err
		}
		if now.Sub(used) > refreshLeeway {
			if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ?`, grantID); err != nil {
				return Tokens{}, err
			}
			if err := tx.Commit(); err != nil {
				return Tokens{}, err
			}
			return Tokens{}, ErrInvalidGrant
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE oauth_tokens SET used_at = ? WHERE hash = ?`, stamp(now), h); err != nil {
		return Tokens{}, err
	}
	tokens, err := issue(ctx, tx, grantID, expires, now)
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit()
}

// Verify returns whose access token this is.
func (s *Store) Verify(ctx context.Context, access string) (string, error) {
	now := nowFunc()
	var grantID, principal string
	err := s.db.QueryRowContext(ctx, `SELECT g.id, g.principal_id FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.hash = ? AND t.kind = 'access' AND t.expires_at > ? AND g.expires_at > ?`,
		hash(access), stamp(now), stamp(now)).Scan(&grantID, &principal)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidGrant
	}
	if err != nil {
		return "", err
	}
	// Precise enough for "last used", and most requests write nothing.
	if _, err := s.db.ExecContext(ctx, `UPDATE oauth_grants SET last_used_at = ? WHERE id = ? AND last_used_at <= ?`,
		stamp(now), grantID, stamp(now.Add(-5*time.Minute))); err != nil {
		return "", err
	}
	return principal, nil
}

// Connections lists the clients a person has connected, newest first.
func (s *Store) Connections(ctx context.Context, principalID string) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id, c.name, c.redirect_uris, g.connected_at, g.last_used_at, g.expires_at
		FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id
		WHERE g.principal_id = ? AND g.connected_at <> '' AND g.expires_at > ?
		ORDER BY g.connected_at DESC, g.id`, principalID, stamp(nowFunc()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		var uris string
		if err := rows.Scan(&c.ID, &c.ClientName, &uris, &c.ConnectedAt, &c.LastUsedAt, &c.ExpiresAt); err != nil {
			return nil, err
		}
		var list []string
		if json.Unmarshal([]byte(uris), &list) == nil && len(list) > 0 {
			c.RedirectHost = RedirectLabel(list[0])
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RedirectLabel names where a redirect leads, for a person to judge.
func RedirectLabel(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	if u.Host != "" && !isLoopback(u) {
		return u.Host
	}
	if isLoopback(u) {
		return "это устройство"
	}
	return u.Scheme
}

// Disconnect withdraws a person's own connection. Someone else's looks missing.
func (s *Store) Disconnect(ctx context.Context, id, principalID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ? AND principal_id = ?`, id, principalID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
