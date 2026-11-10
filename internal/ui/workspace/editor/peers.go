package editor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/textwidth"
)

const tabWidth = 4

// cell is one drawn grapheme of a line.
type cell struct {
	col  int    // rune column of its first rune
	n    int    // runes in it
	x    int    // cell column where it starts
	w    int    // cells it takes
	text string // what is drawn (sanitized, tabs expanded)
}

// layoutLine cuts a line into drawn graphemes. Text from a file is drawn
// sanitized grapheme by grapheme, so escapes cannot reach the terminal and a
// rune column always maps to one cell column.
func layoutLine(line []rune) []cell {
	var out []cell
	s := string(line)
	col, x := 0, 0
	for len(s) > 0 {
		g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			break
		}
		s = s[len(g):]
		n := len([]rune(g))
		var text string
		var w int
		if g == "\t" {
			w = tabWidth - x%tabWidth
			text = strings.Repeat(" ", w)
		} else {
			text = textwidth.Sanitize(g)
			w = textwidth.Width(text)
			if w == 0 || strings.ContainsAny(text, "\n\r") {
				text, w = "□", 1
			}
		}
		out = append(out, cell{col: col, n: n, x: x, w: w, text: text})
		col += n
		x += w
	}
	return out
}

// clusterStarts lists the rune columns where graphemes start.
func clusterStarts(line []rune) []int {
	cells := layoutLine(line)
	out := make([]int, len(cells))
	for i, c := range cells {
		out[i] = c.col
	}
	return out
}

func isStart(starts []int, col int) bool {
	for _, s := range starts {
		if s == col {
			return true
		}
	}
	return false
}

// around returns the cluster start at or before col and the next one after
// it (len of the line when none).
func around(starts []int, col int) (prev, next int) {
	next = -1
	for _, s := range starts {
		if s <= col {
			prev = s
		} else if next < 0 {
			next = s
		}
	}
	if next < 0 {
		next = col + 1
	}
	return prev, next
}

// cellX is the cell column of rune column col.
func cellX(cells []cell, col int) int {
	x := 0
	for _, c := range cells {
		if c.col+c.n > col {
			return c.x
		}
		x = c.x + c.w
	}
	return x
}

func (e *Editor) gutterWidth() int {
	n := e.core.GetBuffer().LineCount()
	gw := len(fmt.Sprint(n)) + 1
	if gw < 4 {
		gw = 4
	}
	if e.w < gw+8 {
		return 0
	}
	return gw
}

func (e *Editor) textWidth() int { return max(e.w-e.gutterWidth(), 1) }

// scrollColumn keeps the cursor inside the shown columns.
func (e *Editor) scrollColumn() {
	tw := e.textWidth()
	b := e.core.GetBuffer()
	pos := b.GetCursor().Position
	x := cellX(layoutLine(b.GetLineRunes(pos.Row)), pos.Col)
	if x < e.left {
		e.left = x
	}
	if x >= e.left+tw-1 {
		e.left = x - tw + 2
	}
	if e.left < 0 {
		e.left = 0
	}
}

// statusRow is the bottom row's text while the command or search line is
// open ("" otherwise).
func (e *Editor) statusRow() string {
	st := e.core.GetState()
	if e.core.IsCommandMode() || e.core.IsSearchMode() {
		if st.CommandLine != "" {
			return st.CommandLine
		}
	}
	return ""
}

