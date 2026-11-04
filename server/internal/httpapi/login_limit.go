package httpapi

import (
	"strings"
	"sync"
	"time"
)

const (
	loginWindow   = 5 * time.Minute
	loginMaxFails = 10
)

// loginLimiter slows password guessing per login, in front of Keycloak's own
// brute-force protection. It counts by login, not address: behind the ingress
// every request comes from the same address, and a forwarded header can be forged.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
	now   func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{fails: map[string][]time.Time{}, now: time.Now}
}

func (l *loginLimiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-loginWindow)
	kept := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = kept
	return kept
}

func (l *loginLimiter) allow(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(strings.ToLower(username))) < loginMaxFails
}

func (l *loginLimiter) fail(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Made-up logins must not grow the map without limit.
	if len(l.fails) > 10000 {
		for key := range l.fails {
			l.recent(key)
		}
	}
	key := strings.ToLower(username)
	l.fails[key] = append(l.fails[key], l.now())
}

func (l *loginLimiter) success(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, strings.ToLower(username))
}
