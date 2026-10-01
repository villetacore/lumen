package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	// The tz database is embedded so TZID=Europe/Moscow works on a bare Windows
	// machine, where Go would otherwise find no zoneinfo at all.
	_ "time/tzdata"
)

// ICS subscribes to an iCalendar feed: the "secret address in iCal format"
// that Google Calendar, Outlook and Yandex all expose. Read-only by design —
// two-way sync is CalDAV's job.
type ICS struct{}

func (*ICS) Kind() string { return "ics" }

func (*ICS) Title() string { return "Календарь ICS (Google, Outlook, Яндекс)" }

func (*ICS) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "url", Label: "Ссылка на .ics", Type: FieldURL, Required: true,
			Placeholder: "https://calendar.google.com/calendar/ical/…/basic.ics",
			Help:        "В Google Calendar: Настройки календаря → Секретный адрес в формате iCal"},
		{Key: "calendar", Label: "Название", Type: FieldText,
			Help: "Как подписывать события в календаре Lumen"},
		{Key: "allow_private", Label: "Разрешить приватные адреса", Type: FieldBool,
			Help: "Нужно для календаря во внутренней сети или на localhost"},
	}
}

func (*ICS) Validate(c Config) error {
	if err := requireValue(c, "url", "ссылка на .ics"); err != nil {
		return err
	}
	u := normalizeICSURL(c.Value("url", ""))
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("ссылка должна начинаться с http:// или https:// (webcal:// подставляется автоматически)")
	}
	return nil
}

// normalizeICSURL turns the webcal:// scheme calendars advertise into https://.
func normalizeICSURL(u string) string {
	u = strings.TrimSpace(u)
	if rest, ok := strings.CutPrefix(u, "webcal://"); ok {
		return "https://" + rest
	}
	return u
}

func (ics *ICS) Sync(ctx context.Context, in SyncInput) (SyncOutput, error) {
	url := normalizeICSURL(in.Config.Value("url", ""))
	out := SyncOutput{State: map[string]string{}}

	// Conditional GET: a calendar polled every 15 minutes is almost always
	// unchanged, and 304 costs nothing.
	headers := map[string]string{}
	if etag := in.State["etag"]; etag != "" {
		headers["If-None-Match"] = etag
	}
	if lm := in.State["last_modified"]; lm != "" {
		headers["If-Modified-Since"] = lm
	}

	body, resp, err := Fetch(ctx, in.HTTP, url, headers)
	if err != nil {
		return out, err
	}
	if resp != nil {
		if v := resp.Header.Get("ETag"); v != "" {
			out.State["etag"] = v
		}
		if v := resp.Header.Get("Last-Modified"); v != "" {
			out.State["last_modified"] = v
		}
		if resp.StatusCode == 304 {
			out.State["unchanged"] = "true"
			return out, ErrNotModified
		}
	}

	events, err := ParseICS(body)
	if err != nil {
		return out, err
	}

	from, to := in.From, in.To
	if from.IsZero() {
		from = in.Now.AddDate(0, -1, 0)
	}
	if to.IsZero() {
		to = in.Now.AddDate(0, 6, 0)
	}
	calendar := in.Config.Value("calendar", in.Config.Name)

	for _, e := range events {
		for _, occ := range e.Occurrences(from, to) {
			item := ExternalItem{
				ExternalID: e.UID + "@" + occ.Start.Format(time.RFC3339),
				Kind:       ItemEvent,
				Title:      e.Summary,
				Body:       e.Description,
				URL:        e.URL,
				Location:   e.Location,
				Start:      occ.Start,
				End:        occ.End,
				AllDay:     e.AllDay,
				Labels:     nil,
			}
			if calendar != "" {
				item.Labels = []string{calendar}
			}
			item.Hash = hashItem(item)
			out.Items = append(out.Items, item)
		}
	}
	out.State["events"] = strconv.Itoa(len(out.Items))
	return out, nil
}

// ErrNotModified tells the scheduler that nothing changed upstream.
var ErrNotModified = fmt.Errorf("не изменилось")

