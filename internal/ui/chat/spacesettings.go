package chat

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteSpaceSettings, newSpaceSettings)
	palette.Register(palette.Command{
		ID: "space.settings", Title: "방 설정", Keywords: []string{"space", "settings", "members", "roles", "bans", "방", "설정", "멤버", "역할", "관리"}, Group: palette.GroupNav,
		Available: func(ctx *ui.Context) bool { return currentSpaceID(ctx) != "" },
		Run: func(ctx *ui.Context, _ string) tea.Cmd {
			id := currentSpaceID(ctx)
			if id == "" {
				return nil
			}
			return ui.Navigate(ui.Route{Kind: ui.RouteSpaceSettings, ID: id})
		},
	})
	palette.Register(palette.Command{
		ID: "space.leave", Title: "방 나가기", Keywords: []string{"space", "leave", "quit", "방", "나가기", "탈퇴"}, Group: palette.GroupCommand,
		Available: func(ctx *ui.Context) bool { return currentSpaceID(ctx) != "" },
		Run: func(ctx *ui.Context, _ string) tea.Cmd {
			id := currentSpaceID(ctx)
			if id == "" {
				return nil
			}
			if myRole(ctx, id) == api.RoleOwner {
				return ui.Toast(ownerLeaveText, ui.ToastInfo)
			}
			return ui.OpenOverlay(leaveSpaceConfirm(ctx, id, false))
		},
	})
}

// settingsTab is a tab of the space settings route.
type settingsTab int

// Space settings tabs.
const (
	tabOverview settingsTab = iota
	tabMembers
	tabInvites
	tabChannels
	tabBans
)

// label is the tab's label.
func (t settingsTab) label() string {
	switch t {
	case tabMembers:
		return "멤버"
	case tabInvites:
		return "초대"
	case tabChannels:
		return "채널"
	case tabBans:
		return "차단됨"
	}
	return "개요"
}

// Space settings texts.
const (
	settingsTitleSuffix = " 설정"
	settingsFallback    = "방 설정"
	spaceGoneText       = "볼 수 없는 방이에요"
	spaceGoneHint       = "방이 삭제됐거나 더 이상 멤버가 아니에요"
	roleOwnerLabel      = "방장"
	roleAdminLabel      = "관리자"
	roleMemberLabel     = "멤버"

	itemEditSpace     = "방 정보 바꾸기"
	itemTransferSpace = "방장 넘기기"
	itemLeaveSpace    = "방 나가기"
	itemDeleteSpace   = "방 삭제"

	editSpaceSubmit   = "바꾸기"
	spaceUpdatedText  = "방 정보를 바꿨어요"
	spaceUpdateFailed = "방 정보를 바꾸지 못했어요"
	transferTitle     = "새 방장 고르기"
	transferSubmit    = "고르기"
	transferAskSuffix = "님에게 방장을 넘길까요?"
	transferBody      = "넘기면 나는 관리자가 돼요"
	transferYes       = "넘기기"
	transferredSuffix = "님에게 방장을 넘겼어요"
	transferFailed    = "방장을 넘기지 못했어요"
	membersLoadTitle  = "멤버 불러오기"
	deleteSpaceAsk    = " 방을 삭제할까요?"
	deleteSpaceBody   = "모든 채널과 메시지가 사라지고 되돌릴 수 없어요"
	deleteSpaceYes    = "삭제"
	spaceDeletedText  = "방을 삭제했어요"
	spaceDeleteFailed = "방을 삭제하지 못했어요"
	leaveSpaceAsk     = "에서 나갈까요?"
	leaveSpaceBody    = "다시 들어오려면 초대를 받아야 해요"
	leaveSpaceYes     = "나가기"
	spaceLeftText     = "방에서 나왔어요"
	leavingSpaceText  = "나가는 중…"
	spaceLeaveFailed  = "방에서 나가지 못했어요"
	ownerLeaveText    = "방장은 방장을 넘긴 뒤에 나갈 수 있어요"

	tagSpaceGet        = "chat.space_get:"
	tagSpaceUpdate     = "chat.space_update:"
	tagSpaceTransfer   = "chat.space_transfer:"
	tagSpaceDelete     = "chat.space_delete:"
	tagSpaceLeave      = "chat.space_leave:"
	tagSpaceMembers    = "chat.space_members:"
	tagTransferMembers = "chat.space_transfer_members:"
)

