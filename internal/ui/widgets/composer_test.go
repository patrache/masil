package widgets_test

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"golang.org/x/text/unicode/norm"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/widgets"
)

// composerInputs are the CA 10.3 composer cases ("ᄀ"+"ᅡ" is the NFD jamo pair).
var composerInputs = []string{
	"❄️ 첫눈", "👍🏽", "👨‍👩‍👧 가족", "🇰", "क्षि", "가", "가",
	"탭\t그리고\r\n줄", "\x1b[31m빨강\x1b[0m",
}

var csiRe = regexp.MustCompile(`^\x1b\[[0-9;]*[A-Za-z]`)

// typeKeys splits s the way a terminal delivers typed text: one KeyPressMsg per
// grapheme cluster (an escape sequence arrives as one unit).
func typeKeys(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for s != "" {
		g := csiRe.FindString(s)
		if g == "" {
			g, _ = ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		}
		if g == "" {
			_, n := utf8.DecodeRuneInString(s)
			g = s[:n]
		}
		k := tea.KeyPressMsg{Code: tea.KeyExtended, Text: g}
		if r, n := utf8.DecodeRuneInString(g); n == len(g) && r >= 0x20 {
			k.Code = r
		}
		out = append(out, k)
		s = s[len(g):]
	}
	return out
}

func typeInto(ctx *ui.Context, c *widgets.Composer, s string) {
	for _, k := range typeKeys(s) {
		c.Update(ctx, k)
	}
}

// assertComposerView checks the view is Height lines of exactly width cells, every
// line width-stable, and returns the lines.
func assertComposerView(t *testing.T, ctx *ui.Context, c *widgets.Composer, width, maxRows int) []string {
	t.Helper()
	out := c.View(ctx, width, maxRows)
	lines := strings.Split(out, "\n")
	if want := c.Height(width, maxRows); len(lines) != want {
		t.Fatalf("view has %d lines, Height = %d", len(lines), want)
	}
	assertCells(t, lines, width)
	return lines
}

// cursorCol asserts the real cursor column matches the width of the text before
// the cursor on its (last, unwrapped) line. The cursor is at the end of the value.
func assertCursorAtEnd(t *testing.T, c *widgets.Composer, popup int) {
	t.Helper()
	cur := c.Cursor()
	if cur == nil {
		t.Fatal("focused composer has no cursor")
	}
	v := c.Value()
	lines := strings.Split(v, "\n")
	last := lines[len(lines)-1]
	if got, want := cur.X-1, ansi.StringWidth(last); got != want {
		t.Errorf("cursor X-1 = %d, want StringWidth(%q) = %d", got, last, want)
	}
	if got, want := cur.Y, popup+len(lines); got != want {
		t.Errorf("cursor Y = %d, want %d", got, want)
	}
}

func TestComposerNormalizesTypeAndPaste(t *testing.T) {
	ctx := newCtx(nil, false)
	for _, in := range composerInputs {
		for _, mode := range []string{"type", "paste"} {
			t.Run(fmt.Sprintf("%s/%q", mode, in), func(t *testing.T) {
				c := widgets.NewComposer("메시지")
				c.Focus()
				if mode == "type" {
					typeInto(ctx, c, in)
				} else {
					c.Update(ctx, tea.PasteMsg{Content: in})
				}
				want := textwidth.SanitizeInput(in)
				if got := c.Value(); got != want {
					t.Fatalf("Value() = %q, want %q", got, want)
				}
				lines := assertComposerView(t, ctx, c, 60, 5)
				for i, l := range lines {
					if ansi.StringWidth(l) != ansi.StringWidthWc(l) {
						t.Errorf("line %d not width-stable: %q", i, ansi.Strip(l))
					}
				}
				assertCursorAtEnd(t, c, 0)
			})
		}
	}
}

func TestComposerRenormalizesAcrossKeys(t *testing.T) {
	ctx := newCtx(nil, false)
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"👨", "‍", "👩"}, textwidth.SanitizeInput("👨‍👩")},
		{[]string{"👨", "‍", "👩", "‍", "👧", " ", "가"}, textwidth.SanitizeInput("👨‍👩‍👧 가")},
		{[]string{"👍", "🏽", "!"}, textwidth.SanitizeInput("👍🏽!")},
		{[]string{"❄", "️", " "}, textwidth.SanitizeInput("❄️ ")},
		{[]string{"ᄀ", "ᅡ"}, textwidth.SanitizeInput("가")},
		{[]string{"a", "́", "b"}, textwidth.SanitizeInput("áb")},
	}
	if cases[0].want != "👨" {
		t.Fatalf("SanitizeInput(👨‍👩) = %q, want 👨", cases[0].want)
	}
	for _, tc := range cases {
		c := widgets.NewComposer("")
		c.Focus()
		for _, k := range tc.keys {
			c.Update(ctx, tea.KeyPressMsg{Code: tea.KeyExtended, Text: k})
		}
		if got := c.Value(); got != tc.want {
			t.Errorf("keys %q: Value() = %q, want %q", tc.keys, got, tc.want)
		}
		assertComposerView(t, ctx, c, 30, 5)
		assertCursorAtEnd(t, c, 0)
	}
}

