package workspace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/wire"
)

// v0.1.3-1: the code cell completes words. The menu is the guest's own:
// moving in it sends nothing; accepting sends one ordinary doc_op that the
// owner sequences like typed text.
func TestGuestCompletionSendsOnlyOnAccept(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelEditor)
	r.s.guestOpen(h.Context(), hubTS)
	r.settle()
	d := r.s.code.current()
	if d == nil || d.ed.Language() != "typescript" {
		t.Fatalf("document not open as typescript: %+v", d)
	}
	h.Press("o", "D", "u", "r")
	r.settle()
	items, _, ok := d.ed.Completion()
	if !ok || items[0] != "DurableObject" {
		t.Fatalf("menu = %v %v\n%s", items, ok, h.Screen())
	}
	if !strings.Contains(ansi.Strip(h.Screen()), " DurableObject ") {
		t.Fatalf("menu not drawn:\n%s", h.Screen())
	}
	sent := r.sentKinds()[wire.KindDocOp]
	h.Press("down", "up", "ctrl+n", "ctrl+p")
	r.settle()
	if n := r.sentKinds()[wire.KindDocOp]; n != sent {
		t.Fatalf("moving in the menu sent %d doc_op frames", n-sent)
	}
	h.Press("tab")
	r.settle()
	if n := r.sentKinds()[wire.KindDocOp]; n != sent+1 {
		t.Fatalf("accepting sent %d doc_op frames, want 1 (%d before); text %.80q mode %s", n-sent, sent, d.ed.Text(), d.ed.Mode())
	}
	text, _, _ := r.docs.Text(hubTS)
	if !strings.Contains(text, "(not real code).\nDurableObject\nimport") {
		t.Fatalf("owner text = %.120q", text)
	}
	if d.ed.Text() != text {
		t.Fatal("the guest's copy and the owner's differ")
	}
	// esc closes the menu first, then leaves insert mode
	h.Press("enter", "D", "u")
	if _, _, ok := d.ed.Completion(); !ok {
		t.Fatal("no menu for Du")
	}
	h.Press("esc")
	if _, _, ok := d.ed.Completion(); ok || d.ed.Mode() != "insert" {
		t.Fatalf("first esc: menu %v, mode %s", ok, d.ed.Mode())
	}
	h.Press("esc")
	if d.ed.Mode() != "normal" {
		t.Fatalf("second esc: mode %s", d.ed.Mode())
	}
	h.Check()
}

// The settings switches reach an open document.
func TestCodeAssistSwitches(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	dir := copyFixture(t)
	s := openFolder(t, h, "workspace:test", dir)
	h.Press("j", "enter", "j", "enter", "j", "enter")
	d := s.code.current()
	if d == nil {
		t.Fatalf("not open:\n%s", h.Screen())
	}
	h.Context().Config.UI.CodeComplete = false
	h.Press("o", "D", "u", "r")
	if _, _, ok := d.ed.Completion(); ok {
		t.Fatal("menu with completion off")
	}
	h.Context().Config.UI.CodeComplete = true
	h.Press("a")
	if _, _, ok := d.ed.Completion(); !ok {
		t.Fatalf("no menu with completion on:\n%s", h.Screen())
	}
	h.Press("esc", "esc")
	h.Check()
}

// ctrl+n opens the menu in insert mode (before the notifications' ctrl+n);
// in normal mode it is the notifications again.
func TestCtrlNInCodeCell(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	dir := copyFixture(t)
	s := openFolder(t, h, "workspace:test", dir)
	h.Press("j", "enter", "j", "enter", "j", "enter")
	d := s.code.current()
	if d == nil {
		t.Fatalf("not open:\n%s", h.Screen())
	}
	front := h.Nav().Front
	h.Press("o", "D", "ctrl+n")
	if items, _, ok := d.ed.Completion(); !ok || items[0] != "DurableObject" {
		t.Fatalf("ctrl+n with one char: %v %v", items, ok)
	}
	if h.Nav().Front != front {
		t.Fatal("ctrl+n in insert mode opened the notifications")
	}
	h.Press("ctrl+n", "ctrl+p", "enter")
	if !strings.Contains(d.ed.Text(), "\nDurableObject\n") {
		t.Fatalf("enter did not accept: %.100q", d.ed.Text())
	}
	h.Press("esc")
	if d.ed.Mode() != "normal" {
		t.Fatalf("mode %s", d.ed.Mode())
	}
	h.Press("ctrl+n")
	if h.Nav().Front == front {
		t.Fatal("ctrl+n in normal mode no longer opens the notifications")
	}
}
