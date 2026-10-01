package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Nextcloud imports notes from the Nextcloud Notes app. Calendars from the same
// server come through the CalDAV connector; contacts and files are separate
// APIs and are not covered yet.
type Nextcloud struct{}

func (*Nextcloud) Kind() string { return "nextcloud" }

func (*Nextcloud) Title() string { return "Nextcloud Notes" }

func (*Nextcloud) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Адрес Nextcloud", Type: FieldURL, Required: true,
			Placeholder: "https://cloud.example.com"},
		{Key: "username", Label: "Пользователь", Type: FieldText, Required: true},
		{Key: "password", Label: "Пароль приложения", Type: FieldSecret, Required: true,
			Placeholder: "bitwarden:nextcloud-app-password",
			Help: "Ссылка на секрет. В Nextcloud заведите отдельный пароль приложения, " +
				"а не основной пароль учётной записи"},
		{Key: "category", Label: "Только категория", Type: FieldText,
			Help: "Пусто — брать все заметки"},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для сервера в домашней сети"},
	}
}

func (*Nextcloud) Validate(c Config) error {
	for _, f := range []struct{ key, hint string }{
		{"url", "адрес сервера Nextcloud"},
		{"username", "имя пользователя"},
		{"password", "ссылка на секрет с паролем приложения"},
	} {
		if err := requireValue(c, f.key, f.hint); err != nil {
			return err
		}
	}
	u := c.Value("url", "")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("адрес должен начинаться с http:// или https://")
	}
	return nil
}

// ncNote is the subset of the Notes API response Lumen uses.
type ncNote struct {
	ID       int64  `json:"id"`
	Etag     string `json:"etag"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Category string `json:"category"`
	Favorite bool   `json:"favorite"`
	Modified int64  `json:"modified"`
	ReadOnly bool   `json:"readonly"`
}

func (n *Nextcloud) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}

	password, err := in.Secret(ctx, in.Config.Value("password", ""))
	if err != nil {
		return out, fmt.Errorf("пароль: %w", err)
	}

	base := strings.TrimSuffix(in.Config.Value("url", ""), "/")
	endpoint := base + "/index.php/apps/notes/api/v1/notes?exclude=content"
	// Запрашиваем содержимое: без него заметка в vault будет пустой.
	endpoint = strings.TrimSuffix(endpoint, "?exclude=content")
	if cat := in.Config.Value("category", ""); cat != "" {
		endpoint += "?category=" + cat
	}

	headers := map[string]string{
		"OCS-APIRequest": "true",
		"Accept":         "application/json",
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString(
			[]byte(in.Config.Value("username", "")+":"+string(password))),
	}
	// Условный запрос: Notes отдаёт ETag на коллекцию.
	if etag := in.State["etag"]; etag != "" {
		headers["If-None-Match"] = etag
	}

	body, resp, err := Fetch(ctx, in.HTTP, endpoint, headers)
	if err != nil {
		return out, err
	}
	if resp != nil {
		if v := resp.Header.Get("ETag"); v != "" {
			out.State["etag"] = v
		}
		if resp.StatusCode == http.StatusNotModified {
			return out, ErrNotModified
		}
	}

	var notes []ncNote
	if err := json.Unmarshal(body, &notes); err != nil {
		return out, fmt.Errorf("не удалось разобрать ответ Nextcloud Notes: %w", err)
	}

	for _, nc := range notes {
		item := ExternalItem{
			ExternalID: "nextcloud:" + strconv.FormatInt(nc.ID, 10),
			Kind:       ItemNote,
			Title:      nc.Title,
			Body:       nc.Content,
			URL:        fmt.Sprintf("%s/index.php/apps/notes/note/%d", base, nc.ID),
		}
		if nc.Category != "" {
			item.Labels = append(item.Labels, slugTag(nc.Category))
		}
		if nc.Favorite {
			item.Labels = append(item.Labels, "favorite")
		}
		// Etag меняется вместе с содержимым — это и есть признак изменения.
		item.Hash = nc.Etag
		if item.Hash == "" {
			item.Hash = hashItem(item) + strconv.FormatInt(nc.Modified, 10)
		}
		out.Items = append(out.Items, item)
	}

	out.State["notes"] = strconv.Itoa(len(out.Items))
	out.State["synced_at"] = in.Now.Format(time.RFC3339)
	return out, nil
}
