package ui

import (
	"fmt"
	"maps"
	"sync"

	"github.com/patrache/masil/internal/ui/layout"
)

// PaneFactory builds a pane-slot component (rail, sidebar, members).
type PaneFactory func(ctx *Context) Component

// RouteFactory builds a route instance.
type RouteFactory func(ctx *Context, r Route) Component

// Registry is a snapshot of every registration.
type Registry struct {
	Panes    map[layout.SlotID]PaneFactory
	Routes   map[RouteKind]RouteFactory
	Overlays map[string]func(*Context, string) Overlay
	// AlertTargets opens alert targets core does not know (RegisterAlertTarget).
	AlertTargets map[string]AlertTargetFunc
	// Cards draws kind "card" messages by card type (RegisterCard).
	Cards map[string]CardRenderer
}

// OverlayFocusSummary is opened by core (ui/app) but registered by sns-fun ui/fun; args = alert id (CA 9.1).
const OverlayFocusSummary = "fun.focus_summary"

var (
	regMu sync.Mutex
	reg   = emptyRegistry()
)

func emptyRegistry() Registry {
	return Registry{
		Panes:    map[layout.SlotID]PaneFactory{},
		Routes:   map[RouteKind]RouteFactory{},
		Overlays: map[string]func(*Context, string) Overlay{},

		AlertTargets: map[string]AlertTargetFunc{},
		Cards:        map[string]CardRenderer{},
	}
}

func (r Registry) clone() Registry {
	return Registry{
		Panes:    maps.Clone(r.Panes),
		Routes:   maps.Clone(r.Routes),
		Overlays: maps.Clone(r.Overlays),

		AlertTargets: maps.Clone(r.AlertTargets),
		Cards:        maps.Clone(r.Cards),
	}
}

// RegisterPane registers a pane factory. ONLY rail, sidebar, members (chat);
// route slots (main, thread, feed, notifications) and core slots => panic. Duplicate => panic.
func RegisterPane(slot layout.SlotID, f PaneFactory) {
	switch slot {
	case layout.SlotRail, layout.SlotSidebar, layout.SlotMembers:
	default:
		panic(fmt.Sprintf("ui: RegisterPane: slot %q is not a pane-factory slot (rail, sidebar, members)", slot))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg.Panes[slot]; dup {
		panic(fmt.Sprintf("ui: RegisterPane: duplicate slot %q", slot))
	}
	reg.Panes[slot] = f
}

// RegisterRoute registers a route factory (see RouteKind owners). Duplicate => panic.
func RegisterRoute(kind RouteKind, f RouteFactory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg.Routes[kind]; dup {
		panic(fmt.Sprintf("ui: RegisterRoute: duplicate route kind %q", kind))
	}
	reg.Routes[kind] = f
}

// RegisterOverlay registers an overlay factory; id has the plan prefix ("fun.", "chat.", ...). Duplicate => panic.
func RegisterOverlay(id string, f func(ctx *Context, args string) Overlay) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg.Overlays[id]; dup {
		panic(fmt.Sprintf("ui: RegisterOverlay: duplicate overlay id %q", id))
	}
	reg.Overlays[id] = f
}

// Registered returns a copy of the registry (used by app & tests).
func Registered() Registry {
	regMu.Lock()
	defer regMu.Unlock()
	return reg.clone()
}

// ResetRegistryForTest empties the registry. Tests only; call
// t.Cleanup(ui.SnapshotRegistryForTest()) first so init()-time registrations come back.
func ResetRegistryForTest() {
	regMu.Lock()
	defer regMu.Unlock()
	reg = emptyRegistry()
}

// SnapshotRegistryForTest deep-copies the current registry and returns a func
// that restores it. Tests only.
func SnapshotRegistryForTest() (restore func()) {
	regMu.Lock()
	saved := reg.clone()
	regMu.Unlock()
	return func() {
		regMu.Lock()
		defer regMu.Unlock()
		reg = saved.clone()
	}
}
