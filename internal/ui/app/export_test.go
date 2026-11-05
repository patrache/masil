package app

import "github.com/patrache/masil/internal/ui"

// SetRealtimeForTest replaces the Context's realtime (status-line connection tests).
func SetRealtimeForTest(m *Model, rt ui.Realtime) {
	m.ctx.RT = rt
}
