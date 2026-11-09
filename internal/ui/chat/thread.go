package chat

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteThread, newThread)
}

// Thread view constants.
const (
	threadTitle        = "스레드"
	threadPlaceholder  = "› 답글"
	threadTitleCells   = 12 // root preview in the title
	rootMaxLines       = 6  // the root's line cap until expanded
	rootMoreText       = "… 더 보기"
	threadNarrow       = 48 // a Full thread narrower than this draws one-line rows
	anchorMaxPages     = 20 // pages turned looking for an anchor
	anchorNotFoundText = "답글을 찾을 수 없어요"
	repliesRowID       = "replies"
)

// threadPage is the result of one GetThread request of a thread view; T is
// the chatstore ticket taken when the request was built.
type threadPage struct {
	Q api.ThreadQuery
	W *api.ThreadWindow
	T chatstore.PageTicket
}

// threadView is RouteThread: a thread's root, its replies and a reply composer.
// Placement per mode is the app's (CA 5.3.3); it does not implement
// ui.ChannelViewer (a lone thread does not mark its channel seen).
type threadView struct {
	route    ui.Route
	pane     *msgPane
	composer *chatComposer
	size     ui.Size
	inputY   int

	firstBusy bool // the first (latest) page is in flight
	newerBusy bool // a newer page is in flight
	notFound  bool // the server does not know the root (404)

	anchorDone string // last anchor handled (never applied twice)
	jump       string // anchor being paged toward ("" = none)
	jumpPages  int    // pages turned for jump

	chat *chatstore.ChatStore // the store this view marked the thread open in
	gen  chatstore.PageTicket // reply window the older / newer flags belong to (syncGen)
}

var (
	_ ui.Component     = (*threadView)(nil)
	_ ui.KeyHandler    = (*threadView)(nil)
	_ ui.MouseHandler  = (*threadView)(nil)
	_ ui.InputCapturer = (*threadView)(nil)
	_ ui.Cursorer      = (*threadView)(nil)
	_ ui.Completer     = (*threadView)(nil)
	_ ui.Titled        = (*threadView)(nil)
	_ ui.Helper        = (*threadView)(nil)
	_ ui.Persistent    = (*threadView)(nil)
)

// openThread opens the thread of rootID in channelID (placement is the app's).
func openThread(channelID, rootID string) tea.Cmd {
	return ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: rootID, Arg: channelID})
}

// newThread is the RouteThread factory (ID = root message id, Arg = channel id).
func newThread(ctx *ui.Context, r ui.Route) ui.Component {
	root := r.ID
	v := &threadView{route: r, pane: newMsgPane(r.Arg, root), composer: newChatComposer(r.Arg, &root, threadPlaceholder)}
	v.pane.older = v.loadOlder
	v.pane.newer = v.loadNewer
	v.pane.open = v.openRow
	v.pane.head = v.headRows
	v.pane.dens = v.density
	v.composer.onEmptyEdit = func(ctx *ui.Context, m api.Message) tea.Cmd { return openOverlayCmd(deleteConfirm(ctx, m)) }
	return v
}

func (v *threadView) rootID() string    { return v.route.ID }
func (v *threadView) channelID() string { return v.route.Arg }

// root is the held root copy: the thread's, else the channel window's (nil = none yet).
func (v *threadView) root(ctx *ui.Context) *api.Message {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	if r, _ := c.Thread(v.rootID()); r != nil {
		return r
	}
	if m, ok := c.FindMessage(v.rootID()); ok {
		return &m
	}
	return nil
}

// replies is the reply window (nil without a chat store).
func (v *threadView) replies(ctx *ui.Context) *store.Window[api.Message] {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	_, w := c.Thread(v.rootID())
	return w
}

// loaded: a page of the thread has landed.
func (v *threadView) loaded(ctx *ui.Context) bool {
	c := chatOf(ctx)
	if c == nil {
		return false
	}
	r, _ := c.Thread(v.rootID())
	return r != nil
}

// gone: the root is unknown to the server, or its channel left the Store after bootstrap.
func (v *threadView) gone(ctx *ui.Context) bool {
	if v.notFound {
		return true
	}
	return ctx != nil && ctx.Store != nil && ctx.Store.Sync.Bootstrapped && ctx.Store.Channels[v.channelID()] == nil
}