// roleLabel is the Korean name of a role ("" for an unknown role).
func roleLabel(r api.SpaceRole) string {
	switch r {
	case api.RoleOwner:
		return roleOwnerLabel
	case api.RoleAdmin:
		return roleAdminLabel
	case api.RoleMember:
		return roleMemberLabel
	}
	return ""
}

// spaceLeftMsg tells the settings of SpaceID that I left or deleted it from there.
type spaceLeftMsg struct{ SpaceID string }

// spaceGot is the GET /v1/spaces/{id} answer of one view.
type spaceGot struct {
	owner *spaceSettingsView
	d     *api.SpaceDetail
}

// settingsSaved is the Persistent state of the space settings route.
type settingsSaved struct{ Tab int }

// spaceSettingsView is RouteSpaceSettings{ID: space}: the tabs 개요 / 멤버 (all
// members) and 초대 / 채널 / 차단됨 (owner and admin). It reads the space from
// the Store (kept fresh by space.* events) and shows "볼 수 없는 방이에요" once
// it is gone from there.
type spaceSettingsView struct {
	spaceID string
	tab     settingsTab
	tabs    *widgets.Tabs
	size    ui.Size
	focused bool
	gone    bool // GET said the space is not (or no longer) mine to see

	hasFocus      bool // the app's focus is on this route (FocusChangedMsg / input)
	homeAfterBack bool // stepped back after leaving: open home once the focus moved

	// shown: the route is on screen. A route is placed when it is navigated to
	// (created then, or reused with the focus moving to it), and another route
	// replaces it in its slot by a navigation, which moves the focus to that
	// route in that slot; a focus move to another slot leaves it shown.
	shown bool
	slot  layout.SlotID // the slot the route was last focused in ("" = Main)

	actCur int // overview: selected action
	actTop int // overview: first drawn line (mouse)
	actRow int // overview: line of the first action

	members  *memberTab
	invites  *inviteTab
	channels *channelTab
	bans     *banTab
}

var (
	_ ui.Component     = (*spaceSettingsView)(nil)
	_ ui.KeyHandler    = (*spaceSettingsView)(nil)
	_ ui.MouseHandler  = (*spaceSettingsView)(nil)
	_ ui.Titled        = (*spaceSettingsView)(nil)
	_ ui.Helper        = (*spaceSettingsView)(nil)
	_ ui.Cursorer      = (*spaceSettingsView)(nil)
	_ ui.InputCapturer = (*spaceSettingsView)(nil)
	_ ui.Persistent    = (*spaceSettingsView)(nil)
)

// newSpaceSettings is the RouteSpaceSettings factory (r.ID = the space).
func newSpaceSettings(_ *ui.Context, r ui.Route) ui.Component {
	v := &spaceSettingsView{spaceID: r.ID, tabs: widgets.NewTabs(), shown: true}
	v.members = newSpaceMembers(v)
	v.invites = newSpaceInvites(v)
	v.channels = newSpaceChannels(v)
	v.bans = newSpaceBans(v)
	return v
}

// space is the Store's space (nil once gone).
func (v *spaceSettingsView) space(ctx *ui.Context) *api.SpaceDetail {
	if v.gone || ctx == nil || ctx.Store == nil {
		return nil
	}
	return ctx.Store.Spaces[v.spaceID]
}

// tabSet is the tabs my role sees: everyone 개요 / 멤버, owner and admin also 초대 / 채널 / 차단됨.
func (v *spaceSettingsView) tabSet(ctx *ui.Context) []settingsTab {
	if canManageSpace(ctx, v.spaceID) {
		return []settingsTab{tabOverview, tabMembers, tabInvites, tabChannels, tabBans}
	}
	return []settingsTab{tabOverview, tabMembers}
}

