package textwidth_test

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
)

func TestWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0}, {"abc", 3}, {"가나", 4}, {"❄", 1}, {"👍", 2}, {"\x1b[1m가\x1b[0m", 2}, {"□ ", 2},
	}
	for _, c := range cases {
		if got := textwidth.Width(c.in); got != c.want {
			t.Errorf("Width(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		tail string
		want string
	}{
		{"가나다", 5, "…", "가나…"},
		{"가나다", 4, "…", "가… "}, // wide char not split; 1 leftover cell padded
		{"ab", 5, "…", "ab"},
		{"가나", 0, "…", ""},
		{"가나", -3, "…", ""},
		{"가나", 4, "…", "가나"},
		{"가나다", 3, "", "가 "},
		{"abcdef", 4, "...", "a..."},
	}
	for _, c := range cases {
		if got := textwidth.Truncate(c.in, c.w, c.tail); got != c.want {
			t.Errorf("Truncate(%q, %d, %q) = %q, want %q", c.in, c.w, c.tail, got, c.want)
		}
	}
	styled := textwidth.Truncate("\x1b[1m가나다\x1b[0m", 3, "…")
	if textwidth.Width(styled) != 3 || ansi.Strip(styled) != "가…" {
		t.Errorf("styled truncate = %q (width %d)", styled, textwidth.Width(styled))
	}
	if got := textwidth.Truncate("가나다", 1, "……"); textwidth.Width(got) > 1 {
		t.Errorf("tail wider than w: %q", got)
	}
}

func TestTruncateLeft(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		head string
		want string
	}{
		{"가나다", 5, "…", "…나다"},
		{"가나다", 4, "…", " …다"}, // wide char not split; 1 leftover cell padded
		{"가나다", 6, "…", "가나다"},
		{"가나다", 0, "…", ""},
		{"abcdef", 4, "…", "…def"},
		{"abcdef", 1, "…", "…"},
		{"abcdef", 1, "<<", "<"},
	}
	for _, c := range cases {
		if got := textwidth.TruncateLeft(c.in, c.w, c.head); got != c.want {
			t.Errorf("TruncateLeft(%q, %d, %q) = %q, want %q", c.in, c.w, c.head, got, c.want)
		}
	}
	styled := textwidth.TruncateLeft("\x1b[1m가나다\x1b[0m", 3, "…")
	if textwidth.Width(styled) != 3 || ansi.Strip(styled) != "…다" {
		t.Errorf("styled truncate-left = %q (width %d)", styled, textwidth.Width(styled))
	}
}

func TestWrap(t *testing.T) {
	if got, want := textwidth.Wrap("가나다라마바사", 5), []string{"가나", "다라", "마바", "사"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Wrap hangul = %q, want %q", got, want)
	}
	in := "오늘 배포 몇 시에 해요?"
	lines := textwidth.Wrap(in, 10)
	for _, l := range lines {
		if textwidth.Width(l) > 10 {
			t.Errorf("line %q wider than 10", l)
		}
	}
	if strings.ReplaceAll(strings.Join(lines, ""), " ", "") != strings.ReplaceAll(in, " ", "") {
		t.Errorf("Wrap lost text: %q", lines)
	}
	if got := textwidth.Wrap(strings.Repeat("x", 25), 10); len(got) != 3 || got[0] != strings.Repeat("x", 10) || got[2] != "xxxxx" {
		t.Errorf("hard wrap = %q", got)
	}
	if got := textwidth.Wrap("", 10); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("Wrap empty = %q", got)
	}
	if got := textwidth.Wrap("가나다", 0); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("Wrap w=0 = %q", got)
	}
	if got := textwidth.Wrap("가나다", -1); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("Wrap w<0 = %q", got)
	}
	if got, want := textwidth.Wrap("hello world foo", 11), []string{"hello world", "foo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("word wrap = %q, want %q", got, want)
	}
	if got, want := textwidth.Wrap("one\ntwo three", 5), []string{"one", "two", "three"}; !reflect.DeepEqual(got, want) {
		t.Errorf("newline wrap = %q, want %q", got, want)
	}
	// Hangul is breakable between syllables: the first line is filled.
	if got, want := textwidth.Wrap("안녕 가나다라", 7), []string{"안녕 가", "나다라"}; !reflect.DeepEqual(got, want) {
		t.Errorf("syllable fill = %q, want %q", got, want)
	}
	// Closing punctuation stays with the preceding syllable.
	for _, l := range textwidth.Wrap("가나다라?", 8) {
		if strings.HasPrefix(l, "?") {
			t.Errorf("line starts with closing punctuation: %q", l)
		}
	}
	// A grapheme wider than w never overflows.
	for _, l := range textwidth.Wrap("가a", 1) {
		if textwidth.Width(l) > 1 {
			t.Errorf("w=1 line %q too wide", l)
		}
	}
	// ANSI styling is kept and not counted.
	styled := textwidth.Wrap("\x1b[1m가나다\x1b[0m", 4)
	if len(styled) != 2 || ansi.Strip(strings.Join(styled, "")) != "가나다" {
		t.Errorf("styled wrap = %q", styled)
	}
}

func TestWrapProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < 3000; i++ {
		s := textwidth.SanitizeInput(randomHostile(rng))
		w := rng.IntN(12)
		lines := textwidth.Wrap(s, w)
		if len(lines) == 0 {
			t.Fatalf("Wrap(%q, %d) returned no lines", s, w)
		}
		for _, l := range lines {
			if textwidth.Width(l) > w || (w <= 0 && l != "") {
				t.Fatalf("Wrap(%q, %d): line %q width %d", s, w, l, textwidth.Width(l))
			}
			if strings.Contains(l, "\n") {
				t.Fatalf("Wrap(%q, %d): line %q contains newline", s, w, l)
			}
		}
	}
}

func TestPadAndCenter(t *testing.T) {
	cases := []struct{ got, want string }{
		{textwidth.PadRight("가", 4), "가  "},
		{textwidth.PadLeft("가", 4), "  가"},
		{textwidth.Center("가", 5), " 가  "},
		{textwidth.PadRight("가나다", 3), "가 "},
		{textwidth.PadLeft("가나다", 3), "가 "},
		{textwidth.Center("가나다", 3), "가 "},
		{textwidth.PadRight("ab", 0), ""},
		{textwidth.PadRight("ab", -1), ""},
		{textwidth.Center("ab", 2), "ab"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d = %q, want %q", i, c.got, c.want)
		}
	}
}

func TestFit(t *testing.T) {
	if got, want := textwidth.Fit([]string{"가나다"}, 5, 2), []string{"가나 ", "     "}; !reflect.DeepEqual(got, want) {
		t.Errorf("Fit = %q, want %q", got, want)
	}
	if got := textwidth.Fit(nil, 0, 0); got == nil || len(got) != 0 {
		t.Errorf("Fit(nil,0,0) = %#v, want empty non-nil", got)
	}
	if got, want := textwidth.Fit([]string{"abcd", "가", "x", "y"}, 3, 1), []string{"abc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Fit truncate lines = %q, want %q", got, want)
	}
	got := textwidth.Fit([]string{"\x1b[1m가나다라\x1b[0m", "", "👍👍"}, 3, 4)
	if len(got) != 4 {
		t.Fatalf("Fit len = %d", len(got))
	}
	for _, l := range got {
		if textwidth.Width(l) != 3 {
			t.Errorf("Fit line %q width %d", l, textwidth.Width(l))
		}
	}
}
