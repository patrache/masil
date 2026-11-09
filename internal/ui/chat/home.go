package chat

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteHome, newHome)
}

// homeKind is the kind of a home row.
type homeKind int

const (
	homeEntry  homeKind = iota // 받은 방 초대 N, 친구 요청 N, 피드 보기, 친구 목록, ＋ 방 만들기, 초대 코드로 방 참여
	homeLabel                  // "─ DM", "─ 접속 중인 친구" (not selectable)
	homeDM                     // a visible DM / group DM
	homeFriend                 // an online or away friend
	homeSpace                  // "─ 노바 개발방": a joined space's section label (not selectable)
	homeChan                   // a text channel of a joined space
	homeMore                   // "…N개 더": a collapsed space's hidden channels (enter expands)
)

// Home row ids.
const (
	homeInvitesID    = "invites"
	homeRequestsID   = "requests"
	homeFeedID       = "feed"
	homeFriendsID    = "friends"
	homeSpaceNewID   = "space.create"
	homeSpaceJoinID  = "space.join"
	homeDMPrefix     = "dm:"
	homeFriendPrefix = "friend:"
	homeSpacePrefix  = "space:"
	homeChanPrefix   = "ch:"
	homeMorePrefix   = "more:"
)

// Home texts.
const (
	homeTitle       = "홈"
	homeDMLabel     = "─ DM"
	homeOnlineLabel = "─ 접속 중인 친구"
	homeSpaceNew    = "＋ 방 만들기"
	homeSpaceJoin   = "초대 코드로 방 참여"
	homeEmptyTitle  = "아직 대화가 없어요" // only without a Store: a bootstrapped home always has rows
	textNotLoaded   = "불러오는 중…"
	textOffline     = "서버에 연결할 수 없어요"
	textReconnect   = "다시 연결 중"
	homeEmptyHint   = "ctrl+k 로 명령을 찾아 보세요"
)

// homeItem is one home row (rebuilt from the Store on every View; every row is
// exactly one line). Label and Detail are sanitized display text; Route is what
// enter / a second click opens, else Overlay, a registered overlay id (both zero
// for labels).
type homeItem struct {
	ID, Label, Detail string
	Badge             ui.Badge
	Route             ui.Route
	Overlay           string

	kind     homeKind
	ch       *api.Channel       // homeDM, homeChan
	presence api.PresenceStatus // homeFriend
	space    string             // homeSpace, homeChan, homeMore: the space id
}

func (it homeItem) selectable() bool { return it.kind != homeLabel && it.kind != homeSpace }

// homeSaved is the Persistent state of the home route.
type homeSaved struct {
	Anchor   widgets.ScrollAnchor
	Selected string
	Expanded []string // the collapsed spaces opened with "…N개 더"
}

// homeView is RouteHome: the entry points (received invites, friend requests,
// feed, friends, new space, join by code), the visible DMs and the online friends.
type homeView struct {
	list    *widgets.List[homeItem]
	items   []homeItem // last sync
	size    ui.Size
	focused bool // of the View being drawn (render reads it)
	restore *homeSaved
	expand  map[string]bool // joined spaces (homeItemsWith): ids whose "…N개 더" was opened
}

var (
	_ ui.Component    = (*homeView)(nil)
	_ ui.KeyHandler   = (*homeView)(nil)
	_ ui.MouseHandler = (*homeView)(nil)
	_ ui.Titled       = (*homeView)(nil)
	_ ui.Helper       = (*homeView)(nil)
	_ ui.Persistent   = (*homeView)(nil)
)

// newHome is the RouteHome factory.
func newHome(ctx *ui.Context, _ ui.Route) ui.Component {
	v := &homeView{}
	v.list = widgets.NewList(func(it homeItem) string { return it.ID }, v.render)
	v.setItems(homeItemsWith(ctx, nil, ""))
	return v
}

