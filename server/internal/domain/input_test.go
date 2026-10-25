package domain

import (
	"testing"
	"time"
)

func TestRelativeInput(t *testing.T) {
	// 2026-10-25 is a Sunday.
	now := time.Date(2026, 10, 25, 15, 4, 0, 0, time.Local)
	cases := map[string]string{
		"сегодня":     "2026-10-25",
		"завтра":      "2026-10-26",
		"послезавтра": "2026-10-27",
		"пн":          "2026-10-26",
		"Пятница":     "2026-10-30",
		"вс":          "2026-11-01", // never today
		"fri":         "2026-10-30",
		"неделя":      "2026-10-26",
		"+3d":         "2026-10-28",
		"+2w":         "2026-11-08",
		"+1m":         "2026-11-25",
	}
	for in, want := range cases {
		d, ok := RelativeInput(in, now)
		if !ok || d.String() != want {
			t.Errorf("RelativeInput(%q) = %q, %v; want %q", in, d.String(), ok, want)
		}
	}
	for _, in := range []string{"", "2026-10-30", "+d", "+xd", "+3y", "потом"} {
		if d, ok := RelativeInput(in, now); ok {
			t.Errorf("RelativeInput(%q) = %q, want no match", in, d.String())
		}
	}

	// Midweek: Wednesday → next Monday is 5 days away.
	wed := time.Date(2026, 10, 28, 9, 0, 0, 0, time.Local)
	if d, _ := RelativeInput("неделя", wed); d.String() != "2026-11-02" {
		t.Errorf("неделя from Wednesday = %s", d.String())
	}
}
