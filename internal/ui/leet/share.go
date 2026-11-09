package leet

import (
	"context"
	"sync"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The solve record and the card dialog (A10.4): an Accepted submit is
// recorded once at masil (the streak) with no card; c opens leet.share to
// put a card in the #daily thread of the chosen spaces (default: every
// space with #daily on), the code only when ticked (a spoiler, at most
// 4000 characters, with the warning that masil stores it). Nothing of the
// LeetCode cookies ever goes into these requests.

// OverlayShare is the card dialog (args = the instance id).
const OverlayShare = "leet.share"

const (
	shareW, shareH = 64, 16
	maxCodeRunes   = 4000
)

func init() {
	ui.RegisterOverlay(OverlayShare, newShare)
}

// accepted solves waiting for a card, by instance id (UI goroutine).
var (
	acceptedMu sync.Mutex
	acceptedBy = map[string]acceptedMsg{}
)

func putAccepted(m acceptedMsg) {
	acceptedMu.Lock()
	defer acceptedMu.Unlock()
	acceptedBy[m.id] = m
}

func getAccepted(id string) (acceptedMsg, bool) {
	acceptedMu.Lock()
	defer acceptedMu.Unlock()
	m, ok := acceptedBy[id]
	return m, ok
}

// solveRequest is the record of an accepted solve (no card, no code).
func solveRequest(m acceptedMsg) api.LeetSolveRequest {
	sub := m.sub
	r := api.LeetSolveRequest{Slug: m.q.Slug, Lang: m.lang.Slug, FrontendID: m.q.FrontendID, SubmissionID: &sub}
	if m.res != nil && m.res.RuntimePercentile != nil {
		p := *m.res.RuntimePercentile
		r.RuntimePercentile = &p
	}
	return r
}

// recordTag is the ui.Do tag of the streak record.
const recordTag = "leet.solve:record"

// recordSolve is the one record of an accepted solve.
func recordSolve(ctx *ui.Context, m acceptedMsg) tea.Cmd {
	req, date := solveRequest(m), m.date
	return ui.Do(ctx, recordTag, func(c context.Context, a *api.Client) (*api.LeetSolveResult, error) {
		if a == nil {
			return nil, nil
		}
		return a.PutLeetSolve(c, date, req)
	})
}

// shareDlg is leet.share.
type shareDlg struct {
	m       acceptedMsg
	spaces  []string        // #daily spaces, LeetMe order
	checked map[string]bool // space -> share
	code    bool            // attach the code
	tooLong bool
	sel     int // rows: spaces…, code, button
	busy    bool
	size    ui.Size
}

var (
	_ ui.Overlay    = (*shareDlg)(nil)
	_ ui.KeyHandler = (*shareDlg)(nil)
	_ ui.Titled     = (*shareDlg)(nil)
)

func newShare(ctx *ui.Context, id string) ui.Overlay {
	m, ok := getAccepted(id)
	if !ok || m.q == nil {
		return nil
	}
	d := &shareDlg{m: m, checked: map[string]bool{}, tooLong: utf8.RuneCountInString(m.code) > maxCodeRunes}
	if me := knownMe(ctx); me != nil {
		for _, s := range me.DailySpaces {
			if s.SpaceID != "" && !d.checked[s.SpaceID] {
				d.spaces = append(d.spaces, s.SpaceID)
				d.checked[s.SpaceID] = true
			}
		}
	}
	d.sel = len(d.spaces) + 1 // the button
	return d
}

func (d *shareDlg) Init(*ui.Context) tea.Cmd        { return nil }
func (d *shareDlg) Resize(_ *ui.Context, s ui.Size) { d.size = s }
func (d *shareDlg) Modal() bool                     { return true }
func (d *shareDlg) Bounds(body layout.Rect) layout.Rect {
	return centered(body, shareW, shareH)
}
func (d *shareDlg) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Leetcode.ShareTitle(), ui.Badge{}
}

// shareCardTag is the ui.Do tag of the card request.
const shareCardTag = "leet.solve:share"

