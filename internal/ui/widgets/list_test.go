package widgets_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// item is a test list row: its text is word-wrapped to the render width, plus extra
// fixed lines (so heights vary).
type item struct {
	id    string
	text  string
	extra int
}

func renderItem(ctx *ui.Context, it item, selected bool, width int) []string {
	lines := textwidth.Wrap(it.id+" "+it.text, width)
	for i := range it.extra {
		lines = append(lines, fmt.Sprintf("  %s.%d", it.id, i+1))
	}
	if selected {
		for i, l := range lines {
			lines[i] = ctx.Theme.S.Selected.Render(l)
		}
	}
	return lines
}

func newTestList() *widgets.List[item] {
	return widgets.NewList(func(it item) string { return it.id }, renderItem)
}

func items(n int, extra func(i int) int, text string) []item {
	out := make([]item, n)
	for i := range out {
		e := 0
		if extra != nil {
			e = extra(i)
		}
		out[i] = item{id: fmt.Sprintf("m%d", i), text: text, extra: e}
	}
	return out
}

func selectedID[T any](t *testing.T, l *widgets.List[T], idOf func(T) string) string {
	t.Helper()
	it, ok := l.Selected()
	if !ok {
		return ""
	}
	return idOf(it)
}

func selID(t *testing.T, l *widgets.List[item]) string {
	t.Helper()
	return selectedID(t, l, func(it item) string { return it.id })
}

// keyMsg builds the KeyPressMsg a terminal delivers for the few names these tests use.
func keyMsg(s string) tea.KeyPressMsg {
	named := map[string]rune{
		"up": tea.KeyUp, "down": tea.KeyDown, "home": tea.KeyHome, "end": tea.KeyEnd,
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
		"left": tea.KeyLeft, "right": tea.KeyRight, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"delete": tea.KeyDelete,
	}
	var mod tea.KeyMod
	name := s
	for {
		switch {
		case strings.HasPrefix(name, "ctrl+"):
			mod |= tea.ModCtrl
			name = name[5:]
			continue
		case strings.HasPrefix(name, "alt+"):
			mod |= tea.ModAlt
			name = name[4:]
			continue
		case strings.HasPrefix(name, "shift+"):
			mod |= tea.ModShift
			name = name[6:]
			continue
		}
		break
	}
	if c, ok := named[name]; ok {
		return tea.KeyPressMsg{Code: c, Mod: mod}
	}
	r := []rune(name)[0]
	k := tea.KeyPressMsg{Code: r, Mod: mod}
	if mod == 0 {
		k.Text = name
	}
	return k
}

func viewLines(ctx *ui.Context, l *widgets.List[item], w, h int) []string {
	return strings.Split(l.View(ctx, ui.Size{W: w, H: h}), "\n")
}

// firstID returns the item id at the start of a stripped view line ("m7 ..." / "  m7.1").
func firstID(line string) string {
	f := strings.Fields(ansi.Strip(line))
	if len(f) == 0 {
		return ""
	}
	id, _, _ := strings.Cut(f[0], ".")
	return id
}

func TestListSelectionByID(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	if _, ok := l.Selected(); ok {
		t.Fatal("empty list has a selection")
	}
	l.SetItems(items(5, nil, ""))
	if got := selID(t, l); got != "m0" {
		t.Fatalf("default selection = %q, want m0", got)
	}
	l.Select("m2")
	if got := selID(t, l); got != "m2" {
		t.Fatalf("Select(m2) => %q", got)
	}
	// reorder: selection follows the ID
	l.SetItems([]item{{id: "m4"}, {id: "m2"}, {id: "m0"}, {id: "m1"}, {id: "m3"}})
	if got := selID(t, l); got != "m2" {
		t.Fatalf("after reorder = %q, want m2", got)
	}
	// selected removed => the item now at the same index (index 1)
	l.SetItems([]item{{id: "m4"}, {id: "m0"}, {id: "m1"}, {id: "m3"}})
	if got := selID(t, l); got != "m0" {
		t.Fatalf("after removal = %q, want m0 (same index)", got)
	}
	// last selected removed => new last
	l.Select("m3")
	l.SetItems([]item{{id: "m4"}, {id: "m0"}})
	if got := selID(t, l); got != "m0" {
		t.Fatalf("after removing last = %q, want m0", got)
	}
	// unknown id keeps the selection
	l.Select("nope")
	if got := selID(t, l); got != "m0" {
		t.Fatalf("Select(unknown) changed selection to %q", got)
	}
	l.SetItems(nil)
	if _, ok := l.Selected(); ok {
		t.Fatal("empty list after SetItems(nil) has a selection")
	}
	assertCells(t, viewLines(ctx, l, 10, 3), 10)
}

