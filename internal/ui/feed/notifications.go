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
	textNotifLoadFail = "알림을 불러오지 못했어요"
	iconNotifEmpty    = "🔔"
	textNotifSomeone  = "알 수 없음"
	textNotifSpace    = "방"
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
		return ui.Route{Kind: ui.RouteFriends, Anchor: ui.FriendsIncoming}, "", true
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
// pane's lower right (SlotNotifications) or as the single pane in Main. The
// "안 읽은 대화" section (inboxconv.go) comes before the notifications.
type notifications struct {
	list  *widgets.List[inboxRow]
	items []api.Notification // newest first, blocked actors skipped
	convs []convRow          // the 안 읽은 대화 section, newest first
	rows  []inboxRow         // what the List shows: convs, then items
	size  ui.Size
	spin  *widgets.Spinner
	fade  widgets.Fade

	fadeUntil   time.Time
	loading     bool
	loadingMore bool
	loaded      bool
	err         error // the last first-page request failed (retry notice; x is off)
	quietFail   bool  // the first-page request in flight is an automatic retry: a failure does not toast
	focused     bool  // the last View's focus (selection is drawn only when focused)

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
	n.list = widgets.NewList(func(x inboxRow) string { return x.id() }, n.renderRow)
	return n
}

func (n *notifications) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init marks the inbox open for resync and loads the first page unless one
// already filled the window (items that only notification.created added do
// not count: the inbox may be built after they arrived).
func (n *notifications) Init(ctx *ui.Context) tea.Cmd {
	fs := n.fs(ctx)
	if fs == nil {
		return nil
	}
	fs.SetOpen(notifOpenKey, true)
	var cmd tea.Cmd
	if !fs.NotificationsLoaded() {
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
		n.loading, n.quietFail = true, false
	}
	return tea.Batch(n.spin.Start(ctx), ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.NotificationPage, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListNotifications(c, api.NotificationQuery{PageOpts: api.PageOpts{Cursor: cursor}})
	}))
}

// key is the inbox's route key (FocusChangedMsg).
func (n *notifications) key() string { return ui.Route{Kind: ui.RouteNotifications}.Key() }

// retry re-requests a failed first page (nil unless the last one failed and
// none is in flight): r, being shown again, and a successful resync — as the
// timeline and the profile do. Automatic retries (quiet) fail silently: the
// pane keeps its retry notice.
func (n *notifications) retry(ctx *ui.Context, quiet bool) tea.Cmd {
	if n.err == nil || n.loading {
		return nil
	}
	cmd := n.load(ctx, "")
	if n.loading {
		n.quietFail = quiet
	}
	return cmd
}

// pageLoaded reports whether a first page (or a resync's) filled the window:
// x marks everything read only then (not unseen notifications).
func (n *notifications) pageLoaded() bool { return n.loaded && n.err == nil }

// maybeMore loads the next older page (FeedStore keeps its cursor, so a
// rebuilt inbox still pages) once the selection nears the end.
func (n *notifications) maybeMore(ctx *ui.Context) tea.Cmd {
	fs := n.fs(ctx)
	if fs == nil || fs.NotificationsCursor() == nil || len(n.items) == 0 {
		return nil
	}
	if i := n.selectedIndex(); i >= len(n.items)-moreThreshold {
		return n.load(ctx, *fs.NotificationsCursor())
	}
	return nil
}

func (n *notifications) selectedIndex() int {
	sel, ok := n.list.Selected()
	if !ok || sel.conv != nil {
		return -1
	}
	for i, x := range n.items {
		if x.ID == sel.n.ID {
			return i
		}
	}
	return -1
}

