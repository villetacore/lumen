// Package integrations pulls data from the outside world into the vault:
// calendars, issue trackers, anything with a feed or an API.
//
// A connector only fetches and normalises; it never touches the filesystem.
// Turning items into notes, tasks and calendar entries is the job of the
// mapper in internal/service, so a new integration is one file and a
// registration line.
package integrations

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ItemKind decides where an external item ends up.
type ItemKind string

const (
	// ItemNote becomes a markdown note in the vault.
	ItemNote ItemKind = "note"
	// ItemTask becomes a note with a checkbox line inside it.
	ItemTask ItemKind = "task"
	// ItemEvent stays in the index and shows up on the calendar only: a work
	// calendar with thousands of meetings has no business littering the vault.
	ItemEvent ItemKind = "event"
)

// ExternalItem is one thing fetched from an external system, already
// normalised into Lumen's vocabulary.
type ExternalItem struct {
	ExternalID string
	Kind       ItemKind
	Title      string
	Body       string
	URL        string
	Location   string
	Start      time.Time
	End        time.Time
	AllDay     bool
	Due        time.Time
	Done       bool
	Labels     []string
	// Hash covers everything the connector considers meaningful. The mapper
	// uses it to skip items that have not changed since the last sync.
	Hash string
}

// FieldType tells the UI how to render a configuration field.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldURL      FieldType = "url"
	FieldSecret   FieldType = "secret" // a secrets reference, never a raw value
	FieldBool     FieldType = "bool"
	FieldDuration FieldType = "duration"
)

// ConfigField describes one setting of a connector.
type ConfigField struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required"`
	Placeholder string    `json:"placeholder,omitempty"`
	Help        string    `json:"help,omitempty"`
}

// Config is a configured instance of a connector.
type Config struct {
	ID       string            `yaml:"id" json:"id"`
	Kind     string            `yaml:"kind" json:"kind"`
	Name     string            `yaml:"name" json:"name"`
	Enabled  bool              `yaml:"enabled" json:"enabled"`
	Interval time.Duration     `yaml:"interval" json:"interval"`
	Folder   string            `yaml:"folder,omitempty" json:"folder,omitempty"`
	Values   map[string]string `yaml:"values" json:"values"`
}

// Value returns a setting, or a default.
func (c Config) Value(key, def string) string {
	if v, ok := c.Values[key]; ok && v != "" {
		return v
	}
	return def
}

// Bool reads a boolean setting.
func (c Config) Bool(key string) bool {
	switch c.Values[key] {
	case "true", "yes", "1", "on":
		return true
	}
	return false
}

// SecretFunc resolves a secrets reference such as "bitwarden:github-token".
type SecretFunc func(ctx context.Context, ref string) ([]byte, error)

// SyncInput is everything a connector may use. It gets no vault access and no
// arbitrary HTTP client: the one it is handed refuses private addresses unless
// the integration opted in.
type SyncInput struct {
	Config Config
	State  map[string]string // connector-owned cursor from the previous run
	Since  time.Time
	Now    time.Time
	From   time.Time // calendar window the UI cares about
	To     time.Time
	HTTP   *http.Client
	Secret SecretFunc
}

// SyncOutput is what a connector produces.
type SyncOutput struct {
	Items []ExternalItem
	State map[string]string
}

// Connector is implemented by every integration.
type Connector interface {
	// Kind is the stable identifier used in configuration.
	Kind() string
	// Title is the human-readable name shown in the UI.
	Title() string
	// ConfigSchema drives the settings form.
	ConfigSchema() []ConfigField
	// Validate checks a configuration before it is stored.
	Validate(Config) error
	// Sync fetches whatever changed since the last run.
	Sync(ctx context.Context, in SyncInput) (SyncOutput, error)
}

// Registry holds the connectors this build knows about.
type Registry struct {
	byKind map[string]Connector
	order  []string
}

// NewRegistry returns every built-in connector.
func NewRegistry() *Registry {
	r := &Registry{byKind: map[string]Connector{}}
	r.Register(&ICS{})
	r.Register(&CalDAV{})
	r.Register(&Nextcloud{})
	r.Register(&GitHub{})
	return r
}

func (r *Registry) Register(c Connector) {
	if _, exists := r.byKind[c.Kind()]; !exists {
		r.order = append(r.order, c.Kind())
	}
	r.byKind[c.Kind()] = c
}

func (r *Registry) Get(kind string) (Connector, error) {
	c, ok := r.byKind[kind]
	if !ok {
		return nil, fmt.Errorf("неизвестная интеграция %q", kind)
	}
	return c, nil
}

// Kinds lists the available connectors in registration order.
func (r *Registry) Kinds() []Connector {
	out := make([]Connector, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKind[k])
	}
	return out
}

// requireValue is the check every connector needs for its mandatory settings.
func requireValue(c Config, key, hint string) error {
	if c.Value(key, "") == "" {
		return fmt.Errorf("не заполнено поле %q (%s)", key, hint)
	}
	return nil
}