func TestListKeys(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	l.SetItems(items(20, nil, ""))
	l.View(ctx, ui.Size{W: 20, H: 10})
	press := func(k string) bool {
		t.Helper()
		handled, _ := l.HandleKey(ctx, keyMsg(k))
		return handled
	}
	steps := []struct {
		key  string
		want string
	}{
		{"j", "m1"}, {"down", "m2"}, {"k", "m1"}, {"up", "m0"}, {"up", "m0"},
		{"G", "m19"}, {"home", "m0"}, {"end", "m19"}, {"j", "m19"},
		{"g", "m19"}, {"g", "m0"},
		{"ctrl+d", "m5"}, {"ctrl+d", "m10"}, {"ctrl+u", "m5"}, {"pgdown", "m10"}, {"pgup", "m5"},
		// a different key between the two g's cancels the sequence
		{"g", "m5"}, {"j", "m6"}, {"g", "m6"},
	}
	for i, s := range steps {
		if !press(s.key) {
			t.Errorf("step %d: %q not handled", i, s.key)
		}
		if got := selID(t, l); got != s.want {
			t.Fatalf("step %d: after %q selected %q, want %q", i, s.key, got, s.want)
		}
		assertCells(t, viewLines(ctx, l, 20, 10), 20)
	}
	if handled, _ := l.HandleKey(ctx, keyMsg("enter")); handled {
		t.Error("enter must not be handled by the list")
	}
	if handled, _ := l.HandleKey(ctx, keyMsg("x")); handled {
		t.Error("unbound key must not be handled")
	}
	// the selection stays visible after jumps
	l.HandleKey(ctx, keyMsg("G"))
	lines := viewLines(ctx, l, 20, 10)
	if got := firstID(lines[9]); got != "m19" {
		t.Errorf("after G last line = %q", ansi.Strip(lines[9]))
	}
	l.HandleKey(ctx, keyMsg("home"))
	if got := firstID(viewLines(ctx, l, 20, 10)[0]); got != "m0" {
		t.Errorf("after home first line id = %q", got)
	}
}

func TestListViewExactSize(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	l.SetItems(items(15, func(i int) int { return i % 3 }, "가나다 라마바 사아자 차카타 파하"))
	check := func(label string) {
		t.Helper()
		lines := viewLines(ctx, l, 20, 7)
		if len(lines) != 7 {
			t.Fatalf("%s: %d lines, want 7", label, len(lines))
		}
		assertCells(t, lines, 20)
	}
	check("initial")
	for i := range 20 {
		l.HandleKey(ctx, keyMsg("j"))
		check(fmt.Sprintf("j #%d", i))
	}
	for i := range 20 {
		l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelUp}, ui.Point{})
		check(fmt.Sprintf("wheel up #%d", i))
	}
	for i := range 20 {
		l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelDown}, ui.Point{})
		check(fmt.Sprintf("wheel down #%d", i))
	}
	// tiny and empty sizes never panic and stay exact
	for w := 0; w <= 4; w++ {
		for h := 0; h <= 3; h++ {
			out := l.View(ctx, ui.Size{W: w, H: h})
			if w == 0 || h == 0 {
				if out != "" {
					t.Errorf("%dx%d = %q, want empty", w, h, out)
				}
				continue
			}
			lines := strings.Split(out, "\n")
			if len(lines) != h {
				t.Errorf("%dx%d: %d lines", w, h, len(lines))
			}
			assertCells(t, lines, w)
		}
	}
	empty := newTestList()
	lines := strings.Split(empty.View(ctx, ui.Size{W: 12, H: 4}), "\n")
	if len(lines) != 4 {
		t.Fatalf("empty list: %d lines", len(lines))
	}
	assertCells(t, lines, 12)
}

