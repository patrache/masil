package chat

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Space members / bans limits and tags (the texts are chat.toml keys).
const (
	memberFilterDebounce = 300 * time.Millisecond
	memberFilterMax      = 100 // the search query limit (api.md 0.3)
	banReasonMax         = 200
	memberMoreRowID      = "more"
	tagSpaceBans         = "chat.space_bans:"
	tagSpaceRole         = "chat.space_role:"
	tagSpaceKick         = "chat.space_kick:"
	tagSpaceBan          = "chat.space_ban:"
	tagSpaceUnban        = "chat.space_unban:"
	tagSpacePresence     = "chat.presence:"
	memberLineInset      = 1 // the filter row above the list
)

// spaceMemberMsg tells the settings of SpaceID that a member changed after one
// of my actions (Gone = left the space, by UserID).
type spaceMemberMsg struct {
	SpaceID string
	Member  api.SpaceMember
	Gone    bool
	UserID  string
}

func memberChanged(spaceID string, m api.SpaceMember) tea.Cmd {
	return func() tea.Msg { return spaceMemberMsg{SpaceID: spaceID, Member: m} }
}

func memberGone(spaceID, userID string) tea.Cmd {
	return func() tea.Msg { return spaceMemberMsg{SpaceID: spaceID, Gone: true, UserID: userID} }
}

// spaceBanGoneMsg tells the settings of SpaceID that UserID was unbanned.
type spaceBanGoneMsg struct{ SpaceID, UserID string }

// ---- member row actions ----

// memberRowActions are my actions on member m of the space (api.md 3.5): the
// owner sets roles (관리자로 지정 / 관리자 해제) and kicks / bans anyone else, an
// admin kicks / bans members only; everyone gets DM 보내기, 사용자 신고 and 차단
// (not for a user I already blocked). My own row has none.
func memberRowActions(ctx *ui.Context, spaceID string, m api.SpaceMember) []menuItem {
	if ctx == nil || ctx.Store == nil || m.User.ID == "" || isMe(ctx, m.User.ID) {
		return nil
	}
	u := m.User
	if cu := ctx.Store.User(u.ID); cu != nil {
		u = *cu
	}
	name := textwidth.Sanitize(displayName(u))
	me := myRole(ctx, spaceID)
	c := cat(ctx)
	var items []menuItem
	if me == api.RoleOwner {
		switch m.Role {
		case api.RoleMember:
			items = append(items, menuItem{Label: c.Chat.ItemMakeAdmin(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return setRoleCmd(ctx, spaceID, u, api.RoleAdmin), nil
			}})
		case api.RoleAdmin:
			items = append(items, menuItem{Label: c.Chat.ItemUnmakeAdmin(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return setRoleCmd(ctx, spaceID, u, api.RoleMember), nil
			}})
		}
	}
	if me == api.RoleOwner && m.Role != api.RoleOwner || me == api.RoleAdmin && m.Role == api.RoleMember {
		items = append(items,
			menuItem{Label: c.Chat.KickYes(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				c := cat(ctx)
				return nil, widgets.Confirm(c.Chat.SpaceKickAsk(name), c.Chat.SpaceKickBody(), c.Chat.KickYes(), c.Chat.Cancel(), kickCmd(ctx, spaceID, u))
			}},
			menuItem{Label: c.Chat.ItemBan(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return nil, banForm(cat(ctx), spaceID, u)
			}},
		)
	}
	items = append(items,
		menuItem{Label: cat(ctx).Chat.ItemSendDm(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return openDMCmd(ctx, []api.UserRef{userRef(u)}, ""), nil
		}},
		menuItem{Label: cat(ctx).Chat.ItemReportUser(), Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, widgets.ReportDialog("user", u.ID, name)
		}},
	)
	if !ctx.Store.IsBlocked(u.ID) {
		items = append(items, menuItem{Label: cat(ctx).Chat.ItemBlockUser(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, blockConfirm(ctx, u)
		}})
	}
	return items
}

