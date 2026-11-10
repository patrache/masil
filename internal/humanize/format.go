package humanize

import (
	"fmt"
	"strconv"
	"time"

	"github.com/patrache/masil/internal/i18n"
)

// Formatter formats times and counts for one language (i18n spec 6.7). Time
// zone and 12/24h come from the caller, never from the language. The words
// come from the catalog's time area; the rules (thresholds) are here.
type Formatter struct {
	locale i18n.Locale
}

// For returns the Formatter for l. Unknown languages format as ko.
func For(l i18n.Locale) Formatter { return Formatter{locale: l} }

// Locale is the language this Formatter was made for.
func (f Formatter) Locale() i18n.Locale { return f.locale }

func (f Formatter) cat() *i18n.T { return i18n.For(f.locale) }

// rules is the language whose rules apply: x-pseudo is en stretched, unknown
// languages are ko (as in i18n.For).
func (f Formatter) rules() i18n.Locale {
	switch f.locale {
	case i18n.En, i18n.Pseudo:
		return i18n.En
	case i18n.ZhHans:
		return i18n.ZhHans
	default:
		return i18n.Ko
	}
}

// Clock returns "11:05" (24h) or the 12h form: ko "오전 11:05", en "11:05 AM",
// zh-Hans "上午11:05".
func (f Formatter) Clock(t time.Time, loc *time.Location, h12 bool) string {
	t = in(t, loc)
	if !h12 {
		return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
	}
	c := f.cat().Time
	ampm := c.Am()
	if t.Hour() >= 12 {
		ampm = c.Pm()
	}
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return c.Clock12h(ampm, fmt.Sprintf("%d:%02d", h, t.Minute()))
}

// MessageTime: today "11:05", yesterday ("어제 11:05"), this year
// ("11월 3일 11:05"), older ("2025. 11. 3.") — always 24h.
func (f Formatter) MessageTime(now, t time.Time, loc *time.Location) string {
	now, t = in(now, loc), in(t, loc)
	clock := f.Clock(t, loc, false)
	c := f.cat().Time
	switch {
	case sameDay(now, t):
		return clock
	case sameDay(now.AddDate(0, 0, -1), t):
		return c.YesterdayAt(clock)
	case now.Year() == t.Year():
		return c.MonthDayAt(f.monthDay(t), clock)
	default:
		return f.fullDate(t)
	}
}

// Relative: now, minutes, hours, yesterday, date this year, full date older.
// The calendar (yesterday, dates) is evaluated in now's location, so callers
// pass now already in the display location (ctx.Now().In(ctx.Loc)).
func (f Formatter) Relative(now, t time.Time) string {
	d := now.Sub(t)
	t = t.In(now.Location())
	c := f.cat().Time
	switch {
	case d < time.Minute:
		return c.RelativeNow()
	case d < time.Hour:
		return c.RelativeMinutes(int(d / time.Minute))
	case d < 24*time.Hour:
		return c.RelativeHours(int(d / time.Hour))
	case sameDay(now.AddDate(0, 0, -1), t):
		return c.RelativeYesterday()
	case now.Year() == t.Year():
		return f.monthDay(t)
	case f.rules() == i18n.Ko:
		return f.fullDate(t) // ko keeps 0.1.2 ("2025. 11. 30.", wider than 8)
	default:
		return c.RelativePastYear(fmt.Sprintf("%02d", t.Year()%100), strconv.Itoa(int(t.Month())), strconv.Itoa(t.Day()))
	}
}

// countTiers are the abbreviation bases per language (tier 1..3 in the
// catalog): ko 천·만·억, en K·M·B, zh-Hans 万·亿·万亿.
var countTiers = map[i18n.Locale][3]int64{
	i18n.Ko:     {1e3, 1e4, 1e8},
	i18n.En:     {1e3, 1e6, 1e9},
	i18n.ZhHans: {1e4, 1e8, 1e12},
}

// Count abbreviates large counts: one decimal place (truncated, ".0"
// omitted) while the unit value is below 10.
func (f Formatter) Count(n int) string {
	if n < 0 {
		return "-" + f.Count(-n)
	}
	tiers := countTiers[f.rules()]
	c := f.cat().Time
	v := int64(n)
	switch {
	case v < tiers[0]:
		return strconv.Itoa(n)
	case v < tiers[1]:
		return c.CountTier1(unit(v, tiers[0]))
	case v < tiers[2]:
		return c.CountTier2(unit(v, tiers[1]))
	default:
		return c.CountTier3(unit(v, tiers[2]))
	}
}

// unit is n in base units: "1.2", "12".
func unit(n, base int64) string {
	whole := n / base
	if whole >= 10 {
		return strconv.FormatInt(whole, 10)
	}
	tenth := (n % base) * 10 / base
	if tenth == 0 {
		return strconv.FormatInt(whole, 10)
	}
	return strconv.FormatInt(whole, 10) + "." + strconv.FormatInt(tenth, 10)
}

// Duration: minutes and hours ("1시간 30분"). Under a minute shows seconds;
// otherwise seconds are truncated. Zero or less is 0 minutes.
func (f Formatter) Duration(d time.Duration) string {
	c := f.cat().Time
	if d <= 0 {
		return c.DurationMinutes(0)
	}
	if d < time.Minute {
		return c.DurationSeconds(int(d / time.Second))
	}
	h := int(d / time.Hour)
	m := int(d%time.Hour) / int(time.Minute)
	switch {
	case h == 0:
		return c.DurationMinutes(m)
	case m == 0:
		return c.DurationHours(h)
	default:
		return c.DurationHoursMinutes(h, m)
	}
}

// DayDivider is the chat day separator with the weekday
// ("2026년 11월 5일 목요일", "Thursday, November 5, 2026").
func (f Formatter) DayDivider(t time.Time, loc *time.Location) string {
	t = in(t, loc)
	return f.cat().Time.DayDivider(strconv.Itoa(t.Year()), f.word("month_long_", int(t.Month())),
		strconv.Itoa(t.Day()), f.word("weekday_", int(t.Weekday())))
}

// MonthDay is the date without the year in loc ("11월 12일", "Nov 12").
func (f Formatter) MonthDay(t time.Time, loc *time.Location) string { return f.monthDay(in(t, loc)) }

func (f Formatter) monthDay(t time.Time) string {
	return f.cat().Time.MonthDay(f.word("month_short_", int(t.Month())), strconv.Itoa(t.Day()))
}

func (f Formatter) fullDate(t time.Time) string {
	return f.cat().Time.FullDate(strconv.Itoa(t.Year()), f.word("month_short_", int(t.Month())), strconv.Itoa(t.Day()))
}

// word renders a numbered time key (month_short_11, weekday_4).
func (f Formatter) word(prefix string, n int) string {
	s, _ := f.cat().Lookup("time", prefix+strconv.Itoa(n), nil)
	return s
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