// density is ctx.Density(), except a Full thread narrower than threadNarrow
// (the command column) draws one-line rows.
func (v *threadView) density(ctx *ui.Context) ui.Density {
	d := ctx.Density()
	if d == ui.DensityFull && v.size.W > 0 && v.size.W < threadNarrow {
		return ui.DensityLine
	}
	return d
}

// headRows are the root and the "답글 N개" divider above the replies.
func (v *threadView) headRows(ctx *ui.Context) []row {
	r := v.root(ctx)
	if r == nil {
		return nil
	}
	m := *r
	n := 0
	if m.Thread != nil {
		n = m.Thread.ReplyCount
	}
	label := "답글 " + humanize.Count(n) + "개"
	if ctx.Density() == ui.DensityMinimal { // tiny: no pane title says this is a thread (nor where it belongs)
		label = v.parentPrefix(ctx) + threadTitle + " · " + label
	}
	return []row{
		{Kind: rowMessage, ID: m.ID, Msg: &m},
		{Kind: rowReplies, ID: repliesRowID, Text: label},
	}
}

func (v *threadView) Init(ctx *ui.Context) tea.Cmd {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	v.chat = c
	c.SetThreadOpen(v.rootID(), true)
	v.firstBusy = true
	cmds := []tea.Cmd{v.request(ctx, api.ThreadQuery{Limit: historyLimit})}
	v.pane.refresh(ctx)
	if v.pane.loading {
		cmds = append(cmds, v.pane.startLoading(ctx))
	}
	if v.route.Anchor != "" {
		cmds = append(cmds, v.jumpTo(ctx, v.route.Anchor))
	}
	return tea.Batch(cmds...)
}

// request builds a GetThread request with the chatstore ticket of now (see
// channelView.request).
func (v *threadView) request(ctx *ui.Context, q api.ThreadQuery) tea.Cmd {
	root := v.rootID()
	var t chatstore.PageTicket
	if c := chatOf(ctx); c != nil {
		t = c.ThreadTicket(root)
	}
	return ui.Do(ctx, "chat.thread:"+root, func(c context.Context, cl *api.Client) (threadPage, error) {
		w, err := cl.GetThread(c, root, q)
		return threadPage{Q: q, W: w, T: t}, err
	})
}

// syncGen drops the older / newer in-flight flags once the reply window they
// were requested for was replaced (their pages will be discarded).
func (v *threadView) syncGen(ctx *ui.Context) {
	c := chatOf(ctx)
	if c == nil || c.ThreadCurrent(v.rootID(), v.gen) {
		return
	}
	v.gen = c.ThreadTicket(v.rootID())
	if v.pane.loadingOlder || v.newerBusy {
		v.pane.loadingOlder, v.newerBusy = false, false
		v.pane.refresh(ctx)
		v.pane.stopLoading()
	}
}

// retryFirst re-requests the first page after it failed.
func (v *threadView) retryFirst(ctx *ui.Context) tea.Cmd {
	if v.pane.failed == "" || v.firstBusy || v.gone(ctx) {
		return nil
	}
	v.pane.failed = ""
	v.firstBusy = true
	cmd := v.request(ctx, api.ThreadQuery{Limit: historyLimit})
	v.pane.refresh(ctx)
	if v.pane.loading {
		cmd = tea.Batch(cmd, v.pane.startLoading(ctx))
	}
	return cmd
}

// loadOlder requests the replies before the first loaded one (one at a time).
func (v *threadView) loadOlder(ctx *ui.Context) tea.Cmd {
	v.syncGen(ctx)
	w := v.replies(ctx)
	if v.pane.loadingOlder || v.firstBusy || !v.loaded(ctx) || w == nil || w.Len() == 0 || !w.HasMoreBefore() {
		return nil
	}
	v.pane.loadingOlder = true
	v.pane.refresh(ctx)
	return tea.Batch(v.pane.startLoading(ctx), v.request(ctx, api.ThreadQuery{Before: w.Items()[0].ID, Limit: historyLimit}))
}

// loadNewer requests the replies after the last loaded one (one at a time).
func (v *threadView) loadNewer(ctx *ui.Context) tea.Cmd {
	v.syncGen(ctx)
	w := v.replies(ctx)
	if v.newerBusy || v.firstBusy || !v.loaded(ctx) || w == nil || w.Len() == 0 || !w.HasMoreAfter() {
		return nil
	}
	items := w.Items()
	v.newerBusy = true
	return v.request(ctx, api.ThreadQuery{After: items[len(items)-1].ID, Limit: historyLimit})
}

