package leet_test

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

// Narrow places (the hub row, sub-tabs, key hints, one-line cards) carry a
// width budget in the catalog (Global Constraints), so a translation that
// does not fit fails go generate instead of an ellipsis on screen.
func TestNarrowKeysHaveMax(t *testing.T) {
	want := map[string]int{
		"hub_status": 40, "hub_status_short": 40, "hub_not_loaded": 40,
		"hints_wide": 64, "hints_widget": 56, "header_widget": 24,
		"tiny_status": 40, "tiny_read": 30, "tiny_edit": 30, "tiny_actions": 30,
		"card_daily_line": 40,
	}
	for _, e := range i18n.Entries() {
		if e.Area != "leetcode" {
			continue
		}
		if m, ok := want[e.Key]; ok {
			if e.Max != m {
				t.Errorf("leetcode.%s max = %d, want %d", e.Key, e.Max, m)
			}
			delete(want, e.Key)
		}
	}
	for k := range want {
		t.Errorf("leetcode.%s missing", k)
	}
}
