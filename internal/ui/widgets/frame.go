// Package widgets holds shared drawing helpers and components (CA 5.7).
package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Frame draws a themed pane border with title/badge; content is fit to inner size.
//
// Top edge: corner + one Top cell + " "+title+" " + badge (+ " " when the badge does
// not end in a blank, e.g. the dot) + Top fill + corner. The title is sanitized and
// truncated to W-6 cells with "…"; the badge is dropped first when space runs out.
// Border color: PaneFocused (acc) when focused, else PaneBlurred (line).
func Frame(t *theme.Theme, title string, badge ui.Badge, focused bool, size ui.Size, content string) string {
	edge := t.S.PaneBlurred
	titleStyle := t.S.Dim
	if focused {
		edge = t.S.PaneFocused
		titleStyle = t.S.Title
	}
	return drawBox(t, t.Border, borderStyle(t, edge), size, content, func(avail int) string {
		return frameTitle(t, titleStyle, title, badge, size.W, avail)
	})
}

// Box draws a nested box (composer input). The border runes come from style when it
// sets one (else the theme border); the color is style's top border foreground.
func Box(t *theme.Theme, style lipgloss.Style, size ui.Size, content string) string {
	border := t.Border
	if b := style.GetBorderStyle(); b.Top != "" {
		border = b
	}
	return drawBox(t, border, borderStyle(t, style), size, content, nil)
}

// borderStyle is Base with style's border foreground (Base alone when unset).
func borderStyle(t *theme.Theme, style lipgloss.Style) lipgloss.Style {
	s := t.S.Base
	if c := style.GetBorderTopForeground(); c != nil {
		s = s.Foreground(c)
	}
	return s
}

// frameTitle renders the top-edge title segment within avail cells ("" when none fits).
func frameTitle(t *theme.Theme, st lipgloss.Style, title string, badge ui.Badge, w, avail int) string {
	title = textwidth.Sanitize(title)
	if title == "" && badge.Count <= 0 && !badge.Dot {
		return ""
	}
	tw := min(w-6, avail-2)
	if tw < 1 {
		return ""
	}
	title = textwidth.Truncate(title, tw, "…")
	seg := st.Render(" " + title + " ")
	segW := textwidth.Width(title) + 2
	if b := Badge(t, badge); b != "" {
		bw := textwidth.Width(b)
		trail := ""
		if !strings.HasSuffix(ansi.Strip(b), " ") {
			trail, bw = t.S.Base.Render(" "), bw+1
		}
		if segW+bw <= avail {
			seg += b + trail
			segW += bw
		}
	}
	if segW > avail {
		return ""
	}
	return seg
}

// drawBox draws a w x h border with runes b in edge style, the optional title segment
// on the top edge after one Top cell, and content fit to the inner (w-2) x (h-2) area
// with every cell Base styled. Sizes below 2 in either direction degrade to blank Base
// cells (never panic); zero/negative sizes yield "".
func drawBox(t *theme.Theme, b lipgloss.Border, edge lipgloss.Style, size ui.Size, content string, titleFn func(avail int) string) string {
	w, h := size.W, size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < 2 || h < 2 {
		return blankLines(t, w, h)
	}
	iw := w - 2
	top := edge.Render(b.TopLeft)
	fill := iw
	if titleFn != nil && iw >= 2 {
		if seg := titleFn(iw - 1); seg != "" {
			top += edge.Render(b.Top) + seg
			fill = iw - 1 - textwidth.Width(seg)
		}
	}
	top += edge.Render(strings.Repeat(b.Top, max(0, fill))) + edge.Render(b.TopRight)

	out := make([]string, 0, h)
	out = append(out, top)
	left, right := edge.Render(b.Left), edge.Render(b.Right)
	for _, l := range fitLines(t, content, iw, h-2) {
		out = append(out, left+l+right)
	}
	out = append(out, edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, iw)+b.BottomRight))
	return strings.Join(out, "\n")
}

// fitLines splits content into exactly h lines of exactly w cells. Each line is
// truncated (grapheme safe) and padded with Base styled blanks; an unstyled line is
// wrapped in Base so no cell is left without the theme colors.
func fitLines(t *theme.Theme, content string, w, h int) []string {
	if h <= 0 {
		return nil
	}
	src := strings.Split(content, "\n")
	out := make([]string, h)
	for i := range out {
		var l string
		if i < len(src) {
			l = src[i]
		}
		out[i] = fitLine(t, l, w)
	}
	return out
}

// fitLine makes one line exactly w cells (truncate + Base padding).
func fitLine(t *theme.Theme, l string, w int) string {
	if w <= 0 {
		return ""
	}
	// lipgloss expands tabs when rendering, so expand them before measuring.
	l = textwidth.Truncate(strings.ReplaceAll(l, "\t", "    "), w, "")
	if l != "" && !strings.Contains(l, "\x1b") {
		l = t.S.Base.Render(l)
	}
	return l + pad(t, w-textwidth.Width(l))
}

// pad returns n Base styled blanks ("" for n <= 0).
func pad(t *theme.Theme, n int) string {
	if n <= 0 {
		return ""
	}
	return t.S.Base.Render(strings.Repeat(" ", n))
}

// blankLines is h lines of w Base blanks.
func blankLines(t *theme.Theme, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	line := pad(t, w)
	out := make([]string, h)
	for i := range out {
		out[i] = line
	}
	return strings.Join(out, "\n")
}
