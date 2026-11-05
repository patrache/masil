package markup

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/patrache/masil/internal/theme"
)

const codeBar = "▎"

// Highlight renders chroma tokens with theme.CodeStyle: every line is the
// CodeBar "▎" plus code hard-wrapped at width-1 and padded with the CodeBlock
// background to exactly width cells. Unknown or empty lang uses lexers.Fallback.
func Highlight(lang, code string, t *theme.Theme, width int) []string {
	cells := highlightCells(lang, code, width)
	out := make([]string, len(cells))
	for i, l := range cells {
		out[i] = renderLine(l, t)
	}
	return out
}

func lexerFor(lang string) chroma.Lexer {
	var l chroma.Lexer
	if lang != "" {
		l = lexers.Get(lang)
	}
	if l == nil {
		l = lexers.Fallback
	}
	return chroma.Coalesce(l)
}

// tokenLines tokenises code and splits the tokens into exactly one segment
// list per source line (lexers may add a trailing newline).
func tokenLines(lang, code string) [][]seg {
	n := strings.Count(code, "\n") + 1
	toks, err := chroma.Tokenise(lexerFor(lang), nil, code)
	if err != nil {
		toks = []chroma.Token{{Type: chroma.Text, Value: code}}
	}
	lines := make([][]seg, 1, n)
	for _, tok := range toks {
		key := styleKey{kind: kToken, tt: tok.Type}
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				lines[len(lines)-1] = append(lines[len(lines)-1], seg{part, key})
			}
		}
	}
	for len(lines) < n {
		lines = append(lines, nil)
	}
	return lines[:n]
}

func highlightCells(lang, code string, width int) []line {
	if width <= 0 {
		return nil
	}
	cw := width - 1
	bar := cell{codeBar, 1, styleKey{kind: kCodeBar}}
	pad := cell{" ", 1, styleKey{kind: kCodeBlock}}
	finish := func(l line) line {
		out := append(line{bar}, l...)
		for used := l.width(); used < cw; used++ {
			out = append(out, pad)
		}
		return out
	}
	var out []line
	for _, src := range tokenLines(lang, code) {
		cells := cellsOf(src)
		var cur line
		used := 0
		for _, c := range cells {
			if c.w > cw { // cannot fit even alone
				if cw <= 0 {
					continue
				}
				c = cell{"…", 1, c.key}
			}
			if used+c.w > cw {
				out = append(out, finish(cur))
				cur, used = nil, 0
			}
			cur = append(cur, c)
			used += c.w
		}
		out = append(out, finish(cur))
	}
	return out
}
