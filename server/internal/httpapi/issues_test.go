package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

func TestIssueTrackerHTTP(t *testing.T) {
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := index.NewMemory(v.Root)
	bus := service.NewBus()
	ix := service.NewIndexer(v, store, bus, log)
	if _, err := ix.ReindexAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := New(&config.Config{}, store, v, ix, bus, nil, nil, nil, log).Handler()
	request := func(method, path string, body any, wantStatus int, out any) {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, rec.Code, wantStatus, rec.Body)
		}
		if out != nil {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}

	var p domain.TrackerProject
	request(http.MethodPost, "/api/v1/tracker/projects", map[string]any{"key": "WEB", "title": "Сайт"}, http.StatusCreated, &p)
	if p.Key != "WEB" || len(p.Workflow) != 3 {
		t.Fatalf("project = %+v", p)
	}
	var a, b domain.Issue
	request(http.MethodPost, "/api/v1/issues", map[string]any{"project": "WEB", "title": "Первый"}, http.StatusCreated, &a)
	request(http.MethodPost, "/api/v1/issues", map[string]any{"project": "WEB", "title": "Второй", "description": "Подробно"}, http.StatusCreated, &b)
	if a.Key != "WEB-1" || b.Key != "WEB-2" {
		t.Fatalf("keys = %s, %s", a.Key, b.Key)
	}
	request(http.MethodPost, "/api/v1/issues/WEB-2/move", map[string]any{"status": "in-progress", "after": "WEB-1"}, http.StatusOK, nil)
	request(http.MethodPost, "/api/v1/issues/WEB-2/comments", map[string]any{"body": "Готово к проверке"}, http.StatusCreated, nil)
	request(http.MethodPatch, "/api/v1/issues/WEB-1", map[string]any{"status": "nope"}, http.StatusUnprocessableEntity, nil)
	request(http.MethodPatch, "/api/v1/issues/WEB-1", map[string]any{"title": "x", "revision": 1}, http.StatusConflict, nil)

	var list listEnvelope[domain.Issue]
	request(http.MethodGet, "/api/v1/issues?project=WEB", nil, http.StatusOK, &list)
	if list.Total != 2 || list.Items[0].Key != "WEB-2" || list.Items[0].Status != "in-progress" || list.Items[0].Description != "" {
		t.Fatalf("list = %+v", list.Items)
	}
	var got domain.Issue
	request(http.MethodGet, "/api/v1/issues/WEB-2", nil, http.StatusOK, &got)
	if got.Description != "Подробно" || len(got.Comments) != 1 || got.StatusCategory != domain.CategoryInProgress {
		t.Fatalf("issue = %+v", got)
	}
	request(http.MethodDelete, "/api/v1/issues/WEB-1", nil, http.StatusNoContent, nil)
	request(http.MethodGet, "/api/v1/issues/WEB-1", nil, http.StatusNotFound, nil)
	// Sprints: plan, start, a second start is refused, complete moves the rest.
	var sp domain.Sprint
	request(http.MethodPost, "/api/v1/tracker/projects/WEB/sprints", map[string]any{"goal": "Запуск"}, http.StatusCreated, &sp)
	request(http.MethodPost, "/api/v1/tracker/projects/WEB/sprints", nil, http.StatusCreated, nil)
	request(http.MethodPost, "/api/v1/issues/WEB-2/move", map[string]any{"sprint": sp.ID}, http.StatusOK, nil)
	request(http.MethodPost, "/api/v1/sprints/"+sp.ID+"/start", nil, http.StatusOK, &sp)
	request(http.MethodPost, "/api/v1/sprints/WEB-S2/start", nil, http.StatusConflict, nil)
	var done struct {
		Sprint domain.Sprint `json:"sprint"`
		Moved  int           `json:"moved"`
	}
	request(http.MethodPost, "/api/v1/sprints/"+sp.ID+"/complete", map[string]any{"move_to": "WEB-S2"}, http.StatusOK, &done)
	if sp.State != domain.SprintActive || done.Sprint.State != domain.SprintClosed || done.Moved != 1 {
		t.Fatalf("sprint = %+v, done = %+v", sp, done)
	}
	var sprints listEnvelope[domain.Sprint]
	request(http.MethodGet, "/api/v1/tracker/projects/WEB/sprints", nil, http.StatusOK, &sprints)
	if sprints.Total != 2 || sprints.Items[0].ID != "WEB-S2" {
		t.Fatalf("sprints = %+v", sprints.Items)
	}
	request(http.MethodGet, "/api/v1/tracker/projects/NOPE/sprints", nil, http.StatusNotFound, nil)

	// A regular note is not reachable as an issue.
	request(http.MethodGet, "/api/v1/issues/20-projects%2Fweb.md", nil, http.StatusNotFound, nil)
}
