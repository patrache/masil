package app_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// ---- stub tools (P0 Task 18) ----

// stubTool is a tool instance: it draws "<tool> WxH" and records CloseTool.
type stubTool struct {
	route   ui.Route
	size    ui.Size
	resizes []ui.Size
	closed  int
	msgs    []tea.Msg
}

func (s *stubTool) Init(*ui.Context) tea.Cmd { return nil }
func (s *stubTool) Update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	s.msgs = append(s.msgs, msg)
	return nil
}
func (s *stubTool) Resize(_ *ui.Context, size ui.Size) {
	s.size = size
	s.resizes = append(s.resizes, size)
}
func (s *stubTool) View(*ui.Context, bool) string {
	lines := []string{fmt.Sprintf("%s %dx%d", s.route.Arg, s.size.W, s.size.H)}
	return strings.Join(textwidth.Fit(lines, s.size.W, s.size.H), "\n")
}
func (s *stubTool) Title(*ui.Context) (string, ui.Badge) { return "tool " + s.route.Arg, ui.Badge{} }
func (s *stubTool) CloseTool(*ui.Context) tea.Cmd {
	s.closed++
	return nil
}

var _ ui.Closer = (*stubTool)(nil)

// stubHub stands in for the ui/tools hub (RouteTools) in the app tests.
type stubHub struct{ size ui.Size }

func (s *stubHub) Init(*ui.Context) tea.Cmd            { return nil }
func (s *stubHub) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (s *stubHub) Resize(_ *ui.Context, size ui.Size)  { s.size = size }
func (s *stubHub) View(*ui.Context, bool) string {
	return strings.Join(textwidth.Fit([]string{"stub hub"}, s.size.W, s.size.H), "\n")
}

// installStubTools registers the five tools (WideDefault as the spec: workspace,
// arch, api wide) and the RouteTools/RouteTool factories, restored on cleanup.
// flags is MASIL_FEATURES.
func installStubTools(t *testing.T, flags string) {
	t.Helper()
	t.Setenv("MASIL_FEATURES", flags)
	t.Cleanup(ui.SnapshotRegistryForTest())
	reg := ui.Registered()
	if _, ok := reg.Routes[ui.RouteTools]; !ok {
		ui.RegisterRoute(ui.RouteTools, func(*ui.Context, ui.Route) ui.Component { return &stubHub{} })
	}
	if _, ok := reg.Routes[ui.RouteTool]; !ok {
		ui.RegisterRoute(ui.RouteTool, func(ctx *ui.Context, r ui.Route) ui.Component {
			if s, ok := ui.ToolByID(r.Arg); ok && s.New != nil {
				return s.New(ctx, r)
			}
			return nil
		})
	}
	for i, id := range []string{ui.ToolLeet, ui.ToolWorkspace, ui.ToolArch, ui.ToolGit, ui.ToolAPI} {
		if _, ok := reg.Tools[id]; ok {
			continue
		}
		ui.RegisterTool(ui.ToolSpec{
			ID: id, Order: i + 1, Icon: "🧪",
			Name:        func(*i18n.T) string { return id },
			New:         func(_ *ui.Context, r ui.Route) ui.Component { return &stubTool{route: r} },
			Open:        func(*ui.Context, termhost.Mode) tea.Cmd { return ui.OpenTool(id, id+"-1", nil) },
			WideDefault: id == ui.ToolWorkspace || id == ui.ToolArch || id == ui.ToolAPI,
		})
	}
}

func toolRoute(toolID, inst string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: inst, Arg: toolID}
}

func openTool(h *uitest.Harness, toolID, inst string) {
	h.T.Helper()
	h.Send(ui.OpenTool(toolID, inst, nil)())
}

// toolSlot is where the tool surface shows in mode (A8.3).
func toolSlot(mode layout.Mode) layout.SlotID {
	if mode == layout.ModeCommand {
		return layout.SlotTool
	}
	return layout.SlotMain
}

func shownTool(h *uitest.Harness) (ui.Route, bool) {
	f := h.Model.Frame()
	r, ok := f.Routes[toolSlot(f.Layout.Mode)]
	if !ok || (r.Kind != ui.RouteTool && r.Kind != ui.RouteTools) {
		return ui.Route{}, false
	}
	return r, true
}

func paneW(h *uitest.Harness, s layout.SlotID) int {
	p, ok := h.Model.Frame().Layout.Pane(s)
	if !ok {
		return -1
	}
	return p.Rect.W
}

