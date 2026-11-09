package htmldoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/markup"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// allText joins every visible string of the blocks (inline text, pre text,
// image alt and URL).
func allText(bs []Block) string {
	var sb strings.Builder
	for _, b := range bs {
		for _, in := range b.Inline {
			sb.WriteString(in.Text)
		}
		sb.WriteString(b.Text)
		sb.WriteString(b.Alt)
		sb.WriteString(b.URL)
		sb.WriteString("\n")
	}
	return sb.String()
}

func inlineText(b Block) string {
	var sb strings.Builder
	for _, in := range b.Inline {
		sb.WriteString(in.Text)
	}
	return sb.String()
}

func count(bs []Block, k Kind) int {
	n := 0
	for _, b := range bs {
		if b.Kind == k {
			n++
		}
	}
	return n
}

func TestConvert1606(t *testing.T) {
	bs := Convert(load(t, "1606.html"))
	if len(bs) == 0 || bs[0].Kind != Para {
		t.Fatalf("first block = %+v, want a Para", bs)
	}
	found := false
	for _, in := range bs[0].Inline {
		if markup.InlineKind(in.Kind) == markup.InlineCode && in.Text == "k" {
			found = true
		}
	}
	if !found {
		t.Errorf("first Para lacks InlineCode k: %+v", bs[0].Inline)
	}
	if n := count(bs, Pre); n != 3 {
		t.Errorf("Pre blocks = %d, want 3", n)
	}
	var bullets []string
	for _, b := range bs {
		if b.Kind == Bullet {
			bullets = append(bullets, inlineText(b))
		}
	}
	want := []string{
		"1 <= k <= 10⁵",
		"1 <= arrival.length, load.length <= 10⁵",
		"arrival.length == load.length",
		"1 <= arrival[i], load[i] <= 10⁹",
		"arrival is strictly increasing.",
	}
	if strings.Join(bullets, "\n") != strings.Join(want, "\n") {
		t.Errorf("bullets =\n%q\nwant\n%q", bullets, want)
	}
	// The first example keeps its lines, without the tags inside <pre>.
	for _, b := range bs {
		if b.Kind != Pre {
			continue
		}
		lines := strings.Split(b.Text, "\n")
		if len(lines) != 4 || !strings.HasPrefix(lines[0], "Input: k = 3") || lines[3] != "fixture text, line two." {
			t.Errorf("first Pre = %q", b.Text)
		}
		break
	}
	if n := count(bs, Image); n != 1 {
		t.Errorf("Image blocks = %d, want 1", n)
	}
	// The &nbsp; spacer paragraphs leave no empty block.
	for i, b := range bs {
		if b.Kind == Para && strings.TrimSpace(inlineText(b)) == "" {
			t.Errorf("block %d is an empty Para", i)
		}
	}
	// Bold label inside a paragraph.
	if !hasInline(bs, markup.Bold, "Constraints:") {
		t.Error("no Bold Constraints: span")
	}
}

func hasInline(bs []Block, k markup.InlineKind, text string) bool {
	for _, b := range bs {
		for _, in := range b.Inline {
			if markup.InlineKind(in.Kind) == k && in.Text == text {
				return true
			}
		}
	}
	return false
}

func TestSuperscript(t *testing.T) {
	cases := map[string]string{
		"<p>10<sup>5</sup></p>":     "10⁵",
		"<p>2<sup>x</sup></p>":      "2^x",
		"<p>x<sup>-1</sup></p>":     "x⁻¹",
		"<p>2<sup>n+1</sup></p>":    "2^n+1",
		"<p>H<sub>2</sub>O</p>":     "H_2O",
		"<p>10<sup>9</sup> + 7</p>": "10⁹ + 7",
	}
	for in, want := range cases {
		bs := Convert(in)
		if len(bs) != 1 || inlineText(bs[0]) != want {
			t.Errorf("Convert(%q) = %+v, want %q", in, bs, want)
		}
	}
}

func TestHostileStripped(t *testing.T) {
	bs := Convert(load(t, "hostile.html"))
	got := allText(bs)
	for _, bad := range []string{"\x1b", "\u202e", "\x07", "\x1b[31m"} {
		if strings.Contains(got, bad) {
			t.Errorf("output contains %q:\n%q", bad, got)
		}
	}
	for _, b := range bs {
		if b.Kind == Image && b.URL != "" {
			t.Errorf("non-assets image kept URL %q", b.URL)
		}
	}
	if strings.Contains(got, "var x") {
		t.Errorf("script text shown: %q", got)
	}
	if strings.Contains(got, "hidden") {
		t.Errorf("comment text shown: %q", got)
	}
	if !strings.Contains(got, "attr with gt") || !strings.Contains(got, "unterminated bold") {
		t.Errorf("text lost: %q", got)
	}
	// Pre keeps its two lines, each sanitized.
	for _, b := range bs {
		if b.Kind == Pre && strings.Count(b.Text, "\n") != 1 {
			t.Errorf("Pre = %q, want 2 lines", b.Text)
		}
	}
}

