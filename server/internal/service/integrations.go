package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/integrations"
	"github.com/villetacore/lumen/server/internal/secrets"
	"github.com/villetacore/lumen/server/internal/vault"
)

// Integrations owns the scheduling of connectors and the mapping of what they
// return onto the vault. Connectors themselves never touch files.
type Integrations struct {
	vault    *vault.Vault
	store    index.Store
	tasks    *Tasks
	notes    *Notes
	ix       *Indexer
	bus      *Bus
	log      *slog.Logger
	registry *integrations.Registry
	cfgStore *integrations.Store
	resolver *secrets.Resolver
	keeper   secrets.Keeper

	mu      sync.Mutex
	configs []integrations.Config
	state   map[string]integrations.State
	running map[string]bool
	// bootstrap schedules a full fetch after Load before normal polling.
	bootstrap map[string]bool
}

func NewIntegrations(
	v *vault.Vault, st index.Store, tasks *Tasks, notes *Notes, ix *Indexer,
	bus *Bus, resolver *secrets.Resolver, log *slog.Logger,
) *Integrations {
	if resolver == nil {
		resolver = secrets.New(nil)
	}
	return &Integrations{
		vault: v, store: st, tasks: tasks, notes: notes, ix: ix, bus: bus, log: log,
		registry:  integrations.NewRegistry(),
		cfgStore:  integrations.NewStore(v.Root),
		resolver:  resolver,
		state:     map[string]integrations.State{},
		running:   map[string]bool{},
		bootstrap: map[string]bool{},
	}
}

// RegisterConnector adds a connector to this instance. Used by tests and by
// future out-of-tree integrations.
func (s *Integrations) RegisterConnector(c integrations.Connector) {
	s.registry.Register(c)
}

// Load reads configuration and state from the vault.
func (s *Integrations) Load() error {
	configs, err := s.cfgStore.Load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = configs
	// A secret left in the file in the clear, which older versions allowed,
	// moves to the keeper and leaves the file.
	if s.keeper != nil {
		moved := false
		for i := range s.configs {
			conn, err := s.registry.Get(s.configs[i].Kind)
			if err != nil {
				continue
			}
			for _, f := range conn.ConfigSchema() {
				if v := s.configs[i].Values[f.Key]; f.Type == integrations.FieldSecret && v != "" && !secrets.IsReference(v) {
					moved = true
				}
			}
			if err := s.keepSecrets(&s.configs[i], conn); err != nil {
				return err
			}
		}
		if moved {
			if err := s.cfgStore.Save(s.configs); err != nil {
				return err
			}
		}
	}
	s.bootstrap = map[string]bool{}
	for _, cfg := range configs {
		s.bootstrap[cfg.ID] = true
	}
	s.state = s.cfgStore.LoadState()
	// Calendar projections are in memory, not in the saved cursor file. Force
	// a full fetch after startup; otherwise a valid ETag can leave them empty.
	for id, state := range s.state {
		state.Cursor = nil
		s.state[id] = state
	}
	return nil
}

