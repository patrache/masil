package codelsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/lsp"
)

// Items: SortText order (then FilterText), a short detail or the kind, and a
// text edit's UTF-16 range in rune columns when it spans the cursor.
func TestItems(t *testing.T) {
	line := []rune("한글 := fmt.Pr") // Hangul is one UTF-16 unit each
	col := len(line)
	l := lsp.List{Items: []lsp.Item{
		{Label: "Printf", InsertText: "Printf", SortText: "0002", Kind: lsp.KindFunction},
		{Label: "Println", InsertText: "Println", SortText: "0001", Detail: "func(a ...any) (n int, err error)\nmore",
			Edit: &lsp.TextEdit{Range: lsp.Range{Start: lsp.Position{Line: 4, Character: 10}, End: lsp.Position{Line: 4, Character: 12}}, NewText: "Println"}},
		{Label: "Errorf", InsertText: "Errorf", SortText: "0002", FilterText: "Aerrorf"},
		{Label: "\x1b[31mevil", InsertText: "evil\x07", SortText: "0003"},
		{Label: "far", InsertText: "far", SortText: "0004", Edit: &lsp.TextEdit{Range: lsp.Range{Start: lsp.Position{Line: 3}, End: lsp.Position{Line: 3, Character: 1}}, NewText: "far"}},
	}}
	got, cut := Items(l, line, 4, col)
	if cut {
		t.Fatal("five items were cut")
	}
	var labels []string
	for _, it := range got {
		labels = append(labels, it.Label)
	}
	want := []string{"Println", "Errorf", "Printf", "evil", "far"}
	if len(labels) != len(want) {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("labels = %q, want %q", labels, want)
		}
	}
	p := got[0]
	if !p.Server || !p.Edit || p.From != 10 || p.After != 0 || p.Text != "Println" {
		t.Fatalf("Println = %+v", p)
	}
	if from, to := p.Span(line, col); string(line[:from])+p.Text+string(line[to:]) != "한글 := fmt.Println" {
		t.Fatalf("applied = %q", string(line[:from])+p.Text+string(line[to:]))
	}
	if len([]rune(p.Detail)) > detailWidth || p.Detail == "" {
		t.Fatalf("detail = %q", p.Detail)
	}
	if got[2].Detail != "function" {
		t.Fatalf("kind detail = %q", got[2].Detail)
	}
	if got[3].Text != "evil" || got[3].Label != "evil" {
		t.Fatalf("escapes not dropped: %+v", got[3])
	}
	if got[4].Edit {
		t.Fatalf("an edit on another line kept its range: %+v", got[4])
	}
}

// Complete runs in a Cmd: the answer, an error, a timeout — and no
// Manager is no Cmd.
func TestComplete(t *testing.T) {
	if Complete(nil, Request{}) != nil {
		t.Fatal("a Cmd without a manager")
	}
	f := NewFake()
	if err := f.Open(testRoot, testMain, "package main\n\nfunc main() { fmt.Pr }"); err != nil {
		t.Fatal(err)
	}
	f.Answer = func(path string, line, col int) (lsp.List, error) {
		return lsp.List{Items: []lsp.Item{{Label: "Println", InsertText: "Println"}}, Incomplete: true}, nil
	}
	r := Request{Key: "k", Seq: 7, Path: testMain, Line: 2, Col: 19, LineText: []rune("func main() { fmt.Pr }")}
	m := Complete(f, r)().(ResultMsg)
	if m.Key != "k" || m.Seq != 7 || m.Err != nil || len(m.Items) != 1 || !m.Incomplete {
		t.Fatalf("result = %+v", m)
	}
	if a := f.Asked(); len(a) != 1 || a[0] != (FakeAsk{testMain, 2, 19}) {
		t.Fatalf("asked = %+v", a)
	}
	f.Answer = func(string, int, int) (lsp.List, error) { return lsp.List{}, lsp.ErrTimeout }
	if m := Complete(f, r)().(ResultMsg); !errors.Is(m.Err, lsp.ErrTimeout) || m.Items != nil {
		t.Fatalf("timeout = %+v", m)
	}
}

// A manager that ignores its context still ends at Timeout (the words stay).
func TestCompleteTimeout(t *testing.T) {
	start := time.Now()
	m := Complete(&slow{Fake: *NewFake()}, Request{Path: filepath.Join(testRoot, "a.go")})().(ResultMsg)
	if m.Err == nil {
		t.Fatalf("no error from a slow server: %+v", m)
	}
	if d := time.Since(start); d > Timeout+time.Second {
		t.Fatalf("took %v", d)
	}
}

type slow struct{ Fake }

func (*slow) Complete(ctx context.Context, _ string, _, _ int) (lsp.List, error) {
	<-ctx.Done()
	return lsp.List{Items: []lsp.Item{{Label: "late"}}}, nil
}

// In a test binary there is no server unless a test sets one.
func TestGetInTests(t *testing.T) {
	if Get() != nil {
		t.Fatal("a real manager inside a test binary")
	}
	f := NewFake()
	restore := SetManagerForTest(f)
	if Get() != f {
		t.Fatal("SetManagerForTest ignored")
	}
	restore()
	if Get() != nil {
		t.Fatal("not restored")
	}
}

func TestIsTrigger(t *testing.T) {
	if !IsTrigger([]string{".", ":"}, ".") || IsTrigger([]string{"."}, "x") || IsTrigger(nil, ".") || IsTrigger([]string{""}, "") {
		t.Fatal("IsTrigger")
	}
}

// A complete answer longer than the menu is cut — and a cut answer is
// incomplete, so the editors ask again as the word grows instead of
// filtering a list that lost what sorts after the cut.
func TestCompleteCutIsIncomplete(t *testing.T) {
	f := NewFake()
	if err := f.Open(testRoot, testMain, "package main\n\nfunc main() { os. }"); err != nil {
		t.Fatal(err)
	}
	var many []lsp.Item
	for i := range codeassist.MaxMenuItems + 50 {
		l := fmt.Sprintf("A%03d", i)
		many = append(many, lsp.Item{Label: l, InsertText: l})
	}
	many = append(many, lsp.Item{Label: "WriteFile", InsertText: "WriteFile"}) // sorts after the cut
	f.Answer = func(string, int, int) (lsp.List, error) { return lsp.List{Items: many}, nil }
	r := Request{Key: "k", Seq: 1, Path: testMain, Line: 2, Col: 17, LineText: []rune("func main() { os. }")}
	m := Complete(f, r)().(ResultMsg)
	if m.Err != nil || len(m.Items) != codeassist.MaxMenuItems {
		t.Fatalf("result: %d items, err %v", len(m.Items), m.Err)
	}
	if !m.Incomplete {
		t.Fatal("a cut answer was marked complete: WriteFile could never show")
	}

	// Exactly the menu's size is not cut: a complete answer stays complete.
	f.Answer = func(string, int, int) (lsp.List, error) { return lsp.List{Items: many[:codeassist.MaxMenuItems]}, nil }
	if m := Complete(f, r)().(ResultMsg); m.Incomplete || len(m.Items) != codeassist.MaxMenuItems {
		t.Fatalf("an uncut answer: %d items, incomplete %v", len(m.Items), m.Incomplete)
	}
}

// testRoot is an absolute folder on every OS (on Windows "/r" has no volume,
// so lsp.Manager refuses it as not local).
var testRoot = filepath.Join(os.TempDir(), "masil-codelsp-r")

var testMain = filepath.Join(testRoot, "main.go")
