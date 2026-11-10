package chat

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteFriends, newFriends)
	ui.RegisterOverlay(overlayFriendAdd, newFriendAdd)
	palette.Register(palette.Command{
		ID: "friends", Label: func(t *i18n.T) string { return t.Chat.CmdFriends() }, Group: palette.GroupNav,
		Keywords:   []string{"friends"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwFriends()) },
		Run:        func(*ui.Context, string) tea.Cmd { return ui.Navigate(ui.Route{Kind: ui.RouteFriends}) },
	})
	palette.Register(palette.Command{
		ID: "friend.add", Label: func(t *i18n.T) string { return t.Chat.CmdFriendAdd() }, Group: palette.GroupCommand,
		Keywords:   []string{"friend", "add"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwFriendAdd()) },
		Run:        func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlayFriendAdd, args) },
	})
}

// overlayFriendAdd is the registered friend.add picker (args = the first query).
const overlayFriendAdd = "chat.friend_add"

// Friends tabs.
const (
	tabFriends = iota
	tabIncoming
	tabOutgoing
	friendTabs
)

// Friends texts.
const (
	tagFriendOut       = "chat.friend_out:me"
	tagFriendAddPrefix = "chat.friend_add:"
)

// friendKind is the kind of a friends row.
type friendKind int

const (
	friendAddRow  friendKind = iota // "＋ 친구 추가"
	friendUserRow                   // a friend
	friendInRow                     // a received request
	friendOutRow                    // a sent request
)

// friendItem is one friends row (one line). Label is the sanitized display name
// (the add row's text for the add row), Handle the sanitized handle, Detail the
// status (friends) or the request age (requests).
type friendItem struct {
	ID, Label, Handle, Detail string

	kind     friendKind
	user     api.UserSummary
	req      api.FriendRequest
	presence api.PresenceStatus
}

// friendsSaved is the Persistent state of the friends route.
type friendsSaved struct {
	Tab      int
	Selected [friendTabs]string
}

// friendOutResult is one outgoing-requests fetch (gen = the fetch generation).
type friendOutResult struct {
	gen   int
	items []api.FriendRequest
}

// friendsRefreshMsg asks the friends route to fetch the sent requests again.
type friendsRefreshMsg struct{}

func friendsRefresh() tea.Msg { return friendsRefreshMsg{} }

// friendsView is RouteFriends: the tabs 친구 / 받은 요청 / 보낸 요청. Friends and
// received requests are the Store's; sent requests are fetched on entry, on
// every friend.request_* event, after friend.add / a cancel here, and on
// ui.FriendRequestsChangedMsg (a request sent or canceled from a profile).
type friendsView struct {
	tab     int
	tabs    *widgets.Tabs
	list    *widgets.List[friendItem]
	items   []friendItem
	sel     [friendTabs]string // remembered selection per tab
	size    ui.Size
	focused bool

	out       []api.FriendRequest
	outGen    int
	outFailed bool

	awaiting map[string]int // direct invites in flight by result tag

	anchorTab int // the tab the route's Anchor asks for (-1 = none); wins over LoadState
}

var (
	_ ui.Component    = (*friendsView)(nil)
	_ ui.KeyHandler   = (*friendsView)(nil)
	_ ui.MouseHandler = (*friendsView)(nil)
	_ ui.Titled       = (*friendsView)(nil)
	_ ui.Helper       = (*friendsView)(nil)
	_ ui.Persistent   = (*friendsView)(nil)
)

// newFriends is the RouteFriends factory.
func newFriends(ctx *ui.Context, r ui.Route) ui.Component {
	v := &friendsView{tabs: widgets.NewTabs(), awaiting: map[string]int{}, anchorTab: -1}
	v.list = widgets.NewList(func(it friendItem) string { return it.ID }, v.render)
	if tab, ok := anchorTab(r.Anchor); ok {
		v.tab, v.anchorTab = tab, tab
	}
	v.sync(ctx)
	return v
}

// anchorTab is the tab a RouteFriends Anchor opens (C1: a friend request leads
// to 받은 요청); ok=false keeps the current / remembered tab.
func anchorTab(anchor string) (int, bool) {
	switch anchor {
	case ui.FriendsList:
		return tabFriends, true
	case ui.FriendsIncoming:
		return tabIncoming, true
	}
	return 0, false
}

