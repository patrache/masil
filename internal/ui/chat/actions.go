package chat

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterOverlay("chat.message_actions", newMessageActionsOverlay)
}

// Message action texts.
const (
	actionsTitle    = "메시지"
	reactFailed     = "리액션을 바꾸지 못했어요"
	deleteFailed    = "메시지를 삭제하지 못했어요"
	pinFailed       = "고정을 바꾸지 못했어요"
	voteFailed      = "투표하지 못했어요"
	blockFailed     = "차단하지 못했어요"
	msgCopied       = "복사했어요"
	pinnedText      = "메시지를 고정했어요"
	unpinnedText    = "고정을 해제했어요"
	notMineEdit     = "내 메시지만 수정할 수 있어요"
	cantEditText    = "이 메시지는 수정할 수 없어요"
	noDeletePerm    = "삭제할 권한이 없어요"
	noPinPerm       = "고정할 권한이 없어요"
	replyNoPin      = "답글은 고정할 수 없어요"
	pollClosedText  = "마감된 투표예요"
	deleteTitle     = "메시지 삭제"
	deleteBody      = "이 메시지를 삭제할까요?"
	deleteYes       = "삭제"
	blockBody       = "차단하면 그 사람의 메시지가 접히고 DM을 주고받을 수 없어요"
	blockYes        = "차단"
	cancelText      = "취소"
	blockedSuffix   = "님을 차단했어요"
	blockAskSuffix  = "님을 차단할까요?"
	reportSuffix    = "의 메시지"
	itemThread      = "스레드로 답글"
	itemReact       = "리액션"
	itemReactUsers  = "리액션 누른 사람"
	itemVotePrefix  = "투표: "
	itemRetract     = "투표 취소"
	itemClosePoll   = "투표 마감"
	itemEdit        = "수정"
	itemPin         = "고정"
	itemUnpin       = "고정 해제"
	itemCopy        = "복사"
	itemDelete      = "삭제"
	itemReport      = "신고"
	itemBlockAuthor = "작성자 차단"
)

// ---- landing action results ----

// msgAction is the result of one message action request. Results are broadcast
// to every live component, so the first conversation that sees one lands it
// (landed guards against a second, e.g. the channel and its thread both open).
type msgAction struct {
	landed *bool
	land   func(ctx *ui.Context, err error) tea.Cmd
}

// doAction runs fn (ui.Do, tag) and lands its value with land on the UI goroutine.
func doAction[T any](ctx *ui.Context, tag string, fn func(context.Context, *api.Client) (T, error), land func(ctx *ui.Context, v T, err error) tea.Cmd) tea.Cmd {
	once := new(bool)
	return ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (msgAction, error) {
		v, err := fn(c, cl)
		return msgAction{landed: once, land: func(ctx *ui.Context, err error) tea.Cmd { return land(ctx, v, err) }}, err
	})
}

// landAction lands an action result once (nil when already landed).
func landAction(ctx *ui.Context, m ui.ResultMsg[msgAction]) tea.Cmd {
	a := m.Value
	if a.landed == nil || *a.landed || a.land == nil {
		return nil
	}
	*a.landed = true
	return a.land(ctx, m.Err)
}

// chatChangedMsg asks the conversations to re-read chatstore after a local
// (optimistic) change made outside their own Update.
type chatChangedMsg struct{}

func chatChanged() tea.Msg { return chatChangedMsg{} }

// editRequestMsg asks the conversation holding the message to edit it in its
// composer (the "수정" item of a menu opened by id).
type editRequestMsg struct{ ChannelID, MessageID string }

func errorCmd(err error, context string) tea.Cmd {
	return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: context} }
}

func openOverlayCmd(o ui.Overlay) tea.Cmd {
	return func() tea.Msg { return ui.OpenOverlayMsg{Overlay: o} }
}

// current is the held copy of m (m itself when none is held).
func current(ctx *ui.Context, m api.Message) api.Message {
	if c := chatOf(ctx); c != nil {
		if cur, ok := c.FindMessage(m.ID); ok {
			return cur
		}
	}
	return m
}

// ---- the actions ----

