package workspace

import (
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/lsp"
	"github.com/patrache/masil/internal/ui/codelsp"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/wire"
)

// v0.1.3-1: the owner's code cell asks a language server; its items come
// first in the menu. A share guest never talks to one.

func installLSP(t *testing.T) *codelsp.Fake {
	t.Helper()
	f := codelsp.NewFake()
	t.Cleanup(codelsp.SetManagerForTest(f))
	return f
}

// openHub opens the fixture folder and hub.ts in the code cell.
func openHub(t *testing.T, h *uitest.Harness) (*Surface, *openDoc) {
	t.Helper()
	s := openFolder(t, h, "workspace:test", copyFixture(t))
	h.Press("j", "enter", "j", "enter", "j", "enter")
	d := s.code.current()
	if d == nil || d.rel != hubTS {
		t.Fatalf("hub.ts not open:\n%s", h.Screen())
	}
	return s, d
}

func TestOwnerLSPItemsFirst(t *testing.T) {
	setup(t)
	f := installLSP(t)
	f.Trigger = []string{"."}
	f.Answer = func(string, int, int) (lsp.List, error) {
		return lsp.List{Items: []lsp.Item{
			{Label: "seatsFromServer", InsertText: "seatsFromServer", Detail: "Map<number, WebSocket>", SortText: "b"},
			{Label: "sendAll", InsertText: "sendAll", Kind: lsp.KindMethod, SortText: "a"},
		}}, nil
	}
	h := uitest.New(t)
	s, d := openHub(t, h)
	path := s.lspPath(d)
	root, text, ok := f.Opened(path)
	if !ok || root != s.dir || !strings.HasPrefix(text, "// Test fixture") {
		t.Fatalf("not opened in the server: %v %q %.20q (open %v)", ok, root, text, f.OpenPaths())
	}
	h.Press("O", "s", "e")
	asked := f.Asked()
	if len(asked) != 1 || asked[0] != (codelsp.FakeAsk{Path: path, Line: 0, UTF16Col: 2}) {
		t.Fatalf("asked %+v", asked)
	}
	if _, text, _ := f.Opened(path); !strings.HasPrefix(text, "se\n") {
		t.Fatalf("the server has %.20q, not the typed text", text)
	}
	items := d.ed.CompletionItems()
	if len(items) < 3 || items[0].Label != "sendAll" || items[1].Label != "seatsFromServer" || items[2].Server {
		t.Fatalf("menu = %+v", items)
	}
	if !slices.ContainsFunc(items, func(it codeassist.Item) bool { return it.Label == "seats" && !it.Server }) {
		t.Fatalf("the file's own word is missing: %+v", items)
	}
	scr := h.Screen()
	if !strings.Contains(scr, "seatsFromServer") || !strings.Contains(scr, "Map<number,") || !strings.Contains(scr, "method") {
		t.Fatalf("menu not drawn with details:\n%s", scr)
	}
	h.Press("tab")
	if line, _, _ := strings.Cut(d.ed.Text(), "\n"); line != "sendAll" {
		t.Fatalf("accepted line %q", line)
	}
	// a trigger character asks at once
	h.Press(".")
	if n := len(f.Asked()); n != 2 {
		t.Fatalf("%d requests after '.'", n)
	}
	h.Press("esc")
	if !d.ed.CompletionOpen() && d.ed.Mode() == "insert" {
		h.Press("esc")
	}
	// closing the document closes it in the server
	h.Press(":", "q", "!", "enter")
	if !slices.Contains(f.Closed(), path) {
		t.Fatalf("closed %v", f.Closed())
	}
}