func TestComposerHugePaste(t *testing.T) {
	ctx := newCtx(nil, false)
	c := widgets.NewComposer("메시지")
	c.Focus()
	in := strings.Repeat("가나다 abc\r\n\x07\xff", 1000)
	start := time.Now()
	c.Update(ctx, tea.PasteMsg{Content: in})
	v := c.Value()
	lines := assertComposerView(t, ctx, c, 40, 5)
	if took := time.Since(start); took > time.Second {
		t.Errorf("huge paste took %v", took)
	}
	if !utf8.ValidString(v) || strings.ContainsAny(v, "\r\x07�") {
		t.Errorf("value not normalized: %q…", v[:min(len(v), 80)])
	}
	if n := utf8.RuneCountInString(v); n > 4000 || n < 3900 {
		t.Errorf("rune count = %d, want close to and at most 4000", n)
	}
	if v != textwidth.SanitizeInput(v) {
		t.Error("value is not a SanitizeInput fixpoint")
	}
	if len(lines) != 7 {
		t.Errorf("huge paste view = %d lines, want 7 (5 rows + border)", len(lines))
	}
	// more input at the limit is ignored, deleting makes room again
	c.Update(ctx, keyMsg("x"))
	if c.Value() != v {
		t.Error("typing past the limit changed the value")
	}
	c.Update(ctx, keyMsg("backspace"))
	c.Update(ctx, keyMsg("x"))
	if got := c.Value(); !strings.HasSuffix(got, "x") || utf8.RuneCountInString(got) > 4000 {
		t.Errorf("after backspace+x: suffix %q, runes %d", got[max(0, len(got)-8):], utf8.RuneCountInString(got))
	}
}

// Edits tells the keys the composer acts on from the ones it ignores (ctrl+o goes
// back in history even while typing; alt+left stays a word move).
func TestComposerEdits(t *testing.T) {
	c := widgets.NewComposer("")
	for _, k := range []string{"a", "한", "ctrl+c", "enter", "tab", "esc", "alt+left", "alt+b", "alt+right", "alt+f", "left", "backspace", "ctrl+a", "shift+enter"} {
		if !c.Edits(keyMsg(k)) {
			t.Errorf("Edits(%s) = false", k)
		}
	}
	for _, k := range []string{"ctrl+o", "ctrl+k", "ctrl+f", "ctrl+n", "ctrl+w", "ctrl+v"} {
		if c.Edits(keyMsg(k)) {
			t.Errorf("Edits(%s) = true", k)
		}
	}
}

func TestComposerHardGlobalsNotConsumed(t *testing.T) {
	ctx := newCtx(nil, false)
	c := widgets.NewComposer("")
	c.Focus()
	typeInto(ctx, c, "안녕 abc")
	c.Update(ctx, keyMsg("left"))
	c.Update(ctx, keyMsg("left"))
	c.View(ctx, 40, 5)
	v, cur := c.Value(), *c.Cursor()
	for _, k := range []string{"ctrl+k", "ctrl+f", "ctrl+n", "ctrl+w", "ctrl+v", "enter", "tab"} {
		c.Update(ctx, keyMsg(k))
		c.View(ctx, 40, 5)
		if c.Value() != v || *c.Cursor() != cur {
			t.Errorf("%s changed the composer: %q %+v -> %q %+v", k, v, cur, c.Value(), *c.Cursor())
		}
	}
	for _, k := range []string{"shift+enter", "alt+enter", "ctrl+j"} {
		before := c.Value()
		c.Update(ctx, keyMsg(k))
		if got := c.Value(); strings.Count(got, "\n") != strings.Count(before, "\n")+1 {
			t.Errorf("%s did not insert a newline: %q", k, got)
		}
	}
	if got := c.Value(); got != "안녕 a\n\n\nbc" {
		t.Errorf("value = %q", got)
	}
	// grapheme-aware editing: backspace removes a whole cluster
	d := widgets.NewComposer("")
	d.Focus()
	d.Update(ctx, tea.PasteMsg{Content: "aक्षि"})
	d.Update(ctx, keyMsg("backspace"))
	if got := d.Value(); got != "a" {
		t.Errorf("backspace over क्षि left %q", got)
	}
	// blurred composer ignores input
	d.Blur()
	d.Update(ctx, keyMsg("z"))
	if d.Value() != "a" || d.Focused() || d.Cursor() != nil {
		t.Errorf("blurred composer: value %q focused %v cursor %v", d.Value(), d.Focused(), d.Cursor())
	}
}

