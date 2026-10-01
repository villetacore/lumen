package integrations

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CalDAV reads a calendar collection from Nextcloud, Radicale, Baikal, Fastmail
// or iCloud. It is read-only for now: writing events back needs conflict
// handling that belongs with the sync work in M2.
type CalDAV struct{}

func (*CalDAV) Kind() string { return "caldav" }

func (*CalDAV) Title() string { return "CalDAV (Nextcloud, Radicale, Baikal)" }

func (*CalDAV) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Адрес календаря", Type: FieldURL, Required: true,
			Placeholder: "https://cloud.example.com/remote.php/dav/calendars/user/personal/"},
		{Key: "username", Label: "Пользователь", Type: FieldText, Required: true},
		{Key: "password", Label: "Пароль", Type: FieldSecret, Required: true,
			Placeholder: "bitwarden:nextcloud-app-password",
			Help:        "Ссылка на секрет; для Nextcloud лучше отдельный пароль приложения"},
		{Key: "calendar", Label: "Название", Type: FieldText},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для сервера в домашней сети"},
	}
}

func (*CalDAV) Validate(c Config) error {
	for _, f := range []struct{ key, hint string }{
		{"url", "адрес коллекции календаря"},
		{"username", "имя пользователя"},
		{"password", "ссылка на секрет с паролем"},
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

// multistatus is the WebDAV response envelope.
type multistatus struct {
	XMLName   xml.Name      `xml:"multistatus"`
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string        `xml:"href"`
	PropStat []davPropStat `xml:"propstat"`
}

type davPropStat struct {
	Status string  `xml:"status"`
	Prop   davProp `xml:"prop"`
}

type davProp struct {
	CalendarData string `xml:"calendar-data"`
	ETag         string `xml:"getetag"`
}

// calendarQuery asks the server for every VEVENT in a time range, with the
// iCalendar data inline — one request instead of one GET per event.
const calendarQuery = `<?xml version="1.0" encoding="utf-8" ?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/><c:calendar-data/></d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="%s" end="%s"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`

func (cd *CalDAV) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	out := SyncOutput{State: map[string]string{}}

	password, err := in.Secret(ctx, in.Config.Value("password", ""))
	if err != nil {
		return out, fmt.Errorf("пароль: %w", err)
	}

	from, to := in.From, in.To
	if from.IsZero() {
		from = in.Now.AddDate(0, -1, 0)
	}
	if to.IsZero() {
		to = in.Now.AddDate(0, 6, 0)
	}
	payload := fmt.Sprintf(calendarQuery,
		from.UTC().Format("20060102T150405Z"), to.UTC().Format("20060102T150405Z"))

	req, err := http.NewRequestWithContext(ctx, "REPORT",
		in.Config.Value("url", ""), strings.NewReader(payload))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	req.Header.Set("User-Agent", "Lumen/0.1")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(in.Config.Value("username", "")+":"+string(password))))

	resp, err := in.HTTP.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return out, fmt.Errorf("CalDAV ответил %s: %s", resp.Status, strings.TrimSpace(string(preview)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return out, err
	}

	var ms multistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return out, fmt.Errorf("не удалось разобрать ответ CalDAV: %w", err)
	}

	calendar := in.Config.Value("calendar", in.Config.Name)
	seen := map[string]bool{}

	for _, r := range ms.Responses {
		for _, ps := range r.PropStat {
			if ps.Prop.CalendarData == "" {
				continue
			}
			events, err := ParseICS([]byte(ps.Prop.CalendarData))
			if err != nil {
				continue // one broken event must not kill the whole sync
			}
			for _, e := range events {
				for _, occ := range e.Occurrences(from, to) {
					item := ExternalItem{
						ExternalID: e.UID + "@" + occ.Start.Format(time.RFC3339),
						Kind:       ItemEvent,
						Title:      e.Summary,
						Body:       e.Description,
						Location:   e.Location,
						URL:        e.URL,
						Start:      occ.Start,
						End:        occ.End,
						AllDay:     e.AllDay,
					}
					if seen[item.ExternalID] {
						continue
					}
					seen[item.ExternalID] = true
					if calendar != "" {
						item.Labels = []string{calendar}
					}
					item.Hash = hashItem(item)
					out.Items = append(out.Items, item)
				}
			}
		}
	}
	return out, nil
}
