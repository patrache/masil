// Package humanize formats times and counts in Korean (CA 4.9).
package humanize

import "time"

// Clock returns "11:05" / "오전 11:05".
func Clock(t time.Time, loc *time.Location, h12 bool) string {
	// TODO(core): Task 7
	return ""
}

// MessageTime: today "11:05", yesterday "어제 11:05", this year "11월 3일 11:05", else "2025. 11. 3.".
func MessageTime(now, t time.Time, loc *time.Location) string {
	// TODO(core): Task 7
	return ""
}

// Relative: "방금", "3분", "2시간", "어제", "11월 3일".
func Relative(now, t time.Time) string {
	// TODO(core): Task 7
	return ""
}

// Count: 999, "1.2천", "3.4만".
func Count(n int) string {
	// TODO(core): Task 7
	return ""
}

// Duration: "25분", "1시간 30분".
func Duration(d time.Duration) string {
	// TODO(core): Task 7
	return ""
}

// DayDivider: "2026년 11월 5일 목요일".
func DayDivider(t time.Time, loc *time.Location) string {
	// TODO(core): Task 7
	return ""
}
