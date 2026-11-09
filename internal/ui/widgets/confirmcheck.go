package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// confirmCheckH is the default outer height of ConfirmCheck (Confirm + the
// option row).
const confirmCheckH = confirmH + 2

// confirmCheck is widgets.ConfirmCheck.
type confirmCheck struct {
	confirmDialog
	check    string
	checked  bool
	onYes    func(checked bool) tea.Cmd
	checkRow int // row of the option in the last layout (-1 = not drawn)
}

var (
	_ ui.Overlay      = (*confirmCheck)(nil)
	_ ui.KeyHandler   = (*confirmCheck)(nil)
	_ ui.MouseHandler = (*confirmCheck)(nil)
)

// ConfirmCheck is Confirm with one option row ("[ ] check", off at first) under
// the body: space or x flips it; yes runs onYes(checked) (nil = nothing). The
// other keys and clicks are Confirm's.
func ConfirmCheck(title, body, check, yes, no string, onYes func(checked bool) tea.Cmd) ui.Overlay {
	d := &confirmCheck{check: check, onYes: onYes}
	d.confirmDialog = confirmDialog{title: title, body: body, yes: yes, no: no}
	return d
}

func (d *confirmCheck) Bounds(body layout.Rect) layout.Rect {
	return centered(body, confirmW, confirmCheckH)
}

func (d *confirmCheck) View(ctx *ui.Context, _ bool) string { return d.renderCheck(themeOf(ctx)) }

// press is button i (0 = yes).
func (d *confirmCheck) press(i int) tea.Cmd {
	if i == 0 && d.onYes != nil {
		return tea.Batch(closeOverlay, d.onYes(d.checked))
	}
	return closeOverlay
}

func (d *confirmCheck) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "space", "x", "X":
		d.checked = !d.checked
		return true, nil
	case "y", "Y":
		return true, d.press(0)
	case "enter":
		return true, d.press(d.sel)
	}
	return d.confirmDialog.HandleKey(ctx, msg) // n/esc close (its onYes is nil), ←/→/tab move
}

func (d *confirmCheck) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft {
		return nil
	}
	if local.Y == d.checkRow {
		d.checked = !d.checked
		return nil
	}
	if local.Y != d.btnRow {
		return nil
	}
	for i, r := range d.btnHits {
		if local.X >= r[0] && local.X < r[1] {
			return d.press(i)
		}
	}
	return nil
}

// box is the option's check box.
func (d *confirmCheck) box() string {
	if d.checked {
		return "[x] "
	}
	return "[ ] "
}

func (d *confirmCheck) renderCheck(t *theme.Theme) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	p := dialogPad(w)
	cw := max(1, w-2*p)
	head := wrapStyled(d.title, cw, t.S.Title.Render)
	if len(head) > 2 {
		head = head[:2]
	}
	var mid []string
	if strings.TrimSpace(d.body) != "" {
		mid = append([]string{""}, wrapStyled(d.body, cw, t.S.Base.Render)...)
	}
	mid = append(mid, "")
	checkAt := len(mid)
	for i, l := range textwidth.Wrap(textwidth.Sanitize(d.check), max(1, cw-4)) {
		lead := d.box()
		if i > 0 {
			lead = "    "
		}
		mid = append(mid, t.S.Bold.Render(lead)+t.S.Base.Render(l))
	}
	labels := [2]string{textwidth.Sanitize(d.yes), textwidth.Sanitize(d.no)}
	open, shut, gap := "[ ", " ]", 2
	width := func() int {
		return textwidth.Width(open+labels[0]+shut) + gap + textwidth.Width(open+labels[1]+shut)
	}
	if width() > cw {
		open, shut, gap = " ", " ", 1
	}
	if width() > cw {
		each := max(1, (cw-gap)/2-2)
		for i := range labels {
			labels[i] = textwidth.Truncate(labels[i], each, "…")
		}
	}
	var segs [2]string
	var widths [2]int
	for i, l := range labels {
		st := t.S.Base
		if i == d.sel {
			st = t.S.Selected
		}
		segs[i] = st.Render(open + l + shut)
		widths[i] = textwidth.Width(open + l + shut)
	}
	x0 := max(0, cw-width())
	buttons := pad(t, x0) + segs[0] + pad(t, gap) + segs[1]
	rows, _ := stack(head, mid, []string{"", buttons}, checkAt, h)
	d.checkRow = -1
	for i, r := range rows {
		if strings.Contains(r, d.box()) {
			d.checkRow = i
			break
		}
	}
	d.btnRow = len(rows) - 1
	d.btnHits = [2][2]int{
		{p + x0, p + x0 + widths[0]},
		{p + x0 + widths[0] + gap, p + x0 + widths[0] + gap + widths[1]},
	}
	return renderRows(t, rows, d.size)
}
