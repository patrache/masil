package archdraw

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
)

// Style is what a cell shows; the UI maps it to theme colours.
type Style uint8

// Cell styles. StyleRemoteBase+n is the n-th remote collaborator's colour.
const (
	StyleNone Style = iota
	StyleNodeBorder
	StyleBoxBorder
	StyleLabel
	StyleBar
	StyleWarn
	StyleDanger
	StyleSelected
	StyleGhost
	StyleEdge
	StyleEdgeSelected
	StyleHandle
	StyleRemoteBase
)

// Cell is one screen cell. The right half of a double-width character has Ch == "".
type Cell struct {
	Ch    string
	Style Style
}

// Grid is a drawn screen: H rows of W cells.
type Grid struct {
	W, H  int
	Cells []Cell
}

func newGrid(w, h int) *Grid {
	w, h = max(w, 0), max(h, 0)
	g := &Grid{W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range g.Cells {
		g.Cells[i] = Cell{Ch: " "}
	}
	return g
}

// At returns the cell at (x, y) (a blank cell outside the grid).
func (g *Grid) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return Cell{Ch: " "}
	}
	return g.Cells[y*g.W+x]
}

// Plain returns the rows as text; every row is exactly W cells wide.
func (g *Grid) Plain() []string {
	out := make([]string, g.H)
	var b strings.Builder
	for y := range g.H {
		b.Reset()
		for _, c := range g.Cells[y*g.W : (y+1)*g.W] {
			b.WriteString(c.Ch)
		}
		out[y] = b.String()
	}
	return out
}

// set writes one grapheme of width 1 or 2 at (x, y), clipped, keeping
// double-width characters whole: a half that gets covered blanks its other half.
func (g *Grid) set(x, y int, ch string, w int, st Style) {
	if y < 0 || y >= g.H || x < 0 || x >= g.W {
		if w == 2 && x == -1 && y >= 0 && y < g.H && g.W > 0 { // right half visible
			g.clear(0, y)
			g.Cells[y*g.W] = Cell{Ch: " ", Style: st}
		}
		return
	}
	if w == 2 && x+1 >= g.W {
		ch, w = " ", 1
	}
	g.clear(x, y)
	if w == 2 {
		g.clear(x+1, y)
		g.Cells[y*g.W+x] = Cell{Ch: ch, Style: st}
		g.Cells[y*g.W+x+1] = Cell{Ch: "", Style: st}
		return
	}
	g.Cells[y*g.W+x] = Cell{Ch: ch, Style: st}
}

// clear blanks the double-width character (if any) that covers (x, y).
func (g *Grid) clear(x, y int) {
	i := y*g.W + x
	c := g.Cells[i]
	switch {
	case c.Ch == "" && x > 0: // right half: blank the left half
		g.Cells[i-1].Ch = " "
		g.Cells[i].Ch = " "
	case c.Ch != "" && x+1 < g.W && g.Cells[i+1].Ch == "": // left half: blank the right half
		g.Cells[i+1].Ch = " "
	}
}

// text writes s from (x, y), at most maxW cells, and returns the cells used.
func (g *Grid) text(x, y int, s string, maxW int, st Style) int {
	used := 0
	for s != "" && used < maxW {
		cl, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if cl == "" {
			break
		}
		s = s[len(cl):]
		if w <= 0 {
			continue
		}
		if used+w > maxW {
			g.set(x+used, y, " ", 1, st)
			used++
			break
		}
		g.set(x+used, y, cl, w, st)
		used += w
	}
	return used
}

// NodeBadge is what the analysis adds to a node: "×4" after the name, the
// utilisation bar, the p99 and the warning marks.
type NodeBadge struct {
	Text         string // "×4"
	BarPermille  int
	P99Ms        int
	Warn, Danger bool
}

// RemoteMark is one collaborator's selection and drag.
type RemoteMark struct {
	User     string
	Index    int // colour: StyleRemoteBase+Index
	Selected []string
	Ghost    *Ghost
}

// Ghost is a node (or box) being dragged: drawn at world (X, Y), IntoBox
// highlighted when dropping would put it into that box.
type Ghost struct {
	ID      string
	X, Y    int
	IntoBox string
}

