// Package passwords gives read-only access to an existing password manager —
// Bitwarden, Vaultwarden or KeePass — from inside Lumen.
//
// Two rules shape everything here:
//
//   - Secret values never leave this package except as the answer to an
//     explicit "reveal this one field" request. They are not written to the
//     vault, not put in the index, not logged and not cached on disk. A
//     password copied into a note would end up in search, in git and in every
//     backup.
//   - Lumen never stores a master password. Unlocking produces an opaque
//     session that lives in memory only and expires on its own.
package passwords

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound    = errors.New("хранилище не найдено")
	ErrLocked      = errors.New("хранилище заблокировано")
	ErrUnknownKind = errors.New("неизвестный тип хранилища")
	ErrNoField     = errors.New("у записи нет такого поля")
)

// FieldType tells the UI how to render a setting.
type FieldType string

const (
	FieldText   FieldType = "text"
	FieldURL    FieldType = "url"
	FieldSecret FieldType = "secret"
	FieldBool   FieldType = "bool"
)

// Field describes one setting of a password store.
type Field struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required"`
	Placeholder string    `json:"placeholder,omitempty"`
	Help        string    `json:"help,omitempty"`
}

// Config is a configured password store. It holds locations and references,
// never a secret value.
type Config struct {
	ID     string            `yaml:"id" json:"id"`
	Kind   string            `yaml:"kind" json:"kind"`
	Name   string            `yaml:"name" json:"name"`
	Values map[string]string `yaml:"values" json:"values"`
}

func (c Config) Value(key, def string) string {
	if v, ok := c.Values[key]; ok && v != "" {
		return v
	}
	return def
}

func (c Config) Bool(key string) bool {
	switch c.Values[key] {
	case "true", "yes", "1", "on":
		return true
	}
	return false
}

// Item is one entry as the list shows it — no secret values.
type Item struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Username string   `json:"username,omitempty"`
	URI      string   `json:"uri,omitempty"`
	Folder   string   `json:"folder,omitempty"`
	Fields   []string `json:"fields"` // что можно запросить: password, username, totp, notes
}

// Session is the opaque result of unlocking. It never reaches the client.
type Session struct {
	// Token is a provider session (Bitwarden), Secret is a database password
	// (KeePass). Whichever the backend needs.
	Token  string
	Secret []byte
}

// Input is one external command to run.
type Input struct {
	Name  string
	Args  []string
	Stdin []byte
	// Env carries secrets that must not appear in the process arguments,
	// which are visible to every user on the machine.
	Env map[string]string
}

// Runner executes external CLIs. An interface so tests never touch a real
// password manager.
type Runner interface {
	Run(ctx context.Context, in Input) ([]byte, error)
}

// Backend is one kind of password store.
type Backend interface {
	Kind() string
	Title() string
	ConfigSchema() []Field
	Validate(Config) error
	// NeedsPassphrase reports whether Unlock requires input from the user, or
	// whether the configuration already points at a usable session.
	NeedsPassphrase(cfg Config) bool
	Unlock(ctx context.Context, cfg Config, passphrase []byte, r Runner) (Session, error)
	List(ctx context.Context, cfg Config, s Session, query string, r Runner) ([]Item, error)
	Reveal(ctx context.Context, cfg Config, s Session, itemID, field string, r Runner) (string, error)
}

