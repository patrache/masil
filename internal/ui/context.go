package ui

import (
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/clipboard"
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
	Routes map[layout.SlotID]Route // what each route slot shows this frame (app.Project; set with Layout, read-only)
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
	// Clipboard is the system clipboard (ui.Copy); clipboard.System() in
	// production, a clipboard.Recorder in uitest, nil = OSC 52 only.
	Clipboard clipboard.Writer
	Debug     bool
}

// Realtime is the UI-facing subset of realtime.Client.
type Realtime interface {
	Typing(channelID string, threadRootID *string)
	SetIdle(idle bool)
	Watch(topics []string)
	State() realtime.State
}

// Me returns the logged-in user.
// It is Store.Me: nil before login, without a store, or on a nil Context.
func (c *Context) Me() *api.Me {
	if c == nil || c.Store == nil {
		return nil
	}
	return c.Store.Me
}

// Density is derived from Mode.
func (c *Context) Density() Density {
	switch c.Mode {
	case layout.ModeCommand, layout.ModeStandard:
		return DensityFull
	case layout.ModeCompact:
		return DensityLine
	case layout.ModeWidget:
		return DensityBubble
	default: // ModeTiny, ModeTooSmall
		return DensityMinimal
	}
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
