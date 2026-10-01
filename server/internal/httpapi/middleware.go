package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/auth"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush keeps Server-Sent Events working through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *API) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if !a.cfg.Verbose && sw.status < 400 && r.URL.Path == "/healthz" {
			return
		}
		a.log.Info("http",
			"method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "dur", time.Since(start))
	})
}

func (a *API) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.log.Error("panic", "path", r.URL.Path, "err", rec)
				writeError(w, http.StatusInternalServerError, "internal", "internal error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *API) withCORS(next http.Handler) http.Handler {
	allowAll := false
	allowed := map[string]bool{}
	for _, o := range a.cfg.CORSOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || allowed[origin]) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearer pulls the token out of the request. EventSource cannot set headers,
// so SSE is allowed to pass it as a query parameter.
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return r.URL.Query().Get("token")
}

func (a *API) tokenMatches(r *http.Request) bool {
	if a.cfg.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(a.cfg.Token)) == 1
}

// isPublicPath lists what stays reachable without credentials: the UI shell,
// health, and the endpoints a client needs to discover how to log in.
func isPublicPath(p string) bool {
	if !strings.HasPrefix(p, "/api/") {
		return true // статика веб-клиента и точки входа /auth/*
	}
	switch p {
	case "/api/v1/me":
		return true
	}
	return false
}

// identify resolves who is making the request, without enforcing anything.
// The claims land in the context so handlers and /api/v1/me can use them.
func (a *API) identify(r *http.Request) (*http.Request, bool) {
	if a.oidc == nil {
		return r, false
	}
	if c, err := r.Cookie(auth.SessionCookie); err == nil && c.Value != "" {
		if session, ok := a.oidc.Session(c.Value); ok {
			return r.WithContext(context.WithValue(r.Context(), claimsKey, session.Claims)), true
		}
	}
	// Токен Keycloak напрямую — так ходят десктоп, скрипты и соседние сервисы.
	if raw := bearer(r); raw != "" && strings.Count(raw, ".") == 2 {
		claims, err := a.oidc.Verify(r.Context(), raw)
		if err == nil {
			return r.WithContext(context.WithValue(r.Context(), claimsKey, claims)), true
		}
		a.log.Debug("токен отклонён", "err", err)
	}
	return r, false
}

func (a *API) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, authenticated := a.identify(r)

		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		switch {
		case a.oidc != nil:
			// При включённом SSO статический токен остаётся как запасной путь
			// для автоматизации, если он задан.
			if authenticated || a.tokenMatches(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "unauthorized",
				"нужен вход через single sign-on", map[string]any{"login_url": "/auth/login"})

		case a.cfg.Token != "":
			if a.tokenMatches(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token", nil)

		default:
			next.ServeHTTP(w, r) // локальный режим без авторизации
		}
	})
}
