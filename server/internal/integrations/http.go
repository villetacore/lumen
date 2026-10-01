package integrations

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// MaxResponseBytes caps what a connector may download in one request, so a
// misconfigured feed cannot exhaust memory.
const MaxResponseBytes = 16 << 20

// NewHTTPClient builds the only client connectors are allowed to use.
//
// URLs come from the user, so a compromised or careless configuration could
// otherwise point Lumen at 169.254.169.254 or at a service on localhost and
// use the server as a proxy into the private network. The dialer refuses
// non-public addresses unless the integration explicitly opted in, which is
// what self-hosted Vaultwarden, Gitea or a home CalDAV server need.
func NewHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivate {
		// Control runs after DNS resolution and again for every redirect hop,
		// so a public name that resolves to a private IP is still blocked.
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("не удалось разобрать адрес %q", address)
			}
			if !IsPublicIP(ip) {
				return fmt.Errorf("адрес %s закрыт: включите allow_private, если это ваш сервер", ip)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &guardTransport{
			next: &http.Transport{
				DialContext:           dialer.DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				MaxIdleConns:          8,
				IdleConnTimeout:       60 * time.Second,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("слишком много перенаправлений")
			}
			return nil
		},
	}
}

// guardTransport rejects everything that is not plain HTTP(S) before a
// connection is even attempted.
type guardTransport struct{ next http.RoundTripper }

func (g *guardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	switch strings.ToLower(r.URL.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("схема %q не поддерживается", r.URL.Scheme)
	}
	return g.next.RoundTrip(r)
}

// IsPublicIP reports whether an address is routable on the public internet.
func IsPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Carrier-grade NAT (100.64.0.0/10) and the benchmarking range are not
	// private by Go's definition but are not public either.
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 100 && ip4[1]&0xc0 == 64:
			return false
		case ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19):
			return false
		}
	}
	return true
}

// Fetch performs a GET and returns the body, bounded by MaxResponseBytes.
func Fetch(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Lumen/0.1 (+https://github.com/alexpyslar/lumen)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, resp, nil
	}
	if resp.StatusCode >= 400 {
		// A short excerpt of the body usually explains the failure.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, resp, fmt.Errorf("%s ответил %s: %s",
			req.URL.Host, resp.Status, strings.TrimSpace(string(preview)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, resp, err
	}
	if len(body) > MaxResponseBytes {
		return nil, resp, fmt.Errorf("ответ больше %d МБ", MaxResponseBytes>>20)
	}
	return body, resp, nil
}
