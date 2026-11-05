package chat

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterPane(layout.SlotMembers, newMembers)
}

// Members pane constants.
const (
	presenceEvery  = 60 * time.Second // presence re-poll while the pane is shown
	presenceChunk  = 100              // GET /v1/presence takes at most this many ids
	membersTitle   = "멤버"
	membersLoading = "불러오는 중…"
	membersFailed  = "멤버를 불러오지 못했어요"
	membersNone    = "대화를 열면 멤버가 보여요"
	roleOwnerText  = "방장"
	memberMoreID   = "more"
	memberNoteID   = "note"

	itemSendDM     = "DM 보내기"
	itemProfile    = "프로필 보기"
	itemReportUser = "사용자 신고"
	itemBlockUser  = "차단"
	dmOpenFailed   = "DM을 열지 못했어요"
)

// presencePollMsg is the presence re-poll tick of one polling generation.
type presencePollMsg struct{ gen uint64 }

// presenceResult is one presence poll of a channel's members.
type presenceResult struct {
	ChannelID string
	Items     []api.PresenceItem
}

// memberItem is one members-pane row (every row is exactly one line).
type memberItem struct {
	ID     string // user id, memberMoreID or memberNoteID
	User   *api.UserSummary
	Status api.PresenceStatus
	Role   string // "방장" / "관리자" / "" (unknown or member)
	Text   string // the more / note line
}

func (it memberItem) selectable() bool { return it.User != nil }

// memberPage is the cached first member page of a text channel.
type memberPage struct {
	users []api.UserSummary
	more  bool // the server has more than this page
}

// membersPane is SlotMembers (command mode, top right while no thread is open):
// the viewed channel's members with presence, polled every 60s while shown.
type membersPane struct {
	list    *widgets.List[memberItem]
	items   []memberItem
	size    ui.Size
	focused bool // of the View being drawn (render reads it)

	polling bool   // a presencePollMsg of gen is pending
	gen     uint64 // latest polling generation
	ch      string // channel the current generation polls

	pages   map[string]*memberPage // text channel -> its first member page
	busy    map[string]bool        // member page in flight
	failed  map[string]bool        // the last member page request failed
	pending map[string]bool        // presence waits for this channel's member page
}

var (
	_ ui.Component    = (*membersPane)(nil)
	_ ui.KeyHandler   = (*membersPane)(nil)
	_ ui.MouseHandler = (*membersPane)(nil)
	_ ui.Titled       = (*membersPane)(nil)
	_ ui.Helper       = (*membersPane)(nil)
)

// newMembers is the SlotMembers pane factory.
func newMembers(ctx *ui.Context) ui.Component {
	p := &membersPane{
		pages: map[string]*memberPage{}, busy: map[string]bool{},
		failed: map[string]bool{}, pending: map[string]bool{},
	}
	p.list = widgets.NewList(func(it memberItem) string { return it.ID }, p.render)
	return p
}

// shown: the current layout places SlotMembers (decided from the layout, never
// from View timing).
func shown(ctx *ui.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Layout.Pane(layout.SlotMembers)
	return ok
}

// ensurePolling starts polling when the pane is shown and not polling yet, and
// restarts it at once when the viewed channel changed.
func (p *membersPane) ensurePolling(ctx *ui.Context) tea.Cmd {
	if ctx == nil || ctx.Store == nil || !shown(ctx) {
		return nil
	}
	ch := viewedChannel(ctx)
	if ch == nil || (p.polling && p.ch == ch.ID) {
		return nil
	}
	p.polling, p.ch = true, ch.ID
	p.gen++
	return tea.Batch(p.poll(ctx, ch), ui.After(ctx, presenceEvery, presencePollMsg{gen: p.gen}))
}

