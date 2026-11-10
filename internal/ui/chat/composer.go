package chat

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Composer texts.
const (
	placeholderShort = "›"
)

// composerMaxRows is the composer's auto-grow cap per density (Full 6, Line 3,
// Bubble 4, Minimal 1).
func composerMaxRows(d ui.Density) int {
	switch d {
	case ui.DensityLine:
		return 3
	case ui.DensityBubble:
		return 4
	case ui.DensityMinimal:
		return 1
	default:
		return 6
	}
}

// sendOutcome is the result of one send (or resend) of a pending message.
type sendOutcome struct {
	ChannelID, Nonce string
	Msg              *api.Message
}

// chatComposer is the input of a conversation (channel, or one thread when
// rootID is set): optimistic send keyed by client_nonce, editing my last
// message, typing announcements. Its owner places it and owns the sub-focus
// keys that do not belong to the input.
type chatComposer struct {
	c         *widgets.Composer
	channelID string
	rootID    *string

	editing    *api.Message // the message being edited (nil = composing a new one)
	savedDraft string       // the draft put aside while editing
	editBusy   bool         // an edit request is in flight

	placeholder string // set by the owner per density before view
	maxRows     int    // set by the owner per density and room before height/view

	// onEmptyEdit runs when an edit is submitted with an empty body (Task 12:
	// the delete confirmation); nil = ignored.
	onEmptyEdit func(ctx *ui.Context, m api.Message) tea.Cmd

	comp completer // the @mention / :emoji / /command popup (complete.go)

	awaiting map[string]int // slash requests in flight by result tag (slashexec.go)
}

// newChatComposer creates the composer of channelID (rootID nil) or of the thread rootID.
func newChatComposer(channelID string, rootID *string, placeholder string) *chatComposer {
	var root *string
	if rootID != nil {
		r := *rootID
		root = &r
	}
	return &chatComposer{
		c:           widgets.NewComposer(""), // the placeholder is drawn here: it changes with the density
		channelID:   channelID,
		rootID:      root,
		placeholder: placeholder,
		maxRows:     1,
		comp:        completer{rows: completionRows},
	}
}

func (cc *chatComposer) focused() bool  { return cc.c.Focused() }
func (cc *chatComposer) focus() tea.Cmd { return cc.c.Focus() }
func (cc *chatComposer) blur() {
	cc.closeCompletion()
	cc.c.Blur()
}

// startsComposing reports whether msg, pressed while a conversation's composer
// is unfocused, starts typing into it: its text holds a non-ASCII letter (Hangul
// from an IME: syllables, compatibility or conjoining jamo), no ctrl/alt/meta/
// super/hyper modifier is held, and no action of km is bound to it. ASCII keys
// stay shortcuts (j/k/t/i...).
func startsComposing(km *keymap.Keymap, msg tea.KeyPressMsg) bool {
	if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 {
		return false
	}
	letter := false
	for _, r := range msg.Text {
		if r >= utf8.RuneSelf && (unicode.IsLetter(r) || unicode.Is(unicode.Hangul, r)) {
			letter = true
			break
		}
	}
	return letter && !km.Bound(msg)
}

// startTyping focuses the composer and types msg into it (startsComposing):
// the focus commands, then the key exactly as if the composer had the focus.
func (cc *chatComposer) startTyping(ctx *ui.Context, msg tea.KeyPressMsg) tea.Cmd {
	focus := cc.focus()
	_, cmd := cc.handleKey(ctx, msg)
	return tea.Batch(focus, cmd)
}

// draft is the text to keep for later: the put-aside draft while editing.
func (cc *chatComposer) draft() string {
	if cc.editing != nil {
		return cc.savedDraft
	}
	return cc.c.Value()
}

// setDraft loads a saved draft (normalized like any input).
func (cc *chatComposer) setDraft(s string) {
	cc.editing, cc.savedDraft, cc.editBusy = nil, "", false
	if s == "" {
		cc.c.Reset()
	} else {
		cc.c.SetValue(s)
	}
	cc.closeCompletion()
}

// handleKey handles a key while the composer has the focus. Everything is
// consumed except ctrl+c on an empty input (it reaches the app's quit arming) and a
// history key that is no edit key (ctrl+o: the app walks the history).
// An open completion popup takes its keys first; any text change refreshes it.
func (cc *chatComposer) handleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if cc.completing() && cc.popupKey(ctx, msg) {
		return true, nil
	}
	handled, cmd := cc.key(ctx, msg)
	return handled, tea.Batch(cmd, cc.recomplete(ctx))
}

// key is handleKey without the completion popup.
func (cc *chatComposer) key(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	switch {
	case km.Matches(msg, keymap.AppQuit):
		if cc.editing != nil {
			cc.cancelEdit()
			return true, nil
		}
		handled := cc.c.Value() != ""
		return handled, cc.c.Update(ctx, msg)
	case km.Matches(msg, keymap.ComposeLeave):
		if cc.editing != nil {
			cc.cancelEdit()
			return true, nil
		}
		cc.blur()
		return true, nil
	case km.Matches(msg, keymap.ComposeSend):
		return true, cc.submit(ctx)
	case km.Matches(msg, keymap.ComposeEditLast) && cc.c.Value() == "" && cc.editing == nil:
		if m, ok := cc.lastMine(ctx); ok {
			cc.startEdit(m)
		}
		return true, nil
	case (km.Matches(msg, keymap.NavHistoryBack) || km.Matches(msg, keymap.NavHistoryForward)) && !cc.c.Edits(msg):
		return false, nil // ctrl+o walks the history while typing (the app's soft global)
	}
	return true, cc.input(ctx, msg)
}

