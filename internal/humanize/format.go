package humanize

import (
	"time"

	"github.com/patrache/masil/internal/i18n"
)

// Formatter formats times and counts for one language (i18n spec 6.7). Time
// zone and 12/24h come from the caller, never from the language.
type Formatter struct {
	locale i18n.Locale
}

// For returns the Formatter for l. 0.1.3: every language prints Korean (the
// en and zh-Hans tables arrive with P5).
func For(l i18n.Locale) Formatter { return Formatter{locale: l} }

// Locale is the language this Formatter was made for.
func (f Formatter) Locale() i18n.Locale { return f.locale }

// Clock returns "11:05" / "오전 11:05".
func (f Formatter) Clock(t time.Time, loc *time.Location, h12 bool) string {
	return koClock(t, loc, h12)
}

// MessageTime: today "11:05", yesterday, this year, older (see the ko table).
func (f Formatter) MessageTime(now, t time.Time, loc *time.Location) string {
	return koMessageTime(now, t, loc)
}

// Relative: "now", minutes, hours, yesterday, date; now is in the display location.
func (f Formatter) Relative(now, t time.Time) string { return koRelative(now, t) }

// Count abbreviates large counts.
func (f Formatter) Count(n int) string { return koCount(n) }

// Duration: minutes and hours (seconds under a minute).
func (f Formatter) Duration(d time.Duration) string { return koDuration(d) }

// DayDivider is the chat day separator with the weekday.
func (f Formatter) DayDivider(t time.Time, loc *time.Location) string { return koDayDivider(t, loc) }
