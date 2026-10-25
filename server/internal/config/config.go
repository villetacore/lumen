// Package config resolves server settings from flags and environment variables.
package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	VaultPath    string
	Token        string
	CORSOrigins  []string
	WatchEnabled bool
	Debounce     time.Duration
	PollInterval time.Duration
	ServeUI      bool
	OpenBrowser  bool

	// PublicURL is how browsers reach this server. Used to build the OIDC
	// redirect and post-logout URLs when they are not set explicitly.
	PublicURL string

	// OIDC ties Lumen into an identity provider — Keycloak in particular.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string // ссылка на секрет, не сам секрет
	OIDCRedirectURL  string
	OIDCRoles        []string
	OIDCScopes       []string
	SessionTTL       time.Duration

	// PassphraseRef is a secrets reference that unlocks an encrypted vault,
	// for example "bitwarden:lumen-vault" or "keepass:db.kdbx#Lumen/vault".
	// PassphraseFile is the older, narrower form of the same thing.
	PassphraseRef  string
	PassphraseFile string

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Verbose      bool
}

// Load parses flags, falling back to LUMEN_* environment variables.
func Load(args []string) (*Config, error) {
	fs := flag.NewFlagSet("lumend", flag.ContinueOnError)
	c := &Config{}
	var origins, roles, scopes string

	fs.StringVar(&c.Addr, "addr", env("LUMEN_ADDR", "127.0.0.1:8787"), "listen address")
	fs.StringVar(&c.VaultPath, "vault", env("LUMEN_VAULT", "./vault"), "path to the vault directory")
	fs.StringVar(&c.Token, "token", env("LUMEN_TOKEN", ""), "bearer token; empty disables auth (local use only)")
	fs.StringVar(&origins, "cors", env("LUMEN_CORS", "http://localhost:5173"), "comma separated allowed origins, or *")
	fs.BoolVar(&c.WatchEnabled, "watch", envBool("LUMEN_WATCH", true), "keep the index in sync with the filesystem")
	fs.DurationVar(&c.Debounce, "debounce", 250*time.Millisecond, "quiet period before a changed file is re-indexed")
	fs.DurationVar(&c.PollInterval, "poll", 2*time.Second, "polling interval used when fsnotify is unavailable")
	fs.BoolVar(&c.ServeUI, "ui", envBool("LUMEN_UI", true), "serve the embedded web client at /")
	fs.BoolVar(&c.OpenBrowser, "open", false, "open the web client in a browser on start")
	fs.StringVar(&c.PassphraseRef, "passphrase-from", env("LUMEN_PASSPHRASE_FROM", ""),
		"where to take the vault passphrase: env:VAR, file:PATH, command:..., bitwarden:ITEM, keepass:DB#ENTRY")
	fs.StringVar(&c.PassphraseFile, "passphrase-file", env("LUMEN_PASSPHRASE_FILE", ""), "file containing the vault passphrase")
	fs.StringVar(&c.PublicURL, "public-url", env("LUMEN_PUBLIC_URL", ""),
		"external address of this server, e.g. https://lumen.example.com")
	fs.StringVar(&c.OIDCIssuer, "oidc-issuer", env("LUMEN_OIDC_ISSUER", ""),
		"OIDC issuer, e.g. https://keycloak.example.com/realms/main")
	fs.StringVar(&c.OIDCClientID, "oidc-client-id", env("LUMEN_OIDC_CLIENT_ID", "lumen"), "OIDC client id")
	fs.StringVar(&c.OIDCClientSecret, "oidc-client-secret", env("LUMEN_OIDC_CLIENT_SECRET", ""),
		"secrets reference for the OIDC client secret; empty for a public client with PKCE")
	fs.StringVar(&c.OIDCRedirectURL, "oidc-redirect-url", env("LUMEN_OIDC_REDIRECT_URL", ""),
		"redirect URI registered in the provider; defaults to <public-url>/auth/callback")
	fs.StringVar(&roles, "oidc-roles", env("LUMEN_OIDC_ROLES", ""),
		"comma separated roles or groups; a user needs at least one")
	fs.StringVar(&scopes, "oidc-scopes", env("LUMEN_OIDC_SCOPES", "openid,profile,email"), "comma separated OIDC scopes")
	fs.DurationVar(&c.SessionTTL, "session-ttl", 12*time.Hour, "how long a browser session lives")
	fs.BoolVar(&c.Verbose, "v", false, "verbose request logging")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	abs, err := filepath.Abs(c.VaultPath)
	if err != nil {
		return nil, err
	}
	c.VaultPath = abs
	// A missing folder is fine: serve creates a new vault there.
	if st, err := os.Stat(abs); (err != nil && !os.IsNotExist(err)) || (err == nil && !st.IsDir()) {
		return nil, fmt.Errorf("vault %q is not a readable directory", abs)
	}

	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}
	c.OIDCRoles = splitCSV(roles)
	c.OIDCScopes = splitCSV(scopes)
	if c.PublicURL == "" {
		c.PublicURL = c.BaseURL()
	}
	c.PublicURL = strings.TrimSuffix(c.PublicURL, "/")
	if c.OIDCRedirectURL == "" {
		c.OIDCRedirectURL = c.PublicURL + "/auth/callback"
	}

	c.ReadTimeout, c.WriteTimeout = 15*time.Second, 30*time.Second
	return c, nil
}

// OIDCEnabled reports whether single sign-on is configured.
func (c *Config) OIDCEnabled() bool { return c.OIDCIssuer != "" && c.OIDCClientID != "" }

// LocalOnly reports whether the server listens on the loopback interface only.
// Some features stay off when the server is reachable from the network without
// any authentication at all.
func (c *Config) LocalOnly() bool {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		host = c.Addr
	}
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// BaseURL is the address to show the user and to open in a browser.
func (c *Config) BaseURL() string {
	host := c.Addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if strings.HasPrefix(host, "0.0.0.0:") {
		host = "127.0.0.1:" + strings.TrimPrefix(host, "0.0.0.0:")
	}
	return "http://" + host
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
