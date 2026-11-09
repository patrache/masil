package termpane_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
	_ "github.com/patrache/masil/internal/ui/tools" // the RouteTool factory
	"github.com/patrache/masil/internal/ui/uitest"
)

// fakeHandle is a recording termpane.Handle with a fixed screen.
type fakeHandle struct {
	mu        sync.Mutex
	program   string
	title     string
	screen    string
	scroll    []string
	mouseMode bool
	shared    bool
	keys      []string
	pastes    []string
	mice      []string
	sizes     []termhost.Size
	focus     []bool
	closed    int
	done      chan termhost.ExitInfo
}

func newFake() *fakeHandle {
	return &fakeHandle{
		program: "/usr/local/bin/lazygit",
		screen:  "lazygit 화면 첫 줄\n  Files  ▸ main.go\n  한글 2칸 확인",
		done:    make(chan termhost.ExitInfo, 1),
	}
}

func (f *fakeHandle) Program() string { return f.program }
func (f *fakeHandle) Resize(s termhost.Size) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, s)
	return nil
}
func (f *fakeHandle) SendKey(k tea.KeyPressMsg) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, k.String())
}
func (f *fakeHandle) SendPaste(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pastes = append(f.pastes, s)
}
func (f *fakeHandle) SendMouse(m tea.MouseMsg, x, y int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kind := "?"
	switch m.(type) {
	case tea.MouseClickMsg:
		kind = "click"
	case tea.MouseReleaseMsg:
		kind = "release"
	case tea.MouseWheelMsg:
		kind = "wheel"
	case tea.MouseMotionMsg:
		kind = "motion"
	}
	f.mice = append(f.mice, fmt.Sprintf("%s:%s@%d,%d", kind, m.Mouse().Button, x, y))
}
func (f *fakeHandle) SetFocus(b bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focus = append(f.focus, b)
}
func (f *fakeHandle) Render() string                 { return f.screen }
func (f *fakeHandle) Cursor() (int, int, bool)       { return 2, 1, true }
func (f *fakeHandle) Title() string                  { return f.title }
func (f *fakeHandle) MouseMode() bool                { return f.mouseMode }
func (f *fakeHandle) Scrollback() []string           { return slices.Clone(f.scroll) }
func (f *fakeHandle) Done() <-chan termhost.ExitInfo { return f.done }
func (f *fakeHandle) Shared() bool                   { return f.shared }
func (f *fakeHandle) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

func (f *fakeHandle) gotKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.keys)
}

func (f *fakeHandle) gotFocus() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.focus)
}

func (f *fakeHandle) gotSizes() []termhost.Size {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sizes)
}

var _ termpane.Handle = (*fakeHandle)(nil)
var _ termpane.Handle = (*termhost.Pane)(nil)

const paneID = "git:test"

// paneRig is a harness showing one pane (tool git, instance paneID) on the
// tool surface, focused.
type paneRig struct {
	h    *uitest.Harness
	f    *fakeHandle
	pane *termpane.Pane
}

// registerGit registers the git tool whose instances are panes over f.
func registerGit(t *testing.T, f *fakeHandle, opts termpane.Options, got **termpane.Pane) {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "git")
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.RegisterTool(ui.ToolSpec{
		ID: ui.ToolGit, Order: 4, Icon: "🌿",
		Name: func(*i18n.T) string { return "lazygit" },
		New: func(_ *ui.Context, r ui.Route) ui.Component {
			o := opts
			o.ToolID, o.InstanceID = ui.ToolGit, r.ID
			*got = termpane.New(f, o)
			return *got
		},
		Open: func(*ui.Context, termhost.Mode) tea.Cmd { return ui.OpenTool(ui.ToolGit, paneID, nil) },
	})
}

func newRig(t *testing.T, opts termpane.Options, hopts ...uitest.Option) *paneRig {
	t.Helper()
	rig := &paneRig{f: newFake()}
	registerGit(t, rig.f, opts, &rig.pane)
	rig.h = uitest.New(t, hopts...)
	rig.h.Send(ui.OpenTool(ui.ToolGit, paneID, nil)())
	if rig.pane == nil {
		t.Fatal("the pane was not built")
	}
	if r, ok := rig.h.Model.Frame().Routes[toolSlot(rig.h)]; !ok || r.ID != paneID || rig.h.Model.Focus().Slot != toolSlot(rig.h) {
		t.Fatalf("the pane is not shown and focused: routes %v focus %+v", rig.h.Model.Frame().Routes, rig.h.Model.Focus())
	}
	return rig
}

func toolSlot(h *uitest.Harness) layout.SlotID {
	if h.Model.Frame().Layout.Mode == layout.ModeCommand {
		return layout.SlotTool
	}
	return layout.SlotMain
}