// setRoleCmd sets u's role (PATCH members/{user}); the row follows at once.
func setRoleCmd(ctx *ui.Context, spaceID string, u api.UserSummary, role api.SpaceRole) tea.Cmd {
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, tagSpaceRole+u.ID, func(c context.Context, cl *api.Client) (*api.SpaceMember, error) {
		return cl.SetSpaceMemberRole(c, spaceID, userRef(u), role)
	}, func(ctx *ui.Context, got *api.SpaceMember, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.RoleFailed())
		}
		m := api.SpaceMember{User: u, Role: role}
		if got != nil && got.User.ID == u.ID {
			m = *got
		}
		text := cat(ctx).Chat.MadeAdmin(name)
		if m.Role != api.RoleAdmin {
			text = cat(ctx).Chat.UnmadeAdmin(name)
		}
		return tea.Batch(memberChanged(spaceID, m), ui.Toast(text, ui.ToastSuccess))
	})
}

// kickCmd kicks u (DELETE members/{user}, no query); the row leaves at once.
func kickCmd(ctx *ui.Context, spaceID string, u api.UserSummary) tea.Cmd {
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, tagSpaceKick+u.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RemoveSpaceMember(c, spaceID, userRef(u), api.RemoveMemberOpts{})
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.SpaceKickFailed())
		}
		return tea.Batch(memberGone(spaceID, u.ID), ui.Toast(cat(ctx).Chat.Kicked(name), ui.ToastSuccess))
	})
}

// banForm asks the ban reason (0..200, optional), then bans u
// (DELETE members/{user}?ban=true&reason=); an error stays in the form.
func banForm(c *i18n.T, spaceID string, u api.UserSummary) ui.Overlay {
	name := textwidth.Sanitize(displayName(u))
	tag := tagSpaceBan + u.ID
	return newForm(formSpec{
		Title: c.Chat.BanTitle(name), Submit: c.Chat.BanSubmit(),
		Fields: []field{{
			Key: "reason", Label: c.Chat.BanReasonLabel(), Placeholder: c.Chat.BanReasonHolder(), Max: banReasonMax,
			Validate: func(v string) string {
				if utf8.RuneCountInString(strings.TrimSpace(v)) > banReasonMax {
					return c.Chat.BanReasonTooLong()
				}
				return ""
			},
		}},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			opts := api.RemoveMemberOpts{Ban: true, Reason: strings.TrimSpace(vals["reason"])}
			return tag, ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (struct{}, error) {
				return struct{}{}, cl.RemoveSpaceMember(c, spaceID, userRef(u), opts)
			})
		},
		OnDone: func(ctx *ui.Context, _ tea.Msg) (tea.Cmd, ui.Overlay) {
			return tea.Batch(memberGone(spaceID, u.ID), ui.Toast(cat(ctx).Chat.Banned(name), ui.ToastSuccess)), nil
		},
	})
}

// ---- the members tab ----

// spaceMemberRow is one members-tab row (one line): a member or the "더 보기" row.
type spaceMemberRow struct {
	ID     string
	member api.SpaceMember
	more   bool
}

// spaceMembersPage is one member page fetch (gen = the fetch generation, more =
// appended to the current list).
type spaceMembersPage struct {
	owner *memberTab
	gen   int
	more  bool
	page  *api.Page[api.SpaceMember]
}

// memberFilterMsg is the filter debounce tick (gen = the filter generation).
type memberFilterMsg struct {
	owner *memberTab
	gen   int
}

// spacePresenceTick is the members tab's presence re-poll tick of one polling generation.
type spacePresenceTick struct {
	owner *memberTab
	gen   uint64
}

// spacePresenceResult is one presence poll of the members tab.
type spacePresenceResult struct {
	owner *memberTab
	items []api.PresenceItem
}

