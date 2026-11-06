package admincli

import (
	"strings"

	"github.com/patrache/masil/internal/textwidth"
)

// tableGap is the space between two columns.
const tableGap = "  "

// Table renders header and rows as textwidth-aligned columns: each column is
// as wide as its widest cell (header included), columns are separated by two
// spaces, the last column is not padded, no line has trailing spaces and every
// line ends with "\n". Cells are sanitized (idempotent for already clean text).
func Table(header []string, rows [][]string) string {
	all := make([][]string, 0, len(rows)+1)
	all = append(all, header)
	all = append(all, rows...)
	cols := 0
	for _, r := range all {
		cols = max(cols, len(r))
	}
	clean := make([][]string, len(all))
	widths := make([]int, cols)
	for i, r := range all {
		clean[i] = make([]string, cols)
		for j := range cols {
			if j < len(r) {
				clean[i][j] = textwidth.Sanitize(r[j])
			}
			widths[j] = max(widths[j], textwidth.Width(clean[i][j]))
		}
	}
	var b strings.Builder
	for _, r := range clean {
		var line strings.Builder
		for j, cell := range r {
			if j == cols-1 {
				line.WriteString(cell)
				break
			}
			line.WriteString(textwidth.PadRight(cell, widths[j]))
			line.WriteString(tableGap)
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
