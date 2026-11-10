package editor

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/workspace/ot"
)

// key builds a key press the way the terminal decoder does for these names.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case " ":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	}
	r, _ := utf8.DecodeRuneInString(s)
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(t *testing.T, e *Editor, keys ...string) []tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	for _, k := range keys {
		_, cmd := e.HandleKey(key(k))
		if cmd != nil {
			if m := cmd(); m != nil {
				msgs = append(msgs, m)
			}
		}
	}
	return msgs
}

func newEd(text string, ro bool) *Editor {
	e := New(theme.Get(theme.Default), text, ro)
	e.Resize(40, 10)
	return e
}

func TestTypingProducesOps(t *testing.T) {
	e := newEd("abc", false)
	press(t, e, "i", "X")
	if got, want := e.TakeOps(), []ot.Op{ot.Insert(0, "X", 3)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("insert ops = %v, want %v", got, want)
	}
	if e.Text() != "Xabc" {
		t.Fatalf("text = %q", e.Text())
	}
	press(t, e, "esc", "x")
	if got, want := e.TakeOps(), []ot.Op{ot.Delete(0, 1, 4)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delete ops = %v, want %v", got, want)
	}
	if got := e.TakeOps(); len(got) != 0 {
		t.Fatalf("TakeOps twice = %v", got)
	}
}

func TestTrailingNewlineKept(t *testing.T) {
	e := newEd("a\nb\n", false)
	if e.Text() != "a\nb\n" {
		t.Fatalf("text = %q", e.Text())
	}
	press(t, e, "i", "Z")
	if e.Text() != "Za\nb\n" {
		t.Fatalf("after insert = %q", e.Text())
	}
	if got, want := e.TakeOps(), []ot.Op{ot.Insert(0, "Z", 4)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
}

func TestKoreanInputIsOneRunePerRune(t *testing.T) {
	e := newEd("", false)
	press(t, e, "i", "한", "글")
	if e.Text() != "한글" {
		t.Fatalf("text = %q", e.Text())
	}
}

func TestApplyRemoteKeepsCursor(t *testing.T) {
	e := newEd("hello world", false)
	press(t, e, "l", "l", "l", "l", "l")
	if e.CursorIndex() != 5 {
		t.Fatalf("cursor = %d", e.CursorIndex())
	}
	e.ApplyRemote(ot.Insert(0, "AB", 11))
	if e.CursorIndex() != 7 || e.Text() != "ABhello world" {
		t.Fatalf("cursor %d text %q", e.CursorIndex(), e.Text())
	}
	if ops := e.TakeOps(); len(ops) != 0 {
		t.Fatalf("a remote edit came back as mine: %v", ops)
	}
}

func TestUndoAndSaveIntercepted(t *testing.T) {
	e := newEd("abc", false)
	msgs := press(t, e, "u")
	if len(msgs) != 1 || !reflect.DeepEqual(msgs[0], UndoMsg{}) {
		t.Fatalf("u = %#v", msgs)
	}
	msgs = press(t, e, "ctrl+r")
	if len(msgs) != 1 || !reflect.DeepEqual(msgs[0], RedoMsg{}) {
		t.Fatalf("ctrl+r = %#v", msgs)
	}
	msgs = press(t, e, ":", "w", "enter")
	if len(msgs) != 1 || !reflect.DeepEqual(msgs[0], SaveMsg{}) {
		t.Fatalf(":w = %#v", msgs)
	}
	msgs = press(t, e, ":", "q", "enter")
	if len(msgs) != 1 || !reflect.DeepEqual(msgs[0], CloseMsg{}) {
		t.Fatalf(":q = %#v", msgs)
	}
	if e.Text() != "abc" || len(e.TakeOps()) != 0 || e.CapturesInput() {
		t.Fatalf("text %q captures %v", e.Text(), e.CapturesInput())
	}
}

func TestAdapterEmojiColumns(t *testing.T) {
	const s = "👩‍💻한글a"
	e := newEd(s, false)
	// l walks whole graphemes: 👩‍💻 (3 runes) -> 한 -> 글 -> a.
	var stops []int
	for range 3 {
		press(t, e, "l")
		stops = append(stops, e.CursorIndex())
	}
	if want := []int{3, 4, 5}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("l stops = %v, want %v", stops, want)
	}
	press(t, e, "h", "h")
	if e.CursorIndex() != 3 {
		t.Fatalf("h back = %d", e.CursorIndex())
	}
	press(t, e, "h")
	if e.CursorIndex() != 0 {
		t.Fatalf("h onto the emoji = %d (inside the ZWJ sequence)", e.CursorIndex())
	}
	press(t, e, "l", "l", "l", "i", "Q")
	at := utf8.RuneCountInString("👩‍💻한글")
	if got, want := e.TakeOps(), []ot.Op{ot.Insert(at, "Q", utf8.RuneCountInString(s))}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
}

func TestViewExactSize(t *testing.T) {
	th := theme.Get(theme.Default)
	text := strings.Repeat("func main() { 한글 👩‍💻 }\n", 40)
	for _, sz := range [][2]int{{30, 5}, {94, 20}} {
		e := New(th, text, false)
		e.Resize(sz[0], sz[1])
		e.SetPeers([]Peer{
			{Name: "minji", Color: th.P.Acc2, Cursor: 3, SelFrom: 3, SelTo: 9},
			{Name: "jun\x1b[31m", Color: th.P.Acc3, Cursor: 30},
		})
		for _, focused := range []bool{true, false} {
			v := e.View(focused)
			lines := strings.Split(v, "\n")
			if len(lines) != sz[1] {
				t.Fatalf("%v: %d lines", sz, len(lines))
			}
			for i, l := range lines {
				if w := textwidth.Width(l); w != sz[0] {
					t.Fatalf("%v line %d width %d: %q", sz, i, w, ansi.Strip(l))
				}
			}
			if strings.Contains(ansi.Strip(v), "\x1b") {
				t.Fatal("a peer name escape reached the screen")
			}
		}
		if sz[0] >= 94 && !strings.Contains(ansi.Strip(e.View(true)), "minji") {
			t.Fatalf("%v: peer name not drawn", sz)
		}
	}
}

func TestReadOnlyRejectsEdits(t *testing.T) {
	e := newEd("abc", true)
	press(t, e, "i", "x", "esc", "x", "d", "d")
	if ops := e.TakeOps(); len(ops) != 0 || e.Text() != "abc" {
		t.Fatalf("ops %v text %q", ops, e.Text())
	}
	e.SetReadOnly(false)
	press(t, e, "x")
	if ops := e.TakeOps(); len(ops) != 1 {
		t.Fatalf("after SetReadOnly(false): %v", ops)
	}
}
