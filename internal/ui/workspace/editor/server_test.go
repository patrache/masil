package editor

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/codeassist"
)

// v0.1.3-1: a language server's items come first in the menu, the words
// after; requests go out through TakeCompletionRequest, answers come back
// through SetServerItems.

func srvItem(label, detail string) codeassist.Item {
	return codeassist.Item{Label: label, Text: label, Detail: detail, Server: true}
}

func take(t *testing.T, e *Editor) CompletionRequest {
	t.Helper()
	r, ok := e.TakeCompletionRequest()
	if !ok {
		t.Fatal("no completion request")
	}
	return r
}

func TestServerItemsFirst(t *testing.T) {
	e := goEd("result results\n")
	e.SetServer(true, nil)
	press(t, e, "o", "r")
	if _, ok := e.TakeCompletionRequest(); ok {
		t.Fatal("asked after one char")
	}
	press(t, e, "e")
	r := take(t, e)
	if r.Line != 1 || r.Col != 2 || string(r.LineText) != "re" {
		t.Fatalf("request = %+v", r)
	}
	if _, ok := e.TakeCompletionRequest(); ok {
		t.Fatal("the request was taken twice")
	}
	// words show while the server thinks
	if got := menu(t, e); len(got) == 0 || got[0] != "result" {
		t.Fatalf("menu before the answer = %v", got)
	}
	if !e.SetServerItems(r.Seq, []codeassist.Item{srvItem("recover", "func(v any)"), srvItem("result", "var")}, false, nil) {
		t.Fatal("answer dropped")
	}
	got := menu(t, e)
	if len(got) < 3 || got[0] != "recover" || got[1] != "result" || got[2] != "results" {
		t.Fatalf("menu = %v", got)
	}
	if n := len(slices.DeleteFunc(slices.Clone(got), func(s string) bool { return s != "result" })); n != 1 {
		t.Fatalf("result twice: %v", got)
	}
	if v := ansi.Strip(e.View(true)); !strings.Contains(v, "func(v any)") {
		t.Fatalf("detail not drawn:\n%s", v)
	}
	// typing on filters the answer, no new request (it was complete)
	press(t, e, "c")
	if _, ok := e.TakeCompletionRequest(); ok {
		t.Fatal("asked again although the answer was complete")
	}
	if got := menu(t, e); len(got) != 1 || got[0] != "recover" {
		t.Fatalf("menu after c = %v", got)
	}
}

func TestServerIncompleteAsksAgain(t *testing.T) {
	e := goEd("\n")
	e.SetServer(true, nil)
	press(t, e, "i", "a", "b")
	r := take(t, e)
	e.SetServerItems(r.Seq, []codeassist.Item{srvItem("abc", "")}, true, nil)
	press(t, e, "c")
	if _, ok := e.TakeCompletionRequest(); !ok {
		t.Fatal("an incomplete answer was not asked again")
	}
}

// An answer for an older request, or after the cursor left the word, is
// dropped; esc drops the request in flight.
func TestServerStaleDropped(t *testing.T) {
	e := goEd("\n")
	e.SetServer(true, nil)
	press(t, e, "i", "a", "b")
	old := take(t, e)
	press(t, e, "c")
	if e.SetServerItems(old.Seq, []codeassist.Item{srvItem("abzz", "")}, false, nil) {
		t.Fatal("an older answer was used")
	}
	cur := take(t, e)
	press(t, e, " ")
	if e.SetServerItems(cur.Seq, []codeassist.Item{srvItem("abcd", "")}, false, nil) {
		t.Fatal("an answer for a word left behind was used")
	}
	press(t, e, "x", "y")
	r := take(t, e)
	press(t, e, "esc")
	if e.SetServerItems(r.Seq, []codeassist.Item{srvItem("xyz", "")}, false, nil) || e.CompletionOpen() {
		t.Fatal("an answer after esc opened the menu")
	}
}

// A failed request (timeout, no server) leaves the words.
func TestServerErrorKeepsWords(t *testing.T) {
	e := goEd("result\n")
	e.SetServer(true, nil)
	press(t, e, "o", "r", "e")
	r := take(t, e)
	e.SetServerItems(r.Seq, nil, false, errors.New("context deadline exceeded"))
	if got := menu(t, e); len(got) == 0 || got[0] != "result" {
		t.Fatalf("menu = %v", got)
	}
}

