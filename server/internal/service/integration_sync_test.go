package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/integrations"
)

func TestSyncDoesNotCommitCursorOnPartialApply(t *testing.T) {
	svc, store, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	svc.state[cfg.ID] = integrations.State{Cursor: map[string]string{"cursor": "old"}}
	item := integrations.ExternalItem{ExternalID: "one", Kind: integrations.ItemNote, Title: "One", Hash: "h1"}
	fail := true
	conn.sync = func(in integrations.SyncInput) (integrations.SyncOutput, error) {
		if in.State["cursor"] != "old" {
			t.Fatalf("cursor advanced before full apply: %v", in.State)
		}
		// Even a connector that mutates its input cannot commit state implicitly.
		in.State["cursor"] = "mutated"
		items := []integrations.ExternalItem{item}
		if fail {
			items = append(items, integrations.ExternalItem{ExternalID: "two", Kind: integrations.ItemNote, Title: "Two", Hash: "h2"})
		}
		return integrations.SyncOutput{Items: items, State: map[string]string{"cursor": "new"}}, nil
	}
	svc.store = &lookupFailureStore{Store: store}
	if _, err := svc.SyncNow(ctx, cfg.ID); err == nil {
		t.Fatal("expected apply failure")
	}
	if got := svc.state[cfg.ID].Cursor["cursor"]; got != "old" {
		t.Fatalf("cursor=%q", got)
	}
	if got := svc.cfgStore.LoadState()[cfg.ID].Cursor["cursor"]; got != "old" {
		t.Fatalf("disk cursor=%q", got)
	}
	fail = false
	svc.store = store
	result, err := svc.SyncNow(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Created != 0 {
		t.Fatalf("retry duplicated first item: %+v", result)
	}
	if got := svc.state[cfg.ID].Cursor["cursor"]; got != "new" {
		t.Fatalf("cursor=%q", got)
	}
}

// Fail while looking up the second note, after the first note was applied.
type lookupFailureStore struct{ index.Store }

func (s *lookupFailureStore) NoteBySource(ctx context.Context, owner, id string) (*domain.Note, error) {
	if id == "two" {
		return nil, errors.New("index unavailable")
	}
	return s.Store.NoteBySource(ctx, owner, id)
}

func TestSyncSaveStateFailureIsVisibleAndRetryable(t *testing.T) {
	svc, _, dir, conn := newIntegrationsFixture(t)
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	svc.state[cfg.ID] = integrations.State{Cursor: map[string]string{"cursor": "old"}}
	conn.sync = func(in integrations.SyncInput) (integrations.SyncOutput, error) {
		if in.State["cursor"] != "old" {
			t.Fatalf("cursor=%v", in.State)
		}
		return integrations.SyncOutput{State: map[string]string{"cursor": "new"}}, nil
	}
	// A directory at the file destination deterministically makes rename fail.
	dest := filepath.Join(dir, filepath.FromSlash(integrations.StateFile))
	if err := os.MkdirAll(dest, 0700); err != nil {
		t.Fatal(err)
	}
	result, err := svc.SyncNow(context.Background(), cfg.ID)
	if err == nil || !strings.Contains(result.Error, "save integration state") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if svc.List()[0].LastError == "" || svc.List()[0].Running {
		t.Fatalf("status=%+v", svc.List())
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncNow(context.Background(), cfg.ID); err != nil {
		t.Fatal(err)
	}
	if got := svc.cfgStore.LoadState()[cfg.ID].Cursor["cursor"]; got != "new" {
		t.Fatalf("disk cursor=%q", got)
	}
}

func TestCalendarRestartDiscardsValidatorsWithoutSnapshot(t *testing.T) {
	svc, _, _, conn := newIntegrationsFixture(t)
	ctx := context.Background()
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Calendar", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Truncate(time.Second)
	item := integrations.ExternalItem{ExternalID: "event", Kind: integrations.ItemEvent, Title: "Meeting", Start: start, End: start.Add(time.Hour)}
	conn.sync = func(in integrations.SyncInput) (integrations.SyncOutput, error) {
		if in.State["etag"] != "" {
			return integrations.SyncOutput{}, integrations.ErrNotModified
		}
		return integrations.SyncOutput{Items: []integrations.ExternalItem{item}, State: map[string]string{"etag": "v1"}}, nil
	}
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	// A real fresh service and index share only the persisted config/state.
	fresh := index.NewMemory(svc.vault.Root)
	restarted := NewIntegrations(svc.vault, fresh, nil, nil, nil, NewBus(), nil, svc.log)
	restarted.RegisterConnector(conn)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	restarted.runDue(ctx) // Must not wait for the saved LastSync + interval.
	events, err := fresh.Events(ctx, start.Add(-time.Minute), start.Add(time.Hour))
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	result, err := restarted.SyncNow(ctx, cfg.ID)
	if err != nil || result.Items != 1 {
		t.Fatalf("304 result=%+v err=%v", result, err)
	}
	events, _ = fresh.Events(ctx, start.Add(-time.Minute), start.Add(time.Hour))
	if len(events) != 1 {
		t.Fatal("304 erased the calendar")
	}
}

func TestSyncFetchFailureDoesNotCommitReturnedOrMutatedCursor(t *testing.T) {
	svc, _, _, conn := newIntegrationsFixture(t)
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	svc.state[cfg.ID] = integrations.State{Cursor: map[string]string{"cursor": "old"}}
	conn.sync = func(in integrations.SyncInput) (integrations.SyncOutput, error) {
		in.State["cursor"] = "mutated"
		return integrations.SyncOutput{State: map[string]string{"cursor": "new"}}, errors.New("fetch failed")
	}
	if _, err := svc.SyncNow(context.Background(), cfg.ID); err == nil {
		t.Fatal("expected failure")
	}
	if got := svc.cfgStore.LoadState()[cfg.ID].Cursor["cursor"]; got != "old" {
		t.Fatalf("cursor=%q", got)
	}
}

type externalFailureStore struct{ index.Store }

func (s *externalFailureStore) PutExternal(context.Context, string, []domain.Event) error {
	return errors.New("calendar unavailable")
}

func TestSyncCalendarApplyFailureKeepsCursor(t *testing.T) {
	svc, store, _, _ := newIntegrationsFixture(t)
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Calendar", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	svc.state[cfg.ID] = integrations.State{Cursor: map[string]string{"cursor": "old"}}
	svc.store = &externalFailureStore{Store: store}
	if _, err := svc.SyncNow(context.Background(), cfg.ID); err == nil {
		t.Fatal("expected calendar write failure")
	}
	if got := svc.cfgStore.LoadState()[cfg.ID].Cursor["cursor"]; got != "old" {
		t.Fatalf("cursor=%q", got)
	}
}

func TestICSRestoresCalendarAfterRestartWithSavedETag(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	conditional := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") != "" {
			conditional++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		fmt.Fprintf(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:meeting\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:Meeting\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", start.Format("20060102T150405Z"), start.Add(time.Hour).Format("20060102T150405Z"))
	}))
	defer upstream.Close()
	svc, _, _, _ := newIntegrationsFixture(t)
	cfg, err := svc.Add(integrations.Config{Kind: "ics", Name: "ICS", Enabled: true, Values: map[string]string{"url": upstream.URL, "allow_private": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	if svc.cfgStore.LoadState()[cfg.ID].Cursor["etag"] == "" {
		t.Fatal("ETag not persisted")
	}
	fresh := index.NewMemory(svc.vault.Root)
	restarted := NewIntegrations(svc.vault, fresh, nil, nil, nil, NewBus(), nil, svc.log)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	restarted.runDue(ctx)
	events, err := fresh.Events(ctx, start.Add(-time.Minute), start.Add(time.Hour))
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v status=%+v", events, err, restarted.List())
	}
	if _, err := restarted.SyncNow(ctx, cfg.ID); err != nil {
		t.Fatal(err)
	}
	events, _ = fresh.Events(ctx, start.Add(-time.Minute), start.Add(time.Hour))
	if len(events) != 1 || conditional != 1 {
		t.Fatalf("events=%+v conditional=%d", events, conditional)
	}
}

func TestDeleteCannotRaceWithSyncCommit(t *testing.T) {
	svc, _, _, conn := newIntegrationsFixture(t)
	cfg, err := svc.Add(integrations.Config{Kind: "fake", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	conn.sync = func(in integrations.SyncInput) (integrations.SyncOutput, error) {
		close(started)
		<-release
		return integrations.SyncOutput{State: map[string]string{"cursor": "new"}}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := svc.SyncNow(context.Background(), cfg.ID); done <- err }()
	<-started
	deleteErr := svc.Delete(context.Background(), cfg.ID)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if deleteErr == nil {
		t.Fatal("delete must not race with an in-flight apply/commit")
	}
	if err := svc.Delete(context.Background(), cfg.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.cfgStore.LoadState()[cfg.ID]; ok {
		t.Fatal("deleted state resurrected")
	}
}
