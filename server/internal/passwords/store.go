package passwords

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigFile lists the password stores. It contains paths and secret
// references only — no passwords, no session tokens.
const ConfigFile = ".lumen/passwords.yaml"

// Store persists the configuration inside the vault.
type Store struct{ root string }

func NewStore(vaultRoot string) *Store { return &Store{root: vaultRoot} }

func (s *Store) path() string {
	return filepath.Join(s.root, filepath.FromSlash(ConfigFile))
}

type yamlFile struct {
	Stores []Config `yaml:"stores"`
}

func (s *Store) Load() ([]Config, error) {
	data, err := os.ReadFile(s.path())
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
	for i := range file.Stores {
		if file.Stores[i].Values == nil {
			file.Stores[i].Values = map[string]string{}
		}
	}
	sort.Slice(file.Stores, func(i, j int) bool { return file.Stores[i].ID < file.Stores[j].ID })
	return file.Stores, nil
}

func (s *Store) Save(configs []Config) error {
	data, err := yaml.Marshal(yamlFile{Stores: configs})
	if err != nil {
		return err
	}
	header := "# Хранилища паролей. Здесь только пути и ссылки на секреты —\n" +
		"# ни мастер-паролей, ни сессий Lumen на диск не пишет.\n"
	path := s.path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(header), data...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NewID makes a unique ASCII identifier from a name.
func NewID(name string, taken []Config) string {
	base := slugID(name)
	if base == "" {
		base = "store"
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
		if candidate := fmt.Sprintf("%s-%d", base, i); !exists(candidate) {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

var cyrillic = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

func slugID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if tr, ok := cyrillic[r]; ok {
				b.WriteString(tr)
			} else {
				b.WriteRune('-')
			}
		}
	}
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == '-' }), "-")
}

// commandTimeout bounds an external CLI so a prompt cannot hang the server.
const commandTimeout = 45 * time.Second

// ExecRunner runs the real command-line tools.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, in Input) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, in.Name, in.Args...)
	if in.Stdin != nil {
		cmd.Stdin = bytes.NewReader(in.Stdin)
	} else {
		cmd.Stdin = strings.NewReader("")
	}
	// Секреты передаются окружением: аргументы процесса видны всей системе.
	if len(in.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range in.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: превышено время ожидания", in.Name)
		}
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return nil, fmt.Errorf("%s: %s", in.Name, firstLines(msg, 3))
		}
		return nil, fmt.Errorf("%s: %w", in.Name, err)
	}
	return out.Bytes(), nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, "; "))
}