// Overlay is everything drawn on top of the design.
type Overlay struct {
	Selected    map[string]bool // node, box and edge IDs
	Ghost       *Ghost
	Remote      []RemoteMark
	Badges      map[string]NodeBadge
	Comments    map[string]int // node ID → open comment count
	ConnectFrom string         // node a connection is being drawn from
	ConnectTo   [2]int         // world point the connection line ends at
	Buttons     bool           // draw the "＋－" zoom buttons at the top-right
}

const (
	glyphWarn    = "⚠"
	glyphDanger  = "✕"
	glyphHandle  = "◆"
	glyphComment = "✎"
	zoomIn       = "＋"
	zoomOut      = "－"
)

// Render draws the scene through viewport v. Nodes and boxes outside it are
// skipped; warnings and dangers show both a colour and a ⚠/✕ mark.
func (s *Scene) Render(v Viewport, ov Overlay) *Grid {
	g := newGrid(v.W, v.H)
	z := v.Zoom.valid()
	v.Zoom = z
	ox, oy := z.sx(v.X), z.sy(v.Y)
	screen := rect{ox, oy, g.W, g.H}
	into := ""
	if ov.Ghost != nil {
		into = ov.Ghost.IntoBox
	}
	brs := s.boxRects(z)
	for i := range s.boxes {
		b := &s.boxes[i]
		r := brs[i]
		if !r.intersects(screen) {
			continue
		}
		st := StyleBoxBorder
		if ov.Selected[b.id] || into == b.id {
			st = StyleSelected
		}
		drawBox(g, rect{r.x - ox, r.y - oy, r.w, r.h}, b.label, st, false)
	}
	s.drawEdges(g, z, ox, oy, ov)
	remote := map[string]*RemoteMark{}
	for i := range ov.Remote {
		for _, id := range ov.Remote[i].Selected {
			if _, ok := remote[id]; !ok {
				remote[id] = &ov.Remote[i]
			}
		}
	}
	for i := range s.nodes {
		n := &s.nodes[i]
		r := s.nodeRect(n, z)
		if !r.intersects(screen) {
			continue
		}
		dragged := ov.Ghost != nil && ov.Ghost.ID == n.id
		st := nodeStyle{border: StyleNodeBorder}
		badge, hasBadge := ov.Badges[n.id]
		switch {
		case dragged:
			st.border = StyleGhost
		case ov.Selected[n.id]:
			st.border = StyleSelected
		case remote[n.id] != nil:
			st.border = StyleRemoteBase + Style(remote[n.id].Index)
		case badge.Danger:
			st.border = StyleDanger
		case badge.Warn:
			st.border = StyleWarn
		}
		st.dim = dragged
		drawNode(g, rect{r.x - ox, r.y - oy, r.w, r.h}, z, n.label, badge, hasBadge, ov.Comments[n.id], st)
		if m := remote[n.id]; m != nil && r.y-oy > 0 {
			g.text(r.x-ox, r.y-oy-1, cleanLabel(m.User), r.w, StyleRemoteBase+Style(m.Index))
		}
	}
	ghost := func(gh *Ghost, st Style) {
		if gh == nil {
			return
		}
		if i, ok := s.nodeIdx[gh.ID]; ok {
			r := nodeRectAt(gh.X, gh.Y, z)
			drawDashed(g, rect{r.x - ox, r.y - oy, r.w, r.h}, s.nodes[i].label, st)
			return
		}
		if i, ok := s.boxIdx[gh.ID]; ok {
			b := s.boxes[i]
			b.x, b.y = gh.X, gh.Y
			r := s.boxRect(&b, z)
			drawDashed(g, rect{r.x - ox, r.y - oy, r.w, r.h}, b.label, st)
		}
	}
	for i := range ov.Remote {
		ghost(ov.Remote[i].Ghost, StyleRemoteBase+Style(ov.Remote[i].Index))
	}
	ghost(ov.Ghost, StyleSelected)
	if i, ok := s.nodeIdx[ov.ConnectFrom]; ok {
		r := s.nodeRect(&s.nodes[i], z)
		hx, hy := r.x+r.w-1-ox, r.y+r.h/2-oy
		tx, ty := z.sx(ov.ConnectTo[0])-ox, z.sy(ov.ConnectTo[1])-oy
		drawConnectLine(g, hx+1, hy, tx, ty)
		g.set(hx, hy, glyphHandle, 1, StyleHandle)
	}
	if ov.Buttons && g.W >= 4 && g.H >= 1 {
		g.set(g.W-4, 0, zoomIn, 2, StyleHandle)
		g.set(g.W-2, 0, zoomOut, 2, StyleHandle)
	}
	return g
}