// toggleReactionCmd toggles my emoji reaction on m: chatstore is changed at once
// (SetMyReaction), then PUT/DELETE is sent; a failure reverts it and toasts.
func toggleReactionCmd(ctx *ui.Context, m api.Message, emoji string) tea.Cmd {
	c := chatOf(ctx)
	if c == nil || emoji == "" {
		return nil
	}
	cur := current(ctx, m)
	on := !slices.ContainsFunc(cur.Reactions, func(r api.ReactionSummary) bool { return r.Emoji == emoji && r.Me })
	c.SetMyReaction(m.ID, emoji, on)
	id := m.ID
	return tea.Batch(chatChanged, doAction(ctx, "chat.react:"+id, func(c context.Context, cl *api.Client) (struct{}, error) {
		if on {
			return struct{}{}, cl.AddReaction(c, id, emoji)
		}
		return struct{}{}, cl.RemoveReaction(c, id, emoji)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err == nil {
			return nil
		}
		if c := chatOf(ctx); c != nil {
			c.SetMyReaction(id, emoji, !on)
		}
		return errorCmd(err, reactFailed)
	}))
}

// deleteMessageCmd deletes m; on success the held copy becomes a tombstone until
// message.deleted lands.
func deleteMessageCmd(ctx *ui.Context, m api.Message) tea.Cmd {
	id := m.ID
	return doAction(ctx, "chat.delete:"+id, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.DeleteMessage(c, id)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, deleteFailed)
		}
		if c := chatOf(ctx); c != nil {
			if cur, ok := c.FindMessage(id); ok && !cur.Deleted {
				cur.Deleted, cur.Body, cur.Mentions, cur.MentionHere = true, "", nil, false
				cur.Reactions, cur.Pinned, cur.Poll, cur.Roll = nil, false, nil, nil
				c.UpsertMessage(cur)
			}
		}
		return nil
	})
}

// pinCmd pins (on) or unpins m; on success the held copy follows at once.
func pinCmd(ctx *ui.Context, m api.Message, on bool) tea.Cmd {
	id := m.ID
	return doAction(ctx, "chat.pin:"+id, func(c context.Context, cl *api.Client) (struct{}, error) {
		if on {
			return struct{}{}, cl.Pin(c, id)
		}
		return struct{}{}, cl.Unpin(c, id)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, pinFailed)
		}
		if c := chatOf(ctx); c != nil {
			if cur, ok := c.FindMessage(id); ok && cur.Pinned != on {
				cur.Pinned = on
				c.UpsertMessage(cur)
			}
		}
		if on {
			return ui.Toast(pinnedText, ui.ToastSuccess)
		}
		return ui.Toast(unpinnedText, ui.ToastInfo)
	})
}

// voteCmd replaces my votes of poll p with idxs (none = retract); the result is
// stored as authoritative (my_votes included).
func voteCmd(ctx *ui.Context, p api.Poll, idxs []int) tea.Cmd {
	pid := p.ID
	idxs = slices.Clone(idxs)
	slices.Sort(idxs)
	return doAction(ctx, "chat.vote:"+pid, func(c context.Context, cl *api.Client) (*api.Poll, error) {
		if len(idxs) == 0 {
			return cl.RetractVote(c, pid)
		}
		return cl.Vote(c, pid, idxs)
	}, landPoll)
}

// closePollCmd closes poll p (its creator only).
func closePollCmd(ctx *ui.Context, p api.Poll) tea.Cmd {
	pid := p.ID
	return doAction(ctx, "chat.poll_close:"+pid, func(c context.Context, cl *api.Client) (*api.Poll, error) {
		return cl.ClosePoll(c, pid)
	}, landPoll)
}

func landPoll(ctx *ui.Context, p *api.Poll, err error) tea.Cmd {
	if err != nil {
		return errorCmd(err, voteFailed)
	}
	if c := chatOf(ctx); c != nil && p != nil {
		c.SetPoll(*p, true)
	}
	return nil
}