func TestListAnchorSurvivesWidthChange(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	l.SetItems(items(20, nil, "가나다 라마바 사아자 차카타 파하 하나 둘 셋"))
	l.View(ctx, ui.Size{W: 40, H: 5})
	l.Restore(widgets.ScrollAnchor{ItemID: "m7"})
	if got := firstID(viewLines(ctx, l, 40, 5)[0]); got != "m7" {
		t.Fatalf("width 40: first visible = %q, want m7", got)
	}
	a := l.Anchor()
	if a.ItemID != "m7" || a.LineOffset != 0 || a.AtBottom {
		t.Fatalf("anchor = %+v", a)
	}
	// width change alone keeps the anchor item on top
	lines := viewLines(ctx, l, 12, 5)
	if got := firstID(lines[0]); got != "m7" {
		t.Fatalf("width 12: first visible = %q, want m7", got)
	}
	assertCells(t, lines, 12)
	// an explicit Restore after re-wrap also lands on m7
	l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelDown}, ui.Point{})
	l.View(ctx, ui.Size{W: 12, H: 5})
	l.Restore(a)
	if got := firstID(viewLines(ctx, l, 12, 5)[0]); got != "m7" {
		t.Fatalf("Restore(a) at width 12: first visible = %q, want m7", got)
	}
	// a line offset beyond the item's lines is clamped, the item stays on top
	l.Restore(widgets.ScrollAnchor{ItemID: "m7", LineOffset: 99})
	lines = viewLines(ctx, l, 40, 5)
	m7 := renderItem(ctx, item{id: "m7", text: "가나다 라마바 사아자 차카타 파하 하나 둘 셋"}, false, 40)
	if got := l.Anchor(); got.ItemID != "m7" || got.LineOffset != len(m7)-1 {
		t.Fatalf("clamped offset: anchor = %+v, want m7 at line %d", got, len(m7)-1)
	}
	if got, want := ansi.Strip(lines[0]), m7[len(m7)-1]; !strings.HasPrefix(got, want) {
		t.Fatalf("clamped offset: first visible = %q, want m7's last line %q", got, want)
	}
	// unknown anchor ids are ignored
	before := l.Anchor()
	l.Restore(widgets.ScrollAnchor{ItemID: "gone"})
	l.View(ctx, ui.Size{W: 40, H: 5})
	if got := l.Anchor(); got.ItemID != before.ItemID {
		t.Fatalf("unknown anchor moved the view: %+v -> %+v", before, got)
	}
}

func TestListFollowBottom(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	l.Follow(true)
	l.SetItems(items(10, nil, ""))
	lines := viewLines(ctx, l, 20, 5)
	if !l.AtBottom() || firstID(lines[4]) != "m9" {
		t.Fatalf("initial follow: AtBottom=%v last=%q", l.AtBottom(), ansi.Strip(lines[4]))
	}
	more := append(items(10, nil, ""), item{id: "m10"}, item{id: "m11"}, item{id: "m12"})
	l.SetItems(more)
	if !l.AtBottom() {
		t.Fatal("AtBottom() false right after SetItems while following")
	}
	lines = viewLines(ctx, l, 20, 5)
	if !l.AtBottom() || firstID(lines[4]) != "m12" {
		t.Fatalf("after append: AtBottom=%v last=%q", l.AtBottom(), ansi.Strip(lines[4]))
	}
	// scrolled up => new items do not move the view
	l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelUp}, ui.Point{})
	lines = viewLines(ctx, l, 20, 5)
	if l.AtBottom() {
		t.Fatal("AtBottom() true after scrolling up")
	}
	top := firstID(lines[0])
	l.SetItems(append(more, item{id: "m13"}, item{id: "m14"}, item{id: "m15"}))
	lines = viewLines(ctx, l, 20, 5)
	if got := firstID(lines[0]); got != top || l.AtBottom() {
		t.Fatalf("scrolled-up view moved: top %q -> %q, AtBottom=%v", top, got, l.AtBottom())
	}
	// scrolling back to the bottom re-engages following
	for range 5 {
		l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelDown}, ui.Point{})
	}
	l.View(ctx, ui.Size{W: 20, H: 5})
	if !l.AtBottom() {
		t.Fatal("not at bottom after scrolling down")
	}
	l.SetItems(append(more, item{id: "m13"}, item{id: "m14"}, item{id: "m15"}, item{id: "m16"}))
	if got := firstID(viewLines(ctx, l, 20, 5)[4]); got != "m16" {
		t.Fatalf("follow after re-reaching bottom: last = %q", got)
	}

	// without Follow, appending keeps the view put even at the bottom
	n := newTestList()
	n.SetItems(items(10, nil, ""))
	n.HandleKey(ctx, keyMsg("G"))
	lines = viewLines(ctx, n, 20, 5)
	top = firstID(lines[0])
	n.SetItems(items(13, nil, ""))
	if got := firstID(viewLines(ctx, n, 20, 5)[0]); got != top {
		t.Fatalf("no follow: top moved %q -> %q", top, got)
	}
}

