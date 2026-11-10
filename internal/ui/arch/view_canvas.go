package arch

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// sty is a cell style of the pane, mapped to theme colours when drawn.
type sty uint8

const (
	sBase sty = iota
	sDim
	sBold
	sAcc
	sAccBold
	sSel // a selected row
	sWarn
	sErr
	sOK
	sLine
	sRemote // + n: the n-th collaborator's colour
)

type cell struct {
	ch string // "" = the right half of a double-width character
	st sty
}

// buf is the pane drawn cell by cell, then turned into styled lines.
type buf struct {
	w, h int
	c    []cell
}

func newBuf(w, h int) *buf {
	w, h = max(w, 0), max(h, 0)
	b := &buf{w: w, h: h, c: make([]cell, w*h)}
	for i := range b.c {
		b.c[i] = cell{ch: " "}
	}
	return b
}

func (b *buf) clear(x, y int) {
	i := y*b.w + x
	switch {
	case b.c[i].ch == "" && x > 0:
		b.c[i-1].ch = " "
		b.c[i].ch = " "
	case b.c[i].ch != "" && x+1 < b.w && b.c[i+1].ch == "":
		b.c[i+1].ch = " "
	}
}

func (b *buf) set(x, y int, ch string, w int, st sty) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	if w == 2 && x+1 >= b.w {
		ch, w = " ", 1
	}
	b.clear(x, y)
	b.c[y*b.w+x] = cell{ch: ch, st: st}
	if w == 2 {
		b.clear(x+1, y)
		b.c[y*b.w+x+1] = cell{ch: "", st: st}
	}
}

// text writes s (already one sanitized line) from (x, y) in at most maxW
// cells; it returns the cells used.
func (b *buf) text(x, y int, s string, maxW int, st sty) int {
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
			b.set(x+used, y, " ", 1, st)
			used++
			break
		}
		b.set(x+used, y, cl, w, st)
		used += w
	}
	return used
}

// line writes s truncated with "…" to the w cells from (x, y), padding with st.
func (b *buf) line(x, y, w int, s string, st sty) {
	s = textwidth.Truncate(s, w, "…")
	used := b.text(x, y, s, w, st)
	for i := used; i < w; i++ {
		b.set(x+i, y, " ", 1, st)
	}
}

// fill paints r with spaces of st.
func (b *buf) fill(r layout.Rect, st sty) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			b.set(x, y, " ", 1, st)
		}
	}
}

// blit copies a drawn design grid to (x0, y0).
func (b *buf) blit(g *archdraw.Grid, x0, y0 int) {
	for y := range g.H {
		for x := range g.W {
			c := g.Cells[y*g.W+x]
			if c.Ch == "" {
				continue // written with its left half
			}
			w := 1
			if x+1 < g.W && g.Cells[y*g.W+x+1].Ch == "" {
				w = 2
			}
			b.set(x0+x, y0+y, c.Ch, w, drawStyle(c.Style))
		}
	}
}

// drawStyle maps an archdraw cell style to a pane style.
func drawStyle(s archdraw.Style) sty {
	switch s {
	case archdraw.StyleBoxBorder, archdraw.StyleGhost:
		return sDim
	case archdraw.StyleEdge:
		return sLine
	case archdraw.StyleBar:
		return sOK
	case archdraw.StyleWarn:
		return sWarn
	case archdraw.StyleDanger:
		return sErr
	case archdraw.StyleSelected, archdraw.StyleEdgeSelected:
		return sAccBold
	case archdraw.StyleHandle:
		return sAcc
	case archdraw.StyleLabel:
		return sBold
	}
	if s >= archdraw.StyleRemoteBase {
		return sRemote + sty(s-archdraw.StyleRemoteBase)%3
	}
	return sBase
}

// lipStyle is a pane style in theme th.
func lipStyle(th *theme.Theme, s sty) lipgloss.Style {
	base := th.S.Base
	fg := func(c color.Color) lipgloss.Style { return base.Foreground(c) }
	switch s {
	case sDim:
		return fg(th.P.Dim)
	case sBold:
		return base.Bold(true)
	case sAcc:
		return fg(th.P.Acc)
	case sAccBold:
		return fg(th.P.Acc).Bold(true)
	case sSel:
		return th.S.Selected
	case sWarn:
		return th.S.Warning
	case sErr:
		return th.S.Error
	case sOK:
		return th.S.Success
	case sLine:
		return fg(th.P.Line)
	case sRemote:
		return fg(th.P.Acc2)
	case sRemote + 1:
		return fg(th.P.Acc3)
	case sRemote + 2:
		return fg(th.P.Acc4)
	}
	return base
}

