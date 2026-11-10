package codeassist

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
)

// spanText is the text and token type of every span of one line.
func spanText(line string, spans []Span) []string {
	r := []rune(line)
	var out []string
	for _, s := range spans {
		out = append(out, fmt.Sprintf("%s:%s", string(r[s.From:s.To]), s.Type))
	}
	return out
}

func linesOf(text string) func(int) []rune {
	ls := strings.Split(text, "\n")
	return func(i int) []rune { return []rune(ls[i]) }
}

func typeOf(t *testing.T, line string, spans []Span, word string) chroma.TokenType {
	t.Helper()
	r := []rune(line)
	for _, s := range spans {
		if string(r[s.From:s.To]) == word {
			return s.Type
		}
	}
	t.Fatalf("no span %q in %q: %v", word, line, spanText(line, spans))
	return 0
}

func TestHighlightGo(t *testing.T) {
	src := "package main\n\n// add sums\nfunc add(a int) int {\n\treturn a + 1 // one\n}\nvar s = \"hi\""
	h := NewHighlighter("go")
	n := strings.Count(src, "\n") + 1
	got := h.Window(linesOf(src), n, 0, n)
	if len(got) != n {
		t.Fatalf("%d lines of spans, want %d", len(got), n)
	}
	lines := strings.Split(src, "\n")
	checks := []struct {
		row  int
		word string
		cat  chroma.TokenType
	}{
		{0, "package", chroma.Keyword},
		{2, "// add sums", chroma.Comment},
		{3, "func", chroma.Keyword},
		{3, "add", chroma.NameFunction},
		{3, "int", chroma.Keyword},
		{4, "return", chroma.Keyword},
		{4, "1", chroma.LiteralNumber},
		{4, "// one", chroma.Comment},
		{6, `"hi"`, chroma.LiteralString},
	}
	for _, c := range checks {
		tt := typeOf(t, lines[c.row], got[c.row], c.word)
		if !tt.InCategory(c.cat) && !tt.InSubCategory(c.cat) && tt != c.cat {
			t.Errorf("row %d %q = %s, want %s", c.row, c.word, tt, c.cat)
		}
	}
	// spans cover each line exactly, in order, with no gaps
	for i, l := range lines {
		at := 0
		for _, s := range got[i] {
			if s.From != at || s.To <= s.From {
				t.Fatalf("row %d spans %v leave a gap at %d", i, got[i], at)
			}
			at = s.To
		}
		if at != len([]rune(l)) {
			t.Fatalf("row %d spans end at %d of %d", i, at, len([]rune(l)))
		}
	}
}

func TestHighlightPython(t *testing.T) {
	src := "def f(x):\n    return \"s\"  # c\nclass A: pass"
	lines := strings.Split(src, "\n")
	got := NewHighlighter(FromLeet("python3")).Window(linesOf(src), 3, 0, 3)
	if tt := typeOf(t, lines[0], got[0], "def"); !tt.InCategory(chroma.Keyword) {
		t.Errorf("def = %s", tt)
	}
	if tt := typeOf(t, lines[0], got[0], "f"); !tt.InSubCategory(chroma.NameFunction) {
		t.Errorf("f = %s", tt)
	}
	if tt := typeOf(t, lines[1], got[1], "# c"); !tt.InCategory(chroma.Comment) {
		t.Errorf("comment = %s", tt)
	}
	found := false
	for _, s := range got[1] {
		if s.Type.InSubCategory(chroma.LiteralString) {
			found = true
		}
	}
	if !found {
		t.Errorf("no string span in %v", spanText(lines[1], got[1]))
	}
}

// The window is all that is lexed: rows outside it are not returned, and a
// window that did not change is not lexed again.
func TestHighlightWindowCached(t *testing.T) {
	var b strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&b, "x%d := %d // n\n", i, i)
	}
	src := b.String()
	lines := strings.Split(src, "\n")
	reads := 0
	line := func(i int) []rune { reads++; return []rune(lines[i]) }
	h := NewHighlighter("go")
	got := h.Window(line, len(lines), 10000, 10040)
	if len(got) != 40 {
		t.Fatalf("got %d rows", len(got))
	}
	if reads > 40+contextRows {
		t.Fatalf("read %d lines for a 40-row window", reads)
	}
	if tt := typeOf(t, lines[10000], got[0], "// n"); !tt.InCategory(chroma.Comment) {
		t.Fatalf("comment = %s", tt)
	}
	lexed := h.lexes
	h.Window(line, len(lines), 10000, 10040)
	if h.lexes != lexed {
		t.Fatal("an unchanged window was lexed again")
	}
	// one edited line: lexed again, fast
	lines[10005] = "y := \"s\""
	start := time.Now()
	got = h.Window(line, len(lines), 10000, 10040)
	if h.lexes != lexed+1 {
		t.Fatal("an edited window was not lexed again")
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("re-lexing a window took %v", d)
	}
	if tt := typeOf(t, lines[10005], got[5], `"s"`); !tt.InSubCategory(chroma.LiteralString) {
		t.Fatalf("edited line = %s", tt)
	}
}

