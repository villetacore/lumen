package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/webui"
)

func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	st, err := a.store.Stats(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	st.Encrypted = a.vault.Encrypted()
	st.UIEmbedded = webui.Available()
	st.AuthRequired = a.cfg.Token != ""
	writeJSON(w, http.StatusOK, st)
}

func (a *API) handleReindex(w http.ResponseWriter, r *http.Request) {
	n, err := a.ix.ReindexAll(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": n})
}

func (a *API) handleCalendar(w http.ResponseWriter, r *http.Request) {
	from := dateParam(r, "from")
	to := dateParam(r, "to")
	if from.IsZero() {
		from = domain.Today()
	}
	if to.IsZero() {
		to = domain.Date{T: from.Day().AddDate(0, 1, 0)}
	}
	ag, err := a.views.Calendar(r.Context(), from.Day(), to.Day().AddDate(0, 0, 1))
	if err != nil {
		fail(w, err)
		return
	}
	if ag.Events == nil {
		ag.Events = []domain.Event{}
	}
	writeJSON(w, http.StatusOK, ag)
}

type calendarMoveReq struct {
	Ref   string `json:"ref"`   // "task:<id>"
	Date  string `json:"date"`  // YYYY-MM-DD
	Field string `json:"field"` // scheduled | due
}

// handleCalendarMove backs drag-and-drop on the calendar.
func (a *API) handleCalendarMove(w http.ResponseWriter, r *http.Request) {
	var req calendarMoveReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	kind, id, ok := strings.Cut(req.Ref, ":")
	if !ok || kind != "task" {
		writeError(w, http.StatusBadRequest, "bad_request", `ref must look like "task:<id>"`, nil)
		return
	}
	field := req.Field
	if field == "" {
		field = "scheduled"
	}
	up := service.TaskUpdate{}
	switch field {
	case "scheduled":
		up.Scheduled = &req.Date
	case "due":
		up.Due = &req.Date
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "field must be scheduled or due", nil)
		return
	}
	t, err := a.tasks.Update(r.Context(), id, up)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	hits, err := a.store.Search(r.Context(), r.URL.Query().Get("q"), intParam(r, "limit", 30))
	if err != nil {
		fail(w, err)
		return
	}
	if hits == nil {
		hits = []domain.SearchHit{}
	}
	writeJSON(w, http.StatusOK, listEnvelope[domain.SearchHit]{Items: hits, Total: len(hits)})
}

func (a *API) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := a.store.Tags(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if tags == nil {
		tags = []domain.TagCount{}
	}
	writeJSON(w, http.StatusOK, listEnvelope[domain.TagCount]{Items: tags, Total: len(tags)})
}

// handleEvents streams change notifications as Server-Sent Events. SSE keeps the
// MVP dependency-free; the WebSocket endpoint arrives with the desktop sync client.
func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported", nil)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := a.bus.Subscribe()
	defer cancel()

	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
			flusher.Flush()
		}
	}
}