// paneInner is the pane's inner rect this frame.
func paneInner(t *testing.T, h *uitest.Harness) layout.Rect {
	t.Helper()
	p, ok := h.Model.Frame().Layout.Pane(toolSlot(h))
	if !ok {
		t.Fatal("no tool pane in the layout")
	}
	if p.Bordered {
		return p.Rect.Inner()
	}
	return p.Rect
}

func hasQuit(h *uitest.Harness) bool {
	return slices.ContainsFunc(h.TeaMsgs(), func(m tea.Msg) bool { _, ok := m.(tea.QuitMsg); return ok })
}

func TestPaneGetsAllKeys(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	rig.h.Press("ctrl+c", "ctrl+c", "ctrl+k", "tab", "ctrl+t", "a")
	rig.h.Paste("붙여넣기")
	if got, want := rig.f.gotKeys(), []string{"ctrl+c", "ctrl+c", "ctrl+k", "tab", "ctrl+t", "a"}; !slices.Equal(got, want) {
		t.Errorf("keys = %q, want %q", got, want)
	}
	if !slices.Equal(rig.f.pastes, []string{"붙여넣기"}) {
		t.Errorf("pastes = %q", rig.f.pastes)
	}
	if hasQuit(rig.h) {
		t.Error("ctrl+c twice quit masil")
	}
	if s := rig.h.Screen(); strings.Contains(s, "한 번 더 누르면 종료해요") {
		t.Error("ctrl+c armed quitting")
	}
	if rig.h.Nav().Front != app.FrontTools {
		t.Error("ctrl+t left the tool surface")
	}
	if !rig.pane.GrabsKeys() {
		t.Error("GrabsKeys = false while the program runs")
	}
}

func TestPrefixLeave(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	rig.h.Press("ctrl+\\", "esc")
	if got := rig.f.gotKeys(); len(got) != 0 {
		t.Fatalf("the program got %q", got)
	}
	if rig.pane.GrabsKeys() {
		t.Fatal("GrabsKeys after ctrl+\\ esc")
	}
	rig.h.Screen()
	if rig.pane.GrabsKeys() {
		t.Fatal("drawing took the keyboard back")
	}
	rig.h.Press("ctrl+t") // masil's key again: back to the chat
	if rig.h.Nav().Front == app.FrontTools {
		t.Error("ctrl+t after leaving did not reach masil")
	}
	if got := rig.f.gotKeys(); len(got) != 0 {
		t.Errorf("the program got %q after leaving", got)
	}
	rig.h.Press("ctrl+t", "enter", "b") // back on the pane: enter types into it again
	if !rig.pane.GrabsKeys() || !slices.Equal(rig.f.gotKeys(), []string{"b"}) {
		t.Errorf("enter did not give the pane the keyboard back: grabs %v keys %q", rig.pane.GrabsKeys(), rig.f.gotKeys())
	}
}

func TestPrefixLiteral(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	rig.h.Press("ctrl+\\", "ctrl+\\")
	if got := rig.f.gotKeys(); !slices.Equal(got, []string{"ctrl+\\"}) {
		t.Errorf("keys = %q, want one ctrl+\\", got)
	}
}

func TestPrefixTimeout(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	rig.h.Press("ctrl+\\")
	rig.h.Advance(2 * time.Second)
	rig.h.Press("a")
	if got := rig.f.gotKeys(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("keys = %q, want a (the prefix timed out)", got)
	}
}

func TestPrefixAcceptsCtrlBackslashVariants(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	variants := []tea.KeyPressMsg{
		{Code: '\\', Mod: tea.ModCtrl},
		{Code: '4', Mod: tea.ModCtrl},
		{Code: 0x1c},
	}
	for _, v := range variants {
		rig.h.Send(v)
		rig.h.Press("q") // not a prefix key: cancels, nothing sent
	}
	if got := rig.f.gotKeys(); len(got) != 0 {
		t.Fatalf("keys = %q: a variant did not arm the prefix", got)
	}
	for _, v := range variants {
		rig.h.Send(v)
		rig.h.Send(v) // twice: the prefix key itself, sent as ctrl+\
	}
	if got := rig.f.gotKeys(); !slices.Equal(got, []string{"ctrl+\\", "ctrl+\\", "ctrl+\\"}) {
		t.Errorf("literal keys = %q", got)
	}
}

