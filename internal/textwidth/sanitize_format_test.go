package textwidth_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"

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

// Every Cf character (and Zl/Zp) is gone from the result, whatever it sits
// next to; the rule is the category, not a hand-kept list.
func TestSanitizeDropsEveryFormatRune(t *testing.T) {
	for _, tab := range []*unicode.RangeTable{unicode.Cf, unicode.Zl, unicode.Zp} {
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
		return unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
	if i < 0 {
		return 0, false
	}
	return []rune(s[i:])[0], true
}
