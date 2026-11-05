package app

import (
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
)

// noopRealtime is the ui.Realtime used when realtime is disabled (tests/offline):
// every call is ignored and State reports the fixed state (PhaseLive when disabled,
// so the status line reads the presence, CA 10.1).
type noopRealtime struct{ state realtime.State }

var _ ui.Realtime = noopRealtime{}

func (noopRealtime) Typing(string, *string)  {}
func (noopRealtime) SetIdle(bool)            {}
func (noopRealtime) Watch([]string)          {}
func (n noopRealtime) State() realtime.State { return n.state }
