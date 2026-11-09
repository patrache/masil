package humanize

import (
	"math"
	"testing"
)

func TestGrouped(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{0, "0"}, {999, "999"}, {1000, "1,000"}, {1840, "1,840"}, {1234567, "1,234,567"},
		{-1840, "-1,840"}, {-999, "-999"}, {math.MaxInt64, "9,223,372,036,854,775,807"},
		{math.MinInt64, "-9,223,372,036,854,775,808"},
	} {
		if got := Grouped(tc.n); got != tc.want {
			t.Errorf("Grouped(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