// memberTab is the members tab: the "/" filter input (300ms debounce,
// ListSpaceMembers{Q}), the members in the server's order (role, then name)
// with presence and role labels, the next page fetched at the bottom. The
// server pushes no presence of members who are not friends or DM peers
// (events.md 4.6), so the listed members' presence is polled (GET
// /v1/presence, tag chat.presence:<space>) every 60s while the tab is shown.
type memberTab struct {
	v     *spaceSettingsView
	items []api.SpaceMember
	rows  []spaceMemberRow
	list  *widgets.List[spaceMemberRow]

	next    *string // the next page cursor (nil = none)
	gen     int
	loading bool
	loaded  bool
	failed  bool

	filter    textinput.Model
	filtering bool
	query     string // the query of the shown list
	filterGen int
	curX      int // the filter cursor column (last View)

	presPolling bool   // a spacePresenceTick of presGen is pending
	presGen     uint64 // latest presence polling generation
}

func newSpaceMembers(v *spaceSettingsView) *memberTab {
	s := &memberTab{v: v, filter: newTextInput()}
	s.filter.CharLimit = memberFilterMax
	s.filter.Blur()
	s.list = widgets.NewList(func(r spaceMemberRow) string { return r.ID }, s.render)
	return s
}

// fetch asks for the first page of the current filter (more = the next page).
func (s *memberTab) fetch(ctx *ui.Context, more bool) tea.Cmd {
	q := api.SpaceMembersQuery{}
	if more {
		if s.next == nil || s.loading {
			return nil
		}
		q.Q, q.Cursor = s.query, *s.next
	} else {
		s.gen++
		s.query = strings.TrimSpace(s.filter.Value())
		q.Q = s.query
	}
	s.loading = true
	gen, id := s.gen, s.v.spaceID
	return ui.Do(ctx, tagSpaceMembers+id, func(c context.Context, cl *api.Client) (spaceMembersPage, error) {
		p, err := cl.ListSpaceMembers(c, id, q)
		return spaceMembersPage{owner: s, gen: gen, more: more, page: p}, err
	})
}

// presenceShown: the members tab of a space I still see is shown.
func (s *memberTab) presenceShown(ctx *ui.Context) bool {
	return s.v.shown && s.v.tab == tabMembers && s.v.space(ctx) != nil
}

// syncPresence starts polling once the tab is shown with its members loaded
// (a poll at once, then every presenceEvery), and stops it when the tab or the
// route is hidden, so a pending tick of the old generation is dropped and the
// next show polls at once again.
func (s *memberTab) syncPresence(ctx *ui.Context) tea.Cmd {
	if !s.presenceShown(ctx) {
		if s.presPolling {
			s.presPolling = false
			s.presGen++
		}
		return nil
	}
	if s.presPolling || !s.loaded {
		return nil
	}
	s.presPolling = true
	s.presGen++
	return tea.Batch(s.pollPresence(ctx, s.items), ui.After(ctx, presenceEvery, spacePresenceTick{owner: s, gen: s.presGen}))
}

// onPresenceTick re-polls and re-arms while shown; hidden, polling stops.
func (s *memberTab) onPresenceTick(ctx *ui.Context, m spacePresenceTick) tea.Cmd {
	if m.owner != s || m.gen != s.presGen || !s.presPolling {
		return nil
	}
	if !s.presenceShown(ctx) {
		s.presPolling = false
		return nil
	}
	return tea.Batch(s.pollPresence(ctx, s.items), ui.After(ctx, presenceEvery, spacePresenceTick{owner: s, gen: s.presGen}))
}

// pollPresence requests the presence of members (me excluded, ≤100 per request).
func (s *memberTab) pollPresence(ctx *ui.Context, members []api.SpaceMember) tea.Cmd {
	users := make([]api.UserSummary, 0, len(members))
	for _, m := range members {
		users = append(users, m.User)
	}
	ids := presenceIDs(ctx, users)
	if len(ids) == 0 {
		return nil
	}
	return ui.Do(ctx, tagSpacePresence+s.v.spaceID, func(c context.Context, cl *api.Client) (spacePresenceResult, error) {
		items, err := getPresence(c, cl, ids)
		return spacePresenceResult{owner: s, items: items}, err
	})
}

