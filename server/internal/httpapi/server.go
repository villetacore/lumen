// Package httpapi exposes the service layer over HTTP. Handlers do parsing and
// shaping only; all rules live in internal/service.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/alexpyslar/lumen/server/internal/auth"
	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/passwords"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
	"github.com/alexpyslar/lumen/server/internal/webui"
)

// API bundles everything the handlers need.
type API struct {
	cfg   *config.Config
	store index.Store
	vault *vault.Vault
	notes *service.Notes
	tasks *service.Tasks
	views *service.Views
	ix    *service.Indexer
	bus   *service.Bus
	log   *slog.Logger

	integrations *service.Integrations
	oidc         *auth.Provider
	passwords    *passwords.Manager
}

func New(
	cfg *config.Config, st index.Store, v *vault.Vault, ix *service.Indexer,
	bus *service.Bus, integrations *service.Integrations, oidc *auth.Provider,
	pw *passwords.Manager, log *slog.Logger,
) *API {
	return &API{
		cfg: cfg, store: st, vault: v, ix: ix, bus: bus, log: log,
		notes:        service.NewNotes(v, st, ix, bus, log),
		tasks:        service.NewTasks(v, st, ix, bus, log),
		views:        service.NewViews(st),
		integrations: integrations,
		oidc:         oidc,
		passwords:    pw,
	}
}

// Handler builds the router with its middleware chain.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /api/v1/me", a.handleMe)
	mux.HandleFunc("GET /auth/login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("GET /auth/logout", a.handleLogout)
	mux.HandleFunc("POST /auth/logout", a.handleLogout)

	mux.HandleFunc("GET /api/v1/stats", a.handleStats)
	mux.HandleFunc("POST /api/v1/reindex", a.handleReindex)
	mux.HandleFunc("GET /api/v1/events", a.handleEvents)

	mux.HandleFunc("GET /api/v1/notes", a.handleListNotes)
	mux.HandleFunc("POST /api/v1/notes", a.handleCreateNote)
	mux.HandleFunc("GET /api/v1/notes/{id...}", a.handleGetNote)
	mux.HandleFunc("PUT /api/v1/notes/{id...}", a.handlePutNote)
	mux.HandleFunc("PATCH /api/v1/notes/{id...}", a.handlePatchNote)
	mux.HandleFunc("DELETE /api/v1/notes/{id...}", a.handleDeleteNote)
	mux.HandleFunc("GET /api/v1/backlinks/{id...}", a.handleBacklinks)

	mux.HandleFunc("GET /api/v1/tasks", a.handleListTasks)
	mux.HandleFunc("POST /api/v1/tasks", a.handleCreateTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", a.handleGetTask)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}", a.handlePatchTask)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", a.handleDeleteTask)

	mux.HandleFunc("GET /api/v1/calendar", a.handleCalendar)
	mux.HandleFunc("POST /api/v1/calendar/move", a.handleCalendarMove)
	mux.HandleFunc("GET /api/v1/search", a.handleSearch)
	mux.HandleFunc("GET /api/v1/tags", a.handleTags)
	mux.HandleFunc("GET /api/v1/daily/{date}", a.handleDaily)

	mux.HandleFunc("GET /api/v1/integrations", a.handleListIntegrations)
	mux.HandleFunc("GET /api/v1/integrations/kinds", a.handleIntegrationKinds)
	mux.HandleFunc("POST /api/v1/integrations", a.handleCreateIntegration)
	mux.HandleFunc("PATCH /api/v1/integrations/{id}", a.handlePatchIntegration)
	mux.HandleFunc("DELETE /api/v1/integrations/{id}", a.handleDeleteIntegration)
	mux.HandleFunc("POST /api/v1/integrations/{id}/sync", a.handleSyncIntegration)

	mux.HandleFunc("GET /api/v1/passwords/kinds", a.handlePasswordKinds)
	mux.HandleFunc("GET /api/v1/passwords/stores", a.handlePasswordStores)
	mux.HandleFunc("POST /api/v1/passwords/stores", a.handleAddPasswordStore)
	mux.HandleFunc("PATCH /api/v1/passwords/stores/{id}", a.handlePatchPasswordStore)
	mux.HandleFunc("DELETE /api/v1/passwords/stores/{id}", a.handleDeletePasswordStore)
	mux.HandleFunc("POST /api/v1/passwords/stores/{id}/unlock", a.handleUnlockPasswordStore)
	mux.HandleFunc("POST /api/v1/passwords/stores/{id}/lock", a.handleLockPasswordStore)
	mux.HandleFunc("POST /api/v1/passwords/lock", a.handleLockAllPasswordStores)
	mux.HandleFunc("GET /api/v1/passwords/stores/{id}/items", a.handlePasswordItems)
	mux.HandleFunc("POST /api/v1/passwords/stores/{id}/reveal", a.handleRevealPassword)

	// The compiled web client lives inside the binary, so `lumend` alone is the
	// whole application. "GET /" is the least specific pattern, so every route
	// above still wins.
	if a.cfg.ServeUI {
		mux.Handle("GET /", webui.Handler())
	}

	var h http.Handler = mux
	h = a.withAuth(h)
	h = a.withCORS(h)
	h = a.withRecover(h)
	h = a.withLogging(h)
	return h
}