func tabRow(h *uitest.Harness) string {
	p, ok := h.Model.Frame().Layout.Pane(layout.SlotTabs)
	if !ok {
		return ""
	}
	return strings.Split(h.Screen(), "\n")[p.Rect.Y]
}

// ---- tests ----

func TestCtrlTOpensHubEveryMode(t *testing.T) {
	installStubTools(t, "all")
	sizes := append(slices.Clone(uitest.Required), uitest.Size{W: 40, H: 14})
	forEachSize(t, sizes, nil, func(t *testing.T, h *uitest.Harness) {
		h.Press("ctrl+t")
		if n := h.Model.Nav(); n.Front != app.FrontTools || len(n.Tools) != 1 || n.Tools[0].Kind != ui.RouteTools {
			t.Fatalf("ctrl+t: front=%v tools=%v, want the hub on the tool surface", n.Front, n.Tools)
		}
		if r, ok := shownTool(h); !ok || r.Kind != ui.RouteTools {
			t.Fatalf("ctrl+t: tool slot shows %+v, %v", r, ok)
		}
		if got := h.Model.Focus(); got.RouteKey != (ui.Route{Kind: ui.RouteTools}).Key() {
			t.Errorf("focus = %+v, want the hub", got)
		}
		h.Check()
		h.Press("ctrl+t")
		if n := h.Model.Nav(); n.Front != app.FrontTab {
			t.Fatalf("ctrl+t again: front = %v, want the chat", n.Front)
		}
		if r, ok := shownTool(h); ok {
			t.Fatalf("back on the chat a tool is still shown: %+v", r)
		}
		h.Press("ctrl+t")
		if r, ok := shownTool(h); !ok || r.Kind != ui.RouteTools {
			t.Fatalf("third ctrl+t: %+v, %v, want the hub (last tool)", r, ok)
		}
		openTool(h, ui.ToolGit, "g1")
		h.Press("ctrl+t", "ctrl+t")
		if r, ok := shownTool(h); !ok || r.Key() != toolRoute(ui.ToolGit, "g1").Key() {
			t.Fatalf("ctrl+t ctrl+t after opening git: %+v, %v, want the last tool", r, ok)
		}
		h.Check()
	})
}

func TestCommandModeKeepsChatColumn(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	openTool(h, ui.ToolGit, "g1")
	f := h.Model.Frame()
	if r := f.Routes[layout.SlotMain]; r.Key() != devRoute.Key() {
		t.Fatalf("SlotMain = %+v, want the dev channel", r)
	}
	if r := f.Routes[layout.SlotTool]; r.Key() != toolRoute(ui.ToolGit, "g1").Key() {
		t.Fatalf("SlotTool = %+v, want git", r)
	}
	for _, s := range []layout.SlotID{layout.SlotFeed, layout.SlotNotifications, layout.SlotMembers} {
		if _, ok := f.Layout.Pane(s); ok {
			t.Errorf("%s still laid out beside the tool", s)
		}
	}
	if w := paneW(h, layout.SlotTool); w != 90 {
		t.Errorf("SlotTool width = %d, want 90", w)
	}
	chat, ok := h.Model.Instance(devRoute.Key()).(*stub.Chat)
	if !ok {
		t.Fatalf("dev instance = %T", h.Model.Instance(devRoute.Key()))
	}
	got := 0
	chat.OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if _, ok := msg.(ui.EventMsg); ok {
			got++
		}
	}
	h.Event("message.created", map[string]any{"message": fixtures.Messages(fixtures.ChDev)[0]})
	if got != 1 {
		t.Errorf("chat column got %d events, want 1", got)
	}
	if !strings.Contains(h.Screen(), "git 88x57") {
		t.Errorf("tool not drawn at 88x57:\n%s", h.Screen())
	}
	// Opening another channel from the chat column keeps the tool beside it.
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}})
	f = h.Model.Frame()
	if r := f.Routes[layout.SlotTool]; r.Key() != toolRoute(ui.ToolGit, "g1").Key() {
		t.Errorf("after opening #general SlotTool = %+v, want git", r)
	}
	if r := f.Routes[layout.SlotMain]; r.ID != fixtures.ChGeneral {
		t.Errorf("after opening #general SlotMain = %+v", r)
	}
	h.Check()
}