// sortSpaceMembers is the server's order: owner, admin, member, then display name.
func sortSpaceMembers(ms []api.SpaceMember) {
	rank := func(r api.SpaceRole) int {
		switch r {
		case api.RoleOwner:
			return 0
		case api.RoleAdmin:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(ms, func(a, b api.SpaceMember) int {
		return cmp.Or(cmp.Compare(rank(a.Role), rank(b.Role)), strings.Compare(displayName(a.User), displayName(b.User)))
	})
}

// matchesQuery reports whether m matches the shown list's query (handle or name substring).
func (s *memberTab) matchesQuery(m api.SpaceMember) bool {
	q := strings.ToLower(strings.TrimPrefix(s.query, "@"))
	return q == "" || strings.Contains(strings.ToLower(m.User.Handle), q) || strings.Contains(strings.ToLower(m.User.DisplayName), q)
}

// upsert replaces m's row (role, user); a joined member is added when it matches the query.
func (s *memberTab) upsert(m api.SpaceMember, joined bool) {
	if i := slices.IndexFunc(s.items, func(x api.SpaceMember) bool { return x.User.ID == m.User.ID }); i >= 0 {
		if m.JoinedAt.IsZero() {
			m.JoinedAt = s.items[i].JoinedAt
		}
		s.items[i] = m
	} else if joined && s.matchesQuery(m) {
		s.items = append(s.items, m)
	} else {
		return
	}
	sortSpaceMembers(s.items)
}

// remove drops the row of userID.
func (s *memberTab) remove(userID string) {
	s.items = slices.DeleteFunc(s.items, func(x api.SpaceMember) bool { return x.User.ID == userID })
}

// sync rebuilds the rows (the list keeps the selection by ID).
func (s *memberTab) sync() {
	rows := make([]spaceMemberRow, 0, len(s.items)+1)
	for _, m := range s.items {
		rows = append(rows, spaceMemberRow{ID: m.User.ID, member: m})
	}
	if s.next != nil {
		rows = append(rows, spaceMemberRow{ID: memberMoreRowID, more: true})
	}
	s.rows = rows
	s.list.SetItems(rows)
}

func (s *memberTab) blur() {
	s.filtering = false
	s.filter.Blur()
}

// debounce restarts the filter debounce.
func (s *memberTab) debounce(ctx *ui.Context) tea.Cmd {
	s.filterGen++
	return ui.After(ctx, memberFilterDebounce, memberFilterMsg{owner: s, gen: s.filterGen})
}

// settled fetches the filter's query unless the list already shows it.
func (s *memberTab) settled(ctx *ui.Context) tea.Cmd {
	s.filterGen++ // a pending tick is stale now
	if strings.TrimSpace(s.filter.Value()) == s.query {
		return nil
	}
	return s.fetch(ctx, false)
}

func (s *memberTab) update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[spaceMembersPage]:
		if m.Value.owner != s || m.Value.gen != s.gen {
			return nil
		}
		s.loading = false
		if m.Err != nil {
			s.failed = !s.loaded || !m.Value.more
			return errorCmd(m.Err, cat(ctx).Chat.MembersFailed())
		}
		s.failed = false
		s.loaded = true
		var items []api.SpaceMember
		if m.Value.page != nil {
			items = m.Value.page.Items
			s.next = m.Value.page.NextCursor
		} else {
			s.next = nil
		}
		for _, it := range items {
			if ctx.Store != nil {
				ctx.Store.ObserveUser(it.User)
			}
		}
		if m.Value.more {
			for _, it := range items {
				if !slices.ContainsFunc(s.items, func(x api.SpaceMember) bool { return x.User.ID == it.User.ID }) {
					s.items = append(s.items, it)
				}
			}
		} else {
			s.items = slices.Clone(items)
		}
		s.sync()
		if s.presPolling && s.presenceShown(ctx) {
			return s.pollPresence(ctx, items) // a new page or filter: its members' presence now
		}
		return nil
	case spacePresenceTick:
		return s.onPresenceTick(ctx, m)
	case ui.ResultMsg[spacePresenceResult]:
		if m.Value.owner == s {
			storePresence(ctx, m.Value.items) // a failed background poll keeps the old values, no toast
		}
		return nil
	case memberFilterMsg:
		if m.owner == s && m.gen == s.filterGen {
			return s.settled(ctx)
		}
	case spaceMemberMsg:
		if m.SpaceID != s.v.spaceID {
			return nil
		}
		if m.Gone {
			s.remove(m.UserID)
		} else {
			s.upsert(m.Member, false)
		}
	}
	return nil
}