// onTick re-polls and re-arms while shown with a viewed channel; hidden or with
// no viewed channel (Home, Feed …), polling stops (the next ensurePolling starts
// it again) so nobody's presence is polled while the pane lists no one.
func (p *membersPane) onTick(ctx *ui.Context, m presencePollMsg) tea.Cmd {
	if m.gen != p.gen {
		return nil
	}
	ch := viewedChannel(ctx)
	if !shown(ctx) || ch == nil {
		p.polling = false
		return nil
	}
	if ch.ID != p.ch {
		p.polling = false
		return p.ensurePolling(ctx)
	}
	return tea.Batch(p.poll(ctx, ch), ui.After(ctx, presenceEvery, presencePollMsg{gen: p.gen}))
}

// poll requests the presence of ch's members (a text channel without its member
// page fetches the page first; presence follows once it lands).
func (p *membersPane) poll(ctx *ui.Context, ch *api.Channel) tea.Cmd {
	if ch == nil {
		return nil
	}
	if ch.Kind != api.ChannelText {
		return p.fetchPresence(ctx, ch.ID, ch.Members)
	}
	if pg := p.pages[ch.ID]; pg != nil {
		return p.fetchPresence(ctx, ch.ID, pg.users)
	}
	p.pending[ch.ID] = true
	return p.fetchMembers(ctx, ch.ID)
}

// fetchMembers requests a text channel's first member page (chat.members:<ch>, the
// same tag as the composer's @mention list, so either request fills both).
func (p *membersPane) fetchMembers(ctx *ui.Context, id string) tea.Cmd {
	if p.busy[id] {
		return nil
	}
	p.busy[id] = true
	return ui.Do(ctx, "chat.members:"+id, func(c context.Context, cl *api.Client) (*api.Page[api.ChannelMember], error) {
		return cl.ListChannelMembers(c, id, api.PageOpts{Limit: memberFetchLimit})
	})
}

