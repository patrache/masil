package app_test

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
)

// 0.1.3 patch (tools-nav fix 2): a component that grabs the keyboard (a
// running termpane: lazygit, nvim, a workspace terminal) gets every key but
// ctrl+t, which leaves it — the program keeps running — and goes back to the
// chat. ctrl+t also arrives as ctrl+ㅅ or with BaseCode 't' on a Korean layout.

// grabTool is a tool instance that grabs (grab) or captures (text field) the
// keyboard and records the keys it is given.
type grabTool struct {
	stubTool
	grab, capture bool
	keys          []string
}

func (g *grabTool) GrabsKeys() bool     { return g.grab }
func (g *grabTool) CapturesInput() bool { return g.capture }
func (g *grabTool) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	g.keys = append(g.keys, msg.String())
	return true, nil
}

var (
	_ ui.KeyGrabber    = (*grabTool)(nil)
	_ ui.InputCapturer = (*grabTool)(nil)
	_ ui.KeyHandler    = (*grabTool)(nil)
)

// installGrabTools registers git (grabbing, a panel) and leet (a text field)
// over the stub tools; got collects the instances by id.
func installGrabTools(t *testing.T, flags string, got map[string]*grabTool) {
	t.Helper()
	t.Cleanup(ui.SnapshotRegistryForTest())
	for id, order := range map[string]int{ui.ToolLeet: 1, ui.ToolGit: 4} { // installStubTools' orders
		ui.RegisterTool(ui.ToolSpec{
			ID: id, Order: order, Icon: "🧪",
			Name: func(*i18n.T) string { return id },
			New: func(_ *ui.Context, r ui.Route) ui.Component {
				g := &grabTool{stubTool: stubTool{route: r}, grab: id == ui.ToolGit, capture: id == ui.ToolLeet}
				got[r.ID] = g
				return g
			},
			Open: func(*ui.Context, termhost.Mode) tea.Cmd { return ui.OpenTool(id, id+"-1", nil) },
		})
	}
	installStubTools(t, flags) // the routes (and the other stub tools)
}

var ctrlTSpellings = map[string]tea.KeyPressMsg{
	"ctrl+t":          {Code: 't', Mod: tea.ModCtrl},
	"ctrl+ㅅ":          {Code: 'ㅅ', Mod: tea.ModCtrl},
	"ctrl+ㅅ/base t":   {Code: 'ㅅ', BaseCode: 't', Mod: tea.ModCtrl},
	"ctrl+ㅅ/base+txt": {Code: 'ㅅ', BaseCode: 't', Mod: tea.ModCtrl, Text: "ㅅ"},
}

func TestCtrlTLeavesGrabbingPanel(t *testing.T) {
	for name, key := range ctrlTSpellings {
		t.Run(name, func(t *testing.T) {
			got := map[string]*grabTool{}
			installGrabTools(t, "all", got)
			forEachSize(t, uitest.Required, nil, func(t *testing.T, h *uitest.Harness) {
				clear(got)
				openTool(h, ui.ToolGit, "git-1")
				g := got["git-1"]
				h.Press("a", "ctrl+k", "tab", "ctrl+c") // the panel's keys, the hard globals included
				h.Send(key)
				if want := []string{"a", "ctrl+k", "tab", "ctrl+c"}; !slices.Equal(g.keys, want) {
					t.Fatalf("panel keys = %q, want %q (ctrl+t not among them)", g.keys, want)
				}
				n := h.Model.Nav()
				if n.Front != app.FrontTab || len(n.Tools) != 1 {
					t.Fatalf("%s: front %v tools %v, want the chat with the panel kept", name, n.Front, n.Tools)
				}
				if g.closed != 0 {
					t.Fatal("ctrl+t closed the panel")
				}
				// Back on the panel (the hub, then the instance): it has the keyboard again.
				h.Send(key)
				if r, ok := shownTool(h); !ok || r.Kind != ui.RouteTools {
					t.Fatalf("ctrl+t from the chat: %+v, %v, want the hub", r, ok)
				}
				h.Send(ui.NavigateMsg{Route: toolRoute(ui.ToolGit, "git-1")})
				h.Press("b")
				if g.keys[len(g.keys)-1] != "b" || got["git-1"] != g {
					t.Fatalf("back on the panel: keys %q (rebuilt %v)", g.keys, got["git-1"] != g)
				}
			})
		})
	}
}

// A tool text field (the built-in Go editor, a picker's filter) captures
// printable keys but ctrl+t, in any spelling, still toggles the surface.
func TestCtrlTLeavesToolTextField(t *testing.T) {
	for name, key := range ctrlTSpellings {
		t.Run(name, func(t *testing.T) {
			got := map[string]*grabTool{}
			installGrabTools(t, "all", got)
			h := uitest.New(t)
			openTool(h, ui.ToolLeet, "leet-1")
			h.Press("x")
			h.Send(key)
			if n := h.Model.Nav(); n.Front != app.FrontTab {
				t.Fatalf("front = %v, want the chat", n.Front)
			}
			if g := got["leet-1"]; !slices.Equal(g.keys, []string{"x"}) {
				t.Fatalf("text field keys = %q, want only x", g.keys)
			}
		})
	}
}

// 0.1.3 patch (tools-nav fix 3): while a tool flag is on, the chat/home status
// line says how to reach the tools — "ctrl+t 도구" right after the ctrl+k
// hint (command and standard, the modes whose hints show ctrl+k), with the
// key from [keys]. Off, or on the tool surface (the hub shows its own close
// key), the 0.1.2 hints are unchanged.
func TestStatusShowsToolsHint(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{200, 60, "ctrl+k 이동 · ctrl+t 도구 · tab 패널 전환"},
		{120, 40, "t 스레드 · ctrl+k · ctrl+t 도구"},
	}
	for _, tc := range cases {
		installStubTools(t, "all")
		h := uitest.New(t)
		h.Resize(tc.w, tc.h)
		if s := statusRow(h); !strings.Contains(s, tc.want) {
			t.Errorf("%dx%d: status = %q, want %q", tc.w, tc.h, s, tc.want)
		}
		h.Press("ctrl+t") // the hub: its first line has "ctrl+t 닫기", the status line no tool hint
		if s := statusRow(h); strings.Contains(s, "ctrl+t 도구") {
			t.Errorf("%dx%d on the hub: status = %q", tc.w, tc.h, s)
		}
		h.Press("ctrl+t")
		if s := statusRow(h); !strings.Contains(s, tc.want) {
			t.Errorf("%dx%d back on the chat: status = %q", tc.w, tc.h, s)
		}
	}
	for _, sz := range [][2]int{{80, 24}, {50, 20}, {40, 14}} { // no ctrl+k hint there: no tool hint either
		installStubTools(t, "all")
		h := uitest.New(t)
		h.Resize(sz[0], sz[1])
		if s := statusRow(h); strings.Contains(s, "ctrl+t") {
			t.Errorf("%dx%d: status = %q", sz[0], sz[1], s)
		}
	}
	installStubTools(t, "none")
	h := uitest.New(t)
	h.Resize(200, 60)
	if s := statusRow(h); strings.Contains(s, "ctrl+t") {
		t.Errorf("flags off: status = %q", s)
	}
	installStubTools(t, "all")
	h = uitest.New(t, uitest.WithKeys(map[string][]string{"tools.toggle": {"ctrl+y"}}))
	h.Resize(200, 60)
	if s := statusRow(h); !strings.Contains(s, "ctrl+k 이동 · ctrl+y 도구") {
		t.Errorf("rebound: status = %q", s)
	}
}
