package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteInvite, newInvite)
	palette.Register(palette.Command{
		ID: "invites.inbox", Label: func(t *i18n.T) string { return t.Chat.CmdInvitesInbox() }, Group: palette.GroupNav,
		Keywords:   []string{"invites", "inbox", "direct invite"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwInvitesInbox()) },
		Run:        func(*ui.Context, string) tea.Cmd { return ui.Navigate(ui.Route{Kind: ui.RouteInvite, Arg: "direct"}) },
	})
}

// directArg is the RouteInvite Arg of direct invites.
const directArg = "direct"

// Invite screen layout and tags (the texts are chat.toml keys).
const (
	inviteCardMaxW = 48

	tagInvitePrefix        = "chat.invite:"
	tagInviteAcceptPrefix  = "chat.invite_accept:"
	tagInviteOpenPrefix    = "chat.invite_open:"
	tagDirectAcceptPrefix  = "chat.direct_accept:"
	tagDirectDeclinePrefix = "chat.direct_decline:"
)

// inviteErrorText is the in-place text of an invite failure (api.md 0.5, 5.8).
func inviteErrorText(t *i18n.T, err error) string {
	switch {
	case api.IsCode(err, "invite_invalid"):
		switch invalidReason(err) {
		case "expired":
			return orKo(t).Chat.InviteExpiredErr()
		case "exhausted":
			return orKo(t).Chat.InviteExhausted()
		case "revoked":
			return orKo(t).Chat.InviteRevoked()
		}
	case api.IsCode(err, "not_found"):
		return orKo(t).Chat.InviteNotFound()
	case api.IsCode(err, "banned_from_space"):
		return orKo(t).Chat.SpaceBanned()
	}
	return errtext.Describe(t, err)
}

// invalidReason is details.reason of an invite_invalid error ("" when absent).
func invalidReason(err error) string {
	var e *api.Error
	if !errors.As(err, &e) || len(e.Details) == 0 {
		return ""
	}
	var d struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(e.Details, &d) != nil {
		return ""
	}
	return d.Reason
}

// finalInviteError reports an error that settles the invite (shown in place, not
// retried): invalid, unknown, or I am banned from the space.
func finalInviteError(err error) bool {
	return api.IsCode(err, "invite_invalid") || api.IsCode(err, "not_found") || api.IsCode(err, "banned_from_space")
}

// inviteMode is the RouteInvite variant.
type inviteMode int

const (
	inviteLink       inviteMode = iota // ID = invite code: preview + accept
	inviteDirectList                   // Arg="direct", ID="": the pending Store.DirectInvites
	inviteDirectOne                    // Arg="direct", ID = invite id: accept / decline
)

// inviteFetched is one preview fetch (gen = the fetch generation).
type inviteFetched struct {
	gen     int
	preview *api.InvitePreview
}

// inviteSpace is a space to enter (accept results and the already-member fetch).
type inviteSpace struct{ space *api.SpaceDetail }

// inviteDeclined is a direct-invite decline result.
type inviteDeclined struct{}

// inviteView is RouteInvite: a link preview (ID = code), the list of received
// direct invites (Arg="direct", ID=""), or one direct invite (Arg="direct", ID).
type inviteView struct {
	mode     inviteMode
	code     string // inviteLink
	inviteID string // inviteDirectOne

	// inviteLink
	preview  *api.InvitePreview
	err      error // fetch or settled accept error
	loading  bool
	fetchGen int

	// inviteDirectOne
	invite *api.DirectInvite // last seen copy
	gone   bool              // no longer in Store.DirectInvites
	final  error             // a settled accept error

	busy     string // the action in flight (its tag), "" = idle
	busyText string
	spinner  *widgets.Spinner

	// inviteDirectList
	list    *widgets.List[api.DirectInvite]
	items   []api.DirectInvite
	focused bool

	size ui.Size
}

var (
	_ ui.Component    = (*inviteView)(nil)
	_ ui.KeyHandler   = (*inviteView)(nil)
	_ ui.MouseHandler = (*inviteView)(nil)
	_ ui.Titled       = (*inviteView)(nil)
	_ ui.Helper       = (*inviteView)(nil)
)