func TestListMouse(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	l.SetItems(items(20, nil, ""))
	l.View(ctx, ui.Size{W: 20, H: 10})
	l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelDown}, ui.Point{})
	lines := viewLines(ctx, l, 20, 10)
	if got := firstID(lines[0]); got != "m3" {
		t.Fatalf("wheel down: first = %q, want m3", got)
	}
	if got := selID(t, l); got != "m0" {
		t.Fatalf("wheel moved the selection to %q", got)
	}
	l.HandleMouse(ctx, tea.MouseClickMsg{Button: tea.MouseLeft, X: 30, Y: 30}, ui.Point{X: 2, Y: 1})
	if got := selID(t, l); got != "m4" {
		t.Fatalf("click row 1: selected %q, want m4", got)
	}
	l.HandleMouse(ctx, tea.MouseWheelMsg{Button: tea.MouseWheelUp}, ui.Point{})
	if got := firstID(viewLines(ctx, l, 20, 10)[0]); got != "m0" {
		t.Fatalf("wheel up: first = %q, want m0", got)
	}
	// click below the last item / outside does nothing
	short := newTestList()
	short.SetItems(items(2, nil, ""))
	short.View(ctx, ui.Size{W: 20, H: 10})
	short.HandleMouse(ctx, tea.MouseClickMsg{Button: tea.MouseLeft}, ui.Point{X: 0, Y: 8})
	short.HandleMouse(ctx, tea.MouseClickMsg{Button: tea.MouseLeft}, ui.Point{X: 0, Y: -1})
	if got := selID(t, short); got != "m0" {
		t.Fatalf("click on blank row selected %q", got)
	}
	// multi-line items: a click on an item's 2nd line selects it
	multi := newTestList()
	multi.SetItems(items(5, func(int) int { return 2 }, ""))
	multi.View(ctx, ui.Size{W: 20, H: 10})
	multi.HandleMouse(ctx, tea.MouseClickMsg{Button: tea.MouseLeft}, ui.Point{X: 0, Y: 4})
	if got := selID(t, multi); got != "m1" {
		t.Fatalf("click on 2nd line of m1 selected %q", got)
	}
}

