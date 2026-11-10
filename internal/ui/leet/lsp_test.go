package leet_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/lsp"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/codelsp"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

// v0.1.3-1: the built-in editor opens the solution in its language's server
// (root: the solution's folder); the server's items come first in the menu.

func TestGoeditorLSP(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	f := codelsp.NewFake()
	t.Cleanup(codelsp.SetManagerForTest(f))
	f.Trigger = []string{"."}
	f.Answer = func(_ string, line, col int) (lsp.List, error) {
		return lsp.List{Items: []lsp.Item{
			{Label: "Println", InsertText: "Println", Detail: "func(a ...any)", SortText: "1"},
			{Label: "buServer", InsertText: "buServer", Kind: lsp.KindFunction, SortText: "0"},
		}}, nil
	}
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	path, _ := filepath.Abs(solutionPath(e))
	root, text, ok := f.Opened(path)
	if !ok || root != filepath.Dir(path) || !strings.Contains(text, "busiestServers") {
		t.Fatalf("not open in the server: %v %q (open %v)", ok, root, f.OpenPaths())
	}
	h.Press("o", "b", "u")
	if a := f.Asked(); len(a) != 1 || a[0].Path != path || a[0].UTF16Col != 2 {
		t.Fatalf("asked %+v", a)
	}
	labels := leet.CompletionLabelsForTest(todayInstance(t, h))
	if len(labels) < 3 || labels[0] != "buServer" || labels[1] != "Println" || !slices.Contains(labels, "busiestServers") {
		t.Fatalf("menu = %v", labels)
	}
	if !strings.Contains(h.Screen(), "function") {
		t.Fatalf("detail not drawn:\n%s", h.Screen())
	}
	h.Press("tab")
	if _, text, _ := f.Opened(path); !strings.Contains(text, "\nbuServer\n") {
		t.Fatalf("server text after accepting: %q", text)
	}
	// a trigger character asks at once
	h.Press("x", ".")
	if n := len(f.Asked()); n != 2 {
		t.Fatalf("%d requests after '.'", n)
	}
	if labels := leet.CompletionLabelsForTest(todayInstance(t, h)); len(labels) != 2 {
		t.Fatalf("members menu = %v", labels)
	}
	h.Press("esc", "esc")
	// leaving the tool closes it in the server
	h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	if !slices.Contains(f.Closed(), path) {
		t.Fatalf("closed %v", f.Closed())
	}
}

// A server's text edit replaces its own range (UTF-16 columns) through
// typed keys.
func TestGoeditorLSPTextEdit(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	f := codelsp.NewFake()
	t.Cleanup(codelsp.SetManagerForTest(f))
	f.Answer = func(_ string, line, col int) (lsp.List, error) {
		// the range takes the "." too (UTF-16 2 = after the emoji), so only
		// the server's own range gives "->"
		return lsp.List{Items: []lsp.Item{{Label: "Println", InsertText: "->Println()",
			Edit: &lsp.TextEdit{Range: lsp.Range{Start: lsp.Position{Line: line, Character: 2}, End: lsp.Position{Line: line, Character: col + 1}}, NewText: "->Println()"}}}}, nil
	}
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e", "O")
	h.Type("😀")
	h.Press(".", "x", "left", "P", "r") // "😀.Pr|x"
	if a := f.Asked(); len(a) == 0 || a[len(a)-1].UTF16Col != 5 {
		t.Fatalf("asked %+v (want UTF-16 column 5)", a)
	}
	h.Press("tab", "esc", ":", "w", "enter")
	if got := readFile(t, solutionPath(e)); !strings.HasPrefix(got, "😀->Println()\n") {
		t.Fatalf("file = %q", got)
	}
}

// The code completion setting turns the server off too; without a server
// the words stay.
func TestGoeditorLSPOff(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	f := codelsp.NewFake() // Answer nil: unavailable
	t.Cleanup(codelsp.SetManagerForTest(f))
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e", "o", "b", "u")
	if labels := leet.CompletionLabelsForTest(todayInstance(t, h)); !slices.Contains(labels, "busiestServers") {
		t.Fatalf("words gone without a server: %v", labels)
	}
	h.Press("esc")
	path, _ := filepath.Abs(solutionPath(e))
	h.Context().Config.UI.CodeComplete = false
	h.Press("i")
	if _, _, ok := f.Opened(path); ok {
		t.Fatal("still open in the server with completion off")
	}
}
