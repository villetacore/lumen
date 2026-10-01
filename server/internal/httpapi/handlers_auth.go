package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/auth"
)

// contextKey keeps request-scoped values from colliding with other packages.
type contextKey string

const claimsKey contextKey = "lumen.claims"

// identity is what the UI shows about the signed-in user.
type identity struct {
	Authenticated bool     `json:"authenticated"`
	Mode          string   `json:"mode"` // none | token | oidc
	Subject       string   `json:"subject,omitempty"`
	Name          string   `json:"name,omitempty"`
	Email         string   `json:"email,omitempty"`
	Username      string   `json:"username,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	LoginURL      string   `json:"login_url,omitempty"`
	LogoutURL     string   `json:"logout_url,omitempty"`
}

func (a *API) authMode() string {
	switch {
	case a.oidc != nil:
		return "oidc"
	case a.cfg.Token != "":
		return "token"
	default:
		return "none"
	}
}

// handleMe describes the current session. It is deliberately readable without
// authentication so the web client can find out how to log in.
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	id := identity{Mode: a.authMode()}

	switch id.Mode {
	case "none":
		id.Authenticated = true
		id.Name = "локальный режим"
	case "token":
		id.Authenticated = a.tokenMatches(r)
		id.Name = "доступ по токену"
	case "oidc":
		id.LoginURL = "/auth/login"
		id.LogoutURL = "/auth/logout"
		if claims, ok := r.Context().Value(claimsKey).(*auth.Claims); ok && claims != nil {
			id.Authenticated = true
			id.Subject = claims.Subject
			id.Name = claims.DisplayName()
			id.Email = claims.Email
			id.Username = claims.Username
			id.Roles = claims.Roles(a.oidc.Config().ClientID)
		}
	}
	writeJSON(w, http.StatusOK, id)
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		writeError(w, http.StatusNotFound, "not_found", "single sign-on не настроен", nil)
		return
	}
	// Куда вернуть после входа. Принимаем только относительный путь, иначе это
	// открытый редирект на чужой сайт.
	returnTo := r.URL.Query().Get("return_to")
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		returnTo = "/"
	}

	url, err := a.oidc.AuthCodeURL(returnTo)
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (a *API) handleCallback(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		writeError(w, http.StatusNotFound, "not_found", "single sign-on не настроен", nil)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		a.log.Warn("вход отклонён провайдером", "error", e, "description", q.Get("error_description"))
		writeError(w, http.StatusUnauthorized, "unauthorized", e+": "+q.Get("error_description"), nil)
		return
	}

	session, returnTo, err := a.oidc.Exchange(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		a.log.Warn("не удалось завершить вход", "err", err)
		status := http.StatusUnauthorized
		if errors.Is(err, auth.ErrForbidden) {
			status = http.StatusForbidden
		}
		writeError(w, status, "unauthorized", err.Error(), nil)
		return
	}

	http.SetCookie(w, a.oidc.Cookie(session.ID, session.Expiry))
	a.log.Info("вход выполнен", "user", session.Claims.DisplayName())
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		writeError(w, http.StatusNotFound, "not_found", "single sign-on не настроен", nil)
		return
	}
	var id string
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		id = c.Value
	}
	// Сбрасываем cookie независимо от того, была ли сессия жива.
	expired := a.oidc.Cookie("", time.Unix(0, 0))
	expired.MaxAge = -1
	http.SetCookie(w, expired)

	http.Redirect(w, r, a.oidc.EndSession(id, a.cfg.PublicURL+"/"), http.StatusFound)
}
