package app

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/authui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Bootstrap / resync orchestration (CA 5.4 start order, events.md 5.3) and the
// session end (401 / 4001 / 4003 / logout, CA 3.4).

// Timings and texts.
const (
	bootstrapRetry = 10 * time.Second // a failed bootstrap is retried after this
	logoutTimeout  = 10 * time.Second // POST /v1/auth/logout before logging out locally anyway
)

// LoggedOutMsg reasons (ui.LoggedOutMsg.Reason).
const (
	reasonUnauthorized = "unauthorized"
	reasonSuspended    = "suspended"
	reasonLogout       = "logout"
)

// loginNotices are the login screen notices per logout reason (plan 03 Task 30).
var loginNotices = map[string]string{
	reasonUnauthorized: "다른 곳에서 로그아웃됐어요. 다시 로그인해 주세요",
	reasonSuspended:    "정지된 계정이에요",
}

// loginNoticeKey is the login Route.Query key carrying the notice for the login screen
// (authui reads it on creation and on RouteUpdatedMsg).
const loginNoticeKey = "notice"

// bootstrapRetryMsg retries the bootstrap of generation gen.
type bootstrapRetryMsg struct{ gen int }

// bootstrapDoneMsg carries the result of the bootstrap of generation gen.
type bootstrapDoneMsg struct {
	gen int
	res store.BootstrapMsg
}

// unauthorizedMsg wakes Update after api.WithOnUnauthorized fired (the flag does the work).
type unauthorizedMsg struct{}

// upgradeMsg wakes Update after api.WithOnUpgradeRequired fired (the flag does the work).
type upgradeMsg struct{}

// logoutDoneMsg ends the logout command after POST /v1/auth/logout (any outcome).
type logoutDoneMsg struct{ err error }

// onUnauthorized is the api.WithOnUnauthorized hook. It runs on an HTTP goroutine, so it
// only sets a flag (read at the start of the next Update) and wakes the program.
func (m *Model) onUnauthorized() {
	m.unauthFlag.Store(true)
	if m.send != nil {
		m.send(unauthorizedMsg{})
	}
}

// onUpgrade is the api.WithOnUpgradeRequired hook (HTTP goroutine; flag + wake).
func (m *Model) onUpgrade(string) {
	m.upgradeFlag.Store(true)
	if m.send != nil {
		m.send(upgradeMsg{})
	}
}

// takeFlags handles the API hook flags at the start of Update.
func (m *Model) takeFlags() tea.Cmd {
	var cmds []tea.Cmd
	if m.unauthFlag.Swap(false) {
		reason := reasonUnauthorized
		if m.loggingOut { // the logout call itself got 401: the token was already gone
			reason = reasonLogout
		}
		cmds = append(cmds, m.logout(reason))
	}
	if m.upgradeFlag.Swap(false) {
		cmds = append(cmds, m.setUpgradeRequired())
	}
	return tea.Batch(cmds...)
}

// startBootstrap runs store.Bootstrap (events.md 5.3 step 1) unless one is in flight
// (then it runs again once that one finishes). ResyncStartedMsg is broadcast first.
func (m *Model) startBootstrap() tea.Cmd {
	if m.login != nil {
		return nil
	}
	if m.booting {
		m.bootAgain = true
		return nil
	}
	m.booting = true
	m.bootGen++
	gen := m.bootGen
	// Events (and channel observations) from now until the result lands are
	// re-applied over the older snapshot by ApplyBootstrap (events.md 5.3 step 5).
	m.ctx.Store.BeginBootstrap()
	boot := store.Bootstrap(m.ctx.API)
	return tea.Batch(
		m.broadcast(ui.ResyncStartedMsg{}),
		func() tea.Msg {
			res, _ := boot().(store.BootstrapMsg)
			return bootstrapDoneMsg{gen: gen, res: res}
		},
	)
}

// onReady decides the resync on a ready event: the first ready of the process, a
// resync_required, or the first ready after a REST bootstrap that ran because the
// socket was late (CA 5.4). Any other resumed ready runs the light Store.Backfill.
func (m *Model) onReady(ev realtime.Event) tea.Cmd {
	v, err := ev.Decode()
	r, ok := v.(realtime.Ready)
	if err != nil || !ok {
		slog.Debug("bad ready", "err", err)
		return nil
	}
	first := !m.ctx.Store.Sync.Ready
	m.ctx.Store.Sync.Ready = true
	m.readyThisRun = true
	again := m.resyncOnReady
	m.resyncOnReady = false
	if first || r.ResyncRequired || again {
		return m.startBootstrap()
	}
	if r.Resumed {
		// the logged events were replayed; refetch what the transient SNS ones
		// (events.md 4.8) would have brought while the socket was down
		return m.ctx.Store.Backfill(m.ctx.API)
	}
	return nil
}

