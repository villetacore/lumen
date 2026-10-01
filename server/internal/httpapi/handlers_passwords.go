package httpapi

import (
	"errors"
	"net/http"

	"github.com/alexpyslar/lumen/server/internal/passwords"
)

// guardPasswords refuses to serve password data from a server that is reachable
// over the network with no authentication at all. Everything else on the API is
// the user's own notes; this is someone's password vault.
func (a *API) guardPasswords(w http.ResponseWriter) bool {
	if a.passwords == nil {
		writeError(w, http.StatusNotFound, "not_found", "хранилища паролей выключены", nil)
		return false
	}
	if a.authMode() == "none" && !a.cfg.LocalOnly() {
		writeError(w, http.StatusForbidden, "forbidden",
			"доступ к паролям запрещён: сервер слушает не только localhost и не требует входа. "+
				"Включите --oidc-issuer или --token", nil)
		return false
	}
	return true
}

func passwordsFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, passwords.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, passwords.ErrLocked):
		writeError(w, http.StatusLocked, "locked", err.Error(), nil)
	case errors.Is(err, passwords.ErrNoField), errors.Is(err, passwords.ErrUnknownKind):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
	default:
		writeError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error(), nil)
	}
}

func (a *API) handlePasswordKinds(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	kinds := a.passwords.Kinds()
	writeJSON(w, http.StatusOK, listEnvelope[passwords.KindInfo]{Items: kinds, Total: len(kinds)})
}

func (a *API) handlePasswordStores(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	stores := a.passwords.List()
	writeJSON(w, http.StatusOK, listEnvelope[passwords.Status]{Items: stores, Total: len(stores)})
}

type passwordStoreRequest struct {
	Kind   string            `json:"kind"`
	Name   string            `json:"name"`
	Values map[string]string `json:"values"`
}

func (a *API) handleAddPasswordStore(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	var req passwordStoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	cfg, err := a.passwords.Add(passwords.Config{Kind: req.Kind, Name: req.Name, Values: req.Values})
	if err != nil {
		passwordsFail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cfg)
}

func (a *API) handlePatchPasswordStore(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	var req passwordStoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	cfg, err := a.passwords.Update(r.PathValue("id"), func(c *passwords.Config) {
		if req.Name != "" {
			c.Name = req.Name
		}
		for k, v := range req.Values {
			if c.Values == nil {
				c.Values = map[string]string{}
			}
			c.Values[k] = v
		}
	})
	if err != nil {
		passwordsFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (a *API) handleDeletePasswordStore(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	if err := a.passwords.Delete(r.PathValue("id")); err != nil {
		passwordsFail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type unlockRequest struct {
	Passphrase string `json:"passphrase"`
}

// handleUnlock takes the master password, uses it and forgets it. Only the
// opaque session the backend returns is kept, in memory.
func (a *API) handleUnlockPasswordStore(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	var req unlockRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	noStore(w)

	if err := a.passwords.Unlock(r.Context(), r.PathValue("id"), []byte(req.Passphrase)); err != nil {
		passwordsFail(w, err)
		return
	}
	// Ответ — только состояние, никаких значений.
	for _, s := range a.passwords.List() {
		if s.ID == r.PathValue("id") {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleLockPasswordStore(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	a.passwords.Lock(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleLockAllPasswordStores(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	a.passwords.LockAll()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handlePasswordItems(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	noStore(w)

	items, err := a.passwords.Items(r.Context(), r.PathValue("id"), r.URL.Query().Get("q"))
	if err != nil {
		passwordsFail(w, err)
		return
	}
	if items == nil {
		items = []passwords.Item{}
	}
	writeJSON(w, http.StatusOK, listEnvelope[passwords.Item]{Items: items, Total: len(items)})
}

type revealRequest struct {
	Item  string `json:"item"`
	Field string `json:"field"`
}

// handleReveal is the only endpoint that returns a secret value. It is a POST
// so the item never lands in a URL, and the answer is marked no-store.
func (a *API) handleRevealPassword(w http.ResponseWriter, r *http.Request) {
	if !a.guardPasswords(w) {
		return
	}
	var req revealRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	noStore(w)

	value, err := a.passwords.Reveal(r.Context(), r.PathValue("id"), req.Item, req.Field)
	if err != nil {
		passwordsFail(w, err)
		return
	}
	field := req.Field
	if field == "" {
		field = "password"
	}
	writeJSON(w, http.StatusOK, map[string]string{"field": field, "value": value})
}

// noStore keeps secrets out of every cache between here and the browser.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
}