// Status is one integration as the UI sees it.
type Status struct {
	integrations.Config
	Title     string    `json:"title"`
	LastSync  time.Time `json:"last_sync,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	LastItems int       `json:"last_items"`
	Running   bool      `json:"running"`
	NextSync  time.Time `json:"next_sync,omitempty"`
}

// List returns every configured integration with its current state. Secret
// references are shown as-is: they name a location, never a value.
func (s *Integrations) List() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Status, 0, len(s.configs))
	for _, c := range s.configs {
		st := s.state[c.ID]
		title := c.Kind
		if conn, err := s.registry.Get(c.Kind); err == nil {
			title = conn.Title()
		}
		status := Status{
			Config: c, Title: title,
			LastSync: st.LastSync, LastError: st.LastError,
			LastItems: st.LastItems, Running: s.running[c.ID],
		}
		if c.Enabled && !s.bootstrap[c.ID] && !st.LastSync.IsZero() {
			status.NextSync = st.LastSync.Add(c.Interval)
		}
		out = append(out, status)
	}
	return out
}

// Kinds describes the connectors this build supports, for the settings form.
type Kind struct {
	Kind   string                     `json:"kind"`
	Title  string                     `json:"title"`
	Group  string                     `json:"group"`
	Fields []integrations.ConfigField `json:"fields"`
}

func (s *Integrations) Kinds() []Kind {
	var out []Kind
	for _, c := range s.registry.Kinds() {
		out = append(out, Kind{Kind: c.Kind(), Title: c.Title(), Group: integrations.GroupOf(c.Kind()), Fields: c.ConfigSchema()})
	}
	return out
}

// UseKeeper lets settings forms accept pasted tokens (ADR-0017).
func (s *Integrations) UseKeeper(k secrets.Keeper) { s.keeper = k }

// keepSecrets replaces pasted values in secret fields by references. Without a
// keeper a value is refused rather than written to a file in the vault.
func (s *Integrations) keepSecrets(cfg *integrations.Config, conn integrations.Connector) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, f := range conn.ConfigSchema() {
		v := strings.TrimSpace(cfg.Values[f.Key])
		if f.Type != integrations.FieldSecret || v == "" || secrets.IsReference(v) {
			continue
		}
		if s.keeper == nil {
			return fmt.Errorf("поле «%s»: хранилище секретов недоступно, укажите ссылку env: или file:", f.Label)
		}
		ref, err := s.keeper.Keep(ctx, "integration:"+cfg.ID+":"+f.Key, v)
		if err != nil {
			return fmt.Errorf("поле «%s» не сохранено: %w", f.Label, err)
		}
		cfg.Values[f.Key] = ref
	}
	return nil
}

func (s *Integrations) forgetReplaced(old, updated integrations.Config) {
	if s.keeper == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for k, v := range old.Values {
		if updated.Values[k] != v && strings.HasPrefix(v, secrets.StoredScheme+":") {
			_ = s.keeper.Forget(ctx, v)
		}
	}
}

// Add validates and stores a new integration.
func (s *Integrations) Add(cfg integrations.Config) (integrations.Config, error) {
	conn, err := s.registry.Get(cfg.Kind)
	if err != nil {
		return cfg, err
	}
	if cfg.Values == nil {
		cfg.Values = map[string]string{}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = integrations.DefaultInterval
	}
	if cfg.Interval < integrations.MinInterval {
		cfg.Interval = integrations.MinInterval
	}
	if cfg.Name == "" {
		cfg.Name = conn.Title()
	}
	if err := conn.Validate(cfg); err != nil {
		return cfg, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.ID == "" {
		cfg.ID = integrations.NewID(cfg.Name, s.configs)
	}
	for _, existing := range s.configs {
		if existing.ID == cfg.ID {
			return cfg, fmt.Errorf("интеграция %q уже есть", cfg.ID)
		}
	}
	if err := s.keepSecrets(&cfg, conn); err != nil {
		return cfg, err
	}
	s.configs = append(s.configs, cfg)
	sort.Slice(s.configs, func(i, j int) bool { return s.configs[i].ID < s.configs[j].ID })
	return cfg, s.cfgStore.Save(s.configs)
}

// Update replaces the mutable parts of an integration.
func (s *Integrations) Update(id string, apply func(*integrations.Config)) (integrations.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.configs {
		if s.configs[i].ID != id {
			continue
		}
		old := s.configs[i]
		updated := old
		updated.Values = make(map[string]string, len(old.Values))
		for k, v := range old.Values {
			updated.Values[k] = v
		}
		apply(&updated)
		if updated.Interval < integrations.MinInterval {
			updated.Interval = integrations.MinInterval
		}
		conn, err := s.registry.Get(updated.Kind)
		if err != nil {
			return updated, err
		}
		if err := conn.Validate(updated); err != nil {
			return updated, err
		}
		if err := s.keepSecrets(&updated, conn); err != nil {
			return updated, err
		}
		s.forgetReplaced(old, updated)
		s.configs[i] = updated
		return updated, s.cfgStore.Save(s.configs)
	}
	return integrations.Config{}, index.ErrNotFound
}

// Delete removes an integration and the calendar entries it contributed. Notes
// it created stay: they may carry the user's own writing.
func (s *Integrations) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] {
		return fmt.Errorf("интеграция %q уже синхронизируется", id)
	}
	kept := s.configs[:0:0]
	found := false
	var gone integrations.Config
	for _, c := range s.configs {
		if c.ID == id {
			found = true
			gone = c
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		return index.ErrNotFound
	}
	if err := s.cfgStore.Save(kept); err != nil {
		return err
	}
	s.configs = kept
	s.forgetReplaced(gone, integrations.Config{})
	delete(s.state, id)
	delete(s.bootstrap, id)
	stateErr := s.cfgStore.SaveState(s.copyStateLocked())
	if stateErr != nil {
		stateErr = fmt.Errorf("save integration state: %w", stateErr)
	}
	return errors.Join(stateErr, s.store.DeleteExternal(ctx, id))
}

// apply turns items into calendar entries and notes.
func (s *Integrations) apply(ctx context.Context, cfg integrations.Config, items []integrations.ExternalItem) (runResult, error) {
	var (
		res    runResult
		events []domain.Event
	)
	res.Items = len(items)

	for _, item := range items {
		switch item.Kind {
		case integrations.ItemEvent:
			events = append(events, externalEvent(cfg, item))

		case integrations.ItemTask, integrations.ItemNote:
			created, changed, err := s.upsertNote(ctx, cfg, item)
			if err != nil {
				return res, err
			}
			switch {
			case created:
				res.Created++
			case changed:
				res.Updated++
			default:
				res.Skipped++
			}
		}
	}

	res.Events = len(events)
	if err := s.store.PutExternal(ctx, cfg.ID, events); err != nil {
		return res, err
	}
	return res, nil
}

func externalEvent(cfg integrations.Config, item integrations.ExternalItem) domain.Event {
	calendar := cfg.Name
	if len(item.Labels) > 0 {
		calendar = item.Labels[0]
	}
	return domain.Event{
		ID:          cfg.ID + ":" + item.ExternalID,
		Source:      domain.SourceICS,
		RefID:       item.ExternalID,
		Title:       item.Title,
		Start:       item.Start,
		End:         item.End,
		AllDay:      item.AllDay,
		Calendar:    calendar,
		Location:    item.Location,
		URL:         item.URL,
		Integration: cfg.ID,
	}
}

func (s *Integrations) copyStateLocked() map[string]integrations.State {
	out := make(map[string]integrations.State, len(s.state))
	for k, v := range s.state {
		out[k] = v
	}
	return out
}

// Run is the scheduler: it wakes up regularly and syncs whatever is due.
func (s *Integrations) Run(ctx context.Context) {
	const tick = 30 * time.Second
	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runDue(ctx)
		}
	}
}

func (s *Integrations) runDue(ctx context.Context) {
	now := time.Now()

	s.mu.Lock()
	var due []integrations.Config
	for _, c := range s.configs {
		if !c.Enabled || s.running[c.ID] {
			continue
		}
		st := s.state[c.ID]
		if !s.bootstrap[c.ID] && now.Before(nextRun(c, st)) {
			continue
		}
		due = append(due, c)
	}
	s.mu.Unlock()

	for _, c := range due {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.SyncNow(ctx, c.ID); err != nil {
			s.log.Warn("integration sync failed", "id", c.ID, "kind", c.Kind, "err", err)
			continue
		}
		s.log.Debug("integration synced", "id", c.ID)
	}
}

// nextRun spaces out repeated failures so a broken feed is not hammered, and
// adds a little jitter so several integrations do not fire in lockstep.
func nextRun(c integrations.Config, st integrations.State) time.Time {
	if st.LastSync.IsZero() {
		return time.Time{}
	}
	interval := c.Interval
	if st.LastError != "" {
		interval *= 4
	}
	jitter := time.Duration(rand.Int63n(int64(interval/10 + 1)))
	return st.LastSync.Add(interval + jitter)
}
