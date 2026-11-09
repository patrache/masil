package arch

import (
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// paletteItem is one row of the parts palette: a node kind or a box kind.
type paletteItem struct {
	node archdoc.NodeKind
	box  archdoc.BoxKind
}

func (p paletteItem) name(t *i18n.T) string {
	if p.box != "" {
		return boxKindName(t, p.box)
	}
	return kindName(t, p.node)
}

// paletteItems are the palette rows: every node kind, then every box kind.
func paletteItems() []paletteItem {
	var out []paletteItem
	for _, k := range archdoc.NodeKinds() {
		out = append(out, paletteItem{node: k})
	}
	for _, k := range archdoc.BoxKinds() {
		out = append(out, paletteItem{box: k})
	}
	return out
}

// paletteRows lays the palette out: row texts and, per row, the item index
// (-1 = a heading).
func paletteRows(t *i18n.T) (texts []string, items []int) {
	all := paletteItems()
	texts = append(texts, t.Archcanvas.PaletteParts())
	items = append(items, -1)
	for i, p := range all {
		if p.box != "" && (i == 0 || all[i-1].box == "") {
			texts = append(texts, "─ "+t.Archcanvas.PaletteBoxes()+" ─")
			items = append(items, -1)
		}
		texts = append(texts, "▸ "+p.name(t))
		items = append(items, i)
	}
	return texts, items
}

// drawPalette draws the parts palette in r (its right column is the divider).
// sel is the highlighted item (-1 = none).
func (m *canvasModel) drawPalette(ctx *ui.Context, b *buf, r layout.Rect) {
	sel := -1
	if m.mode == modePick {
		sel = m.pick
	}
	m.drawPaletteSel(ctx, b, r, sel)
}

func (m *canvasModel) drawPaletteSel(ctx *ui.Context, b *buf, r layout.Rect, sel int) {
	if r.W <= 1 || r.H <= 0 {
		return
	}
	b.fill(r, sBase)
	texts, items := paletteRows(ctx.T)
	inner := r.W - 2
	// keep the highlighted row visible
	off := 0
	for i, it := range items {
		if it == sel && sel >= 0 && i >= r.H {
			off = i - r.H + 1
		}
	}
	for row := 0; row < r.H; row++ {
		i := row + off
		if i >= len(texts) {
			break
		}
		st := sBase
		switch {
		case items[i] < 0:
			st = sDim
		case items[i] == sel:
			st = sSel
		}
		if row == r.H-1 && i < len(texts)-1 {
			b.line(r.X+1, r.Y+row, inner, "…", sDim)
			break
		}
		b.line(r.X+1, r.Y+row, inner, texts[i], st)
	}
	div := r.X + r.W - 1
	if r.X > 0 { // a drawer on the right: the divider is on its left
		div = r.X
	}
	for y := r.Y; y < r.Y+r.H; y++ {
		b.set(div, y, "│", 1, sLine)
	}
}
