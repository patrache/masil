package i18n

import (
	"maps"
	"testing"
)

// SetOpenForTest replaces the open-language set with ls for the rest of tb and
// restores the previous set on cleanup. It panics outside tests.
func SetOpenForTest(tb testing.TB, ls ...Locale) {
	if !testing.Testing() {
		panic("i18n: SetOpenForTest called outside a test")
	}
	tb.Helper()
	openMu.Lock()
	prev := maps.Clone(open)
	next := make(map[Locale]bool, len(ls))
	for _, l := range ls {
		next[l] = true
	}
	open = next
	openMu.Unlock()
	tb.Cleanup(func() {
		openMu.Lock()
		open = prev
		openMu.Unlock()
	})
}
