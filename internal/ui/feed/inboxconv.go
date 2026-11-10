package feed

import (
	"sort"
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// The 알림함's "안 읽은 대화" section (client-architecture.md, 알림함). DMs and
// channel messages make no inbox rows (api.md 5.18: alerts only), so the
// section is derived on the client: one row per conversation with unread
// messages (store.UnreadConversations), plus one row per thread with unread
// thread notifications (there is no per-thread read state; the thread_reply /
// message_mention rows stand in for it). Enter jumps to the first unread place.

// Section copy.
const (
	iconConvDM      = "✉"
	iconConvChannel = "#"
	iconConvThread  = "↩"
	convPreviewMax  = 200 // cells of a message body kept for the preview
	convIDPrefix    = "conv:"
	convThreadIDPre = "conv:thread:"
)

// convRow is one row of the section.
type convRow struct {
	key     string // "conv:<channel>" | "conv:thread:<root>"
	icon    string
	title   string // unsanitized
	author  string // preview prefix ("민지님", "나"); "" = none
	body    string // preview text; "" = feed.conv_new(count)
	count   int
	mention bool
	at      time.Time
	route   ui.Route
	notifs  []string // thread rows: the unread notifications Enter marks read
}

// inboxRow is one 알림함 row: a conversation or a notification. head draws the
// section header ("안 읽은 대화" / "알림") as an extra first line.
type inboxRow struct {
	conv *convRow
	n    api.Notification
	head string
}

func (r inboxRow) id() string {
	if r.conv != nil {
		return r.conv.key
	}
	return r.n.ID
}

// joinRows is the section (header on its first row) then the notifications
// (with the "알림" header only when the section is shown). A notification a
// thread row groups is shown there only; once read it is back in the list.
func joinRows(ctx *ui.Context, convs []convRow, items []api.Notification) []inboxRow {
	out := make([]inboxRow, 0, len(convs)+len(items))
	heads := len(convs) > 0 && ctx.Density() != ui.DensityMinimal
	grouped := map[string]bool{}
	for i := range convs {
		r := inboxRow{conv: &convs[i]}
		if i == 0 && heads {
			r.head = cat(ctx).Feed.ConvHeader()
		}
		out = append(out, r)
		for _, id := range convs[i].notifs {
			grouped[id] = true
		}
	}
	first := true
	for _, x := range items {
		if grouped[x.ID] {
			continue
		}
		r := inboxRow{n: x}
		if first && heads {
			r.head = cat(ctx).Feed.NotifHeader()
		}
		first = false
		out = append(out, r)
	}
	return out
}

// buildConvs is the section, newest first: unread conversations from the
// Store, then threads from items (the shown notifications, newest first).
func buildConvs(ctx *ui.Context, items []api.Notification) []convRow {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	s := ctx.Store
	var out []convRow
	for _, c := range s.UnreadConversations() {
		ch := s.Channels[c.ChannelID]
		if ch == nil {
			continue
		}
		r := convRow{key: convIDPrefix + ch.ID, icon: iconConvDM, title: convTitle(ctx, ch),
			count: max(c.Unread, c.Mentions), mention: c.Mentions > 0, at: c.LastAt,
			route: ui.Route{Kind: ui.RouteChannel, ID: ch.ID}}
		if ch.Kind == api.ChannelText {
			r.icon = iconConvChannel
		}
		if c.Anchor != "" {
			r.route.Anchor = c.Anchor
			r.route.Query = map[string]string{ui.QueryJump: ui.JumpUnread}
		}
		if m, ok := convLast(ctx, ch, c.Anchor); ok {
			r.author, r.body = convAuthor(ctx, ch, m.Author), convBody(cat(ctx), m)
			if !m.CreatedAt.IsZero() && m.CreatedAt.After(r.at) {
				r.at = m.CreatedAt
			}
		} else if a, ok := convAlert(ctx, ch.ID, c.Anchor); ok {
			if a.Actor != nil {
				r.author = convAuthor(ctx, ch, *a.Actor)
			}
			r.body = a.Body
		}
		out = append(out, r)
	}
	out = append(out, threadConvs(ctx, items)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.After(out[j].at) })
	return out
}

