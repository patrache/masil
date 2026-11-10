package markup_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/markup"
)

func hasHangul(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) })
}

// TestMarkupTailsLocalized (P5 Task 15): the "more" tail and the code-block
// summary come from the caller (RenderOptions.MoreTail / CodeSummary, Plain's
// codeSummary) — en "… more", "‹code: 3 lines›"; markup itself has no Korean.
func TestMarkupTailsLocalized(t *testing.T) {
	en, ko := i18n.For(i18n.En), i18n.KoT()
	for _, c := range []struct{ got, want string }{
		{en.Common.MoreTail(), "… more"},
		{en.Common.CodeSummary(1), "‹code: 1 line›"},
		{en.Common.CodeSummary(3), "‹code: 3 lines›"},
		{ko.Common.MoreTail(), "… 더 보기"},
		{ko.Common.CodeSummary(3), "‹코드 3줄›"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}

	long := markup.Parse("one\ntwo\nthree\nfour", markup.ParseOptions{})
	lines := markup.Render(long, amber, markup.RenderOptions{Width: 40, MaxLines: 2, MoreTail: en.Common.MoreTail()})
	if len(lines) != 2 || !strings.HasSuffix(ansi.Strip(lines[1]), "… more") {
		t.Errorf("en MaxLines tail: %q", lines)
	}
	code := markup.Parse("see\n```\na\nb\nc\n```", markup.ParseOptions{})
	got := ansi.Strip(strings.Join(markup.Render(code, amber, markup.RenderOptions{Width: 60, Compact: true, CodeSummary: en.Common.CodeSummary}), "\n"))
	if !strings.Contains(got, "‹code: 3 lines›") {
		t.Errorf("en compact code summary: %q", got)
	}
	if got := markup.Plain("see\n```\na\nb\nc\n```", 60, en.Common.CodeSummary); !strings.Contains(got, "‹code: 3 lines›") {
		t.Errorf("en Plain code summary: %q", got)
	}
	if got := markup.Plain("see\n```\na\nb\nc\n```", 60, ko.Common.CodeSummary); !strings.Contains(got, "‹코드 3줄›") {
		t.Errorf("ko Plain code summary: %q", got)
	}

	// No options: no Korean of markup's own.
	bare := append(markup.Render(long, amber, markup.RenderOptions{Width: 40, MaxLines: 2}),
		markup.Render(code, amber, markup.RenderOptions{Width: 60, Compact: true})...)
	bare = append(bare, markup.Plain("see\n```\na\n```", 60, nil))
	for _, l := range bare {
		if hasHangul(l) {
			t.Errorf("markup default %q has Korean", ansi.Strip(l))
		}
	}
}
