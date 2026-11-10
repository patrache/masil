package app

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/toolstate"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// The tool surface (v0.1.3 spec A8, CA 5.3.3 "도구 표면", P0 Task 18): the hub
// and the open tool instances live in NavState.Tools, apart from the chat tabs
// and the feed stack (one key, one place). It is in front when Front ==
// FrontTools. Every entry point (ctrl+t, tools.* commands, the tab) exists only
// while a tool feature flag is on (features.Any): with no tool open the shell
// is the 0.1.2 shell byte for byte.

// maxTools is the tool-surface limit, the hub included (A8.4). One place is
// always kept for the hub: at most maxTools-1 instances, so the hub (where
// the limit toast sends the user to close one) can always be opened.
const maxTools = 6

// widgetTools is the widget/tiny tab index of the tool tab (after 채팅/DM/피드/알림).
const widgetTools = 4

// hubRoute is the single hub route (key "tools//").
var hubRoute = ui.Route{Kind: ui.RouteTools}

// toolSlots is where the tool surface shows, by mode (A8.3): command gives it
// its own slot beside the chat column; a mode not listed shows it in Main.
var toolSlots = map[layout.Mode]layout.SlotID{
	layout.ModeCommand: layout.SlotTool,
}

// toolSlotFor is the slot the tool surface shows in for mode.
func toolSlotFor(mode layout.Mode) layout.SlotID {
	if s, ok := toolSlots[mode]; ok {
		return s
	}
	return layout.SlotMain
}

// isToolKind reports the tool-surface route kinds (they never go on a chat or
// feed stack).
func isToolKind(k ui.RouteKind) bool { return k == ui.RouteTools || k == ui.RouteTool }

// cloneTools copies n's tool surface into c (clone), repairing it: ToolActive in
// range, and FrontTools only while a tool is open.
func (c *NavState) cloneTools(n NavState) {
	c.Tools = slices.Clone(n.Tools)
	c.ToolZoom = maps.Clone(n.ToolZoom)
	c.ToolActive = n.ToolActive
	if c.ToolActive < 0 || c.ToolActive >= len(c.Tools) {
		c.ToolActive = 0
	}
	if len(c.Tools) == 0 && c.Front == FrontTools {
		c.Front = FrontTab
	}
	c.ToolLast = n.ToolLast
	if i := c.toolIndex(c.ToolLast); i < 0 || c.Tools[i].Kind != ui.RouteTool {
		c.ToolLast = ""
	}
}

// activeTool is the tool-surface entry shown when it is in front.
func (n NavState) activeTool() (ui.Route, bool) {
	if n.ToolActive < 0 || n.ToolActive >= len(n.Tools) {
		return ui.Route{}, false
	}
	return n.Tools[n.ToolActive], true
}

// zoomed reports the active entry's wide view: remembered per instance
// (ToolZoom), else its ToolSpec.WideDefault (the hub: off).
func (n NavState) zoomed() bool {
	r, ok := n.activeTool()
	if !ok {
		return false
	}
	if z, ok := n.ToolZoom[r.Key()]; ok {
		return z
	}
	if r.Kind != ui.RouteTool {
		return false
	}
	s, ok := ui.ToolByID(r.Arg)
	return ok && s.WideDefault
}

// toolTabs is how many tool tabs the tab bars add: one while a tool is open.
func (n NavState) toolTabs() int {
	if len(n.Tools) > 0 {
		return 1
	}
	return 0
}

// instances is how many tool instances (not the hub) are open.
func (n NavState) instances() int {
	c := 0
	for _, r := range n.Tools {
		if r.Kind == ui.RouteTool {
			c++
		}
	}
	return c
}

// idTaken reports whether r's instance id is already open under another tool.
// An instance id names one instance on the whole surface (ui.CloseTool carries
// only the id), so such an r is refused.
func (n NavState) idTaken(r ui.Route) bool {
	return slices.ContainsFunc(n.Tools, func(o ui.Route) bool {
		return o.Kind == ui.RouteTool && o.ID == r.ID && o.Arg != r.Arg
	})
}

// toolIndex is the index of the entry with key k, or -1.
func (n NavState) toolIndex(k string) int {
	return slices.IndexFunc(n.Tools, func(r ui.Route) bool { return r.Key() == k })
}

