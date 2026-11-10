package chat

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

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
	ui.RegisterOverlay(overlayInviteCreate, newInviteCreate)
	ui.RegisterOverlay(overlayInviteDirect, newInviteDirect)
	palette.Register(palette.Command{
		ID: "invite.create", Label: func(t *i18n.T) string { return t.Chat.CmdInviteCreate() }, Group: palette.GroupCommand,
		Keywords:   []string{"invite", "link", "code", "create"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwInviteCreate()) },
		Available:  canCreateInvite,
		Run: func(ctx *ui.Context, args string) tea.Cmd {
			return ui.OpenRegisteredOverlay(overlayInviteCreate, spaceArg(ctx, args))
		},
	})
	palette.Register(palette.Command{
		ID: "invite.direct", Label: func(t *i18n.T) string { return t.Chat.CmdInviteDirect() }, Group: palette.GroupCommand,
		Keywords:   []string{"invite", "friend", "direct"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwInviteDirect()) },
		Available:  canDirectInvite,
		Run: func(ctx *ui.Context, args string) tea.Cmd {
			return ui.OpenRegisteredOverlay(overlayInviteDirect, spaceArg(ctx, args))
		},
	})
}

// Registered overlay ids (args = the space id; "" = the current space).
const (
	overlayInviteCreate = "chat.invite_create"
	overlayInviteDirect = "chat.invite_direct"
)

// Invite management tags and row ids (the texts are chat.toml keys).
const (
	tagSpaceInvites     = "chat.space_invites:"
	tagInviteRevoke     = "chat.invite_revoke:"
	tagDirectCandidates = "chat.invite_direct_members:"
	inviteRowNewID      = "new"
	inviteRowDirectID   = "direct"
	inviteRowStatusID   = "status"
)

// inviteExpiries are the 만료 choices in expires_in_sec (0 = null: no expiry).
var inviteExpiries = []int{604800, 86400, 3600, 2592000, 0}

// expiryLabel is "7일" / "1시간" / "무기한" in c's language.
func expiryLabel(c *i18n.T, sec int) string {
	switch {
	case sec <= 0:
		return c.Chat.InviteForever()
	case sec%86400 == 0:
		return c.Chat.InviteDays(sec / 86400)
	}
	return c.Chat.InviteHours(sec / 3600)
}

// inviteUses are the 최대 사용 choices (0 = no limit: max_uses omitted).
var inviteUses = []int{0, 1, 5, 10, 25, 100}

// usesLabel is "무제한" / "10회" in c's language.
func usesLabel(c *i18n.T, n int) string {
	if n <= 0 {
		return c.Chat.InviteUsesNoLimit()
	}
	return c.Chat.InviteUsesN(n)
}

// spaceArg is the space of a palette/overlay args: args when given, else the current space.
func spaceArg(ctx *ui.Context, args string) string {
	if id := strings.TrimSpace(args); id != "" {
		return id
	}
	return currentSpaceID(ctx)
}

// knownSpace is spaceArg when the Store knows that space, else "".
func knownSpace(ctx *ui.Context, args string) string {
	id := spaceArg(ctx, args)
	if id == "" || ctx == nil || ctx.Store == nil || ctx.Store.Spaces[id] == nil {
		return ""
	}
	return id
}

// canCreateInvite: invite links of the current space are its owner's and admins' (api.md 5.8).
func canCreateInvite(ctx *ui.Context) bool { return canManageSpace(ctx, currentSpaceID(ctx)) }

// canCreateChannel: channels of the current space are made by its owner and admins (api.md 5.9).
func canCreateChannel(ctx *ui.Context) bool { return canManageSpace(ctx, currentSpaceID(ctx)) }

// canDirectInvite: any member may invite a friend to the current space (api.md 5.8).
func canDirectInvite(ctx *ui.Context) bool { return currentSpaceID(ctx) != "" }

// inviteRow is "AB12CD34 · 3/10회 · 11월 12일 만료 · 민지" (no limit "3회", no
// expiry "만료 없음") in at most width cells, plain and sanitized.
func inviteRow(ctx *ui.Context, inv api.Invite, width int) string {
	c := cat(ctx)
	uses := c.Chat.InviteUsesN(inv.UseCount)
	if inv.MaxUses != nil {
		uses = c.Chat.InviteUsesOf(inv.UseCount, *inv.MaxUses)
	}
	exp := c.Chat.InviteNoExpiry()
	if inv.ExpiresAt != nil {
		exp = inviteExpiryDate(ctx, *inv.ExpiresAt)
	}
	parts := []string{textwidth.Sanitize(inv.Code), uses, exp}
	if by := strings.TrimSpace(textwidth.Sanitize(displayName(inv.CreatedBy))); by != "" {
		parts = append(parts, by)
	}
	return textwidth.Truncate(strings.Join(parts, " · "), max(0, width), "…")
}

