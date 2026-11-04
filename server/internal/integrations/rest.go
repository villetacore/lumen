package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// REST reads any service with a JSON API: the settings say where the list is and
// which fields mean what. It covers what has no connector of its own.
type REST struct{}

func (*REST) Kind() string { return "rest" }

func (*REST) Title() string { return "Любой сервис с JSON API" }

func (*REST) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Адрес списка", Type: FieldURL, Required: true,
			Placeholder: "https://api.example.com/v1/tickets?status=open"},
		{Key: "token", Label: "Токен", Type: FieldSecret, Help: secretHelp},
		{Key: "auth_header", Label: "Заголовок токена", Type: FieldText,
			Placeholder: "Authorization", Help: "Для другого заголовка, например X-Api-Key, токен отправляется как есть, без Bearer"},
		{Key: "items_path", Label: "Где в ответе список", Type: FieldText,
			Placeholder: "data.items", Help: "Через точку. Пусто — ответ сам является списком"},
		{Key: "id_field", Label: "Поле с идентификатором", Type: FieldText, Required: true, Placeholder: "id"},
		{Key: "title_field", Label: "Поле с названием", Type: FieldText, Required: true, Placeholder: "title"},
		{Key: "body_field", Label: "Поле с описанием", Type: FieldText, Placeholder: "description"},
		{Key: "url_field", Label: "Поле со ссылкой", Type: FieldText, Placeholder: "url"},
		{Key: "due_field", Label: "Поле со сроком", Type: FieldText, Placeholder: "due_date"},
		{Key: "labels_field", Label: "Поле с метками", Type: FieldText, Placeholder: "tags"},
		{Key: "done_field", Label: "Поле готовности", Type: FieldText, Placeholder: "status",
			Help: "Если значение совпало с полем ниже или равно true, задача выполнена"},
		{Key: "done_value", Label: "Значение «готово»", Type: FieldText, Placeholder: "closed"},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для сервиса во внутренней сети"},
	}
}

func (*REST) Validate(c Config) error {
	u, err := url.Parse(c.Value("url", ""))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("адрес указывается как https://api.example.com/…")
	}
	if err := requireValue(c, "id_field", "поле с идентификатором"); err != nil {
		return err
	}
	return requireValue(c, "title_field", "поле с названием")
}

// restPages bounds one run; a service with more items should be filtered in the URL.
const restPages = 50

var linkNext = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

func (r *REST) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}
	cfg := in.Config
	headers := map[string]string{"Accept": "application/json"}
	if ref := cfg.Value("token", ""); ref != "" {
		token, err := in.Secret(ctx, ref)
		if err != nil {
			return out, fmt.Errorf("токен: %w", err)
		}
		value := strings.TrimSpace(string(token))
		name := cfg.Value("auth_header", "Authorization")
		if strings.EqualFold(name, "Authorization") {
			value = "Bearer " + value
		}
		headers[name] = value
	}

	base, err := url.Parse(cfg.Value("url", ""))
	if err != nil {
		return out, err
	}
	next := base.String()
	for page := 0; page < restPages && next != ""; page++ {
		body, resp, err := Fetch(ctx, in.HTTP, next, headers)
		if err != nil {
			return SyncOutput{State: in.State}, err
		}
		var doc any
		if err := json.Unmarshal(body, &doc); err != nil {
			return SyncOutput{State: in.State}, fmt.Errorf("ответ — не JSON: %w", err)
		}
		list, ok := jsonPath(doc, cfg.Value("items_path", "")).([]any)
		if !ok {
			return SyncOutput{State: in.State}, fmt.Errorf("по пути %q нет списка", cfg.Value("items_path", ""))
		}
		for _, raw := range list {
			if item, ok := restItem(cfg, raw); ok {
				out.Items = append(out.Items, item)
			}
		}
		next = ""
		if m := linkNext.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
			// Stay on the same host: the token must not follow a link elsewhere.
			if u, err := base.Parse(m[1]); err == nil && u.Host == base.Host {
				next = u.String()
			}
		}
	}
	return out, nil
}

func restItem(cfg Config, raw any) (ExternalItem, bool) {
	text := func(key string) string {
		field := cfg.Value(key, "")
		if field == "" {
			return ""
		}
		return jsonText(jsonPath(raw, field))
	}
	id, title := text("id_field"), text("title_field")
	if id == "" || title == "" {
		return ExternalItem{}, false
	}
	item := ExternalItem{ExternalID: id, Kind: ItemTask, Title: title, Body: text("body_field"), URL: text("url_field")}
	if done := text("done_field"); done != "" {
		want := cfg.Value("done_value", "")
		item.Done = strings.EqualFold(done, "true") || (want != "" && strings.EqualFold(done, want))
	}
	if due := text("due_field"); due != "" {
		item.Due = parseAnyDate(due)
	}
	if field := cfg.Value("labels_field", ""); field != "" {
		if list, ok := jsonPath(raw, field).([]any); ok {
			for _, l := range list {
				if s := slugTag(jsonText(l)); s != "" {
					item.Labels = append(item.Labels, s)
				}
			}
		}
	}
	item.Hash = hashItem(item) + boolMark(item.Done)
	return item, true
}

// jsonPath walks "a.b.0.c" through decoded JSON; nil when the path is not there.
func jsonPath(v any, path string) any {
	if path == "" {
		return v
	}
	for _, part := range strings.Split(path, ".") {
		switch node := v.(type) {
		case map[string]any:
			v = node[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil
			}
			v = node[i]
		default:
			return nil
		}
	}
	return v
}

func jsonText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func parseAnyDate(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Local()
		}
	}
	return time.Time{}
}
