package tools_test

import (
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/tools"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // home/channel stand-ins under the tool surface
	os.Exit(m.Run())
}

// ---- test tools ----

// opened records spec.Open calls ("<tool>|<mode>").
type opened struct {
	mu    sync.Mutex
	calls []string
}

func (o *opened) add(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, s)
}

func (o *opened) list() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.calls)
}

// instance is a test tool instance: a title, a hub status, CloseTool counted.
type instance struct {
	route  ui.Route
	title  string
	size   ui.Size
	closed int
}

func (s *instance) Init(*ui.Context) tea.Cmd            { return nil }
func (s *instance) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (s *instance) Resize(_ *ui.Context, size ui.Size)  { s.size = size }
func (s *instance) View(*ui.Context, bool) string {
	return strings.Join(textwidth.Fit([]string{"instance " + s.route.ID}, s.size.W, s.size.H), "\n")
}
func (s *instance) Title(*ui.Context) (string, ui.Badge) { return s.title, ui.Badge{} }
func (s *instance) ToolStatus(*ui.Context) string        { return "패널 · 실행 중" }
func (s *instance) CloseTool(*ui.Context) tea.Cmd {
	s.closed++
	return nil
}

var (
	_ ui.Closer       = (*instance)(nil)
	_ ui.ToolStatuser = (*instance)(nil)
)

type testTool struct {
	id, icon, name, status, title string
}

var testTools = []testTool{
	{ui.ToolLeet, "🧩", "오늘의 문제", "1606 · Hard · 아직 안 풂 · 🔥 3일", "1606 편집 중"},
	{ui.ToolWorkspace, "🤖", "작업공간", "masil · 🔒 나만 보기 · 터미널 1", "작업공간 · masil"},
	{ui.ToolArch, "📐", "아키텍처 설계", "결제 시스템 v3 (방) · 2명 편집 중", "결제 시스템 v3"},
	{ui.ToolGit, "🌿", "lazygit", "설치됨 v0.66.3 · 최근 masil", "lazygit · masil"},
	{ui.ToolAPI, "🧪", "API 시험", "결제팀 (공유) · staging", "API 시험 · 결제팀"},
}

// fixture is one hub test's tools: their Open calls and live instances.
type fixture struct {
	open      *opened
	instances map[string]*instance // instance id -> instance
}

// installTools registers the five test tools (flags = MASIL_FEATURES), restored on cleanup.
func installTools(t *testing.T, flags string) *fixture {
	t.Helper()
	t.Setenv("MASIL_FEATURES", flags)
	t.Cleanup(ui.SnapshotRegistryForTest())
	fx := &fixture{open: &opened{}, instances: map[string]*instance{}}
	for i, tt := range testTools {
		ui.RegisterTool(ui.ToolSpec{
			ID: tt.id, Order: i + 1, Icon: tt.icon,
			Name:   func(*i18n.T) string { return tt.name },
			Status: func(*ui.Context) string { return tt.status },
			New: func(_ *ui.Context, r ui.Route) ui.Component {
				in := &instance{route: r, title: tt.title}
				fx.instances[r.ID] = in
				return in
			},
			Open: func(_ *ui.Context, m termhost.Mode) tea.Cmd {
				fx.open.add(tt.id + "|" + string(m))
				return ui.OpenTool(tt.id, tt.id+"-1", nil)
			},
			WideDefault: tt.id == ui.ToolWorkspace || tt.id == ui.ToolArch || tt.id == ui.ToolAPI,
		})
	}
	return fx
}

// fixedEnv makes the open-mode menu decide in env.
func fixedEnv(t *testing.T, env termhost.Env) {
	t.Helper()
	t.Cleanup(tools.SetEnvForTest(func(*config.Config, string, bool) termhost.Env { return env }))
}

var (
	macEnv     = termhost.Env{GOOS: "darwin", TERM: "xterm-256color", Embedded: true}
	tmuxEnv    = termhost.Env{GOOS: "linux", TERM: "tmux-256color", TMUX: "/tmp/tmux-1/default,1,0", TmuxVersion: "3.4", Embedded: true}
	windowsEnv = termhost.Env{GOOS: "windows", TERM: "xterm-256color", Embedded: true}
)

var hubRoute = ui.Route{Kind: ui.RouteTools}

func showHub(h *uitest.Harness) {
	h.T.Helper()
	h.Send(ui.NavigateMsg{Route: hubRoute})
}

