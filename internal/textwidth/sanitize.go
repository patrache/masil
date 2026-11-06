// Package textwidth measures, normalizes, truncates and wraps terminal text (CA 4.7, 8).
package textwidth

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"
)

const (
	// placeholder replaces invalid UTF-8, U+FFFD and graphemes whose grapheme
	// width differs from their wcwidth (CA 8.2 rule 7). It is 1/1 wide.
	placeholder = "□"
	tabSpaces   = "    "
	zwj         = '‍'

	// maxPasses bounds the fixpoint loop; replacements can merge with
	// neighbors into new graphemes (e.g. a dropped ZWJ tail leaving NFD
	// jamo that NFC composes on the next pass), so one pass is not always
	// enough for idempotence.
	maxPasses = 16
)

// Sanitize is the display-only normalization (CA 8.2); idempotent; every grapheme
// of the result has StringWidth == StringWidthWc.
func Sanitize(s string) string {
	return sanitize(s, false)
}

// SanitizeInput is the text-entry normalization (CA 8.4): same rules as Sanitize
// except "\n" is kept ("\r\n"/"\r"/U+2028/U+2029 -> "\n"); idempotent.
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
		s = lineSeparators.Replace(s)
	}
	// Rule 1 (+ plan decision): invalid UTF-8 and U+FFFD become □, ANSI
	// escapes are stripped, then C0/C1 controls, line/paragraph separators and
	// format characters (bidi, zero-width, BOM, soft hyphen) removed (tab -> 4
	// spaces).
	s = validUTF8(s)
	s = validUTF8(ansi.Strip(s))
	s = filterRunes(s, keepNewlines)
	// Rule 6: NFC-compose (NFD Hangul from macOS file names would otherwise
	// fail rule 7 cluster by cluster). The server stores NFC as well.
	s = norm.NFC.String(s)
	// Rules 3, 5 and 7 work per grapheme.
	return perGrapheme(s)
}

// lineSeparators turns U+2028/U+2029 into "\n" for text entry; Sanitize drops
// them like "\n" (rule 1).
var lineSeparators = strings.NewReplacer("\u2028", "\n", "\u2029", "\n")

func validUTF8(s string) string {
	s = strings.ToValidUTF8(s, placeholder)
	return strings.ReplaceAll(s, string(utf8.RuneError), placeholder)
}

// filterRunes implements rule 1 (controls, U+2028/U+2029, format characters),
// rule 2 (VS15/VS16) and rule 4 (skin tones, keycap, tag characters).
//
// Format characters (category Cf) are invisible and can make the screen show
// something other than what was sent: bidi embeddings/overrides/isolates and
// marks reorder text ("\u202e" + "txt.exe" reads as "exe.txt"), zero-width
// spaces/joiners/BOM/soft hyphens split or pad a name that looks whole
// (pending-fixes 1). All of them go except ZWJ, which rule 3 needs to cut an
// emoji sequence (and which never survives rule 3 either).
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
		case r == 0x2028, r == 0x2029: // line / paragraph separator
		case r != zwj && unicode.Is(unicode.Cf, r): // bidi, zero-width, BOM, SHY, tags...
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
	// Keep every rune, each conjoining jamo turned into a form that cannot
	// cluster (e.g. "ᄀ가" -> "ㄱ가"), when that makes every grapheme stable.
	if alt := standaloneRunes(g); widthStable(alt) {
		return alt
	}
	r, _ := utf8.DecodeRuneInString(g)
	rs := standalone(r)
	if rw := ansi.StringWidth(rs); rw > 0 && rw == ansi.StringWidthWc(rs) {
		return rs
	}
	return placeholder + strings.Repeat(" ", max(gw, 1)-1)
}

// standaloneRunes maps each conjoining jamo of g with standalone.
func standaloneRunes(g string) string {
	var b strings.Builder
	for _, r := range g {
		b.WriteString(standalone(r))
	}
	return b.String()
}

// standalone returns r, except that a conjoining Hangul jamo (which clusters
// with neighboring jamo and syllables) becomes its compatibility jamo, or a
// placeholder as wide as the jamo when there is none.
func standalone(r rune) string {
	if !conjoiningJamo(r) {
		return string(r)
	}
	if c, ok := compatJamo[r]; ok {
		return string(c)
	}
	return placeholder + strings.Repeat(" ", max(ansi.StringWidth(string(r)), 1)-1)
}

func conjoiningJamo(r rune) bool {
	return r >= 0x1100 && r <= 0x11ff || r >= 0xa960 && r <= 0xa97f || r >= 0xd7b0 && r <= 0xd7ff
}

// widthStable reports whether every grapheme of s has StringWidth == StringWidthWc.
func widthStable(s string) bool {
	for rest := s; rest != ""; {
		g, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if g == "" {
			return false
		}
		if ansi.StringWidth(g) != ansi.StringWidthWc(g) {
			return false
		}
		rest = rest[len(g):]
	}
	return true
}

// compatJamo maps modern conjoining jamo (and arae-a) to Hangul Compatibility Jamo.
var compatJamo = func() map[rune]rune {
	m := map[rune]rune{0x119e: 0x318d, 0x11a1: 0x318e}
	initials := []rune("ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ")
	for i, c := range initials {
		m[0x1100+rune(i)] = c
	}
	for i := rune(0); i < 21; i++ { // ㅏ..ㅣ
		m[0x1161+i] = 0x314f + i
	}
	finals := []rune("ㄱㄲㄳㄴㄵㄶㄷㄹㄺㄻㄼㄽㄾㄿㅀㅁㅂㅄㅅㅆㅇㅈㅊㅋㅌㅍㅎ")
	for i, c := range finals {
		m[0x11a8+rune(i)] = c
	}
	return m
}()

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