func hashItem(i ExternalItem) string {
	h := sha256.New()
	for _, part := range []string{i.ExternalID, i.Title, i.Body, i.Location, i.URL,
		i.Start.Format(time.RFC3339), i.End.Format(time.RFC3339)} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// VEvent is a parsed VEVENT, including its recurrence rule.
type VEvent struct {
	UID         string
	Summary     string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Rule        *RRule
	ExDates     map[int64]bool
}

// Occurrence is one concrete instance of an event.
type Occurrence struct{ Start, End time.Time }

// maxOccurrences bounds the expansion of an open-ended recurrence rule.
const maxOccurrences = 750

// Occurrences expands the event inside [from, to).
func (e VEvent) Occurrences(from, to time.Time) []Occurrence {
	duration := e.End.Sub(e.Start)
	if duration < 0 {
		duration = 0
	}
	emit := func(start time.Time) Occurrence {
		return Occurrence{Start: start, End: start.Add(duration)}
	}

	if e.Rule == nil {
		if e.Start.Before(from) || !e.Start.Before(to) {
			return nil
		}
		return []Occurrence{emit(e.Start)}
	}

	var out []Occurrence
	count := 0
	for _, start := range e.Rule.expand(e.Start, from, to) {
		if e.ExDates[start.Unix()] {
			continue
		}
		out = append(out, emit(start))
		count++
		if count >= maxOccurrences {
			break
		}
	}
	return out
}

// RRule is the subset of RFC 5545 recurrence that ordinary calendars use.
// BYSETPOS, BYMONTHDAY and RECURRENCE-ID overrides are not supported; such
// events simply appear at their base times.
type RRule struct {
	Freq     string // DAILY | WEEKLY | MONTHLY | YEARLY
	Interval int
	Count    int
	Until    time.Time
	ByDay    []time.Weekday
}

func (r *RRule) expand(dtstart, from, to time.Time) []time.Time {
	interval := r.Interval
	if interval < 1 {
		interval = 1
	}
	var out []time.Time
	emitted := 0

	add := func(t time.Time) bool {
		if !r.Until.IsZero() && t.After(r.Until) {
			return false
		}
		if r.Count > 0 && emitted >= r.Count {
			return false
		}
		emitted++
		if !t.Before(from) && t.Before(to) {
			out = append(out, t)
		}
		return true
	}

	for i := 0; i < maxOccurrences; i++ {
		var base time.Time
		switch strings.ToUpper(r.Freq) {
		case "DAILY":
			base = dtstart.AddDate(0, 0, i*interval)
		case "WEEKLY":
			base = dtstart.AddDate(0, 0, i*interval*7)
		case "MONTHLY":
			base = dtstart.AddDate(0, i*interval, 0)
		case "YEARLY":
			base = dtstart.AddDate(i*interval, 0, 0)
		default:
			return out
		}
		if base.After(to) && emitted > 0 {
			break
		}

		if strings.EqualFold(r.Freq, "WEEKLY") && len(r.ByDay) > 0 {
			// Walk the week the occurrence starts in and emit each listed day.
			weekStart := base.AddDate(0, 0, -int((base.Weekday()+6)%7))
			for _, wd := range r.ByDay {
				day := weekStart.AddDate(0, 0, int((wd+6)%7))
				t := time.Date(day.Year(), day.Month(), day.Day(),
					base.Hour(), base.Minute(), base.Second(), 0, base.Location())
				if t.Before(dtstart) {
					continue
				}
				if !add(t) {
					return out
				}
			}
			continue
		}
		if !add(base) {
			return out
		}
	}
	return out
}

// ParseICS turns an iCalendar document into events.
func ParseICS(data []byte) ([]VEvent, error) {
	lines := unfold(string(data))
	var (
		events  []VEvent
		current *VEvent
		depth   []string
	)

	for _, line := range lines {
		name, params, value := splitProperty(line)
		upper := strings.ToUpper(name)

		switch upper {
		case "BEGIN":
			depth = append(depth, strings.ToUpper(value))
			if strings.EqualFold(value, "VEVENT") {
				current = &VEvent{ExDates: map[int64]bool{}}
			}
			continue
		case "END":
			if len(depth) > 0 {
				depth = depth[:len(depth)-1]
			}
			if strings.EqualFold(value, "VEVENT") && current != nil {
				if current.UID == "" {
					current.UID = fmt.Sprintf("nouid-%d", len(events))
				}
				if current.End.IsZero() {
					current.End = defaultEnd(*current)
				}
				if !current.Start.IsZero() {
					events = append(events, *current)
				}
				current = nil
			}
			continue
		}
		if current == nil {
			continue // properties of VCALENDAR or VTIMEZONE
		}

		switch upper {
		case "UID":
			current.UID = value
		case "SUMMARY":
			current.Summary = unescape(value)
		case "DESCRIPTION":
			current.Description = unescape(value)
		case "LOCATION":
			current.Location = unescape(value)
		case "URL":
			current.URL = value
		case "DTSTART":
			t, allDay, err := parseICSTime(params, value)
			if err == nil {
				current.Start, current.AllDay = t, allDay
			}
		case "DTEND":
			if t, _, err := parseICSTime(params, value); err == nil {
				current.End = t
			}
		case "DURATION":
			if d, err := parseICSDuration(value); err == nil && !current.Start.IsZero() {
				current.End = current.Start.Add(d)
			}
		case "RRULE":
			current.Rule = parseRRule(value)
		case "EXDATE":
			for _, part := range strings.Split(value, ",") {
				if t, _, err := parseICSTime(params, part); err == nil {
					current.ExDates[t.Unix()] = true
				}
			}
		}
	}
	if len(events) == 0 && !strings.Contains(strings.ToUpper(string(data)), "BEGIN:VCALENDAR") {
		return nil, fmt.Errorf("это не iCalendar: нет BEGIN:VCALENDAR")
	}
	return events, nil
}

func defaultEnd(e VEvent) time.Time {
	if e.AllDay {
		return e.Start.AddDate(0, 0, 1)
	}
	return e.Start.Add(time.Hour)
}

// unfold joins continuation lines, which iCalendar marks with a leading space
// or tab, and drops empty lines.
func unfold(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for _, line := range raw {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitProperty parses "NAME;PARAM=VALUE:content".
func splitProperty(line string) (name string, params map[string]string, value string) {
	params = map[string]string{}
	colon := indexUnquoted(line, ':')
	if colon < 0 {
		return line, params, ""
	}
	head, value := line[:colon], line[colon+1:]

	parts := splitUnquoted(head, ';')
	name = parts[0]
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		params[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return name, params, value
}

// indexUnquoted finds a separator outside of a quoted parameter value.
func indexUnquoted(s string, sep byte) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case sep:
			if !inQuote {
				return i
			}
		}
	}
	return -1
}

func splitUnquoted(s string, sep byte) []string {
	var out []string
	start, inQuote := 0, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case sep:
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}

// parseICSTime handles the three shapes calendars emit: a bare date, UTC with a
// trailing Z, and a local time qualified by TZID.
func parseICSTime(params map[string]string, value string) (time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false, fmt.Errorf("пустая дата")
	}
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	if strings.EqualFold(params["VALUE"], "DATE") || len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, loc)
		return t, true, err
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC)
		return t.Local(), false, err
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	return t, false, err
}