// inviteExpiryDate is "11월 12일 만료" in the user's zone ("2027년 1월 3일 만료" in
// another year, "만료됨" once past).
func inviteExpiryDate(ctx *ui.Context, at time.Time) string {
	now := time.Now()
	if ctx != nil && ctx.Now != nil {
		now = ctx.Now()
	}
	c := cat(ctx)
	if !at.After(now) {
		return c.Chat.InviteExpired()
	}
	loc := locOf(ctx)
	t, n := at.In(loc), now.In(loc)
	// The time catalog's month_day with its month_short_N word: the same text
	// as humanize's this-year date, built here so P5 batch 2 does not widen
	// humanize's API (P0-owned, open to P5 Task 5 only).
	month, _ := c.Lookup("time", "month_short_"+strconv.Itoa(int(t.Month())), nil)
	d := c.Time.MonthDay(month, strconv.Itoa(t.Day()))
	if t.Year() != n.Year() {
		d = c.Chat.DateWithYear(strconv.Itoa(t.Year()), d)
	}
	return c.Chat.InviteExpiresOn(d)
}

// ---- overlays ----

// inviteAddedMsg tells the invites tab of SpaceID that I created Invite.
type inviteAddedMsg struct {
	SpaceID string
	Invite  api.Invite
}

// inviteGoneMsg tells the invites tab of SpaceID that Code was revoked.
type inviteGoneMsg struct{ SpaceID, Code string }

// newInviteCreate is the chat.invite_create factory (args = the space): 만료
// (7일 | 1일 | 1시간 | 30일 | 무기한) and 최대 사용 (무제한 | 1..100회); on success
// it morphs into the created invite (code, "masil join <code>"). nil without a
// known space.
func newInviteCreate(ctx *ui.Context, args string) ui.Overlay {
	id := knownSpace(ctx, args)
	if id == "" {
		return nil
	}
	c := cat(ctx)
	expiries := make([]string, len(inviteExpiries))
	for i, sec := range inviteExpiries {
		expiries[i] = expiryLabel(c, sec)
	}
	uses := make([]string, len(inviteUses))
	for i, n := range inviteUses {
		uses[i] = usesLabel(c, n)
	}
	return newForm(formSpec{
		Title: c.Chat.CmdInviteCreate(), Submit: c.Chat.CreateSubmit(),
		Fields: []field{
			{Key: "expires", Label: c.Chat.InviteExpiryLabel(), Kind: fieldChoice, Choices: expiries},
			{Key: "uses", Label: c.Chat.InviteUsesLabel(), Kind: fieldChoice, Choices: uses},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			var r api.CreateInviteRequest
			for i, sec := range inviteExpiries {
				if expiries[i] == vals["expires"] {
					if sec > 0 {
						r.ExpiresInSec = api.Some(sec)
					} else {
						r.ExpiresInSec = api.Null[int]()
					}
				}
			}
			for i, n := range inviteUses {
				if n > 0 && uses[i] == vals["uses"] {
					r.MaxUses = api.Some(n)
				}
			}
			return tagInviteCreate + id, createInviteCmd(ctx, id, r)
		},
		OnDone: func(_ *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
			m, ok := result.(ui.ResultMsg[*api.Invite])
			if !ok || m.Value == nil {
				return nil, nil
			}
			inv := *m.Value
			added := func() tea.Msg { return inviteAddedMsg{SpaceID: id, Invite: inv} }
			return added, newInviteCreated(inv)
		},
	})
}

// directCandidates is the member set of a space for the direct invite picker
// (known = the member list was fetched).
type directCandidates struct {
	known   bool
	members map[string]bool
}

// newInviteDirect is the chat.invite_direct factory (args = the space): the
// space's members are fetched, then one friend who is not a member yet is
// picked (every friend when the member list could not be fetched; the server
// then answers 409 already_member, shown in the picker). nil without a known space.
func newInviteDirect(ctx *ui.Context, args string) ui.Overlay {
	id := knownSpace(ctx, args)
	if id == "" {
		return nil
	}
	c := cat(ctx)
	return newLoader(c.Chat.InviteDirectTitle(), tagDirectCandidates+id, func(c context.Context, cl *api.Client) (directCandidates, error) {
		ms, err := allPages(func(cursor string) (*api.Page[api.SpaceMember], error) {
			return cl.ListSpaceMembers(c, id, api.SpaceMembersQuery{PageOpts: api.PageOpts{Cursor: cursor, Limit: memberPageSize}})
		})
		if err != nil {
			return directCandidates{}, nil // unknown members: offer every friend
		}
		set := make(map[string]bool, len(ms))
		for _, m := range ms {
			set[m.User.ID] = true
		}
		return directCandidates{known: true, members: set}, nil
	}, func(_ *ui.Context, dc directCandidates) ui.Overlay {
		return newUserPicker(pickerSpec{
			Title: c.Chat.InviteDirectTitle(), Submit: c.Chat.InviteDirectSubmit(), Max: 1, Empty: c.Chat.InviteDirectNone(),
			Candidates: friendCandidates, Order: byPresence,
			Exclude: func(ctx *ui.Context, uid string) bool { return dc.members[uid] || blockedUser(ctx, uid) },
			OnPick: func(ctx *ui.Context, users []api.UserSummary, _ string) (string, tea.Cmd) {
				if len(users) == 0 {
					return "", nil
				}
				return tagInviteDirect + id, directInviteCmd(ctx, id, userRef(users[0]))
			},
			OnDone: func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
				if m, ok := result.(ui.ResultMsg[*api.DirectInvite]); ok {
					return directInviteDone(ctx, m), nil
				}
				return nil, nil
			},
		})
	})
}

