package keymap

import "github.com/patrache/masil/internal/i18n"

// SetRowForTest adds (or replaces) a default row; the returned func restores it.
func SetRowForTest(a Action, keys []string, desc func(*i18n.T) string) (restore func()) {
	old, had := defaults[a]
	defaults[a] = spec{keys: keys, desc: desc}
	return func() {
		if had {
			defaults[a] = old
		} else {
			delete(defaults, a)
		}
	}
}
