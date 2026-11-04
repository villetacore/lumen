package httpapi

import (
	"net/http"

	"github.com/villetacore/lumen/server/internal/people"
	"github.com/villetacore/lumen/server/internal/policy"
)

// handleListUsers serves the directory for pickers and the team page. Every
// workspace reader may see names and groups; e-mail is for admins.
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if err := a.policy.CheckWorkspace(r.Context(), policy.RoleViewer); err != nil {
		fail(w, err)
		return
	}
	list := []people.User{}
	if a.people != nil {
		var err error
		if list, err = a.people.Users(r.Context(), r.URL.Query().Get("q")); err != nil {
			fail(w, err)
			return
		}
	}
	if a.policy.CheckWorkspace(r.Context(), policy.RoleAdmin) != nil {
		for i := range list {
			list[i].Email = ""
		}
	}
	writeJSON(w, http.StatusOK, listEnvelope[people.User]{Items: list, Total: len(list)})
}

func (a *API) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if err := a.policy.CheckWorkspace(r.Context(), policy.RoleViewer); err != nil {
		fail(w, err)
		return
	}
	list := []people.Group{}
	if a.people != nil {
		var err error
		if list, err = a.people.Groups(r.Context()); err != nil {
			fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, listEnvelope[people.Group]{Items: list, Total: len(list)})
}