// paste feeds pasted text into the filter while it is focused.
func (s *memberTab) paste(ctx *ui.Context, p tea.PasteMsg) tea.Cmd {
	if !s.filtering {
		return nil
	}
	before := s.filter.Value()
	_, cmd := inputUpdate(&s.filter, p)
	if s.filter.Value() != before {
		return tea.Batch(cmd, s.debounce(ctx))
	}
	return cmd
}

// filterKey handles keys while the filter input is focused: esc / ↓ leave it,
// enter applies at once, the rest edits (debounced fetch).
func (s *memberTab) filterKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc", "down":
		s.blur()
		return true, nil
	case "enter":
		s.blur()
		return true, s.settled(ctx)
	}
	before := s.filter.Value()
	handled, cmd := inputUpdate(&s.filter, msg)
	if s.filter.Value() != before {
		return true, tea.Batch(cmd, s.debounce(ctx))
	}
	return handled, cmd
}

// open runs a row: the "더 보기" row fetches the next page, a member opens its menu.
func (s *memberTab) open(ctx *ui.Context, r spaceMemberRow) tea.Cmd {
	if r.more {
		return s.fetch(ctx, true)
	}
	items := memberRowActions(ctx, s.v.spaceID, r.member)
	if len(items) == 0 {
		return nil
	}
	return ui.OpenOverlay(newMenu(textwidth.Sanitize(displayName(r.member.User)), items))
}

// atBottom fetches the next page when the selection reached the last member (or the "더 보기" row).
func (s *memberTab) atBottom(ctx *ui.Context) tea.Cmd {
	r, ok := s.list.Selected()
	if !ok || s.next == nil {
		return nil
	}
	if i := slices.IndexFunc(s.rows, func(x spaceMemberRow) bool { return x.ID == r.ID }); i < len(s.items)-1 {
		return nil
	}
	return s.fetch(ctx, true)
}

func (s *memberTab) key(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s.sync()
	switch {
	case ctx.Keys.Matches(msg, keymap.ChannelSearch):
		s.filtering = true
		s.filter.Focus()
		s.filter.CursorEnd()
		return true, nil
	case ctx.Keys.Matches(msg, keymap.ListOpen), ctx.Keys.Matches(msg, keymap.MsgActions):
		r, ok := s.list.Selected()
		if !ok {
			return false, nil
		}
		return true, s.open(ctx, r)
	}
	handled, cmd := s.list.HandleKey(ctx, msg)
	if handled {
		return true, tea.Batch(cmd, s.atBottom(ctx))
	}
	return false, cmd
}

// rowAt is the row drawn at list line y (every row is one line), or -1.
func (s *memberTab) rowAt(y, h int) int {
	if y < 0 || y >= h {
		return -1
	}
	top := slices.IndexFunc(s.rows, func(r spaceMemberRow) bool { return r.ID == s.list.Anchor().ItemID })
	if top < 0 {
		top = 0
	}
	if i := top + y; i < len(s.rows) {
		return i
	}
	return -1
}

