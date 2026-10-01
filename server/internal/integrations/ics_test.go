package integrations

import (
	"strings"
	"testing"
	"time"
)

const sampleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//RU
BEGIN:VEVENT
UID:allday-1
SUMMARY:День рождения
DTSTART;VALUE=DATE:20260910
DTEND;VALUE=DATE:20260911
END:VEVENT
BEGIN:VEVENT
UID:timed-1
SUMMARY:Созвон с командой
DESCRIPTION:Обсуждаем формат\nи сроки
LOCATION:Переговорка 3
DTSTART;TZID=Europe/Moscow:20260909T140000
DTEND;TZID=Europe/Moscow:20260909T150000
END:VEVENT
BEGIN:VEVENT
UID:folded-1
SUMMARY:Очень длинное название встречи ко
 торое перенесено на следующую строку
DTSTART:20260911T090000Z
DURATION:PT1H30M
END:VEVENT
BEGIN:VEVENT
UID:weekly-1
SUMMARY:Дейли
DTSTART;TZID=Europe/Moscow:20260907T100000
DTEND;TZID=Europe/Moscow:20260907T101500
RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=6
EXDATE;TZID=Europe/Moscow:20260909T100000
END:VEVENT
END:VCALENDAR
`

func mustParse(t *testing.T, src string) []VEvent {
	t.Helper()
	events, err := ParseICS([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func find(t *testing.T, events []VEvent, uid string) VEvent {
	t.Helper()
	for _, e := range events {
		if e.UID == uid {
			return e
		}
	}
	t.Fatalf("event %q not found", uid)
	return VEvent{}
}

func TestParseICSBasics(t *testing.T) {
	events := mustParse(t, sampleICS)
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}

	allday := find(t, events, "allday-1")
	if !allday.AllDay {
		t.Error("VALUE=DATE must produce an all-day event")
	}
	if allday.Start.Format("2006-01-02") != "2026-09-10" {
		t.Errorf("start = %s", allday.Start)
	}

	timed := find(t, events, "timed-1")
	if timed.AllDay {
		t.Error("a timed event must not be all-day")
	}
	if h := timed.Start.In(time.FixedZone("MSK", 3*3600)).Hour(); h != 14 {
		t.Errorf("TZID was ignored: hour = %d", h)
	}
	if !strings.Contains(timed.Description, "\n") {
		t.Errorf("escaped newline not decoded: %q", timed.Description)
	}
	if timed.Location != "Переговорка 3" {
		t.Errorf("location = %q", timed.Location)
	}
}

func TestParseICSUnfoldsLongLines(t *testing.T) {
	e := find(t, mustParse(t, sampleICS), "folded-1")
	if !strings.Contains(e.Summary, "которое перенесено") {
		t.Errorf("folded line not joined: %q", e.Summary)
	}
	if got := e.End.Sub(e.Start); got != 90*time.Minute {
		t.Errorf("DURATION ignored: %v", got)
	}
}

func TestRecurrenceWithByDayCountAndExdate(t *testing.T) {
	e := find(t, mustParse(t, sampleICS), "weekly-1")
	if e.Rule == nil {
		t.Fatal("RRULE not parsed")
	}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)

	occ := e.Occurrences(from, to)
	// COUNT=6 minus one EXDATE.
	if len(occ) != 5 {
		var got []string
		for _, o := range occ {
			got = append(got, o.Start.Format("2006-01-02"))
		}
		t.Fatalf("occurrences = %v, want 5 (COUNT=6 minus one EXDATE)", got)
	}
	for _, o := range occ {
		switch o.Start.Weekday() {
		case time.Monday, time.Wednesday, time.Friday:
		default:
			t.Errorf("unexpected weekday %s on %s", o.Start.Weekday(), o.Start)
		}
		if d := o.End.Sub(o.Start); d != 15*time.Minute {
			t.Errorf("duration not carried to occurrence: %v", d)
		}
	}
}

func TestRecurrenceRespectsWindow(t *testing.T) {
	src := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:daily-1
SUMMARY:Каждый день
DTSTART:20260101T080000Z
DTEND:20260101T083000Z
RRULE:FREQ=DAILY
END:VEVENT
END:VCALENDAR`
	e := mustParse(t, src)[0]

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	occ := e.Occurrences(from, to)
	if len(occ) != 7 {
		t.Fatalf("occurrences = %d, want 7 for a week-long window", len(occ))
	}
}

func TestRecurrenceUntil(t *testing.T) {
	src := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:until-1
DTSTART:20260901T080000Z
DTEND:20260901T090000Z
RRULE:FREQ=DAILY;UNTIL=20260905T235959Z
END:VEVENT
END:VCALENDAR`
	e := mustParse(t, src)[0]
	occ := e.Occurrences(
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	)
	if len(occ) != 5 {
		t.Fatalf("occurrences = %d, want 5 (1..5 September)", len(occ))
	}
}

func TestParseICSRejectsGarbage(t *testing.T) {
	if _, err := ParseICS([]byte("<html>404 not found</html>")); err == nil {
		t.Fatal("an HTML error page must not parse as a calendar")
	}
}

func TestParseICSDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"PT1H":    time.Hour,
		"PT30M":   30 * time.Minute,
		"PT1H30M": 90 * time.Minute,
		"P2D":     48 * time.Hour,
		"P1W":     7 * 24 * time.Hour,
		"PT45S":   45 * time.Second,
		"-PT1H":   -time.Hour,
	}
	for in, want := range cases {
		got, err := parseICSDuration(in)
		if err != nil || got != want {
			t.Errorf("%s → %v (%v), want %v", in, got, err, want)
		}
	}
}

func TestICSValidate(t *testing.T) {
	ics := &ICS{}
	if err := ics.Validate(Config{Values: map[string]string{}}); err == nil {
		t.Error("url is required")
	}
	if err := ics.Validate(Config{Values: map[string]string{"url": "webcal://example.com/c.ics"}}); err != nil {
		t.Errorf("webcal:// must be accepted: %v", err)
	}
	if err := ics.Validate(Config{Values: map[string]string{"url": "file:///etc/passwd"}}); err == nil {
		t.Error("only http(s) may be fetched")
	}
}
