package chat

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteChannel, newChannel)
}

// Conversation view constants.
const (
	historyLimit = 50              // page size of every history request
	readDelay    = time.Second     // the view must stay at the bottom this long before marking read
	afterOverlap = 5 * time.Second // newer-page overlap (events.md 5.3)
	goneTitle    = "삭제되었거나 볼 수 없는 채널이에요"
	goneHint     = "esc 뒤로"
	loadFailed   = "메시지를 불러오지 못했어요"
	retryDesc    = "다시 시도"
)

// readTickMsg fires a read arming (gen = the arming it belongs to).
type readTickMsg struct {
	ch  string
	gen uint64
}

// historyPage is the result of one ListMessages request of the view; T is the
// chatstore ticket taken when the request was built.
type historyPage struct {
	Q api.MessageQuery
	W *api.MessageWindow
	T chatstore.PageTicket
}

// channelSaved is the Persistent state of a conversation.
type channelSaved struct {
	Anchor   widgets.ScrollAnchor
	Selected string
	Draft    string
}

// channelView is RouteChannel: one channel's (text / DM / group DM) conversation.
type channelView struct {
	route    ui.Route
	pane     *msgPane
	composer *chatComposer
	size     ui.Size
	inputY   int // row of the composer (or read-only line) in the last View

	readGen  uint64 // latest read arming
	readSent string // message id of the read request in flight / done

	firstBusy bool   // a first page (latest / around) is in flight
	newerBusy bool   // a newer page is in flight
	jump      string // anchor to select once its page lands

	chat *chatstore.ChatStore // the store this view marked open (SetClosed on release)
	gen  chatstore.PageTicket // window the older / newer flags belong to (syncGen)
}

var (
	_ ui.Component     = (*channelView)(nil)
	_ ui.KeyHandler    = (*channelView)(nil)
	_ ui.MouseHandler  = (*channelView)(nil)
	_ ui.InputCapturer = (*channelView)(nil)
	_ ui.Cursorer      = (*channelView)(nil)
	_ ui.Completer     = (*channelView)(nil)
	_ ui.ChannelViewer = (*channelView)(nil)
	_ ui.Titled        = (*channelView)(nil)
	_ ui.Helper        = (*channelView)(nil)
	_ ui.Persistent    = (*channelView)(nil)
)

// newChannel is the RouteChannel factory. The unread divider pointer is frozen
// here, from the channel's read pointer when the view opens.
func newChannel(ctx *ui.Context, r ui.Route) ui.Component {
	v := &channelView{route: r, pane: newMsgPane(r.ID, ""), composer: newChatComposer(r.ID, nil, placeholderShort)}
	if ch := v.channel(ctx); ch != nil && ch.My.LastReadMessageID != nil {
		v.pane.lastRead = *ch.My.LastReadMessageID
	}
	v.pane.older = v.loadOlder
	v.pane.newer = v.loadNewer
	v.pane.open = v.openThread
	v.composer.onEmptyEdit = func(ctx *ui.Context, m api.Message) tea.Cmd { return openOverlayCmd(deleteConfirm(ctx, m)) }
	return v
}

// startEdit edits my message m in the composer (focused).
func (v *channelView) startEdit(ctx *ui.Context, m api.Message) tea.Cmd {
	if v.inputBlocked(ctx) {
		return nil
	}
	v.composer.startEdit(m)
	return v.composer.focus()
}

func (v *channelView) id() string { return v.route.ID }

// channel is the Store channel (nil when unknown).
func (v *channelView) channel(ctx *ui.Context) *api.Channel {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return ctx.Store.Channels[v.id()]
}

// gone: the channel left the Store after bootstrap (deleted, kicked, left).
func (v *channelView) gone(ctx *ui.Context) bool {
	return v.channel(ctx) == nil && ctx != nil && ctx.Store != nil && ctx.Store.Sync.Bootstrapped
}