// ---- rows ----

// friendList is my friends (blocked users never), online first, then by name.
func friendList(ctx *ui.Context) []friendItem {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	s := ctx.Store
	var out []friendItem
	for id, f := range s.Friends {
		if s.IsBlocked(id) {
			continue
		}
		u := f.User
		if cu := s.User(id); cu != nil {
			u = *cu
		}
		out = append(out, friendItem{
			ID: "user:" + id, Label: strings.TrimSpace(textwidth.Sanitize(displayName(u))), Handle: textwidth.Sanitize(u.Handle),
			Detail: statusText(ctx, u.Status), kind: friendUserRow, user: u, presence: userPresence(ctx, id),
		})
	}
	slices.SortFunc(out, func(a, b friendItem) int {
		return cmp.Or(
			cmp.Compare(presenceRank(a.presence), presenceRank(b.presence)),
			strings.Compare(a.Label, b.Label),
			strings.Compare(a.ID, b.ID),
		)
	})
	return out
}

// requestItems are request rows (blocked users never) in the given order: the
// other side's name and handle, the request age.
func requestItems(ctx *ui.Context, reqs []api.FriendRequest, kind friendKind) []friendItem {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	var out []friendItem
	for _, r := range reqs {
		u := r.From
		if kind == friendOutRow {
			u = r.To
		}
		if ctx.Store.IsBlocked(u.ID) {
			continue
		}
		age := ""
		if ctx.Now != nil && !r.CreatedAt.IsZero() {
			age = hz(ctx).Relative(ctx.Now(), r.CreatedAt)
		}
		out = append(out, friendItem{
			ID: "req:" + r.ID, Label: strings.TrimSpace(textwidth.Sanitize(displayName(u))), Handle: textwidth.Sanitize(u.Handle),
			Detail: age, kind: kind, user: u, req: r,
		})
	}
	return out
}

// tabItems are the rows of tab (the friends tab starts with the add row).
func (v *friendsView) tabItems(ctx *ui.Context, tab int) []friendItem {
	switch tab {
	case tabIncoming:
		if ctx == nil || ctx.Store == nil {
			return nil
		}
		return requestItems(ctx, ctx.Store.FriendReqIn, friendInRow)
	case tabOutgoing:
		return requestItems(ctx, v.out, friendOutRow)
	}
	return append([]friendItem{{ID: "add", Label: cat(ctx).Chat.FriendAddLabel(), kind: friendAddRow}}, friendList(ctx)...)
}

// counts are the tab counts: friends, received, sent (blocked users excluded).
func (v *friendsView) counts(ctx *ui.Context) [friendTabs]int {
	return [friendTabs]int{
		len(friendList(ctx)),
		len(v.tabItems(ctx, tabIncoming)),
		len(v.tabItems(ctx, tabOutgoing)),
	}
}

// sync rebuilds the tab bar and the rows of the current tab (the list keeps the
// selection by ID).
func (v *friendsView) sync(ctx *ui.Context) {
	n, c := v.counts(ctx), cat(ctx)
	v.tabs.Set([]widgets.Tab{
		{Label: c.Chat.FriendsTabFriends(n[tabFriends])},
		{Label: c.Chat.FriendsTabIncoming(n[tabIncoming])},
		{Label: c.Chat.FriendsTabOutgoing(n[tabOutgoing])},
	}, v.tab)
	v.items = v.tabItems(ctx, v.tab)
	v.list.SetItems(v.items)
	if id := v.sel[v.tab]; id != "" && slices.ContainsFunc(v.items, func(it friendItem) bool { return it.ID == id }) {
		v.list.Select(id)
	}
	if it, ok := v.list.Selected(); ok {
		v.sel[v.tab] = it.ID
	}
}

// setTab switches to tab (clamped), remembering the selection of the old one.
func (v *friendsView) setTab(ctx *ui.Context, tab int) {
	tab = max(0, min(friendTabs-1, tab))
	if it, ok := v.list.Selected(); ok {
		v.sel[v.tab] = it.ID
	}
	v.tab = tab
	v.sync(ctx)
}

