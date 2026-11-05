package feed

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteNotifications, newNotifications)
}

// Notifications copy.
const (
	textNotifTitle    = "알림"
	textNotifEmpty    = "새 알림이 없어요"
	iconNotifEmpty    = "🔔"
	textNotifSomeone  = "알 수 없음"
	textNotifSpace    = "스페이스"
	errContextNotif   = "알림"
	notifOpenKey      = "notifications" // FeedStore.SetOpen key
	notifNarrowWidth  = 40              // inner width below which rows use the short copy
	notifPreviewMin   = 8               // fewest cells a ` · "…"` preview is drawn in
	notifGutterW      = 2               // selection bar + space
	notifDotW         = 2               // "● " (wide rows)
	notifMarginW      = 1               // blank cell after the time
	notifTimeMinCells = 6               // fewest text cells kept before the time is dropped
)

// Notifications result tags (ui.Do).
const (
	tagNotifs     = "feed.notifications:me"
	tagNotifsMore = "feed.notifications_more:me"
	tagNotifsRead = "feed.notifications_read:" // + notification id | "all"
)

// notifCopy is one notification type's icon and its narrow / wide text;
// "{n}" is the actor's display name, "{s}" the space name.
type notifCopy struct{ icon, narrow, wide string }

// notifCopies is the Task 15 copy table.
var notifCopies = map[api.NotificationType]notifCopy{
	"post_like":       {"♥", "{n}님이 좋아함", "{n}님이 내 글을 좋아해요"},
	"post_repost":     {"↻", "{n}님이 리포스트", "{n}님이 내 글을 리포스트했어요"},
	"post_reply":      {"↩", "{n}님이 답글", "{n}님이 답글을 남겼어요"},
	"post_quote":      {"❝", "{n}님이 인용", "{n}님이 내 글을 인용했어요"},
	"post_mention":    {"@", "{n}님이 멘션", "{n}님이 글에서 나를 멘션했어요"},
	"message_mention": {"@", "{n}님이 멘션", "{n}님이 {s}에서 나를 멘션했어요"},
	"thread_reply":    {"↩", "{n}님이 스레드 답글", "{n}님이 스레드에 답글을 달았어요"},
	"follow":          {"+", "{n}님이 팔로우", "{n}님이 나를 팔로우해요"},
	"follow_request":  {"+", "{n}님의 팔로우 요청", "{n}님이 팔로우를 요청했어요"},
	"follow_accepted": {"✓", "{n}님이 수락", "{n}님이 팔로우 요청을 수락했어요"},
	"friend_request":  {"+", "{n}님의 친구 요청", "{n}님이 친구 요청을 보냈어요"},
	"friend_accepted": {"✓", "{n}님과 친구", "{n}님과 친구가 되었어요"},
	"space_invite":    {"⌂", "{s} 초대", "{n}님이 {s}에 초대했어요"},
	"guestbook_entry": {"✎", "{n}님의 방명록", "{n}님이 방명록을 남겼어요"},
}

// notifFallback is the copy of a type this client does not know.
var notifFallback = notifCopy{"•", "{n}님의 알림", "{n}님의 새 알림이 있어요"}

// notifTarget is where opening n leads: a route, or (follow_request) the
// feed.follow_requests overlay. ok is false when n lacks what its target needs.
func notifTarget(n api.Notification) (r ui.Route, overlay string, ok bool) {
	c := n.Context
	actor := ""
	if n.Actor != nil {
		actor = n.Actor.ID
	}
	switch n.Type {
	case "post_like", "post_repost", "post_reply", "post_quote", "post_mention":
		if id := deref(c.PostID); id != "" {
			return ui.Route{Kind: ui.RoutePost, ID: id}, "", true
		}
	case "message_mention", "thread_reply":
		ch, msg := deref(c.ChannelID), deref(c.MessageID)
		if ch == "" {
			break
		}
		if root := deref(c.ThreadRootID); root != "" {
			return ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch, Anchor: msg}, "", true
		}
		return ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: msg}, "", true
	case "follow", "follow_accepted", "friend_accepted":
		if actor != "" {
			return ui.Route{Kind: ui.RouteProfile, ID: actor}, "", true
		}
	case "follow_request":
		return ui.Route{}, overlayFollowRequests, true
	case "friend_request":
		return ui.Route{Kind: ui.RouteFriends}, "", true
	case "space_invite":
		return ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: n.Subject.ID}, "", true
	case "guestbook_entry":
		return ui.Route{Kind: ui.RouteProfile, ID: string(api.MeRef), Query: map[string]string{"tab": "guestbook"}}, "", true
	}
	return ui.Route{}, "", false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// markReadOutcome is a POST /v1/notifications/read: what it marked locally