// onReadyTimeout is the 5s fallback: no ready on run gen => REST bootstrap now, and
// one more resync when ready finally arrives.
func (m *Model) onReadyTimeout(msg readyTimeoutMsg) tea.Cmd {
	if msg.gen != m.rtGen || m.readyThisRun || m.rt == nil {
		return nil
	}
	m.resyncOnReady = true
	return m.startBootstrap()
}

// onBootstrapDone applies a bootstrap result: the Store (partial results too, see
// ApplyBootstrap) and every Resyncer; on failure Sync.Err, an error toast (the first
// failure in a row only) and a 10s retry; on success the tabs of channels that no longer exist are dropped. The
// relayout (and so SetViewing) happens before ResyncDoneMsg is broadcast.
func (m *Model) onBootstrapDone(msg bootstrapDoneMsg) tea.Cmd {
	if msg.gen != m.bootGen || m.login != nil {
		return nil
	}
	m.booting = false
	res := msg.res
	failedBefore := m.ctx.Store.Sync.Err != nil
	cmds := []tea.Cmd{m.ctx.Store.ApplyBootstrap(res)}
	if res.Err != nil {
		m.bootAgain = false
		if api.CodeOf(res.Err) != "unauthorized" { // 401: the session ends instead (takeFlags)
			if !failedBefore { // one toast per outage, not one per retry
				cmds = append(cmds, m.toasts.push(m.ctx, errorToast(ui.ErrorMsg{Err: res.Err})))
			}
			cmds = append(cmds, ui.After(m.ctx, bootstrapRetry, bootstrapRetryMsg{gen: m.bootGen}))
		}
		cmds = append(cmds, m.relayout(), m.broadcast(ui.ResyncDoneMsg{Err: res.Err}))
		return tea.Batch(cmds...)
	}
	// Channels created during the flight were re-applied by ApplyBootstrap, so
	// only tabs of channels that are really gone are dropped.
	// DM tabs restored from state.json (the Store did not know them yet) move to the DM surface.
	m.nav = m.nav.pruneChannels(func(id string) bool { return m.ctx.Store.Channels[id] != nil }).liftDMs(m.isDM)
	cmds = append(cmds, m.relayout(), m.broadcast(ui.ResyncDoneMsg{}))
	if m.ctx.Store.Sync.Stale { // the flight journal overflowed: the snapshot may be behind
		m.bootAgain = true
	}
	if m.bootAgain {
		m.bootAgain = false
		cmds = append(cmds, m.startBootstrap())
	}
	return tea.Batch(cmds...)
}

// onBootstrapRetry retries a failed bootstrap of generation gen.
func (m *Model) onBootstrapRetry(msg bootstrapRetryMsg) tea.Cmd {
	if msg.gen != m.bootGen || m.booting || m.login != nil {
		return nil
	}
	return m.startBootstrap()
}

// foldDM folds the DM surface back to its list when its open DM is not kept (gone
// from the Store, or hidden: a 1:1 DM with someone I blocked), and closes a DM
// thread of a channel not kept. It reports whether anything changed.
func (n *NavState) foldDM(keep func(id string) bool) bool {
	d := &n.DM
	if len(d.Stack) >= 2 && d.Stack[1].Kind == ui.RouteChannel && !keep(d.Stack[1].ID) {
		d.Stack, d.Thread = d.Stack[:1], nil
		return true
	}
	if d.Thread != nil && !keep(d.Thread.Arg) {
		d.Thread = nil
		return true
	}
	return false
}

// pruneChannels drops the tabs whose channel root is unknown (deleted or left while
// away) and threads of unknown channels; the active tab stays where it was or clamps.
// An unknown DM on the DM surface folds it to the DM list (foldDM).
func (n NavState) pruneChannels(known func(id string) bool) NavState {
	c := n.clone()
	c.foldDM(known)
	var tabs []TabState
	active := 0
	for i, t := range c.Tabs {
		root := t.Stack[0]
		if root.Kind == ui.RouteChannel && !known(root.ID) {
			continue
		}
		if t.Thread != nil && !known(t.Thread.Arg) {
			t.Thread = nil
		}
		if i < c.Active {
			active++
		}
		tabs = append(tabs, t)
	}
	c.Tabs = tabs
	c.Active = min(active, len(tabs)-1)
	return c.clone()
}

