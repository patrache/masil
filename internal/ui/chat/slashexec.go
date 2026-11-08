package chat

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Slash command texts.
const (
	threadPollText       = "스레드에서는 투표를 만들 수 없어요"
	inviteSpaceOnlyText  = "방 채널에서만 초대할 수 있어요"
	inviteFailedText     = "초대하지 못했어요"
	inviteLinkFailedText = "초대 링크를 만들지 못했어요"
	invitedSuffix        = "에 초대했어요"
)

// Slash result tags (ui.Do "chat.<action>:<space>").
const (
	tagInviteCreate = "chat.invite_create:"
	tagInviteDirect = "chat.invite_direct:"
)

// rawArgs is the original text after the command name: everything after the
// first whitespace of the trimmed input, trimmed ("/status 🎧 듣는 중" → "🎧 듣는 중").
func rawArgs(input string) string {
	s := strings.TrimSpace(input)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(s[i:])
}

// runSlash runs a parsed slash command typed into cc (api.md 5.10, CA 4.9).
// /me /shrug /roll /poll go through the optimistic send path; /status and /focus
// hand their raw arguments to the palette commands sns-fun registers; /invite
// asks the server for this channel's space. The input is cleared only when the
// command went ahead: a refused one (a poll in a thread, an invite outside a
// space) keeps the draft and toasts the reason.
func runSlash(ctx *ui.Context, cc *chatComposer, raw string, cmd *slash.Command) tea.Cmd {
	if cmd == nil {
		return nil
	}
	switch cmd.Name {
	case slash.Me:
		return cc.send(ctx, "me", cmd.Text, nil)
	case slash.Shrug:
		return cc.send(ctx, "text", cmd.Text, nil) // slash.Parse already appended the face
	case slash.Roll:
		return cc.send(ctx, "roll", cmd.Dice, nil)
	case slash.Poll:
		if cc.rootID != nil {
			return ui.Toast(threadPollText, ui.ToastError)
		}
		if cmd.Poll == nil {
			return nil
		}
		// the server ignores a poll's body; the question keeps the pending row readable
		return cc.send(ctx, "poll", cmd.Poll.Question, cmd.Poll)
	case slash.Status:
		cc.c.Reset()
		return ui.RunCommand("status.set", rawArgs(raw))
	case slash.Focus:
		cc.c.Reset()
		if cmd.FocusOff {
			return ui.RunCommand("focus.stop", "")
		}
		return ui.RunCommand("focus.start", rawArgs(raw))
	case slash.Invite:
		return cc.invite(ctx, cmd)
	}
	return nil
}

// invite runs /invite @h (a direct invite) or /invite link [7d] [10] (an invite
// code) for the space of cc's channel. The request is remembered by tag so only
// this composer lands its result.
func (cc *chatComposer) invite(ctx *ui.Context, cmd *slash.Command) tea.Cmd {
	ch := cc.channel(ctx)
	if ch == nil || ch.SpaceID == nil || *ch.SpaceID == "" {
		return ui.Toast(inviteSpaceOnlyText, ui.ToastError)
	}
	space := *ch.SpaceID
	if cc.awaiting == nil {
		cc.awaiting = map[string]int{}
	}
	cc.c.Reset()
	if cmd.InviteLink {
		var r api.CreateInviteRequest
		if cmd.LinkMaxUses > 0 {
			r.MaxUses = api.Some(cmd.LinkMaxUses)
		}
		if cmd.LinkExpires > 0 {
			r.ExpiresInSec = api.Some(int(cmd.LinkExpires / time.Second))
		}
		cc.awaiting[tagInviteCreate+space]++
		return createInviteCmd(ctx, space, r)
	}
	cc.awaiting[tagInviteDirect+space]++
	return directInviteCmd(ctx, space, api.Handle(cmd.InviteUser))
}

// createInviteCmd creates an invite code of spaceID (tag chat.invite_create:<space>).
func createInviteCmd(ctx *ui.Context, spaceID string, r api.CreateInviteRequest) tea.Cmd {
	return ui.Do(ctx, tagInviteCreate+spaceID, func(c context.Context, cl *api.Client) (*api.Invite, error) {
		return cl.CreateInvite(c, spaceID, r)
	})
}

// directInviteCmd invites u to spaceID (tag chat.invite_direct:<space>).
func directInviteCmd(ctx *ui.Context, spaceID string, u api.UserRef) tea.Cmd {
	return ui.Do(ctx, tagInviteDirect+spaceID, func(c context.Context, cl *api.Client) (*api.DirectInvite, error) {
		return cl.SendDirectInvite(c, spaceID, u)
	})
}

// claim reports whether cc is waiting for a result tagged tag and, if so, stops
// waiting for one of them. Results are broadcast to every conversation: only
// the one that asked lands it.
func (cc *chatComposer) claim(tag string) bool {
	if cc.awaiting[tag] <= 0 {
		return false
	}
	cc.awaiting[tag]--
	if cc.awaiting[tag] == 0 {
		delete(cc.awaiting, tag)
	}
	return true
}

// onInviteCreated lands an invite code this composer asked for: the invite
// overlay opens, a failure becomes an error toast.
func (cc *chatComposer) onInviteCreated(m ui.ResultMsg[*api.Invite]) tea.Cmd {
	if !strings.HasPrefix(m.Tag, tagInviteCreate) || !cc.claim(m.Tag) {
		return nil
	}
	if m.Err != nil || m.Value == nil {
		err := m.Err
		return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: inviteLinkFailedText} }
	}
	o := newInviteCreated(*m.Value)
	return func() tea.Msg { return ui.OpenOverlayMsg{Overlay: o} }
}

// onDirectInvite lands a direct invite this composer sent.
func (cc *chatComposer) onDirectInvite(ctx *ui.Context, m ui.ResultMsg[*api.DirectInvite]) tea.Cmd {
	if !strings.HasPrefix(m.Tag, tagInviteDirect) || !cc.claim(m.Tag) {
		return nil
	}
	return directInviteDone(ctx, m)
}

// directInviteDone is the end of a direct invite (directInviteCmd): "<이름>님을
// <방>에 초대했어요", or an error toast.
func directInviteDone(ctx *ui.Context, m ui.ResultMsg[*api.DirectInvite]) tea.Cmd {
	if m.Err != nil || m.Value == nil {
		err := m.Err
		return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: inviteFailedText} }
	}
	inv := m.Value
	who := inv.Invitee.DisplayName
	if textwidth.Blank(who) { // nothing visible: name the handle (pending-fixes 1)
		who = string(api.Handle(inv.Invitee.Handle))
	}
	space := inv.Space.Name
	if ctx != nil && ctx.Store != nil {
		if sd := ctx.Store.Spaces[strings.TrimPrefix(m.Tag, tagInviteDirect)]; space == "" && sd != nil {
			space = sd.Name
		}
	}
	text := textwidth.Sanitize(who) + "님을 " + textwidth.Sanitize(space) + invitedSuffix
	return ui.Toast(text, ui.ToastSuccess)
}
