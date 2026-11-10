package leet

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The first-run notice (A10.4 "처음"): masil fetches LeetCode problems from
// this PC and is not affiliated with LeetCode (never cut: no_truncate), the
// language to solve in (Go first) and the solutions folder. enter confirms;
// the notice does not come back (leetState.FirstRunDone).

const firstRunW, firstRunH = 60, 18

type firstRun struct {
	sel  int
	size ui.Size
}

var (
	_ ui.Overlay    = (*firstRun)(nil)
	_ ui.KeyHandler = (*firstRun)(nil)
	_ ui.Titled     = (*firstRun)(nil)
)

func newFirstRun(ctx *ui.Context, _ string) ui.Overlay {
	cur := currentLang(ctx)
	return &firstRun{sel: max(0, slices.IndexFunc(leetcode.Languages, func(l leetcode.Lang) bool { return l.Slug == cur.Slug }))}
}

func (d *firstRun) Init(*ui.Context) tea.Cmd            { return nil }
func (d *firstRun) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (d *firstRun) Resize(_ *ui.Context, s ui.Size)     { d.size = s }
func (d *firstRun) Modal() bool                         { return true }
func (d *firstRun) Bounds(body layout.Rect) layout.Rect { return centered(body, firstRunW, firstRunH) }
func (d *firstRun) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Leetcode.FirstRunTitle(), ui.Badge{}
}

func (d *firstRun) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		d.sel = max(0, d.sel-1)
	case "down", "j", "tab":
		d.sel = min(len(leetcode.Languages)-1, d.sel+1)
	case "home", "g":
		d.sel = 0
	case "end", "G":
		d.sel = len(leetcode.Languages) - 1
	case "enter", "esc":
		st := loadState(ctx)
		st.FirstRunDone = true
		if msg.String() == "enter" {
			st.Lang = leetcode.Languages[d.sel].Slug
		}
		saveState(ctx, st)
		return true, tea.Batch(closeOverlay, emit(firstRunDoneMsg{}))
	case "ctrl+c":
		return false, nil
	}
	return true, nil // modal
}

// firstRunDoneMsg tells open instances the language may have changed.
type firstRunDoneMsg struct{}

func (d *firstRun) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	cw := contentWidth(w)
	var head []string
	for _, l := range textwidth.Wrap(textwidth.Sanitize(t.Leetcode.FirstRunNotice()), cw) {
		head = append(head, th.S.Base.Render(l))
	}
	head = append(head, "", th.S.Bold.Render(textwidth.Truncate(t.Leetcode.FirstRunLang(), cw, "…")))
	mid := make([]string, len(leetcode.Languages))
	for i, l := range leetcode.Languages {
		if i == d.sel {
			mid[i] = th.S.Selected.Render(textwidth.PadRight("› "+l.Name, cw))
		} else {
			mid[i] = th.S.Base.Render("  " + l.Name)
		}
	}
	var foot []string
	foot = append(foot, "")
	for _, l := range textwidth.Wrap(t.Leetcode.FirstRunDir(shortPath(solutionDir(ctx))), cw) {
		foot = append(foot, th.S.Dim.Render(l))
	}
	foot = append(foot, th.S.Dim.Render(textwidth.Truncate(t.Leetcode.FirstRunFooter(), cw, "…")))
	rows, _ := stack(head, mid, foot, d.sel, h, len(head)-2)
	return renderRows(th, rows, d.size)
}

// ---- dialog helpers (shared by the tool's overlays) ----

// centered is a w x h rect centered in body (the app clamps it to body).
func centered(body layout.Rect, w, h int) layout.Rect {
	w, h = min(w, body.W), min(h, body.H)
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

// emit is a Cmd delivering msg.
func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// dialogPad is the left/right margin inside a dialog (none when very narrow).
func dialogPad(w int) int {
	if w >= 12 {
		return 1
	}
	return 0
}

// contentWidth is the text width inside a dialog of inner width w.
func contentWidth(w int) int { return max(1, w-2*dialogPad(w)) }

// stack lays out head (pinned top), mid (scrolled so mid[focus] stays
// visible; focus < 0 = top) and foot (pinned bottom) into at most h rows.
// While too tall, blank spacer rows ("") are dropped (mid, foot, head). The
// first keepHead rows of head are never dropped (a no_truncate notice): when
// head+foot leave no row for mid, the foot goes first, then the head's
// droppable tail; if the kept rows alone overflow, the whole thing scrolls
// with focus. midTop is the row of mid[0].
func stack(head, mid, foot []string, focus, h, keepHead int) (rows []string, midTop int) {
	head, mid, foot = slices.Clone(head), slices.Clone(mid), slices.Clone(foot)
	keepHead = min(max(keepHead, 0), len(head))
	dropBlank := func(s []string, from int, focus *int) ([]string, bool) {
		for i := len(s) - 1; i >= from; i-- {
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
		if mid, ok = dropBlank(mid, 0, &focus); ok {
			continue
		}
		if foot, ok = dropBlank(foot, 0, nil); ok {
			continue
		}
		if head, ok = dropBlank(head, keepHead, nil); ok {
			continue
		}
		break
	}
	min1 := 0
	if len(mid) > 0 {
		min1 = 1
	}
	for len(head)+len(foot)+min1 > h && len(foot) > 0 {
		foot = foot[1:] // the foot's top first (the key hint stays longest)
	}
	for len(head)+len(foot)+min1 > h && len(head) > keepHead {
		head = head[:len(head)-1]
	}
	if len(head)+len(foot)+min1 > h { // the kept notice alone overflows: scroll everything
		all := append(append(append([]string{}, head...), mid...), foot...)
		f := len(head) + max(focus, 0)
		off := 0
		if f >= h {
			off = f - h + 1
		}
		off = min(off, max(0, len(all)-h))
		return all[off:min(len(all), off+h)], len(head) - off
	}
	avail := max(0, h-len(head)-len(foot))
	off := 0
	if focus >= avail {
		off = focus - avail + 1
	}
	off = min(off, max(0, len(mid)-avail))
	rows = append(rows, head...)
	midTop = len(rows) - off
	for i := off; i < off+avail && i < len(mid); i++ {
		rows = append(rows, mid[i])
	}
	for len(rows)+len(foot) < h {
		rows = append(rows, "")
	}
	rows = append(rows, foot...)
	return rows, midTop
}

// renderRows fits rows into exactly size (pad cells left and right, every
// cell styled).
func renderRows(th *theme.Theme, rows []string, size ui.Size) string {
	p := dialogPad(size.W)
	cw := contentWidth(size.W)
	pad := th.S.Base.Render(strings.Repeat(" ", p))
	tail := th.S.Base.Render(strings.Repeat(" ", max(0, size.W-p-cw)))
	out := make([]string, size.H)
	for i := range out {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		out[i] = pad + fitStyled(th, l, cw) + tail
	}
	return strings.Join(textwidth.Fit(out, size.W, size.H), "\n")
}