// render turns the cells into exactly h styled lines of w cells.
func (b *buf) render(th *theme.Theme) string {
	styles := map[sty]lipgloss.Style{}
	out := make([]string, b.h)
	var run strings.Builder
	for y := range b.h {
		var line strings.Builder
		row := b.c[y*b.w : (y+1)*b.w]
		cur := sty(255)
		flush := func() {
			if run.Len() == 0 {
				return
			}
			st, ok := styles[cur]
			if !ok {
				st = lipStyle(th, cur)
				styles[cur] = st
			}
			line.WriteString(st.Render(run.String()))
			run.Reset()
		}
		for _, c := range row {
			if c.ch == "" {
				continue
			}
			if c.st != cur {
				flush()
				cur = c.st
			}
			run.WriteString(c.ch)
		}
		flush()
		out[y] = line.String()
	}
	return strings.Join(out, "\n")
}

func (m *canvasModel) View(ctx *ui.Context, focused bool) string {
	w, h := m.size.W, m.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	b := newBuf(w, h)
	if m.design == nil {
		return b.render(ctx.Theme)
	}
	r := m.regs
	switch r.View {
	case viewSummary:
		m.drawSummary(ctx, b, r.Canvas)
		return b.render(ctx.Theme)
	case viewList:
		if m.mode == modeComments && r.Panel.W <= 2 {
			b.fill(r.Canvas, sBase)
			m.drawComments(ctx, b, r.Canvas, r.Canvas.X+1, r.Canvas.W-2)
		} else if m.mode == modePanel {
			b.fill(r.Canvas, sBase)
			m.drawForm(ctx, b, r.Canvas, r.Canvas.X+1, r.Canvas.W-2)
		} else {
			m.drawList(ctx, b, r.Canvas, focused)
		}
	default:
		m.drawCanvas(ctx, b, r.Canvas)
		if m.mode == modeComments && r.Panel.W <= 2 {
			// no room for the panel drawer: the comments take the canvas
			b.fill(r.Canvas, sBase)
			m.drawComments(ctx, b, r.Canvas, r.Canvas.X+1, r.Canvas.W-2)
		}
	}
	if m.mode == modeHelp {
		m.drawHelp(ctx, b, r.Canvas)
	}
	m.drawFind(b, r.Canvas)
	m.drawHeader(ctx, b, r.Header)
	m.drawPalette(ctx, b, r.Palette)
	m.drawPanel(ctx, b, r.Panel)
	m.drawMetrics(ctx, b, r.Metrics)
	m.drawPaletteGhost(ctx, b)
	m.drawMenu(ctx, b)
	m.drawFaults(ctx, b)
	return b.render(ctx.Theme)
}

// overlay is what the canvas draws on top of the design: the selection, the
// move ghost (m) and the connect line (c).
func (m *canvasModel) overlay() archdraw.Overlay {
	ov := archdraw.Overlay{Badges: m.shownBadges(), Buttons: true, Selected: map[string]bool{}, RouteLimit: archdraw.FrameRouteLimit,
		Comments: m.commentCounts()}
	for _, id := range m.selection() {
		ov.Selected[id] = true
	}
	switch m.mode {
	case modeConnect:
		if n := m.node(m.cursor); n != nil && m.cursor != m.sel {
			ov.Selected[m.cursor] = true
			ov.ConnectFrom = m.sel
			ov.ConnectTo = [2]int{n.Pos[0] + archdoc.NodeW/2, n.Pos[1] + archdoc.NodeH/2}
		}
	case modeMove:
		if m.moveDX != 0 || m.moveDY != 0 {
			if n := m.node(m.sel); n != nil {
				x, y := n.Pos[0]+m.moveDX, n.Pos[1]+m.moveDY
				ov.Ghost = &archdraw.Ghost{ID: n.ID, X: x, Y: y, IntoBox: m.boxUnder(n.ID, x, y)}
			} else if bx := m.box(m.sel); bx != nil {
				ov.Ghost = &archdraw.Ghost{ID: bx.ID, X: bx.Rect[0] + m.moveDX, Y: bx.Rect[1] + m.moveDY}
			}
		}
	}
	m.gestureOverlay(&ov)
	return ov
}

