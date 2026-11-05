package app

import (
	"context"
	"net/http"

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