// threadConvs groups the unread thread notifications (thread_reply, and
// message_mention inside a thread) by thread root. Threads of muted or
// notify-"none" channels are left out like those channels (store.Quiet); the
// server writes their rows regardless, so they stay in the 알림 list.
func threadConvs(ctx *ui.Context, items []api.Notification) []convRow {
	s := ctx.Store
	var out []convRow
	at := map[string]int{}
	for _, x := range items { // newest first
		root, chID := deref(x.Context.ThreadRootID), deref(x.Context.ChannelID)
		if x.Read || root == "" || chID == "" || (x.Type != "thread_reply" && x.Type != "message_mention") {
			continue
		}
		if ch := s.Channels[chID]; ch != nil && store.Quiet(ch) {
			continue
		}
		key := convThreadIDPre + root
		if i, ok := at[key]; ok {
			r := &out[i]
			r.count++
			r.mention = r.mention || x.Type == "message_mention"
			r.notifs = append(r.notifs, x.ID)
			if id := deref(x.Context.MessageID); id != "" {
				r.route.Anchor = id // older: the jump goes to the oldest unread reply
			}
			continue
		}
		base := cat(ctx).Feed.NotifSomeone()
		if ch := s.Channels[chID]; ch != nil {
			base = convTitle(ctx, ch)
			if ch.Kind == api.ChannelText { // "#개발": the space name would crowd out the preview
				base = iconConvChannel + notifLine(deref(ch.Name))
			}
		}
		r := convRow{key: key, icon: iconConvThread, title: cat(ctx).Feed.ConvThread() + " · " + base,
			body: x.Preview, count: 1, mention: x.Type == "message_mention", at: x.CreatedAt,
			route:  ui.Route{Kind: ui.RouteThread, ID: root, Arg: chID, Anchor: deref(x.Context.MessageID)},
			notifs: []string{x.ID}}
		if x.Actor != nil {
			r.author = convAuthor(ctx, s.Channels[chID], *x.Actor)
		}
		at[key] = len(out)
		out = append(out, r)
	}
	return out
}

// personName is a user's shown name (display name, else handle); ko adds 님.
func personName(ctx *ui.Context, u api.UserSummary) string {
	if c := ctx.Store.User(u.ID); c != nil {
		u = *c
	}
	name := notifLine(u.DisplayName)
	if textwidth.Blank(name) {
		name = notifLine(u.Handle)
	}
	if name == "" {
		return cat(ctx).Feed.NotifSomeone()
	}
	return cat(ctx).Feed.ConvPerson(name)
}

// convTitle: a DM "민지님", a group DM its name (else "민지님, 준호님"), a text
// channel "일반 · 노바 개발방" (the row's # icon in front).
func convTitle(ctx *ui.Context, ch *api.Channel) string {
	s := ctx.Store
	switch ch.Kind {
	case api.ChannelText:
		name := notifLine(deref(ch.Name))
		if ch.SpaceID != nil {
			if sp := s.Spaces[*ch.SpaceID]; sp != nil {
				if sn := notifLine(sp.Name); sn != "" {
					return name + " · " + sn
				}
			}
		}
		return name
	case api.ChannelGroupDM:
		if n := notifLine(deref(ch.Name)); n != "" {
			return n
		}
	}
	var names []string
	for _, m := range ch.Members {
		if !s.IsMe(m.ID) {
			names = append(names, personName(ctx, m))
		}
	}
	if len(names) == 0 {
		return cat(ctx).Feed.ConvMe()
	}
	return strings.Join(names, ", ")
}

// convAuthor is the preview's "민지님" prefix: none in a 1:1 DM unless it is
// my own message ("나").
func convAuthor(ctx *ui.Context, ch *api.Channel, u api.UserSummary) string {
	switch {
	case ctx.Store.IsMe(u.ID):
		return cat(ctx).Feed.ConvMe()
	case ch != nil && ch.Kind == api.ChannelDM:
		return ""
	}
	return personName(ctx, u)
}

// convBody is a message's one-line preview text.
func convBody(c *i18n.T, m api.Message) string {
	if m.Poll != nil && strings.TrimSpace(m.Body) == "" {
		return c.Feed.ConvPoll() + " · " + m.Poll.Question
	}
	return markup.Plain(m.Body, convPreviewMax, c.Common.CodeSummary)
}

// convLast is the newest known top-level message of ch after the read pointer:
// the one the Store saw this session, or the end of a loaded chat window.
// Messages of blocked users are not previewed.
func convLast(ctx *ui.Context, ch *api.Channel, anchor string) (api.Message, bool) {
	s := ctx.Store
	m, ok := s.LastMessage(ch.ID)
	if c := chatstore.Get(s); c != nil && c.Loaded(ch.ID) {
		items := c.Window(ch.ID).Items()
		for i := len(items) - 1; i >= 0; i-- {
			w := items[i]
			if w.ThreadRootID != nil || w.Kind == "system" || w.Deleted {
				continue
			}
			if !ok || w.ID > m.ID {
				m, ok = w, true
			}
			break
		}
	}
	if !ok || (anchor != "" && m.ID <= anchor) || s.IsBlocked(m.Author.ID) {
		return api.Message{}, false
	}
	return m, true
}

// convAlert is the newest alert about a top-level message of the channel (a
// message that arrived while the window was not loaded) that is still unread
// (after the read pointer anchor) and not deleted since.
func convAlert(ctx *ui.Context, channelID, anchor string) (api.Alert, bool) {
	s := ctx.Store
	if s.Alerts == nil {
		return api.Alert{}, false
	}
	for _, a := range s.Alerts.Items() { // newest first
		if deref(a.Target.ChannelID) != channelID || a.Target.ThreadRootID != nil {
			continue
		}
		if id := deref(a.Target.MessageID); id != "" && ((anchor != "" && id <= anchor) || s.MessageDeleted(id)) {
			continue
		}
		if a.Actor != nil && s.IsBlocked(a.Actor.ID) {
			return api.Alert{}, false
		}
		return a, true
	}
	return api.Alert{}, false
}

