package fun

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Focus summary sizes (copy: the fun catalog area).
const (
	summaryDialogW  = 64
	summaryMaxItems = 20 // api.md FocusSummary.items: <= 20
	// summaryChrome is the outer rows besides the item rows: border 2, the
	// duration line, the counts line, the rule and the key hint.
	summaryChrome = 6
	// summaryStaleGap: a LastFocusSummary that ended this long before the alert
	// belongs to an earlier session; the overlay waits for this one's focus.ended.
	summaryStaleGap = 10 * time.Minute
)

func init() {
	ui.RegisterOverlay(ui.OverlayFocusSummary, func(ctx *ui.Context, alertID string) ui.Overlay {
		return newFocusSummary(ctx, alertID)
	})
}

// alertRoute is where a missed alert leads — the same table as core's alert
// toasts (plan 03 Task 31): friend requests to the friends screen, focus
// summaries to the inbox, otherwise by target.type. ok=false: the target is
// unresolvable and the route is the notifications inbox.
func alertRoute(st *store.Store, a api.Alert) (ui.Route, bool) {
	inbox := ui.Route{Kind: ui.RouteNotifications}
	switch a.Kind {
	case api.AlertFocusSummary:
		return inbox, true
	case api.AlertFriendRequest:
		return ui.Route{Kind: ui.RouteFriends, Anchor: ui.FriendsIncoming}, true
	}
	t := a.Target
	switch t.Type {
	case "message", "channel":
		ch := deref(t.ChannelID)
		if ch == "" && t.Type == "channel" {
			ch = t.ID
		}
		if ch == "" {
			return inbox, false
		}
		msg := deref(t.MessageID)
		if root := deref(t.ThreadRootID); root != "" {
			return ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch, Anchor: msg}, true
		}
		return ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: msg}, true
	case "post":
		id := deref(t.PostID)
		if id == "" {
			id = t.ID
		}
		if id == "" {
			return inbox, false
		}
		return ui.Route{Kind: ui.RoutePost, ID: id}, true
	case "space":
		space := deref(t.SpaceID)
		if space == "" {
			space = t.ID
		}
		r := ui.Route{Kind: ui.RouteInvite, Arg: "direct"}
		if st != nil && space != "" {
			for _, inv := range st.DirectInvites {
				if inv.Space.ID == space {
					r.ID = inv.ID
					break
				}
			}
		}
		return r, true
	case "user":
		if t.ID == "" {
			return inbox, false
		}
		return ui.Route{Kind: ui.RouteProfile, ID: t.ID}, true
	case "notification":
		return inbox, true
	}
	return inbox, false
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// alertIcon is the item glyph of an alert kind.
func alertIcon(k api.AlertKind) string {
	switch k {
	case api.AlertMention:
		return "@"
	case api.AlertDM:
		return "✉"
	case api.AlertThreadReply:
		return "↩"
	case api.AlertInvite:
		return "⌂"
	case api.AlertFriendRequest:
		return "+"
	}
	return "•"
}

// focusSummary is fun.focus_summary (args = alert id): what was missed during
// a focus session. It reads the Store on every View, so a summary that arrives
// (focus.ended) after the alert replaces the "loading" body by itself.
type focusSummary struct {
	alertID string
	st      *store.Store // for Bounds (no Context); refreshed from ctx
	sel     int
	size    ui.Size
	itemY   []int // row of each item in the last View (-1 = not drawn)
}

var (
	_ ui.Overlay      = (*focusSummary)(nil)
	_ ui.KeyHandler   = (*focusSummary)(nil)
	_ ui.MouseHandler = (*focusSummary)(nil)
	_ ui.Titled       = (*focusSummary)(nil)
	_ ui.Helper       = (*focusSummary)(nil)
)

func newFocusSummary(ctx *ui.Context, alertID string) *focusSummary {
	d := &focusSummary{alertID: alertID}
	if ctx != nil {
		d.st = ctx.Store
	}
	return d
}

func (d *focusSummary) sync(ctx *ui.Context) {
	if ctx != nil && ctx.Store != nil {
		d.st = ctx.Store
	}
}