// mouse: a click on the filter row focuses it; a left click selects a row and a
// click on the selected row opens it; a right click opens its menu; the wheel scrolls.
func (s *memberTab) mouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	s.sync()
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		if local.Y >= memberLineInset {
			return s.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - memberLineInset})
		}
		return nil
	}
	if local.Y < memberLineInset {
		if click.Button == tea.MouseLeft {
			s.filtering = true
			s.filter.Focus()
		}
		return nil
	}
	s.blur()
	i := s.rowAt(local.Y-memberLineInset, s.v.size.H-1-memberLineInset)
	if i < 0 {
		return nil
	}
	r := s.rows[i]
	switch click.Button {
	case tea.MouseRight:
		s.list.Select(r.ID)
		return s.open(ctx, r)
	case tea.MouseLeft:
		if cur, ok := s.list.Selected(); ok && cur.ID == r.ID {
			return s.open(ctx, r)
		}
		s.list.Select(r.ID)
		return s.atBottom(ctx)
	}
	return nil
}

// cursor is the filter input's cursor relative to the route (nil when not filtering).
func (s *memberTab) cursor(size ui.Size) *tea.Cursor {
	if !s.filtering || size.H < 2 || s.curX >= size.W {
		return nil
	}
	return tea.NewCursor(s.curX, 1)
}

// view draws the filter row and the list (or its state) in exactly size.
func (s *memberTab) view(ctx *ui.Context, t *theme.Theme, size ui.Size) string {
	s.sync()
	w := size.W
	p := 1
	if w < 12 {
		p = 0
	}
	iw := max(0, w-2*p-2)
	var line string
	switch {
	case s.filtering:
		var col int
		line, col = inputLine(t, s.filter, cat(ctx).Chat.MemberFilterHolder(), iw)
		line = blank(t, p) + t.S.Title.Render("/ ") + line + blank(t, p)
		s.curX = p + 2 + col
	case s.filter.Value() != "":
		line = blank(t, p) + t.S.Dim.Render("/ ") + cell(t.S.Base.Render, textwidth.Sanitize(s.filter.Value()), iw) + blank(t, p)
	default:
		line = blank(t, p) + cell(t.S.Dim.Render, cat(ctx).Chat.MemberFilterIdle(), iw+2) + blank(t, p)
	}
	if w < 2*p+2 {
		line = blank(t, w)
	}
	lines := []string{line}
	if size.H > 1 {
		body := ui.Size{W: w, H: size.H - 1}
		var out string
		switch {
		case len(s.rows) > 0:
			out = s.list.View(ctx, body)
		case s.failed:
			out = widgets.Empty(t, body, "⚠", cat(ctx).Chat.MembersFailed(), "")
		case !s.loaded:
			out = widgets.Empty(t, body, "", cat(ctx).Chat.Loading(), "")
		case s.query != "":
			out = widgets.Empty(t, body, "🔍", cat(ctx).Chat.MembersNoMatch(), "")
		default:
			out = widgets.Empty(t, body, "👥", cat(ctx).Chat.SpaceMembersNone(), "")
		}
		lines = append(lines, strings.Split(out, "\n")...)
	}
	return strings.Join(textwidth.Fit(lines, w, size.H), "\n")
}

// render draws a row: column 0 is the focus cursor "›", then "<dot> 이름 @handle"
// and the right-aligned role label (방장 / 관리자), which never gets cut.
func (s *memberTab) render(ctx *ui.Context, r spaceMemberRow, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && s.v.focused && !s.filtering {
		cur = t.S.Title.Render("›")
	}
	cw, margin := width-1, ""
	if cw <= 0 {
		return []string{cur}
	}
	if cw >= 8 { // a right margin, like the filter row's
		cw, margin = cw-1, blank(t, 1)
	}
	if r.more {
		text := cat(ctx).Chat.MemberMore()
		if s.loading {
			text = cat(ctx).Chat.MemberMoreLoading()
		}
		return []string{cur + cell(t.S.Dim.Render, text, cw) + margin}
	}
	u := r.member.User
	if ctx != nil && ctx.Store != nil {
		if cu := ctx.Store.User(u.ID); cu != nil {
			u = *cu
		}
	}
	return []string{cur + memberLine(cat(ctx), t, userPresence(ctx, u.ID), u, r.member.Role, cw) + margin}
}