// inboxBadge is the 알림함's unread count (the palette command's badge).
func inboxBadge(ctx *ui.Context) ui.Badge {
	if ctx == nil || ctx.Store == nil {
		return ui.Badge{}
	}
	return ui.Badge{Count: ctx.Store.InboxBadge()}
}

// ---- rendering ----

// renderRow draws one 알림함 row (with its section header line, if any).
func (n *notifications) renderRow(ctx *ui.Context, r inboxRow, selected bool, width int) []string {
	var line string
	if r.conv != nil {
		line = n.convLine(ctx, *r.conv, selected && n.focused, width)
	} else {
		line = n.row(ctx, r.n, selected && n.focused, width)
	}
	if r.head == "" {
		return []string{line}
	}
	t := themeOf(ctx)
	head := fit(t, t.S.Base.Render(strings.Repeat(" ", notifGutterW))+t.S.Subtitle.Render(textwidth.Truncate(r.head, max(width-notifGutterW, 0), "…")), width)
	return []string{head, line}
}

// convLine draws one conversation in exactly width cells:
//
//	wide:   "  ● ✉ 민지님 · "저녁 뭐 먹지?"                 2  3분"
//	narrow: "  ✉ 민지님                                   2  3분"
func (n *notifications) convLine(ctx *ui.Context, r convRow, sel bool, width int) string {
	t := themeOf(ctx)
	if width <= 0 {
		return ""
	}
	narrow := n.narrow(ctx)
	gutter := t.S.Base.Render(strings.Repeat(" ", notifGutterW))
	if sel {
		gutter = t.S.Base.Foreground(t.P.Acc).Render("▎") + t.S.Selected.Render(" ")
	}
	cw := width - notifGutterW - notifMarginW
	if cw <= 0 {
		return fit(t, gutter, width)
	}
	dot := ""
	if !narrow && cw > notifDotW {
		dot = "● "
	}
	avail := cw - textwidth.Width(dot) - textwidth.Width(r.icon) - 1
	if avail <= 0 {
		return fit(t, gutter+n.paintConv(t, sel, dot, r.icon, "", "", "", "", ""), width)
	}
	title := r.title
	ts := hz(ctx).Relative(nowIn(ctx), r.at)
	badge := widgets.Badge(t, ui.Badge{Count: r.count, Mention: r.mention})
	bw, tw := textwidth.Width(badge), textwidth.Width(ts)
	tail := func() int { // cells of " badge time" right of the text
		w := 0
		if bw > 0 {
			w += 1 + bw
		}
		if tw > 0 {
			w += 1 + tw
		}
		return w
	}
	if textwidth.Width(title)+tail() > avail {
		if avail-tail() < notifTimeMinCells {
			ts, tw = "", 0
		}
		if avail-tail() < notifTimeMinCells {
			badge, bw = "", 0
		}
		title = textwidth.Truncate(title, max(avail-tail(), 0), "…")
	}
	rem := avail - textwidth.Width(title) - tail()
	preview := ""
	if !narrow && rem >= notifPreviewMin {
		body := notifLine(r.body)
		if r.author != "" && body != "" {
			body = r.author + ": " + body
		}
		if body != "" {
			preview = ` · "` + strings.TrimRight(textwidth.Truncate(body, rem-5, "…"), " ") + `"`
		} else {
			preview = " · " + textwidth.Truncate(cat(ctx).Feed.ConvNew(r.count), rem-3, "…")
		}
		rem -= textwidth.Width(preview)
	}
	pad := strings.Repeat(" ", max(rem, 0))
	if bw > 0 {
		pad += " "
	}
	return fit(t, gutter+n.paintConv(t, sel, dot, r.icon, title, preview, pad, badge, ts), width)
}

// paintConv styles a conversation row like an unread notification row: dot and
// icon in acc, title Base, preview Dim, then the count badge and the time
// (Timestamp); on the selection background when sel (the badge keeps its style).
func (n *notifications) paintConv(t *theme.Theme, sel bool, dot, icon, title, preview, pad, badge, ts string) string {
	body := ""
	if title != "" {
		body = " " + title
	}
	if ts != "" {
		ts = " " + ts
	}
	if sel {
		out := t.S.Selected.Render(dot + icon + body + preview + pad)
		return out + badge + t.S.Selected.Render(ts)
	}
	acc := t.S.Base.Foreground(t.P.Acc)
	out := ""
	if dot != "" {
		out += acc.Render(dot)
	}
	out += acc.Render(icon) + t.S.Base.Render(body)
	if preview != "" {
		out += t.S.Dim.Render(preview)
	}
	out += t.S.Base.Render(pad) + badge
	if ts != "" {
		out += t.S.Base.Render(" ") + t.S.Timestamp.Render(ts[1:])
	}
	return out
}
