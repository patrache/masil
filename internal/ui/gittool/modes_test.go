package gittool_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/gittool"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// modeWalk is the resize walk of the mode-switch tests (P1 Task 24): every
// layout mode and back.
var modeWalk = []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}, {W: 40, H: 14}, {W: 120, H: 40}}

// TestModeSwitchKeepsInstances: two lazygit panes stay the same processes
// through every layout mode, each mode shows the tool again, and the frame
// (the tab row's tool cell at 80x24 included) holds at every size.
func TestModeSwitchKeepsInstances(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"))
	h := uitest.New(t)
	h.Resize(200, 60)
	openMasil(t, h)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	h.Press("4", "down", "enter") // the second recent: blog
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil"), "git:" + native("/r/blog")}) {
		t.Fatalf("instances = %v:\n%s", ids, h.Screen())
	}
	started := len(f.Started())
	if started != 2 {
		t.Fatalf("Started = %d, want 2", started)
	}
	for _, sz := range modeWalk {
		h.Resize(sz.W, sz.H)
		h.Check()
		if n := len(f.Started()); n != started {
			t.Fatalf("%dx%d: Started = %d, want %d (a resize restarted lazygit)", sz.W, sz.H, n, started)
		}
		if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil"), "git:" + native("/r/blog")}) {
			t.Fatalf("%dx%d: instances = %v", sz.W, sz.H, ids)
		}
		if s := h.Screen(); !strings.Contains(s, "lazygit pane") {
			t.Fatalf("%dx%d (%v): the tool is not shown:\n%s", sz.W, sz.H, h.Context().Mode, s)
		}
	}
	// The other instance comes back too, still the same process.
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTool, ID: "git:" + native("/r/masil"), Arg: ui.ToolGit}})
	h.Check()
	if s := h.Screen(); !strings.Contains(s, "lazygit pane") || len(f.Started()) != started {
		t.Fatalf("masil again: Started %d\n%s", len(f.Started()), s)
	}
}

// TestTinyOpensExec: in the tiny mode lazygit opens as a screen switch (P0
// Choose's tiny rule) without the pane-only color overlay, and the frame
// holds before and after it returns.
func TestTinyOpensExec(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"))
	h := uitest.New(t)
	h.Resize(40, 14)
	if h.Context().Mode != layout.ModeTiny {
		t.Fatalf("40x14 is %v, want tiny", h.Context().Mode)
	}
	openMasil(t, h)
	got := f.Started()
	if len(got) != 1 {
		t.Fatalf("Started = %+v:\n%s", got, h.Screen())
	}
	// The mode termpane.Launch decided (P0 Choose's tiny rule), as the
	// instance got it in termpane.Launched.Mode.
	if m := gittool.LaunchedModeForTest(h.Instance(ui.Route{Kind: ui.RouteTool, ID: "git:" + native("/r/masil"), Arg: ui.ToolGit}.Key())); m != termhost.ModeExec {
		t.Fatalf("tiny: Launched.Mode = %q, want %q", m, termhost.ModeExec)
	}
	if slices.Contains(got[0].Args, "--use-config-file") {
		t.Fatalf("tiny: the pane overlay rode an exec launch: %q", got[0].Args)
	}
	if s := h.Screen(); strings.Contains(s, "lazygit pane") || !strings.Contains(s, "화면 전환으로 실행 중") {
		t.Fatalf("tiny did not open a screen switch:\n%s", s)
	}
	h.Check()
	h.Send(f.Exit("git:"+native("/r/masil"), 0)) // back from the screen switch
	h.Check()
	if s := h.Screen(); !strings.Contains(s, "종료됨 · enter 다시 열기") {
		t.Fatalf("no exited line after the switch back:\n%s", s)
	}
	h.Resize(120, 40)
	h.Check()
	if s := h.Screen(); strings.Contains(s, "lazygit pane") {
		t.Fatalf("an exec instance became a pane:\n%s", s)
	}
	if n := len(f.Started()); n != 1 {
		t.Fatalf("Started = %d after returning, want 1", n)
	}
}