// busy: a page of the thread is in flight.
func (v *threadView) busy() bool { return v.firstBusy || v.pane.loadingOlder || v.newerBusy }

// onPage lands a page of this thread.
func (v *threadView) onPage(ctx *ui.Context, m ui.ResultMsg[threadPage]) tea.Cmd {
	q := m.Value.Q
	first := q.Before == "" && q.After == ""
	c := chatOf(ctx)
	current := c == nil || c.ThreadCurrent(v.rootID(), m.Value.T)
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
		if api.IsCode(m.Err, "not_found") {
			v.notFound = true
			v.composer.blur()
		} else if first && !v.loaded(ctx) {
			v.pane.failed = failedNotice(ctx)
		}
		v.jump = ""
		v.pane.refresh(ctx)
		v.pane.stopLoading()
		if v.notFound {
			return nil
		}
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: loadFailed} }
	}
	if c == nil || m.Value.W == nil {
		return nil
	}
	if !c.ApplyThreadPageFor(v.rootID(), m.Value.T, q, m.Value.W) {
		// a page of a reply window replaced meanwhile: the jump goes on from the new one
		v.pane.refresh(ctx)
		v.pane.stopLoading()
		return v.advanceJump(ctx)
	}
	v.syncGen(ctx)
	if q.After != "" {
		v.pane.noFollow = true
	}
	v.pane.refresh(ctx)
	v.pane.stopLoading()
	return v.advanceJump(ctx)
}

// jumpTo selects the reply id (Route.Anchor: a notification, the inbox, a search
// result), turning pages toward it when it is not loaded (ThreadQuery has no
// Around): before the first reply while has_more_before, after the last while
// has_more_after (ULIDs sort by time). Not found after that: the root is
// selected and a toast says so. A handled anchor is remembered so the same value
// never drags the selection again.
func (v *threadView) jumpTo(ctx *ui.Context, replyID string) tea.Cmd {
	if replyID == "" {
		return nil
	}
	v.anchorDone = replyID
	v.jump = replyID
	v.jumpPages = 0
	return v.advanceJump(ctx)
}

// advanceJump takes the next step toward v.jump (nil while a page is in flight).
func (v *threadView) advanceJump(ctx *ui.Context) tea.Cmd {
	id := v.jump
	if id == "" || v.busy() || !v.loaded(ctx) {
		return nil
	}
	if v.pane.selectID(id) {
		v.jump = ""
		return nil
	}
	if w := v.replies(ctx); w != nil && w.Len() > 0 && v.jumpPages < anchorMaxPages {
		items := w.Items()
		switch {
		case id < items[0].ID && w.HasMoreBefore():
			v.jumpPages++
			v.pane.loadingOlder = true
			v.pane.refresh(ctx)
			return tea.Batch(v.pane.startLoading(ctx), v.request(ctx, api.ThreadQuery{Before: items[0].ID, Limit: historyLimit}))
		case id > items[len(items)-1].ID && w.HasMoreAfter():
			v.jumpPages++
			v.newerBusy = true
			return v.request(ctx, api.ThreadQuery{After: items[len(items)-1].ID, Limit: historyLimit})
		}
	}
	v.jump = ""
	v.pane.selectID(v.rootID())
	return ui.Toast(anchorNotFoundText, ui.ToastInfo)
}