func TestPrefixKillPrefersShare(t *testing.T) {
	active, killed := true, 0
	rig := newRig(t, termpane.Options{})
	ui.RegisterKillSwitch(ui.KillSwitch{
		Active: func() bool { return active },
		Kill:   func(*ui.Context) tea.Cmd { killed++; return nil },
	})
	rig.h.Press("ctrl+\\", "x")
	if killed != 1 {
		t.Fatalf("Kill calls = %d, want 1", killed)
	}
	if strings.Contains(rig.h.Screen(), "프로그램을 끝낼까요") {
		t.Fatal("a confirm dialog opened while sharing")
	}
	active = false
	rig.h.Press("ctrl+\\", "x")
	if killed != 1 {
		t.Fatalf("Kill called while not active")
	}
	if s := rig.h.Screen(); !strings.Contains(s, "lazygit 프로그램을 끝낼까요?") || !strings.Contains(s, "프로그램 끝내기") {
		t.Fatalf("no exit confirm on screen:\n%s", s)
	}
	rig.h.Press("y")
	if rig.f.closed != 1 {
		t.Errorf("Close calls = %d after yes", rig.f.closed)
	}
}

func TestPaneChildExitReleasesFocus(t *testing.T) {
	exits := 0
	rig := newRig(t, termpane.Options{OnExit: func(*ui.Context, termhost.ExitInfo) tea.Cmd { exits++; return nil }})
	rig.f.done <- termhost.ExitInfo{Mode: termhost.ModePane, Code: 0}
	rig.h.WaitPosted(1, 2*time.Second)
	rig.h.Flush()
	if exits != 1 {
		t.Errorf("OnExit calls = %d", exits)
	}
	if rig.pane.GrabsKeys() {
		t.Fatal("GrabsKeys after the program ended")
	}
	if s := rig.h.Screen(); !strings.Contains(s, "끝남 · 종료 코드 0") {
		t.Fatalf("no exit title:\n%s", s)
	}
	rig.h.Press("ctrl+t")
	if rig.h.Nav().Front == app.FrontTools {
		t.Error("masil did not get ctrl+t after the exit")
	}
	if got := rig.f.gotKeys(); len(got) != 0 {
		t.Errorf("keys went to the ended program: %q", got)
	}
}

func TestPaneSurvivesTooSmallAndBack(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	rig.h.Screen()
	rig.h.Resize(19, 10)
	rig.h.Screen()
	rig.h.Resize(120, 40)
	rig.h.Screen()
	sizes := rig.f.gotSizes()
	if len(sizes) == 0 {
		t.Fatal("no Resize")
	}
	for _, s := range sizes {
		if s.W <= 0 || s.H <= 0 {
			t.Fatalf("a zero size reached the program: %v", sizes)
		}
	}
	in := paneInner(t, rig.h)
	if last := sizes[len(sizes)-1]; last != (termhost.Size{W: in.W, H: in.H}) {
		t.Errorf("last size %v, want the inner %dx%d (all %v)", last, in.W, in.H, sizes)
	}
}

func TestPaneStatusHint(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	status := func() string {
		lines := strings.Split(rig.h.Screen(), "\n")
		return lines[len(lines)-1]
	}
	if s := status(); !strings.Contains(s, "⌨ lazygit 에 입력 중 · ctrl+\\ esc 나가기") {
		t.Errorf("status = %q", s)
	}
	rig.h.Press("ctrl+\\")
	if s := status(); !strings.Contains(s, "ctrl+\\ 다음: esc 나가기 · z 확대 · x 끝내기 · p 팔레트 · ? 도움말") {
		t.Errorf("prefix status = %q", s)
	}
	rig.h.Press("esc")
	if s := status(); strings.Contains(s, "⌨") {
		t.Errorf("status after leaving = %q", s)
	}
}

func TestPaneMouseOnlyInMouseMode(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	for i := range 40 {
		rig.f.scroll = append(rig.f.scroll, fmt.Sprintf("지난 줄 %02d", i))
	}
	in := paneInner(t, rig.h)
	rig.h.Wheel(in.X+3, in.Y+2, -1)
	if len(rig.f.mice) != 0 {
		t.Fatalf("mouse went to the program out of mouse mode: %q", rig.f.mice)
	}
	if s := rig.h.Screen(); !strings.Contains(s, "지난 줄 39") {
		t.Fatalf("the wheel did not scroll back:\n%s", s)
	}
	rig.h.Press("esc") // ends the scroll view
	if s := rig.h.Screen(); strings.Contains(s, "지난 줄 39") || !strings.Contains(s, "lazygit 화면 첫 줄") {
		t.Fatalf("esc did not end the scroll view:\n%s", s)
	}
	rig.f.mouseMode = true
	rig.h.Wheel(in.X+3, in.Y+2, -1)
	rig.h.Click(in.X+5, in.Y+1)
	if want := []string{"wheel:wheelup@3,2", "click:left@5,1", "release:left@5,1"}; !slices.Equal(rig.f.mice, want) {
		t.Errorf("mice = %q, want %q", rig.f.mice, want)
	}
}

