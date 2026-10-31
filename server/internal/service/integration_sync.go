package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/integrations"
)

// SyncResult reports one run.
type SyncResult struct {
	ID       string        `json:"id"`
	Items    int           `json:"items"`
	Created  int           `json:"created"`
	Updated  int           `json:"updated"`
	Events   int           `json:"events"`
	Skipped  int           `json:"skipped"`
	Duration time.Duration `json:"-"`
	TookMS   int64         `json:"took_ms"`
	Error    string        `json:"error,omitempty"`
}

// SyncNow runs one integration immediately.
func (s *Integrations) SyncNow(ctx context.Context, id string) (SyncResult, error) {
	s.mu.Lock()
	var cfg integrations.Config
	found := false
	for _, c := range s.configs {
		if c.ID == id {
			cfg, found = c, true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return SyncResult{}, index.ErrNotFound
	}
	if s.running[id] {
		s.mu.Unlock()
		return SyncResult{}, fmt.Errorf("интеграция %q уже синхронизируется", id)
	}
	s.running[id] = true
	state := s.state[id]
	state.Cursor = maps.Clone(state.Cursor)
	s.mu.Unlock()

	result := s.run(ctx, cfg, state)

	s.mu.Lock()
	next := s.state[id]
	next.LastSync = time.Now()
	next.LastItems = result.Items
	next.LastError = result.Error
	if result.Error == "" && result.cursor != nil {
		next.Cursor = result.cursor
	}
	stateCopy := s.copyStateLocked()
	stateCopy[id] = next
	// Serialize whole-file writes across integrations. A failed commit must
	// retain the previous cursor in memory as well as on disk for safe retry.
	if err := s.cfgStore.SaveState(stateCopy); err != nil {
		saveErr := fmt.Errorf("save integration state: %w", err)
		if result.Error != "" {
			saveErr = errors.Join(errors.New(result.Error), saveErr)
		}
		result.Error = saveErr.Error()
		next.Cursor = s.state[id].Cursor
		next.LastError = result.Error
	}
	s.state[id] = next
	// Failed bootstrap attempts use the normal error backoff; their cursor
	// remains empty, so the next attempt will still fetch a full snapshot.
	delete(s.bootstrap, id)
	delete(s.running, id)
	s.mu.Unlock()
	s.bus.Publish(Event{Type: "integration.synced", ID: id})

	if result.Error != "" {
		return result.SyncResult, errors.New(result.Error)
	}
	return result.SyncResult, nil
}

type runResult struct {
	SyncResult
	cursor map[string]string
}

func (s *Integrations) run(ctx context.Context, cfg integrations.Config, state integrations.State) runResult {
	start := time.Now()
	res := runResult{SyncResult: SyncResult{ID: cfg.ID}}

	conn, err := s.registry.Get(cfg.Kind)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	now := time.Now()
	in := integrations.SyncInput{
		Config: cfg,
		State:  state.Cursor,
		Since:  state.LastSync,
		Now:    now,
		// Keep a window around today: enough history to see last week, enough
		// future for planning, without expanding a decade of recurrences.
		From:   now.AddDate(0, -1, 0),
		To:     now.AddDate(0, 6, 0),
		HTTP:   integrations.NewHTTPClient(cfg.Bool("allow_private")),
		Secret: s.resolver.Resolve,
	}
	if in.State == nil {
		in.State = map[string]string{}
	}

	out, err := conn.Sync(ctx, in)
	if err != nil {
		if errors.Is(err, integrations.ErrNotModified) {
			// Keep the already applied projection and its item count.
			if len(state.Cursor) == 0 {
				res.Error = "connector returned not modified without an applied snapshot"
			} else {
				res.Items = state.LastItems
			}
			res.TookMS = time.Since(start).Milliseconds()
			return res
		}
		res.Error = err.Error()
		res.TookMS = time.Since(start).Milliseconds()
		return res
	}

	applied, err := s.apply(ctx, cfg, out.Items)
	applied.ID = cfg.ID
	if err != nil {
		applied.Error = err.Error()
	} else {
		applied.cursor = maps.Clone(out.State)
	}
	applied.TookMS = time.Since(start).Milliseconds()
	return applied
}