// pollToggle is the vote set after choosing option idx: single = [idx], multi =
// my votes with idx toggled.
func pollToggle(p api.Poll, idx int) []int {
	if !p.Multi {
		return []int{idx}
	}
	if slices.Contains(p.MyVotes, idx) {
		return slices.DeleteFunc(slices.Clone(p.MyVotes), func(i int) bool { return i == idx })
	}
	return append(slices.Clone(p.MyVotes), idx)
}

// blockCmd blocks u (PUT /v1/blocks/@handle); on success the block applies at
// once (block.updated follows) and a toast confirms it.
func blockCmd(ctx *ui.Context, u api.UserSummary) tea.Cmd {
	ref := api.UserID(u.ID)
	if u.Handle != "" {
		ref = api.Handle(u.Handle)
	}
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, "chat.block:"+u.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.Block(c, ref)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, blockFailed)
		}
		if s := ctx.Store; s != nil {
			if s.Blocks == nil {
				s.Blocks = map[string]time.Time{}
			}
			if _, ok := s.Blocks[u.ID]; !ok {
				s.Blocks[u.ID] = ctx.Now()
			}
		}
		return ui.Toast(name+blockedSuffix, ui.ToastSuccess)
	})
}

// copyText is what msg.copy puts on the clipboard: the body, a roll's result, a
// poll's question.
func copyText(m api.Message) string {
	switch {
	case m.Kind == "roll" && m.Roll != nil:
		return rollText(*m.Roll)
	case m.Kind == "poll" && m.Poll != nil:
		return m.Poll.Question
	}
	return m.Body
}

func copyCmd(m api.Message) tea.Cmd {
	return tea.Batch(tea.SetClipboard(copyText(m)), ui.Toast(msgCopied, ui.ToastSuccess))
}

// ---- rules ----

// actionable: a live user message (not deleted, not a system line).
func actionable(m api.Message) bool { return !m.Deleted && m.Kind != "system" }

// editable: my live text or /me message.
func editable(ctx *ui.Context, m api.Message) bool {
	return actionable(m) && isMe(ctx, m.Author.ID) && (m.Kind == "text" || m.Kind == "me")
}

// folded: someone I blocked (drawn as one folded line).
func folded(ctx *ui.Context, m api.Message) bool {
	return ctx != nil && ctx.Store != nil && !isMe(ctx, m.Author.ID) && ctx.Store.IsBlocked(m.Author.ID)
}

// threadRootOf is the thread a message belongs to (its own id for a top-level one).
func threadRootOf(m api.Message) string {
	if m.ThreadRootID != nil {
		return *m.ThreadRootID
	}
	return m.ID
}

func reactPicker(ctx *ui.Context, m api.Message) ui.Overlay {
	return widgets.EmojiPicker(func(e string) tea.Cmd { return toggleReactionCmd(ctx, m, e) })
}

func deleteConfirm(ctx *ui.Context, m api.Message) ui.Overlay {
	return widgets.Confirm(deleteTitle, deleteBody, deleteYes, cancelText, deleteMessageCmd(ctx, m))
}

func blockConfirm(ctx *ui.Context, u api.UserSummary) ui.Overlay {
	return widgets.Confirm(textwidth.Sanitize(displayName(u))+blockAskSuffix, blockBody, blockYes, cancelText, blockCmd(ctx, u))
}

// reactionUsersOverlay is who reacted to m: the list of the one emoji, else a
// menu to pick the emoji first.
func reactionUsersOverlay(ctx *ui.Context, m api.Message) ui.Overlay {
	switch len(m.Reactions) {
	case 0:
		return nil
	case 1:
		return newReactionUsersFor(m.ID, m.Reactions[0].Emoji)
	}
	items := make([]menuItem, 0, len(m.Reactions))
	for _, r := range m.Reactions {
		emoji := r.Emoji
		items = append(items, menuItem{Label: textwidth.Sanitize(emoji) + " " + strconv.Itoa(r.Count), Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, newReactionUsersFor(m.ID, emoji)
		}})
	}
	return newMenu(itemReactUsers, items)
}

// ---- keys ----