// clampTab keeps the tab inside the current set (the last one when it left it).
func (v *spaceSettingsView) clampTab(ctx *ui.Context) {
	set := v.tabSet(ctx)
	if !slices.Contains(set, v.tab) {
		v.tab = set[len(set)-1]
	}
}

// setTab switches to the tab at index i of the set (clamped); entering 초대 /
// 차단됨 fetches the invites / bans.
func (v *spaceSettingsView) setTab(ctx *ui.Context, i int) tea.Cmd {
	set := v.tabSet(ctx)
	i = max(0, min(len(set)-1, i))
	if set[i] == v.tab {
		return nil
	}
	v.members.blur()
	v.tab = set[i]
	presence := v.members.syncPresence(ctx) // members tab shown: poll; left: stop
	switch v.tab {
	case tabInvites:
		return tea.Batch(presence, v.invites.fetch(ctx))
	case tabBans:
		return tea.Batch(presence, v.bans.fetch(ctx))
	}
	return presence
}

// onFocus follows whether the route is shown (see shown) from a focus change.
func (v *spaceSettingsView) onFocus(m ui.FocusChangedMsg) {
	slot := v.slot
	if slot == "" {
		slot = layout.SlotMain
	}
	switch {
	case m.RouteKey == v.routeKey():
		v.shown, v.slot = true, m.Slot
	case m.RouteKey != "" && m.Slot == slot:
		v.shown = false // another route took the slot
	}
}

func (v *spaceSettingsView) tabIndex(ctx *ui.Context) int {
	return max(0, slices.Index(v.tabSet(ctx), v.tab))
}

// ---- overview ----

// overviewActions are my role's actions on the space (api.md 3.5).
func (v *spaceSettingsView) overviewActions(ctx *ui.Context) []menuItem {
	sd := v.space(ctx)
	if sd == nil {
		return nil
	}
	id := v.spaceID
	name := textwidth.Sanitize(sd.Name)
	role := sd.MyRole
	var items []menuItem
	if role == api.RoleOwner || role == api.RoleAdmin {
		items = append(items, menuItem{Label: itemEditSpace, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, editSpaceForm(ctx, id)
		}})
	}
	if role == api.RoleOwner {
		items = append(items, menuItem{Label: itemTransferSpace, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, transferLoader(ctx, id)
		}})
	} else {
		items = append(items, menuItem{Label: itemLeaveSpace, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, leaveSpaceConfirm(ctx, id, true)
		}})
	}
	if role == api.RoleOwner {
		items = append(items, menuItem{Label: itemDeleteSpace, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, widgets.Confirm(name+deleteSpaceAsk, deleteSpaceBody, deleteSpaceYes, cancelText, deleteSpaceCmd(ctx, id, true))
		}})
	} else {
		items = append(items, menuItem{Label: itemReportSpace, Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, widgets.ReportDialog("space", id, name)
		}})
	}
	return items
}

// runAction runs overview action i: its overlay opens (an overlay-less action runs its Cmd).
func (v *spaceSettingsView) runAction(ctx *ui.Context, i int) tea.Cmd {
	items := v.overviewActions(ctx)
	if i < 0 || i >= len(items) || items[i].Run == nil {
		return nil
	}
	v.actCur = i
	cmd, o := items[i].Run(ctx)
	if o != nil {
		return tea.Batch(cmd, ui.OpenOverlay(o))
	}
	return cmd
}

