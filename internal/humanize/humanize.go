// Package humanize formats times and counts (CA 4.9). For(locale) picks the
// language; the package functions are For(i18n.Ko), unchanged from 0.1.2.
package humanize

import (
	"time"

	"github.com/patrache/masil/internal/i18n"
)

// Clock returns the ko clock: 24h "11:05", 12h "AM/PM 11:05" in Korean.
func Clock(t time.Time, loc *time.Location, h12 bool) string {
	return For(i18n.Ko).Clock(t, loc, h12)
}

// MessageTime is For(i18n.Ko).MessageTime: today, yesterday, this year, older.
func MessageTime(now, t time.Time, loc *time.Location) string {
	return For(i18n.Ko).MessageTime(now, t, loc)
}

// Relative is For(i18n.Ko).Relative (now already in the display location).
func Relative(now, t time.Time) string { return For(i18n.Ko).Relative(now, t) }

// Count is For(i18n.Ko).Count: 999, then thousands, ten-thousands, 1e8.
func Count(n int) string { return For(i18n.Ko).Count(n) }

// Duration is For(i18n.Ko).Duration: minutes and hours.
func Duration(d time.Duration) string { return For(i18n.Ko).Duration(d) }

// DayDivider is For(i18n.Ko).DayDivider: the date with the weekday.
func DayDivider(t time.Time, loc *time.Location) string { return For(i18n.Ko).DayDivider(t, loc) }