func TestPaneSharedBadge(t *testing.T) {
	sizes := append(slices.Clone(uitest.Required), uitest.Size{W: 40, H: 14}, uitest.Size{W: 19, H: 10})
	badge := i18n.KoT().Termpane.SharedBadge()
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			rig := newRig(t, termpane.Options{})
			rig.f.title = strings.Repeat("아주 긴 저장소 제목 ", 20)
			rig.f.shared = true
			rig.h.Resize(sz.W, sz.H)
			s := rig.h.Screen()
			rig.h.Check()
			if rig.h.Model.Frame().Layout.Mode == layout.ModeTooSmall {
				return // nothing but the too-small notice is drawn
			}
			if rig.h.Model.Frame().Layout.Mode == layout.ModeTiny {
				// No border: the mark is on the tab line's tool cell, and the
				// program keeps the whole body (no row taken for a badge).
				tabs := func() string { return strings.Split(rig.h.Screen(), "\n")[0] }
				if !strings.Contains(tabs(), "●") {
					t.Fatalf("no share mark on the tiny tab line:\n%s", s)
				}
				in := paneInner(t, rig.h)
				if sizes := rig.f.gotSizes(); sizes[len(sizes)-1] != (termhost.Size{W: in.W, H: in.H}) {
					t.Fatalf("program size %v, want the whole %dx%d", sizes[len(sizes)-1], in.W, in.H)
				}
				rig.f.shared = false
				if strings.Contains(tabs(), "●") {
					t.Fatal("share mark still on the tab line after sharing ended")
				}
				rig.h.Check()
				return
			}
			if !strings.Contains(s, badge) {
				t.Fatalf("no %q:\n%s", badge, s)
			}
			rig.f.shared = false
			if s := rig.h.Screen(); strings.Contains(s, badge) {
				t.Fatalf("badge still shown after sharing ended:\n%s", s)
			}
			rig.h.Check()
		})
	}
}

// The program hears focus-out when the pane is hidden (ctrl+\ t to the chat)
// and focus-in when it is back; terminal focus changes while it is hidden
// tell it nothing.
func TestPaneFocusFollowsShell(t *testing.T) {
	rig := newRig(t, termpane.Options{})
	rig.h.Screen()
	if got := rig.f.gotFocus(); !slices.Equal(got, []bool{true}) {
		t.Fatalf("focus reports on open = %v, want [true]", got)
	}
	rig.h.Press("ctrl+\\", "t")
	if rig.h.Nav().Front == app.FrontTools {
		t.Fatal("ctrl+\\ t did not leave the tool surface")
	}
	rig.h.Screen()
	if got := rig.f.gotFocus(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("focus reports after hiding = %v, want [true false]", got)
	}
	rig.h.Send(tea.BlurMsg{})
	rig.h.Send(tea.FocusMsg{})
	rig.h.Screen()
	if got := rig.f.gotFocus(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("a hidden pane got %v after blur+focus, want [true false]", got)
	}
	rig.h.Press("ctrl+t")
	rig.h.Screen()
	if got := rig.f.gotFocus(); !slices.Equal(got, []bool{true, false, true}) {
		t.Fatalf("focus reports after coming back = %v, want [true false true]", got)
	}
}

// Drawing has no effect on the program: no focus report, no resize — however
// often the pane is drawn and whatever it is drawn with (contract: View is pure).
func TestPaneViewIsPure(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 40, H: 14}} {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			rig := newRig(t, termpane.Options{})
			rig.h.Resize(sz.W, sz.H)
			rig.h.Screen()
			focus, sizes := rig.f.gotFocus(), rig.f.gotSizes()
			ctx := rig.h.Context()
			rig.pane.View(ctx, false)
			rig.pane.View(ctx, true)
			rig.f.shared = true
			rig.h.Screen()
			rig.h.Screen()
			rig.f.shared = false
			rig.h.Screen()
			if got := rig.f.gotFocus(); !slices.Equal(got, focus) {
				t.Errorf("drawing sent focus reports: %v -> %v", focus, got)
			}
			if got := rig.f.gotSizes(); !slices.Equal(got, sizes) {
				t.Errorf("drawing resized the program: %v -> %v", sizes, got)
			}
		})
	}
}

func TestPaneGolden(t *testing.T) {
	sizes := append(slices.Clone(uitest.Required), uitest.Size{W: 40, H: 14})
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			rig := newRig(t, termpane.Options{})
			rig.h.Resize(sz.W, sz.H)
			rig.h.Golden("pane")
		})
	}
	t.Run("shared_120x40", func(t *testing.T) {
		rig := newRig(t, termpane.Options{})
		rig.f.shared = true
		rig.h.Golden("pane")
	})
}