// openTool shows r (RouteTools or RouteTool) on the tool surface: an open entry
// with that key is shown again (its Anchor/Query updated), a new one is added —
// the hub first, an instance last — unless the surface is full (then c is
// unchanged and the intent empty): maxTools-1 instances, the hub's place kept.
func (c NavState) openTool(r ui.Route, mode layout.Mode) (NavState, FocusIntent) {
	k := r.Key()
	i := c.toolIndex(k)
	switch {
	case i >= 0:
		c.Tools[i] = r
	case r.Kind == ui.RouteTool && (c.instances() >= maxTools-1 || c.idTaken(r)):
		return c, FocusIntent{}
	case r.Kind == ui.RouteTools:
		c.Tools = append([]ui.Route{r}, c.Tools...)
		i = 0
	default:
		c.Tools = append(c.Tools, r)
		i = len(c.Tools) - 1
	}
	c.ToolActive, c.Front = i, FrontTools
	return c, FocusIntent{Slot: toolSlotFor(mode), RouteKey: k}
}

// closeToolEntry removes the entry with key k (ok=false when none). The active
// entry stays when another one closes; closing the active one shows the one
// before it; closing the last one leaves the surface (FrontTab).
func (n NavState) closeToolEntry(k string) (NavState, bool) {
	c := n.clone()
	i := c.toolIndex(k)
	if i < 0 {
		return c, false
	}
	c.Tools = slices.Delete(c.Tools, i, i+1)
	delete(c.ToolZoom, k)
	if c.ToolLast == k {
		c.ToolLast = ""
	}
	switch {
	case len(c.Tools) == 0:
		c.ToolActive = 0
		if c.Front == FrontTools {
			c.Front = FrontTab
		}
	case i < c.ToolActive:
		c.ToolActive--
	case i == c.ToolActive:
		c.ToolActive = max(i-1, 0)
	}
	return c, true
}

// toggleTools is tools.toggle (ctrl+t): the tool surface in front goes back to
// the chat; otherwise the hub comes to the front (added before the open
// instances when it is not open), the open instances kept. The instance that
// was active is remembered as ToolLast, so the hub's cursor starts on it and
// ctrl+t enter shows that same instance again (0.1.3 patch: the hub used to
// appear only the first time). When the hub itself was the active entry,
// ToolLast is cleared: the hub keeps the cursor where the user left it.
func (n NavState) toggleTools(mode layout.Mode) (NavState, FocusIntent) {
	c := n.clone()
	if c.Front == FrontTools {
		c.Front = FrontTab
		return c, FocusIntent{Slot: layout.SlotMain, RouteKey: c.shownMain(mode).Key()}
	}
	if r, ok := c.activeTool(); ok && r.Kind == ui.RouteTool {
		c.ToolLast = r.Key()
	} else {
		c.ToolLast = ""
	}
	return c.openTool(hubRoute, mode)
}

// cycleTool shows the next (delta +1) or previous open tool, wrapping.
func (n NavState) cycleTool(delta int, mode layout.Mode) (NavState, FocusIntent) {
	c := n.clone()
	if len(c.Tools) == 0 {
		return c, FocusIntent{}
	}
	size := len(c.Tools)
	c.ToolActive = ((c.ToolActive+delta)%size + size) % size
	c.Front = FrontTools
	r, _ := c.activeTool()
	return c, FocusIntent{Slot: toolSlotFor(mode), RouteKey: r.Key()}
}

// toggleZoom flips the active entry's wide view (remembered per instance).
func (n NavState) toggleZoom() NavState {
	c := n.clone()
	r, ok := c.activeTool()
	if !ok {
		return c
	}
	z := !c.zoomed()
	if c.ToolZoom == nil {
		c.ToolZoom = map[string]bool{}
	}
	c.ToolZoom[r.Key()] = z
	return c
}

// ---- the shell ----

// toolSnap is what ToolSwitchedMsg compares: the entry in front ("" when the
// surface is not) and its wide view.
type toolSnap struct {
	key    string
	zoomed bool
}

func (m *Model) toolSnap() toolSnap {
	r, ok := m.nav.activeTool()
	if !ok || m.nav.Front != FrontTools {
		return toolSnap{}
	}
	return toolSnap{key: r.Key(), zoomed: m.nav.zoomed()}
}

// toolSwitched broadcasts ui.ToolSwitchedMsg when the entry in front or its wide
// view changed since prev (and the surface is in front).
func (m *Model) toolSwitched(prev toolSnap) tea.Cmd { return m.toolSwitchedBy(prev, false) }