// newInvite is the RouteInvite factory.
func newInvite(ctx *ui.Context, r ui.Route) ui.Component {
	v := &inviteView{spinner: widgets.NewSpinner()}
	switch {
	case r.Arg == directArg && r.ID == "":
		v.mode = inviteDirectList
		v.list = widgets.NewList(func(in api.DirectInvite) string { return in.ID }, v.renderRow)
	case r.Arg == directArg:
		v.mode, v.inviteID = inviteDirectOne, r.ID
	default:
		v.mode, v.code = inviteLink, strings.TrimSpace(r.ID)
	}
	v.sync(ctx)
	return v
}

func (v *inviteView) Init(ctx *ui.Context) tea.Cmd {
	if v.mode == inviteLink {
		return v.fetch(ctx)
	}
	return nil
}

func (v *inviteView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

// fetch (re)loads the link preview (the newest fetch wins).
func (v *inviteView) fetch(ctx *ui.Context) tea.Cmd {
	v.fetchGen++
	gen, code := v.fetchGen, v.code
	v.loading, v.err = true, nil
	return tea.Batch(v.spinner.Start(ctx), ui.Do(ctx, tagInvitePrefix+code, func(c context.Context, cl *api.Client) (inviteFetched, error) {
		p, err := cl.GetInvite(c, code)
		return inviteFetched{gen: gen, preview: p}, err
	}))
}

// sync refreshes the Store-backed state: the list rows, the direct invite copy.
func (v *inviteView) sync(ctx *ui.Context) {
	if ctx == nil || ctx.Store == nil {
		return
	}
	switch v.mode {
	case inviteDirectList:
		v.items = slices.Clone(ctx.Store.DirectInvites)
		v.list.SetItems(v.items)
	case inviteDirectOne:
		i := slices.IndexFunc(ctx.Store.DirectInvites, func(in api.DirectInvite) bool { return in.ID == v.inviteID })
		if i >= 0 {
			in := ctx.Store.DirectInvites[i]
			v.invite, v.gone = &in, false
		} else if v.busy == "" {
			v.gone = true // an accept in flight keeps its card until the result lands
		}
	}
}

// ---- actions ----

// setBusy marks tag in flight with a status text and starts the spinner.
func (v *inviteView) setBusy(ctx *ui.Context, tag, text string, cmd tea.Cmd) tea.Cmd {
	v.busy, v.busyText = tag, text
	return tea.Batch(v.spinner.Start(ctx), cmd)
}

func (v *inviteView) idle() {
	v.busy, v.busyText = "", ""
	v.spinner.Stop()
}

// ready reports whether the primary action can run now.
func (v *inviteView) ready() bool {
	if v.busy != "" {
		return false
	}
	switch v.mode {
	case inviteLink:
		return v.preview != nil && v.err == nil && !v.loading
	case inviteDirectOne:
		return v.invite != nil && !v.gone && v.final == nil
	}
	return false
}

// primary runs enter: join (or open, when already a member) / accept.
func (v *inviteView) primary(ctx *ui.Context) tea.Cmd {
	if !v.ready() {
		return nil
	}
	if v.mode == inviteDirectOne {
		return v.acceptDirect(ctx)
	}
	if v.preview.AlreadyMember {
		return v.openSpace(ctx, v.preview.Space.ID)
	}
	code := v.code
	tag := tagInviteAcceptPrefix + code
	return v.setBusy(ctx, tag, cat(ctx).Chat.InviteJoining(), ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (inviteSpace, error) {
		d, err := cl.AcceptInvite(c, code)
		return inviteSpace{space: d}, err
	}))
}

// openSpace opens a space I am in: from the Store, else fetched first.
func (v *inviteView) openSpace(ctx *ui.Context, id string) tea.Cmd {
	if cmd := goSpace(ctx, id); cmd != nil {
		return cmd
	}
	tag := tagInviteOpenPrefix + id
	return v.setBusy(ctx, tag, cat(ctx).Chat.InviteLoading(), ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (inviteSpace, error) {
		d, err := cl.GetSpace(c, id)
		return inviteSpace{space: d}, err
	}))
}