// overviewLines are the overview's lines (exactly w cells each) and the line of the first action.
func (v *spaceSettingsView) overviewLines(ctx *ui.Context, t *theme.Theme, w int) ([]string, int) {
	sd := v.space(ctx)
	if sd == nil {
		return nil, 0
	}
	p := 1
	if w < 12 {
		p = 0
	}
	cw := max(0, w-2*p)
	pad := func(s string) string { return blank(t, p) + s + blank(t, w-p-cw) }
	name := textwidth.Sanitize(sd.Name)
	if sd.IconEmoji != nil && *sd.IconEmoji != "" {
		name = textwidth.Sanitize(*sd.IconEmoji) + " " + name
	}
	lines := []string{pad(cell(t.S.Title.Render, name, cw))}
	if desc := strings.TrimSpace(textwidth.Sanitize(sd.Description)); desc != "" {
		lines = append(lines, pad(cell(t.S.Base.Render, desc, cw)))
	}
	info := "멤버 " + strconv.Itoa(sd.MemberCount) + "명 · 내 역할: " + roleLabel(sd.MyRole)
	lines = append(lines, pad(cell(t.S.Dim.Render, info, cw)), blank(t, w))
	first := len(lines)
	items := v.overviewActions(ctx)
	v.actCur = max(0, min(len(items)-1, v.actCur))
	for i, it := range items {
		st := t.S.Base
		if it.Danger {
			st = t.S.Error
		}
		marker := "  "
		if i == v.actCur {
			marker = "› "
			if v.focused {
				st = t.S.Selected
			}
		}
		lines = append(lines, pad(cell(st.Render, marker+it.Label, cw)))
	}
	return lines, first
}

// overviewView draws the overview in size, scrolled so the selected action shows.
func (v *spaceSettingsView) overviewView(ctx *ui.Context, t *theme.Theme, size ui.Size) []string {
	lines, first := v.overviewLines(ctx, t, size.W)
	top := 0
	if cur := first + v.actCur; cur >= size.H {
		top = cur - size.H + 1
	}
	top = max(0, min(top, len(lines)-size.H))
	v.actTop, v.actRow = top, first
	return lines[top:min(len(lines), top+size.H)]
}

// ---- drawing ----

func (v *spaceSettingsView) View(ctx *ui.Context, focused bool) string {
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	v.focused = focused
	t := themeOf(ctx)
	if v.space(ctx) == nil {
		return widgets.Empty(t, v.size, "🚫", spaceGoneText, spaceGoneHint)
	}
	v.clampTab(ctx)
	set := v.tabSet(ctx)
	tabs := make([]widgets.Tab, len(set))
	for i, tab := range set {
		tabs[i] = widgets.Tab{Label: tab.label()}
	}
	v.tabs.Set(tabs, v.tabIndex(ctx))
	lines := []string{v.tabs.View(t, w)}
	if h > 1 {
		body := ui.Size{W: w, H: h - 1}
		var rows []string
		switch v.tab {
		case tabOverview:
			rows = v.overviewView(ctx, t, body)
		case tabMembers:
			rows = strings.Split(v.members.view(ctx, t, body), "\n")
		case tabInvites:
			rows = strings.Split(v.invites.view(ctx, t, body), "\n")
		case tabChannels:
			rows = strings.Split(v.channels.view(ctx, t, body), "\n")
		case tabBans:
			rows = strings.Split(v.bans.view(ctx, t, body), "\n")
		}
		for len(rows) < body.H {
			rows = append(rows, blank(t, w))
		}
		lines = append(lines, rows...)
	}
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// Title is "<방 이름> 설정".
func (v *spaceSettingsView) Title(ctx *ui.Context) (string, ui.Badge) {
	if sd := v.space(ctx); sd != nil {
		return textwidth.Sanitize(sd.Name) + settingsTitleSuffix, ui.Badge{}
	}
	return settingsFallback, ui.Badge{}
}

func (v *spaceSettingsView) Help(ctx *ui.Context) []key.Binding {
	if v.space(ctx) == nil {
		return nil
	}
	tabs := key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "탭"))
	switch v.tab {
	case tabMembers:
		if v.members.filtering {
			return []key.Binding{
				key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "찾기")),
				key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "그만 찾기")),
			}
		}
		return append(ctx.Keys.Help(keymap.MsgActions), key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "멤버 찾기")), tabs)
	case tabInvites:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "열기")),
			key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "복사")),
			key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "메뉴")),
			tabs,
		}
	case tabChannels:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "설정")),
			key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "새 채널")),
			tabs,
		}
	case tabBans:
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "차단 풀기")), tabs}
	}
	return append(ctx.Keys.Help(keymap.ListOpen), tabs)
}