func (v *channelView) Init(ctx *ui.Context) tea.Cmd {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	v.chat = c
	c.SetActiveChannel(v.id())
	var cmds []tea.Cmd
	// a window a resync never refreshed (no live view applied it) is refetched
	if !c.Fresh(v.id()) || v.route.Anchor != "" {
		cmds = append(cmds, v.fetchHistory(ctx, v.route.Anchor))
	}
	v.pane.refresh(ctx)
	v.syncComposer(ctx)
	if v.pane.loading {
		cmds = append(cmds, v.pane.startLoading(ctx))
	}
	v.syncOpen(ctx)
	cmds = append(cmds, v.maybeArmRead(ctx))
	return tea.Batch(cmds...)
}

// fetchHistory requests the latest page (anchor "") or the page around anchor.
func (v *channelView) fetchHistory(ctx *ui.Context, anchor string) tea.Cmd {
	v.jump = anchor
	v.firstBusy = true
	return v.request(ctx, api.MessageQuery{Around: anchor, Limit: historyLimit})
}

// request builds a ListMessages request with the chatstore ticket of now: a
// replacing page re-applies what happened while it was in flight, an older /
// newer page of a window replaced meanwhile is discarded.
func (v *channelView) request(ctx *ui.Context, q api.MessageQuery) tea.Cmd {
	ch := v.id()
	var t chatstore.PageTicket
	if c := chatOf(ctx); c != nil {
		t = c.Ticket(ch)
	}
	return ui.Do(ctx, "chat.history:"+ch, func(c context.Context, cl *api.Client) (historyPage, error) {
		w, err := cl.ListMessages(c, ch, q)
		return historyPage{Q: q, W: w, T: t}, err
	})
}

// syncGen drops the older / newer in-flight flags once the window they were
// requested for was replaced (their pages will be discarded), so the new
// window can page at once.
func (v *channelView) syncGen(ctx *ui.Context) {
	c := chatOf(ctx)
	if c == nil || c.Current(v.id(), v.gen) {
		return
	}
	v.gen = c.Ticket(v.id())
	if v.pane.loadingOlder || v.newerBusy {
		v.pane.loadingOlder, v.newerBusy = false, false
		v.pane.refresh(ctx)
		v.pane.stopLoading()
	}
}

// loadFirst requests the first page (latest, or around anchor) with the loading
// row shown again, dropping a failed-first-page notice.
func (v *channelView) loadFirst(ctx *ui.Context, anchor string) tea.Cmd {
	v.pane.failed = ""
	cmd := v.fetchHistory(ctx, anchor)
	v.pane.refresh(ctx)
	if v.pane.loading {
		cmd = tea.Batch(cmd, v.pane.startLoading(ctx))
	}
	return cmd
}

// retryHistory re-requests the first page after it failed (nil while none failed,
// so a loaded conversation or one with its first request in flight is left alone).
func (v *channelView) retryHistory(ctx *ui.Context) tea.Cmd {
	if v.pane.failed == "" || v.gone(ctx) {
		return nil
	}
	return v.loadFirst(ctx, v.jump)
}

// failedNotice is the row shown instead of the loading row once the first page
// failed: "메시지를 불러오지 못했어요 · r 다시 시도" (the retry key is msg.react's).
func failedNotice(ctx *ui.Context) string {
	k := "r"
	if ctx != nil && ctx.Keys != nil {
		if hk := ctx.Keys.Binding(keymap.MsgReact).Help().Key; hk != "" {
			k = hk
		}
	}
	return loadFailed + " · " + k + " " + retryDesc
}

// loadOlder requests the page before the first loaded message (one at a time).
func (v *channelView) loadOlder(ctx *ui.Context) tea.Cmd {
	v.syncGen(ctx)
	w := v.pane.window(ctx)
	if v.pane.loadingOlder || w == nil || w.Len() == 0 || !w.HasMoreBefore() {
		return nil
	}
	v.pane.loadingOlder = true
	v.pane.refresh(ctx)
	return tea.Batch(v.pane.startLoading(ctx), v.request(ctx, api.MessageQuery{Before: w.Items()[0].ID, Limit: historyLimit}))
}

