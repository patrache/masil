package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// Key routing (CA 5.3.4 step 2): ctrl+c, overlays, the ctrl+w prefix, hard globals,
// the focused component, input capture, soft globals. Every global is matched through
// ctx.Keys so [keys] overrides apply; only the key after the ctrl+w prefix is fixed.

// quitWindow is how long one ctrl+c stays armed (CA 5.3.2 timers, CA 5.5 app.quit).
const quitWindow = time.Second

// quitToastText is shown when the first ctrl+c arms quitting.
const quitToastText = "한 번 더 누르면 종료해요"

// prefixState is the pending multi-key code (CA 5.3.4 2.2).
type prefixState int

const (
	prefixNone   prefixState = iota
	prefixWindow             // ctrl+w seen: the next key picks a pane, a pin or cancels
	prefixEscape             // ctrl+\ seen while a kill switch is active: x cuts it
)

// escapeWindow is how long ctrl+\ waits for x while a kill switch is active
// (the same wait as a pane's own prefix, A7.5).
const escapeWindow = 2 * time.Second

// escapeArmMsg disarms a ctrl+\ of generation gen once escapeWindow has passed.
type escapeArmMsg struct{ gen int }

// quitArmMsg disarms a ctrl+c arming of generation gen once quitWindow has passed.
type quitArmMsg struct{ gen int }

// idleMsg reports no key/mouse input for the idle period (generation gen; Task 30).
type idleMsg struct{ gen int }

// pinMsg pins the layout mode (nil = auto); ctrl+w digits and the layout.* commands.
type pinMsg struct{ mode *layout.Mode }

// pinKeys are the ctrl+w digits (CA 5.5 window.prefix).
var pinKeys = map[string]layout.Mode{
	"1": layout.ModeCommand,
	"2": layout.ModeStandard,
	"3": layout.ModeCompact,
	"4": layout.ModeWidget,
	"5": layout.ModeTiny,
}

// focusedComponent is the component in the resolved focus slot, or nil.
func (m *Model) focusedComponent() ui.Component {
	if in := m.focusedInstance(); in != nil {
		return in.comp
	}
	return nil
}

// capturing reports whether c captures input (ui.InputCapturer).
func capturing(c ui.Component) bool {
	ic, ok := c.(ui.InputCapturer)
	return ok && ic.CapturesInput()
}

// completing reports whether c captures input with its completion popup open (CR-A).
func completing(c ui.Component) bool {
	cp, ok := c.(ui.Completer)
	return ok && capturing(c) && cp.Completing()
}

// codeCompleting reports whether c is a code editor taking ctrl+n / ctrl+p.
func codeCompleting(c ui.Component) bool {
	cc, ok := c.(ui.CodeCompleter)
	return ok && capturing(c) && cc.CompletionKeys()
}

// handleKey gives msg to c's HandleKey (not a KeyHandler => not handled).
func (m *Model) handleKey(c ui.Component, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if kh, ok := c.(ui.KeyHandler); ok {
		return kh.HandleKey(m.ctx, msg)
	}
	return false, nil
}

// routeKey routes one key press (CA 5.3.4 step 2).
func (m *Model) routeKey(msg tea.KeyPressMsg) tea.Cmd {
	keys := m.ctx.Keys
	// ctrl+\ x while sharing (A7.5 JX6): any screen, before everything else.
	if m.prefix == prefixEscape {
		m.prefix = prefixNone
		return m.escapeKey(msg)
	}
	// A component that grabs the keyboard (a running termpane) gets every key —
	// ctrl+c and the hard globals included — unless an overlay is above it.
	if cmd, ok := m.grabbedKey(msg); ok {
		return cmd
	}
	// ctrl+c skips the overlays (2.1) and is decided first.
	if keys.Matches(msg, keymap.AppQuit) {
		m.prefix = prefixNone
		return m.quitKey(msg)
	}
	if ui.KillSwitchActive() && keys.MatchesEscape(msg) {
		m.prefix = prefixEscape
		m.escGen++
		return ui.After(m.ctx, escapeWindow, escapeArmMsg{gen: m.escGen})
	}
	// 2.1 the top overlay; only a non-modal overlay lets an unhandled key go on (steps 3–6).
	if len(m.overlays) > 0 {
		return m.overlayKey(msg)
	}
	// Logged out: every key but ctrl+c goes to the login screen (no globals).
	if m.login != nil {
		m.prefix = prefixNone
		_, cmd := m.handleKey(m.focusedComponent(), msg)
		return cmd
	}
	// 2.2 the ctrl+w prefix.
	if m.prefix == prefixWindow {
		m.prefix = prefixNone
		return m.windowKey(msg)
	}
	return m.globalKeys(msg)
}

