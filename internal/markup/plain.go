package markup

import (
	"strings"

	"github.com/patrache/masil/internal/textwidth"
)

// Plain is a markdown-stripped one-line preview: markers removed, link labels
// kept, code blocks as codeSummary(lines) (the caller's language, e.g.
// common.code_summary; nil = "‹N›"), newlines as " ", sanitized, cut to
// maxCells with a trailing "…".
func Plain(body string, maxCells int, codeSummary func(lines int) string) string {
	if maxCells <= 0 {
		return ""
	}
	var b strings.Builder
	for i, bl := range Parse(body, ParseOptions{}).Blocks {
		if i > 0 {
			b.WriteByte(' ')
		}
		if bl.Kind == Code {
			b.WriteString(summarizeCode(bl.Code, codeSummary))
			continue
		}
		for _, in := range bl.Inline {
			b.WriteString(strings.ReplaceAll(in.Text, "\n", " "))
		}
	}
	s := textwidth.Sanitize(b.String())
	if textwidth.Width(s) <= maxCells {
		return s
	}
	return strings.TrimRight(textwidth.Truncate(s, maxCells-1, ""), " ") + "…"
}
