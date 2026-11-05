// Package textwidth measures, normalizes, truncates and wraps terminal text (CA 4.7, 8).
package textwidth

// Sanitize is the display-only normalization (CA 8.2); idempotent; every grapheme
// of the result has StringWidth == StringWidthWc.
func Sanitize(s string) string {
	// TODO(core): Task 6
	return ""
}

// SanitizeInput is the text-entry normalization (CA 8.4): same rules as Sanitize
// except "\n" is kept ("\r\n"/"\r" -> "\n"); idempotent.
func SanitizeInput(s string) string {
	// TODO(core): Task 6
	return ""
}