// globalKeys is steps 3–6: hard globals, the focused component, input capture, soft globals.
func (m *Model) globalKeys(msg tea.KeyPressMsg) tea.Cmd {
	keys := m.ctx.Keys
	comp := m.focusedComponent()

	// 2.4 a code editor's completion keys (v0.1.3-1), before ctrl+n's notifications
	if s := msg.String(); (s == "ctrl+n" || s == "ctrl+p") && codeCompleting(comp) {
		if handled, cmd := m.handleKey(comp, msg); handled {
			return cmd
		}
	}

	// 3. hard globals
	switch {
	case keys.Matches(msg, keymap.PaletteOpen):
		return m.openPalette("")
	case keys.Matches(msg, keymap.SearchGlobal):
		return m.applyNavigate(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSearch}})
	case keys.Matches(msg, keymap.NotificationsOpen):
		return m.applyToggle(m.nav.toggleNotifications(m.lay.Mode))
	}
	if cmd, ok := m.toolKey(msg); ok { // ctrl+t, before the composer's transpose (tools.go)
		return cmd
	}
	var pre tea.Cmd
	tabKey := keys.Matches(msg, keymap.FocusNext) || keys.Matches(msg, keymap.FocusPrev) || keys.Matches(msg, keymap.ComposeComplete)
	if tabKey && completing(comp) { // CR-A: an open completion popup takes tab/shift+tab first
		handled, cmd := m.handleKey(comp, msg)
		if handled {
			return cmd
		}
		pre = cmd
	}
	switch {
	case keys.Matches(msg, keymap.FocusNext):
		return tea.Batch(pre, m.cycleFocus(+1))
	case keys.Matches(msg, keymap.FocusPrev):
		return tea.Batch(pre, m.cycleFocus(-1))
	case !capturing(comp) && keys.Matches(msg, keymap.WindowPrefix):
		m.prefix = prefixWindow
		return pre
	}

	// 4. the focused component
	handled, cmd := m.handleKey(comp, msg)
	cmd = tea.Batch(pre, cmd)
	if handled {
		return cmd
	}
	// 5. input capture: printable keys never reach the soft globals; a history key the
	// component left alone (ctrl+o in a chat composer) still walks the history.
	walk := keys.Matches(msg, keymap.NavHistoryBack) || keys.Matches(msg, keymap.NavHistoryForward)
	if capturing(comp) && !walk {
		return cmd
	}
	// 6. soft globals
	switch {
	case keys.Matches(msg, keymap.HelpToggle):
		return tea.Batch(cmd, m.openHelp())
	case keys.Matches(msg, keymap.FeedToggle):
		return tea.Batch(cmd, m.applyToggle(m.nav.toggleFeed(m.lay.Mode)))
	case keys.Matches(msg, keymap.TabPrev): // the walk of the top tab bar (walk.go)
		return tea.Batch(cmd, m.walkStep(-1))
	case keys.Matches(msg, keymap.TabNext):
		return tea.Batch(cmd, m.walkStep(+1))
	case keys.Matches(msg, keymap.NavBack):
		return tea.Batch(cmd, m.goBack())
	case keys.Matches(msg, keymap.NavHistoryBack):
		return tea.Batch(cmd, m.walkHistory(-1))
	case keys.Matches(msg, keymap.NavHistoryForward):
		return tea.Batch(cmd, m.walkHistory(+1))
	case keys.Matches(msg, keymap.ThreadChannel):
		return tea.Batch(cmd, m.openParent())
	case keys.Matches(msg, keymap.ListOpen):
		return tea.Batch(cmd, m.openAlertToast())
	}
	return cmd
}

