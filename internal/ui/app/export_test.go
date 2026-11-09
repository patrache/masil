package app

import (
	"context"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
)

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