func TestLanguages(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"main.go", "go"}, {"a/b/x.py", "python3"}, {"x.cpp", "cpp"}, {"x.hpp", "cpp"}, {"x.c", "c"},
		{"X.java", "java"}, {"x.cs", "csharp"}, {"x.js", "javascript"}, {"x.ts", "typescript"},
		{"x.php", "php"}, {"x.swift", "swift"}, {"x.kt", "kotlin"}, {"x.rs", "rust"}, {"x.rb", "ruby"},
		{"x.scala", "scala"}, {"README", ""},
	} {
		if got := FromFile(c.in); got != c.want {
			t.Errorf("FromFile(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if FromLeet("golang") != "go" || FromLeet("python") != "python" || FromLeet("cpp") != "cpp" {
		t.Fatal("FromLeet")
	}
	// every LeetCode language of the spec has a keyword table
	for _, l := range []string{"go", "python3", "python", "c", "cpp", "java", "csharp", "javascript",
		"typescript", "php", "swift", "kotlin", "rust", "ruby", "scala"} {
		ks := Keywords(l)
		if len(ks) < 10 {
			t.Errorf("%s: %d keywords", l, len(ks))
		}
		for _, k := range ks {
			if !IsWord(k) {
				t.Errorf("%s keyword %q is not an identifier", l, k)
			}
		}
	}
	if !slices.Contains(Keywords("python3"), "nonlocal") || slices.Contains(Keywords("python"), "nonlocal") {
		t.Fatal("python3 and python share a table")
	}
	// a file type without a table still highlights (markdown, yaml)
	if FromFile("notes.md") == "" {
		t.Fatal("markdown has no lexer")
	}
}

func TestCompleteRanking(t *testing.T) {
	buf := map[string]int{}
	CountWords([]rune("result := results + resolve(resolve(re)) + Retry\nreturned"), buf)
	got := Complete("go", "re", "re", buf)
	// prefix matches first (case-sensitive before case-insensitive), then
	// frequency, then shorter, then a-z; the word being typed is left out
	idx := func(s string) int { return slices.Index(got, s) }
	if idx("re") >= 0 {
		t.Fatalf("the word under the cursor is offered: %v", got)
	}
	if idx("resolve") != 0 {
		t.Fatalf("most frequent first: %v", got)
	}
	if idx("Retry") < idx("returned") || idx("Retry") < idx("return") {
		t.Fatalf("case-sensitive prefix matches before others: %v", got)
	}
	if idx("return") < 0 || idx("recover") < 0 || idx("real") < 0 {
		t.Fatalf("keywords and builtins missing: %v", got)
	}
	if idx("result") > idx("results") {
		t.Fatalf("same frequency: shorter first: %v", got)
	}
	for _, s := range got {
		if !strings.HasPrefix(strings.ToLower(s), "re") {
			t.Fatalf("%q does not match the prefix", s)
		}
	}
	// a word under the cursor that is longer than the prefix (cursor in a word)
	got = Complete("go", "res", "resolve", buf)
	if slices.Contains(got, "resolve") {
		t.Fatalf("current word offered: %v", got)
	}
	// several sources add up
	other := map[string]int{"results": 5}
	got = Complete("go", "res", "res", buf, other)
	if got[0] != "results" {
		t.Fatalf("other buffers count: %v", got)
	}
	// nothing matches
	if got := Complete("go", "zzq", "zzq", buf); len(got) != 0 {
		t.Fatalf("no match = %v", got)
	}
	// an unknown language still offers buffer words
	if got := Complete("", "Ret", "Ret", buf); !slices.Equal(got, []string{"Retry", "returned"}) {
		t.Fatalf("unknown language = %v", got)
	}
}

func TestWords(t *testing.T) {
	line := []rune("x := fooBar_1(ab")
	if p := Prefix(line, len(line)); p != "ab" {
		t.Fatalf("prefix = %q", p)
	}
	if p := Prefix(line, 8); p != "foo" {
		t.Fatalf("prefix mid word = %q", p)
	}
	if w := WordAt(line, 8); w != "fooBar_1" {
		t.Fatalf("word at = %q", w)
	}
	if p := Prefix([]rune("a 12"), 4); p != "" {
		t.Fatalf("a number is not a word prefix: %q", p)
	}
	m := map[string]int{}
	CountWords([]rune("한글 이름 x1 2x _y 이름"), m)
	if m["이름"] != 2 || m["x1"] != 1 || m["_y"] != 1 || m["2x"] != 0 {
		t.Fatalf("counts = %v", m)
	}
}

func TestIndexIncremental(t *testing.T) {
	var x Index
	x.Update([]string{"alpha beta", "gamma", "beta"})
	if c := x.Counts(); c["beta"] != 2 || c["gamma"] != 1 {
		t.Fatalf("counts = %v", c)
	}
	x.Update([]string{"alpha beta", "delta beta", "beta"})
	if c := x.Counts(); c["beta"] != 3 || c["gamma"] != 0 || c["delta"] != 1 {
		t.Fatalf("after edit = %v", c)
	}
	if _, ok := x.Counts()["gamma"]; ok {
		t.Fatal("a word with no occurrence stays in the index")
	}
	x.Update(nil)
	if len(x.Counts()) != 0 {
		t.Fatalf("emptied = %v", x.Counts())
	}
}

// Every LeetCode language lexes its keyword "return" as a keyword (PHP
// without "<?php", as LeetCode's snippets are).
func TestHighlightLeetLanguages(t *testing.T) {
	for _, l := range []string{"go", "python3", "python", "c", "cpp", "java", "csharp", "javascript",
		"typescript", "php", "swift", "kotlin", "rust", "ruby", "scala"} {
		src := "class Solution { return x }"
		if l == "go" {
			src = "func f() { return x }"
		}
		got := NewHighlighter(l).Window(linesOf(src), 1, 0, 1)
		if tt := typeOf(t, src, got[0], "return"); !tt.InCategory(chroma.Keyword) {
			t.Errorf("%s: return = %s", l, tt)
		}
	}
}
