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
)

// readTickMsg fires a read arming (gen = the arming it belongs to).
type readTickMsg struct {
	ch  string
	gen uint64
}

// historyPage is the result of one ListMessages request of the view.
type historyPage struct {
	Q api.MessageQuery
	W *api.MessageWindow
}

// channelSaved is the Persistent state of a conversation (Draft: Task 9).
type channelSaved struct {
	Anchor   widgets.ScrollAnchor
	Selected string
	Draft    string
}

// channelView is RouteChannel: one channel's (text / DM / group DM) conversation.
type channelView struct {
	route ui.Route
	pane  *msgPane
	size  ui.Size

	readGen  uint64 // latest read arming
	readSent string // message id of the read request in flight / done

	newerBusy bool   // a newer page is in flight
	jump      string // anchor to select once its page lands
}

var (
	_ ui.Component     = (*channelView)(nil)
	_ ui.KeyHandler    = (*channelView)(nil)
	_ ui.MouseHandler  = (*channelView)(nil)
	_ ui.InputCapturer = (*channelView)(nil)
	_ ui.Cursorer      = (*channelView)(nil)
	_ ui.ChannelViewer = (*channelView)(nil)
	_ ui.Titled        = (*channelView)(nil)
	_ ui.Helper        = (*channelView)(nil)
	_ ui.Persistent    = (*channelView)(nil)
)

// newChannel is the RouteChannel factory. The unread divider pointer is frozen
// here, from the channel's read pointer when the view opens.
func newChannel(ctx *ui.Context, r ui.Route) ui.Component {
	v := &channelView{route: r, pane: newMsgPane(r.ID, "")}
	if ch := v.channel(ctx); ch != nil && ch.My.LastReadMessageID != nil {
		v.pane.lastRead = *ch.My.LastReadMessageID
	}
	v.pane.older = v.loadOlder
	v.pane.newer = v.loadNewer
	v.pane.open = v.openThread
	return v
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
	c.SetActiveChannel(v.id())
	var cmds []tea.Cmd
	if !c.Loaded(v.id()) || v.route.Anchor != "" {
		cmds = append(cmds, v.fetchHistory(ctx, v.route.Anchor))
	}
	v.pane.refresh(ctx)
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
	return v.request(ctx, api.MessageQuery{Around: anchor, Limit: historyLimit})
}

func (v *channelView) request(ctx *ui.Context, q api.MessageQuery) tea.Cmd {
	ch := v.id()
	return ui.Do(ctx, "chat.history:"+ch, func(c context.Context, cl *api.Client) (historyPage, error) {
		w, err := cl.ListMessages(c, ch, q)
		return historyPage{Q: q, W: w}, err
	})
}

// loadOlder requests the page before the first loaded message (one at a time).
func (v *channelView) loadOlder(ctx *ui.Context) tea.Cmd {
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
	switch {
	case q.Before != "":
		v.pane.loadingOlder = false
	case q.After != "":
		v.newerBusy = false
	}
	if m.Err != nil {
		v.pane.refresh(ctx)
		v.pane.stopLoading()
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: "메시지를 불러오지 못했어요"} }
	}
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	c.ApplyPage(v.id(), q, m.Value.W)
	if q.After != "" {
		v.pane.noFollow = true
	}
	v.pane.refresh(ctx)
	v.pane.stopLoading()
	if q.Around != "" && q.Around == v.jump {
		v.pane.selectID(q.Around)
		v.jump = ""
	}
	v.syncOpen(ctx)
	return tea.Batch(v.fetchMissing(ctx), v.maybeArmRead(ctx))
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
	cmds := []tea.Cmd{v.pane.update(ctx, msg)}
	switch m := msg.(type) {
	case ui.EventMsg:
		cmds = append(cmds, v.onEvent(ctx, m.Event))
	case chatstore.ResyncResultMsg:
		if c := chatOf(ctx); c != nil {
			c.ApplyResync(m)
		}
		v.pane.refresh(ctx)
		v.syncOpen(ctx)
		cmds = append(cmds, v.maybeArmRead(ctx))
	case ui.RouteUpdatedMsg:
		if m.Route.Key() != v.route.Key() {
			break
		}
		v.route = m.Route
		if c := chatOf(ctx); c != nil {
			c.SetActiveChannel(v.id())
		}
		if m.Route.Anchor != "" {
			cmds = append(cmds, v.fetchHistory(ctx, m.Route.Anchor))
		}
		cmds = append(cmds, v.maybeArmRead(ctx))
	case ui.FocusChangedMsg:
		if m.RouteKey == v.route.Key() {
			if c := chatOf(ctx); c != nil {
				c.SetActiveChannel(v.id())
			}
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
		if strings.HasPrefix(m.Tag, "chat.fetch:") && m.Err == nil && m.Value != nil && m.Value.ChannelID == v.id() {
			if c := chatOf(ctx); c != nil {
				c.UpsertMessage(*m.Value)
			}
			v.pane.refresh(ctx)
			cmds = append(cmds, v.maybeArmRead(ctx))
		}
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
	cmds = append(cmds, v.fetchMissing(ctx), v.maybeArmRead(ctx))
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
	return v.pane.view(ctx, v.size, focused)
}

func (v *channelView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if v.gone(ctx) {
		return false, nil
	}
	if ctx.Keys.Matches(msg, keymap.MsgThread) {
		r, ok := v.pane.selected()
		if !ok || r.Kind != rowMessage || r.Msg == nil {
			return false, nil
		}
		return true, v.openThread(r)
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
	if r.Kind != rowMessage || r.Msg == nil {
		return nil
	}
	return ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: r.Msg.ID, Arg: v.id()})
}

func (v *channelView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if v.gone(ctx) {
		return nil
	}
	cmd := v.pane.handleMouse(ctx, msg, local)
	v.syncOpen(ctx)
	return tea.Batch(cmd, v.maybeArmRead(ctx))
}

// CapturesInput: no text field yet (the composer comes with Task 9).
func (v *channelView) CapturesInput() bool { return false }

// Cursor: none while nothing captures input.
func (v *channelView) Cursor() *tea.Cursor { return nil }

func (v *channelView) ViewingChannels() []string { return []string{v.id()} }

func (v *channelView) Title(ctx *ui.Context) (string, ui.Badge) {
	ch := v.channel(ctx)
	if ch == nil {
		return "채널", ui.Badge{}
	}
	return channelTitle(ctx, ch, ctx.Density() == ui.DensityFull), sideBadge(ch)
}

func (v *channelView) Help(ctx *ui.Context) []key.Binding {
	return ctx.Keys.Help(keymap.MsgThread, keymap.MsgReact, keymap.ComposeFocus, keymap.ChannelSearch)
}

func (v *channelView) SaveState() any {
	st := channelSaved{Anchor: v.pane.list.Anchor()}
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
	v.pane.restore = &st
	if v.pane.started {
		v.pane.applyRestore()
	}
}
