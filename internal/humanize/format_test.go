package humanize

import (
	"testing"
	"time"

	"github.com/patrache/masil/internal/i18n"
)

// TestFormatterKoMatchesLegacy: For(i18n.Ko) prints exactly what the 0.1.2
// package functions print (the tables in humanize_test.go run against those
// package functions, which now delegate to For(i18n.Ko)). In 0.1.3 every
// locale prints Korean (en/zh-Hans tables are P5).
func TestFormatterKoMatchesLegacy(t *testing.T) {
	// Spot values straight from humanize_test.go's tables.
	ko := For(i18n.Ko)
	for got, want := range map[string]string{
		ko.Clock(now, seoul, true):                           "오전 11:05",
		ko.Clock(kst(2026, 11, 5, 13, 5), seoul, true):       "오후 1:05",
		ko.MessageTime(now, kst(2026, 11, 4, 23, 0), seoul):  "어제 23:00",
		ko.MessageTime(now, kst(2026, 11, 3, 11, 5), seoul):  "11월 3일 11:05",
		ko.MessageTime(now, kst(2025, 11, 3, 11, 5), seoul):  "2025. 11. 3.",
		ko.Relative(now.In(seoul), now.Add(-30*time.Second)): "방금",
		ko.Relative(now.In(seoul), now.Add(-3*time.Minute)):  "3분",
		ko.Count(1234):                "1.2천",
		ko.Count(34567):               "3.4만",
		ko.Count(123456789):           "1.2억",
		ko.Duration(90 * time.Minute): "1시간 30분",
		ko.Duration(45 * time.Second): "45초",
		ko.DayDivider(now, seoul):     "2026년 11월 5일 목요일",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	// A grid: every locale's Formatter equals the package functions.
	times := []time.Time{now, now.Add(-30 * time.Second), now.Add(-59 * time.Minute), now.Add(-5 * time.Hour),
		now.Add(-26 * time.Hour), now.AddDate(0, -2, 0), now.AddDate(-1, 0, 0), kst(2026, 11, 5, 0, 30), kst(2026, 11, 5, 12, 30)}
	counts := []int{-1500, 0, 7, 999, 1000, 1234, 9999, 10000, 34567, 99999999, 100000000, 123456789}
	durs := []time.Duration{0, -time.Second, 45 * time.Second, time.Minute, 25 * time.Minute, time.Hour, 90 * time.Minute}
	for _, l := range []i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans, i18n.Pseudo} {
		f := For(l)
		for _, tm := range times {
			for _, h12 := range []bool{false, true} {
				if got, want := f.Clock(tm, seoul, h12), Clock(tm, seoul, h12); got != want {
					t.Errorf("%s Clock(%v,%v) = %q, want %q", l, tm, h12, got, want)
				}
			}
			if got, want := f.MessageTime(now, tm, seoul), MessageTime(now, tm, seoul); got != want {
				t.Errorf("%s MessageTime(%v) = %q, want %q", l, tm, got, want)
			}
			if got, want := f.Relative(now.In(seoul), tm), Relative(now.In(seoul), tm); got != want {
				t.Errorf("%s Relative(%v) = %q, want %q", l, tm, got, want)
			}
			if got, want := f.DayDivider(tm, seoul), DayDivider(tm, seoul); got != want {
				t.Errorf("%s DayDivider(%v) = %q, want %q", l, tm, got, want)
			}
		}
		for _, n := range counts {
			if got, want := f.Count(n), Count(n); got != want {
				t.Errorf("%s Count(%d) = %q, want %q", l, n, got, want)
			}
		}
		for _, d := range durs {
			if got, want := f.Duration(d), Duration(d); got != want {
				t.Errorf("%s Duration(%v) = %q, want %q", l, d, got, want)
			}
		}
	}
}
