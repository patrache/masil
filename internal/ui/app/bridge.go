package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Realtime bridge (CA 5.3.4 step 5, CA 5.4 start order, idle, watch): the realtime
// client runs on its own goroutine and reaches the UI only through the delivery
// queue (deliver.go, then tea.Program.Send) as ui.EventMsg / ui.RealtimeStateMsg;
// a full queue drops events and queues resyncNeededMsg instead. Every Store access stays on
// the Update goroutine.

// Bridge timings (CA 5.4, CA 5.3.2 timers).
const (
	readyTimeout  = 5 * time.Second        // no ready by then => REST bootstrap first
	idleAfter     = 10 * time.Minute       // no key/mouse input => SetIdle(true)
	watchDebounce = 300 * time.Millisecond // layout/scroll change => Watch
	watchMax      = 50                     // topics per watch frame (events.md)
	upgradeText   = "masil을 업데이트해 주세요"     // toast on 426 / close 4010
)

// readyTimeoutMsg fires readyTimeout after realtime run gen started.
type readyTimeoutMsg struct{ gen int }

// watchDebounceMsg is the debounced watch update of generation gen.
type watchDebounceMsg struct{ gen int }

// liveRealtime is the ui.Realtime of a running realtime.Client. State is the last
// RealtimeStateMsg the app received (Update goroutine only).
type liveRealtime struct {
	c  *realtime.Client
	st realtime.State
}

var _ ui.Realtime = (*liveRealtime)(nil)

func (l *liveRealtime) Typing(channelID string, threadRootID *string) {
	l.c.Typing(channelID, threadRootID)
}
func (l *liveRealtime) SetIdle(idle bool)       { l.c.SetIdle(idle) }
func (l *liveRealtime) Watch(topics []string)   { l.c.Watch(topics) }
func (l *liveRealtime) State() realtime.State   { return l.st }
func (l *liveRealtime) MarkProcessed(seq int64) { l.c.MarkProcessed(seq) }

// processedMarker is the optional MarkProcessed of Context.RT (the live client, or a
// test double).
type processedMarker interface{ MarkProcessed(seq int64) }

// rtRun is one realtime.Client run. stopped silences its callbacks once the app moved
// on (reconnect, logout), so a late state of the old client never overwrites the new one.
type rtRun struct{ stopped atomic.Bool }

// realtimeActive reports whether Context.RT is a real (or test) realtime rather than
// the disabled no-op: idle and watch are only tracked then.
func (m *Model) realtimeActive() bool {
	_, off := m.ctx.RT.(noopRealtime)
	return !off && m.login == nil
}

// startRealtime starts the realtime client (full mode, resuming from m.resume) on its
// own goroutine and arms the 5s ready timeout. It is a no-op when realtime is disabled,
// logged out, already running, or there is no sender (tea.Program.Send) yet.
func (m *Model) startRealtime() tea.Cmd {
	if m.opts.DisableRealtime || m.creds == nil || m.login != nil || m.rt != nil || m.send == nil {
		return nil
	}
	devID, err := auth.DeviceID(m.ctx.Paths)
	if err != nil {
		slog.Warn("device id", "err", err)
	}
	m.rtGen++
	run := &rtRun{}
	q := m.queue() // the realtime goroutine only queues: it never waits for Update
	c := realtime.New(realtime.Options{
		API:      m.ctx.API,
		DeviceID: devID,
		Mode:     realtime.ModeFull,
		Resume:   m.resume,
		OnEvent: func(ev realtime.Event) {
			if !run.stopped.Load() {
				q.push(ui.EventMsg{Event: ev})
			}
		},
		OnState: func(st realtime.State) {
			if !run.stopped.Load() {
				q.push(ui.RealtimeStateMsg{State: st})
			}
		},
		Logger: slog.Default(),
		Now:    m.ctx.Now,
		Dial:   m.rtDial,
	})
	ctx, cancel := context.WithCancel(context.Background())
	m.rt, m.rtRun, m.rtCancel = c, run, cancel
	m.ctx.RT = &liveRealtime{c: c, st: realtime.State{Phase: realtime.PhaseConnecting}}
	m.readyThisRun = false
	m.watched, m.watchSent = nil, false
	if m.idle {
		c.SetIdle(true) // re-sent after the connection is live
	}
	go func() {
		if err := c.Run(ctx); err != nil {
			slog.Debug("realtime stopped", "err", err)
		}
	}()
	return tea.Batch(
		ui.After(m.ctx, readyTimeout, readyTimeoutMsg{gen: m.rtGen}),
		m.armWatch(),
	)
}

