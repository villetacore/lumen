package httpapi

import (
	"net/http"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/service"
)

func (a *API) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := domain.TaskQuery{
		Tags:          csvParam(r, "tag"),
		Project:       r.URL.Query().Get("project"),
		PathPrefix:    r.URL.Query().Get("prefix"),
		Text:          r.URL.Query().Get("q"),
		Overdue:       r.URL.Query().Get("overdue") == "true",
		DueBefore:     dateParam(r, "due_before"),
		DueAfter:      dateParam(r, "due_after"),
		ScheduledFrom: dateParam(r, "scheduled_from"),
		ScheduledTo:   dateParam(r, "scheduled_to"),
		Sort:          sortParam(r),
		Limit:         intParam(r, "limit", 500),
		Offset:        intParam(r, "offset", 0),
	}
	for _, s := range csvParam(r, "status") {
		q.Status = append(q.Status, domain.TaskStatus(s))
	}
	q = service.ResolveView(r.URL.Query().Get("view"), q, time.Now())

	tasks, total, err := a.tasks.Query(r.Context(), q)
	if err != nil {
		fail(w, err)
		return
	}
	if tasks == nil {
		tasks = []*domain.Task{}
	}
	writeJSON(w, http.StatusOK, listEnvelope[*domain.Task]{
		Items: tasks, Total: total, Limit: q.Limit, Offset: q.Offset,
	})
}

func (a *API) handleGetTask(w http.ResponseWriter, r *http.Request) {
	t, err := a.tasks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type patchTaskReq struct {
	Status    *string `json:"status"`
	Text      *string `json:"text"`
	Due       *string `json:"due"`
	Scheduled *string `json:"scheduled"`
	Start     *string `json:"start"`
	Priority  *string `json:"priority"`
	Estimate  *string `json:"estimate"`
	Project   *string `json:"project"`
	Revision  int64   `json:"revision"`
}

func (req patchTaskReq) toUpdate() (service.TaskUpdate, error) {
	up := service.TaskUpdate{
		Text: req.Text, Due: req.Due, Scheduled: req.Scheduled, Start: req.Start,
		Priority: req.Priority, Estimate: req.Estimate, Project: req.Project,
		Revision: req.Revision,
	}
	if req.Status != nil {
		s := domain.TaskStatus(*req.Status)
		switch s {
		case domain.StatusTodo, domain.StatusDoing, domain.StatusDone,
			domain.StatusCancelled, domain.StatusDeferred:
			up.Status = &s
		default:
			return up, errBadStatus
		}
	}
	return up, nil
}

var errBadStatus = errBadRequest("status must be one of todo, doing, done, cancelled, deferred")

type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

func (a *API) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	var req patchTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	up, err := req.toUpdate()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	t, err := a.tasks.Update(r.Context(), r.PathValue("id"), up)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type createTaskReq struct {
	Note      string  `json:"note"`
	Text      string  `json:"text"`
	Section   string  `json:"section"`
	Due       *string `json:"due"`
	Scheduled *string `json:"scheduled"`
	Priority  *string `json:"priority"`
	Project   *string `json:"project"`
}

func (a *API) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	if req.Note == "" {
		// Quick capture defaults to today's daily note.
		n, err := a.notes.Daily(r.Context(), domain.Today(), true)
		if err != nil {
			fail(w, err)
			return
		}
		req.Note = n.Path
	}
	t, err := a.tasks.Create(r.Context(), req.Note, req.Text, req.Section, service.TaskUpdate{
		Due: req.Due, Scheduled: req.Scheduled, Priority: req.Priority, Project: req.Project,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *API) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := a.tasks.Delete(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