// toolSwitchedBy is toolSwitched with the msg's Toggled (tools.toggle made the
// change).
func (m *Model) toolSwitchedBy(prev toolSnap, toggled bool) tea.Cmd {
	now := m.toolSnap()
	if now.key == "" || now == prev {
		return nil
	}
	r, _ := m.nav.activeTool()
	return m.broadcast(ui.ToolSwitchedMsg{Route: r, Zoomed: now.zoomed, Toggled: toggled})
}

// applyTools installs a tool-surface transition: the new state, its focus
// intent (when set), the relayout, then ToolSwitchedMsg.
func (m *Model) applyTools(n NavState, intent FocusIntent) tea.Cmd {
	return m.applyToolsBy(n, intent, false)
}

// applyToolsBy is applyTools with the ToolSwitchedMsg's Toggled.
func (m *Model) applyToolsBy(n NavState, intent FocusIntent, toggled bool) tea.Cmd {
	prev := m.toolSnap()
	m.nav = n
	if intent.Slot != "" {
		m.intent = intent
	}
	cmd := m.relayout()
	return tea.Batch(cmd, m.toolSwitchedBy(prev, toggled))
}

// toolsOn reports whether the tool surface exists in this run (a tool flag is on).
func toolsOn() bool { return features.Any() }

// toolRouteOK reports whether r can be shown: the hub while any tool flag is on,
// an instance while its tool is registered and flagged on.
func toolRouteOK(r ui.Route) bool {
	if !toolsOn() {
		return false
	}
	if r.Kind == ui.RouteTool {
		_, ok := ui.ToolByID(r.Arg)
		return ok
	}
	return true
}

// navigateTool is applyNavigate for a tool-surface route: dropped while its
// tool is off or its instance id is open under another tool (idTaken);
// refused with a toast when the surface is full.
func (m *Model) navigateTool(msg ui.NavigateMsg) tea.Cmd {
	r := msg.Route
	if !toolRouteOK(r) {
		return nil
	}
	if r.Kind == ui.RouteTool && m.nav.idTaken(r) {
		slog.Warn("tool surface: instance id already open under another tool", "tool", r.Arg, "id", r.ID)
		return nil
	}
	isNew := m.nav.toolIndex(r.Key()) < 0
	n, intent := m.nav.navigate(msg, m.lay.Mode, m.isDM)
	if n.toolIndex(r.Key()) < 0 {
		return m.toasts.push(m.ctx, ui.ToastMsg{Text: m.ctx.T.Tools.LimitToast(maxTools), Kind: ui.ToastInfo})
	}
	if isNew {
		n = m.rememberedZoom(n, r)
	}
	if r.Anchor != "" {
		m.cache.rejump = r.Key()
	}
	return m.applyTools(n, intent)
}

// keepToolColumn keeps the tool column of command mode in front when a chat
// navigation (a channel, a tab switch) lands on the chat surface: there the
// chat column is beside the tool, so opening a channel must not close the tool
// (A8.6). A zoomed tool covers the chat column, so it steps aside instead.
func (m *Model) keepToolColumn(n NavState) NavState {
	if m.lay.Mode == layout.ModeCommand && m.nav.Front == FrontTools && !m.nav.zoomed() &&
		n.Front == FrontTab && len(n.Tools) > 0 {
		n.Front = FrontTools
	}
	return n
}

// toolMsg handles the tool-surface msgs the shell consumes (never broadcast).
func (m *Model) toolMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ui.CloseToolMsg:
		return m.closeTool(msg.InstanceID), true
	case ui.ToolSurfaceMsg:
		if !toolsOn() {
			return nil, true
		}
		switch msg.Action {
		case ui.ToolSurfaceToggle:
			return m.toggleTools(), true
		case ui.ToolSurfaceNext:
			return m.applyTools(m.nav.cycleTool(+1, m.lay.Mode)), true
		case ui.ToolSurfacePrev:
			return m.applyTools(m.nav.cycleTool(-1, m.lay.Mode)), true
		case ui.ToolSurfaceZoom:
			return m.zoomTool(), true
		}
		return nil, true
	}
	return nil, false
}

// toggleTools is tools.toggle.
func (m *Model) toggleTools() tea.Cmd {
	n, intent := m.nav.toggleTools(m.lay.Mode)
	return m.applyToolsBy(n, intent, true)
}

