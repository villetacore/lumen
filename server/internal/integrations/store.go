package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigFile travels with the vault, so the same integrations follow it to
	// another machine.
	ConfigFile = ".lumen/integrations.yaml"
	// StateFile holds cursors and error messages. It lives in the cache, which
	// is never synced: a stale ETag on another machine is worse than useless.
	StateFile = ".lumen/cache/integrations-state.json"

	DefaultInterval = 15 * time.Minute
	MinInterval     = time.Minute
)

// State is what one integration remembers between runs.
type State struct {
	Cursor    map[string]string `json:"cursor,omitempty"`
	LastSync  time.Time         `json:"last_sync,omitempty"`
	LastError string            `json:"last_error,omitempty"`
	LastItems int               `json:"last_items,omitempty"`
}

// Store persists integration configuration and state inside the vault.
type Store struct {
	root string
}

func NewStore(vaultRoot string) *Store { return &Store{root: vaultRoot} }

func (s *Store) configPath() string {
	return filepath.Join(s.root, filepath.FromSlash(ConfigFile))
}

func (s *Store) statePath() string {
	return filepath.Join(s.root, filepath.FromSlash(StateFile))
}

// yamlFile mirrors the on-disk shape; Interval is a string so the file stays
// readable ("15m" rather than 900000000000).
type yamlFile struct {
	Integrations []yamlEntry `yaml:"integrations"`
}

type yamlEntry struct {
	ID       string            `yaml:"id"`
	Kind     string            `yaml:"kind"`
	Name     string            `yaml:"name"`
	Enabled  bool              `yaml:"enabled"`
	Interval string            `yaml:"interval,omitempty"`
	Folder   string            `yaml:"folder,omitempty"`
	Values   map[string]string `yaml:"values,omitempty"`
}

// Load reads the configured integrations. A missing file is not an error.
func (s *Store) Load() ([]Config, error) {
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file yamlFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}

	out := make([]Config, 0, len(file.Integrations))
	for _, e := range file.Integrations {
		interval := DefaultInterval
		if e.Interval != "" {
			if d, err := time.ParseDuration(e.Interval); err == nil {
				interval = d
			}
		}
		if interval < MinInterval {
			interval = MinInterval
		}
		if e.Values == nil {
			e.Values = map[string]string{}
		}
		out = append(out, Config{
			ID: e.ID, Kind: e.Kind, Name: e.Name, Enabled: e.Enabled,
			Interval: interval, Folder: e.Folder, Values: e.Values,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Save writes the whole list atomically.
func (s *Store) Save(configs []Config) error {
	file := yamlFile{Integrations: make([]yamlEntry, 0, len(configs))}
	for _, c := range configs {
		file.Integrations = append(file.Integrations, yamlEntry{
			ID: c.ID, Kind: c.Kind, Name: c.Name, Enabled: c.Enabled,
			Interval: c.Interval.String(), Folder: c.Folder, Values: c.Values,
		})
	}
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	header := "# Интеграции Lumen. Значения полей-секретов — ссылки вида\n" +
		"# env:VAR, file:ПУТЬ, command:..., bitwarden:ЗАПИСЬ, keepass:БАЗА#ЗАПИСЬ,\n" +
		"# а не сами токены.\n"
	return writeFileAtomic(s.configPath(), append([]byte(header), data...), 0o600)
}

// LoadState reads cursors; a missing or corrupt file just means "start fresh".
func (s *Store) LoadState() map[string]State {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		return map[string]State{}
	}
	var out map[string]State
	if err := json.Unmarshal(data, &out); err != nil || out == nil {
		return map[string]State{}
	}
	return out
}

func (s *Store) SaveState(state map[string]State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statePath(), data, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// cyrillic transliterates Russian names so an identifier stays ASCII. Ids end
// up in URLs, folder names and YAML keys on three platforms; tags keep their
// original alphabet, identifiers do not.
var cyrillic = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// slugID builds an ASCII identifier from any name.
func slugID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		default:
			if tr, ok := cyrillic[r]; ok {
				b.WriteString(tr)
			} else {
				b.WriteRune('-')
			}
		}
	}
	// Collapse the dashes that separators and dropped characters leave behind.
	parts := strings.FieldsFunc(b.String(), func(r rune) bool { return r == '-' })
	return strings.Join(parts, "-")
}

// NewID builds a short, readable identifier from a name, unique among existing.
func NewID(name string, taken []Config) string {
	base := slugID(name)
	if base == "" {
		base = "integration"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	exists := func(id string) bool {
		for _, c := range taken {
			if c.ID == id {
				return true
			}
		}
		return false
	}
	if !exists(base) {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists(candidate) {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

// DefaultFolder is where a connector's notes land when nothing else is set.
func DefaultFolder(c Config) string {
	if c.Folder != "" {
		return strings.Trim(c.Folder, "/")
	}
	return "70-integrations/" + c.ID
}