// windowKey handles the key after the ctrl+w prefix: h/j/k/l or arrows move the focus
// by pane geometry, w goes to the next pane, 1..5 pin a mode, 0 returns to auto; any
// other key cancels the prefix and is dropped.
func (m *Model) windowKey(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := m.toolWindowKey(msg); ok { // ctrl+w z: tools.zoom (tools.go)
		return cmd
	}
	s := msg.String()
	switch s {
	case "h", "left":
		m.moveFocus(-1, 0)
	case "l", "right":
		m.moveFocus(+1, 0)
	case "k", "up":
		m.moveFocus(0, -1)
	case "j", "down":
		m.moveFocus(0, +1)
	case "w":
		m.nextPane(+1)
	case "0":
		return m.setPin(nil)
	default:
		if mode, ok := pinKeys[s]; ok {
			return m.setPin(&mode)
		}
	}
	return nil
}

// grabbing reports whether c owns the keyboard (ui.KeyGrabber).
func grabbing(c ui.Component) bool {
	g, ok := c.(ui.KeyGrabber)
	return ok && g.GrabsKeys()
}

// grabbedKey gives msg to the focused component when it grabs the keyboard
// and nothing (an overlay, the login screen) is above it — every key but
// tools.toggle (ctrl+t, in any Korean-layout spelling) while a tool flag is
// on: that one is the way out of a panel (0.1.3 patch: inside lazygit or nvim
// esc is theirs, and ctrl+\ is hard to type on a Korean layout). It goes back
// to the chat and the program keeps running; showing the panel again (the
// hub's enter) gives it the keyboard again. The program never gets ctrl+t.
func (m *Model) grabbedKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if len(m.overlays) > 0 || m.login != nil {
		return nil, false
	}
	comp := m.focusedComponent()
	if !grabbing(comp) {
		return nil, false
	}
	m.prefix = prefixNone
	if toolsOn() && m.ctx.Keys.MatchesToolsToggle(msg) {
		return m.toggleTools(), true
	}
	_, cmd := m.handleKey(comp, msg)
	return cmd, true
}

// escapeKey is the key after ctrl+\ while a kill switch was active: x cuts
// every active one at once (no question: failing safe, B8.5); any other key
// cancels and is dropped.
func (m *Model) escapeKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "x" && ui.KillSwitchActive() {
		return ui.KillAll(m.ctx)
	}
	return nil
}

// grabMsg handles the msgs of key grabbing the shell consumes: the ctrl+\
// timeout and the shell actions a grabbing component asks for (its keys go to
// its program, so it asks with ui.ShellActionMsg).
func (m *Model) grabMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case escapeArmMsg:
		if msg.gen == m.escGen && m.prefix == prefixEscape {
			m.prefix = prefixNone
		}
		return nil, true
	case ui.ShellActionMsg:
		switch msg.Action {
		case ui.ShellFocusLeft:
			m.moveFocus(-1, 0)
		case ui.ShellFocusRight:
			m.moveFocus(+1, 0)
		case ui.ShellFocusUp:
			m.moveFocus(0, -1)
		case ui.ShellFocusDown:
			m.moveFocus(0, +1)
		case ui.ShellPalette:
			return m.openPalette(""), true
		}
		return nil, true
	}
	return nil, false
}

// quitKey is app.quit (CA 5.3.4 2.1, 5.5): a focused component capturing input gets it
// first (a non-empty input clears itself and handles it — Task 22 rule); otherwise the
// first press arms quitting for quitWindow (toast), the second one quits. With an
// overlay open the key skips the overlay and the panes below and arms directly.
func (m *Model) quitKey(msg tea.KeyPressMsg) tea.Cmd {
	var pre tea.Cmd
	if comp := m.focusedComponent(); len(m.overlays) == 0 && capturing(comp) {
		handled, cmd := m.handleKey(comp, msg)
		if handled {
			return cmd
		}
		pre = cmd
	}
	if m.quitArmed {
		return tea.Batch(pre, m.requestQuit(false)) // quit.go: live panes are asked about first
	}
	m.quitArmed = true
	m.quitGen++
	return tea.Batch(pre,
		m.toasts.push(m.ctx, ui.ToastMsg{Text: quitToastText, Kind: ui.ToastInfo}),
		ui.After(m.ctx, quitWindow, quitArmMsg{gen: m.quitGen}))
}