// boxUnder is the box node id would land in at (x, y) ("" = none).
func (m *canvasModel) boxUnder(id string, x, y int) string {
	d := *m.design
	d.Nodes = nil
	for _, n := range m.design.Nodes {
		if n.ID != id {
			d.Nodes = append(d.Nodes, n)
		}
	}
	return boxAround(&d, x, y)
}

// visibleScene is the scene to draw at the current zoom: nodes that overlap
// there are left out (the canvas says "zoom in" instead of drawing them).
func (m *canvasModel) visibleScene() (*archdraw.Scene, bool) {
	z := m.vp.Zoom
	over := m.scene.Overlaps(z)
	if len(over) == 0 {
		return m.scene, false
	}
	if sc, ok := m.hidden[z]; ok {
		return sc, true
	}
	hide := map[string]bool{}
	for _, id := range over {
		hide[id] = true
	}
	d := *m.design
	d.Nodes = nil
	for _, n := range m.design.Nodes {
		if !hide[n.ID] {
			d.Nodes = append(d.Nodes, n)
		}
	}
	sc := archdraw.BuildFrom(m.oldHide[z], &d)
	m.hidden[z] = sc
	return sc, true
}

// drawCanvas draws the design in r (or the empty-design hints).
func (m *canvasModel) drawCanvas(ctx *ui.Context, b *buf, r layout.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	t := ctx.T.Archcanvas
	v := m.vp
	v.W, v.H = r.W, r.H
	sc, overlapped := m.visibleScene()
	ov := m.overlay()
	ov.Remote = m.remoteMarks(ctx)
	b.blit(sc.Render(v, ov), r.X, r.Y)
	m.drawRemoteTags(ctx, b, ov.Remote)
	if len(m.design.Nodes) == 0 && len(m.design.Boxes) == 0 {
		mid := r.Y + r.H/2 - 1
		for i, s := range []string{t.EmptyCanvas(), t.TrafficHint()} {
			s = textwidth.Truncate(s, r.W, "…")
			b.text(r.X+(r.W-textwidth.Width(s))/2, mid+i, s, r.W, sDim)
		}
	}
	if overlapped {
		s := " ⚠ " + t.ZoomOverlap() + " "
		b.text(r.X, r.Y, textwidth.Truncate(s, max(r.W-5, 0), "…"), max(r.W-5, 0), sWarn)
	}
}

// kindName is a node kind's display name.
func kindName(t *i18n.T, k archdoc.NodeKind) string {
	a := t.Archcanvas
	switch k {
	case archdoc.KindClient:
		return a.KindClient()
	case archdoc.KindCDN:
		return a.KindCdn()
	case archdoc.KindLB:
		return a.KindLb()
	case archdoc.KindAPI:
		return a.KindApi()
	case archdoc.KindWorker:
		return a.KindWorker()
	case archdoc.KindCache:
		return a.KindCache()
	case archdoc.KindDBPrimary:
		return a.KindDbPrimary()
	case archdoc.KindDBReplica:
		return a.KindDbReplica()
	case archdoc.KindQueue:
		return a.KindQueue()
	case archdoc.KindObjectStorage:
		return a.KindObjectStorage()
	case archdoc.KindSearch:
		return a.KindSearch()
	case archdoc.KindExternal:
		return a.KindExternal()
	}
	return cleanText(string(k))
}

// boxKindName is a box kind's display name.
func boxKindName(t *i18n.T, k archdoc.BoxKind) string {
	switch k {
	case archdoc.BoxRegion:
		return t.Archcanvas.BoxRegion()
	case archdoc.BoxVPC:
		return t.Archcanvas.BoxVpc()
	case archdoc.BoxAZ:
		return t.Archcanvas.BoxAz()
	}
	return cleanText(string(k))
}