// ---- drawing ----

// render draws a row: column 0 is the focus cursor "›", then the content.
func (v *friendsView) render(ctx *ui.Context, it friendItem, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && v.focused {
		cur = t.S.Title.Render("›")
	}
	cw := width - 1
	if cw <= 0 {
		return []string{cur}
	}
	if it.kind == friendAddRow {
		return []string{cur + cell(t.S.Title.Render, it.Label, cw)}
	}
	return []string{cur + personRow(t, it, it.kind == friendUserRow, cw)}
}

// personRow is "[<dot> ]이름 @handle · detail" in exactly cw cells: the detail
// goes first when room runs short, then the handle.
func personRow(t *theme.Theme, it friendItem, dot bool, cw int) string {
	lead, leadW := "", 0
	if dot && cw > 2 {
		lead, leadW = widgets.PresenceDot(t, it.presence)+t.S.Base.Render(" "), 2
	}
	room := cw - leadW
	handle := ""
	if it.Handle != "" {
		handle = " @" + it.Handle
	}
	detail := ""
	if it.Detail != "" {
		detail = " · " + it.Detail
	}
	nameW := textwidth.Width(it.Label)
	if nameW+textwidth.Width(handle)+textwidth.Width(detail) > room {
		detail = ""
	}
	if nameW+textwidth.Width(handle) > room {
		if room-nameW >= 4 {
			handle = textwidth.Truncate(handle, room-nameW, "…")
		} else {
			handle = ""
		}
	}
	name := textwidth.Truncate(it.Label, max(room-textwidth.Width(handle), 0), "…")
	gap := room - textwidth.Width(name) - textwidth.Width(handle) - textwidth.Width(detail)
	return lead + t.S.Base.Render(name) + t.S.Dim.Render(handle+detail) + t.S.Base.Render(strings.Repeat(" ", max(gap, 0)))
}

