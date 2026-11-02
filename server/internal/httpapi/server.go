// Package httpapi exposes the service layer over HTTP. Handlers do parsing and
// shaping only; all rules live in internal/service.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/alexpyslar/lumen/server/internal/auth"
	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/history"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/notify"
	"github.com/alexpyslar/lumen/server/internal/passwords"
	"github.com/alexpyslar/lumen/server/internal/people"
	"github.com/alexpyslar/lumen/server/internal/policy"
	"github.com/alexpyslar/lumen/server/internal/proposals"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/state"
	"github.com/alexpyslar/lumen/server/internal/vault"
	"github.com/alexpyslar/lumen/server/internal/webui"
)

// API bundles everything the handlers need.
type API struct {
	cfg    *config.Config
	store  index.Store
	vault  *vault.Vault
	raw    index.Store    // unfiltered: indexing and background work only
	policy *policy.Policy // every request decision (ADR-0012)
	notes  *service.Notes
	tasks  *service.Tasks
	issues *service.Issues
	views  *service.Views
	ix     *service.Indexer
	bus    *service.Bus
	log    *slog.Logger

	state        *state.Store
	people       *people.Directory // nil without a state store
	notify       *notify.Store     // nil without a state store
	history      *history.Journal  // nil: no issue history
	proposals    *proposals.Store  // nil without a state store
	integrations *service.Integrations
	oidc         *auth.Provider
	passwords    *passwords.Manager
}

func New(
	cfg *config.Config, st index.Store, v *vault.Vault, ix *service.Indexer,
	bus *service.Bus, integrations *service.Integrations, oidc *auth.Provider,
	pw *passwords.Manager, log *slog.Logger,
) *API {
	a := &API{
		cfg: cfg, raw: st, vault: v, ix: ix, bus: bus, log: log,
		integrations: integrations,
		oidc:         oidc,
		passwords:    pw,
	}
	a.wire(policy.New(st, nil))
	return a
}

// wire builds the request-side services on the policy-scoped index, so every
// read and write of a request goes through one set of rules.
func (a *API) wire(p *policy.Policy) {
	a.policy = p
	a.store = policy.Scope(a.raw, p)
	a.notes = service.NewNotes(a.vault, a.store, a.ix, a.bus, a.log).WithPolicy(p)
	a.tasks = service.NewTasks(a.vault, a.store, a.ix, a.bus, a.log).WithPolicy(p)
	a.issues = service.NewIssues(a.vault, a.store, a.ix, a.bus, a.log).WithPolicy(p).WithNotify(a.notify)
	a.views = service.NewViews(a.store)
}

// WithHistory serves the issue journal the indexer writes (LUM-068).
func (a *API) WithHistory(j *history.Journal) *API {
	a.history = j
	return a
}

// WithState attaches the operational store (ADR-0011); project member lists
// live there.
func (a *API) WithState(st *state.Store) *API {
	a.state = st
	a.people = people.New(st.DB())
	a.proposals = proposals.New(st.DB(), st)
	// The SSE nudge names no issue: the client refetches the list, which is
	// filtered by what the recipient may see.
	a.notify = notify.New(st.DB(), func(recipient, _ string) {
		a.bus.Publish(service.Event{Type: "notification", User: recipient})
	})
	a.wire(policy.New(a.raw, policy.NewMembers(st.DB())))
	return a
}