func TestZoomDefaultsAndMemory(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t) // 120x40 standard
	openTool(h, ui.ToolArch, "a1")
	if w := paneW(h, layout.SlotMain); w != 120 {
		t.Fatalf("arch (wide by default): Main = %d, want 120", w)
	}
	if n := h.Model.Nav(); !n.LayoutState(layout.ModeStandard).Zoomed {
		t.Error("arch: LayoutState not zoomed")
	}
	h.Press("ctrl+w", "z")
	if w := paneW(h, layout.SlotMain); w != 94 {
		t.Fatalf("after tools.zoom: Main = %d, want 94", w)
	}
	openTool(h, ui.ToolGit, "g1")
	if w := paneW(h, layout.SlotMain); w != 94 {
		t.Fatalf("git (normal by default): Main = %d, want 94", w)
	}
	h.Press("ctrl+w", "z")
	if w := paneW(h, layout.SlotMain); w != 120 {
		t.Fatalf("git zoomed: Main = %d, want 120", w)
	}
	openTool(h, ui.ToolArch, "a1")
	if w := paneW(h, layout.SlotMain); w != 94 {
		t.Errorf("back on arch: Main = %d, want 94 (its zoom off is remembered)", w)
	}
	openTool(h, ui.ToolGit, "g1")
	if w := paneW(h, layout.SlotMain); w != 120 {
		t.Errorf("back on git: Main = %d, want 120 (remembered)", w)
	}
	// Command: a zoomed tool covers the chat column too.
	h.Resize(200, 60)
	if w := paneW(h, layout.SlotTool); w != 168 {
		t.Errorf("command zoomed: SlotTool = %d, want 168", w)
	}
	if _, ok := h.Model.Frame().Layout.Pane(layout.SlotMain); ok {
		t.Error("command zoomed: Main still laid out")
	}
	h.Check()
}