// fetchPresence requests the presence of users (me excluded, ≤100 per request).
func (p *membersPane) fetchPresence(ctx *ui.Context, channelID string, users []api.UserSummary) tea.Cmd {
	var ids []string
	for _, u := range users {
		if u.ID == "" || isMe(ctx, u.ID) || slices.Contains(ids, u.ID) {
			continue
		}
		ids = append(ids, u.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return ui.Do(ctx, "chat.presence:"+channelID, func(c context.Context, cl *api.Client) (presenceResult, error) {
		out := presenceResult{ChannelID: channelID}
		for chunk := range slices.Chunk(ids, presenceChunk) {
			items, err := cl.GetPresence(c, chunk)
			out.Items = append(out.Items, items...)
			if err != nil {
				return out, err
			}
		}
		return out, nil
	})
}

// onMembers caches a member page (whoever asked for it) and runs the presence
// request that waited for it.
func (p *membersPane) onMembers(ctx *ui.Context, m ui.ResultMsg[*api.Page[api.ChannelMember]]) tea.Cmd {
	id, ok := strings.CutPrefix(m.Tag, "chat.members:")
	if !ok {
		return nil
	}
	p.busy[id] = false
	if m.Err != nil || m.Value == nil {
		p.failed[id] = true
		delete(p.pending, id) // the next tick retries the page
		return nil
	}
	pg := &memberPage{more: !m.Value.Done()}
	for _, it := range m.Value.Items {
		pg.users = append(pg.users, it.User)
	}
	p.pages[id] = pg
	p.failed[id] = false
	if !p.pending[id] {
		return nil
	}
	delete(p.pending, id)
	return p.fetchPresence(ctx, id, pg.users)
}

// onPresence stores a presence poll. A failed poll keeps the cached values (the
// next tick retries; a background poll raises no toast).
func (p *membersPane) onPresence(ctx *ui.Context, m ui.ResultMsg[presenceResult]) {
	if !strings.HasPrefix(m.Tag, "chat.presence:") || ctx == nil || ctx.Store == nil {
		return
	}
	s := ctx.Store
	if s.Presence == nil {
		s.Presence = map[string]store.PresenceEntry{}
	}
	now := ctx.Now()
	for _, it := range m.Value.Items {
		if it.UserID == "" {
			continue
		}
		s.Presence[it.UserID] = store.PresenceEntry{Status: it.Status, LastSeenAt: it.LastSeenAt, FetchedAt: now}
	}
}

func (p *membersPane) Init(ctx *ui.Context) tea.Cmd { return p.ensurePolling(ctx) }

func (p *membersPane) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	switch m := msg.(type) {
	case presencePollMsg:
		cmds = append(cmds, p.onTick(ctx, m))
	case ui.ResultMsg[*api.Page[api.ChannelMember]]:
		cmds = append(cmds, p.onMembers(ctx, m))
	case ui.ResultMsg[presenceResult]:
		p.onPresence(ctx, m)
	case ui.ResultMsg[msgAction]:
		cmds = append(cmds, landAction(ctx, m)) // once-guarded: whoever sees it first lands it
	}
	cmds = append(cmds, p.ensurePolling(ctx))
	return tea.Batch(cmds...)
}

func (p *membersPane) Resize(_ *ui.Context, size ui.Size) { p.size = size }

// userPresence is a user's presence: mine from Store.Self, else Store.Presence,
// else the friend entry, else offline.
func userPresence(ctx *ui.Context, id string) api.PresenceStatus {
	s := ctx.Store
	if s.IsMe(id) {
		return s.Self.Status
	}
	if e, ok := s.Presence[id]; ok && e.Status != "" {
		return e.Status
	}
	if f, ok := s.Friends[id]; ok && f.Presence != "" {
		return f.Presence
	}
	return api.PresenceOffline
}

// presenceRank orders online → away → dnd → offline.
func presenceRank(st api.PresenceStatus) int {
	switch st {
	case api.PresenceOnline:
		return 0
	case api.PresenceAway:
		return 1
	case api.PresenceDND:
		return 2
	}
	return 3
}

// memberCount is the title count of ch (0 = unknown).
func (p *membersPane) memberCount(ch *api.Channel) int {
	if ch == nil {
		return 0
	}
	if ch.Kind != api.ChannelText {
		return len(ch.Members)
	}
	n := ch.MemberCount
	if pg := p.pages[ch.ID]; pg != nil {
		n = max(n, len(pg.users))
	}
	return n
}

// memberItems builds the rows of the viewed channel: members sorted by presence
// then display name, then "+ N명 더" when the first page is not all of them.
func (p *membersPane) memberItems(ctx *ui.Context) []memberItem {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	ch := viewedChannel(ctx)
	if ch == nil {
		return []memberItem{{ID: memberNoteID, Text: membersNone}}
	}
	var users []api.UserSummary
	more := 0
	if ch.Kind == api.ChannelText {
		pg := p.pages[ch.ID]
		switch {
		case pg != nil:
			users = pg.users
			if n := ch.MemberCount - len(users); n > 0 || pg.more {
				more = max(n, 0)
			}
		case p.failed[ch.ID]:
			return []memberItem{{ID: memberNoteID, Text: membersFailed}}
		default:
			return []memberItem{{ID: memberNoteID, Text: membersLoading}}
		}
	} else {
		users = ch.Members
	}
	var owner string
	if sd := spaceOf(ctx, ch); sd != nil {
		owner = sd.OwnerID
	}
	items := make([]memberItem, 0, len(users)+1)
	seen := map[string]bool{}
	for i := range users {
		u := users[i]
		if u.ID == "" || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		if cu := ctx.Store.User(u.ID); cu != nil && ch.Kind != api.ChannelText {
			u = *cu
		}
		it := memberItem{ID: u.ID, User: &u, Status: userPresence(ctx, u.ID)}
		if owner != "" && u.ID == owner {
			it.Role = roleOwnerText
		}
		items = append(items, it)
	}
	slices.SortStableFunc(items, func(a, b memberItem) int {
		return cmp.Or(
			cmp.Compare(presenceRank(a.Status), presenceRank(b.Status)),
			strings.Compare(textwidth.Sanitize(displayName(*a.User)), textwidth.Sanitize(displayName(*b.User))),
			strings.Compare(a.ID, b.ID),
		)
	})
	if more > 0 {
		items = append(items, memberItem{ID: memberMoreID, Text: "+ " + strconv.Itoa(more) + "명 더"})
	}
	return items
}

// render draws a row: column 0 is the focus cursor "›", then "<dot> 이름 · 역할".
func (p *membersPane) render(ctx *ui.Context, it memberItem, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && p.focused && it.selectable() {
		cur = t.S.Title.Render("›")
	}
	cw := width - 1
	if cw <= 0 {
		return []string{cur}
	}
	if !it.selectable() {
		return []string{cur + cell(t.S.Dim.Render, it.Text, cw)}
	}
	return []string{cur + memberRow(ctx, t, it, cw)}
}

// memberRow is "<dot> 이름 · 역할" in exactly cw cells (the role is S.Dim).
func memberRow(ctx *ui.Context, t *theme.Theme, it memberItem, cw int) string {
	lead, leadW := widgets.PresenceDot(t, it.Status)+t.S.Base.Render(" "), 2
	if leadW > cw {
		lead, leadW = "", 0
	}
	room := cw - leadW
	name := strings.TrimSpace(textwidth.Sanitize(displayName(*it.User)))
	role := ""
	if it.Role != "" {
		role = " · " + it.Role
	}
	st := t.S.Base
	if ctx.Store.IsBlocked(it.ID) {
		st = t.S.Dim
	}
	rw := textwidth.Width(role)
	if rw > 0 && room-rw < 2 { // keep at least a little of the name
		role, rw = "", 0
	}
	name = textwidth.Truncate(name, max(room-rw, 0), "…")
	gap := room - textwidth.Width(name) - rw
	return lead + st.Render(name) + t.S.Dim.Render(role) + t.S.Base.Render(strings.Repeat(" ", max(gap, 0)))
}

// sync rebuilds the rows from the Store and the cached member page; the cursor
// stays on its user (or moves off a non-selectable row).
func (p *membersPane) sync(ctx *ui.Context) {
	p.items = p.memberItems(ctx)
	p.list.SetItems(p.items)
	p.snap(-1)
}

func (p *membersPane) index(id string) int {
	for i, it := range p.items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

func (p *membersPane) selectedIndex() int {
	if it, ok := p.list.Selected(); ok {
		return p.index(it.ID)
	}
	return -1
}

// snap moves the cursor off a non-selectable row (the more line is last, the
// note is alone): to the nearest selectable row before it.
func (p *membersPane) snap(prev int) {
	cur := p.selectedIndex()
	if cur < 0 || p.items[cur].selectable() {
		return
	}
	for i := cur - 1; i >= 0; i-- {
		if p.items[i].selectable() {
			p.list.Select(p.items[i].ID)
			return
		}
	}
	if prev >= 0 && prev < len(p.items) {
		p.list.Select(p.items[prev].ID)
	}
}

func (p *membersPane) View(ctx *ui.Context, focused bool) string {
	w, h := p.size.W, p.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	p.focused = focused
	if viewedChannel(ctx) == nil { // home and other non-conversation routes
		p.items = nil
		p.list.SetItems(nil)
		return widgets.Empty(themeOf(ctx), p.size, "", membersNone, "")
	}
	p.sync(ctx)
	lines := strings.Split(p.list.View(ctx, p.size), "\n")
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// Title is "멤버 N" ("멤버" while the count is unknown).
func (p *membersPane) Title(ctx *ui.Context) (string, ui.Badge) {
	if n := p.memberCount(viewedChannel(ctx)); n > 0 {
		return membersTitle + " " + strconv.Itoa(n), ui.Badge{}
	}
	return membersTitle, ui.Badge{}
}

func (p *membersPane) Help(ctx *ui.Context) []key.Binding {
	return ctx.Keys.Help(keymap.ListOpen, keymap.MsgActions)
}

func (p *membersPane) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	p.focused = true
	poll := p.ensurePolling(ctx)
	if ctx.Keys.Matches(msg, keymap.MsgActions) || ctx.Keys.Matches(msg, keymap.ListOpen) {
		it, ok := p.list.Selected()
		if !ok || !it.selectable() {
			return false, poll
		}
		return true, tea.Batch(poll, p.openMenu(ctx, it))
	}
	prev := p.selectedIndex()
	handled, cmd := p.list.HandleKey(ctx, msg)
	if handled {
		p.snap(prev)
	}
	return handled, tea.Batch(poll, cmd)
}

// itemAt is the row index drawn at local row y of the last View, or -1.
func (p *membersPane) itemAt(y int) int {
	if y < 0 || y >= p.size.H {
		return -1
	}
	top := p.index(p.list.Anchor().ItemID)
	if top < 0 {
		top = 0
	}
	if i := top + y; i < len(p.items) {
		return i
	}
	return -1
}

// HandleMouse: the wheel scrolls; a left click selects a member and a click on
// the selected one opens its menu; a right click opens the menu at once.
func (p *membersPane) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return p.list.HandleMouse(ctx, msg, local)
	}
	i := p.itemAt(local.Y)
	if i < 0 || !p.items[i].selectable() {
		return nil
	}
	it := p.items[i]
	switch click.Button {
	case tea.MouseRight:
		p.list.Select(it.ID)
		return p.openMenu(ctx, it)
	case tea.MouseLeft:
		if cur, ok := p.list.Selected(); ok && cur.ID == it.ID {
			return p.openMenu(ctx, it)
		}
		p.list.Select(it.ID)
	}
	return nil
}