func TestImageHostAllowlist(t *testing.T) {
	bs := Convert(`<img alt="a" src="https://assets.leetcode.com/uploads/x.png"><img src="http://evil.example/a.png" alt="b"><img src="https://assets.leetcode.com.evil.example/x.png">`)
	if len(bs) != 3 {
		t.Fatalf("blocks = %+v", bs)
	}
	if bs[0].Kind != Image || bs[0].URL != "https://assets.leetcode.com/uploads/x.png" || bs[0].Alt != "a" {
		t.Errorf("assets image = %+v", bs[0])
	}
	if bs[1].Kind != Image || bs[1].URL != "" || bs[1].Alt != "b" {
		t.Errorf("evil image = %+v", bs[1])
	}
	if bs[2].URL != "" {
		t.Errorf("look-alike host kept URL %q", bs[2].URL)
	}
}

func TestDeepNestingNoPanic(t *testing.T) {
	src := strings.Repeat("<ul><li>", 10000) + "deep" + strings.Repeat("</li></ul>", 10000)
	bs := Convert(src)
	for _, b := range bs {
		if b.Depth > 3 {
			t.Fatalf("Depth = %d > 3", b.Depth)
		}
	}
	if !strings.Contains(allText(bs), "deep") {
		t.Errorf("text lost: %+v", bs)
	}
	// Unclosed tags too.
	_ = Convert(strings.Repeat("<ul><li>", 10000) + "x")
	_ = Convert(strings.Repeat("<b><i><code><sup>", 5000) + "x")
}

func TestHugeInputTruncated(t *testing.T) {
	src := "<p>" + strings.Repeat("a", 2<<20) + "</p>"
	bs := Convert(src)
	if n := len(allText(bs)); n > MaxInput {
		t.Errorf("output %d bytes > %d", n, MaxInput)
	}
}

func TestNestedListsTablesTransparent(t *testing.T) {
	bs := Convert(load(t, "nested.html"))
	type row struct {
		k     Kind
		depth int
		n     int
		text  string
	}
	var got []row
	for _, b := range bs {
		got = append(got, row{b.Kind, b.Depth, b.N, inlineText(b)})
	}
	want := []row{
		{Para, 0, 0, "Lists, tables and scripts: H_2O and x⁻¹ and 2^n+1."},
		{Numbered, 0, 1, "first"},
		{Bullet, 1, 0, "inner a"},
		{Bullet, 1, 0, "inner b"},
		{Para, 1, 0, "continued"},
		{Numbered, 0, 2, "second"},
		{Numbered, 0, 5, "fifth"},
		{Para, 0, 0, "name | value"},
		{Para, 0, 0, "a | 1"},
		{Para, 0, 0, "transparent text"},
		{Para, 0, 0, "bold italic em c"},
	}
	if len(got) != len(want) {
		t.Fatalf("blocks =\n%+v\nwant\n%+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	last := bs[len(bs)-1].Inline
	kinds := []markup.InlineKind{}
	for _, in := range last {
		if strings.TrimSpace(in.Text) != "" {
			kinds = append(kinds, markup.InlineKind(in.Kind))
		}
	}
	wantKinds := []markup.InlineKind{markup.Bold, markup.Italic, markup.Italic, markup.InlineCode}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("inline kinds = %v (%+v)", kinds, last)
	}
	for i := range kinds {
		if kinds[i] != wantKinds[i] {
			t.Errorf("inline %d kind = %v, want %v", i, kinds[i], wantKinds[i])
		}
	}
}

func TestEntitiesAndWhitespace(t *testing.T) {
	bs := Convert("<p>  a &amp;&lt;b&gt;\n\t c&nbsp;d  </p>")
	if len(bs) != 1 || inlineText(bs[0]) != "a &<b> c d" {
		t.Errorf("got %+v", bs)
	}
	if bs := Convert("plain text, no tags"); len(bs) != 1 || bs[0].Kind != Para || inlineText(bs[0]) != "plain text, no tags" {
		t.Errorf("bare text = %+v", bs)
	}
	if bs := Convert("a < b and c<1"); len(bs) != 1 || inlineText(bs[0]) != "a < b and c<1" {
		t.Errorf("literal < = %+v", bs)
	}
	if bs := Convert(""); len(bs) != 0 {
		t.Errorf("empty = %+v", bs)
	}
}

// Inline kinds share markup's values so the UI converts with a plain cast
// (htmldoc cannot import markup: markup imports internal/api).
func TestInlineKindsMatchMarkup(t *testing.T) {
	pairs := []struct {
		got  InlineKind
		want markup.InlineKind
	}{{Text, markup.Text}, {Bold, markup.Bold}, {Italic, markup.Italic}, {InlineCode, markup.InlineCode}}
	for _, p := range pairs {
		if int(p.got) != int(p.want) {
			t.Errorf("kind %d != markup %d", p.got, p.want)
		}
	}
}