// revokeInviteCmd revokes code (DELETE /v1/invites/{code}); the row leaves on success.
func revokeInviteCmd(ctx *ui.Context, spaceID, code string) tea.Cmd {
	return doAction(ctx, tagInviteRevoke+code, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RevokeInvite(c, code)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.RevokeFailed())
		}
		gone := func() tea.Msg { return inviteGoneMsg{SpaceID: spaceID, Code: code} }
		return tea.Batch(gone, ui.Toast(cat(ctx).Chat.Revoked(), ui.ToastSuccess))
	})
}

// copyInviteCmd copies "masil join <code>" (ui.Copy).
func copyInviteCmd(ctx *ui.Context, code string) tea.Cmd {
	return ui.Copy(ctx, clipboardLine(inviteJoinPrefix+code), cat(ctx).Chat.InviteCopied())
}

// ---- the invites tab ----

// spaceInvitesResult is one invites fetch.
type spaceInvitesResult struct {
	owner *inviteTab
	gen   int
	items []api.Invite
}

// inviteItem is one invites-tab row: an action (＋ 새 초대 링크 / ＋ 친구 직접
// 초대), the list state, or an invite.
type inviteItem struct {
	id    string
	label string // sanitized row text (an invite: its code first)
	inv   *api.Invite
}

// inviteTab is the 초대 tab (owner, admin): ＋ 새 초대 링크, ＋ 친구 직접 초대,
// then the space's live invite links (inviteRow). enter runs an action / shows
// an invite, y copies its share line, m opens 복사 / 폐기. Fetched each time the
// tab is entered.
type inviteTab struct {
	v       *spaceSettingsView
	items   []api.Invite
	list    *widgets.List[inviteItem]
	rows    []inviteItem
	gen     int
	loading bool
	loaded  bool
	failed  bool
}

func newSpaceInvites(v *spaceSettingsView) *inviteTab {
	b := &inviteTab{v: v}
	b.list = widgets.NewList(func(it inviteItem) string { return it.id }, b.render)
	return b
}

func (b *inviteTab) fetch(ctx *ui.Context) tea.Cmd {
	b.gen++
	b.loading = true
	gen, id := b.gen, b.v.spaceID
	return ui.Do(ctx, tagSpaceInvites+id, func(c context.Context, cl *api.Client) (spaceInvitesResult, error) {
		items, err := cl.ListInvites(c, id)
		return spaceInvitesResult{owner: b, gen: gen, items: items}, err
	})
}

// sync rebuilds the rows: the two actions, then the invites or the list state.
func (b *inviteTab) sync(ctx *ui.Context) {
	c := cat(ctx)
	rows := []inviteItem{{id: inviteRowNewID, label: c.Chat.InviteRowNew()}, {id: inviteRowDirectID, label: c.Chat.InviteRowDirect()}}
	status := ""
	switch {
	case len(b.items) > 0:
		for i := range b.items {
			inv := b.items[i]
			rows = append(rows, inviteItem{id: "inv:" + inv.Code, label: inviteRow(ctx, inv, 1<<20), inv: &inv})
		}
	case b.failed:
		status = c.Chat.InvitesFailed()
	case !b.loaded:
		status = c.Chat.Loading()
	default:
		status = c.Chat.InvitesNone()
	}
	if status != "" {
		rows = append(rows, inviteItem{id: inviteRowStatusID, label: status})
	}
	b.rows = rows
	b.list.SetItems(rows)
}