func (v *threadView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	v.syncGen(ctx)
	cmds := []tea.Cmd{v.pane.update(ctx, msg)}
	defer v.syncComposer(ctx)
	switch m := msg.(type) {
	case tea.PasteMsg:
		if v.composer.focused() && !v.inputBlocked(ctx) {
			cmds = append(cmds, v.composer.update(ctx, m))
		}
	case ui.ResultMsg[threadPage]:
		if m.Tag == "chat.thread:"+v.rootID() {
			cmds = append(cmds, v.onPage(ctx, m))
		}
	case ui.ResultMsg[sendOutcome]:
		if strings.HasPrefix(m.Tag, "chat.send:") && m.Value.ChannelID == v.channelID() {
			applySend(ctx, m) // idempotent: the channel view may land the same result
			v.pane.refresh(ctx)
		}
	case ui.EventMsg:
		v.pane.refresh(ctx)
		cmds = append(cmds, typingTick(ctx, m.Event, v.channelID(), v.rootID()), v.composer.onMembersEvent(ctx, m.Event))
	case ui.ResultMsg[msgAction]:
		cmds = append(cmds, landAction(ctx, m))
		v.pane.refresh(ctx)
	case chatChangedMsg:
		v.pane.refresh(ctx)
	case editRequestMsg:
		if m.ChannelID != v.channelID() {
			break
		}
		if c := chatOf(ctx); c != nil {
			if msg, ok := c.FindMessage(m.MessageID); ok && msg.ThreadRootID != nil && *msg.ThreadRootID == v.rootID() && editable(ctx, msg) {
				cmds = append(cmds, v.startEdit(ctx, msg))
			}
		}
	case chatstore.ResyncResultMsg:
		if c := chatOf(ctx); c != nil {
			c.ApplyResync(m)
		}
		v.syncGen(ctx)
		if err := m.ThreadErr(v.rootID()); err != nil && !v.loaded(ctx) && !v.firstBusy && !v.gone(ctx) {
			// chatstore dropped the thread: the failed-first-page notice offers the retry
			v.pane.failed = failedNotice(ctx)
			v.pane.stopLoading()
		}
		v.pane.refresh(ctx)
	case ui.ResyncDoneMsg:
		cmds = append(cmds, v.refetchDropped(ctx))
	case ui.RouteUpdatedMsg:
		if m.Route.Key() != v.route.Key() {
			break
		}
		v.route = m.Route
		if m.Route.Anchor != "" && m.Route.Anchor != v.anchorDone {
			cmds = append(cmds, v.jumpTo(ctx, m.Route.Anchor))
		} else {
			cmds = append(cmds, v.retryFirst(ctx))
		}
	case ui.FocusChangedMsg:
		if m.RouteKey == v.route.Key() {
			cmds = append(cmds, v.retryFirst(ctx))
		}
	case ui.ResultMsg[*api.Message]:
		if strings.HasPrefix(m.Tag, "chat.edit:") {
			cmds = append(cmds, v.composer.onEditResult(ctx, m))
			v.pane.refresh(ctx)
			break
		}
		if strings.HasPrefix(m.Tag, "chat.fetch:") {
			v.pane.refresh(ctx)
		}
	case ui.ResultMsg[*api.Page[api.ChannelMember]]:
		cmds = append(cmds, v.composer.onMembers(ctx, m))
	case ui.ResultMsg[*api.Invite]:
		cmds = append(cmds, v.composer.onInviteCreated(m))
	case ui.ResultMsg[*api.DirectInvite]:
		cmds = append(cmds, v.composer.onDirectInvite(ctx, m))
	}
	return tea.Batch(cmds...)
}

func (v *threadView) Resize(ctx *ui.Context, size ui.Size) {
	v.size = size
	v.pane.sync(ctx)
}