// alert is the focus_summary alert this overlay was opened for (ok=false when
// it is no longer in Store.Alerts).
func (d *focusSummary) alert() (api.Alert, bool) {
	if d.st == nil || d.st.Alerts == nil {
		return api.Alert{}, false
	}
	items := d.st.Alerts.Items()
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].ID == d.alertID {
			return items[i], true
		}
	}
	return api.Alert{}, false
}

// summary is the session summary to show, nil while it has not arrived (none
// yet, or only an earlier session's).
func (d *focusSummary) summary() *api.FocusSummary {
	if d.st == nil || d.st.LastFocusSummary == nil {
		return nil
	}
	sum := d.st.LastFocusSummary
	if a, ok := d.alert(); ok && a.CreatedAt.Sub(sum.EndedAt) > summaryStaleGap {
		return nil
	}
	return sum
}

// items are the summary's missed alerts (newest first, at most 20).
func (d *focusSummary) items() []api.Alert {
	sum := d.summary()
	if sum == nil {
		return nil
	}
	if len(sum.Items) > summaryMaxItems {
		return sum.Items[:summaryMaxItems]
	}
	return sum.Items
}

func (d *focusSummary) Init(ctx *ui.Context) tea.Cmd { d.sync(ctx); return nil }

func (d *focusSummary) Update(ctx *ui.Context, _ tea.Msg) tea.Cmd {
	d.sync(ctx)
	d.sel = min(d.sel, max(len(d.items())-1, 0))
	return nil
}

// open goes to the selected item's target and closes the overlay; with no
// items it only closes.
func (d *focusSummary) open(ctx *ui.Context) tea.Cmd {
	items := d.items()
	if d.sel < 0 || d.sel >= len(items) {
		return closeOverlay
	}
	var st *store.Store
	if ctx != nil {
		st = ctx.Store
	}
	r, _ := alertRoute(st, items[d.sel])
	return tea.Batch(closeOverlay, ui.Navigate(r))
}

func (d *focusSummary) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	d.sync(ctx)
	n := len(d.items())
	switch msg.String() {
	case "esc", "ctrl+k":
		return false, nil
	case "j", "down", "tab":
		d.sel = min(d.sel+1, max(n-1, 0))
	case "k", "up", "shift+tab":
		d.sel = max(d.sel-1, 0)
	case "g", "home":
		d.sel = 0
	case "G", "end":
		d.sel = max(n-1, 0)
	case "enter", "space":
		return true, d.open(ctx)
	}
	return true, nil
}

func (d *focusSummary) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft {
		return nil
	}
	for i, y := range d.itemY {
		if y >= 0 && y == local.Y {
			d.sel = i
			return d.open(ctx)
		}
	}
	return nil
}

func (d *focusSummary) Resize(ctx *ui.Context, size ui.Size) { d.sync(ctx); d.size = size }

// Bounds is a centred min(64, W-4) × min(6+items, H-2) box (one body row while
// loading or empty).
func (d *focusSummary) Bounds(body layout.Rect) layout.Rect {
	return centered(body, summaryDialogW, summaryChrome+max(len(d.items()), 1))
}

func (d *focusSummary) Modal() bool { return true }

func (d *focusSummary) Title(ctx *ui.Context) (string, ui.Badge) {
	return cat(ctx).Fun.SummaryTitle(), ui.Badge{}
}

func (d *focusSummary) Help(ctx *ui.Context) []key.Binding {
	t := cat(ctx)
	return []key.Binding{helpKey("enter", t.Fun.HelpGo()), helpKey("j/k", t.Common.HelpSelect()), helpKey("esc", t.Common.HelpClose())}
}

// countsLine is "<mentions> 3 · DM 2 · <threads> 1 · …" (fun.summary_counts).
func countsLine(t *i18n.T, c api.FocusCounts) string {
	return t.Fun.SummaryCounts(c.Mention, c.DM, c.ThreadReply, c.Invite, c.FriendRequest, c.Other)
}

// clockOf is the HH:MM (or 12-hour, per ui.clock) local time of t.
func clockOf(ctx *ui.Context, t time.Time) string {
	h12 := ctx != nil && ctx.Config != nil && ctx.Config.UI.Clock == "12h"
	var loc *time.Location
	if ctx != nil {
		loc = ctx.Loc
	}
	return hz(ctx).Clock(t, loc, h12)
}

