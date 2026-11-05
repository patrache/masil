package app

import (
	"log/slog"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// TUI alert handling (CA 9.1): every `alert` event goes through notify.Notifier.Handle
// (OS popup, de-duplicated by the shared ledger); an alert whose target is not on
// screen becomes a ToastAlert that opens the target route; alert{kind:focus_summary}
// opens the focus-summary overlay (registered by sns-fun) instead, once per alert id.

const (
	// summaryRemember is how many opened focus-summary alert ids are remembered.
	summaryRemember = 50
	// staleReplay mirrors notify's rule 1: a replayed alert older than this is old news
	// (events.md 4.7); it never opens the focus summary.
	staleReplay = 10 * time.Minute
	// alertToastSep joins the alert title and body on the toast line.
	alertToastSep = " — "
)

// onTerminalFocus handles tea.FocusMsg / tea.BlurMsg: the first one proves the terminal
// reports focus (Caps.FocusReports), and each one records whether it is focused now.
func (m *Model) onTerminalFocus(focused bool) {
	m.ctx.Caps.FocusReports = true
	m.termFocused = focused
}

// alertOf decodes an `alert` event's alert (ok false for other types or bad data).
func alertOf(ev realtime.Event) (api.Alert, bool) {
	if ev.Type != "alert" {
		return api.Alert{}, false
	}
	v, err := ev.Decode()
	if err != nil {
		slog.Debug("bad alert event", "err", err)
		return api.Alert{}, false
	}
	d, ok := v.(realtime.AlertEvent)
	return d.Alert, ok
}

// handleAlert is CA 9.1 for one alert (Store.Apply already put it in Store.Alerts).
func (m *Model) handleAlert(ev realtime.Event, a api.Alert) tea.Cmd {
	viewing := m.viewingTarget(a)
	now := m.ctx.Now()
	in := notify.Input{
		Role:               notify.RoleTUI,
		Now:                now,
		Replayed:           ev.Replayed,
		DesktopEnabled:     m.ctx.Config.Notify.Desktop,
		TerminalFocusKnown: m.ctx.Caps.FocusReports,
		TerminalFocused:    m.termFocused,
		ViewingTarget:      viewing,
	}
	d, err := m.notifier.Handle(a, in)
	if err != nil {
		slog.Debug("os notification failed", "alert_id", a.ID, "kind", string(a.Kind), "err", err)
	}
	if d == notify.Skip {
		return nil
	}
	if a.Kind == api.AlertFocusSummary {
		if m.summaryOpened(a.ID) { // already shown in this process (replay)
			return nil
		}
		stale := ev.Replayed && now.Sub(a.CreatedAt) > staleReplay
		if _, ok := ui.Registered().Overlays[ui.OverlayFocusSummary]; ok && !stale {
			m.rememberSummary(a.ID)
			return ui.OpenRegisteredOverlay(ui.OverlayFocusSummary, a.ID)
		}
	}
	if viewing {
		return nil
	}
	r := m.alertTargetRoute(a)
	return m.toasts.push(m.ctx, ui.ToastMsg{Text: alertToastText(a), Kind: ui.ToastAlert, Route: &r})
}

// alertToastText is "<title> — <body>" (sanitized, one line); the title alone when the
// body is empty.
func alertToastText(a api.Alert) string {
	title, body := textwidth.Sanitize(a.Title), textwidth.Sanitize(a.Body)
	switch {
	case body == "":
		return title
	case title == "":
		return body
	}
	return title + alertToastSep + body
}

// summaryOpened reports whether the focus summary of alert id was opened already.
func (m *Model) summaryOpened(id string) bool {
	return id != "" && slices.Contains(m.summaries, id)
}

// rememberSummary records id in the ring of the last summaryRemember opened ids.
func (m *Model) rememberSummary(id string) {
	if id == "" {
		return
	}
	m.summaries = append(m.summaries, id)
	if n := len(m.summaries) - summaryRemember; n > 0 {
		m.summaries = slices.Delete(m.summaries, 0, n)
	}
}

// placedComponents are the components drawn in this frame's panes.
func (m *Model) placedComponents() []ui.Component {
	var out []ui.Component
	for _, p := range m.lay.Panes {
		if in := m.slotInstance(p.Slot); in != nil {
			out = append(out, in.comp)
		}
	}
	return out
}

// viewingTarget reports whether the alert's target is on screen now (CA 9.1): a thread
// reply's thread (a placed RouteThread whose ID is thread_root_id), otherwise its
// channel among the placed ChannelViewers' channels, or its post among the placed
// Watchers' "post:<id>" topics.
func (m *Model) viewingTarget(a api.Alert) bool {
	t := a.Target
	if root := deref(t.ThreadRootID); root != "" {
		for _, r := range m.routes {
			if r.Kind == ui.RouteThread && r.ID == root {
				return true
			}
		}
		return false
	}
	ch := deref(t.ChannelID)
	if ch == "" && t.Type == "channel" {
		ch = t.ID
	}
	post := deref(t.PostID)
	if post == "" && t.Type == "post" {
		post = t.ID
	}
	for _, c := range m.placedComponents() {
		if cv, ok := c.(ui.ChannelViewer); ok && ch != "" && slices.Contains(cv.ViewingChannels(), ch) {
			return true
		}
		if w, ok := c.(ui.Watcher); ok && post != "" && slices.Contains(w.WatchTopics(), "post:"+post) {
			return true
		}
	}
	return false
}

// alertTargetRoute is where an alert's toast leads (plan 03 Task 31 table): focus
// summaries and anything unresolvable go to the notifications inbox, friend requests to
// the friends screen, and otherwise by target.type.
func (m *Model) alertTargetRoute(a api.Alert) ui.Route {
	switch a.Kind {
	case api.AlertFocusSummary:
		return NotifRoute
	case api.AlertFriendRequest:
		return ui.Route{Kind: ui.RouteFriends}
	}
	t := a.Target
	switch t.Type {
	case "message", "channel":
		ch := deref(t.ChannelID)
		if ch == "" && t.Type == "channel" {
			ch = t.ID
		}
		if ch == "" {
			break
		}
		msg := deref(t.MessageID)
		if root := deref(t.ThreadRootID); root != "" {
			return ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch, Anchor: msg}
		}
		return ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: msg}
	case "post":
		id := deref(t.PostID)
		if id == "" {
			id = t.ID
		}
		if id == "" {
			break
		}
		return ui.Route{Kind: ui.RoutePost, ID: id}
	case "space":
		space := deref(t.SpaceID)
		if space == "" {
			space = t.ID
		}
		r := ui.Route{Kind: ui.RouteInvite, Arg: "direct"}
		for _, inv := range m.ctx.Store.DirectInvites {
			if space != "" && inv.Space.ID == space {
				r.ID = inv.ID
				break
			}
		}
		return r
	case "user":
		if t.ID == "" {
			break
		}
		return ui.Route{Kind: ui.RouteProfile, ID: t.ID}
	case "notification":
		return NotifRoute
	}
	return NotifRoute
}

// deref is *p, or "" for nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