// update handles the non-key input routed to the composer (pastes).
func (cc *chatComposer) update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.PasteMsg); ok && cc.focused() {
		return tea.Batch(cc.input(ctx, msg), cc.recomplete(ctx))
	}
	return nil
}

// input feeds msg to the Composer and announces typing when the text changed
// and is not empty (realtime throttles).
func (cc *chatComposer) input(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	before := cc.c.Value()
	cmd := cc.c.Update(ctx, msg)
	if v := cc.c.Value(); v != "" && v != before && ctx.RT != nil {
		ctx.RT.Typing(cc.channelID, cc.rootID)
	}
	return cmd
}

// lastMine is my newest text / me message of the list (the channel, or the thread).
func (cc *chatComposer) lastMine(ctx *ui.Context) (api.Message, bool) {
	c := chatOf(ctx)
	if c == nil || ctx.Store == nil {
		return api.Message{}, false
	}
	var items []api.Message
	if cc.rootID != nil {
		_, w := c.Thread(*cc.rootID)
		items = w.Items()
	} else if c.HasWindow(cc.channelID) {
		items = c.Window(cc.channelID).Items()
	}
	for i := len(items) - 1; i >= 0; i-- {
		m := items[i]
		if ctx.Store.IsMe(m.Author.ID) && (m.Kind == "text" || m.Kind == "me") && !m.Deleted {
			return m, true
		}
	}
	return api.Message{}, false
}

// startEdit puts the draft aside and loads m's body (normalized by SetValue).
func (cc *chatComposer) startEdit(m api.Message) {
	if cc.editing == nil {
		cc.savedDraft = cc.c.Value()
	}
	cc.editing = &m
	cc.editBusy = false
	cc.c.SetValue(m.Body)
	cc.closeCompletion()
}

// cancelEdit ends editing and restores the put-aside draft.
func (cc *chatComposer) cancelEdit() {
	draft := cc.savedDraft
	cc.editing, cc.savedDraft, cc.editBusy = nil, "", false
	if draft == "" {
		cc.c.Reset()
	} else {
		cc.c.SetValue(draft)
	}
	cc.closeCompletion()
}

// submit sends the input (compose.send): whitespace only is ignored; while
// editing it saves the edit; a slash command runs (runSlash), a usage error is
// toasted with the draft kept; otherwise it adds a pending send and posts it.
func (cc *chatComposer) submit(ctx *ui.Context) tea.Cmd {
	body := cc.c.Value()
	if cc.editing != nil {
		return cc.submitEdit(ctx, body)
	}
	if strings.TrimSpace(body) == "" {
		return nil
	}
	cmd, ok, err := slash.Parse(body)
	switch {
	case err != nil: // a usage error: the draft stays for a fix
		return ui.Toast(slash.Message(ctx.T, err), ui.ToastError)
	case ok:
		return runSlash(ctx, cc, body, cmd)
	}
	return cc.send(ctx, "text", body, nil)
}

// send adds a pending send of kind with body (and the poll input of a /poll),
// clears the input and posts it.
func (cc *chatComposer) send(ctx *ui.Context, kind, body string, poll *api.PollInput) tea.Cmd {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	p := chatstore.PendingMessage{Nonce: ids.New(), Body: body, Kind: kind, ThreadRootID: cc.rootID, SentAt: ctx.Now()}
	c.AddPending(cc.channelID, p)
	cc.c.Reset()
	return sendCmd(ctx, cc.channelID, cc.rootID, p, poll)
}

// submitEdit saves the edited body (PATCH). An unchanged body just ends the edit.
func (cc *chatComposer) submitEdit(ctx *ui.Context, body string) tea.Cmd {
	m := *cc.editing
	if cc.editBusy {
		return nil
	}
	if strings.TrimSpace(body) == "" {
		if cc.onEmptyEdit != nil {
			return cc.onEmptyEdit(ctx, m)
		}
		return nil
	}
	if body == m.Body {
		cc.cancelEdit()
		return nil
	}
	cc.editBusy = true
	id := m.ID
	return ui.Do(ctx, "chat.edit:"+id, func(c context.Context, cl *api.Client) (*api.Message, error) {
		return cl.EditMessage(c, id, body)
	})
}

