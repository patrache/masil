package textwidth_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"

	"github.com/patrache/masil/internal/textwidth"
)

var sanitizeCases = []struct{ in, want string }{
	{"❄️ 첫눈", "❄ 첫눈"}, {"⚠️", "⚠"}, {"👍🏽", "👍"}, {"👨‍👩‍👧 가족", "👨 가족"}, {"1️⃣", "1"},
	{"🇰", "□ "}, {"🇰🇷", "🇰🇷"}, {"क्षि", "क"}, {"\u1100\u1161", "\uac00"},
	// NFD Hangul is NFC-composed before rule 7 (CA 8.2 rule 6); a jamo cluster
	// that does not compose falls back to compatibility jamo, never losing runes.
	{"\u1100\u1161\u11a8", "\uac01"}, {nfdFile, "회의록.pdf 한글 파일"},
	{"\u1100\uac00\ub098", "\u3131\uac00\ub098"}, {"\u1100\u119e", "\u3131\u318d"}, {"\u1100\u1100\u1161", "\u3131\uac00"},
	{"\x1b[31m빨강\x1b[0m", "빨강"}, {"\x1b]8;;https://x\x07링크\x1b]8;;\x07", "링크"},
	{"a\tb", "a    b"}, {"a\x00b\x07c\u0085d", "abcd"}, {"줄\n바꿈", "줄바꿈"},
	{"\xff\xfe", "□"}, {"�", "□"}, {"태그\U000E0067", "태그"},
}

// nfdFile is "회의록.pdf 한글 파일" in NFD (macOS file names, drag and drop).
var nfdFile = norm.NFD.String("회의록.pdf 한글 파일")

