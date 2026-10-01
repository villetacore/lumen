package passwords

import (
	"context"
	"fmt"
	"strings"
)

// KeePass reads a .kdbx database through keepassxc-cli. Lumen does not parse
// KDBX itself — see ADR-0007 for why.
type KeePass struct{}

func (*KeePass) Kind() string { return "keepass" }

func (*KeePass) Title() string { return "KeePass (KeePassXC)" }

func (*KeePass) ConfigSchema() []Field {
	return []Field{
		{Key: "db", Label: "Файл базы", Type: FieldText, Required: true,
			Placeholder: `D:\keys\personal.kdbx`},
		{Key: "key_file", Label: "Файл-ключ", Type: FieldText,
			Help: "Если база открывается ключом"},
		{Key: "no_password", Label: "Только файл-ключ, без пароля", Type: FieldBool},
		{Key: "cli", Label: "Путь к keepassxc-cli", Type: FieldText,
			Placeholder: "keepassxc-cli",
			Help:        `На Windows обычно C:\Program Files\KeePassXC\keepassxc-cli.exe`},
	}
}

func (*KeePass) Validate(c Config) error {
	if c.Value("db", "") == "" {
		return fmt.Errorf("укажите путь к файлу .kdbx")
	}
	if c.Bool("no_password") && c.Value("key_file", "") == "" {
		return fmt.Errorf("без пароля нужен файл-ключ")
	}
	return nil
}

func (*KeePass) NeedsPassphrase(c Config) bool { return !c.Bool("no_password") }

func (k *KeePass) bin(c Config) string { return c.Value("cli", "keepassxc-cli") }

// baseArgs assembles the flags every keepassxc-cli call needs.
func (k *KeePass) baseArgs(c Config, command string, extra ...string) []string {
	args := []string{command, "--quiet"}
	if kf := c.Value("key_file", ""); kf != "" {
		args = append(args, "--key-file", kf)
	}
	if c.Bool("no_password") {
		args = append(args, "--no-password")
	}
	return append(args, extra...)
}

// stdin is the database password; keepassxc-cli reads it from there when it is
// not attached to a terminal.
func (k *KeePass) stdin(s Session) []byte {
	if len(s.Secret) == 0 {
		return nil
	}
	return append(append([]byte{}, s.Secret...), '\n')
}

// Unlock verifies the password by listing the root group, then keeps the
// password in memory: keepassxc-cli has no session of its own, so every later
// call has to present it again.
func (k *KeePass) Unlock(ctx context.Context, cfg Config, passphrase []byte, r Runner) (Session, error) {
	if k.NeedsPassphrase(cfg) && len(passphrase) == 0 {
		return Session{}, fmt.Errorf("нужен пароль базы")
	}
	session := Session{Secret: passphrase}

	args := k.baseArgs(cfg, "ls", cfg.Value("db", ""))
	if _, err := r.Run(ctx, Input{Name: k.bin(cfg), Args: args, Stdin: k.stdin(session)}); err != nil {
		// keepassxc-cli с --quiet не объясняет причину, а она почти всегда одна.
		return Session{}, fmt.Errorf(
			"не удалось открыть базу %s: неверный пароль, не тот файл-ключ или файл недоступен (%w)",
			cfg.Value("db", ""), err)
	}
	return session, nil
}

func (k *KeePass) List(ctx context.Context, cfg Config, s Session, query string, r Runner) ([]Item, error) {
	db := cfg.Value("db", "")

	var args []string
	if q := strings.TrimSpace(query); q != "" {
		// search обходит базу сам и возвращает только совпадения — дешевле,
		// чем выгружать всё дерево и фильтровать здесь.
		args = k.baseArgs(cfg, "search", db, q)
	} else {
		args = k.baseArgs(cfg, "ls", "--recursive", "--flatten", db)
	}

	out, err := r.Run(ctx, Input{Name: k.bin(cfg), Args: args, Stdin: k.stdin(s)})
	if err != nil {
		if strings.Contains(err.Error(), "No results") {
			return nil, nil
		}
		return nil, fmt.Errorf("keepassxc-cli: %w", err)
	}

	var items []Item
	for _, line := range strings.Split(string(out), "\n") {
		path := strings.TrimRight(strings.TrimSpace(line), "\r")
		// Группы заканчиваются слэшем; записи — нет.
		if path == "" || strings.HasSuffix(path, "/") {
			continue
		}
		name := path
		folder := ""
		if i := strings.LastIndex(path, "/"); i >= 0 {
			name, folder = path[i+1:], path[:i]
		}
		items = append(items, Item{
			ID: path, Name: name, Folder: folder,
			// keepassxc-cli не отдаёт значения списком, поэтому поля
			// перечисляем оптимистично и достаём по запросу.
			Fields: []string{"password", "username", "totp", "notes"},
		})
	}
	return items, nil
}

func (k *KeePass) Reveal(ctx context.Context, cfg Config, s Session, itemID, field string, r Runner) (string, error) {
	attribute, ok := map[string]string{
		"password": "Password",
		"username": "UserName",
		"notes":    "Notes",
		"uri":      "URL",
	}[field]
	if field == "totp" {
		out, err := r.Run(ctx, Input{
			Name:  k.bin(cfg),
			Args:  k.baseArgs(cfg, "show", "--totp", cfg.Value("db", ""), itemID),
			Stdin: k.stdin(s),
		})
		if err != nil {
			return "", fmt.Errorf("keepassxc-cli show --totp: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoField, field)
	}

	out, err := r.Run(ctx, Input{
		Name:  k.bin(cfg),
		Args:  k.baseArgs(cfg, "show", "--show-protected", "--attributes", attribute, cfg.Value("db", ""), itemID),
		Stdin: k.stdin(s),
	})
	if err != nil {
		return "", fmt.Errorf("keepassxc-cli show: %w", err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}
