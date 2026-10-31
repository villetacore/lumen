package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/alexpyslar/lumen/server/internal/config"
	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/alexpyslar/lumen/server/internal/vault"
)

func TestCreateAndListProjectDocumentationHTTP(t *testing.T) {
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
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var data []byte
		if body != nil {
			var err error
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	created := request(http.MethodPost, "/api/v1/notes", map[string]any{"title": "Project", "type": "project", "body": "Project description"})
	if created.Code != http.StatusCreated {
		t.Fatalf("%d %s", created.Code, created.Body.String())
	}
	var project domain.Note
	if err := json.Unmarshal(created.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	created = request(http.MethodPost, "/api/v1/notes", map[string]any{"title": "Documentation", "parent": "note:" + project.ID, "content_kind": "document", "body": "# Instructions"})
	if created.Code != http.StatusCreated {
		t.Fatalf("%d %s", created.Code, created.Body.String())
	}
	listed := request(http.MethodGet, "/api/v1/notes?parent="+url.QueryEscape("note:"+project.ID)+"&content_kind=document", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("%d %s", listed.Code, listed.Body.String())
	}
	var result listEnvelope[noteSummary]
	if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].Parent != "note:"+project.ID || result.Items[0].ContentKind != "document" {
		t.Fatalf("result=%+v", result)
	}
	// Existing clients can still supply a path without the new content fields.
	legacy := request(http.MethodPost, "/api/v1/notes", map[string]any{"path": "legacy.md", "title": "Legacy", "body": "Old client"})
	if legacy.Code != http.StatusCreated {
		t.Fatalf("legacy: %d %s", legacy.Code, legacy.Body.String())
	}
}