func (n *notifications) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if forwardResync(ctx, msg) {
		if m := msg.(feedstore.ResyncMsg); m.Key == notifOpenKey && m.Notifs != nil {
			n.loaded = true
			if fs := n.fs(ctx); fs != nil && fs.NotificationsLoaded() {
				n.err = nil // a resync page replaced the failed one
			}
		}
	}
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	var before map[string]bool
	switch m := msg.(type) {
	case ui.ResultMsg[*api.NotificationPage]:
		cmds = append(cmds, n.onPage(ctx, m))
	case ui.ResultMsg[*markReadOutcome]:
		cmds = append(cmds, n.onMarkRead(ctx, m))
	case ui.FocusChangedMsg:
		if m.RouteKey == n.key() {
			cmds = append(cmds, n.retry(ctx, true)) // shown again (reopened)
		}
	case ui.RouteUpdatedMsg:
		cmds = append(cmds, n.retry(ctx, true))
	case ui.ResyncDoneMsg:
		if m.Err == nil { // a failed bootstrap (offline) retries itself every 10s
			cmds = append(cmds, n.retry(ctx, true))
		}
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
		if m.Err == nil || (!more && n.quietFail) {
			return nil
		}
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextNotif} }
	}
	n.err, n.loaded = nil, true
	fs := n.fs(ctx)
	if fs == nil {
		return nil
	}
	if more {
		fs.AddOlderNotifications(m.Value)
	} else {
		fs.SetNotificationsPage(m.Value)
		ctx.Store.NotifUnread = m.Value.UnreadCount
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

// sync re-reads the window and the unread conversations into the List (the
// rows are cheap, so every sync redraws: read flags and names change in place).
// A list at the top stays at the top, so a new notification shows.
func (n *notifications) sync(ctx *ui.Context) {
	items := n.build(ctx)
	convs := buildConvs(ctx, items)
	rows := joinRows(ctx, convs, items)
	a := n.list.Anchor()
	atTop := len(n.rows) == 0 || a.ItemID == "" || (a.ItemID == n.rows[0].id() && a.LineOffset == 0)
	n.items, n.convs, n.rows = items, convs, rows
	n.list.SetItems(rows)
	if atTop && len(rows) > 0 {
		n.list.Restore(widgets.ScrollAnchor{ItemID: rows[0].id()})
	}
}

// syncConvs re-syncs when the section changed since the last sync: a read can
// reach the Store outside this view's Update (a channel view's local read
// result is broadcast to it after the inbox already ran), so View checks.
func (n *notifications) syncConvs(ctx *ui.Context) {
	if !reflect.DeepEqual(buildConvs(ctx, n.items), n.convs) {
		n.sync(ctx)
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
// The server names the space only for space_invite (api.md 5.18); otherwise
// x.Context.SpaceName is nil and the caller passes x through withSpaceName.
func notifText(tmpl string, x api.Notification) string {
	name := textNotifSomeone
	if x.Actor != nil {
		name = notifLine(x.Actor.DisplayName)
		if textwidth.Blank(name) { // nothing visible (pending-fixes 1)
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
	text := notifText(tmpl, withSpaceName(ctx, x))
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
	n.syncConvs(ctx)
	if n.focused != focused {
		n.focused = focused
		n.list.SetItems(n.rows) // the selection bar follows the focus
	}
	n.listY, n.listH = -1, 0
	var out []string
	switch {
	case len(n.rows) == 0 && (n.loading || !n.loaded):
		out = append(out, n.loadingLine(ctx, w))
	case len(n.rows) == 0 && n.err != nil:
		out = strings.Split(widgets.Empty(t, n.size, "⚠", retryHint(ctx, textNotifLoadFail), ""), "\n")
	case len(n.rows) == 0:
		out = strings.Split(widgets.Empty(t, n.size, iconNotifEmpty, textNotifEmpty, ""), "\n")
	case !n.listsNotifs():
		// the section alone: the notifications' state (loading / failed / none) right under it
		foot := n.sectionFoot(ctx, w)
		n.listY, n.listH = 0, min(max(h-len(foot), 0), n.contentLines())
		if n.listH > 0 {
			out = append(out, strings.Split(n.list.View(ctx, ui.Size{W: w, H: n.listH}), "\n")...)
		}
		out = append(out, foot...)
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

// listsNotifs reports whether any notification row is shown (the section may
// group every unread one).
func (n *notifications) listsNotifs() bool {
	for _, r := range n.rows {
		if r.conv == nil {
			return true
		}
	}
	return false
}

// contentLines is how many lines the rows take (one each, plus section headers).
func (n *notifications) contentLines() int {
	c := len(n.rows)
	for _, r := range n.rows {
		if r.head != "" {
			c++
		}
	}
	return c
}

// sectionFoot is drawn under the 안 읽은 대화 section while no notification is
// shown: the "알림" header, then loading, the retry notice or "새 알림이 없어요".
func (n *notifications) sectionFoot(ctx *ui.Context, w int) []string {
	t := themeOf(ctx)
	indent := t.S.Base.Render(strings.Repeat(" ", notifGutterW))
	var out []string
	if ctx.Density() != ui.DensityMinimal {
		out = append(out, fit(t, indent+t.S.Subtitle.Render(textNotifHeader), w))
	}
	switch {
	case n.loading || !n.loaded:
		out = append(out, n.loadingLine(ctx, w))
	case n.err != nil:
		out = append(out, fit(t, indent+t.S.Dim.Render("⚠ "+retryHint(ctx, textNotifLoadFail)), w))
	default:
		out = append(out, fit(t, indent+t.S.Dim.Render(textNotifEmpty), w))
	}
	return out
}

func (n *notifications) Resize(_ *ui.Context, size ui.Size) {
	if n.size.W != size.W {
		n.list.SetItems(n.rows) // the narrow / wide copy depends on the width
	}
	n.size = size
}

// ---- input ----

func (n *notifications) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if n.err != nil && ctx.Keys.Matches(msg, keymap.PostReply) {
		return true, n.retry(ctx, false)
	}
	if msg.String() == "x" {
		if !n.pageLoaded() {
			return true, nil // nothing seen to mark
		}
		return true, n.markAll(ctx)
	}
	if len(n.rows) == 0 {
		return false, nil
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		if x, ok := n.list.Selected(); ok {
			return true, n.openRow(ctx, x)
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

// openRow opens a conversation (marking a thread's notifications read) or a notification.
func (n *notifications) openRow(ctx *ui.Context, r inboxRow) tea.Cmd {
	if r.conv == nil {
		return n.open(ctx, r.n)
	}
	var cmds []tea.Cmd
	if len(r.conv.notifs) > 0 {
		cmds = append(cmds, n.markRead(ctx, r.conv.notifs))
	}
	return tea.Batch(append(cmds, ui.Navigate(r.conv.route))...)
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
		x, ok := n.list.At(ly.Y)
		if !ok {
			return nil
		}
		n.list.Select(x.id())
		return n.openRow(ctx, x)
	}
	return nil
}

// ---- optional capabilities ----

// Title is "알림" with the unread count: notifications plus the unread
// conversations no notification counts (Store.InboxBadge).
func (n *notifications) Title(ctx *ui.Context) (string, ui.Badge) {
	b := ui.Badge{}
	if ctx != nil && ctx.Store != nil {
		b.Count = ctx.Store.InboxBadge()
	}
	return textNotifTitle, b
}

func (n *notifications) Help(ctx *ui.Context) []key.Binding {
	open := ctx.Keys.Binding(keymap.ListOpen)
	open.SetHelp(open.Help().Key, "열기")
	if n.err != nil && len(n.rows) == 0 {
		r := ctx.Keys.Binding(keymap.PostReply)
		r.SetHelp(r.Help().Key, textRetry)
		return []key.Binding{r}
	}
	if !n.pageLoaded() {
		return []key.Binding{open}
	}
	return []key.Binding{open, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "모두 읽음"))}
}

// withSpaceName fills a missing context.space_name with the name of the space
// the client knows: by context.space_id, else by the space of
// context.channel_id. Unknown spaces stay nil (the copy then says 방).
func withSpaceName(ctx *ui.Context, x api.Notification) api.Notification {
	if x.Context.SpaceName != nil || ctx == nil || ctx.Store == nil {
		return x
	}
	id := deref(x.Context.SpaceID)
	if id == "" && x.Context.ChannelID != nil {
		if ch := ctx.Store.Channels[*x.Context.ChannelID]; ch != nil {
			id = deref(ch.SpaceID)
		}
	}
	if sp := ctx.Store.Spaces[id]; id != "" && sp != nil {
		name := sp.Name
		x.Context.SpaceName = &name
	}
	return x
}
