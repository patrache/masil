package textwidth_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
)

// spoofRunes are the bidi controls and invisible format characters another
// user can put in a name or a message to make the screen (or a desktop
// notification) show something other than what was sent (pending-fixes 1):
// U+202E + "txt.exe" reads as "exe.txt", a zero-width space splits a handle that looks
// whole, U+2028 starts a fake second line.
var spoofRunes = []rune{
	0x202a, 0x202b, 0x202c, 0x202d, 0x202e, // LRE RLE PDF LRO RLO
	0x2066, 0x2067, 0x2068, 0x2069, // LRI RLI FSI PDI
	0x200e, 0x200f, 0x061c, // LRM RLM ALM
	0x200b, 0x200c, // ZWSP ZWNJ
	0x2060, 0x2061, 0x2062, 0x2063, 0x2064, // WJ, invisible operators
	0xfeff, 0x00ad, // BOM/ZWNBSP, soft hyphen
	0x2028, 0x2029, // line / paragraph separator
	// Default-ignorable runes outside Cf (pending-fixes 1 review): zero-width
	// in both width models, or a blank cell, so they split or blank a name
	// just like U+200B does.
	0x034f,         // combining grapheme joiner
	0xfe00, 0xfe0d, // VS1, VS14
	0xe0100, 0xe01ef, // VS17, VS256 (can carry hidden data)
	0x180b, 0x180c, 0x180d, 0x180f, // Mongolian free variation selectors
	0x17b4, 0x17b5, // Khmer inherent vowels
	0x115f, 0x1160, 0x3164, 0xffa0, // Hangul fillers (choseong, jungseong, filler, halfwidth)
	0x2065, 0xfff0, 0xe0000, 0xe0080, 0xe0fff, // unassigned default ignorables
	// Noncharacters (Unicode Noncharacter_Code_Point, category Cn): never
	// meant for interchange; U+FFFE/U+FFFF are zero-width in both width
	// models just like CGJ, the others draw an empty or tofu cell
	// (pending-fixes 1, second review).
	0xfdd0, 0xfdef, 0xfffe, 0xffff, 0x1fffe, 0x1ffff, 0x10fffe, 0x10ffff,
}