// rounded box-drawing set (lipgloss RoundedBorder)
const (
	bTL, bTR, bBL, bBR, bH, bV = "╭", "╮", "╰", "╯", "─", "│"
	nTL, nTR, nBL, nBR, nH, nV = "┌", "┐", "└", "┘", "─", "│"
	dH, dV                     = "╌", "╎"
)

// labelSpan is where a box's label sits on its top border: from x0 (the space
// before it) for n cells; n == 0 when the box is too narrow for a label.
func labelSpan(r rect, label string) (x0, n int) {
	room := r.w - 4
	if room < 1 {
		return 0, 0
	}
	lw := min(textwidth.Width(textwidth.Truncate(label, room, "…")), room)
	return r.x + 1, lw + 2
}

// drawBox draws a region/VPC/AZ outline with its label on the top border.
func drawBox(g *Grid, r rect, label string, st Style, dashed bool) {
	if r.w < 2 || r.h < 1 {
		return
	}
	h, vv := bH, bV
	if dashed {
		h, vv = dH, dV
	}
	if r.h == 1 { // flattened at a small zoom: the label line only
		for x := r.x; x < r.x+r.w; x++ {
			g.set(x, r.y, h, 1, st)
		}
	} else {
		g.set(r.x, r.y, bTL, 1, st)
		g.set(r.x+r.w-1, r.y, bTR, 1, st)
		g.set(r.x, r.y+r.h-1, bBL, 1, st)
		g.set(r.x+r.w-1, r.y+r.h-1, bBR, 1, st)
		for x := r.x + 1; x < r.x+r.w-1; x++ {
			g.set(x, r.y, h, 1, st)
			g.set(x, r.y+r.h-1, h, 1, st)
		}
		for y := r.y + 1; y < r.y+r.h-1; y++ {
			g.set(r.x, y, vv, 1, st)
			g.set(r.x+r.w-1, y, vv, 1, st)
		}
	}
	if x0, n := labelSpan(r, label); n > 0 {
		g.set(x0, r.y, " ", 1, st)
		used := g.text(x0+1, r.y, textwidth.Truncate(label, n-2, "…"), n-2, labelStyle(st))
		g.set(x0+1+used, r.y, " ", 1, st)
	}
}

func labelStyle(border Style) Style {
	if border == StyleSelected || border >= StyleRemoteBase || border == StyleGhost {
		return border
	}
	return StyleLabel
}

// drawDashed draws a dragged item's ghost outline.
func drawDashed(g *Grid, r rect, label string, st Style) {
	if r.h <= 1 {
		for x := r.x; x < r.x+r.w; x++ {
			g.set(x, r.y, dH, 1, st)
		}
		g.text(r.x+1, r.y, textwidth.Truncate(label, max(r.w-2, 0), "…"), max(r.w-2, 0), st)
		return
	}
	drawBox(g, r, label, st, true)
	for y := r.y + 1; y < r.y+r.h-1; y++ { // a ghost hides what is under it
		for x := r.x + 1; x < r.x+r.w-1; x++ {
			g.set(x, y, " ", 1, StyleNone)
		}
	}
}

type nodeStyle struct {
	border Style
	dim    bool
}

func (st nodeStyle) part(p Style) Style {
	if st.dim {
		return StyleGhost
	}
	return p
}

// pct formats a permille as a whole percent ("81%", ">999%").
func pct(permille int) string {
	p := max(permille, 0) / 10
	if p > 999 {
		return ">999%"
	}
	return strconv.Itoa(p) + "%"
}

// bar is a w-cell utilisation bar in eighth blocks.
func bar(permille, w int) string {
	eighths := min(max(permille, 0), 1000) * w * 8 / 1000
	var b strings.Builder
	for range eighths / 8 {
		b.WriteString("█")
	}
	if r := eighths % 8; r > 0 {
		b.WriteString(string([]rune("▏▎▍▌▋▊▉")[r-1]))
	}
	return textwidth.PadRight(b.String(), w)
}

// FormatMs is a short latency: "140ms", "2.5s", "12s".
func FormatMs(ms int) string {
	switch {
	case ms < 0:
		return "0ms"
	case ms < 1000:
		return strconv.Itoa(ms) + "ms"
	case ms < 10000:
		return strconv.Itoa(ms/1000) + "." + strconv.Itoa(ms%1000/100) + "s"
	case ms < 1000000:
		return strconv.Itoa(ms/1000) + "s"
	}
	return ">999s"
}

