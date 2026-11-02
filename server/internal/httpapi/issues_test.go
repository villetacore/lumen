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
	// A regular note is not reachable as an issue.
	request(http.MethodGet, "/api/v1/issues/20-projects%2Fweb.md", nil, http.StatusNotFound, nil)
}
