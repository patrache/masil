package codeassist

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// contextRows are lexed above the shown window, so a comment or string
// opened just above it is still seen (a longer one is drawn as code: the
// price of never lexing the whole file).
const contextRows = 60

// chromaNames are the chroma lexer names of the LeetCode languages.
var chromaNames = map[string]string{
	"go": "go", "python3": "python", "python": "python2", "c": "c", "cpp": "c++", "java": "java",
	"csharp": "c#", "javascript": "javascript", "typescript": "typescript", "php": "php",
	"swift": "swift", "kotlin": "kotlin", "rust": "rust", "ruby": "ruby", "scala": "scala",
	"dart": "dart", "racket": "racket", "erlang": "erlang", "elixir": "elixir",
}

// ChromaName is lang's chroma lexer name (lang itself when it has no entry).
func ChromaName(lang string) string {
	if n, ok := chromaNames[lang]; ok {
		return n
	}
	return lang
}

// Lexer is lang's chroma lexer (nil: none, the text stays plain).
func Lexer(lang string) chroma.Lexer {
	if lang == "" {
		return nil
	}
	l := lexers.Get(ChromaName(lang))
	if l == nil {
		return nil
	}
	return chroma.Coalesce(l)
}

// Span is one token of a line: rune columns [From, To) and its type.
type Span struct {
	From, To int
	Type     chroma.TokenType
}

// Highlighter lexes the shown window of a buffer and keeps the result until
// the window's text changes (an edit, a remote edit, a scroll).
type Highlighter struct {
	lexer chroma.Lexer
	start int    // first lexed row
	text  string // what was lexed
	rows  [][]Span
	lexes int // how many times it lexed (tests)
}

// NewHighlighter highlights lang (an unknown or empty one: no spans).
func NewHighlighter(lang string) *Highlighter { return &Highlighter{lexer: Lexer(lang)} }

// Window is the spans of rows [from, to) of a buffer of n rows whose row i
// is line(i): one slice per row (nil for a row with no tokens or without a
// lexer). Only those rows and contextRows above them are read and lexed.
func (h *Highlighter) Window(line func(int) []rune, n, from, to int) [][]Span {
	from, to = max(0, from), min(to, n)
	if h == nil || h.lexer == nil || from >= to {
		return make([][]Span, max(to-from, 0))
	}
	start := max(0, from-contextRows)
	var b strings.Builder
	for i := start; i < to; i++ {
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(string(line(i)))
	}
	text := b.String()
	if text != h.text || start != h.start || h.rows == nil {
		h.start, h.text, h.rows = start, text, lexRows(h.lexer, text, to-start)
		h.lexes++
	}
	return h.rows[from-start : to-start]
}

// lexRows tokenises text into the spans of each of its n rows.
func lexRows(l chroma.Lexer, text string, n int) [][]Span {
	rows := make([][]Span, n)
	it, err := l.Tokenise(nil, text+"\n")
	if err != nil {
		return rows
	}
	row, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				row, col = row+1, 0
			}
			if row >= n {
				return rows
			}
			if k := len([]rune(part)); k > 0 {
				rows[row] = append(rows[row], Span{From: col, To: col + k, Type: tok.Type})
				col += k
			}
		}
	}
	return rows
}