func (v *inviteView) acceptDirect(ctx *ui.Context) tea.Cmd {
	if !v.ready() {
		return nil
	}
	id := v.inviteID
	tag := tagDirectAcceptPrefix + id
	return v.setBusy(ctx, tag, cat(ctx).Chat.InviteAccepting(), ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (inviteSpace, error) {
		d, err := cl.AcceptDirectInvite(c, id)
		return inviteSpace{space: d}, err
	}))
}

func (v *inviteView) declineDirect(ctx *ui.Context) tea.Cmd {
	if !v.ready() {
		return nil
	}
	id := v.inviteID
	tag := tagDirectDeclinePrefix + id
	return v.setBusy(ctx, tag, cat(ctx).Chat.InviteDeclining(), ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (inviteDeclined, error) {
		return inviteDeclined{}, cl.DeclineDirectInvite(c, id)
	}))
}

// dropDirectInvite removes a direct invite from the pending list.
func dropDirectInvite(ctx *ui.Context, id string) {
	if ctx == nil || ctx.Store == nil {
		return
	}
	ctx.Store.DirectInvites = slices.DeleteFunc(slices.Clone(ctx.Store.DirectInvites), func(in api.DirectInvite) bool { return in.ID == id })
}

// spacePreview is the space of the shown invite (zero when none).
func (v *inviteView) spacePreview() (api.SpacePreview, bool) {
	switch {
	case v.mode == inviteLink && v.preview != nil:
		return v.preview.Space, true
	case v.mode == inviteDirectOne && v.invite != nil:
		return v.invite.Space, true
	}
	return api.SpacePreview{}, false
}

// menu is the m menu: the primary action(s) and 방 신고.
func (v *inviteView) menu(ctx *ui.Context) tea.Cmd {
	sp, ok := v.spacePreview()
	if !ok || (v.mode == inviteLink && v.err != nil) {
		return nil
	}
	c := cat(ctx)
	var items []menuItem
	act := func(f func(*ui.Context) tea.Cmd) func(*ui.Context) (tea.Cmd, ui.Overlay) {
		return func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return f(ctx), nil }
	}
	switch v.mode {
	case inviteLink:
		label := c.Chat.ItemJoin()
		if v.preview.AlreadyMember {
			label = c.Chat.ItemOpenSpace()
		}
		items = append(items, menuItem{Label: label, Disabled: !v.ready(), Run: act(v.primary)})
	case inviteDirectOne:
		items = append(items,
			menuItem{Label: c.Chat.ItemAccept(), Disabled: !v.ready(), Run: act(v.acceptDirect)},
			menuItem{Label: c.Chat.ItemDecline(), Disabled: !v.ready(), Run: act(v.declineDirect)},
		)
	}
	name := strings.TrimSpace(textwidth.Sanitize(sp.Name))
	items = append(items, menuItem{Label: c.Chat.ItemReportSpace(), Danger: true, Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, widgets.ReportDialog("space", sp.ID, name)
	}})
	return ui.OpenOverlay(newMenu(name, items))
}

func (v *inviteView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[inviteFetched]:
		if v.mode != inviteLink || m.Tag != tagInvitePrefix+v.code || m.Value.gen != v.fetchGen {
			return nil
		}
		v.loading = false
		if v.busy == "" {
			v.spinner.Stop()
		}
		v.preview, v.err = m.Value.preview, m.Err
		if m.Err == nil && m.Value.preview == nil {
			v.err = &api.Error{Code: "not_found"}
		}
		return nil
	case ui.ResultMsg[inviteSpace]:
		if m.Tag == "" || m.Tag != v.busy {
			return nil
		}
		v.idle()
		return v.landSpace(ctx, m)
	case ui.ResultMsg[inviteDeclined]:
		if m.Tag == "" || m.Tag != v.busy {
			return nil
		}
		v.idle()
		if m.Err != nil {
			if finalInviteError(m.Err) {
				v.final = m.Err
				dropDirectInvite(ctx, v.inviteID)
				return nil
			}
			return errorCmd(m.Err, cat(ctx).Chat.InviteDeclineFailed())
		}
		dropDirectInvite(ctx, v.inviteID)
		// Leave the invite: back pops it off its stack (to 홈, or the received list it
		// was opened from); Navigate(RouteHome) would only switch to the home tab and
		// leave this screen on top of it.
		return tea.Batch(ui.Toast(cat(ctx).Chat.InviteDeclined(), ui.ToastInfo), ui.Back())
	}
	return v.spinner.Update(ctx, msg)
}

