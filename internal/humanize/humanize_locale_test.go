package humanize

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// The plan's "now" is uitest.Start: 2026-11-05 11:05 Seoul, a Thursday (the
// package-level now in humanize_test.go is the same instant).

func TestHumanizeEN(t *testing.T) {
	en := For(i18n.En)
	n := now.In(seoul)
	checks := []struct{ name, got, want string }{
		{"Clock 11:05 h12", en.Clock(kst(2026, 11, 5, 11, 5), seoul, true), "11:05 AM"},
		{"Clock 23:05 h12", en.Clock(kst(2026, 11, 5, 23, 5), seoul, true), "11:05 PM"},
		{"Clock 00:05 h12", en.Clock(kst(2026, 11, 5, 0, 5), seoul, true), "12:05 AM"},
		{"Clock 11:05 24h", en.Clock(kst(2026, 11, 5, 11, 5), seoul, false), "11:05"},
		{"MessageTime today", en.MessageTime(now, kst(2026, 11, 5, 11, 5), seoul), "11:05"},
		{"MessageTime yesterday", en.MessageTime(now, kst(2026, 11, 4, 11, 5), seoul), "Yesterday 11:05"},
		{"MessageTime this year", en.MessageTime(now, kst(2026, 11, 3, 11, 5), seoul), "Nov 3, 11:05"},
		{"MessageTime older", en.MessageTime(now, kst(2025, 11, 3, 11, 5), seoul), "Nov 3, 2025"},
		{"Relative 30s", en.Relative(n, n.Add(-30*time.Second)), "now"},
		{"Relative 3m", en.Relative(n, n.Add(-3*time.Minute)), "3m"},
		{"Relative 2h", en.Relative(n, n.Add(-2*time.Hour)), "2h"},
		{"Relative yesterday", en.Relative(n, kst(2026, 11, 4, 9, 0)), "1d"},
		{"Relative 11-03", en.Relative(n, kst(2026, 11, 3, 11, 5)), "Nov 3"},
		{"Count 999", en.Count(999), "999"},
		{"Count 1234", en.Count(1234), "1.2K"},
		{"Count 34000", en.Count(34000), "34K"},
		{"Count 3400000", en.Count(3400000), "3.4M"},
		{"Count 1200000000", en.Count(1200000000), "1.2B"},
		{"Count -1200", en.Count(-1200), "-1.2K"},
		{"Duration 45s", en.Duration(45 * time.Second), "45s"},
		{"Duration 25m", en.Duration(25 * time.Minute), "25m"},
		{"Duration 90m", en.Duration(90 * time.Minute), "1h 30m"},
		{"Duration 2h", en.Duration(2 * time.Hour), "2h"},
		{"Duration 0", en.Duration(0), "0m"},
		{"DayDivider", en.DayDivider(kst(2026, 11, 5, 0, 0), seoul), "Thursday, November 5, 2026"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("en %s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestHumanizeZH(t *testing.T) {
	zh := For(i18n.ZhHans)
	n := now.In(seoul)
	checks := []struct{ name, got, want string }{
		{"Clock 11:05 h12", zh.Clock(kst(2026, 11, 5, 11, 5), seoul, true), "上午11:05"},
		{"Clock 23:05 h12", zh.Clock(kst(2026, 11, 5, 23, 5), seoul, true), "下午11:05"},
		{"Clock 11:05 24h", zh.Clock(kst(2026, 11, 5, 11, 5), seoul, false), "11:05"},
		{"MessageTime today", zh.MessageTime(now, kst(2026, 11, 5, 11, 5), seoul), "11:05"},
		{"MessageTime yesterday", zh.MessageTime(now, kst(2026, 11, 4, 11, 5), seoul), "昨天 11:05"},
		{"MessageTime this year", zh.MessageTime(now, kst(2026, 11, 3, 11, 5), seoul), "11月3日 11:05"},
		{"MessageTime older", zh.MessageTime(now, kst(2025, 11, 3, 11, 5), seoul), "2025/11/3"},
		{"Relative 30s", zh.Relative(n, n.Add(-30*time.Second)), "刚刚"},
		{"Relative 3m", zh.Relative(n, n.Add(-3*time.Minute)), "3分钟"},
		{"Relative 2h", zh.Relative(n, n.Add(-2*time.Hour)), "2小时"},
		{"Relative yesterday", zh.Relative(n, kst(2026, 11, 4, 9, 0)), "昨天"},
		{"Relative 11-03", zh.Relative(n, kst(2026, 11, 3, 11, 5)), "11月3日"},
		{"Count 999", zh.Count(999), "999"},
		{"Count 9999", zh.Count(9999), "9999"},
		{"Count 12000", zh.Count(12000), "1.2万"},
		{"Count 120000000", zh.Count(120000000), "1.2亿"},
		{"Duration 45s", zh.Duration(45 * time.Second), "45秒"},
		{"Duration 25m", zh.Duration(25 * time.Minute), "25分钟"},
		{"Duration 90m", zh.Duration(90 * time.Minute), "1小时30分钟"},
		{"DayDivider", zh.DayDivider(kst(2026, 11, 5, 0, 0), seoul), "2026年11月5日 星期四"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("zh-Hans %s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestRelativeWidthBudget: Relative fits 8 cells in en and zh-Hans for every
// age (i18n 6.7), previous years included ("11/30/25", "25/11/30"). ko keeps
// its 0.1.2 output (TestHumanizeKOUnchanged), which is wider in two places —
// "10월 31일" (9 cells) and the previous-year full date ("2025. 10. 1.") — so
// ko alone is held to 9 cells this year and to its 0.1.2 value before.
func TestRelativeWidthBudget(t *testing.T) {
	n := now.In(seoul)
	for _, l := range []i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans} {
		f := For(l)
		for d := time.Duration(0); d <= 400*24*time.Hour; d += time.Minute {
			at := n.Add(-d)
			got := f.Relative(n, at)
			w := textwidth.Width(got)
			if l == i18n.Ko {
				if at.In(seoul).Year() != n.Year() {
					continue // 0.1.2 full date, held by TestHumanizeKOUnchanged
				}
				if w > 9 {
					t.Fatalf("ko Relative(-%v) = %q is %d cells, budget 9", d, got, w)
				}
				continue
			}
			if w > 8 {
				t.Fatalf("%s Relative(-%v) = %q is %d cells, budget 8", l, d, got, w)
			}
		}
	}
	old := kst(2025, 11, 30, 11, 5)
	for l, want := range map[i18n.Locale]string{i18n.Ko: "2025. 11. 30.", i18n.En: "11/30/25", i18n.ZhHans: "25/11/30"} {
		if got := For(l).Relative(n, old); got != want {
			t.Errorf("%s Relative(2025-11-30) = %q, want %q", l, got, want)
		}
	}
}

// TestHumanizeKOUnchanged runs the 0.1.2 ko expectations of humanize_test.go
// against For(Ko) (the package functions there are the same path).
func TestHumanizeKOUnchanged(t *testing.T) {
	ko := For(i18n.Ko)
	n := now.In(seoul)
	checks := []struct{ got, want string }{
		{ko.Clock(now, seoul, false), "11:05"},
		{ko.Clock(now, seoul, true), "오전 11:05"},
		{ko.Clock(kst(2026, 11, 5, 13, 5), seoul, true), "오후 1:05"},
		{ko.Clock(kst(2026, 11, 5, 13, 5), seoul, false), "13:05"},
		{ko.Clock(kst(2026, 11, 5, 0, 30), seoul, true), "오전 12:30"},
		{ko.Clock(kst(2026, 11, 5, 12, 30), seoul, true), "오후 12:30"},
		{ko.Clock(kst(2026, 11, 5, 9, 7), seoul, false), "09:07"},
		{ko.MessageTime(now, now.Add(-3*time.Minute), seoul), "11:02"},
		{ko.MessageTime(now, kst(2026, 11, 5, 0, 1), seoul), "00:01"},
		{ko.MessageTime(now, kst(2026, 11, 4, 23, 10), seoul), "어제 23:10"},
		{ko.MessageTime(now, kst(2026, 11, 3, 11, 5), seoul), "11월 3일 11:05"},
		{ko.MessageTime(now, kst(2026, 1, 1, 8, 0), seoul), "1월 1일 08:00"},
		{ko.MessageTime(now, kst(2025, 11, 3, 11, 5), seoul), "2025. 11. 3."},
		{ko.Relative(n, n.Add(-30*time.Second)), "방금"},
		{ko.Relative(n, n.Add(-3*time.Minute)), "3분"},
		{ko.Relative(n, n.Add(-2*time.Hour)), "2시간"},
		{ko.Relative(n, n.Add(-23*time.Hour-59*time.Minute)), "23시간"},
		{ko.Relative(n, kst(2026, 11, 4, 9, 0)), "어제"},
		{ko.Relative(n, kst(2026, 11, 3, 11, 5)), "11월 3일"},
		{ko.Relative(n, kst(2025, 1, 2, 11, 5)), "2025. 1. 2."},
		{ko.Relative(n, n.Add(time.Hour)), "방금"},
		{ko.Count(0), "0"}, {ko.Count(999), "999"}, {ko.Count(1000), "1천"}, {ko.Count(1200), "1.2천"},
		{ko.Count(1290), "1.2천"}, {ko.Count(9999), "9.9천"}, {ko.Count(10000), "1만"}, {ko.Count(34000), "3.4만"},
		{ko.Count(99999), "9.9만"}, {ko.Count(120000), "12만"}, {ko.Count(12345678), "1234만"},
		{ko.Count(100000000), "1억"}, {ko.Count(120000000), "1.2억"}, {ko.Count(-1200), "-1.2천"},
		{ko.Duration(45 * time.Second), "45초"},
		{ko.Duration(25 * time.Minute), "25분"},
		{ko.Duration(25*time.Minute + 30*time.Second), "25분"},
		{ko.Duration(90 * time.Minute), "1시간 30분"},
		{ko.Duration(2 * time.Hour), "2시간"},
		{ko.Duration(0), "0분"},
		{ko.Duration(-time.Minute), "0분"},
		{ko.DayDivider(now, seoul), "2026년 11월 5일 목요일"},
		{ko.DayDivider(kst(2026, 11, 8, 0, 0), seoul), "2026년 11월 8일 일요일"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("ko got %q, want %q", c.got, c.want)
		}
	}
}

// TestHumanizeMonthsWeekdays: every month and weekday name renders in each
// language (the dynamic catalog lookups by month/weekday number).
func TestHumanizeMonthsWeekdays(t *testing.T) {
	enShort := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	enLong := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	for m := 1; m <= 12; m++ {
		at := kst(2026, time.Month(m), 1, 9, 0)
		if got, want := For(i18n.En).Relative(kst(2026, 12, 31, 9, 0), at), enShort[m-1]+" 1"; got != want {
			t.Errorf("en month %d = %q, want %q", m, got, want)
		}
		if got, want := For(i18n.En).DayDivider(at, seoul), enLong[m-1]; !strings.Contains(got, want) {
			t.Errorf("en DayDivider month %d = %q, want %q in it", m, got, want)
		}
		if got := For(i18n.ZhHans).Relative(kst(2026, 12, 31, 9, 0), at); got != strconv.Itoa(m)+"月1日" {
			t.Errorf("zh month %d = %q", m, got)
		}
	}
	enDays := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	zhDays := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	for d := 0; d < 7; d++ {
		at := kst(2026, 11, 1+d, 9, 0) // 2026-11-01 is a Sunday
		if got := For(i18n.En).DayDivider(at, seoul); !strings.Contains(got, enDays[d]) {
			t.Errorf("en weekday %d = %q", d, got)
		}
		if got := For(i18n.ZhHans).DayDivider(at, seoul); !strings.Contains(got, zhDays[d]) {
			t.Errorf("zh weekday %d = %q", d, got)
		}
	}
}