// loadNewer requests the page after the overlap anchor of a window that is not
// the latest (one at a time).
func (v *channelView) loadNewer(ctx *ui.Context) tea.Cmd {
	v.syncGen(ctx)
	w := v.pane.window(ctx)
	if v.newerBusy || w == nil || w.Len() == 0 || !w.HasMoreAfter() {
		return nil
	}
	after := w.OverlapAnchor(afterOverlap)
	if after == "" {
		after = w.Items()[0].ID
	}
	v.newerBusy = true
	return v.request(ctx, api.MessageQuery{After: after, Limit: historyLimit})
}

// onHistory lands a page of this view.
func (v *channelView) onHistory(ctx *ui.Context, m ui.ResultMsg[historyPage]) tea.Cmd {
	q := m.Value.Q
	c := chatOf(ctx)
	// an older / newer page of a replaced window is not this window's: its
	// flag was dropped when the window changed (syncGen)
	current := c == nil || c.Current(v.id(), m.Value.T)
	switch {
	case q.Before != "":
		if current {
			v.pane.loadingOlder = false
		}
	case q.After != "":
		if current {
			v.newerBusy = false
		}
	default:
		v.firstBusy = false
	}
	if m.Err != nil {
		// a failed first page (latest or around) of a conversation that holds none:
		// the loading row becomes a retry notice (v.jump keeps the anchor to retry)
		if c := chatOf(ctx); q.Before == "" && q.After == "" && c != nil && !c.Loaded(v.id()) {
			v.pane.failed = failedNotice(ctx)
		}
		v.pane.refresh(ctx)
		v.pane.stopLoading()
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: loadFailed} }
	}
	if c == nil {
		return nil
	}
	if !c.ApplyPageFor(v.id(), m.Value.T, q, m.Value.W) {
		v.pane.refresh(ctx)
		v.pane.stopLoading()
		return nil
	}
	v.syncGen(ctx)
	if q.After != "" {
		v.pane.noFollow = true
	}
	v.pane.refresh(ctx)
	v.pane.stopLoading()
	if q.Around != "" && q.Around == v.jump {
		target := q.Around
		if v.route.Query[ui.QueryJump] == ui.JumpUnread {
			if id := v.pane.firstUnreadAfter(ctx, q.Around); id != "" {
				target = id
			}
		}
		v.pane.selectID(target)
		v.jump = ""
	}
	v.syncOpen(ctx)
	return tea.Batch(v.fetchMissing(ctx), v.maybeArmRead(ctx))
}

// onResyncErr: the resync refetch of this window failed and chatstore dropped
// it — the failed-first-page notice offers the retry (and, when the
// conversation is on screen, the error is toasted).
func (v *channelView) onResyncErr(ctx *ui.Context, err error) tea.Cmd {
	c := chatOf(ctx)
	if err == nil || c == nil || c.Loaded(v.id()) || v.firstBusy || v.gone(ctx) {
		return nil
	}
	v.pane.failed = failedNotice(ctx)
	v.pane.stopLoading()
	if ctx.Store != nil && ctx.Store.Viewing(v.id()) {
		return errorCmd(err, loadFailed)
	}
	return nil
}

// refetchDropped (after a resync) loads the first page again when the window
// was dropped (this channel was not open when the resync started, e.g. the
// instance had been released) and nothing is in flight or failed.
func (v *channelView) refetchDropped(ctx *ui.Context) tea.Cmd {
	c := chatOf(ctx)
	if c == nil || c.Loaded(v.id()) || v.firstBusy || v.pane.failed != "" || v.gone(ctx) {
		return nil
	}
	return v.loadFirst(ctx, v.jump)
}

// fetchMissing fetches every in-range message an event referred to that we hold no copy of.
func (v *channelView) fetchMissing(ctx *ui.Context) tea.Cmd {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, id := range c.TakeMissing(v.id()) {
		cmds = append(cmds, ui.Do(ctx, "chat.fetch:"+id, func(c context.Context, cl *api.Client) (*api.Message, error) {
			return cl.GetMessage(c, id)
		}))
	}
	return tea.Batch(cmds...)
}