// (restored on error) and the server's unread count.
type markReadOutcome struct {
	claim
	ids      []string // marked IDs; nil = all
	prevRead map[string]bool
	prevN    int
	unread   int
}

// notifications is RouteNotifications: the inbox, newest first, in the command
// pane's lower right (SlotNotifications) or as the single pane in Main.
type notifications struct {
	list  *widgets.List[api.Notification]
	items []api.Notification // newest first, blocked actors skipped
	size  ui.Size
	spin  *widgets.Spinner
	fade  widgets.Fade

	fadeUntil   time.Time
	cursor      *string
	loading     bool
	loadingMore bool
	loaded      bool
	err         error
	focused     bool // the last View's focus (selection is drawn only when focused)

	listY, listH int // geometry of the last View (-1 / 0 = not drawn)
}

var (
	_ ui.Component    = (*notifications)(nil)
	_ ui.KeyHandler   = (*notifications)(nil)
	_ ui.MouseHandler = (*notifications)(nil)
	_ ui.Titled       = (*notifications)(nil)
	_ ui.Helper       = (*notifications)(nil)
)

// newNotifications builds RouteNotifications.
func newNotifications(_ *ui.Context, _ ui.Route) ui.Component {
	n := &notifications{spin: widgets.NewSpinner(), listY: -1}
	n.list = widgets.NewList(func(x api.Notification) string { return x.ID }, n.renderItem)
	return n
}

func (n *notifications) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init marks the inbox open for resync and loads the first page unless the
// window already holds one.
func (n *notifications) Init(ctx *ui.Context) tea.Cmd {
	fs := n.fs(ctx)
	if fs == nil {
		return nil
	}
	fs.SetOpen(notifOpenKey, true)
	var cmd tea.Cmd
	if fs.Notifications().Len() == 0 {
		cmd = n.load(ctx, "")
	} else {
		n.loaded = true
	}
	n.sync(ctx)
	return cmd
}

// load requests a page (cursor "" = the first page, replacing the window).
func (n *notifications) load(ctx *ui.Context, cursor string) tea.Cmd {
	if n.loading || n.loadingMore {
		return nil
	}
	tag := tagNotifs
	if cursor != "" {
		n.loadingMore, tag = true, tagNotifsMore
	} else {
		n.loading = true
	}
	return tea.Batch(n.spin.Start(ctx), ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.NotificationPage, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListNotifications(c, api.NotificationQuery{PageOpts: api.PageOpts{Cursor: cursor}})
	}))
}

func (n *notifications) maybeMore(ctx *ui.Context) tea.Cmd {
	if n.cursor == nil || len(n.items) == 0 {
		return nil
	}
	if i := n.selectedIndex(); i >= len(n.items)-moreThreshold {
		return n.load(ctx, *n.cursor)
	}
	return nil
}

func (n *notifications) selectedIndex() int {
	sel, ok := n.list.Selected()
	if !ok {
		return -1
	}
	for i, x := range n.items {
		if x.ID == sel.ID {
			return i
		}
	}
	return -1
}

