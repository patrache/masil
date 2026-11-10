package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
)

// The tool registry (v0.1.3 spec A8, CA 5.3.6): each tool package registers
// one ToolSpec from init(); the shell (ui/app) shows its instances on the tool
// surface (RouteTool) and the hub (RouteTools) lists them. A tool whose
// feature flag is off (features.On) is invisible everywhere.

// Tool ids (A8.1) — also their feature flag ids.
const (
	ToolLeet      = "leet"
	ToolWorkspace = "workspace"
	ToolArch      = "arch"
	ToolGit       = "git"
	ToolAPI       = "api"
)

// toolIDs are the only ids RegisterTool accepts (JX3: no other tool kinds).
var toolIDs = []string{ToolLeet, ToolWorkspace, ToolArch, ToolGit, ToolAPI}

// PrefixKey is a key a tool adds after ctrl+\ (help display only — the key
// itself is handled by termpane.Options.OnPrefix or the tool's own pane).
type PrefixKey struct {
	Key  string
	Help func(t *i18n.T) string
}

// ReservedPrefixKeys are the keys after ctrl+\ the shell owns (A7.5). The single
// definition: termpane.NewPrefix and RegisterTool both reject them.
var ReservedPrefixKeys = []string{"esc", "h", "j", "k", "l", "left", "down", "up", "right", "z", "p", "t", "[", "x", "?", "ctrl+\\"}

// ToolSpec is one tool's registration (A8.2).
type ToolSpec struct {
	ID        string
	Order     int // hub order: leet 1, workspace 2, arch 3, git 4, api 5
	Icon      string
	Name      func(t *i18n.T) string
	Available func(ctx *Context) bool                        // false: the hub row stays, Status explains (e.g. "설치 필요 · enter 안내")
	Status    func(ctx *Context) string                      // hub status text (the hub sanitizes and truncates it)
	New       func(ctx *Context, r Route) Component          // a RouteTool instance
	Open      func(ctx *Context, mode termhost.Mode) tea.Cmd // hub enter / digit / m menu: usually OpenTool(...) or termhost.Exec(...)
	// PrefixKeys are the tool's extra ctrl+\ keys (a ReservedPrefixKeys key panics).
	PrefixKeys  []PrefixKey
	WideDefault bool // the wide view starts on: workspace, arch, api = true; leet, git = false
}

// RegisterTool registers a tool (init()-time). An unknown id, a duplicate id or
// Order, or a reserved prefix key panics.
func RegisterTool(s ToolSpec) {
	if !slices.Contains(toolIDs, s.ID) {
		panic(fmt.Sprintf("ui: RegisterTool: unknown tool id %q (want one of %v)", s.ID, toolIDs))
	}
	for _, k := range s.PrefixKeys {
		if slices.Contains(ReservedPrefixKeys, k.Key) {
			panic(fmt.Sprintf("ui: RegisterTool %q: prefix key %q is reserved", s.ID, k.Key))
		}
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg.Tools[s.ID]; dup {
		panic(fmt.Sprintf("ui: RegisterTool: duplicate tool id %q", s.ID))
	}
	for _, o := range reg.Tools {
		if o.Order == s.Order {
			panic(fmt.Sprintf("ui: RegisterTool %q: order %d already used by %q", s.ID, s.Order, o.ID))
		}
	}
	reg.Tools[s.ID] = cloneSpec(s)
}

// Tools returns the registered tools whose feature flag is on, by Order.
func Tools() []ToolSpec {
	regMu.Lock()
	all := cloneTools(reg.Tools)
	regMu.Unlock()
	var out []ToolSpec
	for _, s := range all {
		if features.On(s.ID) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b ToolSpec) int { return cmp.Compare(a.Order, b.Order) })
	return out
}

// ToolByID returns the registered tool id when its feature flag is on.
func ToolByID(id string) (ToolSpec, bool) {
	regMu.Lock()
	s, ok := reg.Tools[id]
	regMu.Unlock()
	if !ok || !features.On(id) {
		return ToolSpec{}, false
	}
	return cloneSpec(s), true
}

// OpenTool shows instance instanceID of tool toolID on the tool surface
// (NavigateMsg{RouteTool}): an instance already open is shown again, not rebuilt.
// An instance id names one instance on the whole surface (CloseTool takes only
// the id): one already open under another tool is refused, so tools start
// their ids with "<toolID>:" (e.g. "git:"+repo).
func OpenTool(toolID, instanceID string, query map[string]string) tea.Cmd {
	r := Route{Kind: RouteTool, ID: instanceID, Arg: toolID, Query: maps.Clone(query)}
	return func() tea.Msg { return NavigateMsg{Route: r} }
}

// CloseToolMsg asks the shell to close a tool instance (CloseTool).
type CloseToolMsg struct{ InstanceID string }

// CloseTool closes tool instance instanceID: its Closer.CloseTool (when it has
// one) runs, then it leaves the tool surface. An unknown id is ignored. A tool
// closing itself (lazygit x) uses this too.
func CloseTool(instanceID string) tea.Cmd {
	return func() tea.Msg { return CloseToolMsg{InstanceID: instanceID} }
}

// Closer is an optional tool-instance capability: cleanup when the instance is
// closed from the hub (x) or by CloseTool.
type Closer interface{ CloseTool(ctx *Context) tea.Cmd }

// ToolStatuser is an optional tool-instance capability: its status in the hub's
// open list (A8.2, e.g. "패널 · 실행 중" — how it is open and what it is doing).
type ToolStatuser interface{ ToolStatus(ctx *Context) string }

// OpenToolInfo is one open tool instance (Context.OpenTools).
type OpenToolInfo struct {
	Route  Route  // its RouteTool route
	Title  string // its tab title with the tool's icon (sanitized)
	Status string // ToolStatuser.ToolStatus ("" when it has none)
	Active bool   // it is the tool surface's active entry and the surface is in front
	Last   bool   // it was active when ctrl+t last brought the hub forward (the hub's cursor starts on it)
}

// ToolSwitchedMsg is broadcast when the tool surface's active instance, or its
// wide view, changes while the surface is in front.
type ToolSwitchedMsg struct {
	Route  Route
	Zoomed bool
}

// ToolSurfaceAction is a tool-surface command of the shell.
type ToolSurfaceAction int

// Tool-surface actions (palette tools.toggle / tools.next / tools.prev / tools.zoom).
const (
	ToolSurfaceToggle ToolSurfaceAction = iota + 1 // ctrl+t: the hub <-> chat (the open tools kept)
	ToolSurfaceNext                                // the next open tool
	ToolSurfacePrev                                // the previous open tool
	ToolSurfaceZoom                                // the active instance's wide view on/off
)

// ToolSurfaceMsg runs a tool-surface action in the shell (the app consumes it).
type ToolSurfaceMsg struct{ Action ToolSurfaceAction }

func cloneSpec(s ToolSpec) ToolSpec {
	s.PrefixKeys = slices.Clone(s.PrefixKeys)
	return s
}

func cloneTools(m map[string]ToolSpec) map[string]ToolSpec {
	out := make(map[string]ToolSpec, len(m))
	for k, v := range m {
		out[k] = cloneSpec(v)
	}
	return out
}