// latestID is the newest top-level message id of the window ("" = none).
func (v *channelView) latestID(ctx *ui.Context) string {
	w := v.pane.window(ctx)
	if w == nil || w.Len() == 0 {
		return ""
	}
	items := w.Items()
	return items[len(items)-1].ID
}

// unread: the newest loaded message is past my read pointer (and not already sent).
func (v *channelView) unread(ctx *ui.Context) bool {
	ch := v.channel(ctx)
	latest := v.latestID(ctx)
	if ch == nil || latest == "" || latest == v.readSent {
		return false
	}
	return ch.My.LastReadMessageID == nil || latest > *ch.My.LastReadMessageID
}

// maybeArmRead is read phase 1 (CR-2): at the bottom with something unread, arm
// a readTickMsg 1s out. Store.Viewing is NOT consulted here (it may still hold the
// previous Update's set); the tick checks everything again when it fires.
func (v *channelView) maybeArmRead(ctx *ui.Context) tea.Cmd {
	if v.gone(ctx) || !v.pane.atBottom(ctx) || !v.unread(ctx) {
		return nil
	}
	v.readGen++
	return ui.After(ctx, readDelay, readTickMsg{ch: v.id(), gen: v.readGen})
}

// fireRead is read phase 2: the latest arming, still viewed, at the bottom and
// unread => PUT …/read; otherwise dropped (the next trigger re-arms).
func (v *channelView) fireRead(ctx *ui.Context, m readTickMsg) tea.Cmd {
	if m.gen != v.readGen || v.gone(ctx) || !ctx.Store.Viewing(v.id()) || !v.pane.atBottom(ctx) || !v.unread(ctx) {
		return nil
	}
	ch, latest := v.id(), v.latestID(ctx)
	v.readSent = latest
	return ui.Do(ctx, "chat.read:"+ch, func(c context.Context, cl *api.Client) (*api.UnreadState, error) {
		return cl.MarkRead(c, ch, latest)
	})
}

// syncOpen tells chatstore which window to refetch on resync.
func (v *channelView) syncOpen(ctx *ui.Context) {
	c := chatOf(ctx)
	if c == nil || v.gone(ctx) {
		return
	}
	anchor := ""
	if r, ok := v.pane.selected(); ok && r.Kind == rowMessage {
		anchor = r.ID
	}
	c.SetOpen(v.id(), anchor, v.pane.atBottom(ctx))
}