func (n *notifications) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if forwardResync(ctx, msg) {
		if m := msg.(feedstore.ResyncMsg); m.Key == notifOpenKey && m.Notifs != nil {
			n.cursor = m.Notifs.NextCursor
			n.loaded = true
		}
	}
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	var before map[string]bool
	switch m := msg.(type) {
	case ui.ResultMsg[*api.NotificationPage]:
		cmds = append(cmds, n.onPage(ctx, m))
	case ui.ResultMsg[*markReadOutcome]:
		cmds = append(cmds, n.onMarkRead(ctx, m))
	case ui.EventMsg:
		if m.Event.Type == "notification.created" {
			before = make(map[string]bool, len(n.items))
			for _, x := range n.items {
				before[x.ID] = true
			}
		}
	}
	cmds = append(cmds, n.spin.Update(ctx, msg), n.fade.Update(ctx, msg))
	if rt := reflect.TypeOf(msg); rt == nil || rt.PkgPath() != widgetsPkg || ctx.Now().Before(n.fadeUntil) {
		n.sync(ctx) // spinner ticks change nothing in the rows
	}
	if len(before) > 0 { // a list that was empty gets no fade
		for _, x := range n.items {
			if !before[x.ID] {
				if c := n.fade.Start(ctx, x.ID); c != nil {
					n.fadeUntil = ctx.Now().Add(fadeSpan)
					cmds = append(cmds, c)
				}
			}
		}
	}
	return tea.Batch(cmds...)
}

func (n *notifications) onPage(ctx *ui.Context, m ui.ResultMsg[*api.NotificationPage]) tea.Cmd {
	more := false
	switch m.Tag {
	case tagNotifs:
		if !n.loading {
			return nil
		}
		n.loading = false
	case tagNotifsMore:
		if !n.loadingMore {
			return nil
		}
		n.loadingMore, more = false, true
	default:
		return nil
	}
	if !n.loading && !n.loadingMore {
		n.spin.Stop()
	}
	if m.Err != nil || m.Value == nil {
		if !more {
			n.err, n.loaded = m.Err, true
		}
		if m.Err == nil {
			return nil
		}
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextNotif} }
	}
	n.err, n.loaded = nil, true
	fs := n.fs(ctx)
	if fs == nil {
		return nil
	}
	hasMore := m.Value.NextCursor != nil
	if more {
		fs.Notifications().PrependOlder(m.Value.Items, hasMore)
	} else {
		fs.Notifications().Replace(m.Value.Items, hasMore, false)
		ctx.Store.NotifUnread = m.Value.UnreadCount
	}
	n.cursor = m.Value.NextCursor
	if len(m.Value.Items) == 0 {
		n.cursor = nil // an empty page never loops
	}
	return nil
}

// build is the window newest first (by created_at, then ID), blocked actors skipped.
func (n *notifications) build(ctx *ui.Context) []api.Notification {
	fs := n.fs(ctx)
	if fs == nil {
		return nil
	}
	src := fs.Notifications().Items()
	out := make([]api.Notification, 0, len(src))
	for _, x := range src {
		if x.Actor != nil && ctx.Store.IsBlocked(x.Actor.ID) {
			continue
		}
		out = append(out, x)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// sync re-reads the window into the List (the rows are cheap, so every sync
// redraws: read flags and names change in place). A list at the top stays at
// the top, so a new notification shows.
func (n *notifications) sync(ctx *ui.Context) {
	items := n.build(ctx)
	a := n.list.Anchor()
	atTop := len(n.items) == 0 || a.ItemID == "" || (a.ItemID == n.items[0].ID && a.LineOffset == 0)
	n.items = items
	n.list.SetItems(items)
	if atTop && len(items) > 0 {
		n.list.Restore(widgets.ScrollAnchor{ItemID: items[0].ID})
	}
}

// ---- marking read ----

// markRead marks ids (nil = all) read locally at once and sends POST
// /v1/notifications/read; the answer sets Store.NotifUnread, an error restores
// the previous state.
func (n *notifications) markRead(ctx *ui.Context, ids []string) tea.Cmd {
	fs := n.fs(ctx)
	if fs == nil {
		return nil
	}
	o := &markReadOutcome{ids: ids, prevRead: map[string]bool{}, prevN: ctx.Store.NotifUnread}
	w := fs.Notifications()
	marked := 0
	for _, x := range w.Items() {
		if ids != nil && !containsID(ids, x.ID) {
			continue
		}
		o.prevRead[x.ID] = x.Read
		if !x.Read {
			marked++
			w.Update(x.ID, func(p *api.Notification) { p.Read = true })
		}
	}
	tag := tagNotifsRead + "all"
	req := api.MarkNotificationsRead{All: true}
	if ids != nil {
		tag = tagNotifsRead + strings.Join(ids, ",")
		req = api.MarkNotificationsRead{IDs: ids}
		ctx.Store.NotifUnread = max(ctx.Store.NotifUnread-marked, 0)
	} else {
		ctx.Store.NotifUnread = 0
	}
	n.sync(ctx)
	return ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*markReadOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		u, err := a.MarkNotificationsRead(c, req)
		o.unread = u
		return o, err
	})
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func (n *notifications) onMarkRead(ctx *ui.Context, m ui.ResultMsg[*markReadOutcome]) tea.Cmd {
	o := m.Value
	if !strings.HasPrefix(m.Tag, tagNotifsRead) || !o.take() {
		return nil
	}
	if m.Err != nil {
		if fs := n.fs(ctx); fs != nil {
			for id, was := range o.prevRead {
				fs.Notifications().Update(id, func(p *api.Notification) { p.Read = was })
			}
		}
		ctx.Store.NotifUnread = o.prevN
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextNotif} }
	}
	ctx.Store.NotifUnread = o.unread
	return nil
}

