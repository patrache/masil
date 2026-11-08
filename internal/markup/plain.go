package markup

import (
	"strings"

	"github.com/patrache/masil/internal/textwidth"
)

// Plain is a markdown-stripped one-line preview: markers removed, link labels
// kept, code blocks as "‹코드 N줄›", newlines as " ", sanitized, cut to
// maxCells with a trailing "…".
func Plain(body string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	var b strings.Builder
	for i, bl := range Parse(body, ParseOptions{}).Blocks {
		if i > 0 {
			b.WriteByte(' ')
		}
		if bl.Kind == Code {
			b.WriteString(codeSummary(bl.Code))
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