func (v *channelView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	v.syncGen(ctx)
	cmds := []tea.Cmd{v.pane.update(ctx, msg)}
	defer v.syncComposer(ctx)
	switch m := msg.(type) {
	case tea.PasteMsg:
		if v.composer.focused() && !v.inputBlocked(ctx) {
			cmds = append(cmds, v.composer.update(ctx, m))
		}
	case ui.ResultMsg[sendOutcome]:
		if strings.HasPrefix(m.Tag, "chat.send:") && m.Value.ChannelID == v.id() {
			applySend(ctx, m)
			v.pane.refresh(ctx)
			v.syncOpen(ctx)
			cmds = append(cmds, v.maybeArmRead(ctx))
		}
	case ui.EventMsg:
		cmds = append(cmds, v.onEvent(ctx, m.Event), v.composer.onMembersEvent(ctx, m.Event))
	case ui.ResultMsg[msgAction]:
		cmds = append(cmds, landAction(ctx, m))
		v.pane.refresh(ctx)
	case chatChangedMsg:
		v.pane.refresh(ctx)
	case editRequestMsg:
		if m.ChannelID != v.id() {
			break
		}
		if c := chatOf(ctx); c != nil {
			if msg, ok := c.FindMessage(m.MessageID); ok && msg.ThreadRootID == nil && editable(ctx, msg) {
				cmds = append(cmds, v.startEdit(ctx, msg))
			}
		}
	case chatstore.ResyncResultMsg:
		if c := chatOf(ctx); c != nil {
			c.ApplyResync(m)
		}
		v.syncGen(ctx)
		cmds = append(cmds, v.onResyncErr(ctx, m.ChannelErr(v.id())))
		v.pane.refresh(ctx)
		v.syncOpen(ctx)
		cmds = append(cmds, v.maybeArmRead(ctx))
	case ui.ResyncDoneMsg:
		cmds = append(cmds, v.refetchDropped(ctx))
	case ui.RouteUpdatedMsg:
		if m.Route.Key() != v.route.Key() {
			break
		}
		v.route = m.Route
		if c := chatOf(ctx); c != nil {
			c.SetActiveChannel(v.id())
		}
		if ch := v.channel(ctx); m.Route.Query[ui.QueryJump] == ui.JumpUnread && ch != nil && ch.My.LastReadMessageID != nil {
			// reopened at the first unread (알림함): the divider follows the read pointer of now
			v.pane.lastRead = *ch.My.LastReadMessageID
			v.pane.refresh(ctx)
		}
		switch {
		case m.Route.Anchor != "" && v.pane.failed != "":
			cmds = append(cmds, v.loadFirst(ctx, m.Route.Anchor))
		case m.Route.Anchor != "":
			cmds = append(cmds, v.fetchHistory(ctx, m.Route.Anchor))
		default:
			cmds = append(cmds, v.retryHistory(ctx))
		}
		cmds = append(cmds, v.maybeArmRead(ctx))
	case ui.FocusChangedMsg:
		if m.RouteKey == v.route.Key() {
			if c := chatOf(ctx); c != nil {
				c.SetActiveChannel(v.id())
			}
			cmds = append(cmds, v.retryHistory(ctx))
		}
		cmds = append(cmds, v.maybeArmRead(ctx))
	case ui.ModeChangedMsg:
		cmds = append(cmds, v.maybeArmRead(ctx))
	case readTickMsg:
		if m.ch == v.id() {
			cmds = append(cmds, v.fireRead(ctx, m))
		}
	case ui.ResultMsg[historyPage]:
		if m.Tag == "chat.history:"+v.id() {
			cmds = append(cmds, v.onHistory(ctx, m))
		}
	case ui.ResultMsg[*api.Message]:
		if strings.HasPrefix(m.Tag, "chat.edit:") {
			cmds = append(cmds, v.composer.onEditResult(ctx, m))
			v.pane.refresh(ctx)
			break
		}
		if strings.HasPrefix(m.Tag, "chat.fetch:") && m.Err == nil && m.Value != nil && m.Value.ChannelID == v.id() {
			if c := chatOf(ctx); c != nil {
				c.UpsertMessage(*m.Value)
			}
			v.pane.refresh(ctx)
			cmds = append(cmds, v.maybeArmRead(ctx))
		}
	case ui.ResultMsg[*api.Page[api.ChannelMember]]:
		cmds = append(cmds, v.composer.onMembers(ctx, m))
	case ui.ResultMsg[*api.Invite]:
		cmds = append(cmds, v.composer.onInviteCreated(m))
	case ui.ResultMsg[*api.DirectInvite]:
		cmds = append(cmds, v.composer.onDirectInvite(ctx, m))
	case ui.ResultMsg[*api.UnreadState]:
		if m.Tag != "chat.read:"+v.id() {
			break
		}
		if m.Err != nil || m.Value == nil {
			v.readSent = ""
			break
		}
		markReadLocal(ctx.Store, *m.Value)
	}
	return tea.Batch(cmds...)
}

