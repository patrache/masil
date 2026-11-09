// Package humanize formats times and counts (CA 4.9). For(locale) picks the
// language; the package functions are For(i18n.Ko), unchanged from 0.1.2.
package humanize

import (
	"fmt"
	"strconv"
	"time"

	"github.com/patrache/masil/internal/i18n"
)

// Clock returns "11:05" / "오전 11:05".
func Clock(t time.Time, loc *time.Location, h12 bool) string {
	return For(i18n.Ko).Clock(t, loc, h12)
}

// MessageTime: today "11:05", yesterday "어제 11:05", this year "11월 3일 11:05", else "2025. 11. 3.".
func MessageTime(now, t time.Time, loc *time.Location) string {
	return For(i18n.Ko).MessageTime(now, t, loc)
}

// Relative: "방금", "3분", "2시간", "어제", "11월 3일" (now already in the display location).
func Relative(now, t time.Time) string { return For(i18n.Ko).Relative(now, t) }

// Count: 999, "1.2천", "3.4만".
func Count(n int) string { return For(i18n.Ko).Count(n) }

// Duration: "25분", "1시간 30분".
func Duration(d time.Duration) string { return For(i18n.Ko).Duration(d) }

// DayDivider: "2026년 11월 5일 목요일".
func DayDivider(t time.Time, loc *time.Location) string { return For(i18n.Ko).DayDivider(t, loc) }

// --- Korean (the 0.1.2 implementation) ---

var weekdays = [...]string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"}

// koClock returns "11:05" / "오전 11:05".
func koClock(t time.Time, loc *time.Location, h12 bool) string {
	t = in(t, loc)
	if !h12 {
		return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
	}
	ampm := "오전"
	if t.Hour() >= 12 {
		ampm = "오후"
	}
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return fmt.Sprintf("%s %d:%02d", ampm, h, t.Minute())
}

// koMessageTime: today "11:05", yesterday "어제 11:05", this year "11월 3일 11:05", else "2025. 11. 3.".
func koMessageTime(now, t time.Time, loc *time.Location) string {
	now, t = in(now, loc), in(t, loc)
	clock := koClock(t, loc, false)
	switch {
	case sameDay(now, t):
		return clock
	case sameDay(now.AddDate(0, 0, -1), t):
		return "어제 " + clock
	case now.Year() == t.Year():
		return monthDay(t) + " " + clock
	default:
		return fullDate(t)
	}
}

// koRelative: "방금", "3분", "2시간", "어제", "11월 3일".
// The calendar ("어제", dates) is evaluated in now's location, so callers pass
// now already in the display location (ctx.Now().In(ctx.Loc)).
func koRelative(now, t time.Time) string {
	d := now.Sub(t)
	t = t.In(now.Location())
	switch {
	case d < time.Minute:
		return "방금"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "분"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "시간"
	case sameDay(now.AddDate(0, 0, -1), t):
		return "어제"
	case now.Year() == t.Year():
		return monthDay(t)
	default:
		return fullDate(t)
	}
}

// koCount: 999, "1.2천", "3.4만".
// One decimal place (truncated, ".0" omitted) while the unit value is below 10.
func koCount(n int) string {
	if n < 0 {
		return "-" + koCount(-n)
	}
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 10000:
		return unit(int64(n), 1000, "천")
	case n < 100000000:
		return unit(int64(n), 10000, "만")
	default:
		return unit(int64(n), 100000000, "억")
	}
}

func unit(n, base int64, suffix string) string {
	whole := n / base
	if whole >= 10 {
		return strconv.FormatInt(whole, 10) + suffix
	}
	tenth := (n % base) * 10 / base
	if tenth == 0 {
		return strconv.FormatInt(whole, 10) + suffix
	}
	return strconv.FormatInt(whole, 10) + "." + strconv.FormatInt(tenth, 10) + suffix
}

// koDuration: "25분", "1시간 30분".
// Under a minute shows seconds ("45초"); otherwise seconds are truncated.
func koDuration(d time.Duration) string {
	if d <= 0 {
		return "0분"
	}
	if d < time.Minute {
		return strconv.Itoa(int(d/time.Second)) + "초"
	}
	h := int(d / time.Hour)
	m := int(d%time.Hour) / int(time.Minute)
	switch {
	case h == 0:
		return strconv.Itoa(m) + "분"
	case m == 0:
		return strconv.Itoa(h) + "시간"
	default:
		return fmt.Sprintf("%d시간 %d분", h, m)
	}
}

// koDayDivider: "2026년 11월 5일 목요일".
func koDayDivider(t time.Time, loc *time.Location) string {
	t = in(t, loc)
	return fmt.Sprintf("%d년 %d월 %d일 %s", t.Year(), int(t.Month()), t.Day(), weekdays[t.Weekday()])
}

func in(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		return t
	}
	return t.In(loc)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func monthDay(t time.Time) string {
	return fmt.Sprintf("%d월 %d일", int(t.Month()), t.Day())
}

func fullDate(t time.Time) string {
	return fmt.Sprintf("%d. %d. %d.", t.Year(), int(t.Month()), t.Day())
}
