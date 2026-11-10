package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
)

// SetLogoutTimeoutForTest shortens the POST /v1/auth/logout timeout for tb.
func SetLogoutTimeoutForTest(tb testing.TB, d time.Duration) {
	prev := logoutTimeout
	logoutTimeout = d
	tb.Cleanup(func() { logoutTimeout = prev })
}

// SetRealtimeForTest replaces the Context's realtime (status-line connection tests).
func SetRealtimeForTest(m *Model, rt ui.Realtime) {
	m.ctx.RT = rt
}

// SetPaletteOpenerForTest replaces palette.Open for m (palette.open routing tests).
func SetPaletteOpenerForTest(m *Model, f func(ctx *ui.Context, initial string) ui.Overlay) {
	m.openPal = f
}

// OverlaysForTest returns m's open overlays, bottom -> top.
func OverlaysForTest(m *Model) []ui.Overlay {
	return append([]ui.Overlay(nil), m.overlays...)
}

// SetDialForTest replaces the realtime dialer used by startRealtime.
func SetDialForTest(m *Model, dial func(ctx context.Context, url string, h http.Header) (realtime.Conn, error)) {
	m.rtDial = dial
}

// SetSenderForTest sets the tea.Program.Send stand-in the realtime bridge and the
// API hooks deliver msgs through.
func SetSenderForTest(m *Model, send func(tea.Msg)) {
	m.send = send
}

// SetObserverForTest records every msg the app broadcasts (called once per broadcast,
// before the components).
func SetObserverForTest(m *Model, f func(tea.Msg)) {
	m.observe = f
}

// WriteStateForTest writes m's state.json (what Run does on exit).
func WriteStateForTest(m *Model) error {
	return writeState(m.ctx.Paths, m.savedState())
}

// RunProgramForTest is Run with extra tea.ProgramOptions (input, output, size).
func RunProgramForTest(ctx context.Context, o Options, opts ...tea.ProgramOption) error {
	return runProgram(ctx, New(o), opts...)
}

// ConfirmLogoutForTest delivers the confirmed "logout" to m and returns the
// POST /v1/auth/logout Cmd without running it, so a test can deliver other msgs
// while the request is in flight.
func ConfirmLogoutForTest(m *Model) tea.Cmd {
	_, cmd := m.Update(logoutConfirmedMsg{})
	return cmd
}

// SetPanesForTest replaces termhost.Running / termhost.CloseAll for the quit
// confirmation (P0 Task 20).
func SetPanesForTest(running func() []string, closeAll func(grace time.Duration)) (restore func()) {
	oldR, oldC := runningPanes, closeAllPanes
	runningPanes, closeAllPanes = running, closeAll
	return func() { runningPanes, closeAllPanes = oldR, oldC }
}

// OptionsFromEnvForTest is Run's reading of the process env (MASIL_PANE).
func OptionsFromEnvForTest(o Options) Options { return optionsFromEnv(o) }

// PaneListForTest is the quit question's program list.
func PaneListForTest(m *Model, names []string) string { return paneList(m, names) }

// TabBarForTest is the top tab bar's labels (the [ / ] walk) and current entry.
func TabBarForTest(m *Model) (labels []string, active int) {
	items, active := m.walkItems()
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	return labels, active
}

// TabHitForTest maps a column of the last drawn tab bar to its entry.
func TabHitForTest(m *Model, x int) (int, bool) { return m.tabs.Hit(x) }
