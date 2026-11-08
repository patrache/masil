package palette

import "github.com/patrache/masil/internal/ui"

// ItemsForTest returns the ranked items for query (built-in and registered sources,
// no people-search results), before grouping.
func ItemsForTest(ctx *ui.Context, query string) []Item {
	return collect(ctx, query, nil, "")
}

// RememberForTest records it as a route chosen in the palette (recent list).
func RememberForTest(it Item) {
	remember(it)
}

// ResetRecentForTest empties the recently chosen routes; returns a restore func.
func ResetRecentForTest() (restore func()) {
	recentMu.Lock()
	saved := append([]Item(nil), recent...)
	recent = nil
	recentMu.Unlock()
	return func() {
		recentMu.Lock()
		recent = saved
		recentMu.Unlock()
	}
}