// A text edit's UTF-16 range lands on the right runes (an emoji is two
// UTF-16 units, one rune).
func TestOwnerLSPTextEdit(t *testing.T) {
	setup(t)
	f := installLSP(t)
	f.Answer = func(_ string, line, col int) (lsp.List, error) {
		return lsp.List{Items: []lsp.Item{{Label: "Println", InsertText: "Println()",
			Edit: &lsp.TextEdit{Range: lsp.Range{Start: lsp.Position{Line: line, Character: 3}, End: lsp.Position{Line: line, Character: col}}, NewText: "Println()"}}}}, nil
	}
	h := uitest.New(t)
	s, d := openHub(t, h)
	h.Press("O", "😀", ".", "P", "r")
	if a := f.Asked(); len(a) == 0 || a[len(a)-1].UTF16Col != 5 {
		t.Fatalf("asked %+v (want UTF-16 column 5)", a)
	}
	h.Press("tab")
	if line, _, _ := strings.Cut(d.ed.Text(), "\n"); line != "😀.Println()" {
		t.Fatalf("line %q", line)
	}
	h.Press("esc")
	if _, text, _ := f.Opened(s.lspPath(d)); !strings.HasPrefix(text, "😀.Println()\n") {
		t.Fatalf("server text %.20q", text)
	}
}

// A co-editor's (or the disk's) edit reaches the server too.
func TestOwnerLSPFollowsRemoteEdits(t *testing.T) {
	setup(t)
	f := installLSP(t)
	h := uitest.New(t)
	s, d := openHub(t, h)
	before := f.Changes()
	seq, applied, err := s.docs.Submit(d.rel, 1, "minji", d.seq, ot.Insert(0, "Q", len(d.base)))
	if err != nil {
		t.Fatal(err)
	}
	s.applyRemote(d, seq, applied)
	if _, text, _ := f.Opened(s.lspPath(d)); !strings.HasPrefix(text, "Q// Test") || f.Changes() != before+1 {
		t.Fatalf("server text %.20q, %d changes", text, f.Changes()-before)
	}
}

// The code completion setting switches the server off too; the tool's
// close closes every document.
func TestOwnerLSPSettingAndClose(t *testing.T) {
	setup(t)
	f := installLSP(t)
	h := uitest.New(t)
	s, d := openHub(t, h)
	path := s.lspPath(d)
	h.Context().Config.UI.CodeComplete = false
	h.Press("O", "s", "e")
	if _, _, ok := f.Opened(path); ok || len(f.Asked()) != 0 {
		t.Fatalf("server still in use with completion off (asked %v)", f.Asked())
	}
	h.Press("esc")
	h.Context().Config.UI.CodeComplete = true
	h.Press("i")
	if _, _, ok := f.Opened(path); !ok {
		t.Fatal("not opened again when switched back on")
	}
	s.CloseTool(h.Context())
	if len(f.OpenPaths()) != 0 {
		t.Fatalf("still open after the tool closed: %v", f.OpenPaths())
	}
}

// Without a server (or an answer) the words stay.
func TestOwnerLSPUnavailableKeepsWords(t *testing.T) {
	setup(t)
	installLSP(t) // Answer nil: ErrUnavailable
	h := uitest.New(t)
	_, d := openHub(t, h)
	h.Press("O", "s", "e")
	if labels, _, ok := d.ed.Completion(); !ok || !slices.Contains(labels, "seats") {
		t.Fatalf("menu = %v", labels)
	}
	if strings.Contains(h.Screen(), "lsp") {
		t.Fatalf("an unavailable server made noise:\n%s", h.Screen())
	}
}

// A share guest never opens a document in a language server, nor asks.
func TestGuestNeverUsesLSP(t *testing.T) {
	setup(t)
	f := installLSP(t)
	f.Answer = func(string, int, int) (lsp.List, error) { return lsp.List{Items: []lsp.Item{{Label: "x"}}}, nil }
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelEditor)
	r.s.guestOpen(h.Context(), hubTS)
	r.settle()
	h.Press("O", "s", "e", "esc")
	r.settle()
	if len(f.OpenPaths()) != 0 || len(f.Asked()) != 0 || len(f.Closed()) != 0 {
		t.Fatalf("guest used the server: open %v asked %v", f.OpenPaths(), f.Asked())
	}
}