func TestComposerCtrlCClears(t *testing.T) {
	ctx := newCtx(nil, false)
	c := widgets.NewComposer("")
	c.Focus()
	typeInto(ctx, c, "abc")
	c.Update(ctx, keyMsg("left"))
	c.SetCompletion([]widgets.Completion{{Label: "@abc", Insert: "@abc "}})
	// embedding components report handled = Value() != "" taken BEFORE Update
	if handled := c.Value() != ""; !handled {
		t.Fatal("first ctrl+c must be handled")
	}
	if cmd := c.Update(ctx, keyMsg("ctrl+c")); cmd != nil {
		t.Error("ctrl+c returned a cmd")
	}
	if c.Value() != "" {
		t.Fatalf("ctrl+c left %q", c.Value())
	}
	if h := c.Height(40, 5); h != 3 {
		t.Errorf("ctrl+c kept the completion popup: Height = %d", h)
	}
	if handled := c.Value() != ""; handled {
		t.Fatal("second ctrl+c must not be handled")
	}
	if cmd := c.Update(ctx, keyMsg("ctrl+c")); cmd != nil || c.Value() != "" {
		t.Errorf("ctrl+c on empty: cmd %v value %q", cmd, c.Value())
	}
}

func TestComposerAutoGrowAndCompletion(t *testing.T) {
	ctx := newCtx(nil, false)
	c := widgets.NewComposer("메시지")
	c.Focus()
	if h := c.Height(40, 5); h != 3 {
		t.Errorf("empty Height = %d, want 3", h)
	}
	lines := assertComposerView(t, ctx, c, 40, 5)
	if !strings.Contains(ansi.Strip(lines[1]), "메시지") {
		t.Errorf("placeholder not shown: %q", ansi.Strip(lines[1]))
	}
	if !strings.Contains(lines[0], sgr(38, th.P.Acc)) {
		t.Error("focused composer border not acc colored")
	}
	c.Blur()
	if !strings.Contains(c.View(ctx, 40, 5), sgr(38, th.P.Line)) {
		t.Error("blurred composer border not line colored")
	}
	c.Focus()

	c.SetValue("1\n2\n3")
	if h := c.Height(40, 5); h != 5 {
		t.Errorf("3 lines Height = %d, want 5", h)
	}
	c.SetValue("1\n2\n3\n4\n5\n6\n7")
	if h := c.Height(40, 5); h != 7 {
		t.Errorf("7 lines Height = %d, want 7", h)
	}
	lines = assertComposerView(t, ctx, c, 40, 5)
	if got := ansi.Strip(lines[5]); !strings.HasPrefix(got, "║7") {
		t.Errorf("scrolled view last row = %q, want the cursor row 7", got)
	}
	if cur := c.Cursor(); cur.X != 2 {
		t.Errorf("cursor X = %d, want 2", cur.X)
	}
	if cur := c.Cursor(); cur.Y != 5 {
		t.Errorf("cursor Y = %d, want 5 (last visible row)", cur.Y)
	}
	// soft wrap: a long line grows the box by visual rows
	c.SetValue(strings.Repeat("가", 30)) // 60 cells in 38 => 2 rows
	if h := c.Height(40, 5); h != 4 {
		t.Errorf("wrapped Height = %d, want 4", h)
	}
	lines = assertComposerView(t, ctx, c, 40, 5)
	if cur := c.Cursor(); cur.X != 1+ansi.StringWidth(strings.Repeat("가", 11)) || cur.Y != 2 {
		t.Errorf("wrapped cursor = %+v", cur.Position)
	}
	_ = lines

	// completion popup above the box
	c.SetValue("@mi")
	base := c.Height(40, 5)
	c.SetCompletion([]widgets.Completion{
		{Label: "@minji", Insert: "@minji ", Detail: "민지"},
		{Label: "@mina", Insert: "@mina ", Detail: "미나"},
		{Label: "@mirae", Insert: "@mirae ", Detail: "미래"},
	})
	if h := c.Height(40, 5); h != base+3 {
		t.Errorf("Height with 3 completions = %d, want %d", h, base+3)
	}
	lines = assertComposerView(t, ctx, c, 40, 5)
	if got := ansi.Strip(lines[0]); !strings.Contains(got, "@minji") || !strings.Contains(got, "민지") {
		t.Errorf("popup row 0 = %q", got)
	}
	if !strings.Contains(lines[0], sgr(48, th.P.SelBG)) {
		t.Error("selected popup row not Selected styled")
	}
	if got := ansi.Strip(lines[3]); !strings.HasPrefix(got, "╔") {
		t.Errorf("box does not start below the popup: %q", got)
	}
	if cur := c.Cursor(); cur.Y != 4 || cur.X != 1+3 {
		t.Errorf("cursor with popup = %+v, want (4,4)", cur.Position)
	}
	// popup keys: down moves the selection, enter inserts and closes
	c.Update(ctx, keyMsg("down"))
	lines = assertComposerView(t, ctx, c, 40, 5)
	if !strings.Contains(lines[1], sgr(48, th.P.SelBG)) {
		t.Error("down did not move the popup selection")
	}
	c.Update(ctx, keyMsg("enter"))
	if got := c.Value(); got != "@mina " {
		t.Errorf("accept = %q, want %q", got, "@mina ")
	}
	if h := c.Height(40, 5); h != 3 {
		t.Errorf("popup still open after accept: Height %d", h)
	}
	// more than 5 completions show 5 rows; a "› " label marks the selected row
	many := make([]widgets.Completion, 8)
	for i := range many {
		many[i] = widgets.Completion{Label: fmt.Sprintf("  :e%d", i), Insert: fmt.Sprintf(":e%d:", i)}
	}
	many[2].Label = "› :e2"
	c.SetCompletion(many)
	if h := c.Height(40, 5); h != 3+5 {
		t.Errorf("Height with 8 completions = %d, want 8", h)
	}
	lines = assertComposerView(t, ctx, c, 40, 5)
	if !strings.Contains(lines[2], sgr(48, th.P.SelBG)) || strings.Contains(lines[0], sgr(48, th.P.SelBG)) {
		t.Error("› marked row is not the highlighted row")
	}
	c.Update(ctx, keyMsg("tab"))
	if got := c.Value(); got != "@mina :e2:" {
		t.Errorf("tab accept = %q", got)
	}
	c.SetCompletion(many[:1])
	c.Update(ctx, keyMsg("esc"))
	if h := c.Height(40, 5); h != 3 {
		t.Errorf("esc did not close the popup: Height %d", h)
	}
	// tiny widths never panic and stay exact
	for w := 1; w <= 4; w++ {
		assertComposerView(t, ctx, c, w, 5)
	}
	if out := c.View(ctx, 0, 5); out != "" {
		t.Errorf("width 0 view = %q", out)
	}
}