// openMenu opens the member menu of a row.
func (p *membersPane) openMenu(ctx *ui.Context, it memberItem) tea.Cmd {
	if it.User == nil {
		return nil
	}
	return ui.OpenOverlay(newMenu(textwidth.Sanitize(displayName(*it.User)), memberActions(ctx, *it.User)))
}

// memberActions is a member's menu: DM 보내기, 프로필 보기, 사용자 신고, 차단 (not
// for a user I already blocked); my own row has only 프로필 보기.
func memberActions(ctx *ui.Context, u api.UserSummary) []menuItem {
	profile := menuItem{Label: itemProfile, Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
		return ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: u.ID}), nil
	}}
	if isMe(ctx, u.ID) {
		return []menuItem{profile}
	}
	items := []menuItem{
		{Label: itemSendDM, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return openDMCmd(ctx, []api.UserRef{userRef(u)}, ""), nil }},
		profile,
		{Label: itemReportUser, Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, widgets.ReportDialog("user", u.ID, textwidth.Sanitize(displayName(u)))
		}},
	}
	if ctx == nil || ctx.Store == nil || !ctx.Store.IsBlocked(u.ID) {
		items = append(items, menuItem{Label: itemBlockUser, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, blockConfirm(ctx, u)
		}})
	}
	return items
}

