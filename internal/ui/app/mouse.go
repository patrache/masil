package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// routeMouse is CA 5.3.4 step 3 (View.OnMouse is not used). A left click on the toast
// box is handled first; while an overlay is open it gets the mouse and nothing reaches
// the panes below (overlayMouse: outside click = close). A click focuses the
// pane under the pointer (border cells included) and hands it to the component in
// inner-rect coordinates; the wheel goes to the pane under the pointer without moving
// the focus. Tab-bar clicks select that tab, a context-line click opens the palette.
// Motion and release go to the pane a left press grabbed (dragMouse) and are
// dropped otherwise.
func (m *Model) routeMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return m.dragMouse(msg)
	}
	click, isClick := msg.(tea.MouseClickMsg)
	_, isWheel := msg.(tea.MouseWheelMsg)
	if !isClick && !isWheel {
		return nil
	}
	if isClick && click.Button == tea.MouseLeft {
		m.grab = dragGrab{} // a new press ends a gesture whose release got lost
		defer m.afterPress()
	}
	if isClick {
		if cmd, ok := m.clickToast(click); ok {
			return cmd
		}
	}
	if !m.sized || m.lay.Mode == layout.ModeTooSmall {
		return nil
	}
	if len(m.overlays) > 0 {
		return m.overlayMouse(msg)
	}
	mo := msg.Mouse()
	p, ok := m.lay.At(mo.X, mo.Y)
	if !ok {
		return nil
	}
	switch p.Slot {
	case layout.SlotTabs:
		if isClick && mo.Button == tea.MouseLeft {
			return m.clickTab(mo.X - p.Rect.X)
		}
		return nil
	case layout.SlotContext:
		if isClick && mo.Button == tea.MouseLeft {
			return m.openPalette("")
		}
		return nil
	}
	if !isFocusable(p.Slot) {
		return nil
	}
	if isClick {
		m.setFocus(p.Slot)
	}
	return m.paneMouse(p, msg)
}

// paneMouse hands msg to the MouseHandler in pane p with coordinates relative to its
// inner rect; events on the border (outside the inner rect) are not passed on.
func (m *Model) paneMouse(p layout.Pane, msg tea.MouseMsg) tea.Cmd {
	in := m.slotInstance(p.Slot)
	if in == nil {
		return nil
	}
	mh, ok := in.comp.(ui.MouseHandler)
	if !ok {
		return nil
	}
	inner := p.Rect
	if p.Bordered {
		inner = inner.Inner()
	}
	mo := msg.Mouse()
	if !inner.Contains(mo.X, mo.Y) {
		return nil
	}
	if dh, ok := in.comp.(ui.DragHandler); ok && mo.Button == tea.MouseLeft {
		if _, press := msg.(tea.MouseClickMsg); press {
			m.grab = dragGrab{slot: p.Slot, h: dh}
		}
	}
	return mh.HandleMouse(m.ctx, msg, ui.Point{X: mo.X - inner.X, Y: mo.Y - inner.Y})
}

// dragGrab is the pane a left press went to while its button is held (h nil =
// none): it gets every motion and the release (CA 5.3.4 drag).
type dragGrab struct {
	slot layout.SlotID
	h    ui.DragHandler
}

// afterPress runs after a left press was routed: the component that owns the
// last drag selection is told to clear it when this press went anywhere else,
// and the grabbing component (if any) becomes the owner.
func (m *Model) afterPress() {
	if m.dragOwner != nil && m.dragOwner != m.grab.h {
		m.dragOwner.ClearDrag()
	}
	m.dragOwner = m.grab.h
}

// dragMouse hands a motion or the release to the grabbing pane in its inner
// coordinates, unclipped (the pointer may have left the pane: the component
// clamps and scrolls). The release ends the grab. A gesture cut short (an
// overlay opened, the pane left the layout or shows another component, the
// terminal got too small) ends without it, and the component clears its drag.
func (m *Model) dragMouse(msg tea.MouseMsg) tea.Cmd {
	g := m.grab
	if g.h == nil {
		return nil
	}
	if _, release := msg.(tea.MouseReleaseMsg); release {
		m.grab = dragGrab{}
	}
	p, placed := m.lay.Pane(g.slot)
	in := m.slotInstance(g.slot)
	if len(m.overlays) > 0 || !m.sized || m.lay.Mode == layout.ModeTooSmall || !placed || in == nil || any(in.comp) != any(g.h) {
		m.grab = dragGrab{}
		g.h.ClearDrag()
		if m.dragOwner == g.h {
			m.dragOwner = nil
		}
		return nil
	}
	inner := p.Rect
	if p.Bordered {
		inner = inner.Inner()
	}
	mo := msg.Mouse()
	return g.h.HandleDrag(m.ctx, msg, ui.Point{X: mo.X - inner.X, Y: mo.Y - inner.Y})
}

// clickTab selects the tab drawn at column x of the tab bar (widgets.Tabs.Hit on the
// last drawn bar); the focus intent follows what Main shows now.
func (m *Model) clickTab(x int) tea.Cmd {
	i, ok := m.tabs.Hit(x)
	if !ok {
		return nil
	}
	m.nav = m.nav.selectTab(i, m.lay.Mode)
	cmd := m.relayout()
	m.followMain()
	return cmd
}