// onEvent re-reads the window after the stores applied ev, highlights a message
// someone else just sent here, fetches missing ids and arms the read.
func (v *channelView) onEvent(ctx *ui.Context, ev realtime.Event) tea.Cmd {
	var cmds []tea.Cmd
	v.pane.refresh(ctx)
	if ev.Type == "message.created" {
		if d, err := ev.Decode(); err == nil {
			if me, ok := d.(realtime.MessageEvent); ok {
				m := me.Message
				if w := v.pane.window(ctx); m.ChannelID == v.id() && m.ThreadRootID == nil && w != nil && !isMe(ctx, m.Author.ID) {
					if _, in := w.Get(m.ID); in {
						cmds = append(cmds, v.pane.startFade(ctx, m.ID))
					}
				}
			}
		}
	}
	v.syncOpen(ctx)
	cmds = append(cmds, v.fetchMissing(ctx), v.maybeArmRead(ctx), typingTick(ctx, ev, v.id(), ""))
	return tea.Batch(cmds...)
}

func (v *channelView) Resize(ctx *ui.Context, size ui.Size) {
	v.size = size
	v.pane.sync(ctx)
}

func (v *channelView) View(ctx *ui.Context, focused bool) string {
	if v.size.W <= 0 || v.size.H <= 0 {
		return ""
	}
	if v.gone(ctx) {
		return widgets.Empty(themeOf(ctx), v.size, "", goneTitle, goneHint)
	}
	v.syncComposer(ctx)
	t := themeOf(ctx)
	w := v.size.W
	var bottom string
	bh := 0
	if text, ro := v.readOnlyText(ctx); ro {
		bottom, bh = cell(t.S.Dim.Render, text, w), 1
	} else {
		cc := v.composer
		cc.placeholder = v.placeholder(ctx)
		label := 0
		if cc.editing != nil {
			label = 1
		}
		// the box (border 2) keeps at least one list line above it; the popup
		// gets the rows left between that line and the box
		cc.maxRows = max(1, min(composerMaxRows(ctx.Density()), v.size.H-1-2-label))
		cc.fitPopup(v.size.H - 1 - cc.boxHeight(w))
		bottom, bh = cc.view(ctx, w), cc.height(w)
	}
	// the typing line sits right above the composer while it keeps one list line
	typing := typingLine(ctx, v.id(), "")
	th := 0
	if typing != "" && v.size.H-bh-1 >= 1 {
		th = 1
	}
	listH := max(0, v.size.H-bh-th)
	v.inputY = listH + th
	var out []string
	if listH > 0 {
		out = append(out, strings.Split(v.pane.view(ctx, ui.Size{W: w, H: listH}, focused && !v.composer.focused()), "\n")...)
	} else {
		v.pane.view(ctx, ui.Size{}, false)
	}
	if th > 0 {
		out = append(out, renderTypingLine(ctx, typing, w))
	}
	out = append(out, strings.Split(bottom, "\n")...)
	return strings.Join(textwidth.Fit(out, w, v.size.H), "\n")
}

// readOnlyText is the line drawn instead of the composer in a read-only
// conversation (ok=false: the conversation takes input).
func (v *channelView) readOnlyText(ctx *ui.Context) (string, bool) {
	return readOnlyLine(v.channel(ctx))
}

// readOnlyLine is the line a read-only conversation (and its threads) draws
// instead of the composer (ok=false: ch takes input, or is unknown).
func readOnlyLine(ch *api.Channel) (string, bool) {
	if ch == nil || !ch.ReadOnly {
		return "", false
	}
	if ch.ReadOnlyReason != nil && *ch.ReadOnlyReason == "blocked" {
		return readOnlyBlocked, true
	}
	return readOnlyText, true
}

// inputBlocked: the conversation takes no input (gone or read-only).
func (v *channelView) inputBlocked(ctx *ui.Context) bool {
	_, ro := v.readOnlyText(ctx)
	return ro || v.gone(ctx)
}

// syncComposer drops the composer's focus once the conversation takes no input.
func (v *channelView) syncComposer(ctx *ui.Context) {
	if v.composer.focused() && v.inputBlocked(ctx) {
		v.composer.blur()
	}
}