// Cursor is the filter input's cursor (members tab, while filtering).
func (v *spaceSettingsView) Cursor() *tea.Cursor {
	if v.tab != tabMembers || v.gone {
		return nil
	}
	return v.members.cursor(v.size)
}

func (v *spaceSettingsView) CapturesInput() bool {
	return v.tab == tabMembers && !v.gone && v.members.filtering
}

func (v *spaceSettingsView) SaveState() any { return settingsSaved{Tab: int(v.tab)} }

func (v *spaceSettingsView) LoadState(s any) {
	switch x := s.(type) {
	case settingsSaved:
		v.tab = settingsTab(max(0, min(int(tabBans), x.Tab)))
	case *settingsSaved:
		if x != nil {
			v.tab = settingsTab(max(0, min(int(tabBans), x.Tab)))
		}
	}
}

// ---- updates ----

// Init fetches the space and its first member page (the invites / bans with their tab).
func (v *spaceSettingsView) Init(ctx *ui.Context) tea.Cmd {
	if ctx == nil || ctx.Store == nil || ctx.Store.Spaces[v.spaceID] == nil {
		return nil
	}
	id := v.spaceID
	get := ui.Do(ctx, tagSpaceGet+id, func(c context.Context, cl *api.Client) (spaceGot, error) {
		d, err := cl.GetSpace(c, id)
		return spaceGot{owner: v, d: d}, err
	})
	cmds := []tea.Cmd{get, v.members.fetch(ctx, false)}
	if canManageSpace(ctx, id) {
		switch v.tab {
		case tabInvites:
			cmds = append(cmds, v.invites.fetch(ctx))
		case tabBans:
			cmds = append(cmds, v.bans.fetch(ctx))
		}
	}
	return tea.Batch(cmds...)
}

func (v *spaceSettingsView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

func (v *spaceSettingsView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if r, ok := msg.(ui.RouteUpdatedMsg); ok && r.Route.Key() == v.routeKey() {
		v.shown = true
	}
	if f, ok := msg.(ui.FocusChangedMsg); ok {
		v.onFocus(f)
	}
	cmd := v.update(ctx, msg)
	return tea.Batch(cmd, v.members.syncPresence(ctx))
}

func (v *spaceSettingsView) update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[spaceGot]:
		if m.Value.owner != v {
			return nil
		}
		if m.Err != nil {
			if api.IsCode(m.Err, "not_found") {
				v.gone = true
			}
			return nil
		}
		if d := m.Value.d; d != nil && ctx.Store != nil {
			if sd := ctx.Store.Spaces[v.spaceID]; sd != nil {
				sd.Space = d.Space
			}
		}
		return nil
	case ui.ResultMsg[msgAction]:
		return landAction(ctx, m) // once-guarded: whoever sees it first lands it
	case ui.FocusChangedMsg:
		v.hasFocus = m.RouteKey == v.routeKey()
		if !v.hasFocus && v.homeAfterBack {
			v.homeAfterBack = false
			return goHome(ctx)
		}
		return nil
	case spaceLeftMsg:
		if m.SpaceID != v.spaceID {
			return nil
		}
		if v.hasFocus { // ui.Back pops this route; the focus moving off it opens home
			v.homeAfterBack = true
			return ui.Back()
		}
		return goHome(ctx)
	case tea.PasteMsg:
		if v.tab == tabMembers {
			return v.members.paste(ctx, m)
		}
		return nil
	case ui.EventMsg:
		v.onEvent(m.Event)
	}
	icmd := v.invites.update(ctx, msg)
	if cmd := v.members.update(ctx, msg); cmd != nil {
		return tea.Batch(icmd, cmd)
	}
	return tea.Batch(icmd, v.bans.update(ctx, msg))
}