// drawNode draws one node at screen rect r.
func drawNode(g *Grid, r rect, z Zoom, label string, b NodeBadge, hasBadge bool, comments int, st nodeStyle) {
	title := label + cleanLabel(b.Text)
	mark, markSt := "", StyleNone
	switch {
	case b.Danger:
		mark, markSt = glyphDanger, StyleDanger
	case b.Warn:
		mark, markSt = glyphWarn, StyleWarn
	}
	border := st.border
	if z == ZoomDot {
		inner := r.w - 2
		g.set(r.x, r.y, "[", 1, border)
		g.set(r.x+r.w-1, r.y, "]", 1, border)
		for x := r.x + 1; x < r.x+r.w-1; x++ {
			g.set(x, r.y, " ", 1, StyleNone)
		}
		x := r.x + 1
		if mark != "" {
			g.set(x, r.y, mark, 1, st.part(markSt))
			x++
			inner--
		}
		tail := ""
		if hasBadge {
			tail = " " + pct(b.BarPermille)
		}
		nameW := max(inner-textwidth.Width(tail), 1)
		used := g.text(x, r.y, textwidth.Truncate(title, nameW, "…"), nameW, st.part(StyleLabel))
		g.text(x+used, r.y, tail, inner-used, st.part(StyleBar))
		return
	}
	inner := r.w - 2
	// frame
	g.set(r.x, r.y, nTL, 1, border)
	g.set(r.x+r.w-1, r.y, nTR, 1, border)
	g.set(r.x, r.y+r.h-1, nBL, 1, border)
	g.set(r.x+r.w-1, r.y+r.h-1, nBR, 1, border)
	for x := r.x + 1; x < r.x+r.w-1; x++ {
		g.set(x, r.y, nH, 1, border)
		g.set(x, r.y+r.h-1, nH, 1, border)
	}
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		g.set(r.x, y, nV, 1, border)
		g.set(r.x+r.w-1, y, nV, 1, border)
		for x := r.x + 1; x < r.x+r.w-1; x++ {
			g.set(x, y, " ", 1, StyleNone)
		}
	}
	// name×n on the top border
	g.text(r.x+1, r.y, textwidth.Truncate(title, inner, "…"), inner, st.part(StyleLabel))
	if hasBadge {
		barW := 5
		if z == ZoomNormal {
			barW = 3
		}
		p := pct(b.BarPermille)
		barSt := StyleBar
		if b.Danger {
			barSt = StyleDanger
		}
		used := g.text(r.x+1, r.y+1, bar(b.BarPermille, barW), barW, st.part(barSt))
		g.text(r.x+1+used, r.y+1, textwidth.PadLeft(p, inner-used), inner-used, st.part(StyleLabel))
		if z == ZoomDetail {
			g.text(r.x+1, r.y+2, textwidth.Truncate("p99 "+FormatMs(b.P99Ms), inner, ""), inner, st.part(StyleLabel))
		}
	}
	// warning and comment marks on the bottom border
	if mark != "" {
		g.set(r.x+1, r.y+r.h-1, mark, 1, st.part(markSt))
	}
	if comments > 0 {
		c := glyphComment + strconv.Itoa(min(comments, 99))
		cw := textwidth.Width(c)
		if cw <= inner-2 {
			g.text(r.x+r.w-1-cw, r.y+r.h-1, c, cw, st.part(StyleLabel))
		}
	}
	// connection handle: the middle of the right border
	g.set(r.x+r.w-1, r.y+r.h/2, glyphHandle, 1, st.part(StyleHandle))
}

// drawConnectLine draws the line of a connection being made: across, then down.
func drawConnectLine(g *Grid, x0, y0, x1, y1 int) {
	step := func(a, b int) int {
		if b < a {
			return -1
		}
		return 1
	}
	x := x0
	for ; x != x1; x += step(x0, x1) {
		g.set(x, y0, dH, 1, StyleEdgeSelected)
	}
	for y := y0; y != y1; y += step(y0, y1) {
		if y != y0 {
			g.set(x1, y, dV, 1, StyleEdgeSelected)
		}
	}
	g.set(x1, y1, glyphHandle, 1, StyleEdgeSelected)
}