// landSpace finishes an accept (or the already-member fetch): enter the space on
// success; 409 already_member opens it; a settling error shows in place; others toast.
func (v *inviteView) landSpace(ctx *ui.Context, m ui.ResultMsg[inviteSpace]) tea.Cmd {
	open := strings.HasPrefix(m.Tag, tagInviteOpenPrefix)
	direct := strings.HasPrefix(m.Tag, tagDirectAcceptPrefix)
	if m.Err != nil {
		switch {
		case open:
			return errorCmd(m.Err, cat(ctx).Chat.SpaceOpenFailed())
		case api.IsCode(m.Err, "already_member"):
			if direct {
				dropDirectInvite(ctx, v.inviteID)
			}
			if sp, ok := v.spacePreview(); ok {
				return v.openSpace(ctx, sp.ID)
			}
			return nil
		case finalInviteError(m.Err):
			if direct {
				v.final = m.Err
				dropDirectInvite(ctx, v.inviteID)
			} else {
				v.err = m.Err
			}
			return nil
		}
		return errorCmd(m.Err, cat(ctx).Chat.InviteAcceptFailed())
	}
	if direct {
		dropDirectInvite(ctx, v.inviteID)
	} else if v.preview != nil {
		p := *v.preview
		p.AlreadyMember = true // coming back to this screen opens the space
		v.preview = &p
	}
	if m.Value.space == nil {
		return nil
	}
	return enterSpace(ctx, m.Value.space)
}

// ---- input ----

func (v *inviteView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	v.focused = true
	v.sync(ctx)
	if v.mode == inviteDirectList {
		return v.listKey(ctx, msg)
	}
	switch {
	case ctx.Keys.Matches(msg, keymap.ListOpen):
		if v.mode == inviteLink && v.err != nil {
			return false, nil
		}
		return true, v.primary(ctx)
	case ctx.Keys.Matches(msg, keymap.MsgActions):
		cmd := v.menu(ctx)
		return cmd != nil, cmd
	}
	switch msg.String() {
	case "d":
		if v.mode == inviteDirectOne {
			return true, v.declineDirect(ctx)
		}
	case "r":
		if v.mode == inviteLink && v.err != nil && !finalInviteError(v.err) && !v.loading {
			return true, v.fetch(ctx)
		}
	}
	return false, nil
}

func (v *inviteView) listKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if len(v.items) == 0 {
		return false, nil
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		in, ok := v.list.Selected()
		if !ok {
			return false, nil
		}
		return true, openDirectInvite(in.ID)
	}
	return v.list.HandleKey(ctx, msg)
}

func openDirectInvite(id string) tea.Cmd {
	return ui.Navigate(ui.Route{Kind: ui.RouteInvite, Arg: directArg, ID: id})
}

// HandleMouse (list only): the wheel scrolls; a left click selects a row, a
// click on the selected row opens it (CA 5.5).
func (v *inviteView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.mode != inviteDirectList || len(v.items) == 0 {
		return nil
	}
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return v.list.HandleMouse(ctx, msg, local)
	}
	if click.Button != tea.MouseLeft || local.Y < 0 || local.Y >= v.size.H {
		return nil
	}
	top := slices.IndexFunc(v.items, func(in api.DirectInvite) bool { return in.ID == v.list.Anchor().ItemID })
	i := max(top, 0) + local.Y
	if i >= len(v.items) {
		return nil
	}
	in := v.items[i]
	if cur, ok := v.list.Selected(); ok && cur.ID == in.ID {
		return openDirectInvite(in.ID)
	}
	v.list.Select(in.ID)
	return nil
}

// ---- view ----

func (v *inviteView) Title(ctx *ui.Context) (string, ui.Badge) {
	if v.mode == inviteDirectList {
		n := 0
		if ctx != nil && ctx.Store != nil {
			n = len(ctx.Store.DirectInvites)
		}
		return cat(ctx).Chat.CmdInvitesInbox(), ui.Badge{Count: n}
	}
	return cat(ctx).Chat.InviteTitle(), ui.Badge{}
}

