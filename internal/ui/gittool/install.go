package gittool

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The install guide (A11.4 "미설치"): lazygit was not found, so the dialog
// shows the OS's install command(s) — never run by masil — with y to copy
// the selected one and esc to close.

const installW, installH = 64, 12

type install struct {
	cmds []string
	goos string
	sel  int
	size ui.Size
}

var (
	_ ui.Overlay    = (*install)(nil)
	_ ui.KeyHandler = (*install)(nil)
	_ ui.Titled     = (*install)(nil)
)

func newInstall(*ui.Context, string) ui.Overlay {
	goos := curEnv().GOOS
	return &install{cmds: lazygit.InstallHint(goos), goos: goos}
}

func (d *install) Init(*ui.Context) tea.Cmd            { return nil }
func (d *install) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (d *install) Resize(_ *ui.Context, s ui.Size)     { d.size = s }
func (d *install) Modal() bool                         { return true }
func (d *install) Bounds(body layout.Rect) layout.Rect {
	return layout.Rect{X: body.X + max(0, (body.W-installW)/2), Y: body.Y + max(0, (body.H-installH)/2), W: installW, H: installH}
}
func (d *install) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Lazygit.InstallTitle(), ui.Badge{}
}

func (d *install) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		d.sel = max(0, d.sel-1)
	case "down", "j", "tab":
		d.sel = min(len(d.cmds)-1, d.sel+1)
	case "y", "Y":
		if len(d.cmds) == 0 {
			return true, nil
		}
		return true, ui.Copy(ctx, d.cmds[min(max(d.sel, 0), len(d.cmds)-1)], ctx.T.Lazygit.InstallCopied())
	case "esc", "enter", "q":
		return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
	case "ctrl+c":
		return false, nil
	}
	return true, nil // modal
}

// dlgLine is one dialog row.
type dlgLine struct {
	text  string
	style func(...string) string
	keep  bool // never dropped to make room
}

func (d *install) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	pad := 0
	if w >= 12 {
		pad = 1
	}
	cw := max(1, w-2*pad)
	body := t.Lazygit.InstallBody()
	if d.goos != "darwin" && d.goos != "windows" {
		body += " " + t.Lazygit.InstallLinux()
	}
	var rows []dlgLine
	for _, l := range textwidth.Wrap(textwidth.Sanitize(body), cw) {
		rows = append(rows, dlgLine{text: l, style: th.S.Base.Render, keep: true})
	}
	rows = append(rows, dlgLine{})
	focus := len(rows)
	for i, c := range d.cmds {
		if i == d.sel {
			focus = len(rows)
			rows = append(rows, dlgLine{text: "› " + c, style: th.S.Selected.Render, keep: true})
		} else {
			rows = append(rows, dlgLine{text: "  " + c, style: th.S.Base.Render, keep: true})
		}
	}
	rows = append(rows, dlgLine{})
	for _, l := range textwidth.Wrap(t.Lazygit.InstallTested(lazygit.TestedVersion), cw) {
		rows = append(rows, dlgLine{text: l, style: th.S.Dim.Render})
	}
	rows = append(rows, dlgLine{}, dlgLine{text: t.Lazygit.InstallFooter(), style: th.S.Dim.Render})
	return renderDialog(th, rows, d.size, pad, focus)
}

// renderDialog lays rows into exactly size: rows that do not fit are dropped
// from the bottom (blanks first, then droppable rows); focus stays visible.
func renderDialog(th *theme.Theme, rows []dlgLine, size ui.Size, pad, focus int) string {
	w, h := size.W, size.H
	for len(rows) > h {
		i := lastDroppableRow(rows)
		if i < 0 {
			break
		}
		if i < focus {
			focus--
		}
		rows = append(rows[:i:i], rows[i+1:]...)
	}
	top := 0
	if len(rows) > h && focus >= h {
		top = focus - h + 1
	}
	cw := max(1, w-2*pad)
	blank := th.S.Base.Render(strings.Repeat(" ", w))
	out := make([]string, h)
	for y := range out {
		i := top + y
		if i >= len(rows) {
			out[y] = blank
			continue
		}
		r := rows[i]
		st := r.style
		if st == nil {
			st = th.S.Base.Render
		}
		txt := textwidth.PadRight(textwidth.Truncate(textwidth.Sanitize(r.text), cw, "…"), cw)
		out[y] = th.S.Base.Render(strings.Repeat(" ", pad)) + st(txt) + th.S.Base.Render(strings.Repeat(" ", max(0, w-pad-cw)))
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

func lastDroppableRow(rows []dlgLine) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].text == "" && !rows[i].keep {
			return i
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].keep {
			return i
		}
	}
	return -1
}