// homeItems builds the rows: (when any) 받은 방 초대 N and 친구 요청 N, then 피드
// 보기, 친구 목록, ＋ 방 만들기 and 초대 코드로 방 참여 (the space.create /
// space.join dialogs), "─ DM" + visibleDMs (a blocked peer's 1:1 DM leaves at
// once), "─ 접속 중인 친구" + online / away friends (blocked users never). The
// entry rows are there for a brand-new user too. nil without a Store, and before
// the first bootstrap while there is nothing to talk about (notLoaded says so).
func homeItems(ctx *ui.Context) []homeItem {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	s := ctx.Store
	dms := visibleDMs(ctx)
	friends := onlineFriends(ctx)
	anyFriend := false
	for id := range s.Friends {
		if !s.IsBlocked(id) {
			anyFriend = true
			break
		}
	}
	nInv, nReq := len(s.DirectInvites), len(s.FriendReqIn)
	if len(dms) == 0 && !anyFriend && nInv == 0 && nReq == 0 {
		if _, _, ok := notLoaded(ctx); ok {
			return nil
		}
	}

	var items []homeItem
	if nInv > 0 {
		items = append(items, homeItem{ID: homeInvitesID, Label: "받은 방 초대 " + strconv.Itoa(nInv),
			Route: ui.Route{Kind: ui.RouteInvite, Arg: "direct"}})
	}
	if nReq > 0 {
		items = append(items, homeItem{ID: homeRequestsID, Label: "친구 요청 " + strconv.Itoa(nReq),
			Route: ui.Route{Kind: ui.RouteFriends, Anchor: ui.FriendsIncoming}})
	}
	items = append(items,
		homeItem{ID: homeFeedID, Label: "피드 보기", Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}},
		homeItem{ID: homeFriendsID, Label: "친구 목록", Route: ui.Route{Kind: ui.RouteFriends, Anchor: ui.FriendsList}},
		homeItem{ID: homeSpaceNewID, Label: homeSpaceNew, Overlay: overlaySpaceCreate},
		homeItem{ID: homeSpaceJoinID, Label: homeSpaceJoin, Overlay: overlaySpaceJoin},
	)
	if len(dms) > 0 {
		items = append(items, homeItem{ID: "label:dms", Label: homeDMLabel, kind: homeLabel})
		for _, ch := range dms {
			items = append(items, homeItem{
				ID: homeDMPrefix + ch.ID, Label: textwidth.Sanitize(s.ChannelLabel(ch.ID)), Badge: sideBadge(ch),
				Route: ui.Route{Kind: ui.RouteChannel, ID: ch.ID}, kind: homeDM, ch: ch,
			})
		}
	}
	if len(friends) > 0 {
		items = append(items, homeItem{ID: "label:online", Label: homeOnlineLabel, kind: homeLabel})
		items = append(items, friends...)
	}
	return items
}

// Joined spaces on the home (CA RouteHome).

// homeItemsWith is what the home shows: homeItems with the joined spaces'
// sections (homeSpaceItems) after the entry rows, before "─ DM". homeItems alone
// has no space section (its one-argument form stays for its callers); expanded
// and keep are the view's (see homeSpaceItems).
func homeItemsWith(ctx *ui.Context, expanded map[string]bool, keep string) []homeItem {
	items := homeItems(ctx)
	if len(items) == 0 {
		return items
	}
	at := slices.IndexFunc(items, func(it homeItem) bool { return it.kind != homeEntry })
	if at < 0 {
		at = len(items)
	}
	return slices.Insert(items, at, homeSpaceItems(ctx, expanded, keep)...)
}

// homeSpaceMax is the most channels a space shows on the home before it collapses
// to its unread channels and "…N개 더".
const homeSpaceMax = 12