func (v *friendsView) View(ctx *ui.Context, focused bool) string {
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	v.focused = focused
	v.sync(ctx)
	t := themeOf(ctx)
	lines := []string{v.tabs.View(t, w)}
	if h > 1 {
		body := ui.Size{W: w, H: h - 1}
		if len(v.items) == 0 {
			lines = append(lines, strings.Split(v.emptyView(cat(ctx), t, body), "\n")...)
		} else {
			lines = append(lines, strings.Split(v.list.View(ctx, body), "\n")...)
		}
	}
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// emptyView is the empty state of a request tab.
func (v *friendsView) emptyView(c *i18n.T, t *theme.Theme, size ui.Size) string {
	switch {
	case v.tab == tabOutgoing && v.outFailed:
		return widgets.Empty(t, size, "⚠", c.Chat.OutFailed(), "")
	case v.tab == tabOutgoing:
		return widgets.Empty(t, size, "📮", c.Chat.NoOutgoing(), c.Chat.NoOutgoingHint())
	}
	return widgets.Empty(t, size, "📭", c.Chat.NoIncoming(), "")
}

// Title is "친구", badged with the received requests.
func (v *friendsView) Title(ctx *ui.Context) (string, ui.Badge) {
	if ctx == nil || ctx.Store == nil {
		return cat(ctx).Chat.FriendsTitle(), ui.Badge{}
	}
	return cat(ctx).Chat.FriendsTitle(), ui.Badge{Count: len(v.tabItems(ctx, tabIncoming))}
}

func (v *friendsView) Help(ctx *ui.Context) []key.Binding {
	hs := ctx.Keys.Help(keymap.ListOpen, keymap.MsgActions)
	return append(hs, key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", cat(ctx).Chat.HelpTabs())))
}

func (v *friendsView) SaveState() any {
	if it, ok := v.list.Selected(); ok {
		v.sel[v.tab] = it.ID
	}
	return friendsSaved{Tab: v.tab, Selected: v.sel}
}

func (v *friendsView) LoadState(s any) {
	var st friendsSaved
	switch x := s.(type) {
	case friendsSaved:
		st = x
	case *friendsSaved:
		if x == nil {
			return
		}
		st = *x
	default:
		return
	}
	v.tab = max(0, min(friendTabs-1, st.Tab))
	v.sel = st.Selected
	if v.anchorTab >= 0 {
		v.tab = v.anchorTab
	}
}

// ---- updates ----

func (v *friendsView) Init(ctx *ui.Context) tea.Cmd { return v.fetchOut(ctx) }

func (v *friendsView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

// fetchOut fetches the sent requests (the newest fetch wins).
func (v *friendsView) fetchOut(ctx *ui.Context) tea.Cmd {
	v.outGen++
	gen := v.outGen
	return ui.Do(ctx, tagFriendOut, func(c context.Context, cl *api.Client) (friendOutResult, error) {
		items, err := cl.ListFriendRequests(c, "outgoing")
		return friendOutResult{gen: gen, items: items}, err
	})
}

func (v *friendsView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[friendOutResult]:
		if m.Tag != tagFriendOut || m.Value.gen != v.outGen {
			return nil
		}
		v.outFailed = m.Err != nil
		if m.Err == nil {
			v.out = slices.Clone(m.Value.items)
		}
	case ui.ResultMsg[msgAction]:
		return landAction(ctx, m) // once-guarded: whoever sees it first lands it
	case ui.ResultMsg[*api.DirectInvite]:
		if v.awaiting[m.Tag] > 0 {
			if v.awaiting[m.Tag]--; v.awaiting[m.Tag] == 0 {
				delete(v.awaiting, m.Tag)
			}
			return directInviteDone(ctx, m)
		}
	case ui.ResultMsg[*api.FriendRequestResult]:
		if strings.HasPrefix(m.Tag, tagFriendAddPrefix) && m.Err == nil {
			return v.fetchOut(ctx)
		}
	case ui.RouteUpdatedMsg:
		if tab, ok := anchorTab(m.Route.Anchor); ok {
			v.setTab(ctx, tab)
		}
	case friendsRefreshMsg, ui.FriendRequestsChangedMsg:
		return v.fetchOut(ctx)
	case ui.EventMsg:
		if strings.HasPrefix(m.Event.Type, "friend.request_") {
			return v.fetchOut(ctx)
		}
	}
	return nil
}

// ---- input ----

func (v *friendsView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	v.focused = true
	v.sync(ctx)
	switch msg.String() {
	case "left", "h":
		v.setTab(ctx, v.tab-1)
		return true, nil
	case "right", "l":
		v.setTab(ctx, v.tab+1)
		return true, nil
	}
	it, ok := v.list.Selected()
	switch {
	case ctx.Keys.Matches(msg, keymap.ListOpen):
		if !ok {
			return false, nil
		}
		return true, v.open(ctx, it)
	case ctx.Keys.Matches(msg, keymap.MsgActions):
		if !ok {
			return false, nil
		}
		return true, v.openMenu(ctx, it)
	}
	handled, cmd := v.list.HandleKey(ctx, msg)
	if sel, ok := v.list.Selected(); ok {
		v.sel[v.tab] = sel.ID
	}
	return handled, cmd
}

// itemAt is the row index drawn at body row y (every row is one line), or -1.
func (v *friendsView) itemAt(y int) int {
	if y < 0 || y >= v.size.H-1 {
		return -1
	}
	top := slices.IndexFunc(v.items, func(it friendItem) bool { return it.ID == v.list.Anchor().ItemID })
	if top < 0 {
		top = 0
	}
	if i := top + y; i < len(v.items) {
		return i
	}
	return -1
}

// HandleMouse: a click on the tab row switches tabs; a left click selects a row
// and a click on the selected row runs it; a right click opens its menu; the
// wheel scrolls.
func (v *friendsView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		if local.Y >= 1 {
			return v.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - 1})
		}
		return nil
	}
	if local.Y == 0 {
		if i, hit := v.tabs.Hit(local.X); hit && click.Button == tea.MouseLeft {
			v.setTab(ctx, i)
		}
		return nil
	}
	i := v.itemAt(local.Y - 1)
	if i < 0 {
		return nil
	}
	it := v.items[i]
	switch click.Button {
	case tea.MouseRight:
		v.list.Select(it.ID)
		v.sel[v.tab] = it.ID
		return v.openMenu(ctx, it)
	case tea.MouseLeft:
		if cur, ok := v.list.Selected(); ok && cur.ID == it.ID {
			return v.open(ctx, it)
		}
		v.list.Select(it.ID)
		v.sel[v.tab] = it.ID
	}
	return nil
}