// zoomTool is tools.zoom on the active tool (no-op while the surface is not in
// front). A tool instance's new wide view is remembered for its tool in
// tools-state.json (rememberedZoom); the hub's is not.
func (m *Model) zoomTool() tea.Cmd {
	if m.nav.Front != FrontTools {
		return nil
	}
	n := m.nav.toggleZoom()
	if r, ok := n.activeTool(); ok && r.Kind == ui.RouteTool {
		c := m.ctx.ToolState.Common(r.Arg)
		z := n.zoomed()
		c.Zoomed = &z
		if err := m.ctx.ToolState.SetCommon(r.Arg, c); err != nil {
			slog.Warn("tools-state.json: zoom not saved", "tool", r.Arg, "err", err)
		}
	}
	return m.applyTools(n, FocusIntent{})
}

// rememberedZoom starts a newly opened tool instance r with its tool's
// remembered wide view (tools-state.json), when there is one; otherwise the
// instance keeps its ToolSpec.WideDefault.
func (m *Model) rememberedZoom(n NavState, r ui.Route) NavState {
	if r.Kind != ui.RouteTool {
		return n
	}
	z := m.ctx.ToolState.Common(r.Arg).Zoomed
	if z == nil {
		return n
	}
	if _, set := n.ToolZoom[r.Key()]; set {
		return n
	}
	n.ToolZoom = maps.Clone(n.ToolZoom)
	if n.ToolZoom == nil {
		n.ToolZoom = map[string]bool{}
	}
	n.ToolZoom[r.Key()] = *z
	return n
}

// initTools sets the tool fields of m.ctx (New): ToolState from paths'
// tools-state.json (an in-memory file when paths has none) and OpenTools.
func (m *Model) initTools(paths config.Paths) {
	m.ctx.ToolState = toolstate.Load(paths)
	m.ctx.OpenTools = m.openTools
}

// openTools is ui.Context.OpenTools: the open tool instances (the hub left
// out), in surface order.
func (m *Model) openTools() []ui.OpenToolInfo {
	var out []ui.OpenToolInfo
	for i, r := range m.nav.Tools {
		if r.Kind != ui.RouteTool {
			continue
		}
		info := ui.OpenToolInfo{Route: r, Title: m.toolEntryTitle(r), Active: i == m.nav.ToolActive && m.nav.Front == FrontTools, Last: r.Key() == m.nav.ToolLast}
		if in := m.cache.lookup(r.Key()); in != nil {
			if s, ok := in.comp.(ui.ToolStatuser); ok {
				info.Status = textwidth.Sanitize(s.ToolStatus(m.ctx))
			}
		}
		out = append(out, info)
	}
	return out
}

// closeTool is ui.CloseTool: the instance's Closer runs, then it leaves the
// surface and the instance cache. An unknown id is ignored. Instance ids are
// unique on the surface (idTaken), so the id names one instance.
func (m *Model) closeTool(instanceID string) tea.Cmd {
	i := slices.IndexFunc(m.nav.Tools, func(r ui.Route) bool { return r.Kind == ui.RouteTool && r.ID == instanceID })
	if i < 0 {
		return nil
	}
	k := m.nav.Tools[i].Key()
	var pre tea.Cmd
	if in := m.cache.lookup(k); in != nil {
		if c, ok := in.comp.(ui.Closer); ok {
			pre = c.CloseTool(m.ctx)
		}
	}
	n, _ := m.nav.closeToolEntry(k)
	intent := FocusIntent{}
	if m.focus.RouteKey == k {
		intent = FocusIntent{Slot: layout.SlotMain, RouteKey: n.shownMain(m.lay.Mode).Key()}
		if r, ok := n.activeTool(); ok && n.Front == FrontTools {
			intent = FocusIntent{Slot: toolSlotFor(m.lay.Mode), RouteKey: r.Key()}
		}
	}
	m.cache.drop(k)
	return tea.Batch(pre, m.applyTools(n, intent))
}

// drop releases the instance with key k at once (a closed tool), without
// keeping its Persistent state.
func (c *instanceCache) drop(k string) {
	if _, ok := c.byKey[k]; !ok {
		return
	}
	delete(c.byKey, k)
	delete(c.saved, k)
	c.order = slices.DeleteFunc(c.order, func(o string) bool { return o == k })
}