// placeholder is the composer placeholder of the density: Full "› # 개발에 메시지
// 보내기" (DM "› 민지에게 메시지 보내기"), Line "› # 개발", Bubble/Minimal "›".
func (v *channelView) placeholder(ctx *ui.Context) string {
	d := ctx.Density()
	ch := v.channel(ctx)
	if ch == nil || d == ui.DensityBubble || d == ui.DensityMinimal {
		return placeholderShort
	}
	title := channelTitle(ctx, ch, false)
	if ch.Kind == api.ChannelDM {
		title = textwidth.Sanitize(ctx.Store.ChannelLabel(ch.ID))
	}
	if d == ui.DensityLine {
		return placeholderShort + " " + title
	}
	if ch.Kind == api.ChannelDM {
		return placeholderShort + " " + title + "에게 메시지 보내기"
	}
	return placeholderShort + " " + title + "에 메시지 보내기"
}

func (v *channelView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if v.gone(ctx) {
		return false, nil
	}
	v.syncComposer(ctx)
	if v.composer.focused() {
		wasEmpty := v.composer.c.Value() == ""
		handled, cmd := v.composer.handleKey(ctx, msg)
		if ctx.Keys.Matches(msg, keymap.ComposeSend) && !wasEmpty && v.composer.editing == nil && v.composer.c.Value() == "" {
			cmd = tea.Batch(cmd, v.afterSend(ctx))
		}
		return handled, cmd
	}
	if v.pane.escDrag(ctx, msg) {
		return true, nil
	}
	if handled, cmd := v.pane.cardKey(ctx, msg); handled {
		return true, cmd
	}
	if ctx.Keys.Matches(msg, keymap.ComposeFocus) {
		if v.inputBlocked(ctx) {
			return false, nil
		}
		return true, v.composer.focus()
	}
	if startsComposing(ctx.Keys, msg) && !v.inputBlocked(ctx) {
		return true, v.composer.startTyping(ctx, msg)
	}
	if v.pane.failed != "" && ctx.Keys.Matches(msg, keymap.MsgReact) {
		return true, v.retryHistory(ctx)
	}
	if r, ok := v.pane.selected(); ok && r.Kind == rowPending && r.Pending != nil && r.Pending.Failed != nil {
		switch {
		case ctx.Keys.Matches(msg, keymap.ComposeSend):
			cmd := resendCmd(ctx, v.id(), r.Pending.Nonce)
			v.pane.refresh(ctx)
			return true, cmd
		case ctx.Keys.Matches(msg, keymap.MsgDelete):
			if c := chatOf(ctx); c != nil {
				c.DropPending(v.id(), r.Pending.Nonce)
			}
			v.pane.refresh(ctx)
			v.pane.snap(-1)
			v.syncOpen(ctx)
			return true, nil
		}
	}
	if ctx.Keys.Matches(msg, keymap.ChannelSearch) {
		return true, ui.Navigate(ui.Route{Kind: ui.RouteSearch, ID: v.id()})
	}
	if ctx.Keys.Matches(msg, keymap.MsgThread) {
		r, ok := v.pane.selected()
		if !ok || r.Kind != rowMessage || r.Msg == nil {
			return false, nil
		}
		return true, v.openThread(r) // nil on a system message (D7)
	}
	if handled, cmd := v.pane.actionKey(ctx, msg, func(m api.Message) tea.Cmd { return v.startEdit(ctx, m) }); handled {
		return true, cmd
	}
	handled, cmd := v.pane.handleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	v.syncOpen(ctx)
	return true, tea.Batch(cmd, v.maybeArmRead(ctx))
}

// openThread opens the thread of a message row.
func (v *channelView) openThread(r row) tea.Cmd {
	if r.Kind != rowMessage || r.Msg == nil || !threadable(*r.Msg) {
		return nil
	}
	return openThread(v.id(), r.Msg.ID)
}

func (v *channelView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.gone(ctx) {
		return nil
	}
	cmd := v.pane.handleMouse(ctx, msg, local)
	v.syncOpen(ctx)
	return tea.Batch(cmd, v.maybeArmRead(ctx))
}

