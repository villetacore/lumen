package passwords

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Bitwarden talks to the official `bw` CLI, which serves both bitwarden.com and
// a self-hosted Vaultwarden — the two differ only by `bw config server`.
type Bitwarden struct{}

func (*Bitwarden) Kind() string { return "bitwarden" }

func (*Bitwarden) Title() string { return "Bitwarden / Vaultwarden" }

func (*Bitwarden) ConfigSchema() []Field {
	return []Field{
		{Key: "bw", Label: "Путь к bw", Type: FieldText, Placeholder: "bw",
			Help: "CLI ставится как npm install -g @bitwarden/cli"},
		{Key: "session", Label: "Готовая сессия", Type: FieldSecret,
			Placeholder: "env:BW_SESSION",
			Help:        "Ссылка на секрет с BW_SESSION. Если заполнено, мастер-пароль спрашиваться не будет"},
		{Key: "server", Label: "Адрес сервера", Type: FieldURL,
			Placeholder: "https://vault.example.com",
			Help:        "Только для справки: адрес задаётся один раз командой bw config server"},
	}
}

func (*Bitwarden) Validate(Config) error { return nil }

// NeedsPassphrase is false when the configuration already points at a session.
func (*Bitwarden) NeedsPassphrase(cfg Config) bool { return cfg.Value("session", "") == "" }

func (b *Bitwarden) bin(cfg Config) string { return cfg.Value("bw", "bw") }

// Unlock either accepts a ready session token or asks `bw` to produce one from
// the master password. The password goes through the environment, never
// through the command line, which any user on the machine can read.
func (b *Bitwarden) Unlock(ctx context.Context, cfg Config, passphrase []byte, r Runner) (Session, error) {
	if len(passphrase) == 0 {
		return Session{}, fmt.Errorf("нужен мастер-пароль или ссылка на BW_SESSION")
	}

	token := string(passphrase)
	if b.NeedsPassphrase(cfg) {
		out, err := r.Run(ctx, Input{
			Name: b.bin(cfg),
			Args: []string{"unlock", "--raw", "--passwordenv", "LUMEN_BW_PASSWORD", "--nointeraction"},
			Env:  map[string]string{"LUMEN_BW_PASSWORD": string(passphrase)},
		})
		if err != nil {
			return Session{}, fmt.Errorf("bw unlock: %w", err)
		}
		token = strings.TrimSpace(string(out))
		if token == "" {
			return Session{}, fmt.Errorf("bw unlock не вернул сессию — возможно, не выполнен bw login")
		}
	}

	// Убедимся, что сессия действительно рабочая, а не просто непустая строка.
	status, err := r.Run(ctx, Input{
		Name: b.bin(cfg),
		Args: []string{"status", "--session", token, "--nointeraction"},
	})
	if err != nil {
		return Session{}, fmt.Errorf("bw status: %w", err)
	}
	var st struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(status, &st); err == nil && st.Status != "" && st.Status != "unlocked" {
		return Session{}, fmt.Errorf("хранилище в состоянии %q; выполните bw login и bw unlock", st.Status)
	}
	return Session{Token: token}, nil
}

// bwItem is the subset of `bw list items` Lumen reads. Secret values are
// deliberately not copied into Item.
type bwItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FolderID string `json:"folderId"`
	Notes    string `json:"notes"`
	Login    *struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		URIs     []struct {
			URI string `json:"uri"`
		} `json:"uris"`
	} `json:"login"`
}

func (b *Bitwarden) List(ctx context.Context, cfg Config, s Session, query string, r Runner) ([]Item, error) {
	args := []string{"list", "items", "--session", s.Token, "--nointeraction"}
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, "--search", q)
	}
	out, err := r.Run(ctx, Input{Name: b.bin(cfg), Args: args})
	if err != nil {
		return nil, fmt.Errorf("bw list: %w", err)
	}

	var raw []bwItem
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("не удалось разобрать ответ bw: %w", err)
	}

	items := make([]Item, 0, len(raw))
	for _, it := range raw {
		item := Item{ID: it.ID, Name: it.Name}
		if it.Notes != "" {
			item.Fields = append(item.Fields, "notes")
		}
		if it.Login != nil {
			item.Username = it.Login.Username
			if len(it.Login.URIs) > 0 {
				item.URI = it.Login.URIs[0].URI
			}
			if it.Login.Username != "" {
				item.Fields = append(item.Fields, "username")
			}
			if it.Login.Password != "" {
				item.Fields = append(item.Fields, "password")
			}
			if it.Login.TOTP != "" {
				item.Fields = append(item.Fields, "totp")
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (b *Bitwarden) Reveal(ctx context.Context, cfg Config, s Session, itemID, field string, r Runner) (string, error) {
	switch field {
	case "password", "username", "totp", "notes", "uri":
	default:
		return "", fmt.Errorf("%w: %s", ErrNoField, field)
	}
	out, err := r.Run(ctx, Input{
		Name: b.bin(cfg),
		Args: []string{"get", field, itemID, "--session", s.Token, "--nointeraction", "--raw"},
	})
	if err != nil {
		return "", fmt.Errorf("bw get %s: %w", field, err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}