func TestToolSwitchedBroadcast(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	var got []ui.ToolSwitchedMsg
	app.SetObserverForTest(h.Model, func(msg tea.Msg) {
		if m, ok := msg.(ui.ToolSwitchedMsg); ok {
			got = append(got, m)
		}
	})
	openTool(h, ui.ToolArch, "a1")
	h.Press("ctrl+w", "z")
	openTool(h, ui.ToolArch, "a1") // already active: no switch
	want := []ui.ToolSwitchedMsg{
		{Route: toolRoute(ui.ToolArch, "a1"), Zoomed: true},
		{Route: toolRoute(ui.ToolArch, "a1"), Zoomed: false},
	}
	if len(got) != len(want) {
		t.Fatalf("ToolSwitchedMsg = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Route.Key() != want[i].Route.Key() || got[i].Zoomed != want[i].Zoomed {
			t.Errorf("ToolSwitchedMsg[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWidgetToolTabOnlyWhenOpen(t *testing.T) {
	installStubTools(t, "all")
	for _, tc := range []struct {
		size  uitest.Size
		label string
	}{
		{uitest.Size{W: 80, H: 24}, "도구"},
		{uitest.Size{W: 44, H: 24}, "도구"},
		{uitest.Size{W: 50, H: 20}, "도"},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.size.W, tc.size.H), func(t *testing.T) {
			h := uitest.New(t)
			h.Resize(tc.size.W, tc.size.H)
			before := tabRow(h)
			if before == "" {
				t.Fatal("no tab bar")
			}
			if strings.Contains(before, "도구") || strings.HasSuffix(strings.TrimSpace(before), " 도") {
				t.Fatalf("no tool open but the tab bar has a tool tab: %q", before)
			}
			openTool(h, ui.ToolGit, "g1")
			row := tabRow(h)
			if !strings.Contains(row, tc.label) {
				t.Fatalf("tool open: tab bar %q has no %q", row, tc.label)
			}
			h.Check()
			// tab.next from the tool tab wraps to 채팅.
			h.Press("]")
			if n := h.Model.Nav(); n.Front != app.FrontTab || n.InDM {
				t.Errorf("] from the tool tab: front=%v inDM=%v, want 채팅", n.Front, n.InDM)
			}
			h.Press("[")
			if n := h.Model.Nav(); n.Front != app.FrontTools {
				t.Errorf("[ from 채팅: front=%v, want the tool tab", n.Front)
			}
			h.Send(ui.CloseTool("g1")())
			if row := tabRow(h); row != before {
				t.Errorf("all tools closed: tab bar %q, want %q", row, before)
			}
			h.Check()
		})
	}
}

func TestCompactToolTab(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	h.Resize(120, 30)
	before := tabRow(h)
	openTool(h, ui.ToolGit, "g1")
	row := tabRow(h)
	if !strings.Contains(row, "도구") || strings.Contains(before, "도구") {
		t.Errorf("compact tab bar: before %q after %q", before, row)
	}
	h.Check()
}

func TestModeChangeKeepsInstance(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	h.Resize(200, 60)
	openTool(h, ui.ToolGit, "g1")
	key := toolRoute(ui.ToolGit, "g1").Key()
	comp := h.Model.Instance(key)
	if comp == nil {
		t.Fatal("no instance")
	}
	for _, s := range []uitest.Size{{W: 80, H: 24}, {W: 120, H: 40}, {W: 19, H: 10}, {W: 40, H: 14}} {
		h.Resize(s.W, s.H)
		if got := h.Model.Instance(key); got != comp {
			t.Fatalf("%dx%d: instance %p, want %p", s.W, s.H, got, comp)
		}
	}
	if r, ok := shownTool(h); !ok || r.Key() != key {
		t.Errorf("tool not shown after the mode changes: %+v", r)
	}
	for _, sz := range comp.(*stubTool).resizes {
		if sz.W <= 0 || sz.H <= 0 {
			t.Errorf("zero resize %+v", sz)
		}
	}
}

func TestCloseToolCmd(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	openTool(h, ui.ToolGit, "g1")
	openTool(h, ui.ToolArch, "a1")
	g := h.Model.Instance(toolRoute(ui.ToolGit, "g1").Key()).(*stubTool)
	h.Send(ui.CloseTool("g1")())
	if g.closed != 1 {
		t.Errorf("CloseTool called %d times, want 1", g.closed)
	}
	for _, r := range h.Model.Nav().Tools {
		if r.ID == "g1" {
			t.Errorf("g1 still on the tool surface: %v", h.Model.Nav().Tools)
		}
	}
	if h.Model.Instance(toolRoute(ui.ToolGit, "g1").Key()) != nil {
		t.Error("closed instance still cached")
	}
	if r, ok := shownTool(h); !ok || r.ID != "a1" {
		t.Errorf("after closing g1 shown = %+v", r)
	}
	before := h.Model.Nav()
	h.Send(ui.CloseTool("nope")())
	if after := h.Model.Nav(); len(after.Tools) != len(before.Tools) || after.Front != before.Front {
		t.Errorf("closing an unknown id changed the surface: %+v -> %+v", before, after)
	}
	h.Send(ui.CloseTool("a1")())
	if n := h.Model.Nav(); len(n.Tools) != 0 || n.Front != app.FrontTab {
		t.Errorf("last tool closed: tools=%v front=%v, want empty and the chat", n.Tools, n.Front)
	}
	h.Check()
}

func TestToolSurfaceLimit(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	h.Press("ctrl+t") // the hub counts
	for i := range 6 {
		openTool(h, ui.ToolGit, fmt.Sprintf("g%d", i))
	}
	n := h.Model.Nav()
	if len(n.Tools) != 6 {
		t.Fatalf("tools = %d, want 6 (hub + 5)", len(n.Tools))
	}
	if r := n.Tools[n.ToolActive]; r.ID != "g4" {
		t.Errorf("active = %+v, want g4 (the 7th stays closed)", r)
	}
	if !strings.Contains(h.Screen(), "6개까지") {
		t.Errorf("no limit toast:\n%s", h.Screen())
	}
}

func TestToolsNotSaved(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	openTool(h, ui.ToolGit, "g1")
	if h.Model.Nav().Front != app.FrontTools {
		t.Fatal("not on the tool surface")
	}
	if err := app.WriteStateForTest(h.Model); err != nil {
		t.Fatal(err)
	}
	got := readStateMap(t, h.Context().Paths)
	if got["front"] != "tab" {
		t.Errorf("front = %v, want tab", got["front"])
	}
	for _, tab := range got["open_tabs"].([]any) {
		if k := tab.(map[string]any)["kind"]; k == string(ui.RouteTool) || k == string(ui.RouteTools) {
			t.Errorf("a tool route was saved: %v", tab)
		}
	}
}

func TestToolRoutesNotInHistory(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t, uitest.WithRoute(devRoute))
	openTool(h, ui.ToolGit, "g1")
	openTool(h, ui.ToolArch, "a1")
	h.Press("ctrl+t") // back on the chat
	h.Press("ctrl+o")
	if n := h.Model.Nav(); n.Front == app.FrontTools {
		t.Errorf("history walked back onto the tool surface")
	}
}

func TestEscLeavesToolSurface(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t)
	openTool(h, ui.ToolGit, "g1")
	h.Press("esc")
	if n := h.Model.Nav(); n.Front != app.FrontTab || len(n.Tools) != 1 {
		t.Errorf("esc: front=%v tools=%v, want the chat with the tool kept", n.Front, n.Tools)
	}
}

