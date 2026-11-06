package markup_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui/uitest"
)

const sample = "오늘 배포 몇 시에 해요? **급함** `make deploy` _진짜_\n```bash\n$ wrangler deploy --env prod\n```\n@minji @nobody @here 링크 [문서](https://example.com/docs) https://example.com/very/long/path, #golang snake_case"

var amber = theme.Get(theme.Amber)

var minji = api.MentionRef{UserID: "01JBMINJI", Handle: "minji"}

// nonText returns the non-Text inlines of every paragraph, in order.
func nonText(d markup.Doc) []markup.Inline {
	var out []markup.Inline
	for _, b := range d.Blocks {
		for _, in := range b.Inline {
			if in.Kind != markup.Text {
				out = append(out, in)
			}
		}
	}
	return out
}

func allText(d markup.Doc) string {
	var b strings.Builder
	for _, bl := range d.Blocks {
		for _, in := range bl.Inline {
			if in.Kind == markup.Text {
				b.WriteString(in.Text)
			}
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	d := markup.Parse(sample, markup.ParseOptions{})
	if len(d.Blocks) != 3 {
		t.Fatalf("blocks = %d (%+v), want 3", len(d.Blocks), d.Blocks)
	}
	kinds := []markup.BlockKind{d.Blocks[0].Kind, d.Blocks[1].Kind, d.Blocks[2].Kind}
	if !reflect.DeepEqual(kinds, []markup.BlockKind{markup.Paragraph, markup.Code, markup.Paragraph}) {
		t.Fatalf("block kinds = %v", kinds)
	}
	if c := d.Blocks[1]; c.Lang != "bash" || c.Code != "$ wrangler deploy --env prod" {
		t.Fatalf("code block = %+v", c)
	}
	want := []markup.Inline{
		{Kind: markup.Bold, Text: "급함"},
		{Kind: markup.InlineCode, Text: "make deploy"},
		{Kind: markup.Italic, Text: "진짜"},
		{Kind: markup.Mention, Text: "@minji", Handle: "minji"},
		{Kind: markup.Mention, Text: "@nobody", Handle: "nobody"},
		{Kind: markup.Here, Text: "@here"},
		{Kind: markup.Link, Text: "문서", URL: "https://example.com/docs"},
		{Kind: markup.Link, Text: "https://example.com/very/long/path", URL: "https://example.com/very/long/path"},
	}
	if got := nonText(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("inlines =\n%+v\nwant\n%+v", got, want)
	}
	txt := allText(d)
	for _, s := range []string{"오늘 배포 몇 시에 해요? ", ", #golang snake_case"} {
		if !strings.Contains(txt, s) {
			t.Errorf("text %q missing %q", txt, s)
		}
	}

	// Hashtags only when enabled.
	h := nonText(markup.Parse(sample, markup.ParseOptions{Hashtags: true}))
	last := h[len(h)-1]
	if last.Kind != markup.Hashtag || last.Text != "#golang" || last.Handle != "golang" {
		t.Fatalf("hashtag = %+v", last)
	}

	// "@x" inside code spans is not a mention.
	in := nonText(markup.Parse("see `@minji` here", markup.ParseOptions{}))
	if len(in) != 1 || in[0].Kind != markup.InlineCode || in[0].Text != "@minji" {
		t.Fatalf("code span mention = %+v", in)
	}

	// Unclosed fence => code to end.
	u := markup.Parse("hi\n```go\nfunc a() {}\n@minji", markup.ParseOptions{})
	if len(u.Blocks) != 2 || u.Blocks[1].Kind != markup.Code || u.Blocks[1].Lang != "go" || u.Blocks[1].Code != "func a() {}\n@minji" {
		t.Fatalf("unclosed fence = %+v", u.Blocks)
	}
}

func TestParseEdges(t *testing.T) {
	cases := []struct {
		body string
		want []markup.Inline
	}{
		{"snake_case and _진짜_ ok", []markup.Inline{{Kind: markup.Italic, Text: "진짜"}}},
		{"*기울임* x", []markup.Inline{{Kind: markup.Italic, Text: "기울임"}}},
		{"**열림만", nil},
		{"a@minji @ab @abcdefghijklmnopqrstu @MinJi", []markup.Inline{{Kind: markup.Mention, Text: "@MinJi", Handle: "minji"}}},
		{"[x](ftp://a.b) [y](https://a.b)", []markup.Inline{{Kind: markup.Link, Text: "y", URL: "https://a.b"}}},
		{"(see https://a.b/c).", []markup.Inline{{Kind: markup.Link, Text: "https://a.b/c", URL: "https://a.b/c"}}},
		{"``a`b``", []markup.Inline{{Kind: markup.InlineCode, Text: "a`b"}}},
	}
	for _, c := range cases {
		got := nonText(markup.Parse(c.body, markup.ParseOptions{}))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", c.body, got, c.want)
		}
	}
	// Text is preserved exactly when nothing matches.
	body := "**열림만 snake_case a@b.c ftp://x"
	if got := allText(markup.Parse(body, markup.ParseOptions{})); got != body {
		t.Errorf("text = %q, want %q", got, body)
	}
}

func checkLines(t *testing.T, lines []string, w int) {
	t.Helper()
	for i, l := range lines {
		if got := ansi.StringWidth(l); got > w {
			t.Errorf("w=%d line %d width %d: %q", w, i, got, ansi.Strip(l))
		}
		if ansi.StringWidth(l) != ansi.StringWidthWc(l) {
			t.Errorf("w=%d line %d width mismatch %d/%d: %q", w, i, ansi.StringWidth(l), ansi.StringWidthWc(l), ansi.Strip(l))
		}
	}
}

func TestRenderWidthAndSanitize(t *testing.T) {
	body := sample + "\n눈 ❄️ 와 👨‍👩‍👧 그리고 \x1b[31m빨강\x1b[0m\x07"
	d := markup.Parse(body, markup.ParseOptions{Hashtags: true})
	for _, w := range []int{200, 120, 80, 50, 20, 1} {
		lines := markup.Render(d, amber, markup.RenderOptions{Width: w, MyHandle: "sam", Mentions: []api.MentionRef{minji}, Hyperlinks: true})
		if len(lines) == 0 {
			t.Fatalf("w=%d no lines", w)
		}
		checkLines(t, lines, w)
		if w >= 50 {
			all := ansi.Strip(strings.Join(lines, "\n"))
			if !strings.Contains(all, "❄") || strings.Contains(all, "️") || strings.Contains(all, "‍") || strings.Contains(all, "\x07") || strings.Contains(all, "[31m") {
				t.Errorf("w=%d not sanitized: %q", w, all)
			}
		}
	}
}

func TestRenderMentions(t *testing.T) {
	sam := api.MentionRef{UserID: "01JBSAM", Handle: "sam"}
	d := markup.Parse("@minji @nobody @sam", markup.ParseOptions{})
	lines := markup.Render(d, amber, markup.RenderOptions{Width: 80, MyHandle: "sam", Mentions: []api.MentionRef{minji, sam}})
	if len(lines) != 1 {
		t.Fatalf("lines = %q", lines)
	}
	want := amber.S.Mention.Render("@minji") + amber.S.Base.Render(" @nobody ") + amber.S.MentionMe.Render("@sam")
	if lines[0] != want {
		t.Fatalf("got  %q\nwant %q", lines[0], want)
	}
	// @here is bold (MentionMe) only when the message mentions here.
	here := markup.Parse("@here", markup.ParseOptions{})
	if got := markup.Render(here, amber, markup.RenderOptions{Width: 80, MentionHere: true})[0]; got != amber.S.MentionMe.Render("@here") {
		t.Errorf("here = %q", got)
	}
	if got := markup.Render(here, amber, markup.RenderOptions{Width: 80})[0]; got != amber.S.Base.Render("@here") {
		t.Errorf("plain here = %q", got)
	}
}

func TestRenderCompact(t *testing.T) {
	d := markup.Parse(sample, markup.ParseOptions{})
	lines := markup.Render(d, amber, markup.RenderOptions{Width: 200, Compact: true})
	if len(lines) != 1 {
		t.Fatalf("compact lines = %d: %q", len(lines), lines)
	}
	got := ansi.Strip(lines[0])
	if !strings.Contains(got, "_진짜_ ‹코드 1줄› @minji") && !strings.Contains(got, "진짜 ‹코드 1줄› @minji") {
		t.Fatalf("compact = %q", got)
	}
	if strings.Contains(got, "wrangler") {
		t.Fatalf("compact shows code: %q", got)
	}
	narrow := markup.Render(d, amber, markup.RenderOptions{Width: 30, Compact: true})
	if len(narrow) != 1 || ansi.StringWidth(narrow[0]) > 30 || !strings.HasSuffix(ansi.Strip(narrow[0]), "…") {
		t.Fatalf("narrow compact = %q", narrow)
	}
	multi := markup.Render(markup.Parse("a\nb\n\nc", markup.ParseOptions{}), amber, markup.RenderOptions{Width: 80, Compact: true})
	if len(multi) != 1 || ansi.Strip(multi[0]) != "a b  c" {
		t.Fatalf("newlines compact = %q", multi)
	}
}

func TestRenderMaxLines(t *testing.T) {
	d := markup.Parse("줄1\n줄2\n줄3\n줄4\n줄5", markup.ParseOptions{})
	lines := markup.Render(d, amber, markup.RenderOptions{Width: 40, MaxLines: 2})
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if ansi.Strip(lines[0]) != "줄1" || !strings.HasSuffix(ansi.Strip(lines[1]), "… 더 보기") {
		t.Fatalf("lines = %q", lines)
	}
	checkLines(t, lines, 40)
	// Exactly MaxLines => no marker.
	exact := markup.Render(markup.Parse("a\nb", markup.ParseOptions{}), amber, markup.RenderOptions{Width: 40, MaxLines: 2})
	if len(exact) != 2 || strings.Contains(ansi.Strip(exact[1]), "더 보기") {
		t.Fatalf("exact = %q", exact)
	}
	// Narrow width still fits.
	narrow := markup.Render(d, amber, markup.RenderOptions{Width: 6, MaxLines: 2})
	checkLines(t, narrow, 6)
	if len(narrow) != 2 {
		t.Fatalf("narrow = %q", narrow)
	}
}

func TestHighlightFallback(t *testing.T) {
	for _, lang := range []string{"nope", "", "go", "bash"} {
		lines := markup.Highlight(lang, "x := 1\n\tfoo()\n", amber, 20)
		if len(lines) != 3 {
			t.Fatalf("lang %q: lines = %q", lang, lines)
		}
		for i, l := range lines {
			s := ansi.Strip(l)
			if !strings.HasPrefix(s, "▎") {
				t.Errorf("lang %q line %d no bar: %q", lang, i, s)
			}
			if w := ansi.StringWidth(l); w != 20 {
				t.Errorf("lang %q line %d width %d", lang, i, w)
			}
		}
		if s := ansi.Strip(lines[1]); !strings.HasPrefix(s, "▎    foo()") {
			t.Errorf("lang %q tab line = %q", lang, s)
		}
	}
	// Long lines are hard-wrapped at width-1.
	long := markup.Highlight("nope", strings.Repeat("가", 15), amber, 10)
	if len(long) != 4 {
		t.Fatalf("long = %q", long)
	}
	for _, l := range long {
		if ansi.StringWidth(l) != 10 {
			t.Errorf("long line width %d: %q", ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	if got := markup.Highlight("go", "x", amber, 0); len(got) != 0 {
		t.Errorf("width 0 = %q", got)
	}
}

func TestPlain(t *testing.T) {
	p := markup.Plain(sample, 30)
	if strings.Contains(p, "\n") || strings.Contains(p, "**") || strings.Contains(p, "`") {
		t.Fatalf("plain = %q", p)
	}
	if w := textwidth.Width(p); w > 30 {
		t.Fatalf("plain width %d: %q", w, p)
	}
	if !strings.HasSuffix(p, "…") {
		t.Fatalf("plain = %q", p)
	}
	if !strings.HasPrefix(p, "오늘 배포 몇 시에 해요? 급함") {
		t.Fatalf("plain = %q", p)
	}
	full := markup.Plain(sample, 1<<16)
	for _, s := range []string{"make deploy 진짜 ‹코드 1줄› @minji", "링크 문서 https://example.com/very/long/path, #golang snake_case"} {
		if !strings.Contains(full, s) {
			t.Errorf("full plain %q missing %q", full, s)
		}
	}
	if got := markup.Plain("a ❄️\x1b[1mb", 80); got != "a ❄b" {
		t.Errorf("sanitized plain = %q", got)
	}
}

func TestRenderGolden(t *testing.T) {
	if len(uitest.Required) != 4 {
		t.Fatal("uitest.Required empty — Phase 0 must fill it")
	}
	d := markup.Parse(sample, markup.ParseOptions{Hashtags: true})
	for _, sz := range uitest.Required {
		t.Run(fmt.Sprint(sz.W), func(t *testing.T) {
			lines := markup.Render(d, amber, markup.RenderOptions{Width: sz.W, MyHandle: "sam", Mentions: []api.MentionRef{minji}})
			checkLines(t, lines, sz.W)
			golden.RequireEqual(t, ansi.Strip(strings.Join(lines, "\n")))
		})
	}
}

// hasControl reports a control rune (C0, DEL, C1) or an invalid UTF-8 byte.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r >= 0x7f && r <= 0x9f || r == '�' {
			return true
		}
	}
	return false
}

func TestParseURLControlBytes(t *testing.T) {
	for _, body := range []string{
		"go https://a.com/\x07XXXX\x1b[8m",
		"https://a.com/x\x07\x1b]0;PWNED\x07",
		"https://a.com/x\x1b\\y \u009b31m https://b.com/\x9b31m",
		"[t](https://a.com/\x07x) [u](https://a.com/\u009bx) [v](https://a.com/\x9bx)",
		"https://a.com/\x7fz",
	} {
		for _, in := range nonText(markup.Parse(body, markup.ParseOptions{})) {
			if in.Kind == markup.Link && hasControl(in.URL) {
				t.Errorf("Parse(%q): link URL has control bytes: %q", body, in.URL)
			}
		}
	}
	// A bare URL that runs into a control rune is plain text, not a link to
	// its clean prefix (pending-fixes 0-b ①: "any one => not a link").
	if got := nonText(markup.Parse("go https://a.com/x\x07y", markup.ParseOptions{})); len(got) != 0 {
		t.Errorf("bare URL with BEL = %+v, want plain text", got)
	}
	// A [t](u) link whose URL holds a control rune is not a link, and neither
	// is the bare URL inside it.
	if got := nonText(markup.Parse("[t](https://a.com/\x1bx)", markup.ParseOptions{})); len(got) != 0 {
		t.Errorf("bracket link with ESC = %+v, want plain text", got)
	}
	// Visible whitespace still ends a bare URL; the next URL is a link again.
	got := nonText(markup.Parse("https://a.com/\x01x https://b.com/\nhttps://c.com/\r\nhttps://d.com/\tx", markup.ParseOptions{}))
	want := []markup.Inline{
		{Kind: markup.Link, Text: "https://b.com/", URL: "https://b.com/"},
		{Kind: markup.Link, Text: "https://c.com/", URL: "https://c.com/"},
		{Kind: markup.Link, Text: "https://d.com/", URL: "https://d.com/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bare URLs around whitespace = %+v, want %+v", got, want)
	}
}

// TestBareURLInvisibleSplit (pending-fixes 0-b ①): a bare URL is its own link
// text, so it must never link to a prefix cut at a rune Sanitize hides. Here
// the screen reads "https://secure-paypa1.com.paypal.com/login" while a
// prefix link would open https://secure-paypa1.com.
func TestBareURLInvisibleSplit(t *testing.T) {
	for _, sep := range []string{"\u0085", "\x01", "\x7f", "\x1b", "\x07", "\u009c", "\u2028", "\u2029", "\f", "\v", "\r", "\xff"} {
		u := "https://secure-paypa1.com" + sep + ".paypal.com/login"
		for _, body := range []string{u, "보기 " + u + " 끝", "(" + u + ")"} {
			d := markup.Parse(body, markup.ParseOptions{})
			if got := nonText(d); len(got) != 0 {
				t.Errorf("Parse(%q) = %+v, want plain text only", body, got)
			}
			lines := markup.Render(d, amber, markup.RenderOptions{Width: 80, Hyperlinks: true})
			for _, l := range lines {
				checkOnlyOSC8(t, l)
				if strings.Contains(l, "\x1b]8;") {
					t.Errorf("Render(%q) hyperlinks a prefix: %q", body, l)
				}
			}
			if got := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(got, "https://secure-paypa1.com") {
				t.Errorf("Render(%q) lost the text: %q", body, got)
			}
		}
	}
}

// checkOnlyOSC8 fails if out contains an OSC sequence other than OSC 8 or any
// escape sequence that is not SGR / OSC 8.
func checkOnlyOSC8(t *testing.T, out string) {
	t.Helper()
	var state byte
	for len(out) > 0 {
		seq, _, n, ns := ansi.DecodeSequence(out, state, nil)
		state = ns
		switch {
		case ansi.HasOscPrefix(seq):
			if !strings.HasPrefix(seq, "\x1b]8;") {
				t.Errorf("non-OSC-8 sequence %q", seq)
			}
		case ansi.HasCsiPrefix(seq):
			if !strings.HasSuffix(seq, "m") {
				t.Errorf("non-SGR CSI %q", seq)
			}
		case strings.HasPrefix(seq, "\x1b") || hasControl(seq):
			t.Errorf("stray control sequence %q", seq)
		}
		out = out[n:]
	}
}

func TestRenderHyperlinkControlBytes(t *testing.T) {
	bodies := []string{
		"go https://a.com/\x07" + strings.Repeat("X", 80) + "\x1b[8m",
		"https://a.com/x\x07\x1b]0;PWNED\x07",
		"https://a.com/x\x1b]52;c;cHduZWQ=\x07 [t](https://a.com/\u009b31m) https://b.com/\x9b31m",
		"[t](https://a.com/\x1b\\x) https://c.com/\x1b\\" + strings.Repeat("Y", 40),
	}
	for _, body := range bodies {
		d := markup.Parse(body, markup.ParseOptions{Hashtags: true})
		for _, w := range []int{80, 20, 5} {
			lines := markup.Render(d, amber, markup.RenderOptions{Width: w, Hyperlinks: true})
			checkLines(t, lines, w)
			for _, l := range lines {
				checkOnlyOSC8(t, l)
			}
		}
		compact := markup.Render(d, amber, markup.RenderOptions{Width: 20, Compact: true, Hyperlinks: true})
		checkLines(t, compact, 20)
		for _, l := range compact {
			checkOnlyOSC8(t, l)
		}
	}
	// Second guard: a hand-built Link with a control rune in URL renders without OSC 8.
	d := markup.Doc{Blocks: []markup.Block{{Kind: markup.Paragraph, Inline: []markup.Inline{
		{Kind: markup.Link, Text: "t", URL: "https://a.com/\x07\x1b]0;X\x07"},
	}}}}
	lines := markup.Render(d, amber, markup.RenderOptions{Width: 20, Hyperlinks: true})
	checkLines(t, lines, 20)
	for _, l := range lines {
		checkOnlyOSC8(t, l)
		if strings.Contains(l, "\x1b]8;") {
			t.Errorf("unsafe URL still hyperlinked: %q", l)
		}
	}
}

// Hangul such as 한 (ED 95 9C) and 서 (EC 84 9C) contains the byte 0x9C, which
// x/ansi reads as String Terminator: a raw non-ASCII OSC 8 target ends early,
// leaks the rest of the URL as text and breaks the width.
func TestRenderHyperlinkNonASCII(t *testing.T) {
	cases := []struct{ body, visible, target string }{
		{"보세요 https://ko.wikipedia.org/wiki/대한민국 끝", "보세요 https://ko.wikipedia.org/wiki/대한민국 끝",
			"https://ko.wikipedia.org/wiki/%EB%8C%80%ED%95%9C%EB%AF%BC%EA%B5%AD"},
		{"https://namu.wiki/w/서울", "https://namu.wiki/w/서울", "https://namu.wiki/w/%EC%84%9C%EC%9A%B8"},
		{"[한글 문서](https://a.com/서 울?q=한&x=%20) 끝", "[한글 문서](https://a.com/서 울?q=한&x=%20) 끝", ""},
		{"[한글 문서](https://a.com/서?q=한&x=%20#f) 끝", "한글 문서 끝", "https://a.com/%EC%84%9C?q=%ED%95%9C&x=%20#f"},
	}
	for _, c := range cases {
		d := markup.Parse(c.body, markup.ParseOptions{})
		lines := markup.Render(d, amber, markup.RenderOptions{Width: 60, Hyperlinks: true})
		if len(lines) != 1 {
			t.Fatalf("%q: lines = %q", c.body, lines)
		}
		l := lines[0]
		if got := ansi.Strip(l); got != c.visible {
			t.Errorf("%q: visible = %q, want %q", c.body, got, c.visible)
		}
		if w := ansi.StringWidth(l); w > 60 || w != ansi.StringWidth(c.visible) {
			t.Errorf("%q: width = %d, want %d", c.body, w, ansi.StringWidth(c.visible))
		}
		checkOnlyOSC8(t, l)
		if c.target != "" && !strings.Contains(l, ";"+c.target+"\x1b\\") && !strings.Contains(l, ";"+c.target+"\x07") {
			t.Errorf("%q: OSC 8 target %q not in %q", c.body, c.target, l)
		}
		for rest := l; ; {
			i := strings.Index(rest, "\x1b]8;;")
			if i < 0 {
				break
			}
			rest = rest[i+len("\x1b]8;;"):]
			end := strings.IndexAny(rest, "\x07\x1b")
			if end < 0 {
				t.Fatalf("%q: unterminated OSC 8 in %q", c.body, l)
			}
			for _, b := range []byte(rest[:end]) {
				if b >= 0x80 || b < 0x21 {
					t.Fatalf("%q: byte %#x inside OSC 8 target %q", c.body, b, rest[:end])
				}
			}
			rest = rest[end:]
		}
	}
}

func TestHyperlinkTarget(t *testing.T) {
	for in, want := range map[string]string{
		"https://a.com/x?q=1&r=a,b;c#f":  "https://a.com/x?q=1&r=a,b;c#f",
		"https://a.com/%20%e2":           "https://a.com/%20%e2",
		"https://a.com/100%":             "https://a.com/100%25",
		"https://a.com/%zz":              "https://a.com/%25zz",
		"https://namu.wiki/w/서울":         "https://namu.wiki/w/%EC%84%9C%EC%9A%B8",
		"https://a.com/a b\"<>\\^`{|}":   "https://a.com/a%20b%22%3C%3E%5C%5E%60%7B%7C%7D",
		"https://a.com/\x07\x1b\x9c\x7f": "https://a.com/%07%1B%9C%7F",
	} {
		if got := markup.HyperlinkTarget(in); got != want {
			t.Errorf("HyperlinkTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRenderLinkGraphemes: underlined link text must keep multi-rune graphemes
// (flags) in one SGR run, or each lone regional indicator measures 2 cells and
// the styled line overflows Width (padding then deletes text).
func TestRenderLinkGraphemes(t *testing.T) {
	bodies := []string{
		"[🇰🇷 국가대표 경기](https://example.com)",
		"[🇯🇵🇺🇸🇰🇷 q̃ x](https://example.com/a)",
		"https://example.com/🇰🇷",
		"보기 https://example.com/🇰🇷🇯🇵?q=1 끝",
	}
	for _, body := range bodies {
		d := markup.Parse(body, markup.ParseOptions{})
		for _, w := range []int{80, 24, 10, 7, 3} {
			for _, hl := range []bool{false, true} {
				lines := markup.Render(d, amber, markup.RenderOptions{Width: w, Hyperlinks: hl})
				checkLines(t, lines, w)
				for i, l := range lines {
					plain := ansi.Strip(l)
					if got := ansi.StringWidth(l); got != ansi.StringWidth(plain) {
						t.Errorf("%q w=%d hl=%v line %d: styled width %d != plain width %d", body, w, hl, i, got, ansi.StringWidth(plain))
					}
					if padded := ansi.Strip(textwidth.PadRight(l, w)); !strings.HasPrefix(padded, plain) {
						t.Errorf("%q w=%d hl=%v line %d: PadRight lost text: %q -> %q", body, w, hl, i, plain, padded)
					}
				}
			}
		}
	}
	// The link stays underlined, in one run around the whole flag text.
	lines := markup.Render(markup.Parse(bodies[0], markup.ParseOptions{}), amber, markup.RenderOptions{Width: 80})
	if len(lines) != 1 || !strings.Contains(lines[0], "\x1b[4m") || !strings.Contains(lines[0], "🇰🇷 국가대표 경기") {
		t.Errorf("link not underlined as one run: %q", lines)
	}
}

// TestRenderLinkURLEscapes (pending-fixes 0-b): a link URL carrying ESC, BEL,
// C1 or invalid UTF-8 must never reach the terminal as an escape; the text is
// shown as plain (sanitized) text instead of a link.
func TestRenderLinkURLEscapes(t *testing.T) {
	urls := []string{
		"https://a.b/\x1b]52;c;ZXZpbA==\x07",
		"https://a.b/\x9b31m",
		"https://a.b/\u009b31m",
		"https://a.b/x\x07y",
		"https://a.b/\xff\xfe",
		"https://a.b/\xc3\x28",
		"https://a.b/\xed\xa0\x80", // CESU-8 lone surrogate
		"https://a.b/\x1b\\",
	}
	for _, u := range urls {
		for _, body := range []string{u, "보기 " + u + " 끝", "[링크](" + u + ")"} {
			d := markup.Parse(body, markup.ParseOptions{})
			for _, in := range nonText(d) {
				if in.Kind == markup.Link && !cleanURL(in.URL) {
					t.Errorf("Parse(%q): link URL %q is not clean ASCII/UTF-8", body, in.URL)
				}
			}
			// Bare or bracketed, a hostile URL is plain text: no Link at all
			// (not even a link to its clean prefix).
			for _, in := range nonText(d) {
				if in.Kind == markup.Link {
					t.Errorf("Parse(%q): hostile URL still yields link %+v", body, in)
				}
			}
			for _, w := range []int{80, 12} {
				for _, o := range []markup.RenderOptions{{Width: w, Hyperlinks: true}, {Width: w, Hyperlinks: true, Compact: true}} {
					lines := markup.Render(d, amber, o)
					checkLines(t, lines, w)
					for _, l := range lines {
						checkOnlyOSC8(t, l)
						checkOSC8Targets(t, l)
						if strings.Contains(l, "\x1b]8;") {
							t.Errorf("Render(%q): hostile URL hyperlinked: %q", body, l)
						}
					}
					if got := ansi.Strip(strings.Join(lines, "")); !strings.Contains(got, "https://a.b/") && w == 80 {
						t.Errorf("Render(%q): URL text not shown: %q", body, got)
					}
				}
			}
		}
	}
	// "[링크](u)" with a hostile u shows its source as plain text.
	lines := markup.Render(markup.Parse("[링크](https://a.b/\x1b]52;c;ZXZpbA==\x07)", markup.ParseOptions{}), amber,
		markup.RenderOptions{Width: 80, Hyperlinks: true})
	if got := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(got, "[링크](") {
		t.Errorf("hostile bracket link not shown as text: %q", got)
	}
}

// TestRenderHyperlinkSanitizedOnly (pending-fixes 0-b ②): style() emits no
// OSC 8 for a URL holding a rune Sanitize would hide (control, invalid UTF-8,
// format character, line/paragraph separator) or ESC/BEL, whatever a caller
// put in the Doc.
func TestRenderHyperlinkSanitizedOnly(t *testing.T) {
	for _, u := range []string{
		"https://a.b/‮exe.txt", // bidi override: Sanitize drops it
		"https://a.b/​x",       // zero-width space
		"https://a.b/\xff",
		"https://a.b/\x1b]52;c;ZXZpbA==\x07",
		"https://a.b/\u009bx",
		"https://a.b/\tx",
		"https://a.b/\u2066x\u2069",              // bidi isolate
		"https://a.b/\u200ex",                    // LRM
		"https://a.b/\u2060x",                    // word joiner
		"https://a.b/\ufeffx",                    // BOM
		"https://a.b/\u00adx",                    // soft hyphen
		"https://a.b/pay\u200dpal",               // ZWJ between letters
		"https://a.b/\U0001F468\u200d\U0001F469", // ZWJ emoji: screen shows only the first
		"https://a.b/\U000E0041x",                // tag character
		"https://a.b/\u2028x",                    // line separator
		"https://a.b/\u2029x",                    // paragraph separator
	} {
		d := markup.Doc{Blocks: []markup.Block{{Kind: markup.Paragraph, Inline: []markup.Inline{
			{Kind: markup.Link, Text: "t", URL: u},
		}}}}
		lines := markup.Render(d, amber, markup.RenderOptions{Width: 20, Hyperlinks: true})
		checkLines(t, lines, 20)
		for _, l := range lines {
			checkOnlyOSC8(t, l)
			if strings.Contains(l, "\x1b]8;") {
				t.Errorf("URL %q hyperlinked although it holds a hidden rune: %q", u, l)
			}
		}
	}
	// A clean URL is still hyperlinked.
	d := markup.Parse("https://example.com/a?b=1", markup.ParseOptions{})
	lines := markup.Render(d, amber, markup.RenderOptions{Width: 80, Hyperlinks: true})
	if len(lines) != 1 || !strings.Contains(lines[0], "\x1b]8;;https://example.com/a?b=1") {
		t.Errorf("clean URL not hyperlinked: %q", lines)
	}
}

// TestRenderHyperlinkScripts (pending-fixes 0-b ②): the OSC 8 guard rejects
// only what can make the target differ from the text the viewer reads
// (controls, invalid UTF-8, format characters, line separators). Scripts and
// emoji whose display Sanitize adjusts (Indic clusters, NFD, VS16, skin tones)
// stay clickable with their exact percent-encoded target.
func TestRenderHyperlinkScripts(t *testing.T) {
	for _, u := range []string{
		"https://hi.wikipedia.org/wiki/हिन्दी",
		"https://hi.wikipedia.org/wiki/क्षत्रिय",
		"https://ta.wikipedia.org/wiki/தமிழ்",
		"https://bn.wikipedia.org/wiki/বাংলা_ভাষা",
		"https://example.com/cafe\u0301", // NFD café
		"https://emojipedia.org/❤️",
		"https://emojipedia.org/👍🏽",
		"https://ko.wikipedia.org/wiki/대한민국",
		"https://ja.wikipedia.org/wiki/日本語",
		"https://th.wikipedia.org/wiki/ภาษาไทย",
		"https://ar.wikipedia.org/wiki/عربي",
	} {
		want := "\x1b]8;;" + markup.HyperlinkTarget(u) + "\x07"
		for _, body := range []string{u, "보기 " + u + " 끝", "[문서](" + u + ")"} {
			d := markup.Parse(body, markup.ParseOptions{})
			lines := markup.Render(d, amber, markup.RenderOptions{Width: 120, Hyperlinks: true})
			checkLines(t, lines, 120)
			joined := strings.Join(lines, "\n")
			checkOnlyOSC8(t, joined)
			checkOSC8Targets(t, joined)
			if !strings.Contains(joined, want) {
				t.Errorf("Render(%q): no OSC 8 to %q in %q", body, markup.HyperlinkTarget(u), joined)
			}
		}
	}
}

// cleanURL: valid UTF-8 without C0, DEL or C1.
func cleanURL(u string) bool {
	return utf8.ValidString(u) && !hasControl(u)
}

// checkOSC8Targets fails if an OSC 8 target holds anything but printable ASCII.
func checkOSC8Targets(t *testing.T, l string) {
	t.Helper()
	for rest := l; ; {
		i := strings.Index(rest, "\x1b]8;")
		if i < 0 {
			return
		}
		rest = rest[i+len("\x1b]8;"):]
		end := strings.IndexAny(rest, "\x07\x1b")
		if end < 0 {
			t.Errorf("unterminated OSC 8 in %q", l)
			return
		}
		for j := 0; j < end; j++ {
			if c := rest[j]; c <= 0x20 || c >= 0x7f {
				t.Errorf("OSC 8 target byte %#x in %q", c, l)
			}
		}
		rest = rest[end:]
	}
}