// actionKey handles the message-scope keys (msg.react / edit / delete / pin /
// copy / actions) on the selected message row; edit starts an edit in the
// owner's composer. A deleted or system message consumes react / edit / pin /
// copy without doing anything.
func (p *msgPane) actionKey(ctx *ui.Context, msg tea.KeyPressMsg, edit func(api.Message) tea.Cmd) (bool, tea.Cmd) {
	r, ok := p.selected()
	if !ok || r.Kind != rowMessage || r.Msg == nil || ctx == nil || ctx.Store == nil {
		return false, nil
	}
	m := current(ctx, *r.Msg)
	ch := ctx.Store.Channels[p.channelID]
	km := ctx.Keys
	switch {
	case km.Matches(msg, keymap.MsgActions):
		return true, openOverlayCmd(newMessageActions(ctx, ch, m))
	case km.Matches(msg, keymap.MsgDelete):
		if m.Deleted || m.Kind == "system" {
			return true, nil
		}
		if !canDelete(ctx, ch, m) {
			return true, ui.Toast(noDeletePerm, ui.ToastInfo)
		}
		return true, openOverlayCmd(deleteConfirm(ctx, m))
	}
	if !actionable(m) {
		switch {
		case km.Matches(msg, keymap.MsgReact), km.Matches(msg, keymap.MsgEdit), km.Matches(msg, keymap.MsgPin), km.Matches(msg, keymap.MsgCopy):
			return true, nil
		}
		return false, nil
	}
	switch {
	case km.Matches(msg, keymap.MsgReact):
		return true, openOverlayCmd(reactPicker(ctx, m))
	case km.Matches(msg, keymap.MsgEdit):
		switch {
		case !isMe(ctx, m.Author.ID):
			return true, ui.Toast(notMineEdit, ui.ToastInfo)
		case !editable(ctx, m):
			return true, ui.Toast(cantEditText, ui.ToastInfo)
		}
		if edit == nil {
			return true, nil
		}
		return true, edit(m)
	case km.Matches(msg, keymap.MsgPin):
		switch {
		case m.ThreadRootID != nil:
			return true, ui.Toast(replyNoPin, ui.ToastInfo)
		case !canPin(ctx, ch):
			return true, ui.Toast(noPinPerm, ui.ToastInfo)
		}
		return true, pinCmd(ctx, m, !m.Pinned)
	case km.Matches(msg, keymap.MsgCopy):
		if folded(ctx, m) {
			return true, nil
		}
		return true, copyCmd(m)
	}
	return false, nil
}

// ---- menu ----

// messageActions is the msg.actions menu of m in ch (CA 6.1): 스레드로 답글 t,
// 리액션 r, 리액션 누른 사람 (when reacted), the poll items (투표: <선택지>, 투표
// 취소, 투표 마감 for its creator), 수정 e (mine), 고정 / 고정 해제 p (disabled
// without permission), 복사 y, 삭제 d (permission), 신고 and 작성자 차단 (others').
func messageActions(ctx *ui.Context, ch *api.Channel, m api.Message) []menuItem {
	var items []menuItem
	if m.Kind != "system" {
		root, chID := threadRootOf(m), m.ChannelID
		if chID == "" && ch != nil {
			chID = ch.ID
		}
		items = append(items, menuItem{Label: itemThread, Hint: "t", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return openThread(chID, root), nil
		}})
	}
	live := actionable(m)
	if live {
		items = append(items, menuItem{Label: itemReact, Hint: "r", Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, reactPicker(ctx, m)
		}})
		if len(m.Reactions) > 0 {
			items = append(items, menuItem{Label: itemReactUsers, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return nil, reactionUsersOverlay(ctx, m)
			}})
		}
	}
	if live && m.Kind == "poll" && m.Poll != nil && !m.Poll.Closed {
		p := *m.Poll
		for _, o := range p.Options {
			idx := o.Idx
			items = append(items, menuItem{Label: itemVotePrefix + o.Text, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return voteCmd(ctx, p, pollToggle(p, idx)), nil
			}})
		}
		if len(p.MyVotes) > 0 {
			items = append(items, menuItem{Label: itemRetract, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return voteCmd(ctx, p, nil), nil
			}})
		}
		if isMe(ctx, p.CreatorID) {
			items = append(items, menuItem{Label: itemClosePoll, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return closePollCmd(ctx, p), nil
			}})
		}
	}
	if editable(ctx, m) {
		chID, id := m.ChannelID, m.ID
		items = append(items, menuItem{Label: itemEdit, Hint: "e", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return func() tea.Msg { return editRequestMsg{ChannelID: chID, MessageID: id} }, nil
		}})
	}
	if live && m.ThreadRootID == nil {
		label := itemPin
		if m.Pinned {
			label = itemUnpin
		}
		items = append(items, menuItem{Label: label, Hint: "p", Disabled: !canPin(ctx, ch), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return pinCmd(ctx, m, !m.Pinned), nil
		}})
	}
	if live && !folded(ctx, m) {
		items = append(items, menuItem{Label: itemCopy, Hint: "y", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return copyCmd(m), nil
		}})
	}
	if !m.Deleted && m.Kind != "system" && canDelete(ctx, ch, m) {
		items = append(items, menuItem{Label: itemDelete, Hint: "d", Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, deleteConfirm(ctx, m)
		}})
	}
	if m.Kind != "system" && !isMe(ctx, m.Author.ID) && m.Author.ID != "" {
		label := textwidth.Sanitize(displayName(m.Author)) + reportSuffix
		items = append(items, menuItem{Label: itemReport, Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, widgets.ReportDialog("message", m.ID, label)
		}})
		if ctx == nil || ctx.Store == nil || !ctx.Store.IsBlocked(m.Author.ID) {
			author := m.Author
			items = append(items, menuItem{Label: itemBlockAuthor, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return nil, blockConfirm(ctx, author)
			}})
		}
	}
	return items
}