// userRef is u as a {user} reference: "@handle", else the id.
func userRef(u api.UserSummary) api.UserRef {
	if u.Handle != "" {
		return api.Handle(u.Handle)
	}
	return api.UserID(u.ID)
}

// openDMCmd opens (or creates) the DM with users (POST /v1/dms: one user = the
// 1:1 DM, several = a new group DM named name, "" = no name) and, on success,
// observes the channel and navigates to it (tag chat.dm_open:<first user>).
func openDMCmd(ctx *ui.Context, users []api.UserRef, name string) tea.Cmd {
	refs := slices.Clone(users)
	first := ""
	if len(refs) > 0 {
		first = string(refs[0])
	}
	req := api.OpenDMRequest{Users: refs}
	if name != "" {
		req.Name = api.Some(name)
	}
	return doAction(ctx, "chat.dm_open:"+first, func(c context.Context, cl *api.Client) (*api.Channel, error) {
		ch, _, err := cl.OpenDM(c, req)
		return ch, err
	}, func(ctx *ui.Context, ch *api.Channel, err error) tea.Cmd {
		if err != nil || ch == nil {
			return errorCmd(err, dmOpenFailed)
		}
		if ctx.Store != nil {
			ctx.Store.ObserveChannel(*ch)
		}
		return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: ch.ID})
	})
}