// open runs a row's primary action: the add row opens the picker, a friend our
// DM, a received request is accepted, a sent request opens its menu.
func (v *friendsView) open(ctx *ui.Context, it friendItem) tea.Cmd {
	switch it.kind {
	case friendAddRow:
		return ui.OpenOverlay(newFriendAdd(ctx, ""))
	case friendUserRow:
		return openDMCmd(ctx, []api.UserRef{userRef(it.user)}, "")
	case friendInRow:
		return acceptFriendCmd(ctx, it.req)
	case friendOutRow:
		return v.openMenu(ctx, it)
	}
	return nil
}

// openMenu opens the row's menu (none for the add row).
func (v *friendsView) openMenu(ctx *ui.Context, it friendItem) tea.Cmd {
	items := v.menuItems(ctx, it)
	if len(items) == 0 {
		return nil
	}
	return ui.OpenOverlay(newMenu(it.Label, items))
}

// menuItems are the actions of a row: received = 수락/거절/사용자 신고/차단, sent =
// 요청 취소, friend = DM 보내기/방에 초대 (with a space)/친구 끊기/사용자 신고/차단.
func (v *friendsView) menuItems(ctx *ui.Context, it friendItem) []menuItem {
	u, req := it.user, it.req
	c := cat(ctx)
	report := menuItem{Label: c.Chat.ItemReportUser(), Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, widgets.ReportDialog("user", u.ID, textwidth.Sanitize(displayName(u)))
	}}
	block := menuItem{Label: c.Chat.ItemBlockUser(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, blockConfirm(ctx, u)
	}}
	switch it.kind {
	case friendInRow:
		return []menuItem{
			{Label: c.Chat.ItemAccept(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return acceptFriendCmd(ctx, req), nil }},
			{Label: c.Chat.ItemDecline(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return declineFriendCmd(ctx, req), nil }},
			report, block,
		}
	case friendOutRow:
		return []menuItem{
			{Label: c.Chat.ItemCancelRequest(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return cancelFriendCmd(ctx, req), nil }},
		}
	case friendUserRow:
		items := []menuItem{
			{Label: c.Chat.ItemSendDm(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return openDMCmd(ctx, []api.UserRef{userRef(u)}, ""), nil
			}},
		}
		if spaces := v.inviteSpaces(ctx, u); len(spaces) > 0 {
			items = append(items, menuItem{Label: c.Chat.ItemInviteToSpace(), Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
				return nil, newMenu(c.Chat.InviteSpaceTitle(), spaces)
			}})
		}
		return append(items,
			menuItem{Label: c.Chat.ItemUnfriend(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				name := textwidth.Sanitize(displayName(u))
				return nil, widgets.Confirm(c.Chat.UnfriendAsk(name), c.Chat.UnfriendBody(), c.Chat.UnfriendYes(), c.Chat.Cancel(), removeFriendCmd(ctx, u))
			}},
			report, block,
		)
	}
	return nil
}

// inviteSpaces is the "방에 초대" submenu: one item per space, sending a direct
// invite of u (directInviteCmd, landed by this view).
func (v *friendsView) inviteSpaces(ctx *ui.Context, u api.UserSummary) []menuItem {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	var out []menuItem
	for _, id := range ctx.Store.SpaceOrder {
		sd := ctx.Store.Spaces[id]
		if sd == nil {
			continue
		}
		space := id
		out = append(out, menuItem{Label: textwidth.Sanitize(sd.Name), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			v.awaiting[tagInviteDirect+space]++
			return directInviteCmd(ctx, space, userRef(u)), nil
		}})
	}
	return out
}

// ---- friend actions ----