func (v *inviteView) Help(ctx *ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	c := cat(ctx)
	switch v.mode {
	case inviteDirectList:
		return ctx.Keys.Help(keymap.ListOpen, keymap.ListDown, keymap.ListUp)
	case inviteDirectOne:
		return []key.Binding{b("enter", c.Chat.ItemAccept()), b("d", c.Chat.ItemDecline()), b("m", c.Chat.HelpMore())}
	}
	if v.err != nil {
		if finalInviteError(v.err) {
			return nil
		}
		return []key.Binding{b("r", c.Chat.HelpRetry())}
	}
	open := c.Chat.ItemJoin()
	if v.preview != nil && v.preview.AlreadyMember {
		open = c.Chat.ItemOpenSpace()
	}
	return []key.Binding{b("enter", open), b("m", c.Chat.HelpMore())}
}

func (v *inviteView) View(ctx *ui.Context, focused bool) string {
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	v.focused = focused
	v.sync(ctx)
	t := themeOf(ctx)
	c := cat(ctx)
	switch v.mode {
	case inviteDirectList:
		if len(v.items) == 0 {
			return widgets.Empty(t, v.size, "📭", c.Chat.InviteNone(), c.Chat.InviteNoneHint())
		}
		lines := strings.Split(v.list.View(ctx, v.size), "\n")
		return strings.Join(textwidth.Fit(lines, w, h), "\n")
	case inviteDirectOne:
		switch {
		case v.final != nil:
			return widgets.Empty(t, v.size, "⚠", directErrorText(tOf(ctx), v.final), c.Chat.InviteCloseHint())
		case v.gone || v.invite == nil:
			return widgets.Empty(t, v.size, "📭", c.Chat.InviteGone(), c.Chat.InviteCloseHint())
		}
		in := v.invite
		return v.card(ctx, t, in.Space, in.Inviter, "", c.Chat.InviteDirectActions())
	}
	switch {
	case v.err != nil:
		hint := c.Chat.InviteRetryHint()
		if finalInviteError(v.err) {
			hint = c.Chat.InviteCloseHint()
		}
		return widgets.Empty(t, v.size, "⚠", inviteErrorText(tOf(ctx), v.err), hint)
	case v.preview == nil:
		return v.centerRows(t, []cardRow{{text: c.Chat.InviteLoading(), style: t.S.Dim.Render, spin: true}})
	}
	p := v.preview
	until := c.Chat.InviteNoExpiry()
	if p.ExpiresAt != nil {
		now := *p.ExpiresAt
		if ctx != nil && ctx.Now != nil {
			now = ctx.Now()
		}
		until = c.Chat.InviteUntil(hz(ctx).MessageTime(now, *p.ExpiresAt, locOf(ctx)))
	}
	actions := c.Chat.InviteLinkActions()
	if p.AlreadyMember {
		actions = c.Chat.InviteMemberActions()
	}
	return v.card(ctx, t, p.Space, p.CreatedBy, until, actions)
}

// directErrorText is the in-place text of a settled direct-invite failure.
func directErrorText(t *i18n.T, err error) string {
	if api.IsCode(err, "banned_from_space") {
		return inviteErrorText(t, err)
	}
	return orKo(t).Chat.InviteGone()
}

// cardRow is one centered card line; rows with a higher drop rank go first when
// the height runs short (0 = never dropped).
type cardRow struct {
	text  string // sanitized
	style func(...string) string
	drop  int
	spin  bool // prefixed with the spinner
}

