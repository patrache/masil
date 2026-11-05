// Package textwidth measures, normalizes, truncates and wraps terminal text (CA 4.7, 8).
package textwidth

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	// placeholder replaces invalid UTF-8, U+FFFD and graphemes whose grapheme
	// width differs from their wcwidth (CA 8.2 rule 7). It is 1/1 wide.
	placeholder = "□"
	tabSpaces   = "    "
	zwj         = '‍'

	// maxPasses bounds the fixpoint loop; replacements can merge with
	// neighbors into new graphemes (e.g. NFD jamo), so one pass is not
	// always enough for idempotence.
	maxPasses = 16
)

// Sanitize is the display-only normalization (CA 8.2); idempotent; every grapheme
// of the result has StringWidth == StringWidthWc.
func Sanitize(s string) string {
	return sanitize(s, false)
}

// SanitizeInput is the text-entry normalization (CA 8.4): same rules as Sanitize
// except "\n" is kept ("\r\n"/"\r" -> "\n"); idempotent.
func SanitizeInput(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, keepNewlines bool) string {
	for i := 0; i < maxPasses; i++ {
		out := sanitizePass(s, keepNewlines)
		if out == s {
			return out
		}
		s = out
	}
	return s
}

// sanitizePass applies CA 8.2 rules 1-7 once, in order.
func sanitizePass(s string, keepNewlines bool) string {
	if keepNewlines {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	// Rule 1 (+ plan decision): invalid UTF-8 and U+FFFD become □, ANSI
	// escapes are stripped, then C0/C1 controls removed (tab -> 4 spaces).
	s = validUTF8(s)
	s = validUTF8(ansi.Strip(s))
	s = filterRunes(s, keepNewlines)
	// Rules 3, 5 and 7 work per grapheme.
	return perGrapheme(s)
}

func validUTF8(s string) string {
	s = strings.ToValidUTF8(s, placeholder)
	return strings.ReplaceAll(s, string(utf8.RuneError), placeholder)
}

// filterRunes implements rule 1 (controls), rule 2 (VS15/VS16) and rule 4
// (skin tones, keycap, tag characters).
func filterRunes(s string, keepNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString(tabSpaces)
		case r == '\n' && keepNewlines:
			b.WriteRune(r)
		case r < 0x20, r >= 0x7f && r <= 0x9f: // C0, DEL, C1
		case r == 0xfe0e, r == 0xfe0f: // VS15, VS16
		case r >= 0x1f3fb && r <= 0x1f3ff: // skin tone modifiers
		case r == 0x20e3: // combining enclosing keycap
		case r >= 0xe0020 && r <= 0xe007f: // tag characters
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func perGrapheme(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for rest := s; rest != ""; {
		g, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if g == "" { // defensive: never loop forever
			_, n := utf8.DecodeRuneInString(rest)
			g = rest[:n]
		}
		rest = rest[len(g):]
		b.WriteString(fixGrapheme(g))
	}
	return b.String()
}

func fixGrapheme(g string) string {
	// Rule 3: a ZWJ sequence keeps only its first component.
	if i := strings.IndexRune(g, zwj); i >= 0 {
		g = g[:i]
		if g == "" {
			return ""
		}
	}
	// Rule 5: a grapheme made only of combining marks gets a base space.
	if onlyMarks(g) {
		g = " " + g
	}
	// Rule 7: grapheme width must equal wcwidth.
	gw := ansi.StringWidth(g)
	if gw == ansi.StringWidthWc(g) {
		return g
	}
	r, _ := utf8.DecodeRuneInString(g)
	rs := string(r)
	if rw := ansi.StringWidth(rs); rw > 0 && rw == ansi.StringWidthWc(rs) {
		return rs
	}
	return placeholder + strings.Repeat(" ", max(gw, 1)-1)
}

func onlyMarks(g string) bool {
	if g == "" {
		return false
	}
	for _, r := range g {
		if !unicode.Is(unicode.M, r) {
			return false
		}
	}
	return true
}
