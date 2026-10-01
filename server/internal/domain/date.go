package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Date is a calendar date with an optional time-of-day, as written in markdown:
// "2026-09-10" or "2026-09-10T14:30". Zero value means "unset".
type Date struct {
	T       time.Time
	HasTime bool
}

const (
	layoutDay     = "2006-01-02"
	layoutMinutes = "2006-01-02T15:04"
)

// ParseDate accepts "YYYY-MM-DD", "YYYY-MM-DDTHH:MM" and "YYYY-MM-DD HH:MM",
// plus full RFC3339. Relative forms are resolved by the caller (see Relative).
func ParseDate(s string) (Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Date{}, fmt.Errorf("empty date")
	}
	s = strings.Replace(s, " ", "T", 1)
	if t, err := time.ParseInLocation(layoutDay, s, time.Local); err == nil {
		return Date{T: t}, nil
	}
	if t, err := time.ParseInLocation(layoutMinutes, s, time.Local); err == nil {
		return Date{T: t, HasTime: true}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return Date{T: t.Local(), HasTime: true}, nil
	}
	return Date{}, fmt.Errorf("bad date %q", s)
}

// Relative resolves UI shorthands into an absolute date. Files never store these.
func Relative(s string, now time.Time) (Date, bool) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "today", "сегодня":
		return Date{T: day}, true
	case "tomorrow", "завтра":
		return Date{T: day.AddDate(0, 0, 1)}, true
	case "yesterday", "вчера":
		return Date{T: day.AddDate(0, 0, -1)}, true
	}
	return Date{}, false
}

func (d Date) IsZero() bool { return d.T.IsZero() }

func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	if d.HasTime {
		return d.T.Format(layoutMinutes)
	}
	return d.T.Format(layoutDay)
}

// Day truncates to midnight, which is how due/scheduled comparisons are made.
func (d Date) Day() time.Time {
	return time.Date(d.T.Year(), d.T.Month(), d.T.Day(), 0, 0, 0, 0, d.T.Location())
}

func (d Date) Before(o Date) bool { return d.T.Before(o.T) }

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == nil || *s == "" {
		*d = Date{}
		return nil
	}
	v, err := ParseDate(*s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Today returns the current local day with no time component.
func Today() Date {
	n := time.Now()
	return Date{T: time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())}
}