// composerScreen draws a full-screen Frame with the composer pinned to the bottom of
// its inner area and a status line, returning the screen and the integrity boxes.
func composerScreen(ctx *ui.Context, c *widgets.Composer, w, h int) (string, []uitest.Box) {
	frameH := h - 1
	inner := ui.Size{W: w - 2, H: frameH - 2}
	ch := min(c.Height(inner.W, 5), inner.H)
	comp := strings.Split(c.View(ctx, inner.W, 5), "\n")
	comp = comp[len(comp)-ch:]
	var lines []string
	lines = append(lines, th.S.Title.Render("#개발")+th.S.Dim.Render(" · 배포 몇 시?"))
	for len(lines) < inner.H-ch {
		lines = append(lines, "")
	}
	lines = append(lines, comp...)
	frame := widgets.Frame(th, "#개발", ui.Badge{}, true, ui.Size{W: w, H: frameH}, strings.Join(lines, "\n"))
	status := th.S.StatusBar.Render(textwidth.PadRight(" 입력", w))
	boxH := c.Height(inner.W, 5) - popupRows(c, inner.W)
	boxes := []uitest.Box{
		{Rect: layout.Rect{X: 0, Y: 0, W: w, H: frameH}, Bordered: true},
		{Rect: layout.Rect{X: 1, Y: 1 + inner.H - boxH, W: inner.W, H: boxH}, Bordered: true, Z: 1},
	}
	return frame + "\n" + status, boxes
}