// stopRealtime stops the running client (if any), remembering its resume position, and
// puts the disabled no-op back into Context.RT.
func (m *Model) stopRealtime() {
	if m.rt == nil {
		return
	}
	m.resume = m.rt.Position()
	m.rtRun.stopped.Store(true)
	m.rt.Close()
	m.rtCancel()
	m.rt, m.rtRun, m.rtCancel = nil, nil, nil
	m.ctx.RT = m.offRT
}

// reconnect is the "reconnect" command: stop the client and start a new one from the
// same position. With realtime disabled (or logged out) it only toasts.
func (m *Model) reconnect() tea.Cmd {
	toast := m.toasts.push(m.ctx, ui.ToastMsg{Text: reconnectText, Kind: ui.ToastInfo})
	if m.rt == nil {
		return tea.Batch(toast, m.startRealtime())
	}
	m.stopRealtime()
	return tea.Batch(toast, m.startRealtime())
}

// onRealtimeState handles a RealtimeStateMsg before it is broadcast: the live state
// lands in Context.RT, Store.Sync.Phase follows it, and terminal errors end the session
// (4001 / 4003) or flag the upgrade (4010).
func (m *Model) onRealtimeState(st realtime.State) tea.Cmd {
	if l, ok := m.ctx.RT.(*liveRealtime); ok {
		l.st = st
	}
	m.ctx.Store.Sync.Phase = st.Phase
	if st.Phase != realtime.PhaseStopped || st.Err == nil {
		return nil
	}
	switch {
	case errors.Is(st.Err, realtime.ErrUnauthorized):
		// Our own logout revokes the session and the server closes the socket with
		// 4001 before it answers the POST, so this usually lands first.
		if m.loggingOut {
			return m.logout(reasonLogout)
		}
		return m.logout(reasonUnauthorized)
	case errors.Is(st.Err, realtime.ErrSuspended):
		return m.logout(reasonSuspended)
	case errors.Is(st.Err, realtime.ErrUpgradeRequired):
		return m.setUpgradeRequired()
	}
	return nil
}

// setUpgradeRequired flags the client as too old (status "✕ 업데이트 필요") and toasts once.
func (m *Model) setUpgradeRequired() tea.Cmd {
	if m.upgradeRequired {
		return nil
	}
	m.upgradeRequired = true
	return m.toasts.push(m.ctx, ui.ToastMsg{Text: upgradeText, Kind: ui.ToastError})
}

// onEvent is CA 5.3.4 step 5: Store.Apply → broadcast → alert handling (CA 9.1) →
// MarkProcessed. Store.Viewing already holds this projection's set (SetViewing runs
// right after every Project), and an event never changes the projection — except
// that a DM which left the Store (deleted, left) or became hidden (I blocked the
// peer) folds the DM surface back to its list (foldDM, CA 5.3.3), relayouting after
// the broadcast. ready also drives the bootstrap / resync (CA 5.4, events.md 5.3).
// Events reaching a logged-out app (the old session's tail) are dropped.
func (m *Model) onEvent(msg ui.EventMsg) tea.Cmd {
	if m.login != nil {
		return nil
	}
	ev := msg.Event
	m.ctx.Store.Apply(ev)
	cmds := []tea.Cmd{m.broadcast(msg)}
	if m.nav.foldDM(m.dmKept) {
		if m.nav.InDM && m.nav.Front == FrontTab {
			m.intent = FocusIntent{Slot: layout.SlotMain, RouteKey: dmsRoute.Key()}
		}
		cmds = append(cmds, m.relayout())
	}
	if ev.Type == "ready" {
		cmds = append(cmds, m.onReady(ev))
	}
	if a, ok := alertOf(ev); ok {
		cmds = append(cmds, m.handleAlert(ev, a))
	}
	if ev.Seq != nil {
		if pm, ok := m.ctx.RT.(processedMarker); ok {
			pm.MarkProcessed(*ev.Seq)
		}
	}
	return tea.Batch(cmds...)
}

