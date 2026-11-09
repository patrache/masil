package i18n

import "sync"

// open is the open-language gate (i18n spec 15.4). 0.1.3 opens ko only; P5
// opens a language once it passes the release gate.
var (
	openMu sync.RWMutex
	open   = map[Locale]bool{Ko: true}
)

// IsOpen reports whether l may be shown to users.
func IsOpen(l Locale) bool {
	openMu.RLock()
	defer openMu.RUnlock()
	return open[l]
}

// OpenLocales lists the open languages in Supported order.
func OpenLocales() []Locale {
	openMu.RLock()
	defer openMu.RUnlock()
	var out []Locale
	for _, l := range Supported {
		if open[l] {
			out = append(out, l)
		}
	}
	return out
}

// gate turns a requested language into the one the UI shows: itself when open,
// otherwise en when en is open, otherwise ko.
func gate(l Locale) Locale {
	switch {
	case IsOpen(l):
		return l
	case IsOpen(En):
		return En
	default:
		return Ko
	}
}