func TestSanitizeCases(t *testing.T) {
	for _, c := range sanitizeCases {
		if got := textwidth.Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeCasesAreWidthStable(t *testing.T) {
	for _, c := range sanitizeCases {
		assertWidthStable(t, c.in, textwidth.Sanitize(c.in))
	}
}

// Review Focus 4: invalid bytes and U+FFFD never survive; result is valid UTF-8.
func TestSanitizeInvalidUTF8(t *testing.T) {
	inputs := []string{
		"\xff", "\xff\xfe", "a\xc3b", "\xed\xa0\x80", "\xe2\x82", "가\x80나",
		"�", "a�b�", "\xf0\x9f\x91", "\x1b[31m\xff\x1b[0m",
		"\xc2\x85", "\xc2\x9b31m", "\x9b31m",
	}
	for _, in := range inputs {
		for name, fn := range map[string]func(string) string{"Sanitize": textwidth.Sanitize, "SanitizeInput": textwidth.SanitizeInput} {
			out := fn(in)
			if !utf8.ValidString(out) {
				t.Errorf("%s(%q) = %q is not valid UTF-8", name, in, out)
			}
			if strings.ContainsRune(out, utf8.RuneError) {
				t.Errorf("%s(%q) = %q contains U+FFFD", name, in, out)
			}
		}
	}
	rng := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 2000; i++ {
		s := randomHostile(rng)
		for _, out := range []string{textwidth.Sanitize(s), textwidth.SanitizeInput(s)} {
			if !utf8.ValidString(out) || strings.ContainsRune(out, utf8.RuneError) {
				t.Fatalf("%q -> %q: invalid UTF-8 or U+FFFD survived", s, out)
			}
		}
	}
}

func TestSanitizeProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 10000; i++ {
		s := randomHostile(rng)
		out := textwidth.Sanitize(s)
		if textwidth.Sanitize(out) != out {
			t.Fatalf("not idempotent %q: %q -> %q", s, out, textwidth.Sanitize(out))
		}
		if strings.ContainsAny(out, "\n\r\t\x1b") {
			t.Fatalf("%q: control survived in %q", s, out)
		}
		assertWidthStable(t, s, out)
	}
}

func TestSanitizeInputKeepsNewlines(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\r\nb\rc\n", "a\nb\nc\n"},
		{"❄️\t", "❄    "},
		{"줄\n바꿈", "줄\n바꿈"},
		{"\x1b[31m빨강\x1b[0m\r\n", "빨강\n"},
		{"\U0001F44D\U0001F3FD\n\U0001F468\u200d\U0001F469\u200d\U0001F467", "\U0001F44D\n\U0001F468"},
		{"a\x00b\u0085c", "abc"},
	}
	for _, c := range cases {
		if got := textwidth.SanitizeInput(c.in); got != c.want {
			t.Errorf("SanitizeInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	rng := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 10000; i++ {
		s := randomHostile(rng)
		out := textwidth.SanitizeInput(s)
		if textwidth.SanitizeInput(out) != out {
			t.Fatalf("not idempotent %q: %q -> %q", s, out, textwidth.SanitizeInput(out))
		}
		if strings.ContainsAny(out, "\r\t\x1b") {
			t.Fatalf("%q: control survived in %q", s, out)
		}
		assertWidthStable(t, s, out)
	}
}

// assertWidthStable checks CA 8.2: every grapheme has grapheme width == wcwidth.
func assertWidthStable(t *testing.T, in, out string) {
	t.Helper()
	for g, rest := "", out; rest != ""; rest = rest[len(g):] {
		g, _ = ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if ansi.StringWidth(g) != ansi.StringWidthWc(g) {
			t.Fatalf("%q: grapheme %q %d/%d", in, g, ansi.StringWidth(g), ansi.StringWidthWc(g))
		}
	}
	if ansi.StringWidth(out) != ansi.StringWidthWc(out) {
		t.Fatalf("%q -> %q: string width %d/%d", in, out, ansi.StringWidth(out), ansi.StringWidthWc(out))
	}
}

type runeRange struct{ lo, hi rune }

var hostileRunePools = [][]runeRange{
	{{0x20, 0x7e}},                       // ASCII
	{{0xac00, 0xd7a3}},                   // Hangul syllables
	{{0x1100, 0x11ff}},                   // Hangul Jamo (NFD)
	{{0x4e00, 0x9fff}, {0x3000, 0x303f}}, // CJK
	{{0x1f600, 0x1f64f}, {0x1f466, 0x1f469}, {0x1f44d, 0x1f44d}, {0x2744, 0x2744}, {0x26a0, 0x26a0}, {0x2764, 0x2764}, {0x1fae1, 0x1fae1}, {0x1f525, 0x1f525}}, // emoji
	{{0xfe0e, 0xfe0f}},                             // VS15/VS16
	{{0x200d, 0x200d}},                             // ZWJ
	{{0x1f3fb, 0x1f3ff}},                           // skin tones
	{{0x1f1e6, 0x1f1ff}},                           // regional indicators
	{{0x20e3, 0x20e3}, {0x30, 0x39}, {0x23, 0x23}}, // keycap
	{{0xe0020, 0xe007f}, {0x1f3f4, 0x1f3f4}},       // tag chars (+ black flag base)
	{{0x0300, 0x036f}, {0x20d0, 0x20ff}},           // combining marks
	{{0x0900, 0x097f}},                             // Devanagari
	{{0x0000, 0x001f}, {0x007f, 0x009f}},           // C0/C1
	{{0x0e00, 0x0e7f}, {0x0600, 0x06ff}, {0x200b, 0x200f}, {0x2028, 0x202e}, {0x00ad, 0x00ad}},                                                         // Thai, Arabic, format chars
	{{0x034f, 0x034f}, {0xfe00, 0xfe0d}, {0xe0100, 0xe01ef}, {0x180b, 0x180f}, {0x17b4, 0x17b5}, {0x115f, 0x1160}, {0x3164, 0x3164}, {0xffa0, 0xffa0}}, // default ignorables
	{{0xfffd, 0xfffd}}, // replacement char
}

var hostileChunks = []string{
	"\x1b[31m", "\x1b[0m", "\x1b]8;;https://x\x07", "\x1b]8;;\x07", "\x1b[", "\x1b", "\x1b]0;title\x1b\\",
	"\x1bP+q\x1b\\", "\x9b31m", "\xff", "\xfe", "\xc3", "\xed\xa0\x80", "\xe2\x82", "\xf0\x9f\x91",
	"\r\n", "\r", "\n", "\t",
	"👨‍👩‍👧", "👍🏽", "1️⃣", "❄️", "🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F", "🇰🇷", "\u0915\u094d\u0937\u093f", "\u1100\u1161", "\u1100\u1161\u1100\u1161", "\u1100\u1100", "\u0301",
	nfdFile, norm.NFD.String("대한민국 서울 각"), "\u1100가나다", "\u1100\u119e\u11a8", "\u1100\u1100\u1100\u1161", "\u1161\u11a8",
}

func randomHostile(rng *rand.Rand) string {
	var b strings.Builder
	n := rng.IntN(16)
	for i := 0; i < n; i++ {
		if rng.IntN(5) == 0 {
			b.WriteString(hostileChunks[rng.IntN(len(hostileChunks))])
			continue
		}
		pool := hostileRunePools[rng.IntN(len(hostileRunePools))]
		r := pool[rng.IntN(len(pool))]
		b.WriteRune(r.lo + rune(rng.IntN(int(r.hi-r.lo)+1)))
	}
	return b.String()
}

// Sanitize must reach its fixpoint in one call (CA 8.2 idempotence): leftover
// L jamo once merged with the next syllable and ate one syllable per pass.
func TestSanitizeIdempotentHangul(t *testing.T) {
	syllables := "가나다라마바사아자차카타파하거너더러머버서어저처커터퍼허"
	inputs := []string{
		"\u1100" + syllables, nfdFile, norm.NFD.String(syllables), norm.NFD.String("/Users/sam/Desktop/회의록 2026.pdf"),
		strings.Repeat("\u1100", 40) + "\u1161", strings.Repeat("\u1100\u119e", 20),
	}
	for _, in := range inputs {
		for name, fn := range map[string]func(string) string{"Sanitize": textwidth.Sanitize, "SanitizeInput": textwidth.SanitizeInput} {
			out := fn(in)
			if again := fn(out); again != out {
				t.Errorf("%s not idempotent for %q: %q -> %q", name, in, out, again)
			}
			if utf8.RuneCountInString(out) < utf8.RuneCountInString(norm.NFC.String(in)) {
				t.Errorf("%s(%q) = %q lost runes", name, in, out)
			}
			assertWidthStable(t, in, out)
		}
	}
	if got, want := textwidth.Sanitize("\u1100"+syllables), "ㄱ"+syllables; got != want {
		t.Errorf("Sanitize(ᄀ+syllables) = %q, want %q", got, want)
	}
}