// memberLine is "<dot> 이름 @handle … 방장" in exactly cw cells: the role label
// stays whole (dropped only when nothing else would fit), the handle gives way
// first, then the name.
func memberLine(c *i18n.T, t *theme.Theme, st api.PresenceStatus, u api.UserSummary, role api.SpaceRole, cw int) string {
	lead, leadW := "", 0
	if cw > 2 {
		lead, leadW = widgets.PresenceDot(t, st)+t.S.Base.Render(" "), 2
	}
	room := cw - leadW
	label, labelSt := "", t.S.Dim
	switch role {
	case api.RoleOwner:
		label, labelSt = c.Chat.RoleOwner(), t.S.Title
	case api.RoleAdmin:
		label = c.Chat.RoleAdmin()
	}
	lw := textwidth.Width(label)
	if lw > 0 && room >= lw+2 {
		room -= lw + 1
	} else {
		label, lw = "", 0
	}
	name := strings.TrimSpace(textwidth.Sanitize(displayName(u)))
	handle := ""
	if h := textwidth.Sanitize(u.Handle); h != "" {
		handle = " @" + h
	}
	nameW := textwidth.Width(name)
	if nameW+textwidth.Width(handle) > room {
		if room-nameW >= 4 {
			handle = textwidth.Truncate(handle, room-nameW, "…")
		} else {
			handle = ""
		}
	}
	name = textwidth.Truncate(name, max(room-textwidth.Width(handle), 0), "…")
	gap := room - textwidth.Width(name) - textwidth.Width(handle)
	out := lead + t.S.Base.Render(name) + t.S.Dim.Render(handle) + blank(t, gap)
	if lw > 0 {
		out += t.S.Base.Render(" ") + labelSt.Render(label)
	}
	return out
}

// ---- the bans tab ----

// spaceBansResult is one bans fetch.
type spaceBansResult struct {
	owner *banTab
	gen   int
	items []api.SpaceBan
}

// banTab is the 차단됨 tab: "이름 @handle · 사유 · 차단: 차단한 사람", enter
// asks, then unbans. Fetched each time the tab is entered.
type banTab struct {
	v       *spaceSettingsView
	items   []api.SpaceBan
	list    *widgets.List[friendItem]
	rows    []friendItem
	gen     int
	loading bool
	loaded  bool
	failed  bool
}

func newSpaceBans(v *spaceSettingsView) *banTab {
	b := &banTab{v: v}
	b.list = widgets.NewList(func(it friendItem) string { return it.ID }, b.render)
	return b
}

func (b *banTab) fetch(ctx *ui.Context) tea.Cmd {
	b.gen++
	b.loading = true
	gen, id := b.gen, b.v.spaceID
	return ui.Do(ctx, tagSpaceBans+id, func(c context.Context, cl *api.Client) (spaceBansResult, error) {
		items, err := cl.ListSpaceBans(c, id)
		return spaceBansResult{owner: b, gen: gen, items: items}, err
	})
}

func (b *banTab) sync(ctx *ui.Context) {
	rows := make([]friendItem, 0, len(b.items))
	for _, ban := range b.items {
		detail := strings.TrimSpace(textwidth.Sanitize(ban.Reason))
		by := cat(ctx).Chat.BannedBy(strings.TrimSpace(textwidth.Sanitize(displayName(ban.BannedBy))))
		if detail != "" {
			detail += " · " + by
		} else {
			detail = by
		}
		rows = append(rows, friendItem{
			ID: "ban:" + ban.User.ID, Label: strings.TrimSpace(textwidth.Sanitize(displayName(ban.User))),
			Handle: textwidth.Sanitize(ban.User.Handle), Detail: detail, user: ban.User,
		})
	}
	b.rows = rows
	b.list.SetItems(rows)
}