func TestSanitizeDropsSpoofRunes(t *testing.T) {
	for _, r := range spoofRunes {
		in := "a" + string(r) + "b"
		if got := textwidth.Sanitize(in); got != "ab" {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, "ab")
		}
	}
	for _, c := range []struct{ in, want string }{
		{"\u202etxt.exe", "txt.exe"},
		{"\u2067evil\u2069 님", "evil 님"},
		{"ad\u200bmin", "admin"},
		{"ad\u034fmin", "admin"},
		{"ad\U000E0100\U000E0101min", "admin"},
		{"\u3164", ""},
		{"\u3164\uffa0\u115f\u1160", ""},
		{"葛\U000E0100", "葛"},
		{"ad\ufffemin", "admin"},
		{"ad\uffffmin", "admin"},
		{"\ufffe", ""},
		{"\ufdd0\U0010ffff", ""},
		{"\ufeff안녕", "안녕"},
		{"가짜\u2028[관리자] 비밀번호를 보내세요", "가짜[관리자] 비밀번호를 보내세요"},
		// ZWJ is not dropped up front: rule 3 still needs it to keep only the
		// first emoji of a sequence, then it is gone too.
		{"👨\u200d👩\u200d👧", "👨"},
		{"👨\u200b\u200d👩", "👨"}, // ZWSP gone first, then rule 3
	} {
		if got := textwidth.Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// SanitizeInput keeps line structure: U+2028/U+2029 become "\n" like "\r";
// every other spoofing rune is dropped as in Sanitize.
func TestSanitizeInputSpoofRunes(t *testing.T) {
	for _, r := range spoofRunes {
		in := "a" + string(r) + "b"
		want := "ab"
		if r == 0x2028 || r == 0x2029 {
			want = "a\nb"
		}
		if got := textwidth.SanitizeInput(in); got != want {
			t.Errorf("SanitizeInput(%q) = %q, want %q", in, got, want)
		}
	}
}

// hiddenTables are the runes the screen never draws: format characters (Cf),
// line/paragraph separators, variation selectors and the other default
// ignorable code points (CGJ, Hangul fillers, Khmer inherent vowels, ...) and
// the noncharacters (U+FDD0-FDEF, U+nFFFE/U+nFFFF).
var hiddenTables = []*unicode.RangeTable{unicode.Cf, unicode.Zl, unicode.Zp,
	unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point,
	unicode.Noncharacter_Code_Point}

// Every Cf, Zl, Zp, variation selector and other default-ignorable rune is
// gone from the result, whatever it sits next to; the rule is the Unicode
// property, not a hand-kept list.
func TestSanitizeDropsEveryFormatRune(t *testing.T) {
	for _, tab := range hiddenTables {
		for _, r16 := range tab.R16 {
			for r := rune(r16.Lo); r <= rune(r16.Hi); r += rune(r16.Stride) {
				checkNoFormat(t, r)
			}
		}
		for _, r32 := range tab.R32 {
			for r := rune(r32.Lo); r <= rune(r32.Hi); r += rune(r32.Stride) {
				checkNoFormat(t, r)
			}
		}
	}
	rng := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < 5000; i++ {
		s := randomHostile(rng)
		for name, fn := range map[string]func(string) string{"Sanitize": textwidth.Sanitize, "SanitizeInput": textwidth.SanitizeInput} {
			if r, ok := formatRune(fn(s)); ok {
				t.Fatalf("%s(%q) kept %U", name, s, r)
			}
		}
	}
}

func checkNoFormat(t *testing.T, r rune) {
	t.Helper()
	for _, in := range []string{string(r), "a" + string(r) + "b", "가" + string(r), string(r) + "👍", "👨" + string(r) + "\u200d👩"} {
		for name, fn := range map[string]func(string) string{"Sanitize": textwidth.Sanitize, "SanitizeInput": textwidth.SanitizeInput} {
			out := fn(in)
			if f, ok := formatRune(out); ok {
				t.Errorf("%s(%q) = %q kept %U", name, in, out, f)
			}
			if fn(out) != out {
				t.Errorf("%s not idempotent for %q", name, in)
			}
			assertWidthStable(t, in, out)
		}
	}
}

func formatRune(s string) (rune, bool) {
	i := strings.IndexFunc(s, func(r rune) bool {
		return unicode.In(r, hiddenTables...)
	})
	if i < 0 {
		return 0, false
	}
	return []rune(s[i:])[0], true
}

// Invisible is exactly the union of hiddenTables over all of Unicode, so the
// parsers (markup) and Sanitize agree on which runes the screen never shows.
func TestInvisible(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if got, want := textwidth.Invisible(r), unicode.In(r, hiddenTables...); got != want {
			t.Errorf("Invisible(%U) = %v, want %v", r, got, want)
		}
	}
	for _, r := range []rune{'a', '가', ' ', '\n', 0x0301, 0x1f44d, 0x1100} {
		if textwidth.Invisible(r) {
			t.Errorf("Invisible(%U) = true for a visible rune", r)
		}
	}
}

// The rule behind Invisible is "zero-width in both width models and not a
// combining mark" (a lone mark gets a base space, rule 5): such a rune draws
// nothing, so "ad"+r+"min" reads "admin". No rune outside Invisible may meet
// that condition, whatever the width tables of the next library upgrade say
// (pending-fixes 1, second review: U+FFFE/U+FFFF did).
func TestNoZeroWidthRuneOutsideInvisible(t *testing.T) {
	for r := rune(0x20); r <= unicode.MaxRune; r++ {
		if r >= 0x7f && r <= 0x9f || r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if textwidth.Invisible(r) || unicode.Is(unicode.M, r) {
			continue
		}
		s := string(r)
		if ansi.StringWidth(s) == 0 && ansi.StringWidthWc(s) == 0 {
			t.Errorf("%U is zero-width in both width models but not Invisible", r)
		}
	}
	for _, s := range []string{"ad\ufffemin", "ad\uffffmin"} {
		if got := textwidth.Sanitize(s); got != "admin" {
			t.Errorf("Sanitize(%q) = %q, want admin", s, got)
		}
	}
}

// Blank is true for text the screen shows as nothing: empty once sanitized,
// or only space separators and the runes fonts draw as an empty cell
// (U+2800 BRAILLE PATTERN BLANK, U+1D159 MUSICAL SYMBOL NULL NOTEHEAD). A
// display name like that must fall back to the handle like an empty one
// (pending-fixes 1, second review).
func TestBlank(t *testing.T) {
	for _, s := range []string{"", " ", "\t", "\u3164", "\u3164\uffa0\u115f\u1160", "\u200b\u202e",
		"\u034f\U000e0100", "\ufffe", "\uffff\ufdd0", "\u2800", "\u2800\u2800 \u2800", "\U0001d159",
		"\u00a0\u3000\u2003", "\x1b[31m\x1b[0m"} {
		if !textwidth.Blank(s) {
			t.Errorf("Blank(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"a", "민지", "\u2800a", "\xff", "\u0301", "_", "\u3164민지", "👍"} {
		if textwidth.Blank(s) {
			t.Errorf("Blank(%q) = true, want false", s)
		}
	}
}
