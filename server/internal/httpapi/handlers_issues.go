package httpapi

import (
	"net/http"

	"github.com/alexpyslar/lumen/server/internal/auth"
	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/service"
)

// actor is the username recorded as reporter or comment author. Local and
// token modes have no person behind the request, so it stays empty.
func actor(r *http.Request) string {
	if c, ok := r.Context().Value(claimsKey).(*auth.Claims); ok && c != nil {
		for _, s := range []string{c.Username, c.Email, c.Subject} {
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func (a *API) handleListTrackerProjects(w http.ResponseWriter, r *http.Request) {
	list, err := a.issues.Projects(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listEnvelope[*domain.TrackerProject]{Items: list, Total: len(list)})
}

func (a *API) handleGetTrackerProject(w http.ResponseWriter, r *http.Request) {
	p, err := a.issues.Project(r.Context(), r.PathValue("key"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type createTrackerProjectReq struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

func (a *API) handleCreateTrackerProject(w http.ResponseWriter, r *http.Request) {
	var req createTrackerProjectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	p, err := a.issues.CreateProject(r.Context(), service.ProjectInput{Key: req.Key, Title: req.Title, Note: req.Note})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *API) handleListIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := domain.IssueQuery{
		Project:  q.Get("project"),
		Status:   csvParam(r, "status"),
		Category: domain.StatusCategory(q.Get("category")),
		Assignee: q.Get("assignee"),
		Type:     q.Get("type"),
		Labels:   csvParam(r, "label"),
		Sprint:   q.Get("sprint"),
		Epic:     q.Get("epic"),
		Text:     q.Get("q"),
		Limit:    intParam(r, "limit", 1000),
		Offset:   intParam(r, "offset", 0),
	}
	list, total, err := a.issues.List(r.Context(), query)
	if err != nil {
		fail(w, err)
		return
	}
	if list == nil {
		list = []*domain.Issue{}
	}
	// The list is for boards and tables: descriptions and comments stay in the card.
	for _, is := range list {
		is.Description, is.Comments, is.Checklist = "", nil, nil
	}
	writeJSON(w, http.StatusOK, listEnvelope[*domain.Issue]{Items: list, Total: total, Limit: query.Limit, Offset: query.Offset})
}

func (a *API) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	is, err := a.issues.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

type createIssueReq struct {
	Project     string   `json:"project"`
	Title       string   `json:"title"`
	Type        string   `json:"issue_type"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Assignee    string   `json:"assignee"`
	Labels      []string `json:"labels"`
	Epic        string   `json:"epic"`
	Parent      string   `json:"parent"`
	Sprint      string   `json:"sprint"`
	Start       string   `json:"start"`
	Due         string   `json:"due"`
	Estimate    string   `json:"estimate"`
	Points      float64  `json:"points"`
	Description string   `json:"description"`
}

func (a *API) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	var req createIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	is, err := a.issues.Create(r.Context(), service.IssueInput(req), actor(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, is)
}

type patchIssueReq struct {
	Title       *string   `json:"title"`
	Type        *string   `json:"issue_type"`
	Status      *string   `json:"status"`
	Priority    *string   `json:"priority"`
	Assignee    *string   `json:"assignee"`
	Labels      *[]string `json:"labels"`
	Watchers    *[]string `json:"watchers"`
	Epic        *string   `json:"epic"`
	Parent      *string   `json:"parent"`
	Sprint      *string   `json:"sprint"`
	Start       *string   `json:"start"`
	Due         *string   `json:"due"`
	Estimate    *string   `json:"estimate"`
	Points      *float64  `json:"points"`
	Description *string   `json:"description"`
	Revision    int64     `json:"revision"`
}

func (a *API) handlePatchIssue(w http.ResponseWriter, r *http.Request) {
	var req patchIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	is, err := a.issues.Update(r.Context(), r.PathValue("key"), service.IssueUpdate(req))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

type moveIssueReq struct {
	Status   string `json:"status"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Revision int64  `json:"revision"`
}

func (a *API) handleMoveIssue(w http.ResponseWriter, r *http.Request) {
	var req moveIssueReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	is, err := a.issues.Move(r.Context(), r.PathValue("key"), service.MoveInput(req))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

type commentReq struct {
	Body string `json:"body"`
}

func (a *API) handleAddComment(w http.ResponseWriter, r *http.Request) {
	var req commentReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	_, c, err := a.issues.AddComment(r.Context(), r.PathValue("key"), req.Body, actor(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	if err := a.issues.Delete(r.Context(), r.PathValue("key")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