// onEvent follows space.member_* of this space in the member list.
func (v *spaceSettingsView) onEvent(ev realtime.Event) {
	switch ev.Type {
	case "space.member_joined", "space.member_updated":
		var d realtime.SpaceMemberEvent
		if json.Unmarshal(ev.Data, &d) == nil && d.SpaceID == v.spaceID {
			v.members.upsert(d.Member, ev.Type == "space.member_joined")
		}
	case "space.member_left":
		var d realtime.SpaceMemberLeft
		if json.Unmarshal(ev.Data, &d) == nil && d.SpaceID == v.spaceID {
			v.members.remove(d.UserID)
		}
	}
}

// ---- input ----

// routeKey is this route's key (focus tracking).
func (v *spaceSettingsView) routeKey() string {
	return ui.Route{Kind: ui.RouteSpaceSettings, ID: v.spaceID}.Key()
}

func (v *spaceSettingsView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	v.hasFocus, v.shown = true, true
	handled, cmd := v.handleKey(ctx, msg)
	return handled, tea.Batch(cmd, v.members.syncPresence(ctx))
}

func (v *spaceSettingsView) handleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if v.space(ctx) == nil {
		return false, nil
	}
	v.focused = true
	v.clampTab(ctx)
	if v.tab == tabMembers && v.members.filtering {
		return v.members.filterKey(ctx, msg)
	}
	switch msg.String() {
	case "left", "h":
		return true, v.setTab(ctx, v.tabIndex(ctx)-1)
	case "right", "l":
		return true, v.setTab(ctx, v.tabIndex(ctx)+1)
	}
	switch v.tab {
	case tabOverview:
		n := len(v.overviewActions(ctx))
		switch {
		case ctx.Keys.Matches(msg, keymap.ListUp):
			v.actCur = max(0, v.actCur-1)
			return true, nil
		case ctx.Keys.Matches(msg, keymap.ListDown):
			v.actCur = min(n-1, v.actCur+1)
			return true, nil
		case ctx.Keys.Matches(msg, keymap.ListOpen):
			return true, v.runAction(ctx, v.actCur)
		}
	case tabMembers:
		return v.members.key(ctx, msg)
	case tabInvites:
		return v.invites.key(ctx, msg)
	case tabChannels:
		return v.channels.key(ctx, msg)
	case tabBans:
		return v.bans.key(ctx, msg)
	}
	return false, nil
}

// HandleMouse: a click on the tab row switches tabs; the body goes to the tab.
func (v *spaceSettingsView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	v.hasFocus, v.shown = true, true
	return tea.Batch(v.handleMouse(ctx, msg, local), v.members.syncPresence(ctx))
}

func (v *spaceSettingsView) handleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.space(ctx) == nil {
		return nil
	}
	click, isClick := msg.(tea.MouseClickMsg)
	if local.Y == 0 {
		if isClick && click.Button == tea.MouseLeft {
			if i, hit := v.tabs.Hit(local.X); hit {
				return v.setTab(ctx, i)
			}
		}
		return nil
	}
	body := ui.Point{X: local.X, Y: local.Y - 1}
	switch v.tab {
	case tabOverview:
		if !isClick || click.Button != tea.MouseLeft {
			return nil
		}
		i := v.actTop + body.Y - v.actRow
		if i < 0 || i >= len(v.overviewActions(ctx)) {
			return nil
		}
		if i == v.actCur {
			return v.runAction(ctx, i)
		}
		v.actCur = i
	case tabMembers:
		return v.members.mouse(ctx, msg, body)
	case tabInvites:
		return v.invites.mouse(ctx, msg, body)
	case tabChannels:
		return v.channels.mouse(ctx, msg, body)
	case tabBans:
		return v.bans.mouse(ctx, msg, body)
	}
	return nil
}

// ---- space actions ----

