package editor

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/workspace/ot"
)

func goEd(text string) *Editor {
	e := newEd(text, false)
	e.SetLanguage("go")
	return e
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func pressMsg(e *Editor, msgs ...tea.KeyPressMsg) {
	for _, m := range msgs {
		e.HandleKey(m)
	}
}

func menu(t *testing.T, e *Editor) []string {
	t.Helper()
	items, _, ok := e.Completion()
	if !ok {
		return nil
	}
	return items
}

func TestHighlightInView(t *testing.T) {
	th := theme.Get(theme.Default)
	e := goEd("func main() {\n\treturn // x\n}\n")
	v := e.View(false) // unfocused: no cursor cell over the first f
	kw := th.EditorTokenStyle(chroma.Keyword)
	if !strings.Contains(v, kw.Render("f")) || !strings.Contains(v, kw.Render("r")) {
		t.Fatalf("keywords are not colored:\n%q", v)
	}
	if cm := th.EditorTokenStyle(chroma.Comment); !strings.Contains(v, cm.Render("x")) {
		t.Fatalf("comment is not colored:\n%q", v)
	}
	if plain := ansi.Strip(v); !strings.Contains(plain, "func main() {") {
		t.Fatalf("text changed:\n%s", plain)
	}
	// switched off: plain FG again
	e.SetAssist(false, true)
	if v := e.View(false); strings.Contains(v, kw.Render("f")) {
		t.Fatal("highlighting stays on when switched off")
	}
	// an unknown file type stays plain
	p := newEd("func x", false)
	if v := p.View(false); strings.Contains(v, kw.Render("f")) {
		t.Fatal("no language, still colored")
	}
}

// A remote edit is drawn with the right colors (the window is lexed again).
func TestHighlightFollowsRemoteEdit(t *testing.T) {
	th := theme.Get(theme.Default)
	e := goEd("x\n")
	e.View(true)
	e.ApplyRemote(ot.Insert(0, "return ", 2))
	if v := e.View(true); !strings.Contains(v, th.EditorTokenStyle(chroma.Keyword).Render("r")) {
		t.Fatalf("remote text not highlighted:\n%q", v)
	}
}

func TestCompletionMenuOpensAfterTwoChars(t *testing.T) {
	e := goEd("result results\n")
	press(t, e, "o", "r")
	if m := menu(t, e); m != nil {
		t.Fatalf("menu after one char: %v", m)
	}
	press(t, e, "e")
	got := menu(t, e)
	if len(got) < 3 || got[0] != "result" || got[1] != "results" || !slices.Contains(got, "return") {
		t.Fatalf("menu = %v", got)
	}
	if v := ansi.Strip(e.View(true)); !strings.Contains(v, "results") || !strings.Contains(v, "return") {
		t.Fatalf("menu not drawn:\n%s", v)
	}
	// a non-word key closes it
	press(t, e, " ")
	if m := menu(t, e); m != nil {
		t.Fatalf("menu after space: %v", m)
	}
}

func TestCompletionMaxVisible(t *testing.T) {
	var words []string
	for _, c := range "abcdefghijkl" {
		words = append(words, "zz"+string(c))
	}
	e := goEd(strings.Join(words, " ") + "\n")
	e.Resize(40, 20)
	press(t, e, "o", "z", "z")
	if n := len(menu(t, e)); n != 12 {
		t.Fatalf("%d items", n)
	}
	v := ansi.Strip(e.View(true))
	shown := 0
	for _, w := range words {
		if strings.Count(v, w) > 1 { // once in the text, once in the menu
			shown++
		}
	}
	if shown != 8 {
		t.Fatalf("%d items drawn, want 8:\n%s", shown, v)
	}
}

// Accepting is one ordinary edit; moving in the menu edits nothing (so
// nothing reaches the others before the choice).
func TestCompletionAcceptIsOneOp(t *testing.T) {
	e := goEd("result results\n")
	press(t, e, "o", "r", "e")
	e.TakeOps()
	before := e.Text()
	press(t, e, "down")
	pressMsg(e, ctrl('n'), ctrl('p'))
	if ops := e.TakeOps(); len(ops) != 0 {
		t.Fatalf("moving in the menu made ops %v", ops)
	}
	if e.Text() != before {
		t.Fatalf("text changed before accepting: %q", e.Text())
	}
	if _, sel, _ := e.Completion(); sel != 1 {
		t.Fatalf("selected %d, want 1 (down, ctrl+n, ctrl+p)", sel)
	}
	press(t, e, "tab")
	if e.Text() != "result results\nresults\n" {
		t.Fatalf("text = %q", e.Text())
	}
	ops := e.TakeOps()
	if len(ops) != 1 {
		t.Fatalf("ops = %v", ops)
	}
	after, err := ops[0].Apply([]rune(before))
	if err != nil || string(after) != e.Text() {
		t.Fatalf("op %v gives %q (%v)", ops[0], string(after), err)
	}
	if m := menu(t, e); m != nil {
		t.Fatalf("menu open after accepting: %v", m)
	}
	if e.Mode() != "insert" {
		t.Fatalf("mode %s after accepting", e.Mode())
	}
}

func TestEnterWithoutMenuIsNewline(t *testing.T) {
	e := goEd("ab\n")
	press(t, e, "A", "x", "enter")
	if e.Text() != "abx\n\n" {
		t.Fatalf("text = %q", e.Text())
	}
	// with the menu open, enter accepts
	e = goEd("result\n")
	press(t, e, "o", "r", "e", "enter")
	if e.Text() != "result\nresult\n" {
		t.Fatalf("enter did not accept: %q", e.Text())
	}
	// and then a second enter is a newline again
	press(t, e, "enter")
	if e.Text() != "result\nresult\n\n" {
		t.Fatalf("enter after accepting: %q", e.Text())
	}
}

func TestEscClosesMenuThenLeavesInsert(t *testing.T) {
	e := goEd("result\n")
	press(t, e, "o", "r", "e")
	if menu(t, e) == nil {
		t.Fatal("no menu")
	}
	press(t, e, "esc")
	if menu(t, e) != nil || e.Mode() != "insert" {
		t.Fatalf("first esc: menu %v, mode %s", menu(t, e), e.Mode())
	}
	if e.Text() != "result\nre\n" {
		t.Fatalf("esc changed the text: %q", e.Text())
	}
	press(t, e, "esc")
	if e.Mode() != "normal" {
		t.Fatalf("second esc: mode %s", e.Mode())
	}
}

func TestCompletionForced(t *testing.T) {
	e := goEd("result\n")
	press(t, e, "o", "r")
	pressMsg(e, ctrl('n'))
	if m := menu(t, e); len(m) == 0 || m[0] != "result" {
		t.Fatalf("ctrl+n menu = %v", m)
	}
	press(t, e, "esc")
	pressMsg(e, tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if menu(t, e) == nil {
		t.Fatal("ctrl+space does not open the menu")
	}
	if e.Text() != "result\nr\n" {
		t.Fatalf("forcing typed: %q", e.Text())
	}
}

func TestCompletionOff(t *testing.T) {
	e := goEd("result\n")
	e.SetAssist(true, false)
	press(t, e, "o", "r", "e")
	pressMsg(e, ctrl('n'))
	if m := menu(t, e); m != nil {
		t.Fatalf("menu while off: %v", m)
	}
}

func TestCompletionOtherBuffers(t *testing.T) {
	e := goEd("\n")
	e.SetWordSource(func() []map[string]int { return []map[string]int{{"elsewhere": 3}} })
	press(t, e, "i", "e", "l")
	if m := menu(t, e); len(m) == 0 || m[0] != "elsewhere" {
		t.Fatalf("menu = %v", m)
	}
}

// Pasted text is typed without the menu: its newlines stay newlines.
func TestPasteSkipsCompletion(t *testing.T) {
	e := goEd("result\n")
	press(t, e, "o")
	e.Paste("re\nx")
	if e.Text() != "result\nre\nx\n" {
		t.Fatalf("text = %q", e.Text())
	}
	if m := menu(t, e); m != nil {
		t.Fatalf("menu after paste: %v", m)
	}
}

// A large file stays fast: each key lexes the shown window only, and the
// word index recounts only the edited line.
func TestLargeFileTyping(t *testing.T) {
	var b strings.Builder
	for i := range 20000 {
		b.WriteString("value" + strings.Repeat("x", i%7) + " := compute(i) // note\n")
	}
	e := goEd(b.String())
	e.Resize(80, 40)
	press(t, e, "G", "o")
	start := time.Now()
	for _, k := range []string{"v", "a", "l", "u", "backspace", "u", "e"} {
		press(t, e, k)
		e.View(true)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("7 keys on a 20000-line file took %v", d)
	}
	if m := menu(t, e); len(m) == 0 || !strings.HasPrefix(m[0], "value") {
		t.Fatalf("menu = %v", m)
	}
}

// In a short pane the menu is drawn with fewer rows than MaxVisible; moving
// down keeps the selected item among the drawn rows.
func TestCompletionShortPaneKeepsSelectionVisible(t *testing.T) {
	var words []string
	for i := 1; i <= 9; i++ {
		words = append(words, "aa"+string(rune('0'+i)))
	}
	e := goEd(strings.Join(words, " ") + "\n\n\n\n\n")
	e.Resize(50, 10)
	press(t, e, "j", "j", "j", "j", "o", "a", "a")
	if n := len(menu(t, e)); n != 9 {
		t.Fatalf("%d items, want 9", n)
	}
	for range 6 {
		press(t, e, "down")
	}
	items, sel, ok := e.Completion()
	if !ok {
		t.Fatal("menu closed")
	}
	want := items[sel]
	v := ansi.Strip(e.View(true))
	if strings.Count(v, want) < 2 { // once in the text, once in the menu
		t.Fatalf("selected %q (%d) is not drawn:\n%s", want, sel, v)
	}
	// and every way back up keeps it drawn too
	for range 9 {
		press(t, e, "up")
		items, sel, _ = e.Completion()
		if v := ansi.Strip(e.View(true)); strings.Count(v, items[sel]) < 2 {
			t.Fatalf("selected %q (%d) is not drawn going up:\n%s", items[sel], sel, v)
		}
	}
}

// Someone else's edit refills the menu but keeps my selection.
func TestRemoteEditKeepsSelection(t *testing.T) {
	e := goEd("alpha1 alpha2 alpha3\nzzz\n")
	press(t, e, "o", "a", "l")
	press(t, e, "down", "down")
	items, sel, ok := e.Completion()
	if !ok || sel != 2 {
		t.Fatalf("menu %v sel %d", items, sel)
	}
	want := items[sel]
	e.ApplyRemote(ot.Insert(24, "y", len([]rune(e.Text())))) // into "zzz" (after "al\n")
	items, sel, ok = e.Completion()
	if !ok || items[sel] != want {
		t.Fatalf("after a remote edit selected %v[%d], want %q", items, sel, want)
	}
}

// Turning read-only (a guest's level drops) closes the menu, and the next
// menu key is not eaten by it.
func TestReadOnlyClosesMenu(t *testing.T) {
	e := goEd("result\n")
	press(t, e, "o", "r", "e")
	if !e.CompletionOpen() {
		t.Fatal("no menu")
	}
	e.SetReadOnly(true)
	if e.CompletionOpen() || e.Mode() != "normal" {
		t.Fatalf("read-only: menu open %v, mode %s", e.CompletionOpen(), e.Mode())
	}
	if v := ansi.Strip(e.View(true)); strings.Count(v, "result") > 1 {
		t.Fatalf("menu still drawn:\n%s", v)
	}
	// the stale-state backstop: a menu left open outside insert mode is not drawn and eats no key
	e.SetReadOnly(false)
	e.as.open, e.as.items = true, []string{"result"}
	if v := ansi.Strip(e.View(true)); strings.Count(v, "result") > 1 {
		t.Fatalf("menu drawn in normal mode:\n%s", v)
	}
	press(t, e, "esc")
	if e.CompletionOpen() {
		t.Fatal("menu still open after a key in normal mode")
	}
}

// A theme switch at run time recolors the tokens (the cache is dropped).
func TestSetThemeRecolorsTokens(t *testing.T) {
	old, next := theme.Get(theme.Default), theme.Get(theme.Cream)
	kwOld, kwNew := old.EditorTokenStyle(chroma.Keyword).Render("f"), next.EditorTokenStyle(chroma.Keyword).Render("f")
	if kwOld == kwNew {
		t.Skip("the two themes color keywords alike")
	}
	e := goEd("func main() {}\n")
	if v := e.View(false); !strings.Contains(v, kwOld) { // unfocused: no cursor cell over f
		t.Fatalf("not colored by the first theme:\n%q", v)
	}
	e.SetTheme(next)
	v := e.View(false)
	if !strings.Contains(v, kwNew) || strings.Contains(v, kwOld) {
		t.Fatalf("still the old theme's colors:\n%q", v)
	}
}
