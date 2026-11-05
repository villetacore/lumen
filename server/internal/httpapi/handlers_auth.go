package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/villetacore/lumen/server/internal/auth"
	"github.com/villetacore/lumen/server/internal/policy"
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
	// Role is the workspace role Lumen applies: viewer, member or admin.
	Role     string `json:"role,omitempty"`
	LoginURL string `json:"login_url,omitempty"`
	// PasswordLogin says Lumen's own sign-in form is available.
	PasswordLogin bool   `json:"password_login,omitempty"`
	LogoutURL     string `json:"logout_url,omitempty"`
	// Version of the server; only for signed-in users, /api/v1/me is public.
	Version string `json:"version,omitempty"`
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
		id.PasswordLogin = a.cfg.OIDCPasswordLogin
		id.LogoutURL = "/auth/logout"
		if claims, ok := r.Context().Value(claimsKey).(*auth.Claims); ok && claims != nil {
			id.Authenticated = true
			id.Subject = claims.Subject
			id.Name = claims.DisplayName()
			id.Email = claims.Email
			id.Username = claims.Username
			id.Roles = claims.Roles(a.oidc.Config().ClientID)
		} else if a.tokenMatches(r) {
			// Запасной статический токен: API его пускает (withAuth), значит и
			// веб-клиент не должен отправлять такого пользователя на вход.
			id.Authenticated = true
			id.Name = "доступ по токену"
		}
	}
	if p := policy.From(r.Context()); p != nil && id.Authenticated {
		id.Role = p.Role.String()
	}
	if id.Authenticated {
		id.Version = a.cfg.Version
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

// handlePasswordLogin is Lumen's own sign-in form: Keycloak checks the password,
// and the browser stays here. The cookie is what a form on another site would be
// after, so a cross-site request is refused before anything else happens.
func (a *API) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil || !a.cfg.OIDCPasswordLogin {
		writeError(w, http.StatusNotFound, "not_found", "вход по паролю не настроен", nil)
		return
	}
	if err := a.loginOrigin.Check(r); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "запрос с другого сайта", nil)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		OTP      string `json:"otp"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "неверный запрос", nil)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "нужны логин и пароль", nil)
		return
	}
	if !a.logins.allow(req.Username) {
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "слишком много попыток, подождите несколько минут", nil)
		return
	}

	session, err := a.oidc.PasswordLogin(r.Context(), req.Username, req.Password, req.OTP)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrBadCredentials):
		a.logins.fail(req.Username)
		a.log.Info("неудачный вход", "user", req.Username)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный логин или пароль", nil)
		return
	case errors.Is(err, auth.ErrForbidden):
		a.log.Warn("вход без нужной группы", "user", req.Username)
		writeError(w, http.StatusForbidden, "forbidden", "Вход выполнен, но у этой учётной записи нет доступа к Lumen. Попросите администратора добавить вас в нужную группу.", nil)
		return
	case errors.Is(err, auth.ErrPasswordLoginDisabled):
		a.log.Warn("вход по паролю выключен в Keycloak: включите Direct access grants у клиента")
		writeError(w, http.StatusConflict, "password_login_disabled", "Вход по паролю пока не включён на сервере. Используйте вход через Keycloak.", nil)
		return
	case errors.Is(err, auth.ErrAccountSetup):
		writeError(w, http.StatusConflict, "password_login_disabled", err.Error(), nil)
		return
	default:
		a.log.Warn("вход по паролю не выполнен", "err", err)
		writeError(w, http.StatusBadGateway, "provider_error", "Сервер входа не отвечает, попробуйте позже", nil)
		return
	}
	a.logins.success(req.Username)
	http.SetCookie(w, a.oidc.Cookie(session.ID, session.Expiry))
	a.log.Info("вход выполнен", "user", session.Claims.DisplayName())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleCallback(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		writeError(w, http.StatusNotFound, "not_found", "single sign-on не настроен", nil)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		a.log.Warn("вход отклонён провайдером", "error", e, "description", q.Get("error_description"))
		http.Redirect(w, r, "/?auth_error=failed", http.StatusFound)
		return
	}

	session, returnTo, err := a.oidc.Exchange(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		a.log.Warn("не удалось завершить вход", "err", err)
		// The web client explains this; a person should not land on raw JSON.
		reason := "failed"
		if errors.Is(err, auth.ErrForbidden) {
			reason = "forbidden"
		}
		http.Redirect(w, r, "/?auth_error="+reason, http.StatusFound)
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
