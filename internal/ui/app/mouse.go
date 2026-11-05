package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// routeMouse is CA 5.3.4 step 3 (View.OnMouse is not used). A left click on the toast
// box is handled first; while an overlay is open nothing reaches the panes below (the
// overlay hit test and outside-click close arrive with Task 28). A click focuses the
// pane under the pointer (border cells included) and hands it to the component in
// inner-rect coordinates; the wheel goes to the pane under the pointer without moving
// the focus. Tab-bar clicks select that tab, a context-line click opens the palette.
// Other mouse messages (release, motion) are dropped.
func (m *Model) routeMouse(msg tea.MouseMsg) tea.Cmd {
	click, isClick := msg.(tea.MouseClickMsg)
	_, isWheel := msg.(tea.MouseWheelMsg)
	if !isClick && !isWheel {
		return nil
	}
	if isClick {
		if cmd, ok := m.clickToast(click); ok {
			return cmd
		}
	}
	if len(m.overlays) > 0 || !m.sized || m.lay.Mode == layout.ModeTooSmall {
		return nil
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
	return mh.HandleMouse(m.ctx, msg, ui.Point{X: mo.X - inner.X, Y: mo.Y - inner.Y})
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