// open marks x read (when unread) and goes to its target.
func (n *notifications) open(ctx *ui.Context, x api.Notification) tea.Cmd {
	var cmds []tea.Cmd
	if !x.Read {
		cmds = append(cmds, n.markRead(ctx, []string{x.ID}))
	}
	r, overlay, ok := notifTarget(x)
	switch {
	case !ok:
	case overlay != "":
		cmds = append(cmds, ui.OpenRegisteredOverlay(overlay, ""))
	case r.Kind == ui.RouteInvite && r.ID != "" && !pendingInvite(ctx, r.ID):
		// already answered (or expired): the pending list instead of a dead invite
		cmds = append(cmds, ui.Navigate(ui.Route{Kind: ui.RouteInvite, Arg: "direct"}))
	default:
		cmds = append(cmds, ui.Navigate(r))
	}
	return tea.Batch(cmds...)
}

// pendingInvite reports a direct invite still in Store.DirectInvites.
func pendingInvite(ctx *ui.Context, id string) bool {
	if ctx == nil || ctx.Store == nil {
		return true
	}
	for _, in := range ctx.Store.DirectInvites {
		if in.ID == id {
			return true
		}
	}
	return false
}

// ---- rendering ----

// notifText fills a copy template with the actor and space names (sanitized).
func notifText(tmpl string, x api.Notification) string {
	name := textNotifSomeone
	if x.Actor != nil {
		name = notifLine(x.Actor.DisplayName)
		if name == "" {
			name = notifLine(x.Actor.Handle)
		}
		if name == "" {
			name = textNotifSomeone
		}
	}
	space := notifLine(deref(x.Context.SpaceName))
	if space == "" {
		space = textNotifSpace
	}
	return strings.NewReplacer("{n}", name, "{s}", space).Replace(tmpl)
}

// notifLine is s sanitized with every whitespace run collapsed to one space.
func notifLine(s string) string {
	return strings.Join(strings.Fields(textwidth.Sanitize(s)), " ")
}

func (n *notifications) narrow(ctx *ui.Context) bool {
	d := ctx.Density()
	return n.size.W < notifNarrowWidth || d == ui.DensityLine || d == ui.DensityMinimal
}

func (n *notifications) renderItem(ctx *ui.Context, x api.Notification, selected bool, width int) []string {
	return []string{n.row(ctx, x, selected && n.focused, width)}
}