func (v *threadView) View(ctx *ui.Context, focused bool) string {
	if v.size.W <= 0 || v.size.H <= 0 {
		return ""
	}
	if v.gone(ctx) {
		return widgets.Empty(themeOf(ctx), v.size, "", goneTitle, goneHint)
	}
	v.syncComposer(ctx)
	v.pane.sync(ctx)
	t := themeOf(ctx)
	w := v.size.W
	var bottom string
	bh := 0
	if text, ro := readOnlyLine(v.channel(ctx)); ro {
		bottom, bh = cell(t.S.Dim.Render, text, w), 1
	} else {
		cc := v.composer
		cc.placeholder = threadPlaceholder
		label := 0
		if cc.editing != nil {
			label = 1
		}
		cc.maxRows = max(1, min(composerMaxRows(v.density(ctx)), v.size.H-1-2-label))
		cc.fitPopup(v.size.H - 1 - cc.boxHeight(w))
		bottom, bh = cc.view(ctx, w), cc.height(w)
	}
	// the typing line of this thread sits right above the composer
	typing := typingLine(ctx, v.channelID(), v.rootID())
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

// channel is the thread's channel in the Store (nil when unknown).
func (v *threadView) channel(ctx *ui.Context) *api.Channel {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return ctx.Store.Channels[v.channelID()]
}

// inputBlocked: the thread takes no input (gone or a read-only conversation).
func (v *threadView) inputBlocked(ctx *ui.Context) bool {
	_, ro := readOnlyLine(v.channel(ctx))
	return ro || v.gone(ctx)
}

// syncComposer drops the composer's focus once the thread takes no input.
func (v *threadView) syncComposer(ctx *ui.Context) {
	if v.composer.focused() && v.inputBlocked(ctx) {
		v.composer.blur()
	}
}

// startEdit edits my message m in the composer (focused).
func (v *threadView) startEdit(ctx *ui.Context, m api.Message) tea.Cmd {
	if v.inputBlocked(ctx) {
		return nil
	}
	v.composer.startEdit(m)
	return v.composer.focus()
}

// openRow runs a click on the selected row / msg.thread on it: the root
// toggles its line cap; a reply is already in its thread.
func (v *threadView) openRow(r row) tea.Cmd {
	if r.Kind == rowMessage && r.ID == v.rootID() {
		v.pane.rootExpanded = !v.pane.rootExpanded
		v.pane.repaint()
	}
	return nil
}

func (v *threadView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
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
		return true, v.retryFirst(ctx)
	}
	if r, ok := v.pane.selected(); ok && r.Kind == rowPending && r.Pending != nil && r.Pending.Failed != nil {
		switch {
		case ctx.Keys.Matches(msg, keymap.ComposeSend):
			cmd := resendCmd(ctx, v.channelID(), r.Pending.Nonce)
			v.pane.refresh(ctx)
			return true, cmd
		case ctx.Keys.Matches(msg, keymap.MsgDelete):
			if c := chatOf(ctx); c != nil {
				c.DropPending(v.channelID(), r.Pending.Nonce)
			}
			v.pane.refresh(ctx)
			v.pane.snap(-1)
			return true, nil
		}
	}
	if ctx.Keys.Matches(msg, keymap.MsgThread) {
		r, ok := v.pane.selected()
		if !ok || r.Kind != rowMessage || r.ID != v.rootID() {
			return false, nil
		}
		return true, v.openRow(r)
	}
	if handled, cmd := v.pane.actionKey(ctx, msg, func(m api.Message) tea.Cmd { return v.startEdit(ctx, m) }); handled {
		return true, cmd
	}
	handled, cmd := v.pane.handleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, v.olderAtRoot(ctx))
}

// olderAtRoot loads older replies once the selection reached the root (the top
// of the thread) while replies are missing above.
func (v *threadView) olderAtRoot(ctx *ui.Context) tea.Cmd {
	if r, ok := v.pane.selected(); ok && r.ID == v.rootID() {
		return v.loadOlder(ctx)
	}
	return nil
}

func (v *threadView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.gone(ctx) {
		return nil
	}
	if local.Y >= v.inputY {
		if c, ok := msg.(tea.MouseClickMsg); ok && c.Button == tea.MouseLeft {
			v.pane.clearDrag()
		}
		if _, ok := msg.(tea.MouseClickMsg); ok && !v.inputBlocked(ctx) && !v.composer.focused() {
			return v.composer.focus()
		}
		return nil
	}
	return tea.Batch(v.pane.handleMouse(ctx, msg, local), v.olderAtRoot(ctx))
}

// HandleDrag (ui.DragHandler): the drag selection of the root and replies; a
// drag past the list (over the composer) clamps to its last line.
func (v *threadView) HandleDrag(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.gone(ctx) {
		return nil
	}
	return v.pane.handleDrag(ctx, msg, local)
}

// ClearDrag (ui.DragHandler) drops the list's drag selection.
func (v *threadView) ClearDrag() { v.pane.clearDrag() }

// afterSend follows a send: the new pending reply is selected at the bottom
// (the latest page is fetched first when an older one is shown).
func (v *threadView) afterSend(ctx *ui.Context) tea.Cmd {
	var cmd tea.Cmd
	if w := v.replies(ctx); w != nil && w.HasMoreAfter() && !v.busy() {
		v.firstBusy = true
		cmd = v.request(ctx, api.ThreadQuery{Limit: historyLimit})
	}
	v.pane.refresh(ctx)
	v.pane.selectID(v.pane.lastSelectable())
	return cmd
}

// CapturesInput: while the composer has the sub-focus.
func (v *threadView) CapturesInput() bool { return v.composer.focused() }

// Completing (ui.Completer): the composer has the focus and its popup shows items.
func (v *threadView) Completing() bool { return v.composer.completing() }

// Cursor: the composer's real cursor inside the pane, nil while it is blurred.
func (v *threadView) Cursor() *tea.Cursor {
	c := v.composer.cursor()
	if c == nil {
		return nil
	}
	c.Y += v.inputY
	return c
}

