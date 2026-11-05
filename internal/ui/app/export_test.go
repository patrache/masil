package app

import "github.com/patrache/masil/internal/ui"

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
