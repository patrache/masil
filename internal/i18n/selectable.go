package i18n

// Selectable reports whether the user may choose a language: two or more real
// languages are open. Pseudo never counts (it is not in Supported). 0.1.3 opens
// ko only, so the language UI (settings row, login row, palette commands) stays
// hidden.
func Selectable() bool {
	return len(OpenLocales()) >= 2
}