// itemLine is "› @ <title>  <body>      11:12" in exactly w cells.
func itemLine(ctx *ui.Context, t *theme.Theme, a api.Alert, selected bool, w int) string {
	prefix, st := rowStyle(t, selected, false)
	clock := " " + clockOf(ctx, a.CreatedAt)
	leftW := w - 2 - textwidth.Width(clock)
	if leftW < 4 {
		clock, leftW = "", w-2
	}
	head := alertIcon(a.Kind) + " " + textwidth.Sanitize(a.Title)
	left := textwidth.Truncate(head, leftW, "…")
	if rest := leftW - textwidth.Width(left); rest > 2 && textwidth.Width(left) == textwidth.Width(head) {
		if body := markup.Plain(a.Body, rest-2); body != "" {
			left = st(left) + t.S.Dim.Render("  "+body)
		} else {
			left = st(left)
		}
	} else {
		left = st(left)
	}
	return prefix + fit(t, left, leftW) + t.S.Dim.Render(clock)
}

// View is the duration line, the counts line, a rule and the missed items
// (while the summary has not arrived: the alert's title/body and a loading
// line), then the key hint; the item rows scroll to keep the selection.
func (d *focusSummary) View(ctx *ui.Context, _ bool) string {
	d.sync(ctx)
	t, c := themeOf(ctx), cat(ctx)
	w, h := d.size.W, d.size.H
	items := d.items()
	d.sel = min(d.sel, max(len(items)-1, 0))
	d.itemY = make([]int, len(items))
	for i := range d.itemY {
		d.itemY[i] = -1
	}
	if w <= 0 || h <= 0 {
		return ""
	}
	pad := 0
	if w >= 12 {
		pad = 1
	}
	cw := max(w-2*pad, 1)

	var head, body []string
	if sum := d.summary(); sum != nil {
		dur := hz(ctx).Duration(sum.EndedAt.Sub(sum.StartedAt).Round(time.Minute))
		head = append(head,
			t.S.Base.Bold(true).Render(c.Fun.SummaryFocused(dur, clockOf(ctx, sum.StartedAt), clockOf(ctx, sum.EndedAt))),
			t.S.Base.Render(countsLine(c, sum.Counts)))
		for i, a := range items {
			body = append(body, itemLine(ctx, t, a, i == d.sel, cw))
		}
		if len(items) == 0 {
			body = append(body, t.S.Dim.Render(c.Fun.SummaryEmpty()))
		}
	} else {
		title, text := c.Fun.SummaryTitle(), ""
		if a, ok := d.alert(); ok {
			if s := textwidth.Sanitize(a.Title); s != "" {
				title = s
			}
			text = markup.Plain(a.Body, cw)
		}
		head = append(head, t.S.Base.Bold(true).Render(title), t.S.Base.Render(text))
		body = append(body, t.S.Dim.Render(c.Fun.SummaryLoading()))
	}
	head = append(head, t.S.Dim.Render(strings.Repeat("─", cw)))
	keys := c.Fun.SummaryKeys()
	if len(items) == 0 {
		keys = c.Fun.SummaryClose()
	}
	hint := t.S.Dim.Render(textwidth.Truncate(keys, cw, "…"))

	// Rows: head, then as many body rows as fit (scrolled to the selection),
	// then the hint; a box too short for that keeps the top rows only.
	var rows []string
	bodyTop := len(head)
	if avail := h - len(head) - 1; avail >= 1 {
		top := 0
		if len(items) > 0 && d.sel >= avail {
			top = d.sel - avail + 1
		}
		end := min(top+avail, len(body))
		rows = append(append(rows, head...), body[top:end]...)
		for i := top; i < end && i < len(items); i++ {
			d.itemY[i] = bodyTop + i - top
		}
		for len(rows) < h-1 {
			rows = append(rows, "")
		}
		rows = append(rows, hint)
	} else {
		rows = append(append(append(rows, head...), body...), hint)
		for i := range items {
			if y := bodyTop + i; y < h {
				d.itemY[i] = y
			}
		}
	}
	side := t.S.Base.Render(strings.Repeat(" ", pad))
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		out = append(out, fit(t, side+fit(t, l, cw)+side, w))
	}
	return strings.Join(out, "\n")
}
