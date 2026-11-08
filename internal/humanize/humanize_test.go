package humanize

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

var seoul = mustLoc("Asia/Seoul")
var now = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC) // 11:05 KST, Thursday

func kst(y int, mo time.Month, d, h, m int) time.Time {
	return time.Date(y, mo, d, h, m, 0, 0, seoul)
}

func TestClock(t *testing.T) {
	cases := []struct {
		t    time.Time
		h12  bool
		want string
	}{
		{now, false, "11:05"},
		{now, true, "오전 11:05"},
		{kst(2026, 11, 5, 13, 5), true, "오후 1:05"},
		{kst(2026, 11, 5, 13, 5), false, "13:05"},
		{kst(2026, 11, 5, 0, 30), true, "오전 12:30"},
		{kst(2026, 11, 5, 12, 30), true, "오후 12:30"},
		{kst(2026, 11, 5, 9, 7), false, "09:07"},
	}
	for _, c := range cases {
		if got := Clock(c.t, seoul, c.h12); got != c.want {
			t.Errorf("Clock(%v, h12=%v) = %q, want %q", c.t, c.h12, got, c.want)
		}
	}
}

func TestMessageTime(t *testing.T) {
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-3 * time.Minute), "11:02"},
		{kst(2026, 11, 5, 0, 1), "00:01"},
		{kst(2026, 11, 4, 23, 10), "어제 23:10"},
		{kst(2026, 11, 3, 11, 5), "11월 3일 11:05"},
		{kst(2026, 1, 1, 8, 0), "1월 1일 08:00"},
		{kst(2025, 11, 3, 11, 5), "2025. 11. 3."},
	}
	for _, c := range cases {
		if got := MessageTime(now, c.t, seoul); got != c.want {
			t.Errorf("MessageTime(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestRelative(t *testing.T) {
	n := now.In(seoul)
	cases := []struct {
		t    time.Time
		want string
	}{
		{n.Add(-30 * time.Second), "방금"},
		{n.Add(-3 * time.Minute), "3분"},
		{n.Add(-2 * time.Hour), "2시간"},
		{n.Add(-23*time.Hour - 59*time.Minute), "23시간"},
		{kst(2026, 11, 4, 9, 0), "어제"},
		{kst(2026, 11, 3, 11, 5), "11월 3일"},
		{kst(2025, 1, 2, 11, 5), "2025. 1. 2."},
		{n.Add(time.Hour), "방금"},
	}
	for _, c := range cases {
		if got := Relative(n, c.t); got != c.want {
			t.Errorf("Relative(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"}, {999, "999"}, {1000, "1천"}, {1200, "1.2천"}, {1290, "1.2천"}, {9999, "9.9천"},
		{10000, "1만"}, {34000, "3.4만"}, {99999, "9.9만"}, {120000, "12만"}, {12345678, "1234만"},
		{100000000, "1억"}, {120000000, "1.2억"}, {-1200, "-1.2천"},
	}
	for _, c := range cases {
		if got := Count(c.n); got != c.want {
			t.Errorf("Count(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45초"},
		{25 * time.Minute, "25분"},
		{25*time.Minute + 30*time.Second, "25분"},
		{90 * time.Minute, "1시간 30분"},
		{2 * time.Hour, "2시간"},
		{0, "0분"},
		{-time.Minute, "0분"},
	}
	for _, c := range cases {
		if got := Duration(c.d); got != c.want {
			t.Errorf("Duration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestDayDivider(t *testing.T) {
	if got := DayDivider(now, seoul); got != "2026년 11월 5일 목요일" {
		t.Fatalf("DayDivider = %q", got)
	}
	if got := DayDivider(kst(2026, 11, 8, 0, 0), seoul); got != "2026년 11월 8일 일요일" {
		t.Fatalf("DayDivider = %q", got)
	}
}
