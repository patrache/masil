package codeassist

import (
	"slices"
	"testing"
)

func labels(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

// v0.1.3-1: the menu shows the language server's items first, in the
// server's order, then the buffer words it does not already offer.
func TestMerge(t *testing.T) {
	server := []Item{{Label: "Println", Text: "Println", Server: true}, {Label: "Printf", Text: "Printf", Server: true},
		{Label: "Sprint(a ...any)", Text: "Sprint", Server: true}}
	got := Merge(server, []string{"Printf", "Print", "Sprint", "Prx"})
	if want := []string{"Println", "Printf", "Sprint(a ...any)", "Print", "Prx"}; !slices.Equal(labels(got), want) {
		t.Fatalf("merge = %q, want %q", labels(got), want)
	}
	if got[3].Server || got[3].Text != "Print" || got[3].Detail != "" {
		t.Fatalf("a word item = %+v", got[3])
	}
	if got := Merge(nil, []string{"ab", "ac"}); !slices.Equal(labels(got), []string{"ab", "ac"}) {
		t.Fatalf("words only = %q", labels(got))
	}
	many := make([]Item, 500)
	for i := range many {
		many[i] = Item{Label: string(rune('a'+i%26)) + "x", Server: true}
	}
	if n := len(Merge(many, []string{"zz"})); n != MaxMenuItems {
		t.Fatalf("merged %d items, want the cap %d", n, MaxMenuItems)
	}
}

func TestFilterPrefix(t *testing.T) {
	items := []Item{{Label: "Println"}, {Label: "printOne"}, {Label: "x", Filter: "PrintX"}, {Label: "Sprint"}}
	if got := labels(FilterPrefix(items, "pri")); !slices.Equal(got, []string{"Println", "printOne", "x"}) {
		t.Fatalf("filter = %q", got)
	}
	if got := FilterPrefix(items, ""); len(got) != len(items) {
		t.Fatalf("empty prefix dropped items: %q", labels(got))
	}
}

// Span: a server edit replaces its own range (From up to After past the
// cursor); anything else replaces the word under the cursor.
func TestItemSpan(t *testing.T) {
	line := []rune("x := fmt.Prin(v)")
	col := 13 // after "Prin"
	if from, to := (Item{Text: "Println"}).Span(line, col); from != 9 || to != 13 {
		t.Fatalf("word span = %d,%d", from, to)
	}
	if from, to := (Item{Text: "Println"}).Span([]rune("fmt.PrinXY z"), 8); from != 4 || to != 10 {
		t.Fatalf("word span with a tail = %d,%d", from, to)
	}
	if from, to := (Item{Text: ".Println", Edit: true, From: 8, After: 0}).Span(line, col); from != 8 || to != 13 {
		t.Fatalf("edit span = %d,%d", from, to)
	}
	if from, to := (Item{Text: "x", Edit: true, From: 20, After: 9}).Span(line, col); from != 13 || to != 16 {
		t.Fatalf("clamped edit span = %d,%d", from, to)
	}
	if from, to := (Item{Text: "x"}).Span([]rune("a 12"), 4); from != 4 || to != 4 {
		t.Fatalf("after a number: %d,%d (nothing replaced)", from, to)
	}
}
