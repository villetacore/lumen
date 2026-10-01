package httpapi

import (
	"net/http"
	"strings"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/service"
)

// noteSummary is the list representation: everything but the body.
type noteSummary struct {
	ID       string          `json:"id"`
	Path     string          `json:"path"`
	Title    string          `json:"title"`
	Type     domain.NoteType `json:"type"`
	Tags     []string        `json:"tags"`
	Tasks    int             `json:"tasks"`
	Open     int             `json:"open_tasks"`
	Revision int64           `json:"revision"`
	Updated  string          `json:"updated,omitempty"`
}

func summarize(n *domain.Note) noteSummary {
	open := 0
	for _, t := range n.Tasks {
		if t.Status.Open() {
			open++
		}
	}
	s := noteSummary{
		ID: n.ID, Path: n.Path, Title: n.Title, Type: n.Type, Tags: n.Tags,
		Tasks: len(n.Tasks), Open: open, Revision: n.Revision,
	}
	if !n.Updated.IsZero() {
		s.Updated = n.Updated.Format("2006-01-02T15:04:05Z07:00")
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return s
}

func (a *API) handleListNotes(w http.ResponseWriter, r *http.Request) {
	q := domain.NoteQuery{
		Type:       domain.NoteType(r.URL.Query().Get("type")),
		Tags:       csvParam(r, "tag"),
		PathPrefix: r.URL.Query().Get("prefix"),
		Text:       r.URL.Query().Get("q"),
		Limit:      intParam(r, "limit", 200),
		Offset:     intParam(r, "offset", 0),
	}
	notes, total, err := a.notes.List(r.Context(), q)
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]noteSummary, 0, len(notes))
	for _, n := range notes {
		items = append(items, summarize(n))
	}
	writeJSON(w, http.StatusOK, listEnvelope[noteSummary]{Items: items, Total: total, Limit: q.Limit, Offset: q.Offset})
}

func (a *API) handleGetNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.notes.Get(r.Context(), pathID(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) handleBacklinks(w http.ResponseWriter, r *http.Request) {
	notes, err := a.notes.Backlinks(r.Context(), pathID(r, "id"))
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]noteSummary, 0, len(notes))
	for _, n := range notes {
		items = append(items, summarize(n))
	}
	writeJSON(w, http.StatusOK, listEnvelope[noteSummary]{Items: items, Total: len(items)})
}

type createNoteReq struct {
	Path  string          `json:"path"`
	Title string          `json:"title"`
	Type  domain.NoteType `json:"type"`
	Tags  []string        `json:"tags"`
	Body  string          `json:"body"`
}

func (a *API) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req createNoteReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	n, err := a.notes.Create(r.Context(), service.CreateInput{
		Path: req.Path, Title: req.Title, Type: req.Type, Tags: req.Tags, Body: req.Body,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

type putNoteReq struct {
	Body     string `json:"body"`
	Revision int64  `json:"revision"`
}

func (a *API) handlePutNote(w http.ResponseWriter, r *http.Request) {
	var req putNoteReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	n, err := a.notes.ReplaceBody(r.Context(), pathID(r, "id"), req.Body, req.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

type patchNoteReq struct {
	Title    *string  `json:"title"`
	Type     *string  `json:"type"`
	Tags     []string `json:"tags"`
	Revision int64    `json:"revision"`
}

func (a *API) handlePatchNote(w http.ResponseWriter, r *http.Request) {
	var req patchNoteReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	kv := map[string]string{}
	if req.Title != nil {
		kv["title"] = *req.Title
	}
	if req.Type != nil {
		kv["type"] = *req.Type
	}
	if req.Tags != nil {
		kv["tags"] = "[" + strings.Join(req.Tags, ", ") + "]"
	}
	if len(kv) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "nothing to update", nil)
		return
	}
	n, err := a.notes.SetFrontmatter(r.Context(), pathID(r, "id"), kv, req.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	if err := a.notes.Delete(r.Context(), pathID(r, "id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleDaily(w http.ResponseWriter, r *http.Request) {
	d, err := domain.ParseDate(r.PathValue("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "date must be YYYY-MM-DD", nil)
		return
	}
	create := r.URL.Query().Get("create") == "true"
	n, err := a.notes.Daily(r.Context(), d, create)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}