// HandleDrag (ui.DragHandler): the drag selection of the message list (the
// composer below it never takes part: the list clamps there).
func (v *channelView) HandleDrag(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.gone(ctx) {
		return nil
	}
	cmd := v.pane.handleDrag(ctx, msg, local)
	if _, release := msg.(tea.MouseReleaseMsg); !release {
		return cmd
	}
	v.syncOpen(ctx)
	return tea.Batch(cmd, v.maybeArmRead(ctx))
}

// ClearDrag (ui.DragHandler) drops the list's drag selection.
func (v *channelView) ClearDrag() { v.pane.clearDrag() }

// afterSend follows a send: the list shows the new pending row at the bottom
// (the latest page is fetched first when an older page is shown).
func (v *channelView) afterSend(ctx *ui.Context) tea.Cmd {
	var cmd tea.Cmd
	if w := v.pane.window(ctx); w != nil && w.HasMoreAfter() {
		cmd = v.fetchHistory(ctx, "")
	}
	v.pane.refresh(ctx)
	v.pane.selectID(v.pane.lastSelectable())
	v.syncOpen(ctx)
	return cmd
}

// CapturesInput: while the composer has the sub-focus.
func (v *channelView) CapturesInput() bool { return v.composer.focused() }

// Completing (ui.Completer, CR-1/CR-A): the composer has the focus and its
// completion popup shows at least one item.
func (v *channelView) Completing() bool { return v.composer.completing() }

// Cursor: the composer's real cursor (IME) inside the pane, nil while it is blurred.
func (v *channelView) Cursor() *tea.Cursor {
	c := v.composer.cursor()
	if c == nil {
		return nil
	}
	c.Y += v.inputY
	return c
}

func (v *channelView) ViewingChannels() []string { return []string{v.id()} }

func (v *channelView) Title(ctx *ui.Context) (string, ui.Badge) {
	ch := v.channel(ctx)
	if ch == nil {
		return "채널", ui.Badge{}
	}
	return channelTitle(ctx, ch, ctx.Density() == ui.DensityFull), sideBadge(ch)
}

func (v *channelView) Help(ctx *ui.Context) []key.Binding {
	if v.composer.focused() {
		return ctx.Keys.Help(keymap.ComposeSend, keymap.ComposeNewline, keymap.ComposeLeave, keymap.ComposeEditLast)
	}
	if v.HidesThread(ctx) {
		// a system line takes neither a thread nor a reaction (D7)
		return ctx.Keys.Help(keymap.ComposeFocus, keymap.ChannelSearch)
	}
	return ctx.Keys.Help(keymap.MsgThread, keymap.MsgReact, keymap.ComposeFocus, keymap.ChannelSearch)
}

// HidesThread (ui.ThreadHider): the selected line is a system message, which
// opens no thread (D7), so the status line drops the mode's thread hint too.
// The composer's own hints replace the mode hints, so it never applies there.
func (v *channelView) HidesThread(*ui.Context) bool {
	r, ok := v.pane.selected()
	return ok && r.Kind == rowMessage && r.Msg != nil && !threadable(*r.Msg)
}

// SaveState is called when the app releases the instance (CA 5.3.3): the
// window stops being open, so a resync no longer refetches it.
func (v *channelView) SaveState() any {
	if v.chat != nil {
		v.chat.SetClosed(v.id())
	}
	st := channelSaved{Anchor: v.pane.list.Anchor(), Draft: v.composer.draft()}
	if r, ok := v.pane.selected(); ok {
		st.Selected = r.ID
	}
	return st
}

func (v *channelView) LoadState(s any) {
	var st channelSaved
	switch x := s.(type) {
	case channelSaved:
		st = x
	case *channelSaved:
		if x == nil {
			return
		}
		st = *x
	default:
		return
	}
	if st.Draft != "" && v.composer.c.Value() == "" && v.composer.editing == nil {
		v.composer.setDraft(st.Draft)
	}
	v.pane.restore = &st
	if v.pane.started {
		v.pane.applyRestore()
	}
}
