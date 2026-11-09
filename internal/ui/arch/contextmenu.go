package arch

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// menuAction is one entry of the right-click menu.
type menuAction int

const (
	menuSettings menuAction = iota // the settings panel (e)
	menuFault                      // the fault menu (P3 Task 19)
	menuDelete                     // d
	menuComment                    // the comments panel (P3 Task 25)
)

var menuActions = []menuAction{menuSettings, menuFault, menuDelete, menuComment}

func (a menuAction) label(t *i18n.T) string {
	switch a {
	case menuSettings:
		return t.Archcanvas.MenuSettings()
	case menuFault:
		return t.Archcanvas.MenuFault()
	case menuDelete:
		return t.Archcanvas.MenuDelete()
	}
	return t.Archcanvas.MenuComment()
}

// ctxMenu is the right-click menu of a node, box or edge, drawn over the
// canvas at pane cell (x, y).
type ctxMenu struct {
	target string
	x, y   int
	sel    int
}

// openMenu opens the menu of target at pane cell (x, y), selecting target.
func (m *canvasModel) openMenu(target string, x, y int) {
	m.cancelGesture()
	m.sel = target
	m.menu = &ctxMenu{target: target, x: x, y: y}
}

// menuRect is where the menu is drawn (clamped into the canvas).
func (m *canvasModel) menuRect(t *i18n.T) layout.Rect {
	w := 0
	for _, a := range menuActions {
		w = max(w, textwidth.Width(a.label(t)))
	}
	w += 4
	h := len(menuActions) + 2
	c := m.regs.Canvas
	x := min(m.menu.x, c.X+c.W-w)
	y := min(m.menu.y, c.Y+c.H-h)
	return layout.Rect{X: max(x, c.X), Y: max(y, c.Y), W: min(w, c.W), H: min(h, c.H)}
}

// runMenu runs menu action a on the menu's target and closes the menu.
func (m *canvasModel) runMenu(ctx *ui.Context, a menuAction) tea.Cmd {
	target := m.menu.target
	m.menu = nil
	switch a {
	case menuSettings:
		m.sel = target
		m.openPanel()
	case menuDelete:
		if ro, cmd := m.readOnly(ctx); ro {
			return cmd
		}
		return m.remove(ctx, []string{target})
	}
	return nil // fault (Task 19) and comment (Task 25) arrive later
}

// menuKey: j/k move, enter runs, esc closes; the rest is swallowed.
func (m *canvasModel) menuKey(ctx *ui.Context, s string) (bool, tea.Cmd) {
	switch s {
	case "j", "down":
		m.menu.sel = min(m.menu.sel+1, len(menuActions)-1)
	case "k", "up":
		m.menu.sel = max(m.menu.sel-1, 0)
	case "enter":
		return true, m.runMenu(ctx, menuActions[m.menu.sel])
	case "esc":
		m.menu = nil
	default:
		return !reservedGlobal(s), nil
	}
	return true, nil
}

// menuClick handles a left click while the menu is open: an entry runs, any
// other place closes the menu (the click goes no further).
func (m *canvasModel) menuClick(ctx *ui.Context, p ui.Point) tea.Cmd {
	r := m.menuRect(ctx.T)
	if p.X > r.X && p.X < r.X+r.W-1 && p.Y > r.Y && p.Y < r.Y+r.H-1 {
		return m.runMenu(ctx, menuActions[p.Y-r.Y-1])
	}
	m.menu = nil
	return nil
}

// drawMenu draws the open menu.
func (m *canvasModel) drawMenu(ctx *ui.Context, b *buf) {
	if m.menu == nil {
		return
	}
	r := m.menuRect(ctx.T)
	if r.W < 4 || r.H < 3 {
		return
	}
	b.fill(r, sBase)
	for x := r.X; x < r.X+r.W; x++ {
		b.set(x, r.Y, "─", 1, sAcc)
		b.set(x, r.Y+r.H-1, "─", 1, sAcc)
	}
	for y := r.Y; y < r.Y+r.H; y++ {
		b.set(r.X, y, "│", 1, sAcc)
		b.set(r.X+r.W-1, y, "│", 1, sAcc)
	}
	b.set(r.X, r.Y, "┌", 1, sAcc)
	b.set(r.X+r.W-1, r.Y, "┐", 1, sAcc)
	b.set(r.X, r.Y+r.H-1, "└", 1, sAcc)
	b.set(r.X+r.W-1, r.Y+r.H-1, "┘", 1, sAcc)
	for i, a := range menuActions {
		if 1+i >= r.H-1 {
			break
		}
		st := sBase
		if i == m.menu.sel {
			st = sSel
		}
		b.line(r.X+1, r.Y+1+i, r.W-2, " "+a.label(ctx.T), st)
	}
}
