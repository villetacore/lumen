package domain

import (
	"strconv"
	"strings"
	"time"
)

// weekdays maps the names a person types in quick capture to time.Weekday.
var weekdays = map[string]time.Weekday{
	"пн": time.Monday, "понедельник": time.Monday, "mon": time.Monday, "monday": time.Monday,
	"вт": time.Tuesday, "вторник": time.Tuesday, "tue": time.Tuesday, "tuesday": time.Tuesday,
	"ср": time.Wednesday, "среда": time.Wednesday, "wed": time.Wednesday, "wednesday": time.Wednesday,
	"чт": time.Thursday, "четверг": time.Thursday, "thu": time.Thursday, "thursday": time.Thursday,
	"пт": time.Friday, "пятница": time.Friday, "fri": time.Friday, "friday": time.Friday,
	"сб": time.Saturday, "суббота": time.Saturday, "sat": time.Saturday, "saturday": time.Saturday,
	"вс": time.Sunday, "воскресенье": time.Sunday, "sun": time.Sunday, "sunday": time.Sunday,
}

// RelativeInput resolves the shorthands accepted from UI input on top of
// Relative: "послезавтра", weekday names (the next such day, never today),
// "неделя"/"next week" (next Monday) and offsets "+3d", "+2w", "+1m".
//
// The markdown parser keeps using Relative only: these forms are for typing,
// files store absolute dates, and both parser implementations must agree.
func RelativeInput(s string, now time.Time) (Date, bool) {
	if d, ok := Relative(s, now); ok {
		return d, true
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "послезавтра":
		return Date{T: day.AddDate(0, 0, 2)}, true
	case "неделя", "след.неделя", "next-week", "nextweek", "week":
		return Date{T: day.AddDate(0, 0, 7-(int(day.Weekday())+6)%7)}, true
	}
	if wd, ok := weekdays[v]; ok {
		diff := (int(wd) - int(day.Weekday()) + 7) % 7
		if diff == 0 {
			diff = 7
		}
		return Date{T: day.AddDate(0, 0, diff)}, true
	}
	if len(v) >= 3 && v[0] == '+' {
		n, err := strconv.Atoi(v[1 : len(v)-1])
		if err != nil || n < 0 || n > 3650 {
			return Date{}, false
		}
		switch v[len(v)-1] {
		case 'd':
			return Date{T: day.AddDate(0, 0, n)}, true
		case 'w':
			return Date{T: day.AddDate(0, 0, 7*n)}, true
		case 'm':
			return Date{T: day.AddDate(0, n, 0)}, true
		}
	}
	return Date{}, false
}