// spaceGoneCmd delivers a local space event (no seq) so the space leaves the
// Store and every list at once (the server's own event repeats it harmlessly).
func spaceGoneCmd(ctx *ui.Context, typ, spaceID, reason string) tea.Cmd {
	var data []byte
	var err error
	if typ == "space.deleted" {
		data, err = json.Marshal(realtime.SpaceDeleted{SpaceID: spaceID})
	} else {
		data, err = json.Marshal(realtime.SpaceRemoved{SpaceID: spaceID, Reason: reason})
	}
	if err != nil {
		return nil
	}
	ev := realtime.Event{Type: typ, TS: ctx.Now(), Data: data}
	return func() tea.Msg { return ui.EventMsg{Event: ev} }
}

// editSpaceForm is 방 정보 바꾸기: 이름 / 설명 / 아이콘 prefilled; only changed
// fields are sent (아이콘 없음 = null).
func editSpaceForm(ctx *ui.Context, spaceID string) ui.Overlay {
	sd := ctx.Store.Spaces[spaceID]
	if sd == nil {
		return nil
	}
	cur := iconNone
	if sd.IconEmoji != nil && *sd.IconEmoji != "" {
		cur = *sd.IconEmoji
	}
	icons := slices.Clone(spaceIcons)
	if !slices.Contains(icons, cur) {
		icons = slices.Insert(icons, 1, cur)
	}
	oldName, oldDesc := sd.Name, sd.Description
	tag := tagSpaceUpdate + spaceID
	return newForm(formSpec{
		Title: itemEditSpace, Submit: editSpaceSubmit,
		Fields: []field{
			{Key: "name", Label: "이름", Placeholder: "예: 노바 개발방", Value: oldName, Max: spaceNameMax, Validate: validSpaceName},
			{Key: "description", Label: "설명 (선택)", Placeholder: "어떤 방인가요?", Value: oldDesc, Max: spaceDescMax, Validate: validSpaceDesc},
			{Key: "icon", Label: "아이콘", Kind: fieldChoice, Choices: icons, Value: cur},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			var req api.UpdateSpaceRequest
			if n := strings.TrimSpace(vals["name"]); n != oldName {
				req.Name = api.Some(n)
			}
			if d := strings.TrimSpace(vals["description"]); d != oldDesc {
				req.Description = api.Some(d)
			}
			if icon := vals["icon"]; icon != cur {
				if icon == iconNone {
					req.IconEmoji = api.Null[string]()
				} else {
					req.IconEmoji = api.Some(icon)
				}
			}
			if !req.Name.Set && !req.Description.Set && !req.IconEmoji.Set {
				return "", nil
			}
			return tag, ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (*api.Space, error) {
				return cl.UpdateSpace(c, spaceID, req)
			})
		},
		OnDone: func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
			m, ok := result.(ui.ResultMsg[*api.Space])
			if !ok {
				return nil, nil
			}
			if sd := ctx.Store.Spaces[spaceID]; sd != nil && m.Value != nil {
				role := sd.MyRole
				sd.Space = *m.Value
				sd.MyRole = role
			}
			return ui.Toast(spaceUpdatedText, ui.ToastSuccess), nil
		},
	})
}

// transferLoader is 방장 넘기기: the space's members are fetched (every page, so
// anyone can become owner), then picked (one, never me), then confirmed.
func transferLoader(_ *ui.Context, spaceID string) ui.Overlay {
	return newLoader(membersLoadTitle, tagTransferMembers+spaceID, func(c context.Context, cl *api.Client) ([]api.UserSummary, error) {
		ms, err := allPages(func(cursor string) (*api.Page[api.SpaceMember], error) {
			return cl.ListSpaceMembers(c, spaceID, api.SpaceMembersQuery{PageOpts: api.PageOpts{Cursor: cursor, Limit: memberPageSize}})
		})
		users := make([]api.UserSummary, len(ms))
		for i, m := range ms {
			users[i] = m.User
		}
		return users, err
	}, func(ctx *ui.Context, users []api.UserSummary) ui.Overlay {
		var picked api.UserSummary
		return newUserPicker(pickerSpec{
			Title: transferTitle, Submit: transferSubmit, Max: 1,
			Candidates: func(*ui.Context) []api.UserSummary { return users },
			OnPick: func(_ *ui.Context, us []api.UserSummary, _ string) (string, tea.Cmd) {
				if len(us) > 0 {
					picked = us[0]
				}
				return "", nil
			},
			OnDone: func(ctx *ui.Context, _ tea.Msg) (tea.Cmd, ui.Overlay) {
				if picked.ID == "" {
					return nil, nil
				}
				name := textwidth.Sanitize(displayName(picked))
				return nil, widgets.Confirm(name+transferAskSuffix, transferBody, transferYes, cancelText, transferSpaceCmd(ctx, spaceID, picked))
			},
		})
	})
}