// acceptFriendCmd accepts a received request; on success the requester is my
// friend at once (friend.added follows).
func acceptFriendCmd(ctx *ui.Context, req api.FriendRequest) tea.Cmd {
	return doAction(ctx, "chat.friend_accept:"+req.ID, func(c context.Context, cl *api.Client) (*api.Friend, error) {
		return cl.AcceptFriendRequest(c, req.ID)
	}, func(ctx *ui.Context, f *api.Friend, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.AcceptFailed())
		}
		friend := api.Friend{User: req.From, Since: ctx.Now(), Presence: api.PresenceOffline}
		if f != nil && f.User.ID == req.From.ID {
			friend = *f
		} else if f != nil && f.Presence != "" {
			friend.Presence = f.Presence
		}
		ctx.Store.AddFriend(friend)
		ctx.Store.DropFriendRequests(func(r api.FriendRequest) bool { return r.ID == req.ID })
		return ui.Toast(cat(ctx).Chat.FriendAccepted(textwidth.Sanitize(displayName(req.From))), ui.ToastSuccess)
	})
}

// declineFriendCmd declines a received request (it leaves at once).
func declineFriendCmd(ctx *ui.Context, req api.FriendRequest) tea.Cmd {
	return doAction(ctx, "chat.friend_decline:"+req.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.DeclineFriendRequest(c, req.ID)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.DeclineFailed())
		}
		ctx.Store.DropFriendRequests(func(r api.FriendRequest) bool { return r.ID == req.ID })
		return ui.Toast(cat(ctx).Chat.FriendDeclined(), ui.ToastInfo)
	})
}

// cancelFriendCmd cancels a sent request; the sent list is fetched again.
func cancelFriendCmd(ctx *ui.Context, req api.FriendRequest) tea.Cmd {
	return doAction(ctx, "chat.friend_cancel:"+req.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.CancelFriendRequest(c, req.ID)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.CancelFailed())
		}
		return tea.Batch(friendsRefresh, ui.Toast(cat(ctx).Chat.FriendCanceled(), ui.ToastInfo))
	})
}

// removeFriendCmd ends the friendship with u (DELETE /v1/friends/@handle).
func removeFriendCmd(ctx *ui.Context, u api.UserSummary) tea.Cmd {
	return doAction(ctx, "chat.friend_remove:"+u.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RemoveFriend(c, userRef(u))
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.UnfriendFailed())
		}
		ctx.Store.RemoveFriend(u.ID)
		return ui.Toast(cat(ctx).Chat.Unfriended(textwidth.Sanitize(displayName(u))), ui.ToastSuccess)
	})
}

// ---- friend.add ----

// newFriendAdd is the friend.add picker (one user, server search; friends and
// blocked users hidden); args is the first query. A 200 answer (they had asked
// me) makes us friends at once.
func newFriendAdd(ctx *ui.Context, args string) ui.Overlay {
	return newUserPicker(pickerSpec{
		Title: cat(ctx).Chat.FriendAddTitle(), Submit: cat(ctx).Chat.FriendAddSubmit(), Max: 1, Search: true, Query: args,
		Exclude: func(ctx *ui.Context, id string) bool {
			if ctx == nil || ctx.Store == nil {
				return false
			}
			_, friend := ctx.Store.Friends[id]
			return friend || ctx.Store.IsBlocked(id)
		},
		OnPick: func(ctx *ui.Context, users []api.UserSummary, _ string) (string, tea.Cmd) {
			if len(users) == 0 {
				return "", nil
			}
			u := users[0]
			tag := tagFriendAddPrefix + u.ID
			return tag, ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (*api.FriendRequestResult, error) {
				return cl.SendFriendRequest(c, userRef(u))
			})
		},
		OnDone: func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
			m, ok := result.(ui.ResultMsg[*api.FriendRequestResult])
			if !ok || m.Value == nil {
				return nil, nil
			}
			r := m.Value
			if r.AutoAccepted && r.Friend != nil {
				ctx.Store.AddFriend(*r.Friend)
				return ui.Toast(cat(ctx).Chat.NowFriends(textwidth.Sanitize(displayName(r.Friend.User))), ui.ToastSuccess), nil
			}
			return ui.Toast(cat(ctx).Chat.RequestSent(textwidth.Sanitize(displayName(r.Request.To))), ui.ToastSuccess), nil
		},
	})
}
