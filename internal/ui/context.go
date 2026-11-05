package ui

import (
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// Context is shared by every component.
type Context struct {
	Theme  *theme.Theme
	Mode   layout.Mode
	Layout layout.Layout
	Keys   *keymap.Keymap
	Store  *store.Store
	API    *api.Client
	RT     Realtime // never nil (noop in tests/offline)
	Config *config.Config
	Paths  config.Paths
	Caps   termcaps.Caps
	Now    func() time.Time // fixed clock in tests (uitest.FakeClock.Now)
	Timers Timers           // never nil; RealTimers() in production, uitest.FakeClock in tests
	Loc    *time.Location   // time.Local; Asia/Seoul in tests
	Debug  bool
}

// Realtime is the UI-facing subset of realtime.Client.
type Realtime interface {
	Typing(channelID string, threadRootID *string)
	SetIdle(idle bool)
	Watch(topics []string)
	State() realtime.State
}

// Me returns the logged-in user.
func (c *Context) Me() *api.Me {
	// TODO(core): Task 19
	return nil
}

// Density is derived from Mode.
func (c *Context) Density() Density {
	// TODO(core): Task 19
	return DensityFull
}

// Density is the message rendering density.
type Density int

// Densities.
const (
	DensityFull    Density = iota // command, standard
	DensityLine                   // compact: one-line messages
	DensityBubble                 // widget: KakaoTalk-style bubbles, mine right-aligned
	DensityMinimal                // tiny
)