// homeSpaceItems are the joined spaces' sections, in the rail's order (SpaceOrder):
// "─ 이름" (not selectable — the sidebar's header opens nothing either) then the
// space's text channels in the sidebar's order (Store.SpaceChannels: the ones I can
// read, by position), drawn like the sidebar (channelRow: 🔒 private, sideBadge,
// muted dim). A space with more than homeSpaceMax channels shows only those with a
// badge, then "…N개 더" (enter expands it in place) unless expanded; the row whose
// id is keep (the cursor's, or the one a pending LoadState selects) shows anyway,
// so a channel just opened (and so read) keeps the cursor on its row. A space
// without a readable channel has no section.
func homeSpaceItems(ctx *ui.Context, expanded map[string]bool, keep string) []homeItem {
	s := ctx.Store
	var items []homeItem
	for _, sid := range s.SpaceOrder {
		sd := s.Spaces[sid]
		if sd == nil {
			continue
		}
		chs := s.SpaceChannels(sid)
		if len(chs) == 0 {
			continue
		}
		name := strings.TrimSpace(textwidth.Sanitize(sd.Name))
		if name == "" {
			name = "방"
		}
		items = append(items, homeItem{ID: homeSpacePrefix + sid, Label: "─ " + name, kind: homeSpace, space: sid})
		collapse := len(chs) > homeSpaceMax && !expanded[sid]
		hidden := 0
		for _, ch := range chs {
			b := sideBadge(ch)
			if collapse && b.Count == 0 && homeChanPrefix+ch.ID != keep {
				hidden++
				continue
			}
			items = append(items, homeItem{
				ID: homeChanPrefix + ch.ID, Label: channelTitle(ctx, ch, false), Badge: b,
				Route: ui.Route{Kind: ui.RouteChannel, ID: ch.ID}, kind: homeChan, ch: ch, space: sid,
			})
		}
		if hidden > 0 {
			items = append(items, homeItem{ID: homeMorePrefix + sid, Label: "…" + strconv.Itoa(hidden) + "개 더", kind: homeMore, space: sid})
		}
	}
	return items
}

// onlineFriends are the friend rows of online / away friends (not blocked), by
// presence then name: the label is the display name, the detail the custom status
// ("🎧 듣는 중…", unless expired); enter opens our 1:1 DM, else the friends route.
func onlineFriends(ctx *ui.Context) []homeItem {
	s := ctx.Store
	var out []homeItem
	for id, f := range s.Friends {
		if s.IsBlocked(id) {
			continue
		}
		st := userPresence(ctx, id)
		if st != api.PresenceOnline && st != api.PresenceAway {
			continue
		}
		u := f.User
		if cu := s.User(id); cu != nil {
			u = *cu
		}
		it := homeItem{
			ID: homeFriendPrefix + id, Label: strings.TrimSpace(textwidth.Sanitize(displayName(u))),
			Detail: statusText(ctx, u.Status), Route: ui.Route{Kind: ui.RouteFriends}, kind: homeFriend, presence: st,
		}
		if dm := dmWithUser(ctx, id); dm != "" {
			it.Route = ui.Route{Kind: ui.RouteChannel, ID: dm}
		}
		out = append(out, it)
	}
	slices.SortFunc(out, func(a, b homeItem) int {
		return cmp.Or(
			cmp.Compare(presenceRank(a.presence), presenceRank(b.presence)),
			strings.Compare(a.Label, b.Label),
			strings.Compare(a.ID, b.ID),
		)
	})
	return out
}

// statusText is "emoji text" of a custom status (sanitized; "" when unset or expired).
func statusText(ctx *ui.Context, st *api.UserStatus) string {
	if st == nil {
		return ""
	}
	if st.ExpiresAt != nil && ctx.Now != nil && !st.ExpiresAt.After(ctx.Now()) {
		return ""
	}
	var parts []string
	if st.Emoji != nil {
		if e := strings.TrimSpace(textwidth.Sanitize(*st.Emoji)); e != "" {
			parts = append(parts, e)
		}
	}
	if t := strings.TrimSpace(textwidth.Sanitize(st.Text)); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

// dmWithUser is the id of the visible 1:1 DM with user id ("" when none).
func dmWithUser(ctx *ui.Context, id string) string {
	for _, ch := range visibleDMs(ctx) {
		if ch.Kind != api.ChannelDM {
			continue
		}
		if p := dmPeer(ctx.Store, ch); p != nil && p.ID == id {
			return ch.ID
		}
	}
	return ""
}

// render draws a row: column 0 is the focus cursor "›" (blank when unfocused),
// then the content in width-1 cells.
func (v *homeView) render(ctx *ui.Context, it homeItem, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && v.focused && it.selectable() {
		cur = t.S.Title.Render("›")
	}
	cw := width - 1
	if cw <= 0 {
		return []string{cur}
	}
	var body string
	switch it.kind {
	case homeLabel, homeSpace, homeMore:
		body = cell(t.S.Dim.Render, it.Label, cw)
	case homeDM, homeChan:
		body = channelRow(ctx, t, it.ch, cw)
	case homeFriend:
		body = friendRow(t, it, cw)
	default:
		st := t.S.Base
		if it.ID == homeInvitesID || it.ID == homeRequestsID {
			st = t.S.Bold
		}
		body = cell(st.Render, it.Label, cw)
	}
	return []string{cur + body}
}

// friendRow is "<dot> 이름 · 상태" in exactly cw cells (the status is S.Dim and
// goes first when room runs short).
func friendRow(t *theme.Theme, it homeItem, cw int) string {
	lead, leadW := widgets.PresenceDot(t, it.presence)+t.S.Base.Render(" "), 2
	if leadW > cw {
		lead, leadW = "", 0
	}
	room := cw - leadW
	name := textwidth.Truncate(it.Label, max(room, 0), "…")
	rest := room - textwidth.Width(name)
	detail := ""
	if it.Detail != "" && rest >= 5 { // " · " + at least a little of the status
		detail = textwidth.Truncate(" · "+it.Detail, rest, "…")
	}
	gap := rest - textwidth.Width(detail)
	return lead + t.S.Base.Render(name) + t.S.Dim.Render(detail) + t.S.Base.Render(strings.Repeat(" ", max(gap, 0)))
}

func (v *homeView) Init(*ui.Context) tea.Cmd { return nil }

func (v *homeView) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }

func (v *homeView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

func (v *homeView) setItems(items []homeItem) {
	v.items = items
	v.list.SetItems(items)
}

func (v *homeView) index(id string) int {
	for i, it := range v.items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

func (v *homeView) selectedIndex() int {
	if it, ok := v.list.Selected(); ok {
		return v.index(it.ID)
	}
	return -1
}

// snap moves the cursor off a label row: on in the direction it moved (from
// prev), else back the other way, else to prev.
func (v *homeView) snap(prev int) {
	cur := v.selectedIndex()
	if cur < 0 || v.items[cur].selectable() {
		return
	}
	dir := 1
	if prev >= 0 && cur < prev {
		dir = -1
	}
	for _, d := range []int{dir, -dir} {
		for i := cur + d; i >= 0 && i < len(v.items); i += d {
			if v.items[i].selectable() {
				v.list.Select(v.items[i].ID)
				return
			}
		}
	}
	if prev >= 0 && prev < len(v.items) {
		v.list.Select(v.items[prev].ID)
	}
}

// sync rebuilds the rows from the Store (the list keeps the selection by ID) and
// applies a pending LoadState.
func (v *homeView) sync(ctx *ui.Context, focused bool) {
	v.focused = focused
	if r := v.restore; r != nil && len(r.Expanded) > 0 {
		for _, id := range r.Expanded {
			v.expandSpace(id)
		}
	}
	keep := ""
	if r := v.restore; r != nil {
		keep = r.Selected
	} else if it, ok := v.list.Selected(); ok {
		keep = it.ID
	}
	v.setItems(homeItemsWith(ctx, v.expand, keep))
	if r := v.restore; r != nil && len(v.items) > 0 {
		v.restore = nil
		v.list.Restore(r.Anchor)
		if v.index(r.Selected) >= 0 {
			v.list.Select(r.Selected)
		}
	}
	v.snap(-1)
}

func (v *homeView) View(ctx *ui.Context, focused bool) string {
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	v.sync(ctx, focused)
	if len(v.items) == 0 {
		if title, hint, ok := notLoaded(ctx); ok {
			icon := ""
			if hint != "" {
				icon = "⚠"
			}
			return widgets.Empty(themeOf(ctx), v.size, icon, title, hint)
		}
		return widgets.Empty(themeOf(ctx), v.size, "💬", homeEmptyTitle, homeEmptyHint)
	}
	lines := strings.Split(v.list.View(ctx, v.size), "\n")
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// notLoaded is what the home and the sidebar say instead of their empty states
// before the first successful bootstrap: "불러오는 중…", or (the last attempt
// failed) "서버에 연결할 수 없어요" with the hint "다시 연결 중". ok is false once
// bootstrapped (or without a Store): an empty list is then really empty.
func notLoaded(ctx *ui.Context) (title, hint string, ok bool) {
	if ctx == nil || ctx.Store == nil || ctx.Store.Sync.Bootstrapped {
		return "", "", false
	}
	if ctx.Store.Sync.Err != nil {
		return textOffline, textReconnect, true
	}
	return textNotLoaded, "", true
}

// Title is "홈", badged with the pending invites + friend requests.
func (v *homeView) Title(ctx *ui.Context) (string, ui.Badge) {
	if ctx == nil || ctx.Store == nil {
		return homeTitle, ui.Badge{}
	}
	return homeTitle, ui.Badge{Count: len(ctx.Store.DirectInvites) + len(ctx.Store.FriendReqIn)}
}

func (v *homeView) Help(ctx *ui.Context) []key.Binding {
	return ctx.Keys.Help(keymap.ListOpen, keymap.ListDown, keymap.ListUp)
}

func (v *homeView) SaveState() any {
	st := homeSaved{Anchor: v.list.Anchor()}
	if it, ok := v.list.Selected(); ok {
		st.Selected = it.ID
	}
	for id := range v.expand {
		st.Expanded = append(st.Expanded, id)
	}
	slices.Sort(st.Expanded)
	return st
}

func (v *homeView) LoadState(s any) {
	switch x := s.(type) {
	case homeSaved:
		v.restore = &x
	case *homeSaved:
		if x != nil {
			st := *x
			v.restore = &st
		}
	}
}

func (v *homeView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	v.focused = true
	if len(v.items) == 0 {
		return false, nil
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		it, ok := v.list.Selected()
		if !ok || !it.selectable() {
			return false, nil
		}
		return true, v.activate(ctx, it)
	}
	prev := v.selectedIndex()
	handled, cmd := v.list.HandleKey(ctx, msg)
	if handled {
		v.snap(prev)
	}
	return handled, cmd
}

// itemAt is the row index drawn at local row y of the last View (every row is one
// line, so it is the scroll anchor's item + y), or -1.
func (v *homeView) itemAt(y int) int {
	if y < 0 || y >= v.size.H {
		return -1
	}
	top := v.index(v.list.Anchor().ItemID)
	if top < 0 {
		top = 0
	}
	if i := top + y; i < len(v.items) {
		return i
	}
	return -1
}

// HandleMouse: the wheel scrolls; a left click selects a row and a click on the
// selected row opens it (CA 5.5); labels only focus the pane.
func (v *homeView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if len(v.items) == 0 {
		return nil
	}
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return v.list.HandleMouse(ctx, msg, local)
	}
	if click.Button != tea.MouseLeft {
		return nil
	}
	i := v.itemAt(local.Y)
	if i < 0 || !v.items[i].selectable() {
		return nil
	}
	it := v.items[i]
	if cur, ok := v.list.Selected(); (ok && cur.ID == it.ID) || it.kind == homeMore { // "…N개 더" expands at once
		return v.activate(ctx, it)
	}
	v.list.Select(it.ID)
	return nil
}

// open navigates to a row's route, or opens its registered dialog (the same one
// the palette's space.create / space.join open).
func (v *homeView) open(_ *ui.Context, it homeItem) tea.Cmd {
	switch {
	case it.Route.Kind != "":
		return ui.Navigate(it.Route)
	case it.Overlay != "":
		return ui.OpenRegisteredOverlay(it.Overlay, "")
	}
	return nil
}

// Joined spaces on the home (CA RouteHome).

// activate is enter / the opening click on a row: "…N개 더" expands its space in
// place (openMore), a space channel also becomes the selected space (as from the
// sidebar) and opens; every other row opens (open).
func (v *homeView) activate(ctx *ui.Context, it homeItem) tea.Cmd {
	switch it.kind {
	case homeMore:
		v.openMore(ctx, it.space)
		return nil
	case homeChan:
		if c := chatOf(ctx); c != nil {
			c.SetSelectedSpace(it.space)
		}
	}
	return v.open(ctx, it)
}

// expandSpace marks space id as expanded.
func (v *homeView) expandSpace(id string) {
	if v.expand == nil {
		v.expand = map[string]bool{}
	}
	v.expand[id] = true
}

// openMore expands space id: every channel shows, and the cursor moves to the first
// one "…N개 더" hid.
func (v *homeView) openMore(ctx *ui.Context, id string) {
	shown := map[string]bool{}
	for _, it := range v.items {
		shown[it.ID] = true
	}
	v.expandSpace(id)
	v.setItems(homeItemsWith(ctx, v.expand, ""))
	for _, it := range v.items {
		if it.kind == homeChan && it.space == id && !shown[it.ID] {
			v.list.Select(it.ID)
			return
		}
	}
	v.snap(-1)
}
