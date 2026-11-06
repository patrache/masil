package chat

import (
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// Invite result overlay texts.
const (
	inviteCreatedTitle = "초대 링크"
	inviteJoinPrefix   = "masil join "
	inviteNoExpiry     = "만료 없음"
	inviteExpiryPrefix = "만료 "
	inviteUnlimited    = "횟수 무제한"
	inviteUsesPrefix   = "최대 "
	inviteUsesSuffix   = "번"
	inviteCopied       = "복사했어요"
	inviteHint         = "y 복사 · esc 닫기"
	inviteMaxW         = 44
)

func init() {
	ui.RegisterOverlay("chat.invite_created", func(_ *ui.Context, args string) ui.Overlay {
		return &inviteCreated{inv: api.Invite{Code: args}} // a code only: no expiry/uses line
	})
}

// inviteCreated shows a new invite code: the code, the share line "masil join
// <code>", its expiry and uses (when known), y copies the share line, esc closes.
type inviteCreated struct {
	inv  api.Invite
	meta bool // inv carries expiry/uses (a CreateInvite result)
	size ui.Size
}

var (
	_ ui.Overlay    = (*inviteCreated)(nil)
	_ ui.KeyHandler = (*inviteCreated)(nil)
	_ ui.Titled     = (*inviteCreated)(nil)
	_ ui.Helper     = (*inviteCreated)(nil)
)

// newInviteCreated is the overlay of a created invite.
func newInviteCreated(inv api.Invite) ui.Overlay {
	return &inviteCreated{inv: inv, meta: true}
}

// share is the string people paste to join (api.md 5.8: "masil join <code>").
func (o *inviteCreated) share() string { return inviteJoinPrefix + o.inv.Code }

// clipboard is share() as it goes to tea.SetClipboard (clipboardLine).
func (o *inviteCreated) clipboard() string { return clipboardLine(o.share()) }

func (o *inviteCreated) Init(*ui.Context) tea.Cmd             { return nil }
func (o *inviteCreated) Update(*ui.Context, tea.Msg) tea.Cmd  { return nil }
func (o *inviteCreated) Resize(_ *ui.Context, size ui.Size)   { o.size = size }
func (o *inviteCreated) Modal() bool                          { return true }
func (o *inviteCreated) Title(*ui.Context) (string, ui.Badge) { return inviteCreatedTitle, ui.Badge{} }
func (o *inviteCreated) Bounds(body layout.Rect) layout.Rect {
	return centered(body, min(body.W, inviteMaxW), o.fullRows()+2)
}
func (o *inviteCreated) Help(*ui.Context) []key.Binding { return inviteHelp }
func (o *inviteCreated) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case ctx.Keys.Matches(msg, keymap.NavBack):
		return false, nil // the app closes the overlay
	case ctx.Keys.Matches(msg, keymap.MsgCopy):
		return true, tea.Batch(tea.SetClipboard(o.clipboard()), ui.Toast(inviteCopied, ui.ToastSuccess))
	}
	return false, nil
}

var inviteHelp = []key.Binding{
	key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "복사")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
}

// fullRows is the row count with room to spare: code, blank, share, [meta,]
// blank, hint.
func (o *inviteCreated) fullRows() int {
	if o.meta {
		return 6
	}
	return 5
}

// metaLine is "만료 11월 12일 11:05 · 최대 10번" (만료 없음 / 횟수 무제한).
func (o *inviteCreated) metaLine(ctx *ui.Context) string {
	exp := inviteNoExpiry
	if o.inv.ExpiresAt != nil {
		now := o.inv.CreatedAt
		if ctx != nil && ctx.Now != nil {
			now = ctx.Now()
		}
		exp = inviteExpiryPrefix + humanize.MessageTime(now, *o.inv.ExpiresAt, locOf(ctx))
	}
	uses := inviteUnlimited
	if o.inv.MaxUses != nil {
		uses = inviteUsesPrefix + strconv.Itoa(*o.inv.MaxUses) + inviteUsesSuffix
	}
	return exp + " · " + uses
}

// View: the rows that fit, dropping the blank lines first, then the hint, then
// the meta line; the code and the share line always stay (the share line wraps
// at its space rather than being cut).
func (o *inviteCreated) View(ctx *ui.Context, _ bool) string {
	if o.size.W <= 0 || o.size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	cw := max(1, o.size.W-2*overlayPad(o.size.W))
	code := textwidth.Sanitize(o.inv.Code)
	var share []string
	for _, l := range textwidth.Wrap(inviteJoinPrefix+code, cw) {
		share = append(share, cell(t.S.Bold.Render, l, cw))
	}
	var meta []string
	if o.meta {
		for _, l := range textwidth.Wrap(o.metaLine(ctx), cw) {
			meta = append(meta, cell(t.S.Dim.Render, l, cw))
		}
	}
	head := cell(t.S.Title.Render, code, cw)
	hint := cell(t.S.Dim.Render, inviteHint, cw)
	build := func(blanks, withHint, withMeta bool) []string {
		rows := []string{head}
		if blanks {
			rows = append(rows, "")
		}
		rows = append(rows, share...)
		if withMeta {
			rows = append(rows, meta...)
		}
		if withHint {
			if blanks {
				rows = append(rows, "")
			}
			rows = append(rows, hint)
		}
		return rows
	}
	var rows []string
	for _, v := range [][3]bool{{true, true, true}, {false, true, true}, {false, false, true}, {false, false, false}} {
		rows = build(v[0], v[1], v[2] && o.meta)
		if len(rows) <= o.size.H {
			break
		}
	}
	return renderOverlayRows(t, rows, o.size)
}
