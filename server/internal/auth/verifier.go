// Package auth authenticates requests against an OIDC provider — Keycloak in
// particular — while keeping the local, no-auth mode working unchanged.
//
// Token validation is done here rather than delegated to a library so the
// server keeps its short dependency list: RS256 verification is stdlib crypto
// plus a JWKS fetch.
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoToken       = errors.New("нет токена")
	ErrBadToken      = errors.New("токен не прошёл проверку")
	ErrExpired       = errors.New("срок действия токена истёк")
	ErrWrongIssuer   = errors.New("токен выдан другим сервером")
	ErrWrongAud      = errors.New("токен выдан другому приложению")
	ErrUnknownKey    = errors.New("неизвестный ключ подписи")
	ErrForbidden     = errors.New("нет доступа: не хватает роли")
	ErrNotConfigured = errors.New("OIDC не настроен")
)

// leeway absorbs clock drift between Lumen and Keycloak.
const leeway = 60 * time.Second

// Discovery is the subset of the OIDC discovery document Lumen uses.
type Discovery struct {
	Issuer        string `json:"issuer"`
	AuthURL       string `json:"authorization_endpoint"`
	TokenURL      string `json:"token_endpoint"`
	JWKSURL       string `json:"jwks_uri"`
	UserInfoURL   string `json:"userinfo_endpoint"`
	EndSessionURL string `json:"end_session_endpoint"`
}

// Claims is what Lumen needs from a verified token. Keycloak puts realm roles
// in realm_access.roles and client roles under resource_access.
type Claims struct {
	Subject   string   `json:"sub"`
	Issuer    string   `json:"iss"`
	Audience  audience `json:"aud"`
	AuthParty string   `json:"azp"`
	Expiry    int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	NotBefore int64    `json:"nbf"`

	Email    string `json:"email"`
	Name     string `json:"name"`
	Username string `json:"preferred_username"`

	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
	ResourceAccess map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
	Groups []string `json:"groups"`
}

// Roles returns every role the token carries, realm and client alike.
func (c *Claims) Roles(clientID string) []string {
	out := append([]string{}, c.RealmAccess.Roles...)
	if ra, ok := c.ResourceAccess[clientID]; ok {
		out = append(out, ra.Roles...)
	}
	return out
}

// DisplayName is the friendliest name available.
func (c *Claims) DisplayName() string {
	for _, s := range []string{c.Name, c.Username, c.Email, c.Subject} {
		if s != "" {
			return s
		}
	}
	return "пользователь"
}

// audience copes with "aud" being either a string or a list of strings.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) contains(s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

// Verifier validates tokens against a provider's published keys.
type Verifier struct {
	discovery Discovery
	clientID  string
	client    *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// jwksCacheTTL bounds how long a rotated-away key stays trusted.
const jwksCacheTTL = 15 * time.Minute

func NewVerifier(d Discovery, clientID string, client *http.Client) *Verifier {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Verifier{discovery: d, clientID: clientID, client: client, keys: map[string]*rsa.PublicKey{}}
}

// Verify checks the signature, the issuer, the audience and the validity window.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: ожидались три части JWT", ErrBadToken)
	}

	headerJSON, err := decodeSegment(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: заголовок", ErrBadToken)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: заголовок", ErrBadToken)
	}

	hash, ok := hashFor(header.Alg)
	if !ok {
		// "none" and HMAC algorithms are refused outright: accepting them is the
		// classic JWT confusion attack.
		return nil, fmt.Errorf("%w: алгоритм %q не поддерживается", ErrBadToken, header.Alg)
	}

	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := decodeSegment(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: подпись", ErrBadToken)
	}

	digest := sum(hash, parts[0]+"."+parts[1])
	if err := rsa.VerifyPKCS1v15(key, hash, digest, sig); err != nil {
		return nil, fmt.Errorf("%w: подпись не совпала", ErrBadToken)
	}

	payload, err := decodeSegment(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: полезная нагрузка", ErrBadToken)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: полезная нагрузка", ErrBadToken)
	}

	now := time.Now()
	if claims.Expiry > 0 && now.After(time.Unix(claims.Expiry, 0).Add(leeway)) {
		return nil, ErrExpired
	}
	if claims.NotBefore > 0 && now.Add(leeway).Before(time.Unix(claims.NotBefore, 0)) {
		return nil, fmt.Errorf("%w: ещё не действует", ErrBadToken)
	}
	if v.discovery.Issuer != "" && claims.Issuer != v.discovery.Issuer {
		return nil, fmt.Errorf("%w: %s", ErrWrongIssuer, claims.Issuer)
	}
	// Keycloak puts the client id in azp for access tokens and in aud for id
	// tokens, so either is enough.
	if v.clientID != "" && !claims.Audience.contains(v.clientID) && claims.AuthParty != v.clientID {
		return nil, fmt.Errorf("%w: aud=%v azp=%s", ErrWrongAud, []string(claims.Audience), claims.AuthParty)
	}
	return &claims, nil
}

// key returns the signing key, refreshing the JWKS when the id is unknown or
// the cache has gone stale. Keycloak rotates keys, so an unknown kid is a
// reason to refetch rather than to fail.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < jwksCacheTTL
	v.mu.RUnlock()
	if ok && fresh {
		return key, nil
	}
	if err := v.refresh(ctx); err != nil {
		if ok {
			return key, nil // сеть недоступна, но ключ у нас есть
		}
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: kid=%s", ErrUnknownKey, kid)
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.discovery.JWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось получить ключи %s: %w", v.discovery.JWKSURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ключи %s: %s", v.discovery.JWKSURL, resp.Status)
	}

	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("ключи %s: %w", v.discovery.JWKSURL, err)
	}

	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := rsaKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return fmt.Errorf("в %s нет пригодных RSA-ключей", v.discovery.JWKSURL)
	}

	v.mu.Lock()
	v.keys, v.fetchedAt = keys, time.Now()
	v.mu.Unlock()
	return nil
}

func rsaKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := decodeSegment(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := decodeSegment(eStr)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("нулевая экспонента")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

func hashFor(alg string) (crypto.Hash, bool) {
	switch alg {
	case "RS256":
		return crypto.SHA256, true
	case "RS384":
		return crypto.SHA384, true
	case "RS512":
		return crypto.SHA512, true
	}
	return 0, false
}

func sum(h crypto.Hash, s string) []byte {
	switch h {
	case crypto.SHA384:
		d := sha512.Sum384([]byte(s))
		return d[:]
	case crypto.SHA512:
		d := sha512.Sum512([]byte(s))
		return d[:]
	default:
		d := sha256.Sum256([]byte(s))
		return d[:]
	}
}

// decodeSegment reads base64url without padding, as JWT and JWKS use it.
func decodeSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// Discover fetches the provider's configuration document.
func Discover(ctx context.Context, issuer string, client *http.Client) (Discovery, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Discovery{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Discovery{}, fmt.Errorf("не удалось прочитать %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Discovery{}, fmt.Errorf("%s ответил %s", url, resp.Status)
	}
	var d Discovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return Discovery{}, fmt.Errorf("%s: %w", url, err)
	}
	if d.Issuer == "" || d.AuthURL == "" || d.TokenURL == "" || d.JWKSURL == "" {
		return Discovery{}, fmt.Errorf("%s: неполный документ обнаружения", url)
	}
	return d, nil
}