func TestFeaturesOffShellUnchanged(t *testing.T) {
	installStubTools(t, "")
	for _, s := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}} {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := uitest.New(t, uitest.WithRoute(devRoute))
			h.Resize(s.W, s.H)
			before := h.Screen()
			h.Press("ctrl+t")
			if n := h.Model.Nav(); n.Front != app.FrontTab || len(n.Tools) != 0 {
				t.Errorf("flags off: ctrl+t opened the tool surface: %+v", n)
			}
			if after := h.Screen(); after != before {
				t.Errorf("flags off: ctrl+t changed the screen")
			}
		})
	}
}

// app.Options.InitialRoute = RouteTool starts on the tool surface (P2 masil
// workspace open); with the tool's flag off it starts on the chat as before.
func TestInitialToolRoute(t *testing.T) {
	installStubTools(t, "workspace")
	h := uitest.New(t, uitest.WithRoute(toolRoute(ui.ToolWorkspace, "w1")))
	if r, ok := shownTool(h); !ok || r.ID != "w1" {
		t.Fatalf("start: tool slot = %+v, %v, want the workspace instance", r, ok)
	}
	if got := h.Model.Focus().RouteKey; got != toolRoute(ui.ToolWorkspace, "w1").Key() {
		t.Errorf("start focus = %q", got)
	}
	h2 := uitest.New(t, uitest.WithRoute(toolRoute(ui.ToolGit, "g1"))) // git is off
	if n := h2.Model.Nav(); n.Front != app.FrontTab || len(n.Tools) != 0 {
		t.Errorf("flag off: start nav = %+v, want the chat", n)
	}
}

// ---- P0 Task 19: tools-state.json ----

func TestContextToolStateNeverNil(t *testing.T) {
	h := uitest.New(t)
	ctx := h.Context()
	if ctx.ToolState == nil {
		t.Fatal("harness ctx.ToolState is nil")
	}
	type leet struct {
		Lang string `json:"lang"`
	}
	if err := ctx.ToolState.SetSection(ui.ToolLeet, leet{Lang: "go"}); err != nil {
		t.Fatalf("SetSection: %v", err)
	}
	// A new app over the same folder reads it back.
	m := app.New(app.Options{Paths: ctx.Paths, Config: config.Defaults(), DisableRealtime: true})
	var got leet
	if !m.Context().ToolState.Section(ui.ToolLeet, &got) || got.Lang != "go" {
		t.Fatalf("new app over the same folder: Section = %+v", got)
	}
	// No paths at all (a bare app): still a usable, in-memory file.
	bare := app.New(app.Options{Config: config.Defaults(), DisableRealtime: true})
	if bare.Context().ToolState == nil {
		t.Fatal("app without paths: ToolState is nil")
	}
	if err := bare.Context().ToolState.SetSection(ui.ToolGit, leet{}); err != nil {
		t.Fatalf("app without paths: SetSection: %v", err)
	}
}

func TestZoomRememberedInToolState(t *testing.T) {
	installStubTools(t, "all")
	h := uitest.New(t) // 120x40 standard
	openTool(h, ui.ToolGit, "g1")
	if c := h.Context().ToolState.Common(ui.ToolGit); c.Zoomed != nil {
		t.Fatalf("before tools.zoom: git zoomed = %v, want unset", *c.Zoomed)
	}
	h.Press("ctrl+w", "z")
	if c := h.Context().ToolState.Common(ui.ToolGit); c.Zoomed == nil || !*c.Zoomed {
		t.Fatalf("after tools.zoom: git common = %+v, want zoomed", c)
	}
	// A new git instance starts with the remembered wide view (git's default is normal).
	openTool(h, ui.ToolGit, "g2")
	if w := paneW(h, layout.SlotMain); w != 120 {
		t.Fatalf("new git instance: Main = %d, want 120 (remembered wide view)", w)
	}
	// Another tool keeps its own default.
	openTool(h, ui.ToolLeet, "l1")
	if w := paneW(h, layout.SlotMain); w != 94 {
		t.Fatalf("leet: Main = %d, want 94", w)
	}
	// The hub itself is never remembered.
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	h.Press("ctrl+w", "z")
	if c := h.Context().ToolState.Common(""); c.Zoomed != nil {
		t.Fatal("hub zoom written to tools-state.json")
	}
	// A new harness over a fresh folder starts from the defaults.
	h2 := uitest.New(t)
	h2.Send(ui.OpenTool(ui.ToolGit, "g9", nil)())
	if w := paneW(h2, layout.SlotMain); w != 94 {
		t.Fatalf("fresh folder git: Main = %d, want 94", w)
	}
}