// onEditResult lands an edit result: the new copy is stored when it belongs to
// this conversation; the edit this composer started ends on success (draft
// restored) or reports the failure and stays open for another try.
func (cc *chatComposer) onEditResult(ctx *ui.Context, m ui.ResultMsg[*api.Message]) tea.Cmd {
	id := strings.TrimPrefix(m.Tag, "chat.edit:")
	if m.Err == nil && m.Value != nil {
		if c := chatOf(ctx); c != nil && (m.Value.ChannelID == cc.channelID || m.Value.ChannelID == "") {
			msg := *m.Value
			if msg.ChannelID == "" {
				msg.ChannelID = cc.channelID
			}
			c.UpsertMessage(msg)
		}
	}
	if cc.editing == nil || cc.editing.ID != id || !cc.editBusy {
		return nil
	}
	cc.editBusy = false
	if m.Err != nil {
		err, errCtx := m.Err, cat(ctx).Chat.EditFailed()
		return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: errCtx} }
	}
	cc.cancelEdit()
	return nil
}

// sendCmd posts a pending message (a thread reply when rootID is set), tag
// chat.send:<nonce>. A poll input is kept with the pending send for resend.
func sendCmd(ctx *ui.Context, channelID string, rootID *string, p chatstore.PendingMessage, poll *api.PollInput) tea.Cmd {
	if poll != nil {
		if c := chatOf(ctx); c != nil {
			c.SetPendingPoll(channelID, p.Nonce, poll)
		}
	}
	root := ""
	if rootID != nil {
		root = *rootID
	}
	return ui.Do(ctx, "chat.send:"+p.Nonce, func(c context.Context, cl *api.Client) (sendOutcome, error) {
		out := sendOutcome{ChannelID: channelID, Nonce: p.Nonce}
		var err error
		if root != "" {
			out.Msg, _, err = cl.ReplyThread(c, root, api.ThreadReplyRequest{ClientNonce: p.Nonce, Kind: p.Kind, Body: p.Body})
		} else {
			out.Msg, _, err = cl.SendMessage(c, channelID, api.SendMessageRequest{ClientNonce: p.Nonce, Kind: p.Kind, Body: p.Body, Poll: poll})
		}
		return out, err
	})
}

// resendCmd resends a failed pending message with the same nonce, body, kind,
// thread and poll input. A poll whose input is gone is not sent (it stays failed).
func resendCmd(ctx *ui.Context, channelID, nonce string) tea.Cmd {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	for _, p := range c.Pending(channelID) {
		if p.Nonce != nonce {
			continue
		}
		poll := c.PendingPoll(channelID, nonce)
		if p.Kind == "poll" && poll == nil {
			return nil
		}
		c.RetryPending(channelID, nonce, ctx.Now())
		return sendCmd(ctx, channelID, p.ThreadRootID, p, poll)
	}
	return nil
}

// applySend lands a send result: failure marks the pending send failed (it
// stays listed for enter = resend / d = discard; the client already retried a
// short 429 once); success stores the message, which drops the pending send
// — unless the message is past the shown window, then the pending row stays
// until the page being fetched shows it (chatstore.UpsertMessage).
func applySend(ctx *ui.Context, m ui.ResultMsg[sendOutcome]) {
	c := chatOf(ctx)
	if c == nil {
		return
	}
	o := m.Value
	if m.Err != nil {
		c.FailPending(o.ChannelID, o.Nonce, m.Err)
		return
	}
	if o.Msg == nil {
		c.DropPending(o.ChannelID, o.Nonce)
		return
	}
	msg := *o.Msg
	if msg.ChannelID == "" {
		msg.ChannelID = o.ChannelID
	}
	if msg.ClientNonce == nil || *msg.ClientNonce != o.Nonce { // the echo carried no (or another) client_nonce
		c.DropPending(o.ChannelID, o.Nonce)
	}
	c.UpsertMessage(msg)
}

// height is the drawn height for width (edit label + composer box).
func (cc *chatComposer) height(width int) int {
	if width <= 0 {
		return 0
	}
	h := cc.c.Height(width, cc.maxRows)
	if cc.editing != nil {
		h++
	}
	return h
}

// boxHeight is height without the completion popup (edit label + box).
func (cc *chatComposer) boxHeight(width int) int {
	return max(0, cc.height(width)-len(cc.comp.shown))
}

// view draws the edit label (while editing) above the composer box, every line
// exactly width cells.
func (cc *chatComposer) view(ctx *ui.Context, width int) string {
	if width <= 0 {
		return ""
	}
	t := themeOf(ctx)
	box := cc.c.View(ctx, width, cc.maxRows)
	if cc.c.Value() == "" && cc.c.Height(width, cc.maxRows) == 3 { // no popup: draw the density's placeholder
		style := t.S.Input
		if cc.focused() {
			style = t.S.InputFocused
		}
		ph := textwidth.Truncate(textwidth.Sanitize(cc.placeholder), max(1, width-2), "…")
		box = widgets.Box(t, style, ui.Size{W: width, H: 3}, t.S.Placeholder.Render(ph))
	}
	if cc.editing == nil {
		return box
	}
	return cell(t.S.Warning.Render, cat(ctx).Chat.EditLabel(), width) + "\n" + box
}

// cursor is the real cursor relative to view's origin (nil while blurred).
func (cc *chatComposer) cursor() *tea.Cursor {
	c := cc.c.Cursor()
	if c == nil {
		return nil
	}
	if cc.editing != nil {
		c.Y++
	}
	return c
}
