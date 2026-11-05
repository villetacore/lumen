package httpapi

import (
	"net/http"

	"github.com/villetacore/lumen/server/internal/automation"
)

// Automations live in the operational store (ADR-0020), so an installation without
// one has no rules to offer.
func (a *API) requireAutomations(w http.ResponseWriter) bool {
	if a.automations == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "automations need the operational store", nil)
		return false
	}
	return true
}

func (a *API) handleListAutomations(w http.ResponseWriter, r *http.Request) {
	if !a.requireAutomations(w) {
		return
	}
	rules, canEdit, err := a.automations.List(r.Context(), r.PathValue("key"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules, "total": len(rules), "can_edit": canEdit})
}

func (a *API) handleCreateAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.requireAutomations(w) {
		return
	}
	var d automation.Definition
	if err := decodeJSON(r, &d); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	rule, err := a.automations.Create(r.Context(), r.PathValue("key"), d)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

type patchAutomationReq struct {
	Definition *automation.Definition `json:"definition"`
	Enabled    *bool                  `json:"enabled"`
	Revision   int64                  `json:"revision"`
}

func (a *API) handlePatchAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.requireAutomations(w) {
		return
	}
	var req patchAutomationReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	if req.Revision == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "revision is required", nil)
		return
	}
	rule, err := a.automations.Update(r.Context(), r.PathValue("id"), req.Definition, req.Enabled, req.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (a *API) handleDeleteAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.requireAutomations(w) {
		return
	}
	if err := a.automations.Delete(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleRunAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.requireAutomations(w) {
		return
	}
	if err := a.automations.RunNow(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) handleAutomationRuns(w http.ResponseWriter, r *http.Request) {
	if !a.requireAutomations(w) {
		return
	}
	runs, err := a.automations.Runs(r.Context(), r.PathValue("key"), intParam(r, "limit", 100))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs, "total": len(runs)})
}