// transferSpaceCmd makes u the owner; on success I am admin at once and the
// member list shows both new roles.
func transferSpaceCmd(ctx *ui.Context, spaceID string, u api.UserSummary) tea.Cmd {
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, tagSpaceTransfer+spaceID, func(c context.Context, cl *api.Client) (*api.Space, error) {
		return cl.TransferSpace(c, spaceID, userRef(u))
	}, func(ctx *ui.Context, _ *api.Space, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, transferFailed)
		}
		s := ctx.Store
		var cmds []tea.Cmd
		if sd := s.Spaces[spaceID]; sd != nil {
			sd.OwnerID = u.ID
			sd.MyRole = api.RoleAdmin
		}
		if s.Me != nil {
			cmds = append(cmds, memberChanged(spaceID, api.SpaceMember{User: s.Me.UserSummary, Role: api.RoleAdmin}))
		}
		cmds = append(cmds, memberChanged(spaceID, api.SpaceMember{User: u, Role: api.RoleOwner}), ui.Toast(name+transferredSuffix, ui.ToastSuccess))
		return tea.Batch(cmds...)
	})
}

// leftNav is where to go once the space is gone: from its settings route
// (fromSettings) that route steps back first, then home opens (spaceLeftMsg);
// from anywhere else home opens.
func leftNav(ctx *ui.Context, spaceID string, fromSettings bool) tea.Cmd {
	if fromSettings {
		return func() tea.Msg { return spaceLeftMsg{SpaceID: spaceID} }
	}
	return goHome(ctx)
}

// deleteSpaceCmd deletes the space; on success it is gone and home opens.
func deleteSpaceCmd(ctx *ui.Context, spaceID string, fromSettings bool) tea.Cmd {
	return doAction(ctx, tagSpaceDelete+spaceID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.DeleteSpace(c, spaceID)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, spaceDeleteFailed)
		}
		return tea.Batch(spaceGoneCmd(ctx, "space.deleted", spaceID, ""), leftNav(ctx, spaceID, fromSettings), ui.Toast(spaceDeletedText, ui.ToastSuccess))
	})
}

// leaveSpaceConfirm asks before leaving the space (DELETE members/@me).
func leaveSpaceConfirm(ctx *ui.Context, spaceID string, fromSettings bool) ui.Overlay {
	name := ""
	if sd := ctx.Store.Spaces[spaceID]; sd != nil {
		name = textwidth.Sanitize(sd.Name)
	}
	// it stays open until the result lands: space.leave runs from any route
	return confirmAndAwait(name+leaveSpaceAsk, leaveSpaceBody, leaveSpaceYes, cancelText, leavingSpaceText, tagSpaceLeave+spaceID, leaveSpaceCmd(ctx, spaceID, fromSettings))
}

// leaveSpaceCmd leaves the space; on success it is gone and home opens.
func leaveSpaceCmd(ctx *ui.Context, spaceID string, fromSettings bool) tea.Cmd {
	return doAction(ctx, tagSpaceLeave+spaceID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RemoveSpaceMember(c, spaceID, api.MeRef, api.RemoveMemberOpts{})
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, spaceLeaveFailed)
		}
		return tea.Batch(spaceGoneCmd(ctx, "space.removed", spaceID, "left"), leftNav(ctx, spaceID, fromSettings), ui.Toast(spaceLeftText, ui.ToastSuccess))
	})
}