// Handler builds the router with its middleware chain.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if a.state != nil {
			if err := a.state.Ping(r.Context()); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "state": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /api/v1/me", a.handleMe)
	mux.HandleFunc("GET /auth/login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("GET /auth/logout", a.handleLogout)
	mux.HandleFunc("POST /auth/logout", a.handleLogout)

	// MCP (ADR-0013). Explicit methods keep these routes apart from the UI's "GET /".
	mcpHandler := a.mcpHandler()
	mux.Handle("POST /mcp", mcpHandler)
	mux.Handle("GET /mcp", mcpHandler)
	mux.Handle("DELETE /mcp", mcpHandler)

	mux.HandleFunc("GET /api/v1/proposals", a.handleListProposals)
	mux.HandleFunc("POST /api/v1/proposals/{id}/approve", a.handleDecideProposal(true))
	mux.HandleFunc("POST /api/v1/proposals/{id}/reject", a.handleDecideProposal(false))

	mux.HandleFunc("GET /api/v1/stats", a.handleStats)
	mux.HandleFunc("POST /api/v1/reindex", a.adminOnly(a.handleReindex))
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

	mux.HandleFunc("GET /api/v1/tracker/projects", a.handleListTrackerProjects)
	mux.HandleFunc("POST /api/v1/tracker/projects", a.handleCreateTrackerProject)
	mux.HandleFunc("GET /api/v1/tracker/projects/{key}", a.handleGetTrackerProject)
	mux.HandleFunc("GET /api/v1/tracker/projects/{key}/sprints", a.handleListSprints)
	mux.HandleFunc("POST /api/v1/tracker/projects/{key}/sprints", a.handleCreateSprint)
	mux.HandleFunc("GET /api/v1/sprints/{id}", a.handleGetSprint)
	mux.HandleFunc("PATCH /api/v1/sprints/{id}", a.handlePatchSprint)
	mux.HandleFunc("POST /api/v1/sprints/{id}/start", a.handleStartSprint)
	mux.HandleFunc("POST /api/v1/sprints/{id}/complete", a.handleCompleteSprint)
	mux.HandleFunc("GET /api/v1/tracker/projects/{key}/members", a.handleListMembers)
	mux.HandleFunc("PUT /api/v1/tracker/projects/{key}/members", a.handleSetMember)
	mux.HandleFunc("DELETE /api/v1/tracker/projects/{key}/members/{kind}/{name}", a.handleRemoveMember)
	mux.HandleFunc("GET /api/v1/notifications", a.handleListNotifications)
	mux.HandleFunc("POST /api/v1/notifications/read", a.handleReadNotifications)
	mux.HandleFunc("GET /api/v1/users", a.handleListUsers)
	mux.HandleFunc("GET /api/v1/groups", a.handleListGroups)
	mux.HandleFunc("GET /api/v1/issues", a.handleListIssues)
	mux.HandleFunc("POST /api/v1/issues", a.handleCreateIssue)
	mux.HandleFunc("GET /api/v1/issues/{key}", a.handleGetIssue)
	mux.HandleFunc("PATCH /api/v1/issues/{key}", a.handlePatchIssue)
	mux.HandleFunc("DELETE /api/v1/issues/{key}", a.handleDeleteIssue)
	mux.HandleFunc("POST /api/v1/issues/{key}/move", a.handleMoveIssue)
	mux.HandleFunc("POST /api/v1/issues/{key}/comments", a.handleAddComment)
	mux.HandleFunc("GET /api/v1/issues/{key}/history", a.handleIssueHistory)

	mux.HandleFunc("GET /api/v1/calendar", a.handleCalendar)
	mux.HandleFunc("POST /api/v1/calendar/move", a.handleCalendarMove)
	mux.HandleFunc("GET /api/v1/search", a.handleSearch)
	mux.HandleFunc("GET /api/v1/tags", a.handleTags)
	mux.HandleFunc("GET /api/v1/daily/{date}", a.handleDaily)

	mux.HandleFunc("GET /api/v1/integrations", a.adminOnly(a.handleListIntegrations))
	mux.HandleFunc("GET /api/v1/integrations/kinds", a.adminOnly(a.handleIntegrationKinds))
	mux.HandleFunc("POST /api/v1/integrations", a.adminOnly(a.handleCreateIntegration))
	mux.HandleFunc("PATCH /api/v1/integrations/{id}", a.adminOnly(a.handlePatchIntegration))
	mux.HandleFunc("DELETE /api/v1/integrations/{id}", a.adminOnly(a.handleDeleteIntegration))
	mux.HandleFunc("POST /api/v1/integrations/{id}/sync", a.adminOnly(a.handleSyncIntegration))

	mux.HandleFunc("GET /api/v1/passwords/kinds", a.adminOnly(a.handlePasswordKinds))
	mux.HandleFunc("GET /api/v1/passwords/stores", a.adminOnly(a.handlePasswordStores))
	mux.HandleFunc("POST /api/v1/passwords/stores", a.adminOnly(a.handleAddPasswordStore))
	mux.HandleFunc("PATCH /api/v1/passwords/stores/{id}", a.adminOnly(a.handlePatchPasswordStore))
	mux.HandleFunc("DELETE /api/v1/passwords/stores/{id}", a.adminOnly(a.handleDeletePasswordStore))
	mux.HandleFunc("POST /api/v1/passwords/stores/{id}/unlock", a.adminOnly(a.handleUnlockPasswordStore))
	mux.HandleFunc("POST /api/v1/passwords/stores/{id}/lock", a.adminOnly(a.handleLockPasswordStore))
	mux.HandleFunc("POST /api/v1/passwords/lock", a.adminOnly(a.handleLockAllPasswordStores))
	mux.HandleFunc("GET /api/v1/passwords/stores/{id}/items", a.adminOnly(a.handlePasswordItems))
	mux.HandleFunc("POST /api/v1/passwords/stores/{id}/reveal", a.adminOnly(a.handleRevealPassword))

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