// A trigger character asks at once; the members show with no word typed.
func TestServerTriggerCharacter(t *testing.T) {
	e := goEd("fmt\n")
	e.SetServer(true, []string{"."})
	press(t, e, "A", ".")
	r := take(t, e)
	if r.Col != 4 {
		t.Fatalf("request = %+v", r)
	}
	e.SetServerItems(r.Seq, []codeassist.Item{srvItem("Println", "func(a ...any)"), srvItem("Sprintf", "")}, false, nil)
	if got := menu(t, e); !slices.Equal(got, []string{"Println", "Sprintf"}) {
		t.Fatalf("menu = %v", got)
	}
	press(t, e, "S")
	if got := menu(t, e); !slices.Equal(got, []string{"Sprintf"}) {
		t.Fatalf("menu after S = %v", got)
	}
	press(t, e, "tab")
	if e.Text() != "fmt.Sprintf\n" {
		t.Fatalf("text = %q", e.Text())
	}
	// without a server "." asks nothing
	w := goEd("fmt\n")
	press(t, w, "A", ".")
	if _, ok := w.TakeCompletionRequest(); ok {
		t.Fatal("asked without a server")
	}
}

// ctrl+n asks with no word typed.
func TestServerForced(t *testing.T) {
	e := goEd("\n")
	e.SetServer(true, nil)
	press(t, e, "i")
	pressMsg(e, ctrl('n'))
	r := take(t, e)
	e.SetServerItems(r.Seq, []codeassist.Item{srvItem("main", "")}, false, nil)
	if got := menu(t, e); len(got) == 0 || got[0] != "main" {
		t.Fatalf("menu = %v", got)
	}
}

// A text edit replaces its own range (rune columns, after Hangul) as one op.
func TestServerTextEdit(t *testing.T) {
	e := goEd("\n")
	e.SetServer(true, nil)
	press(t, e, "i")
	for _, k := range []string{"한", "글", ".", "P", "r", "x"} {
		press(t, e, k)
	}
	press(t, e, "left") // cursor between "Pr" and "x"
	pressMsg(e, ctrl('n'))
	r := take(t, e)
	if r.Col != 5 {
		t.Fatalf("col = %d", r.Col)
	}
	e.TakeOps()
	before := e.Text()
	// From 2: the "." too, so only the server's own range gives "->"
	it := codeassist.Item{Label: "Println", Text: "->Println()", Edit: true, From: 2, After: 1, Server: true}
	e.SetServerItems(r.Seq, []codeassist.Item{it}, false, nil)
	press(t, e, "tab")
	if e.Text() != "한글->Println()\n" {
		t.Fatalf("text = %q", e.Text())
	}
	ops := e.TakeOps()
	if len(ops) != 1 {
		t.Fatalf("ops = %v", ops)
	}
	if after, err := ops[0].Apply([]rune(before)); err != nil || string(after) != e.Text() {
		t.Fatalf("op %v: %q %v", ops[0], string(after), err)
	}
	if e.CursorIndex() != len([]rune("한글->Println()")) {
		t.Fatalf("cursor %d", e.CursorIndex())
	}
}

// Switched off: the server's items go, no more requests.
func TestServerOff(t *testing.T) {
	e := goEd("result\n")
	e.SetServer(true, nil)
	press(t, e, "o", "r", "e")
	r := take(t, e)
	e.SetServerItems(r.Seq, []codeassist.Item{srvItem("reOnlyServer", "")}, false, nil)
	if got := menu(t, e); !slices.Contains(got, "reOnlyServer") {
		t.Fatalf("server item missing: %v", got)
	}
	e.SetServer(false, nil)
	if got := menu(t, e); slices.Contains(got, "reOnlyServer") {
		t.Fatalf("server item kept: %v", got)
	}
	press(t, e, "s")
	if _, ok := e.TakeCompletionRequest(); ok {
		t.Fatal("asked with the server off")
	}
}