func (b *inviteTab) update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[spaceInvitesResult]:
		if m.Value.owner != b || m.Value.gen != b.gen {
			return nil
		}
		b.loading = false
		b.failed = m.Err != nil
		if m.Err == nil {
			b.loaded = true
			b.items = slices.Clone(m.Value.items)
		}
	case inviteAddedMsg:
		if m.SpaceID == b.v.spaceID && !slices.ContainsFunc(b.items, func(x api.Invite) bool { return x.Code == m.Invite.Code }) {
			b.items = append([]api.Invite{m.Invite}, b.items...)
		}
	case inviteGoneMsg:
		if m.SpaceID == b.v.spaceID {
			b.items = slices.DeleteFunc(b.items, func(x api.Invite) bool { return x.Code == m.Code })
		}
	}
	return nil
}

// open runs the row: an action opens its dialog, an invite shows itself.
func (b *inviteTab) open(ctx *ui.Context, it inviteItem) tea.Cmd {
	var o ui.Overlay
	switch {
	case it.id == inviteRowNewID:
		o = newInviteCreate(ctx, b.v.spaceID)
	case it.id == inviteRowDirectID:
		o = newInviteDirect(ctx, b.v.spaceID)
	case it.inv != nil:
		o = newInviteCreated(*it.inv)
	}
	if o == nil {
		return nil
	}
	return ui.OpenOverlay(o)
}

// menu is an invite's 복사 / 폐기 menu (nil on other rows).
func (b *inviteTab) menu(ctx *ui.Context, it inviteItem) tea.Cmd {
	if it.inv == nil {
		return nil
	}
	code, space := it.inv.Code, b.v.spaceID
	shown := textwidth.Sanitize(code)
	return ui.OpenOverlay(newMenu(shown, []menuItem{
		{Label: cat(ctx).Chat.ItemCopy(), Hint: "y", Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return copyInviteCmd(ctx, code), nil }},
		{Label: cat(ctx).Chat.ItemRevokeInvite(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			c := cat(ctx)
			return nil, widgets.Confirm(c.Chat.RevokeAsk(shown), c.Chat.RevokeBody(), c.Chat.ItemRevokeInvite(), c.Chat.Cancel(), revokeInviteCmd(ctx, space, code))
		}},
	}))
}

func (b *inviteTab) key(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	b.sync(ctx)
	it, ok := b.list.Selected()
	switch {
	case ctx.Keys.Matches(msg, keymap.ListOpen):
		if !ok {
			return false, nil
		}
		return true, b.open(ctx, it)
	case ctx.Keys.Matches(msg, keymap.MsgCopy):
		if !ok || it.inv == nil {
			return true, nil
		}
		return true, copyInviteCmd(ctx, it.inv.Code)
	case ctx.Keys.Matches(msg, keymap.MsgActions):
		if !ok {
			return false, nil
		}
		return true, b.menu(ctx, it)
	}
	return b.list.HandleKey(ctx, msg)
}

// mouse: a click selects a row, a click on the selected row runs it, a right
// click opens an invite's menu (runs an action row).
func (b *inviteTab) mouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	b.sync(ctx)
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return b.list.HandleMouse(ctx, msg, local)
	}
	top := slices.IndexFunc(b.rows, func(it inviteItem) bool { return it.id == b.list.Anchor().ItemID })
	i := max(top, 0) + local.Y
	if local.Y < 0 || i >= len(b.rows) {
		return nil
	}
	it := b.rows[i]
	switch {
	case click.Button == tea.MouseRight:
		b.list.Select(it.id)
		if it.inv != nil {
			return b.menu(ctx, it)
		}
		return b.open(ctx, it)
	case click.Button == tea.MouseLeft:
		if cur, ok := b.list.Selected(); ok && cur.id == it.id {
			return b.open(ctx, it)
		}
		b.list.Select(it.id)
	}
	return nil
}

func (b *inviteTab) render(ctx *ui.Context, it inviteItem, selected bool, width int) []string {
	return []string{settingsRow(ctx, b.v.focused, selected, it.id == inviteRowStatusID, it.label, width)}
}

// view draws the rows in exactly size.
func (b *inviteTab) view(ctx *ui.Context, _ *theme.Theme, size ui.Size) string {
	b.sync(ctx)
	return b.list.View(ctx, size)
}

// settingsRow is a settings list row in exactly width cells: the "›" cursor
// (when focused and selected), the text (dim for a state line), a right margin.
func settingsRow(ctx *ui.Context, focused, selected, dim bool, text string, width int) string {
	t := themeOf(ctx)
	if width <= 0 {
		return ""
	}
	cur := t.S.Base.Render(" ")
	if selected && focused {
		cur = t.S.Title.Render("›")
	}
	cw, margin := width-1, ""
	if cw <= 0 {
		return cur
	}
	if cw >= 8 {
		cw, margin = cw-1, blank(t, 1)
	}
	st := t.S.Base
	switch {
	case dim:
		st = t.S.Dim
	case selected && focused:
		st = t.S.Selected
	}
	return cur + cell(st.Render, text, cw) + margin
}
