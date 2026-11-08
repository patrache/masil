package app

import (
	"maps"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// maxInstances is the route-instance cache size (CA 5.3.3): beyond it the least
// recently seen instances are released (Persistent state kept), except pinned keys.
const maxInstances = 32

// placeholderText is drawn for unregistered slots and routes (CA 5.3.6).
const placeholderText = "준비 중"

// instance is one live component: a route instance (keyed by Route.Key()) or a
// pane-factory slot component.
type instance struct {
	route    ui.Route // last route it was shown for (Anchor/Query included)
	comp     ui.Component
	lastSeen uint64  // cache tick of the last frame it was projected
	size     ui.Size // last Resize size
	sized    bool
	mode     layout.Mode // mode of the last Resize
}

// instanceCache holds route instances by Route.Key() (32 LRU, NavState.Keys pinned)
// and the saved Persistent state of released ones.
type instanceCache struct {
	byKey map[string]*instance
	order []string       // creation order (deterministic broadcast)
	saved map[string]any // SaveState of released instances, restored on re-creation
	tick  uint64
	// rejump is the key of the route the last navigation asked to jump to (it
	// carries an Anchor): the next relayout sends it RouteUpdatedMsg even when the
	// cached instance already holds that Anchor (C1). relayout clears it.
	rejump string
}

func newInstanceCache() *instanceCache {
	return &instanceCache{byKey: map[string]*instance{}, saved: map[string]any{}}
}

// lookup returns the cached instance for key or nil.
func (c *instanceCache) lookup(key string) *instance {
	return c.byKey[key]
}

// get returns the instance for r, creating it (registered RouteFactory, else a
// placeholder) when missing. A new instance gets its saved state back (Persistent)
// and its Init Cmd is returned. updated reports an existing instance now shown for a
// different Anchor/Query, or for the same Anchor that a navigation asked for again
// (rejump): the caller sends it ui.RouteUpdatedMsg (only it) once the projection is
// complete (CR-C: after Store.SetViewing).
func (c *instanceCache) get(ctx *ui.Context, r ui.Route) (in *instance, cmd tea.Cmd, updated bool) {
	key := r.Key()
	if in := c.byKey[key]; in != nil {
		rejump := c.rejump == key && r.Anchor != ""
		if rejump || in.route.Anchor != r.Anchor || !maps.Equal(in.route.Query, r.Query) {
			in.route = r
			return in, nil, true
		}
		return in, nil, false
	}
	var comp ui.Component
	if f := ui.Registered().Routes[r.Kind]; f != nil {
		comp = f(ctx, r)
	}
	if comp == nil {
		comp = widgets.Placeholder(placeholderText)
	}
	if st, ok := c.saved[key]; ok {
		if p, ok := comp.(ui.Persistent); ok {
			p.LoadState(st)
		}
		delete(c.saved, key)
	}
	in = &instance{route: r, comp: comp}
	c.byKey[key] = in
	c.order = append(c.order, key)
	return in, comp.Init(ctx), false
}

// evict releases the least recently seen instances beyond maxInstances. Keys in
// pinned (NavState.Keys and this frame's projection) are never released.
func (c *instanceCache) evict(pinned map[string]bool) {
	for len(c.byKey) > maxInstances {
		victim := ""
		var oldest uint64
		for _, k := range c.order {
			if pinned[k] {
				continue
			}
			if in := c.byKey[k]; victim == "" || in.lastSeen < oldest {
				victim, oldest = k, in.lastSeen
			}
		}
		if victim == "" {
			return
		}
		if p, ok := c.byKey[victim].comp.(ui.Persistent); ok {
			c.saved[victim] = p.SaveState()
		}
		delete(c.byKey, victim)
		for i, k := range c.order {
			if k == victim {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}
}

// all returns the live route instances in creation order.
func (c *instanceCache) all() []*instance {
	out := make([]*instance, 0, len(c.order))
	for _, k := range c.order {
		out = append(out, c.byKey[k])
	}
	return out
}

// factorySlots are the pane-factory slots, in broadcast order (CA 5.3.6).
var factorySlots = []layout.SlotID{layout.SlotRail, layout.SlotSidebar, layout.SlotMembers}

func isFactorySlot(s layout.SlotID) bool {
	return s == layout.SlotRail || s == layout.SlotSidebar || s == layout.SlotMembers
}

// pane returns the pane-factory component for slot, building it once (registered
// PaneFactory, else a placeholder) and keeping it forever. The Init Cmd of a new
// one is returned.
func (m *Model) pane(slot layout.SlotID) (*instance, tea.Cmd) {
	if in := m.panes[slot]; in != nil {
		return in, nil
	}
	var comp ui.Component
	if f := ui.Registered().Panes[slot]; f != nil {
		comp = f(m.ctx)
	}
	if comp == nil {
		comp = widgets.Placeholder(placeholderText)
	}
	in := &instance{comp: comp}
	m.panes[slot] = in
	return in, comp.Init(m.ctx)
}
