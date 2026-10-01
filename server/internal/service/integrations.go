package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
	"github.com/alexpyslar/lumen/server/internal/integrations"
	"github.com/alexpyslar/lumen/server/internal/markdown"
	"github.com/alexpyslar/lumen/server/internal/secrets"
	"github.com/alexpyslar/lumen/server/internal/vault"
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

	mu      sync.Mutex
	configs []integrations.Config
	state   map[string]integrations.State
	running map[string]bool
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
		registry: integrations.NewRegistry(),
		cfgStore: integrations.NewStore(v.Root),
		resolver: resolver,
		state:    map[string]integrations.State{},
		running:  map[string]bool{},
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
	s.state = s.cfgStore.LoadState()
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
		if c.Enabled && !st.LastSync.IsZero() {
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
	Fields []integrations.ConfigField `json:"fields"`
}

func (s *Integrations) Kinds() []Kind {
	var out []Kind
	for _, c := range s.registry.Kinds() {
		out = append(out, Kind{Kind: c.Kind(), Title: c.Title(), Fields: c.ConfigSchema()})
	}
	return out
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
		updated := s.configs[i]
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
		s.configs[i] = updated
		return updated, s.cfgStore.Save(s.configs)
	}
	return integrations.Config{}, index.ErrNotFound
}

// Delete removes an integration and the calendar entries it contributed. Notes
// it created stay: they may carry the user's own writing.
func (s *Integrations) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	kept := s.configs[:0:0]
	found := false
	for _, c := range s.configs {
		if c.ID == id {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		s.mu.Unlock()
		return index.ErrNotFound
	}
	s.configs = kept
	delete(s.state, id)
	err := s.cfgStore.Save(s.configs)
	stateCopy := s.copyStateLocked()
	s.mu.Unlock()

	_ = s.cfgStore.SaveState(stateCopy)
	_ = s.store.DeleteExternal(ctx, id)
	return err
}

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
	s.mu.Unlock()

	result := s.run(ctx, cfg, state)

	s.mu.Lock()
	delete(s.running, id)
	next := s.state[id]
	next.LastSync = time.Now()
	next.LastItems = result.Items
	next.LastError = result.Error
	if result.cursor != nil {
		next.Cursor = result.cursor
	}
	s.state[id] = next
	stateCopy := s.copyStateLocked()
	s.mu.Unlock()

	_ = s.cfgStore.SaveState(stateCopy)
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
	res.cursor = out.State
	if err != nil {
		if errors.Is(err, integrations.ErrNotModified) {
			// Nothing changed upstream; keep whatever is already on the calendar.
			res.TookMS = time.Since(start).Milliseconds()
			return res
		}
		res.Error = err.Error()
		res.TookMS = time.Since(start).Milliseconds()
		return res
	}

	applied, err := s.apply(ctx, cfg, out.Items)
	applied.ID = cfg.ID
	applied.cursor = out.State
	if err != nil {
		applied.Error = err.Error()
	}
	applied.TookMS = time.Since(start).Milliseconds()
	return applied
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