// Status is what the UI shows for one store.
type Status struct {
	Config
	Title           string     `json:"title"`
	Unlocked        bool       `json:"unlocked"`
	NeedsPassphrase bool       `json:"needs_passphrase"`
	LocksAt         *time.Time `json:"locks_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

// KindInfo describes a supported backend for the settings form.
type KindInfo struct {
	Kind   string  `json:"kind"`
	Title  string  `json:"title"`
	Fields []Field `json:"fields"`
}

// DefaultAutoLock is how long an unlocked store stays usable without activity.
const DefaultAutoLock = 15 * time.Minute

type unlocked struct {
	session Session
	lastUse time.Time
}

// SecretResolver turns a reference such as "env:BW_SESSION" into a value.
type SecretResolver func(ctx context.Context, ref string) ([]byte, error)

// Manager owns the configured stores and their in-memory unlocked sessions.
type Manager struct {
	store    *Store
	backends map[string]Backend
	order    []string
	runner   Runner
	resolve  SecretResolver
	autoLock time.Duration

	mu       sync.Mutex
	configs  []Config
	sessions map[string]*unlocked
	errors   map[string]string
}

func NewManager(vaultRoot string, runner Runner, resolve SecretResolver) *Manager {
	if runner == nil {
		runner = ExecRunner{}
	}
	m := &Manager{
		store:    NewStore(vaultRoot),
		backends: map[string]Backend{},
		runner:   runner,
		resolve:  resolve,
		autoLock: DefaultAutoLock,
		sessions: map[string]*unlocked{},
		errors:   map[string]string{},
	}
	for _, b := range []Backend{&Bitwarden{}, &KeePass{}} {
		m.Register(b)
	}
	return m
}

func (m *Manager) Register(b Backend) {
	if _, ok := m.backends[b.Kind()]; !ok {
		m.order = append(m.order, b.Kind())
	}
	m.backends[b.Kind()] = b
}

// SetAutoLock changes the idle timeout; zero disables automatic locking.
func (m *Manager) SetAutoLock(d time.Duration) { m.autoLock = d }

// Load reads the configured stores from the vault.
func (m *Manager) Load() error {
	configs, err := m.store.Load()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = configs
	return nil
}

func (m *Manager) Kinds() []KindInfo {
	out := make([]KindInfo, 0, len(m.order))
	for _, k := range m.order {
		b := m.backends[k]
		out = append(out, KindInfo{Kind: b.Kind(), Title: b.Title(), Fields: b.ConfigSchema()})
	}
	return out
}

// List returns every store with its lock state.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()

	out := make([]Status, 0, len(m.configs))
	for _, c := range m.configs {
		b, ok := m.backends[c.Kind]
		status := Status{Config: c, Title: c.Kind, LastError: m.errors[c.ID]}
		if ok {
			status.Title = b.Title()
			status.NeedsPassphrase = b.NeedsPassphrase(c)
		}
		if u, live := m.sessions[c.ID]; live {
			status.Unlocked = true
			if m.autoLock > 0 {
				at := u.lastUse.Add(m.autoLock)
				status.LocksAt = &at
			}
		}
		out = append(out, status)
	}
	return out
}

func (m *Manager) Add(cfg Config) (Config, error) {
	b, ok := m.backends[cfg.Kind]
	if !ok {
		return cfg, fmt.Errorf("%w: %s", ErrUnknownKind, cfg.Kind)
	}
	if cfg.Values == nil {
		cfg.Values = map[string]string{}
	}
	if cfg.Name == "" {
		cfg.Name = b.Title()
	}
	if err := b.Validate(cfg); err != nil {
		return cfg, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg.ID == "" {
		cfg.ID = NewID(cfg.Name, m.configs)
	}
	for _, existing := range m.configs {
		if existing.ID == cfg.ID {
			return cfg, fmt.Errorf("хранилище %q уже есть", cfg.ID)
		}
	}
	m.configs = append(m.configs, cfg)
	sort.Slice(m.configs, func(i, j int) bool { return m.configs[i].ID < m.configs[j].ID })
	return cfg, m.store.Save(m.configs)
}

func (m *Manager) Update(id string, apply func(*Config)) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.configs {
		if m.configs[i].ID != id {
			continue
		}
		updated := m.configs[i]
		apply(&updated)
		b, ok := m.backends[updated.Kind]
		if !ok {
			return updated, fmt.Errorf("%w: %s", ErrUnknownKind, updated.Kind)
		}
		if err := b.Validate(updated); err != nil {
			return updated, err
		}
		m.configs[i] = updated
		// Настройки изменились — прежняя сессия может быть уже не про то.
		delete(m.sessions, id)
		return updated, m.store.Save(m.configs)
	}
	return Config{}, ErrNotFound
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.configs[:0:0]
	found := false
	for _, c := range m.configs {
		if c.ID == id {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		return ErrNotFound
	}
	m.configs = kept
	delete(m.sessions, id)
	delete(m.errors, id)
	return m.store.Save(m.configs)
}

// Unlock opens a store. The passphrase is used and discarded; only the opaque
// session it produces is kept, in memory.
func (m *Manager) Unlock(ctx context.Context, id string, passphrase []byte) error {
	cfg, b, err := m.lookup(id)
	if err != nil {
		return err
	}

	// Конфигурация может указывать на готовую сессию (например env:BW_SESSION) —
	// тогда пароль спрашивать не нужно.
	if len(passphrase) == 0 && !b.NeedsPassphrase(cfg) {
		if ref := cfg.Value("session", ""); ref != "" && m.resolve != nil {
			value, rerr := m.resolve(ctx, ref)
			if rerr != nil {
				m.setError(id, rerr.Error())
				return rerr
			}
			passphrase = value
		}
	}

	session, err := b.Unlock(ctx, cfg, passphrase, m.runner)
	if err != nil {
		m.setError(id, err.Error())
		return err
	}

	m.mu.Lock()
	m.sessions[id] = &unlocked{session: session, lastUse: time.Now()}
	delete(m.errors, id)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Lock(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// LockAll drops every session; called on shutdown and by the UI.
func (m *Manager) LockAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = map[string]*unlocked{}
}

func (m *Manager) Items(ctx context.Context, id, query string) ([]Item, error) {
	cfg, b, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	session, err := m.session(id)
	if err != nil {
		return nil, err
	}
	items, err := b.List(ctx, cfg, session, query, m.runner)
	if err != nil {
		m.setError(id, err.Error())
		return nil, err
	}
	m.setError(id, "")
	return items, nil
}

// Reveal returns one field of one item. This is the only path a secret value
// takes out of this package.
func (m *Manager) Reveal(ctx context.Context, id, itemID, field string) (string, error) {
	cfg, b, err := m.lookup(id)
	if err != nil {
		return "", err
	}
	session, err := m.session(id)
	if err != nil {
		return "", err
	}
	if field == "" {
		field = "password"
	}
	value, err := b.Reveal(ctx, cfg, session, itemID, field, m.runner)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (m *Manager) lookup(id string) (Config, Backend, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.configs {
		if c.ID != id {
			continue
		}
		b, ok := m.backends[c.Kind]
		if !ok {
			return c, nil, fmt.Errorf("%w: %s", ErrUnknownKind, c.Kind)
		}
		return c, b, nil
	}
	return Config{}, nil, ErrNotFound
}

// session returns a live session and refreshes its idle timer.
func (m *Manager) session(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	u, ok := m.sessions[id]
	if !ok {
		return Session{}, fmt.Errorf("%w: %s", ErrLocked, id)
	}
	u.lastUse = time.Now()
	return u.session, nil
}

func (m *Manager) sweepLocked() {
	if m.autoLock <= 0 {
		return
	}
	now := time.Now()
	for id, u := range m.sessions {
		if now.Sub(u.lastUse) > m.autoLock {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) setError(id, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg == "" {
		delete(m.errors, id)
		return
	}
	m.errors[id] = msg
}

// hasField reports whether a field name is among the ones an item offers.
func hasField(fields []string, name string) bool {
	for _, f := range fields {
		if strings.EqualFold(f, name) {
			return true
		}
	}
	return false
}