// Title is "스레드 · <root author>: <root preview>" ("스레드" before the root is known).
func (v *threadView) Title(ctx *ui.Context) (string, ui.Badge) {
	r := v.root(ctx)
	if r == nil || v.gone(ctx) {
		return threadTitle, ui.Badge{}
	}
	switch {
	case folded(ctx, *r):
		return threadTitle + " · " + blockedText, ui.Badge{}
	case r.Deleted:
		return threadTitle + " · " + deletedText, ui.Badge{}
	}
	return v.parentPrefix(ctx) + threadTitle + " · " + textwidth.Sanitize(displayName(r.Author)) + ": " + markup.Plain(plainBody(r), threadTitleCells), ui.Badge{}
}

// parentPrefix is "<conversation> › " while the thread is shown in Main (no thread
// pane: compact, widget, tiny, a narrow standard), so the single pane names where the
// thread belongs; "" beside its conversation or when the channel is unknown.
func (v *threadView) parentPrefix(ctx *ui.Context) string {
	if ctx == nil {
		return ""
	}
	if _, main := ctx.Layout.Pane(layout.SlotMain); !main {
		return ""
	}
	if _, pane := ctx.Layout.Pane(layout.SlotThread); pane {
		return ""
	}
	if l := v.parentLabel(ctx); l != "" {
		return l + " › "
	}
	return ""
}

// parentLabelCells caps the conversation label in the title and the c hint, so a long
// channel name never pushes "스레드" out.
const parentLabelCells = 12

// parentLabel is the thread's conversation label ("#개발", "민지"), at most
// parentLabelCells; "" when unknown.
func (v *threadView) parentLabel(ctx *ui.Context) string {
	if ctx == nil || ctx.Store == nil {
		return ""
	}
	return textwidth.Truncate(textwidth.Sanitize(ctx.Store.ChannelLabel(v.channelID())), parentLabelCells, "…")
}

func (v *threadView) Help(ctx *ui.Context) []key.Binding {
	if v.composer.focused() {
		return ctx.Keys.Help(keymap.ComposeSend, keymap.ComposeNewline, keymap.ComposeLeave, keymap.ComposeEditLast)
	}
	parent := ctx.Keys.Binding(keymap.ThreadChannel)
	if l := v.parentLabel(ctx); l != "" {
		parent.SetHelp(parent.Help().Key, withRo(l)) // "c #개발로"
	}
	if v.parentPrefix(ctx) == "" { // beside its conversation: the way there is a hint among others
		return append(ctx.Keys.Help(keymap.ComposeFocus, keymap.MsgReact, keymap.MsgActions, keymap.NavBack), parent)
	}
	// alone in Main: the way to its conversation and back come first
	return append([]key.Binding{parent}, ctx.Keys.Help(keymap.ComposeFocus, keymap.NavBack, keymap.MsgReact, keymap.MsgActions)...)
}

// withRo appends the particle 으로/로: 로 after a vowel or a final ㄹ, else 으로.
func withRo(word string) string {
	r := []rune(word)
	if len(r) == 0 {
		return word
	}
	if last := r[len(r)-1]; last >= 0xAC00 && last <= 0xD7A3 {
		if fin := (last - 0xAC00) % 28; fin != 0 && fin != 8 { // 8 = ㄹ
			return word + "으로"
		}
	}
	return word + "로"
}

// refetchDropped (after a resync) loads the first page again when the thread
// was dropped (it was not open when the resync started) and nothing is in
// flight or failed.
func (v *threadView) refetchDropped(ctx *ui.Context) tea.Cmd {
	if chatOf(ctx) == nil || v.loaded(ctx) || v.firstBusy || v.pane.failed != "" || v.gone(ctx) {
		return nil
	}
	if c := chatOf(ctx); c != nil {
		c.SetThreadOpen(v.rootID(), true)
	}
	v.firstBusy = true
	cmd := v.request(ctx, api.ThreadQuery{Limit: historyLimit})
	v.pane.refresh(ctx)
	if v.pane.loading {
		cmd = tea.Batch(cmd, v.pane.startLoading(ctx))
	}
	return cmd
}

// SaveState is called when the app releases the instance (CA 5.3.3): the
// thread stops being open (its replies are no longer kept live nor refetched).
func (v *threadView) SaveState() any {
	if v.chat != nil {
		v.chat.SetThreadOpen(v.rootID(), false)
	}
	st := channelSaved{Anchor: v.pane.list.Anchor(), Draft: v.composer.draft()}
	if r, ok := v.pane.selected(); ok {
		st.Selected = r.ID
	}
	return st
}

func (v *threadView) LoadState(s any) {
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
