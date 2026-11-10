package leet

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The ranking overlay leet.board (A10.4): a space's board for this week,
// this month and all time, and my friends' streaks. Each tab is asked once
// when first shown; the rows are what masil sends, sanitized.

const boardW, boardH = 56, 18

// board tabs, in order.
const (
	tabWeek = iota
	tabMonth
	tabAll
	tabFriends
	boardTabs
)

// periods are the space board's period query of the first three tabs.
var periods = [...]string{tabWeek: "week", tabMonth: "month", tabAll: "all"}

func init() {
	ui.RegisterOverlay(OverlayBoard, newBoard)
}

// boardTab is one tab's answer.
type boardTab struct {
	asked, done bool
	err         error
	rows        []string // plain rows (sanitized), ranked
	mine        []bool   // the row is me
}

type boardDlg struct {
	space  string
	tab    int
	tabs   [boardTabs]boardTab
	scroll int
	size   ui.Size
}

var (
	_ ui.Overlay    = (*boardDlg)(nil)
	_ ui.KeyHandler = (*boardDlg)(nil)
	_ ui.Titled     = (*boardDlg)(nil)
)

// newBoard opens on the space's week (no space: the friends tab).
func newBoard(_ *ui.Context, space string) ui.Overlay {
	d := &boardDlg{space: strings.TrimSpace(space)}
	if d.space == "" {
		d.tab = tabFriends
	}
	return d
}

func (d *boardDlg) Init(ctx *ui.Context) tea.Cmd     { return d.ask(ctx) }
func (d *boardDlg) Resize(_ *ui.Context, s ui.Size)  { d.size = s }
func (d *boardDlg) Modal() bool                      { return true }
func (d *boardDlg) Bounds(b layout.Rect) layout.Rect { return centered(b, boardW, boardH) }
func (d *boardDlg) Title(ctx *ui.Context) (string, ui.Badge) {
	return strings.TrimSpace(ctx.T.Leetcode.BoardTitle(spaceName(ctx, d.space))), ui.Badge{}
}

// boardTag is the ui.Do tag of one tab's request.
func (d *boardDlg) tag(tab int) string {
	if tab == tabFriends {
		return "leet.board:friends"
	}
	return "leet.board:" + d.space + ":" + periods[tab]
}

// ask requests the current tab once.
func (d *boardDlg) ask(ctx *ui.Context) tea.Cmd {
	tb := &d.tabs[d.tab]
	if tb.asked || (d.tab != tabFriends && d.space == "") {
		return nil
	}
	tb.asked = true
	if d.tab == tabFriends {
		return ui.Do(ctx, d.tag(tabFriends), func(c context.Context, a *api.Client) (*api.LeetFriendsBoard, error) {
			return a.LeetFriendsBoard(c)
		})
	}
	space, period := d.space, periods[d.tab]
	return ui.Do(ctx, d.tag(d.tab), func(c context.Context, a *api.Client) (*api.LeetBoard, error) {
		return a.LeetSpaceBoard(c, space, period)
	})
}

func (d *boardDlg) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	t, me := ctx.T, myID(ctx)
	switch m := msg.(type) {
	case ui.ResultMsg[*api.LeetBoard]:
		for tab := tabWeek; tab < tabFriends; tab++ {
			if m.Tag != d.tag(tab) {
				continue
			}
			tb := &d.tabs[tab]
			tb.done, tb.err, tb.rows, tb.mine = true, m.Err, nil, nil
			if m.Err == nil && m.Value != nil {
				for i, e := range m.Value.Items {
					tb.rows = append(tb.rows, t.Leetcode.BoardRow(i+1, authorName(e.User), e.Solved, e.Streak))
					tb.mine = append(tb.mine, me != "" && e.User.ID == me)
				}
			}
		}
	case ui.ResultMsg[*api.LeetFriendsBoard]:
		if m.Tag != d.tag(tabFriends) {
			return nil
		}
		tb := &d.tabs[tabFriends]
		tb.done, tb.err, tb.rows, tb.mine = true, m.Err, nil, nil
		if m.Err == nil && m.Value != nil {
			for i, e := range m.Value.Items {
				tb.rows = append(tb.rows, t.Leetcode.BoardFriendRow(i+1, authorName(e.User), e.Current, e.Best))
				tb.mine = append(tb.mine, me != "" && e.User.ID == me)
			}
		}
	}
	return nil
}

func myID(ctx *ui.Context) string {
	if me := ctx.Me(); me != nil {
		return me.ID
	}
	return ""
}

// first is the first tab this dialog can show (no space: friends only).
func (d *boardDlg) first() int {
	if d.space == "" {
		return tabFriends
	}
	return tabWeek
}

func (d *boardDlg) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k := msg.String(); k {
	case "ctrl+c":
		return false, nil
	case "esc", "q":
		return true, closeOverlay
	case "right", "l", "tab":
		return true, d.show(ctx, min(boardTabs-1, d.tab+1))
	case "left", "h", "shift+tab":
		return true, d.show(ctx, max(d.first(), d.tab-1))
	case "1", "2", "3", "4":
		return true, d.show(ctx, max(d.first(), int(k[0]-'1')))
	case "down", "j":
		d.scroll++
	case "up", "k":
		d.scroll = max(0, d.scroll-1)
	}
	return true, nil
}

// show switches to tab (asking for it the first time).
func (d *boardDlg) show(ctx *ui.Context, tab int) tea.Cmd {
	if tab == d.tab {
		return nil
	}
	d.tab, d.scroll = tab, 0
	return d.ask(ctx)
}

// tabNames are the tab labels.
func tabNames(t *i18n.T) [boardTabs]string {
	return [boardTabs]string{t.Leetcode.BoardWeek(), t.Leetcode.BoardMonth(), t.Leetcode.BoardAll(), t.Leetcode.BoardFriends()}
}

func (d *boardDlg) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	cw := contentWidth(w)
	var tabs []string
	for i, name := range tabNames(t) {
		switch {
		case i == d.tab:
			tabs = append(tabs, th.S.Selected.Render("["+name+"]"))
		case i < d.first():
			continue
		default:
			tabs = append(tabs, th.S.Dim.Render(name))
		}
	}
	head := []string{fitStyled(th, strings.Join(tabs, th.S.Dim.Render(" · ")), cw), ""}
	tb := d.tabs[d.tab]
	var mid []string
	switch {
	case tb.err != nil:
		mid = append(mid, cut(th.S.Warning.Render, t.Leetcode.BoardFailed(), cw))
	case !tb.done:
		mid = append(mid, cut(th.S.Dim.Render, t.Leetcode.BoardLoading(), cw))
	case len(tb.rows) == 0:
		mid = append(mid, cut(th.S.Dim.Render, t.Leetcode.BoardEmpty(), cw))
	}
	for i, r := range tb.rows {
		st := th.S.Base.Render
		if tb.mine[i] {
			st = th.S.Bold.Render
		}
		mid = append(mid, cut(st, textwidth.Sanitize(r), cw))
	}
	foot := []string{"", cut(th.S.Dim.Render, t.Leetcode.BoardFooter(), cw)}
	d.scroll = min(d.scroll, max(0, len(mid)-1))
	rows, _ := stack(head, mid[d.scroll:], foot, -1, h, 0)
	return renderRows(th, rows, d.size)
}
