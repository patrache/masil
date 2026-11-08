package widgets

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Default outer sizes of the core dialogs (plan 03 Task 28), centered in the body.
const (
	confirmW, confirmH = 48, 8
	emojiW, emojiH     = 44, 14
	reportW, reportH   = 52, 16
)

// centered is a w x h rect centered in body (the app clamps it to body).
func centered(body layout.Rect, w, h int) layout.Rect {
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

// closeOverlay is the Cmd closing the top overlay.
func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

// dialogPad is the left/right content margin inside a dialog (none when very narrow).
func dialogPad(w int) int {
	if w >= 12 {
		return 1
	}
	return 0
}

// stack lays out head (pinned top), mid (scrolled so mid[focus] stays visible; focus
// < 0 = top) and foot (pinned bottom) into exactly h rows. While the content is too
// tall, blank spacer lines ("") are dropped first (mid, then foot, then head); then
// mid scrolls; when not even one mid row fits the head goes, then the foot is cut
// from the top. midTop is the row of mid[0] (negative when scrolled past).
func stack(head, mid, foot []string, focus, h int) (rows []string, midTop int) {
	head, mid, foot = slices.Clone(head), slices.Clone(mid), slices.Clone(foot)
	dropBlank := func(s []string, focus *int) ([]string, bool) {
		for i := len(s) - 1; i >= 0; i-- {
			if s[i] == "" {
				if focus != nil && *focus > i {
					*focus--
				}
				return slices.Delete(s, i, i+1), true
			}
		}
		return s, false
	}
	for len(head)+len(mid)+len(foot) > h {
		var ok bool
		if mid, ok = dropBlank(mid, &focus); ok {
			continue
		}
		if foot, ok = dropBlank(foot, nil); ok {
			continue
		}
		if head, ok = dropBlank(head, nil); ok {
			continue
		}
		break
	}
	avail := h - len(head) - len(foot)
	if avail < 1 && len(mid) > 0 {
		head = nil
		avail = h - len(foot)
	}
	if avail < 1 && len(mid) > 0 {
		foot = foot[min(len(foot), len(foot)-(h-1)):]
		avail = h - len(foot)
	}
	avail = max(0, avail)
	off := 0
	if focus >= avail {
		off = focus - avail + 1
	}
	off = min(off, max(0, len(mid)-avail))
	rows = append(rows, head...)
	midTop = len(rows) - off
	for i := off; i < off+avail; i++ {
		if i < len(mid) {
			rows = append(rows, mid[i])
		} else {
			rows = append(rows, "")
		}
	}
	rows = append(rows, foot...)
	if len(rows) > h {
		rows = rows[:max(0, h)]
	}
	return rows, midTop
}

// render fits rows into exactly size (pad cells left and right, every cell styled).
func renderRows(t *theme.Theme, rows []string, size ui.Size) string {
	p := dialogPad(size.W)
	out := make([]string, 0, size.H)
	for i := range size.H {
		var l string
		if i < len(rows) {
			l = rows[i]
		}
		out = append(out, pad(t, p)+fitLine(t, l, size.W-2*p)+pad(t, p))
	}
	return strings.Join(out, "\n")
}

// wrapStyled wraps sanitized s to w cells, each line rendered with render (at least one line).
func wrapStyled(s string, w int, render func(...string) string) []string {
	var out []string
	for _, l := range textwidth.Wrap(textwidth.Sanitize(s), max(1, w)) {
		out = append(out, render(l))
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// confirmDialog is widgets.Confirm.
type confirmDialog struct {
	title, body, yes, no string
	onYes                tea.Cmd
	sel                  int // 0 = yes, 1 = no
	size                 ui.Size

	btnRow  int       // row of the buttons in the last layout
	btnHits [2][2]int // x ranges [x0, x1) of the yes/no buttons in the last layout
}

var (
	_ ui.Overlay      = (*confirmDialog)(nil)
	_ ui.KeyHandler   = (*confirmDialog)(nil)
	_ ui.MouseHandler = (*confirmDialog)(nil)
	_ ui.Helper       = (*confirmDialog)(nil)
)

// Confirm is a yes/no dialog overlay (CA 5.7). y / enter on "yes" run onYes (nil =
// nothing) and close it; n, esc and enter on "no" just close it. ←/→ (h/l, tab)
// move between the buttons; a click on a button presses it.
func Confirm(title, body, yes, no string, onYes tea.Cmd) ui.Overlay {
	if yes == "" {
		yes = "확인"
	}
	if no == "" {
		no = "취소"
	}
	return &confirmDialog{title: title, body: body, yes: yes, no: no, onYes: onYes}
}

func (d *confirmDialog) Init(*ui.Context) tea.Cmd            { return nil }
func (d *confirmDialog) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (d *confirmDialog) Resize(_ *ui.Context, size ui.Size)  { d.size = size }
func (d *confirmDialog) Bounds(body layout.Rect) layout.Rect {
	return centered(body, confirmW, confirmH)
}
func (d *confirmDialog) Modal() bool                         { return true }
func (d *confirmDialog) View(ctx *ui.Context, _ bool) string { return d.render(themeOf(ctx)) }
func (d *confirmDialog) Help(*ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", d.yes)),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", d.no)),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "선택")),
	}
}

// choose presses button i (0 = yes).
func (d *confirmDialog) choose(i int) tea.Cmd {
	if i == 0 && d.onYes != nil {
		return tea.Batch(closeOverlay, d.onYes)
	}
	return closeOverlay
}

func (d *confirmDialog) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return true, d.choose(0)
	case "n", "N", "esc":
		return true, d.choose(1)
	case "enter", "space":
		return true, d.choose(d.sel)
	case "left", "h":
		d.sel = 0
	case "right", "l":
		d.sel = 1
	case "tab", "shift+tab":
		d.sel = 1 - d.sel
	default:
		return false, nil
	}
	return true, nil
}

func (d *confirmDialog) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft || local.Y != d.btnRow {
		return nil
	}
	for i, r := range d.btnHits {
		if local.X >= r[0] && local.X < r[1] {
			return d.choose(i)
		}
	}
	return nil
}

func (d *confirmDialog) render(t *theme.Theme) string {
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
	labels := [2]string{textwidth.Sanitize(d.yes), textwidth.Sanitize(d.no)}
	// "[ 로그아웃 ]  [ 취소 ]", narrower " 로그아웃 · 취소 " forms, then cut labels.
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
	rows, _ := stack(head, mid, []string{"", buttons}, -1, h)
	d.btnRow = len(rows) - 1
	d.btnHits = [2][2]int{
		{p + x0, p + x0 + widths[0]},
		{p + x0 + widths[0] + gap, p + x0 + widths[0] + gap + widths[1]},
	}
	return renderRows(t, rows, d.size)
}