// newMessageActions is the msg.actions menu overlay of m.
func newMessageActions(ctx *ui.Context, ch *api.Channel, m api.Message) ui.Overlay {
	return newMenu(actionsTitle, messageActions(ctx, ch, m))
}

// newMessageActionsOverlay is chat.message_actions (args "<channelID> <messageID>");
// nil when the message is not held.
func newMessageActionsOverlay(ctx *ui.Context, args string) ui.Overlay {
	chID, id, ok := strings.Cut(strings.TrimSpace(args), " ")
	c := chatOf(ctx)
	if !ok || c == nil || ctx.Store == nil {
		return nil
	}
	m, found := c.FindMessage(strings.TrimSpace(id))
	if !found {
		return nil
	}
	if m.ChannelID == "" {
		m.ChannelID = chID
	}
	return newMessageActions(ctx, ctx.Store.Channels[chID], m)
}

// ---- mouse ----

// chipAt is the reaction chip of rs (the chips drawn on line, in order) under
// cell x of the plain line: each chip spans from its emoji to the start of the
// next chip (the last one to its own end).
func chipAt(line string, rs []api.ReactionSummary, x int) (emoji string, ok bool) {
	type span struct {
		start, end int
		emoji      string
	}
	var spans []span
	pos := 0
	for _, r := range rs {
		g := textwidth.Sanitize(r.Emoji)
		if g == "" || pos > len(line) {
			continue
		}
		i := strings.Index(line[pos:], g)
		if i < 0 {
			continue
		}
		start := textwidth.Width(line[:pos+i])
		spans = append(spans, span{start: start, end: start + textwidth.Width(g+" "+strconv.Itoa(r.Count)), emoji: r.Emoji})
		pos += i + len(g)
	}
	for i, s := range spans {
		end := s.end
		if i+1 < len(spans) {
			end = spans[i+1].start
		}
		if x >= s.start && x < end {
			return s.emoji, true
		}
	}
	return "", false
}

// chipRows packs rs into lines like widgets.ReactionChips at width.
func chipRows(rs []api.ReactionSummary, width int) [][]api.ReactionSummary {
	if width <= 0 {
		return nil
	}
	gapW := textwidth.Width("  ")
	var out [][]api.ReactionSummary
	curW := 0
	for _, r := range rs {
		tw := textwidth.Width(textwidth.Truncate(textwidth.Sanitize(r.Emoji)+" "+strconv.Itoa(r.Count), width, "…"))
		switch {
		case curW == 0:
			out = append(out, []api.ReactionSummary{r})
			curW = tw
		case curW+gapW+tw <= width:
			out[len(out)-1] = append(out[len(out)-1], r)
			curW += gapW + tw
		default:
			out = append(out, []api.ReactionSummary{r})
			curW = tw
		}
	}
	return out
}