// noteInput records a key/mouse/paste input: the idle clock restarts and an idle
// connection becomes active again.
func (m *Model) noteInput() {
	m.lastInput = m.ctx.Now()
	if m.idle {
		m.idle = false
		if m.realtimeActive() {
			m.ctx.RT.SetIdle(false)
		}
	}
}

// armIdle keeps one idle timer pending while realtime is active and not idle; it is
// due idleAfter after the last input (CA 5.4 idle detection).
func (m *Model) armIdle() tea.Cmd {
	if !m.realtimeActive() || m.idleArmed || m.idle {
		return nil
	}
	now := m.ctx.Now()
	if m.lastInput.IsZero() {
		m.lastInput = now
	}
	d := m.lastInput.Add(idleAfter).Sub(now)
	if d <= 0 {
		d = time.Millisecond
	}
	m.idleArmed = true
	m.idleGen++
	return ui.After(m.ctx, d, idleMsg{gen: m.idleGen})
}

// onIdle handles the idle timer: idle when idleAfter passed since the last input
// (otherwise armIdle re-arms for the rest at the end of this Update).
func (m *Model) onIdle(msg idleMsg) {
	if msg.gen != m.idleGen {
		return
	}
	m.idleArmed = false
	if !m.realtimeActive() || m.idle {
		return
	}
	if m.ctx.Now().Sub(m.lastInput) >= idleAfter {
		m.idle = true
		m.ctx.RT.SetIdle(true)
	}
}

// armWatch (re)starts the 300ms watch debounce after a layout or scroll change.
func (m *Model) armWatch() tea.Cmd {
	if !m.realtimeActive() {
		return nil
	}
	m.watchGen++
	return ui.After(m.ctx, watchDebounce, watchDebounceMsg{gen: m.watchGen})
}

// onWatch sends the visible Watchers' topics (focused pane first, deduplicated, at most
// watchMax) when they changed since the last Watch.
func (m *Model) onWatch(msg watchDebounceMsg) {
	if msg.gen != m.watchGen || !m.realtimeActive() {
		return
	}
	topics := m.watchTopics()
	if m.watchSent && slices.Equal(topics, m.watched) {
		return
	}
	m.watched, m.watchSent = topics, true
	m.ctx.RT.Watch(slices.Clone(topics))
}

// watchTopics is the union of the projected Watchers' topics: the focused slot first,
// then the other panes in focus-ring order.
func (m *Model) watchTopics() []string {
	slots := []layout.SlotID{m.focus.Slot}
	for _, s := range focusOrder {
		if s != m.focus.Slot {
			slots = append(slots, s)
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, s := range slots {
		if _, ok := m.lay.Pane(s); !ok {
			continue
		}
		in := m.slotInstance(s)
		if in == nil {
			continue
		}
		w, ok := in.comp.(ui.Watcher)
		if !ok {
			continue
		}
		for _, t := range w.WatchTopics() {
			if len(out) == watchMax {
				return out
			}
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// updateViewing is Store.SetViewing from this projection (CR-C): the channels of every
// placed ChannelViewer. relayout calls it right after Project/Resize and before any
// broadcast of the same Update.
func (m *Model) updateViewing() {
	var ids []string
	for _, p := range m.lay.Panes {
		in := m.slotInstance(p.Slot)
		if in == nil {
			continue
		}
		if cv, ok := in.comp.(ui.ChannelViewer); ok {
			ids = append(ids, cv.ViewingChannels()...)
		}
	}
	m.ctx.Store.SetViewing(ids...)
}

// dialFunc is the realtime dialer type (Model.rtDial; nil = coder/websocket).
type dialFunc = func(ctx context.Context, url string, h http.Header) (realtime.Conn, error)
