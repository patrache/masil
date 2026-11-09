package app

import (
	"log/slog"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Overlay stack, bounds clamping, cursor and paste routing (CA 5.3.7).
//
// The app owns border and size: r = Bounds(body) ∩ body; in tiny mode, or when r is
// narrower than overlayMinW or shorter than overlayMinH, r = body without a border,
// else the theme border (S.OverlayBorder, Titled title on the top edge) is drawn around
// r and the component gets r.Inner(). Overlays are composited at Z(10+i); TooSmall
// draws none (they stay open).

// Overlay degrade thresholds (CA 5.3.7).
const (
	overlayMinW = 24
	overlayMinH = 6
	overlayZ    = 10
)

// overlayGeom is the final rect of o for the current frame and whether it is bordered;
// ok is false when no overlay is drawn (not sized, TooSmall).
func (m *Model) overlayGeom(o ui.Overlay) (r layout.Rect, bordered, ok bool) {
	if !m.sized || m.lay.Mode == layout.ModeTooSmall || m.cols <= 0 || m.rows <= 0 {
		return layout.Rect{}, false, false
	}
	body := m.body()
	r = intersect(o.Bounds(body), body)
	if m.lay.Mode == layout.ModeTiny || r.W < overlayMinW || r.H < overlayMinH {
		return body, false, true
	}
	return r, true, true
}

// overlayInner is the rect the overlay component draws in.
func overlayInner(r layout.Rect, bordered bool) layout.Rect {
	if bordered {
		return r.Inner()
	}
	return r
}

// intersect is a ∩ b (zero size when they do not overlap).
func intersect(a, b layout.Rect) layout.Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return layout.Rect{X: x0, Y: y0}
	}
	return layout.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// syncOverlays resizes every open overlay whose inner size changed this frame.
func (m *Model) syncOverlays() {
	for i, o := range m.overlays {
		r, b, ok := m.overlayGeom(o)
		if !ok {
			continue
		}
		in := overlayInner(r, b)
		size := ui.Size{W: in.W, H: in.H}
		if !m.ovSized[i] || m.ovSizes[i] != size {
			m.ovSizes[i], m.ovSized[i] = size, true
			o.Resize(m.ctx, size)
		}
	}
}

// pushOverlay opens o on top (Init + Resize); a full stack (maxOverlays) ignores it.
func (m *Model) pushOverlay(o ui.Overlay) (tea.Cmd, bool) {
	return m.pushOverlayID(o, "")
}

// pushOverlayID is pushOverlay remembering the id o was opened by (Box.ID).
func (m *Model) pushOverlayID(o ui.Overlay, id string) (tea.Cmd, bool) {
	if o == nil || len(m.overlays) >= maxOverlays {
		return nil, false
	}
	m.overlays = append(m.overlays, o)
	m.ovSizes = append(m.ovSizes, ui.Size{})
	m.ovSized = append(m.ovSized, false)
	m.ovIDs = append(m.ovIDs, id)
	m.prefix = prefixNone
	cmd := o.Init(m.ctx)
	m.syncOverlays()
	return cmd, true
}

// popOverlay closes the top overlay (forgetting the palette when it was that one).
func (m *Model) popOverlay() {
	n := len(m.overlays)
	if n == 0 {
		return
	}
	if m.overlays[n-1] == m.palette {
		m.palette = nil
	}
	m.overlays = m.overlays[:n-1]
	m.ovSizes = m.ovSizes[:n-1]
	m.ovSized = m.ovSized[:n-1]
	m.ovIDs = m.ovIDs[:n-1]
}

// topOverlay is the top overlay, or nil.
func (m *Model) topOverlay() ui.Overlay {
	if n := len(m.overlays); n > 0 {
		return m.overlays[n-1]
	}
	return nil
}

// openOverlay handles ui.OpenOverlayMsg: Overlay != nil is pushed, else the overlay
// registered under ID is built with Args (unknown ID => ignored + debug log).
func (m *Model) openOverlay(msg ui.OpenOverlayMsg) tea.Cmd {
	o := msg.Overlay
	if o == nil {
		f, ok := ui.Registered().Overlays[msg.ID]
		if !ok {
			if m.ctx.Debug {
				slog.Debug("unknown overlay", "id", msg.ID)
			}
			return nil
		}
		if o = f(m.ctx, msg.Args); o == nil {
			return nil
		}
	}
	cmd, _ := m.pushOverlayID(o, msg.ID)
	return cmd
}

// overlayKey is CA 5.3.4 step 2.1: the top overlay gets the key. Unhandled, esc
// closes it and ctrl+k (palette.open) closes it — opening the palette unless it was
// the palette; any other unhandled key stops at a modal overlay and goes on to steps
// 3–6 for a non-modal one.
func (m *Model) overlayKey(msg tea.KeyPressMsg) tea.Cmd {
	top := m.topOverlay()
	handled, cmd := m.handleKey(top, msg)
	if handled {
		return cmd
	}
	keys := m.ctx.Keys
	switch {
	case keys.Matches(msg, keymap.NavBack):
		m.popOverlay()
		return cmd
	case keys.Matches(msg, keymap.PaletteOpen):
		wasPalette := top == m.palette
		m.popOverlay()
		if wasPalette {
			return cmd
		}
		return tea.Batch(cmd, m.openPalette(""))
	case top.Modal():
		return cmd
	}
	return tea.Batch(cmd, m.globalKeys(msg))
}