// row draws one notification in exactly width cells:
//
//	wide:   "  ● ♥ 하나님이 내 글을 좋아해요 · "오늘 첫 눈…"      3분"
//	narrow: "  ♥ 하나님이 좋아함                  3분"
func (n *notifications) row(ctx *ui.Context, x api.Notification, sel bool, width int) string {
	t := themeOf(ctx)
	if width <= 0 {
		return ""
	}
	narrow := n.narrow(ctx)
	cp, ok := notifCopies[x.Type]
	if !ok {
		cp = notifFallback
	}
	tmpl := cp.wide
	if narrow {
		tmpl = cp.narrow
	}
	text := notifText(tmpl, x)
	ts := humanize.Relative(nowIn(ctx), x.CreatedAt)

	gutter := t.S.Base.Render(strings.Repeat(" ", notifGutterW))
	switch {
	case sel:
		gutter = t.S.Base.Foreground(t.P.Acc).Render("▎") + t.S.Selected.Render(" ")
	case ctx.Now().Before(n.fadeUntil):
		gutter = n.fade.Style(t, x.ID).Render(strings.Repeat(" ", notifGutterW))
	}
	cw := width - notifGutterW - notifMarginW
	if cw <= 0 {
		return fit(t, gutter, width)
	}
	dot := ""
	if !narrow {
		dot = "  "
		if !x.Read {
			dot = "● "
		}
		if cw <= notifDotW {
			dot = ""
		}
	}
	avail := cw - textwidth.Width(dot) - textwidth.Width(cp.icon) - 1
	if avail <= 0 {
		return fit(t, gutter+n.paint(t, sel, x.Read, dot, cp.icon, "", "", ""), width)
	}
	tw := textwidth.Width(ts)
	if textwidth.Width(text)+1+tw > avail {
		if avail-tw-1 >= notifTimeMinCells {
			text = textwidth.Truncate(text, avail-tw-1, "…")
		} else {
			ts, tw = "", 0
			text = textwidth.Truncate(text, avail, "…")
		}
	}
	rem := avail - textwidth.Width(text)
	if tw > 0 {
		rem -= tw + 1
	}
	preview := ""
	if p := notifLine(x.Preview); !narrow && p != "" && rem >= notifPreviewMin {
		preview = ` · "` + strings.TrimRight(textwidth.Truncate(p, rem-5, "…"), " ") + `"`
		rem -= textwidth.Width(preview)
	}
	pad := ""
	if tw > 0 {
		pad = strings.Repeat(" ", max(rem, 0)+1)
	}
	return fit(t, gutter+n.paint(t, sel, x.Read, dot, cp.icon, text, preview, pad+ts), width)
}

// paint styles the row parts: on the selection background when sel; else the
// unread dot and icon in acc, the text in Base (Dim once read), preview Dim,
// time Timestamp.
func (n *notifications) paint(t *theme.Theme, sel, read bool, dot, icon, text, preview, tail string) string {
	body := " " + text
	if text == "" {
		body = ""
	}
	if sel {
		return t.S.Selected.Render(dot + icon + body + preview + tail)
	}
	acc := t.S.Base.Foreground(t.P.Acc)
	txt := t.S.Base
	if read {
		acc, txt = t.S.Dim, t.S.Dim
	}
	out := ""
	if dot != "" {
		out += acc.Render(dot)
	}
	out += acc.Render(icon) + txt.Render(body)
	if preview != "" {
		out += t.S.Dim.Render(preview)
	}
	if tail != "" {
		lead := strings.TrimLeft(tail, " ")
		out += t.S.Base.Render(tail[:len(tail)-len(lead)]) + t.S.Timestamp.Render(lead)
	}
	return out
}

// nowIn is ctx.Now() in the display location.
func nowIn(ctx *ui.Context) time.Time {
	if ctx == nil || ctx.Now == nil {
		return time.Time{}
	}
	now := ctx.Now()
	if ctx.Loc != nil {
		now = now.In(ctx.Loc)
	}
	return now
}

func (n *notifications) loadingLine(ctx *ui.Context, w int) string {
	t := themeOf(ctx)
	return fit(t, t.S.Base.Render("  ")+n.spin.View(t)+t.S.Dim.Render(" "+textLoading), w)
}