// A mode change at the same width must re-render: render output depends on
// ctx.Density()/ctx.Mode, so cached lines from the old mode must not be served.
func TestListModeChangeSameWidthRerenders(t *testing.T) {
	ctx := newCtx(nil, false)
	ctx.Mode = layout.ModeStandard
	l := widgets.NewList(func(it item) string { return it.id },
		func(ctx *ui.Context, it item, selected bool, width int) []string {
			return []string{fmt.Sprintf("%s d%d", it.id, ctx.Density())}
		})
	l.SetItems(items(3, nil, ""))
	want := fmt.Sprintf("d%d", ui.DensityFull)
	if got := ansi.Strip(viewLines(ctx, l, 20, 3)[0]); !strings.Contains(got, want) {
		t.Fatalf("standard: first line = %q, want %q", got, want)
	}
	ctx.Mode = layout.ModeCompact
	want = fmt.Sprintf("d%d", ui.DensityLine)
	if got := ansi.Strip(viewLines(ctx, l, 20, 3)[0]); !strings.Contains(got, want) {
		t.Fatalf("compact at same width: first line = %q, want %q (stale cache)", got, want)
	}
	// command and standard share a density but are different modes: still re-rendered
	ctx.Mode = layout.ModeCommand
	want = fmt.Sprintf("d%d", ui.DensityFull)
	if got := ansi.Strip(viewLines(ctx, l, 20, 3)[0]); !strings.Contains(got, want) {
		t.Fatalf("command at same width: first line = %q, want %q", got, want)
	}
}

// At is the item drawn at a row of the last View (what a click there selects).
func TestListAt(t *testing.T) {
	ctx := newCtx(nil, false)
	multi := newTestList()
	multi.SetItems(items(5, func(int) int { return 2 }, ""))
	multi.View(ctx, ui.Size{W: 20, H: 10})
	for y, want := range map[int]string{0: "m0", 2: "m0", 3: "m1", 9: "m3"} { // 3 lines each
		if it, ok := multi.At(y); !ok || it.id != want {
			t.Fatalf("At(%d) = %+v %v, want %s", y, it, ok, want)
		}
	}
	short := newTestList()
	short.SetItems(items(2, nil, ""))
	short.View(ctx, ui.Size{W: 20, H: 10})
	for _, y := range []int{-1, 2, 8, 10} {
		if it, ok := short.At(y); ok {
			t.Fatalf("At(%d) on a blank row = %+v", y, it)
		}
	}
}

// A list at the bottom with a selection a few items above the last one keeps that
// selection on screen when the view shrinks (a narrower or shorter pane): bottom
// pinning must not push a selection that was visible out of view (r, t and the
// edit/delete keys act on it).
func TestListShrinkKeepsVisibleSelectionOnScreen(t *testing.T) {
	ctx := newCtx(nil, false)
	for _, sz := range [][2]int{{30, 12}, {20, 8}, {12, 6}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			l := newTestList()
			l.Follow(true)
			l.SetItems(items(40, func(i int) int { return i % 2 }, "가나다 라마바"))
			l.Select("m39")
			viewLines(ctx, l, 40, 30)
			l.Select("m33")
			lines := viewLines(ctx, l, 40, 30)
			if !l.AtBottom() || !hasID(lines, "m33") {
				t.Fatalf("precondition: AtBottom=%v, m33 on screen=%v", l.AtBottom(), hasID(lines, "m33"))
			}
			lines = viewLines(ctx, l, sz[0], sz[1])
			if !hasID(lines, "m33") {
				t.Fatalf("selection m33 left the view after shrinking:\n%s", ansi.Strip(strings.Join(lines, "\n")))
			}
			if got := selectedID(t, l, func(it item) string { return it.id }); got != "m33" {
				t.Fatalf("selection = %q, want m33", got)
			}
		})
	}
}

// A selection that was off screen (scrolled away) is not pulled back by a resize.
func TestListResizeLeavesHiddenSelectionAlone(t *testing.T) {
	ctx := newCtx(nil, false)
	l := newTestList()
	l.Follow(true)
	l.SetItems(items(40, nil, ""))
	l.Select("m0")
	viewLines(ctx, l, 20, 5)
	l.Restore(widgets.ScrollAnchor{AtBottom: true})
	lines := viewLines(ctx, l, 20, 5)
	if hasID(lines, "m0") {
		t.Fatal("precondition: m0 on screen")
	}
	lines = viewLines(ctx, l, 15, 4)
	if hasID(lines, "m0") || !l.AtBottom() {
		t.Fatalf("hidden selection pulled into view (AtBottom=%v):\n%s", l.AtBottom(), ansi.Strip(strings.Join(lines, "\n")))
	}
}

func hasID(lines []string, id string) bool {
	for _, l := range lines {
		if firstID(l) == id {
			return true
		}
	}
	return false
}