// overlayMouse is CA 5.3.4 step 3 with an overlay open: a click outside the top
// overlay closes it; clicks and the wheel inside go to its MouseHandler in inner-rect
// coordinates (border cells are not passed on). Nothing reaches the panes below.
func (m *Model) overlayMouse(msg tea.MouseMsg) tea.Cmd {
	top := m.topOverlay()
	r, bordered, ok := m.overlayGeom(top)
	if !ok {
		return nil
	}
	mo := msg.Mouse()
	_, isClick := msg.(tea.MouseClickMsg)
	if !r.Contains(mo.X, mo.Y) {
		if isClick {
			m.popOverlay()
		}
		return nil
	}
	mh, isMH := top.(ui.MouseHandler)
	in := overlayInner(r, bordered)
	if !isMH || !in.Contains(mo.X, mo.Y) {
		return nil
	}
	return mh.HandleMouse(m.ctx, msg, ui.Point{X: mo.X - in.X, Y: mo.Y - in.Y})
}

// overlayCursor is the top overlay's Cursorer result plus its inner origin; nil when
// it has none (the cursor of the panes below is never shown under an overlay).
func (m *Model) overlayCursor() *tea.Cursor {
	top := m.topOverlay()
	r, bordered, ok := m.overlayGeom(top)
	if !ok {
		return nil
	}
	cr, isCr := top.(ui.Cursorer)
	if !isCr {
		return nil
	}
	c := cr.Cursor()
	if c == nil {
		return nil
	}
	in := overlayInner(r, bordered)
	out := *c
	out.X += in.X
	out.Y += in.Y
	return &out
}

// overlayLayers draws the open overlays (bottom -> top) as compositor layers at
// Z(10+i) and records their boxes in m.frame.Overlays.
func (m *Model) overlayLayers() []*lipgloss.Layer {
	m.syncOverlays()
	var layers []*lipgloss.Layer
	for i, o := range m.overlays {
		r, bordered, ok := m.overlayGeom(o)
		if !ok {
			continue
		}
		in := overlayInner(r, bordered)
		body := o.View(m.ctx, i == len(m.overlays)-1)
		var s string
		if bordered {
			s = m.overlayBox(o, r, body)
		} else {
			s = strings.Join(textwidth.Fit(strings.Split(body, "\n"), in.W, in.H), "\n")
		}
		layers = append(layers, lipgloss.NewLayer(s).X(r.X).Y(r.Y).Z(overlayZ+i))
		m.frame.Overlays = append(m.frame.Overlays, Box{Rect: r, Bordered: bordered, ID: m.overlayID(i)})
	}
	return layers
}

// overlayBox draws content in the overlay border (theme Border, S.OverlayBorder
// color) with the Titled title on the top edge after one Top cell.
func (m *Model) overlayBox(o ui.Overlay, r layout.Rect, content string) string {
	th := m.ctx.Theme
	box := widgets.Box(th, th.S.OverlayBorder, ui.Size{W: r.W, H: r.H}, content)
	t, ok := o.(ui.Titled)
	if !ok {
		return box
	}
	title, _ := t.Title(m.ctx)
	title = textwidth.Sanitize(title)
	tw := r.W - 6
	if title == "" || tw < 1 {
		return box
	}
	title = textwidth.Truncate(title, tw, "…")
	edge := th.S.Base
	if c := th.S.OverlayBorder.GetBorderTopForeground(); c != nil {
		edge = edge.Foreground(c)
	}
	b := th.Border
	seg := " " + title + " "
	fill := r.W - 3 - textwidth.Width(seg)
	top := edge.Render(b.TopLeft+b.Top) + th.S.Title.Render(seg) + edge.Render(strings.Repeat(b.Top, max(0, fill))+b.TopRight)
	lines := strings.Split(box, "\n")
	lines[0] = top
	return strings.Join(lines, "\n")
}

// overlayHints are the status hints while an overlay is open (CA 5.4): the top
// overlay's Helper bindings, else "esc 닫기".
func (m *Model) overlayHints() []string {
	var out []string
	if hp, ok := m.topOverlay().(ui.Helper); ok {
		for _, b := range hp.Help(m.ctx) {
			if len(out) >= maxHints {
				break
			}
			if hb := b.Help(); b.Enabled() && hb.Key != "" {
				out = append(out, hintItem{Key: hb.Key, Desc: hb.Desc}.String())
			}
		}
	}
	if len(out) == 0 {
		out = []string{hintItem{Key: "esc", Desc: "닫기"}.String()}
	}
	return out
}

// overlayID is overlays[i]'s Box.ID: its registered id, else its Go type
// without the pointer star ("palette.Model").
func (m *Model) overlayID(i int) string {
	if i < len(m.ovIDs) && m.ovIDs[i] != "" {
		return m.ovIDs[i]
	}
	return strings.TrimPrefix(reflect.TypeOf(m.overlays[i]).String(), "*")
}