// chipLayout locates the reaction chip lines of message m drawn with st in a
// row of n lines: the first chip line, the chips of each line and the lead text
// drawn before the chips of the first line ("💬3", bubble only).
func chipLayout(ctx *ui.Context, m api.Message, st msgStyle, n int) (first int, rows [][]api.ReactionSummary, lead string) {
	t := themeOf(ctx)
	switch st.Density {
	case ui.DensityFull:
		bw := st.Width - indentOf(st.Width)
		meta := metaLines(t, &m, bw, st.InThread)
		rows = chipRows(m.Reactions, bw)
		return n - len(meta), rows, ""
	case ui.DensityBubble:
		w := max(1, st.Width*bubbleRatio/100)
		meta := bubbleMeta(t, &m, w, st.InThread)
		room := w
		if !st.InThread && m.Thread != nil && m.Thread.ReplyCount > 0 {
			lead = textwidth.Truncate("💬"+humanize.Count(m.Thread.ReplyCount), w, "…")
			room -= textwidth.Width(lead) + 1
		}
		if room > 0 {
			rows = chipRows(m.Reactions, room)
		}
		return n - len(meta), rows, lead
	}
	return 0, nil, ""
}

// pollOptionAt is the poll option drawn on line of message m drawn with st (full
// and bubble densities: the question line, then one line per option).
func pollOptionAt(m api.Message, st msgStyle, line int) (idx int, ok bool) {
	if m.Kind != "poll" || m.Poll == nil || (st.Density != ui.DensityFull && st.Density != ui.DensityBubble) {
		return 0, false
	}
	head := 1
	if st.Continuation {
		head = 0
	}
	i := line - head - 1
	if i < 0 || i >= len(m.Poll.Options) {
		return 0, false
	}
	return m.Poll.Options[i].Idx, true
}

// hitAction runs a click on line (cell x) of message row r: a reaction chip
// (left = toggle mine, right = who reacted) or a poll bar (left = vote).
func (p *msgPane) hitAction(ctx *ui.Context, r row, line, x int, b tea.MouseButton) (tea.Cmd, bool) {
	if r.Kind != rowMessage || r.Msg == nil || ctx == nil || ctx.Store == nil {
		return nil, false
	}
	m := *r.Msg
	if !actionable(m) || m.Kind == "me" || folded(ctx, m) {
		return nil, false
	}
	sel, _ := p.selected()
	w := p.listSize.W
	st := p.style(ctx, r, sel.ID == r.ID, w)
	lines := p.render(ctx, r, sel.ID == r.ID, w)
	if line < 0 || line >= len(lines) {
		return nil, false
	}
	if first, rows, lead := chipLayout(ctx, m, st, len(lines)); line >= first && line < first+len(rows) {
		plain := ansi.Strip(lines[line])
		if line == first && lead != "" {
			if i := strings.Index(plain, lead); i >= 0 {
				plain = plain[:i] + strings.Repeat(" ", textwidth.Width(lead)) + plain[i+len(lead):]
			}
		}
		if e, ok := chipAt(plain, rows[line-first], x); ok {
			if b == tea.MouseRight {
				return openOverlayCmd(newReactionUsersFor(m.ID, e)), true
			}
			return toggleReactionCmd(ctx, current(ctx, m), e), true
		}
		return nil, false
	}
	if b != tea.MouseLeft {
		return nil, false
	}
	if idx, ok := pollOptionAt(m, st, line); ok {
		cur := current(ctx, m)
		if cur.Poll == nil {
			return nil, false
		}
		poll := *cur.Poll
		if poll.Closed {
			return ui.Toast(pollClosedText, ui.ToastInfo), true
		}
		return voteCmd(ctx, poll, pollToggle(poll, idx)), true
	}
	return nil, false
}