func (b *banTab) update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[spaceBansResult]:
		if m.Value.owner != b || m.Value.gen != b.gen {
			return nil
		}
		b.loading = false
		b.failed = m.Err != nil
		if m.Err == nil {
			b.loaded = true
			b.items = slices.Clone(m.Value.items)
		}
	case spaceBanGoneMsg:
		if m.SpaceID == b.v.spaceID {
			b.items = slices.DeleteFunc(b.items, func(x api.SpaceBan) bool { return x.User.ID == m.UserID })
		}
	}
	return nil
}

// unban asks before lifting the ban of the row.
func (b *banTab) unban(ctx *ui.Context, it friendItem) tea.Cmd {
	c := cat(ctx)
	return ui.OpenOverlay(widgets.Confirm(c.Chat.UnbanAsk(it.Label), c.Chat.UnbanBody(), c.Chat.UnbanYes(), c.Chat.Cancel(), unbanCmd(ctx, b.v.spaceID, it.user)))
}

// unbanCmd lifts u's ban (DELETE bans/{user}); the row leaves at once.
func unbanCmd(ctx *ui.Context, spaceID string, u api.UserSummary) tea.Cmd {
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, tagSpaceUnban+u.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.Unban(c, spaceID, userRef(u))
	}, func(_ *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.UnbanFailed())
		}
		gone := func() tea.Msg { return spaceBanGoneMsg{SpaceID: spaceID, UserID: u.ID} }
		return tea.Batch(gone, ui.Toast(cat(ctx).Chat.Unbanned(name), ui.ToastSuccess))
	})
}

func (b *banTab) key(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	b.sync(ctx)
	if ctx.Keys.Matches(msg, keymap.ListOpen) || ctx.Keys.Matches(msg, keymap.MsgActions) {
		it, ok := b.list.Selected()
		if !ok {
			return false, nil
		}
		return true, b.unban(ctx, it)
	}
	return b.list.HandleKey(ctx, msg)
}

func (b *banTab) mouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	b.sync(ctx)
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return b.list.HandleMouse(ctx, msg, local)
	}
	top := slices.IndexFunc(b.rows, func(it friendItem) bool { return it.ID == b.list.Anchor().ItemID })
	i := max(top, 0) + local.Y
	if local.Y < 0 || i >= len(b.rows) {
		return nil
	}
	it := b.rows[i]
	if cur, ok := b.list.Selected(); ok && cur.ID == it.ID || click.Button == tea.MouseRight {
		b.list.Select(it.ID)
		return b.unban(ctx, it)
	}
	if click.Button == tea.MouseLeft {
		b.list.Select(it.ID)
	}
	return nil
}

func (b *banTab) render(ctx *ui.Context, it friendItem, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && b.v.focused {
		cur = t.S.Title.Render("›")
	}
	cw, margin := width-1, ""
	if cw <= 0 {
		return []string{cur}
	}
	if cw >= 8 {
		cw, margin = cw-1, blank(t, 1)
	}
	return []string{cur + personRow(t, it, false, cw) + margin}
}

// view draws the bans (or their state) in exactly size.
func (b *banTab) view(ctx *ui.Context, t *theme.Theme, size ui.Size) string {
	b.sync(ctx)
	switch {
	case len(b.rows) > 0:
		return b.list.View(ctx, size)
	case b.failed:
		return widgets.Empty(t, size, "⚠", cat(ctx).Chat.BansFailed(), "")
	case !b.loaded:
		return widgets.Empty(t, size, "", cat(ctx).Chat.Loading(), "")
	}
	return widgets.Empty(t, size, "🙂", cat(ctx).Chat.BansNone(), "")
}