// View draws exactly h lines of w cells.
func (e *Editor) View(focused bool) string {
	if e.w <= 0 || e.h <= 0 {
		return strings.TrimSuffix(strings.Repeat("\n", max(e.h, 1)), "\n")
	}
	th := e.th
	b := e.core.GetBuffer()
	st := e.core.GetState()
	gw := e.gutterWidth()
	tw := e.w - gw
	rows := e.h
	status := e.statusRow()
	if status != "" {
		rows--
	}
	base := lipgloss.NewStyle().Foreground(th.P.FG)
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	sel := lipgloss.NewStyle().Background(th.P.SelBG).Foreground(th.P.FG)
	cursorRow := b.GetCursor().Position.Row

	// peers by row
	type mark struct {
		col   int
		style lipgloss.Style
		name  string
	}
	peerAt := map[int][]mark{}
	type span struct {
		from, to core.Position
		style    lipgloss.Style
	}
	var spans []span
	for _, p := range e.peers {
		ps := lipgloss.NewStyle().Background(p.Color).Foreground(th.P.BG)
		pos := positionOf(e.text, p.Cursor)
		peerAt[pos.Row] = append(peerAt[pos.Row], mark{col: pos.Col, style: ps, name: textwidth.Sanitize(p.Name)})
		if p.SelTo > p.SelFrom {
			spans = append(spans, span{positionOf(e.text, p.SelFrom), positionOf(e.text, p.SelTo),
				lipgloss.NewStyle().Background(th.P.Chip).Foreground(th.P.FG)})
		}
	}
	inSpan := func(row, col int) (lipgloss.Style, bool) {
		for _, s := range spans {
			after := row > s.from.Row || (row == s.from.Row && col >= s.from.Col)
			before := row < s.to.Row || (row == s.to.Row && col < s.to.Col)
			if after && before {
				return s.style, true
			}
		}
		return lipgloss.Style{}, false
	}

	tokens := e.spans(st.TopLine, rows)
	out := make([]string, 0, e.h)
	for i := 0; i < rows; i++ {
		row := st.TopLine + i
		var sb strings.Builder
		if row >= b.LineCount() {
			if gw > 0 {
				sb.WriteString(dim.Render(textwidth.PadLeft("~", gw-1) + " "))
			}
			sb.WriteString(strings.Repeat(" ", tw))
			out = append(out, sb.String())
			continue
		}
		if gw > 0 {
			num := fmt.Sprint(row + 1)
			style := dim
			if row == cursorRow && focused {
				style = lipgloss.NewStyle().Foreground(th.P.Acc)
			}
			sb.WriteString(style.Render(textwidth.PadLeft(num, gw-1) + " "))
		}
		cells := layoutLine(b.GetLineRunes(row))
		marks := peerAt[row]
		var rowSpans []codeassist.Span
		if i < len(tokens) {
			rowSpans = tokens[i]
		}
		si := 0
		used := 0
		for _, c := range cells {
			if c.x < e.left {
				if c.x+c.w > e.left { // a wide char cut by the left edge
					pad := c.x + c.w - e.left
					sb.WriteString(strings.Repeat(" ", pad))
					used += pad
				}
				continue
			}
			if used+c.w > tw {
				break
			}
			style := base
			if tt, ok := spanAt(rowSpans, &si, c.col); ok {
				style = e.tokenStyle(tt)
			}
			if s, ok := inSpan(row, c.col); ok {
				style = s
			}
			if e.core.GetSelectionStatus(core.Position{Row: row, Col: c.col}) != core.SelectionNone {
				style = sel
			}
			for _, m := range marks {
				if m.col >= c.col && m.col < c.col+c.n {
					style = m.style
				}
			}
			sb.WriteString(style.Render(c.text))
			used += c.w
		}
		lineEnd := 0
		if len(cells) > 0 {
			last := cells[len(cells)-1]
			lineEnd = last.x + last.w
		}
		// a peer cursor at the end of the line, then the name tags
		for _, m := range marks {
			endX := cellX(cells, m.col)
			if endX >= lineEnd && endX-e.left >= used && endX-e.left < tw {
				gap := endX - e.left - used
				sb.WriteString(strings.Repeat(" ", gap) + m.style.Render(" "))
				used += gap + 1
			}
		}
		for _, m := range marks {
			tag := "▌" + m.name
			room := tw - used - 1
			if room < 2 {
				break
			}
			tag = textwidth.Truncate(tag, room, "…")
			sb.WriteString(" " + lipgloss.NewStyle().Foreground(m.style.GetBackground()).Render(tag))
			used += 1 + textwidth.Width(tag)
		}
		if used < tw {
			sb.WriteString(strings.Repeat(" ", tw-used))
		}
		out = append(out, sb.String())
	}
	e.drawMenu(out, rows)
	if status != "" {
		out = append(out, base.Render(textwidth.PadRight(textwidth.Sanitize(status), e.w)))
	}
	for i, l := range out {
		if w := textwidth.Width(l); w != e.w {
			out[i] = textwidth.PadRight(l, e.w)
		}
	}
	return strings.Join(out, "\n")
}

// Cursor is my cursor relative to the drawn area (nil when it is off screen).
func (e *Editor) Cursor() *tea.Cursor {
	b := e.core.GetBuffer()
	st := e.core.GetState()
	if status := e.statusRow(); status != "" {
		c := tea.NewCursor(min(textwidth.Width(textwidth.Sanitize(status)), e.w-1), e.h-1)
		c.Shape = tea.CursorBar
		return c
	}
	pos := b.GetCursor().Position
	y := pos.Row - st.TopLine
	if y < 0 || y >= e.h {
		return nil
	}
	x := e.gutterWidth() + cellX(layoutLine(b.GetLineRunes(pos.Row)), pos.Col) - e.left
	if x < 0 || x >= e.w {
		return nil
	}
	c := tea.NewCursor(x, y)
	if e.core.IsInsertMode() {
		c.Shape = tea.CursorBar
	}
	return c
}