// parseICSDuration reads the RFC 5545 duration form, e.g. "PT1H30M" or "P2D".
func parseICSDuration(v string) (time.Duration, error) {
	v = strings.TrimSpace(strings.ToUpper(v))
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	if !strings.HasPrefix(v, "P") {
		return 0, fmt.Errorf("не длительность: %q", v)
	}
	v = v[1:]

	var total time.Duration
	num := ""
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			num = ""
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, fmt.Errorf("не длительность: %q", v)
			}
			num = ""
			switch r {
			case 'W':
				total += time.Duration(n) * 7 * 24 * time.Hour
			case 'D':
				total += time.Duration(n) * 24 * time.Hour
			case 'H':
				total += time.Duration(n) * time.Hour
			case 'M':
				total += time.Duration(n) * time.Minute
			case 'S':
				total += time.Duration(n) * time.Second
			}
		}
	}
	if neg {
		total = -total
	}
	return total, nil
}

var weekdays = map[string]time.Weekday{
	"MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday, "SU": time.Sunday,
}

func parseRRule(v string) *RRule {
	r := &RRule{Interval: 1}
	for _, part := range strings.Split(v, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "FREQ":
			r.Freq = strings.ToUpper(strings.TrimSpace(value))
		case "INTERVAL":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
				r.Interval = n
			}
		case "COUNT":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				r.Count = n
			}
		case "UNTIL":
			if t, _, err := parseICSTime(map[string]string{}, value); err == nil {
				r.Until = t
			}
		case "BYDAY":
			for _, d := range strings.Split(value, ",") {
				// Strip an ordinal prefix such as "2TH"; it is not supported,
				// but the weekday still is.
				d = strings.TrimLeft(strings.ToUpper(strings.TrimSpace(d)), "+-0123456789")
				if wd, ok := weekdays[d]; ok {
					r.ByDay = append(r.ByDay, wd)
				}
			}
		}
	}
	if r.Freq == "" {
		return nil
	}
	return r
}