// openPalette opens the command palette (palette.open) unless the app already has one
// open; the overlay stack holds at most maxOverlays.
func (m *Model) openPalette(initial string) tea.Cmd {
	if m.palette != nil || len(m.overlays) >= maxOverlays {
		return nil
	}
	o := m.openPal(m.ctx, initial)
	cmd, ok := m.pushOverlay(o)
	if ok {
		m.palette = o
	}
	return cmd
}

// maxOverlays is the overlay stack limit (CA 5.3.7).
const maxOverlays = 3

// applyNavigate applies a NavigateMsg (CA 5.3.3) and relayouts.
func (m *Model) applyNavigate(msg ui.NavigateMsg) tea.Cmd {
	switch {
	case msg.Route.Kind == ui.RouteLogin: // the login screen only (outside the shell)
		r := msg.Route
		m.login = &r
		m.nav, m.intent = newNavState(), FocusIntent{Slot: layout.SlotMain}
		return m.relayout()
	case m.creds == nil: // logged out: nothing but the login screen
		return nil
	case m.login != nil: // a forced login screen is left by navigating
		m.login = nil
	}
	if isToolKind(msg.Route.Kind) { // the tool surface (tools.go)
		return m.navigateTool(msg)
	}
	n, intent := m.nav.navigate(msg, m.lay.Mode, m.isDM)
	m.nav = m.keepToolColumn(n)
	if intent.Slot != "" {
		m.intent = intent
	}
	if msg.Route.Anchor != "" { // asked to jump again even if the instance holds this Anchor (C1)
		m.cache.rejump = msg.Route.Key()
	}
	return m.relayout()
}

// isDM reports a DM or group DM channel the Store knows (NavState places it on the
// DM surface).
func (m *Model) isDM(channelID string) bool { return m.ctx.Store.IsDM(channelID) }

// dmKept reports whether the DM surface may keep channel id open: the Store knows it
// (or has not bootstrapped yet: a DM restored from state.json) and it is not hidden
// (Store.HiddenDM).
func (m *Model) dmKept(id string) bool {
	st := m.ctx.Store
	ch := st.Channels[id]
	if ch == nil {
		return !st.Sync.Bootstrapped
	}
	return !st.HiddenDM(ch)
}

// applyToggle installs the result of toggleFeed/toggleNotifications.
func (m *Model) applyToggle(n NavState, intent *FocusIntent) tea.Cmd {
	m.nav = n
	if intent != nil {
		m.intent = *intent
	}
	return m.relayout()
}

// switchTab is tab.next/tab.prev (delta +1/-1). When Main had the focus, the focus
// intent follows what Main shows now.
func (m *Model) switchTab(delta int) tea.Cmd {
	wasMain := m.focus.Slot == layout.SlotMain
	prev := m.toolSnap()
	m.nav = m.nav.cycleTab(delta, m.lay.Mode)
	cmd := m.relayout()
	if wasMain {
		m.followMain()
	}
	return tea.Batch(cmd, m.toolSwitched(prev))
}

// goBack is nav.back / ui.BackMsg: one step back on the surface of the focused slot.
func (m *Model) goBack() tea.Cmd {
	m.nav = m.nav.back(m.focus.Slot, m.lay.Mode, m.lay)
	return m.relayout()
}

// setPin pins the layout mode (nil = auto) at runtime (saving it is Task 32).
func (m *Model) setPin(mode *layout.Mode) tea.Cmd {
	if mode != nil && *mode == layout.ModeTooSmall {
		mode = nil
	}
	m.pin = mode
	return m.relayout()
}

// openAlertToast is the soft-global enter on a visible ToastAlert with a route (CA
// 9.1): the newest such toast is dismissed and its route opened.
func (m *Model) openAlertToast() tea.Cmd {
	if m.lay.Mode == layout.ModeTooSmall {
		return nil
	}
	for _, it := range m.toasts.newestFirst() {
		if it.msg.Kind == ui.ToastAlert && it.msg.Route != nil {
			m.toasts.remove(it.id)
			return m.openToastTarget(it)
		}
	}
	return nil
}
