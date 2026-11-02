package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/alexpyslar/lumen/server/internal/notify"
	"github.com/alexpyslar/lumen/server/internal/policy"
)

// notificationView adds what the reader may see of the issue. Rows store only
// keys; the title comes from the issue through the reader's own access, so a
// notification about a hidden or deleted issue is simply left out.
type notificationView struct {
	notify.Notification
	Title   string `json:"title"`
	Project string `json:"project"`
	Status  string `json:"status"`
}

func (a *API) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	p := policy.From(r.Context())
	if p == nil || p.Username == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []notificationView{}, "unread": 0})
		return
	}
	list, err := a.notify.List(r.Context(), p.Username, r.URL.Query().Get("unread") == "true", intParam(r, "limit", 100))
	if err != nil {
		fail(w, err)
		return
	}
	out := []notificationView{}
	unread := 0
	seen := map[string]*notificationView{}
	for _, n := range list {
		v, ok := seen[n.Issue]
		if !ok {
			if is, err := a.issues.Get(r.Context(), n.Issue); err == nil {
				v = &notificationView{Title: is.Title, Project: is.Project, Status: is.Status}
			}
			seen[n.Issue] = v
		}
		if v == nil {
			continue
		}
		item := *v
		item.Notification = n
		out = append(out, item)
		if !n.Read {
			unread++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "unread": unread})
}

type readReq struct {
	IDs []int64 `json:"ids"`
}

// handleReadNotifications marks the given ids read, or all with an empty body.
func (a *API) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	var req readReq
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	p := policy.From(r.Context())
	if p == nil || p.Username == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := a.notify.MarkRead(r.Context(), p.Username, req.IDs); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