// toolKey is the hard global ctrl+t (tools.toggle) while a tool flag is on —
// ctrl+ㅅ and BaseCode 't' too (keymap.MatchesToolsToggle), so a Korean
// layout reaches it from any text field.
func (m *Model) toolKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !toolsOn() || !m.ctx.Keys.MatchesToolsToggle(msg) {
		return nil, false
	}
	return m.toggleTools(), true
}

// toolWindowKey is tools.zoom after the ctrl+w prefix (handled=false: not it).
func (m *Model) toolWindowKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !toolsOn() || m.nav.Front != FrontTools || !m.ctx.Keys.Matches(msg, keymap.ToolsZoom) {
		return nil, false
	}
	return m.zoomTool(), true
}

// clickContext is a click on the widget context line's ▾: on the tool surface
// with more than one entry open it shows the next one (A8.3 "▾ 로 열린 도구
// 사이를 전환"); otherwise it opens the palette, as in 0.1.2.
func (m *Model) clickContext() tea.Cmd {
	if m.nav.Front == FrontTools && len(m.nav.Tools) > 1 {
		return m.applyTools(m.nav.cycleTool(+1, m.lay.Mode))
	}
	return m.openPalette("")
}

// onToolSurface reports whether the focus is on the tool surface in a mode
// where [ / ] walk the open tools (widget and tiny keep their tab walk: the
// tool tab is one of their fixed tabs; widget walks the tools with the context
// line's ▾ — clickContext — and both with the palette's tools.next/prev).
func (m *Model) onToolSurface() bool {
	if m.nav.Front != FrontTools || m.lay.Mode == layout.ModeWidget || m.lay.Mode == layout.ModeTiny {
		return false
	}
	r, ok := m.routes[m.focus.Slot]
	return ok && isToolKind(r.Kind)
}

// toolTitle is the default title of a tool-surface route: the tool's icon and
// name, the hub's catalog title.
func (m *Model) toolTitle(r ui.Route) string {
	if r.Kind == ui.RouteTool {
		if s, ok := ui.ToolByID(r.Arg); ok && s.Name != nil {
			return s.Icon + " " + s.Name(m.ctx.T)
		}
	}
	return m.ctx.T.Tools.TabLabel()
}

// toolContext is the widget context line's text on the tool surface: the active
// entry's title (its Titled title, else toolTitle), with the tool's icon.
func (m *Model) toolContext() string {
	r, ok := m.nav.activeTool()
	if !ok {
		return ""
	}
	return m.toolEntryTitle(r)
}

// toolEntryTitle is a tool-surface entry's title: its Titled title with the
// tool's icon, else toolTitle — sanitized.
func (m *Model) toolEntryTitle(r ui.Route) string {
	title := m.toolTitle(r)
	if in := m.cache.lookup(r.Key()); in != nil {
		if t, ok := in.comp.(ui.Titled); ok {
			if s, _ := t.Title(m.ctx); s != "" {
				title = s
				if sp, ok := ui.ToolByID(r.Arg); ok && r.Kind == ui.RouteTool && sp.Icon != "" {
					title = sp.Icon + " " + s
				}
			}
		}
	}
	return textwidth.Sanitize(title)
}

// withToolHelp appends the help's 도구 group (tools.toggle, tools.zoom) while a
// tool flag is on (off: the rows are the 0.1.2 help, unchanged), keeping the
// row ids sequential and the key column aligned over every row.
func withToolHelp(ctx *ui.Context, rows []helpRow) []helpRow {
	if !toolsOn() {
		return rows
	}
	km := ctx.Keys
	toggle := km.Binding(keymap.ToolsToggle).Help()
	zoom := km.Binding(keymap.ToolsZoom)
	zoomKey := km.Binding(keymap.WindowPrefix).Help().Key + " " + strings.Join(zoom.Keys(), "/")
	out := append(slices.Clone(rows),
		helpRow{kind: helpBlank},
		helpRow{kind: helpHeader, text: ctx.T.Tools.HelpGroup()},
		helpRow{kind: helpKey, key: toggle.Key, text: toggle.Desc},
		helpRow{kind: helpKey, key: zoomKey, text: zoom.Help().Desc},
	)
	kw := 0
	for i := range out {
		out[i].id = fmt.Sprint(i + 1)
		if w := textwidth.Width(textwidth.Sanitize(out[i].key)); out[i].kind == helpKey && w <= helpKeyColMax {
			kw = max(kw, w)
		}
	}
	for i := range out {
		out[i].keyW = kw
	}
	return out
}