// popupRows is the completion popup height (Height minus the bordered box).
func popupRows(c *widgets.Composer, w int) int {
	v := strings.Split(c.View(newCtx(nil, false), w, 5), "\n")
	for i, l := range v {
		if strings.HasPrefix(ansi.Strip(l), "╔") {
			return i
		}
	}
	return 0
}

func TestComposerGolden(t *testing.T) {
	if len(uitest.Required) != 4 {
		t.Fatal("uitest.Required must list the 4 required sizes")
	}
	ctx := newCtx(nil, false)
	states := []string{"empty", "one_line", "multi_line", "completion"}
	for _, sz := range uitest.Required {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			for _, st := range states {
				t.Run(st, func(t *testing.T) {
					c := widgets.NewComposer("메시지를 입력하세요")
					c.Focus()
					switch st {
					case "one_line":
						typeInto(ctx, c, "❄️ 첫눈 왔어요 👍🏽")
					case "multi_line":
						c.Update(ctx, tea.PasteMsg{Content: "배포는\r\n오후 2시\n\t체크리스트:\n- 마이그레이션\n- 캐시 비우기\n- 공지\n끝 👨‍👩‍👧"})
					case "completion":
						typeInto(ctx, c, "@mi")
						c.SetCompletion([]widgets.Completion{
							{Label: "@minji", Insert: "@minji ", Detail: "민지"},
							{Label: "@mina", Insert: "@mina ", Detail: "미나"},
							{Label: "@mirae", Insert: "@mirae ", Detail: "미래 ❄️"},
						})
					}
					out, boxes := composerScreen(ctx, c, sz.W, sz.H)
					uitest.AssertIntegrity(t, out, sz.W, sz.H, boxes, th.Border)
					golden.RequireEqual(t, ansi.Strip(out))
				})
			}
		})
	}
}

func TestComposerRandomEditsStayNormalized(t *testing.T) {
	ctx := newCtx(nil, false)
	rng := rand.New(rand.NewPCG(7, 22))
	keys := []string{"left", "right", "up", "down", "backspace", "delete", "home", "end",
		"ctrl+u", "alt+backspace", "alt+d", "ctrl+t", "alt+u", "shift+enter", "ctrl+k", "enter", "space"}
	texts := append(slices.Clone(composerInputs), "a", "가", "‍", "️", "🏽", "́", "ᅡ", "\r", "\x00", "\xff", "🇰🇷")
	for _, w := range []int{3, 7, 24} {
		c := widgets.NewComposer("메시지")
		c.Focus()
		for i := range 600 {
			switch r := rng.IntN(10); {
			case r < 4:
				c.Update(ctx, tea.KeyPressMsg{Code: tea.KeyExtended, Text: texts[rng.IntN(len(texts))]})
			case r < 5:
				c.Update(ctx, tea.PasteMsg{Content: texts[rng.IntN(len(texts))] + texts[rng.IntN(len(texts))]})
			default:
				c.Update(ctx, keyMsg(keys[rng.IntN(len(keys))]))
			}
			v := c.Value()
			if v != textwidth.SanitizeInput(v) || utf8.RuneCountInString(v) > 4000 {
				t.Fatalf("w=%d step %d: value not normalized: %q", w, i, v)
			}
			lines := strings.Split(c.View(ctx, w, 4), "\n")
			if len(lines) != c.Height(w, 4) {
				t.Fatalf("w=%d step %d: %d lines, Height %d", w, i, len(lines), c.Height(w, 4))
			}
			assertCells(t, lines, w)
			if cur := c.Cursor(); cur == nil || cur.X < 1 || cur.X > w-1 || cur.Y < 1 || cur.Y > len(lines)-2 {
				t.Fatalf("w=%d step %d: cursor %+v outside the box (%d lines) for %q", w, i, cur, len(lines), v)
			}
		}
	}
}

// NFD Hangul (macOS file names, drag and drop) is composed, not collapsed.
func TestComposerNFDPaste(t *testing.T) {
	ctx := newCtx(nil, false)
	nfd := norm.NFD.String("회의록.pdf 한글 파일")
	for _, name := range []string{"paste", "keys"} {
		c := widgets.NewComposer("")
		c.Focus()
		if name == "paste" {
			c.Update(ctx, tea.PasteMsg{Content: nfd})
		} else {
			typeInto(ctx, c, nfd)
		}
		if got, want := c.Value(), norm.NFC.String(nfd); got != want {
			t.Errorf("%s: Value() = %q, want %q", name, got, want)
		}
		assertComposerView(t, ctx, c, 30, 5)
		assertCursorAtEnd(t, c, 0)
	}
}