// card is the centered invite card: icon + name, description, members, inviter,
// [expiry], a blank, the actions line (or the busy status).
func (v *inviteView) card(ctx *ui.Context, t *theme.Theme, sp api.SpacePreview, inviter api.UserSummary, until, actions string) string {
	name := strings.TrimSpace(textwidth.Sanitize(sp.Name))
	if sp.IconEmoji != nil {
		if g := firstGrapheme(*sp.IconEmoji); g != "" {
			name = g + " " + name
		}
	}
	rows := []cardRow{{text: name, style: t.S.Title.Render}}
	if desc := strings.TrimSpace(textwidth.Sanitize(sp.Description)); desc != "" {
		rows = append(rows, cardRow{text: desc, style: t.S.Base.Render, drop: 3})
	}
	rows = append(rows,
		cardRow{text: cat(ctx).Chat.MemberCount(liveMemberCount(ctx, sp)), style: t.S.Dim.Render, drop: 4},
		cardRow{text: cat(ctx).Chat.InviteInviter(strings.TrimSpace(textwidth.Sanitize(displayName(inviter)))), style: t.S.Base.Render, drop: 1},
	)
	if until != "" {
		rows = append(rows, cardRow{text: until, style: t.S.Dim.Render, drop: 2})
	}
	rows = append(rows, cardRow{drop: 5})
	if v.busy != "" {
		rows = append(rows, cardRow{text: v.busyText, style: t.S.Dim.Render, spin: true})
	} else {
		rows = append(rows, cardRow{text: actions, style: t.S.Bold.Render})
	}
	return v.centerRows(t, rows)
}

// liveMemberCount is the member count of sp: the Store's (kept by the
// space.member_* events) once I am in the space, else the preview's snapshot
// (C3: the card kept the preview count after joining).
func liveMemberCount(ctx *ui.Context, sp api.SpacePreview) int {
	if ctx != nil && ctx.Store != nil {
		if sd := ctx.Store.Spaces[sp.ID]; sd != nil {
			return sd.MemberCount
		}
	}
	return sp.MemberCount
}

// centerRows draws rows centered both ways in exactly size.H x size.W, each line
// at most inviteCardMaxW cells (truncated with "…"), dropping rows by rank first.
func (v *inviteView) centerRows(t *theme.Theme, rows []cardRow) string {
	w, h := v.size.W, v.size.H
	for len(rows) > h {
		at, rank := -1, 0
		for i, r := range rows {
			if r.drop > rank {
				at, rank = i, r.drop
			}
		}
		if at < 0 {
			rows = rows[:h]
			break
		}
		rows = slices.Delete(rows, at, at+1)
	}
	cw := min(inviteCardMaxW, w)
	if w > 2 {
		cw = min(cw, w-2)
	}
	top := (h - len(rows)) / 2
	out := make([]string, h)
	for i := range out {
		j := i - top
		if j < 0 || j >= len(rows) || rows[j].text == "" {
			out[i] = blank(t, w)
			continue
		}
		r := rows[j]
		lead, leadW := "", 0
		if r.spin && cw > 2 {
			lead, leadW = v.spinner.View(t)+t.S.Base.Render(" "), 2
		}
		s := textwidth.Truncate(r.text, max(cw-leadW, 0), "…")
		gap := max(w-leadW-textwidth.Width(s), 0)
		left := gap / 2
		out[i] = blank(t, left) + lead + r.style(s) + blank(t, gap-left)
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// renderRow draws a direct invite row: the focus cursor "›", then "<icon> 방 ·
// 초대한 사람 · 상대 시간" (the details S.Dim, going first when room runs short).
func (v *inviteView) renderRow(ctx *ui.Context, in api.DirectInvite, selected bool, width int) []string {
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
	name := strings.TrimSpace(textwidth.Sanitize(in.Space.Name))
	if in.Space.IconEmoji != nil {
		if g := firstGrapheme(*in.Space.IconEmoji); g != "" {
			name = g + " " + name
		}
	}
	detail := strings.TrimSpace(textwidth.Sanitize(displayName(in.Inviter)))
	if ctx != nil && ctx.Now != nil {
		detail += " · " + hz(ctx).Relative(ctx.Now().In(locOf(ctx)), in.CreatedAt)
	}
	name = textwidth.Truncate(name, cw, "…")
	rest := cw - textwidth.Width(name)
	d := ""
	if rest >= 5 {
		d = textwidth.Truncate(" · "+detail, rest, "…")
	}
	gap := rest - textwidth.Width(d)
	return []string{cur + t.S.Bold.Render(name) + t.S.Dim.Render(d) + blank(t, gap)}
}