// logoutConfirmed is the confirmed "logout" command: POST /v1/auth/logout with the
// current token, then the local logout whatever the outcome (logoutDoneMsg).
// wipeTools also removes this masil account's tool secrets on this device
// (secrets.DeleteAll; the scope is read now, before the session ends).
func (m *Model) logoutConfirmed(wipeTools bool) tea.Cmd {
	if m.login != nil || m.loggingOut {
		return nil
	}
	m.loggingOut = true
	client := m.ctx.API
	st := m.ctx.Secrets
	server, user, scoped := m.ctx.SecretScope()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
		defer cancel()
		err := client.Logout(ctx)
		if wipeTools && scoped && st != nil {
			if werr := st.DeleteAll(ctx, server, user); werr != nil {
				slog.Warn("wipe tool logins", "err", werr)
			}
		}
		return logoutDoneMsg{err: err}
	}
}

// logout ends the session (CA 3.4, plan 03 Task 30): stop realtime → auth.Clear →
// Store.Reset → drop every instance and overlay → NavState reset and the login
// screen → ui.LoggedOutMsg{Reason} broadcast. Calling it while logged out is a no-op.
func (m *Model) logout(reason string) tea.Cmd {
	m.loggingOut = false
	if m.login != nil {
		return nil
	}
	m.stopRealtime()
	m.resume = realtime.ResumePoint{}
	if err := auth.Clear(m.ctx.Paths); err != nil {
		slog.Warn("clear credentials", "err", err)
	}
	m.creds = nil
	m.ctx.API.SetToken("")
	m.ctx.Store.Reset()

	m.booting, m.bootAgain, m.resyncOnReady, m.readyThisRun = false, false, false, false
	m.bootGen++ // pending results / retries of this session are void
	m.idle, m.idleArmed = false, false
	m.watched, m.watchSent = nil, false
	m.prefix = prefixNone

	m.overlays, m.ovSizes, m.ovSized, m.palette = nil, nil, nil, nil
	m.cache = newInstanceCache()
	m.panes = map[layout.SlotID]*instance{}
	m.nav = newNavState()
	m.intent = FocusIntent{Slot: layout.SlotMain}
	m.lastSpaceID = ""

	r := ui.Route{Kind: ui.RouteLogin}
	if notice := loginNotices[reason]; notice != "" {
		r.Query = map[string]string{loginNoticeKey: notice}
	}
	m.login = &r
	return tea.Batch(m.relayout(), m.broadcast(ui.LoggedOutMsg{Reason: reason}))
}

// loggedIn is authui.LoggedInMsg: token, Store.Me, a fresh navigation (then the
// initial route), and realtime when enabled.
func (m *Model) loggedIn(msg authui.LoggedInMsg) tea.Cmd {
	if msg.Credentials == nil {
		return nil
	}
	m.creds = msg.Credentials
	m.ctx.API.SetToken(msg.Credentials.Token)
	if msg.Me != nil {
		me := *msg.Me
		m.ctx.Store.Me = &me
		m.ctx.Store.ObserveUser(me.UserSummary)
	}
	m.login = nil
	m.nav = newNavState()
	m.intent = FocusIntent{Slot: layout.SlotMain}
	if m.opts.InitialRoute != nil {
		var intent FocusIntent
		m.nav, intent = m.nav.navigate(ui.NavigateMsg{Route: *m.opts.InitialRoute}, m.lay.Mode, m.isDM)
		if intent.Slot != "" {
			m.intent = intent
		}
	}
	return tea.Batch(m.relayout(), m.startRealtime())
}

// loginNotice is the notice of the login screen shown now ("" = none).
func (m *Model) loginNotice() string {
	if m.login == nil {
		return ""
	}
	return m.login.Query[loginNoticeKey]
}

// loginLayout is the logged-out screen (plan 03 Task 33 decision): the login route
// fills the body unbordered above the status line.
func loginLayout(mode layout.Mode, cols, rows int) layout.Layout {
	return layout.Layout{Mode: mode, Cols: cols, Rows: rows, Panes: []layout.Pane{
		{Slot: layout.SlotMain, Rect: layout.Rect{X: 0, Y: 0, W: cols, H: rows - 1}},
		{Slot: layout.SlotStatus, Rect: layout.Rect{X: 0, Y: rows - 1, W: cols, H: 1}},
	}}
}