func onHub(t *testing.T, h *uitest.Harness) {
	t.Helper()
	if got := h.Model.Focus(); got.RouteKey != hubRoute.Key() {
		t.Fatalf("focus = %+v, want the hub", got)
	}
}

// ---- tests ----

func TestHubGolden(t *testing.T) {
	installTools(t, "all")
	fixedEnv(t, macEnv)
	sizes := append(slices.Clone(uitest.Required), uitest.Size{W: 40, H: 14})
	uitest.ForEachSize(t, sizes, func(t *testing.T, h *uitest.Harness) {
		h.Press("ctrl+t")
		onHub(t, h)
		h.Golden("hub")
		h.Press("4") // lazygit opens
		showHub(h)
		onHub(t, h)
		h.Golden("hub_open")
		h.Press("m")
		h.Golden("open_menu")
	})
}

func TestHubDigitOpens(t *testing.T) {
	fx := installTools(t, "all")
	h := uitest.New(t)
	h.Press("ctrl+t", "4")
	if got, want := fx.open.list(), []string{"git|auto"}; !slices.Equal(got, want) {
		t.Fatalf("ctrl+t 4: Open calls = %v, want %v", got, want)
	}
	if n := h.Model.Nav(); n.Front != app.FrontTools || len(n.Tools) != 2 || n.Tools[1].Arg != ui.ToolGit {
		t.Fatalf("after 4: front=%v tools=%v, want the hub and git", n.Front, n.Tools)
	}
	showHub(h)
	h.Press("enter") // the first row: leet
	if got, want := fx.open.list(), []string{"git|auto", "leet|auto"}; !slices.Equal(got, want) {
		t.Fatalf("enter: Open calls = %v, want %v", got, want)
	}
	showHub(h)
	h.Press("9") // no tool 9: nothing
	if got := fx.open.list(); len(got) != 2 {
		t.Fatalf("9: Open calls = %v", got)
	}
}

func TestHubOpenModeMenu(t *testing.T) {
	cases := []struct {
		name string
		env  termhost.Env
		want []string // the menu's rows, top first
	}{
		{"mac", macEnv, []string{"자동", "패널", "화면 전환"}},
		{"tmux", tmuxEnv, []string{"자동", "패널", "화면 전환", "외부 창"}},
		{"windows", windowsEnv, []string{"화면 전환"}},
	}
	all := []string{"자동", "패널", "화면 전환", "외부 창"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := installTools(t, "all")
			fixedEnv(t, tc.env)
			h := uitest.New(t)
			h.Press("ctrl+t", "j", "j", "j", "m") // lazygit's row
			s := h.Screen()
			if !strings.Contains(s, "여는 방식 · 🌿 lazygit") {
				t.Fatalf("m: no open-mode menu:\n%s", s)
			}
			for _, name := range all {
				if got, want := hasRow(s, name), slices.Contains(tc.want, name); got != want {
					t.Errorf("menu row %q shown = %v, want %v:\n%s", name, got, want, s)
				}
			}
			if len(fx.open.list()) != 0 {
				t.Fatalf("m opened a tool: %v", fx.open.list())
			}
			h.Press("esc")
			if s := h.Screen(); strings.Contains(s, "여는 방식 · ") {
				t.Fatalf("esc: the menu stays:\n%s", s)
			}
			onHub(t, h)
			if len(fx.open.list()) != 0 {
				t.Fatalf("esc opened a tool: %v", fx.open.list())
			}

			// The last row opens the tool that way, once, and is remembered.
			h.Press("m")
			for range len(tc.want) {
				h.Press("down")
			}
			h.Press("enter")
			last := modeOf(tc.want[len(tc.want)-1])
			if got, want := fx.open.list(), []string{"git|" + string(last)}; !slices.Equal(got, want) {
				t.Fatalf("menu enter: Open calls = %v, want %v", got, want)
			}
			if got := h.Context().ToolState.Common(ui.ToolGit).LastOpen; got != last {
				t.Fatalf("ToolState git last_open = %q, want %q", got, last)
			}
			showHub(h)
			h.Press("m")
			if row := selectedMenuRow(h); row != tc.want[len(tc.want)-1] {
				t.Fatalf("menu again: selected %q, want the last choice %q", row, tc.want[len(tc.want)-1])
			}
			h.Press("enter")
			if got := fx.open.list(); len(got) != 2 || got[1] != "git|"+string(last) {
				t.Fatalf("enter on the remembered row: Open calls = %v", got)
			}
			showHub(h) // a digit after the menu: auto again (the menu is one-off)
			h.Press("4")
			if got := fx.open.list(); got[len(got)-1] != "git|auto" {
				t.Fatalf("digit after the menu: Open calls = %v, want git|auto last", got)
			}
		})
	}
}