func (d *shareDlg) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(ui.ResultMsg[*api.LeetSolveResult])
	if !ok || m.Tag != shareCardTag || !d.busy {
		return nil
	}
	d.busy = false
	if m.Err != nil {
		return ui.Toast(ctx.T.Leetcode.ShareFailed(), ui.ToastError)
	}
	return tea.Batch(closeOverlay, ui.Toast(ctx.T.Leetcode.Shared(), ui.ToastInfo))
}

func (d *shareDlg) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	n := len(d.spaces) + 2
	switch msg.String() {
	case "ctrl+c":
		return false, nil
	case "esc", "q":
		return true, closeOverlay
	case "up", "k", "shift+tab":
		d.sel = max(0, d.sel-1)
	case "down", "j", "tab":
		d.sel = min(n-1, d.sel+1)
	case "space", "x":
		d.toggle()
	case "enter": // leaves the cards from any row (space ticks)
		return true, d.send(ctx)
	}
	return true, nil
}

func (d *shareDlg) toggle() {
	switch {
	case d.sel < len(d.spaces):
		s := d.spaces[d.sel]
		d.checked[s] = !d.checked[s]
	case d.sel == len(d.spaces) && !d.tooLong:
		d.code = !d.code
	}
}

// send leaves the cards (the record again, with the spaces and the code).
func (d *shareDlg) send(ctx *ui.Context) tea.Cmd {
	if d.busy {
		return nil
	}
	req := solveRequest(d.m)
	for _, s := range d.spaces {
		if d.checked[s] {
			req.ShareSpaceIDs = append(req.ShareSpaceIDs, s)
		}
	}
	if d.code && !d.tooLong {
		code := d.m.code
		req.Code = &code
	}
	d.busy = true
	date := d.m.date
	return ui.Do(ctx, shareCardTag, func(c context.Context, a *api.Client) (*api.LeetSolveResult, error) {
		return a.PutLeetSolve(c, date, req)
	})
}

func (d *shareDlg) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	cw := contentWidth(w)
	var head []string
	if len(d.spaces) == 0 {
		head = append(head, th.S.Dim.Render(textwidth.Truncate(t.Leetcode.ShareNoSpaces(), cw, "…")))
	}
	var mid []string
	row := func(i int, text string, dim bool) string {
		mark, st := "  ", th.S.Base
		if dim {
			st = th.S.Dim
		}
		if i == d.sel {
			mark, st = "› ", th.S.Selected
		}
		return st.Render(textwidth.Truncate(mark+text, cw, "…"))
	}
	box := func(on bool) string {
		if on {
			return "[x] "
		}
		return "[ ] "
	}
	for i, s := range d.spaces {
		mid = append(mid, row(i, box(d.checked[s])+textwidth.Sanitize(t.Leetcode.SettingsDaily(spaceName(ctx, s))), false))
	}
	mid = append(mid, row(len(d.spaces), box(d.code)+t.Leetcode.AttachCode(), d.tooLong))
	var warn []string
	if d.tooLong {
		warn = append(warn, th.S.Warning.Render(textwidth.Truncate(t.Leetcode.CodeTooLong(), cw, "…")))
	}
	for _, l := range textwidth.Wrap(t.Leetcode.CodeStoredWarning(), cw) {
		warn = append(warn, th.S.Warning.Render(l))
	}
	mid = append(mid, "")
	mid = append(mid, row(len(d.spaces)+1, "[ "+t.Leetcode.ShareButton()+" ]", false))
	focus := d.sel
	if d.sel == len(d.spaces)+1 {
		focus = len(mid) - 1
	}
	// the warning is never cut: it leads, kept
	head = append(warn, head...)
	keep := len(warn)
	head = append(head, "")
	foot := []string{"", th.S.Dim.Render(textwidth.Truncate(t.Leetcode.ShareFooter(), cw, "…"))}
	rows, _ := stack(head, mid, foot, focus, h, keep)
	return renderRows(th, rows, d.size)
}