// upsertNote creates the note for an external item, or refreshes the parts
// Lumen owns. The user's own text below the generated block is never rewritten
// — only frontmatter markers and the item's own task line are touched.
func (s *Integrations) upsertNote(
	ctx context.Context, cfg integrations.Config, item integrations.ExternalItem,
) (created, changed bool, err error) {
	existing, lookupErr := s.store.NoteBySource(ctx, cfg.ID, item.ExternalID)
	if lookupErr == nil {
		if asStr(existing.Frontmatter["source_hash"]) == item.Hash {
			return false, false, nil
		}
		return false, true, s.refreshNote(ctx, cfg, item, existing)
	}

	body := renderItemBody(cfg, item)
	relPath := path.Join(integrations.DefaultFolder(cfg), itemFileName(item)+".md")
	if s.vault.Exists(relPath) {
		// A file with that name exists but carries no marker: do not touch it.
		relPath = path.Join(integrations.DefaultFolder(cfg),
			itemFileName(item)+"-"+shortHash(item.ExternalID)+".md")
	}

	if _, err := s.notes.Create(ctx, CreateInput{
		Path:  relPath,
		Title: item.Title,
		Type:  domain.TypeNote,
		Tags:  append([]string{cfg.Kind}, item.Labels...),
		Body:  body,
		Extra: map[string]string{
			"source_connector":   cfg.Kind,
			"source_integration": cfg.ID,
			"source_id":          item.ExternalID,
			"source_url":         item.URL,
			"source_hash":        item.Hash,
		},
	}); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// refreshNote updates the markers and the task line, and nothing else.
func (s *Integrations) refreshNote(
	ctx context.Context, cfg integrations.Config, item integrations.ExternalItem, note *domain.Note,
) error {
	if _, err := s.notes.SetFrontmatter(ctx, note.Path, map[string]string{
		"source_hash": item.Hash,
		"source_url":  item.URL,
	}, 0); err != nil {
		return err
	}

	// External completion propagates into the vault; the reverse direction is
	// deliberately not implemented (see docs/INTEGRATIONS.md).
	if item.Kind != integrations.ItemTask {
		return nil
	}
	blockID := taskBlockID(item)
	task, err := s.store.Task(ctx, blockID)
	if err != nil {
		return nil // the user may have deleted the checkbox line
	}
	want := domain.StatusTodo
	if item.Done {
		want = domain.StatusDone
	}
	if task.Status == want || (want == domain.StatusTodo && task.Status != domain.StatusDone) {
		return nil
	}
	_, err = s.tasks.Update(ctx, blockID, TaskUpdate{Status: &want})
	return err
}

func renderItemBody(cfg integrations.Config, item integrations.ExternalItem) string {
	var b strings.Builder
	b.WriteString("# " + item.Title + "\n\n")

	if item.Kind == integrations.ItemTask {
		line := "- [ ] " + item.Title
		if item.Done {
			line = "- [x] " + item.Title
		}
		fields := map[string]string{}
		if !item.Due.IsZero() {
			fields["due"] = domain.Date{T: item.Due}.String()
		}
		patched, err := markdown.ApplyTaskPatch(line, markdown.TaskPatch{
			Set: fields, BlockID: taskBlockID(item),
		})
		if err == nil {
			line = patched
		}
		b.WriteString(line + "\n\n")
	}

	if item.URL != "" {
		b.WriteString("[Открыть в " + cfg.Kind + "](" + item.URL + ")\n\n")
	}
	if item.Location != "" {
		b.WriteString("Место: " + item.Location + "\n\n")
	}
	if body := strings.TrimSpace(item.Body); body != "" {
		b.WriteString("---\n\n" + body + "\n")
	}
	b.WriteString("\n<!-- Ниже можно писать своё: Lumen обновляет только заголовок и строку задачи. -->\n")
	return b.String()
}

// taskBlockID is derived from the external id, so the checkbox line stays
// findable across syncs without storing another mapping.
func taskBlockID(item integrations.ExternalItem) string {
	return "x" + shortHash(item.ExternalID)
}

func itemFileName(item integrations.ExternalItem) string {
	name := strings.ToLower(strings.TrimSpace(item.Title))
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "", "*", "", "?", "",
		`"`, "", "<", "", ">", "", "|", "", "#", "", "[", "", "]", "", "\n", " ")
	name = strings.Join(strings.Fields(repl.Replace(name)), "-")
	if len(name) > 60 {
		name = name[:60]
		name = strings.TrimRight(name, "-")
	}
	if name == "" {
		name = "item-" + shortHash(item.ExternalID)
	}
	return name
}

func shortHash(s string) string {
	return markdown.DerivedTaskID(s, "", 0)[1:9]
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
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
		if now.Before(nextRun(c, st)) {
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