// hasRow reports a menu row named name: a line whose text (trimmed, the
// selection mark dropped) is name.
func hasRow(screen, name string) bool {
	for _, l := range strings.Split(screen, "\n") {
		for _, cell := range strings.Split(l, "║") {
			c := strings.TrimSpace(strings.Trim(strings.TrimSpace(cell), "│"))
			c = strings.TrimSpace(strings.TrimPrefix(c, "▸"))
			if c == name {
				return true
			}
		}
	}
	return false
}

// selectedMenuRow is the menu row with the ▸ mark ("" when none).
func selectedMenuRow(h *uitest.Harness) string {
	for _, l := range strings.Split(h.Screen(), "\n") {
		if i := strings.Index(l, "▸ "); i >= 0 {
			rest := l[i+len("▸ "):]
			if j := strings.IndexAny(rest, "║│"); j >= 0 {
				rest = rest[:j]
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func modeOf(name string) termhost.Mode {
	switch name {
	case "자동":
		return termhost.ModeAuto
	case "패널":
		return termhost.ModePane
	case "화면 전환":
		return termhost.ModeExec
	}
	return termhost.ModeWindow
}

func TestHubCloseInstance(t *testing.T) {
	fx := installTools(t, "all")
	h := uitest.New(t)
	h.Resize(120, 40)
	h.Press("ctrl+t", "4")
	showHub(h)
	s := h.Screen()
	if !strings.Contains(s, "── 열려 있음 ──") || !strings.Contains(s, "🌿 lazygit · masil") || !strings.Contains(s, "패널 · 실행 중") {
		t.Fatalf("hub lacks the open git instance:\n%s", s)
	}
	h.Press("x") // on a tool row: nothing
	if n := len(h.Model.Nav().Tools); n != 2 {
		t.Fatalf("x on a tool row closed something: %d entries", n)
	}
	h.Press("G", "x") // the last row: the git instance
	in := fx.instances["git-1"]
	if in == nil || in.closed != 1 {
		t.Fatalf("x: CloseTool calls = %+v", in)
	}
	if n := h.Model.Nav(); len(n.Tools) != 1 || n.Tools[0].Kind != ui.RouteTools {
		t.Fatalf("after x: tools = %v, want the hub alone", n.Tools)
	}
	if s := h.Screen(); strings.Contains(s, "열려 있음") || strings.Contains(s, "lazygit · masil") {
		t.Fatalf("after x the instance is still listed:\n%s", s)
	}
	onHub(t, h)
}

func TestHubEnterShowsInstance(t *testing.T) {
	installTools(t, "all")
	h := uitest.New(t)
	h.Press("ctrl+t", "4")
	showHub(h)
	h.Press("G", "enter")
	if got := h.Model.Focus(); got.RouteKey != (ui.Route{Kind: ui.RouteTool, ID: "git-1", Arg: ui.ToolGit}).Key() {
		t.Fatalf("enter on the instance row: focus = %+v, want git-1", got)
	}
	if n := len(h.Model.Nav().Tools); n != 2 {
		t.Fatalf("enter on the instance row added an entry: %d", n)
	}
}

func cmdIDs(items []palette.Item) []string {
	var out []string
	for _, it := range items {
		if it.Command != nil {
			out = append(out, it.Command.ID)
		}
	}
	return out
}

func TestPaletteFindsTools(t *testing.T) {
	fx := installTools(t, "all")
	h := uitest.New(t)
	ctx := h.Context()
	if got := cmdIDs(palette.ItemsForTest(ctx, "tools")); !slices.Contains(got, "tools.hub") {
		t.Errorf("tools: %v, want tools.hub", got)
	}
	if got := cmdIDs(palette.ItemsForTest(ctx, "도구")); !slices.Contains(got, "tools.hub") {
		t.Errorf("도구: %v, want tools.hub", got)
	}
	items := palette.ItemsForTest(ctx, "git")
	i := slices.IndexFunc(items, func(it palette.Item) bool { return it.Command != nil && it.Command.ID == "tools.open.git" })
	if i < 0 {
		t.Fatalf("git: %v, want the git tool", cmdIDs(items))
	}
	if items[i].Title != "🌿 lazygit" {
		t.Errorf("git item title = %q, want %q", items[i].Title, "🌿 lazygit")
	}
	if got := cmdIDs(palette.ItemsForTest(ctx, "오늘의")); !slices.Contains(got, "tools.open.leet") {
		t.Errorf("오늘의: %v, want the leet tool", got)
	}
	for _, id := range []string{"tools.toggle", "tools.next", "tools.prev", "tools.zoom"} {
		if _, ok := palette.Lookup(id); !ok {
			t.Errorf("palette command %s not registered", id)
		}
	}

	h.Press("ctrl+k")
	h.Type("lazygit")
	h.Press("enter")
	if got, want := fx.open.list(), []string{"git|auto"}; !slices.Equal(got, want) {
		t.Fatalf("palette lazygit enter: Open calls = %v, want %v", got, want)
	}
	h.Press("ctrl+k")
	h.Type("도구 허브")
	h.Press("enter")
	onHub(t, h)
}

func TestPaletteToolSurfaceCommands(t *testing.T) {
	installTools(t, "all")
	h := uitest.New(t)
	h.Send(ui.RunCommandMsg{ID: "tools.toggle"})
	onHub(t, h)
	h.Press("4")
	h.Send(ui.RunCommandMsg{ID: "tools.prev"})
	onHub(t, h)
	h.Send(ui.RunCommandMsg{ID: "tools.next"})
	if got := h.Model.Focus(); got.RouteKey != (ui.Route{Kind: ui.RouteTool, ID: "git-1", Arg: ui.ToolGit}).Key() {
		t.Fatalf("tools.next: focus = %+v, want git-1", got)
	}
	h.Send(ui.RunCommandMsg{ID: "tools.zoom"})
	if !h.Model.Nav().ToolZoom[(ui.Route{Kind: ui.RouteTool, ID: "git-1", Arg: ui.ToolGit}).Key()] {
		t.Fatal("tools.zoom: git-1 not zoomed")
	}
	h.Send(ui.RunCommandMsg{ID: "tools.toggle"})
	if n := h.Model.Nav(); n.Front != app.FrontTab {
		t.Fatalf("tools.toggle again: front = %v", n.Front)
	}
}

func TestHubHiddenWhenFlagsOff(t *testing.T) {
	installTools(t, "")
	h := uitest.New(t)
	ctx := h.Context()
	for _, q := range []string{"tools", "도구", "git", "lazygit"} {
		for _, id := range cmdIDs(palette.ItemsForTest(ctx, q)) {
			if strings.HasPrefix(id, "tools.") {
				t.Errorf("flags off: %q lists %s", q, id)
			}
		}
	}
	for _, id := range cmdIDs(palette.ItemsForTest(ctx, "")) {
		if strings.HasPrefix(id, "tools.") {
			t.Errorf("flags off: the empty palette lists %s", id)
		}
	}
	h.Press("ctrl+t")
	if n := h.Model.Nav(); n.Front == app.FrontTools || len(n.Tools) != 0 {
		t.Fatalf("flags off: ctrl+t opened the tool surface: %+v", n)
	}
	h.Send(ui.RunCommandMsg{ID: "tools.hub"})
	if n := h.Model.Nav(); len(n.Tools) != 0 {
		t.Fatalf("flags off: tools.hub opened the hub: %v", n.Tools)
	}
}

func TestHubWithoutTools(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "all")
	t.Cleanup(ui.SnapshotRegistryForTest())
	h := uitest.New(t)
	h.Press("ctrl+t")
	onHub(t, h)
	if s := h.Screen(); !strings.Contains(s, "아직 쓸 수 있는 도구가 없어요") {
		t.Fatalf("hub without tools lacks the empty text:\n%s", s)
	}
	h.Press("1", "m", "x", "enter")
	h.Check()
}

// TestHubTitleWithoutBorder: the hub's first line starts with its title "도구"
// (A8.2) where no pane border carries it (tiny); bordered modes keep the
// title in the border only.
func TestHubTitleWithoutBorder(t *testing.T) {
	installTools(t, "all")
	fixedEnv(t, macEnv)
	h := uitest.New(t)
	h.Resize(40, 14)
	h.Press("ctrl+t")
	onHub(t, h)
	first := strings.Split(h.Screen(), "\n")[1] // under the tab bar
	if !strings.HasPrefix(first, " 도구 ") || !strings.Contains(first, "ctrl+t 닫기") {
		t.Errorf("tiny hub first line = %q, want \" 도구 … ctrl+t 닫기\"", first)
	}
	h.Resize(120, 40)
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, "ctrl+t 닫기") && strings.Contains(l, " 도구 ") {
			t.Errorf("bordered hub draws the title twice: %q", l)
		}
	}
}