func (n *notifications) View(ctx *ui.Context, focused bool) string {
	t := themeOf(ctx)
	w, h := n.size.W, n.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	if n.focused != focused {
		n.focused = focused
		n.list.SetItems(n.items) // the selection bar follows the focus
	}
	n.listY, n.listH = -1, 0
	var out []string
	switch {
	case len(n.items) == 0 && (n.loading || !n.loaded):
		out = append(out, n.loadingLine(ctx, w))
	case len(n.items) == 0 && n.err != nil:
		out = strings.Split(widgets.Empty(t, n.size, "⚠", textwidth.Sanitize(api.Describe(n.err)), ""), "\n")
	case len(n.items) == 0:
		out = strings.Split(widgets.Empty(t, n.size, iconNotifEmpty, textNotifEmpty, ""), "\n")
	default:
		footer := 0
		if n.loadingMore {
			footer = 1
		}
		n.listY, n.listH = 0, max(h-footer, 0)
		if n.listH > 0 {
			out = append(out, strings.Split(n.list.View(ctx, ui.Size{W: w, H: n.listH}), "\n")...)
		}
		if footer > 0 {
			for len(out) < n.listH {
				out = append(out, fit(t, "", w))
			}
			out = append(out, n.loadingLine(ctx, w))
		}
	}
	return blankRows(t, out, w, h)
}

func (n *notifications) Resize(_ *ui.Context, size ui.Size) {
	if n.size.W != size.W {
		n.list.SetItems(n.items) // the narrow / wide copy depends on the width
	}
	n.size = size
}

// ---- input ----

func (n *notifications) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if msg.String() == "x" {
		return true, n.markAll(ctx)
	}
	if len(n.items) == 0 {
		return false, nil
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		if x, ok := n.list.Selected(); ok {
			return true, n.open(ctx, x)
		}
		return true, nil
	}
	handled, cmd := n.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, n.maybeMore(ctx))
}

// markAll is x: everything read (nothing is sent when nothing is unread).
func (n *notifications) markAll(ctx *ui.Context) tea.Cmd {
	unread := ctx.Store.NotifUnread > 0
	for _, x := range n.items {
		unread = unread || !x.Read
	}
	if !unread {
		return nil
	}
	return n.markRead(ctx, nil)
}

// rowAt is the notification drawn on list row y: the List's anchor is the top
// row's item after layout, and every notification is one line.
func (n *notifications) rowAt(y int) (api.Notification, bool) {
	if y < 0 || y >= n.listH {
		return api.Notification{}, false
	}
	a := n.list.Anchor()
	start := 0
	for i, x := range n.items {
		if x.ID == a.ItemID {
			start = i
			break
		}
	}
	if i := start + a.LineOffset + y; i >= 0 && i < len(n.items) {
		return n.items[i], true
	}
	return api.Notification{}, false
}

func (n *notifications) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if n.listY < 0 || local.Y < n.listY || local.Y >= n.listY+n.listH {
		return nil
	}
	ly := ui.Point{X: local.X, Y: local.Y - n.listY}
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		n.list.HandleMouse(ctx, msg, ly)
		return n.maybeMore(ctx)
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		x, ok := n.rowAt(ly.Y)
		if !ok {
			return nil
		}
		n.list.Select(x.ID)
		return n.open(ctx, x)
	}
	return nil
}

// ---- optional capabilities ----

// Title is "알림" with the unread count.
func (n *notifications) Title(ctx *ui.Context) (string, ui.Badge) {
	b := ui.Badge{}
	if ctx != nil && ctx.Store != nil {
		b.Count = ctx.Store.NotifUnread
	}
	return textNotifTitle, b
}

func (n *notifications) Help(ctx *ui.Context) []key.Binding {
	open := ctx.Keys.Binding(keymap.ListOpen)
	open.SetHelp(open.Help().Key, "열기")
	return []key.Binding{open, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "모두 읽음"))}
}
