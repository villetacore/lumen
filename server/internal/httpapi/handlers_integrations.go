package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/integrations"
	"github.com/alexpyslar/lumen/server/internal/service"
)

// integrationDTO renders durations as "15m" instead of nanoseconds, and never
// carries a resolved secret: Values holds references such as
// "bitwarden:github-token".
type integrationDTO struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Title     string            `json:"title"`
	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	Interval  string            `json:"interval"`
	Folder    string            `json:"folder,omitempty"`
	Values    map[string]string `json:"values"`
	LastSync  *time.Time        `json:"last_sync,omitempty"`
	NextSync  *time.Time        `json:"next_sync,omitempty"`
	LastError string            `json:"last_error,omitempty"`
	LastItems int               `json:"last_items"`
	Running   bool              `json:"running"`
}

func toDTO(s service.Status) integrationDTO {
	dto := integrationDTO{
		ID: s.ID, Kind: s.Kind, Title: s.Title, Name: s.Name,
		Enabled: s.Enabled, Interval: s.Interval.String(), Folder: s.Folder,
		Values: s.Values, LastError: s.LastError, LastItems: s.LastItems,
		Running: s.Running,
	}
	if dto.Values == nil {
		dto.Values = map[string]string{}
	}
	if !s.LastSync.IsZero() {
		t := s.LastSync
		dto.LastSync = &t
	}
	if !s.NextSync.IsZero() {
		t := s.NextSync
		dto.NextSync = &t
	}
	return dto
}

func (a *API) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	statuses := a.integrations.List()
	items := make([]integrationDTO, 0, len(statuses))
	for _, s := range statuses {
		items = append(items, toDTO(s))
	}
	writeJSON(w, http.StatusOK, listEnvelope[integrationDTO]{Items: items, Total: len(items)})
}

func (a *API) handleIntegrationKinds(w http.ResponseWriter, r *http.Request) {
	kinds := a.integrations.Kinds()
	writeJSON(w, http.StatusOK, listEnvelope[service.Kind]{Items: kinds, Total: len(kinds)})
}

type integrationRequest struct {
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Enabled  *bool             `json:"enabled"`
	Interval string            `json:"interval"`
	Folder   string            `json:"folder"`
	Values   map[string]string `json:"values"`
}

func (a *API) handleCreateIntegration(w http.ResponseWriter, r *http.Request) {
	var req integrationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	cfg := integrations.Config{
		Kind: req.Kind, Name: req.Name, Enabled: true,
		Folder: req.Folder, Values: req.Values,
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Interval != "" {
		d, err := time.ParseDuration(req.Interval)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", `interval выглядит как "15m"`, nil)
			return
		}
		cfg.Interval = d
	}

	created, err := a.integrations.Add(cfg)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(service.Status{Config: created, Title: created.Kind}))
}

func (a *API) handlePatchIntegration(w http.ResponseWriter, r *http.Request) {
	var req integrationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	var interval time.Duration
	if req.Interval != "" {
		d, err := time.ParseDuration(req.Interval)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", `interval выглядит как "15m"`, nil)
			return
		}
		interval = d
	}

	updated, err := a.integrations.Update(r.PathValue("id"), func(c *integrations.Config) {
		if req.Name != "" {
			c.Name = req.Name
		}
		if req.Enabled != nil {
			c.Enabled = *req.Enabled
		}
		if interval > 0 {
			c.Interval = interval
		}
		if req.Folder != "" {
			c.Folder = req.Folder
		}
		// Values are merged, so the UI can send only the field it changed.
		for k, v := range req.Values {
			if c.Values == nil {
				c.Values = map[string]string{}
			}
			c.Values[k] = v
		}
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(service.Status{Config: updated, Title: updated.Kind}))
}

func (a *API) handleDeleteIntegration(w http.ResponseWriter, r *http.Request) {
	if err := a.integrations.Delete(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleSyncIntegration(w http.ResponseWriter, r *http.Request) {
	result, err := a.integrations.SyncNow(r.Context(), r.PathValue("id"))
	if errors.Is(err, index.ErrNotFound) {
		fail(w, err)
		return
	}
	// A failed sync is still a useful answer: the result carries the message,
	// and the UI shows it next to the integration.
	writeJSON(w, http.StatusOK, result)
}
