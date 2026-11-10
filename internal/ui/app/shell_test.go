package app_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// forEachSize is uitest.ForEachSize with options: subtest "WxH", a fresh harness
// per size, then Resize(W, H).
func forEachSize(t *testing.T, sizes []uitest.Size, opts []uitest.Option, fn func(*testing.T, *uitest.Harness)) {
	t.Helper()
	if len(sizes) == 0 {
		t.Fatal("forEachSize: no sizes")
	}
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := uitest.New(t, opts...)
			h.Resize(s.W, s.H)
			fn(t, h)
		})
	}
}

var devRoute = ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}

func TestShellGoldens(t *testing.T) {
	onlyMe := func(s *store.Store) { full := store.New(); fixtures.Seed(full); s.Me = full.Me }
	for _, tc := range []struct {
		name string
		opts []uitest.Option
	}{
		{"empty_home", []uitest.Option{uitest.WithSeed(onlyMe)}},
		{"fixtures_home", nil}, // default seed = fixtures.Seed
		{"dev_channel", []uitest.Option{uitest.WithRoute(devRoute)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), tc.opts, func(t *testing.T, h *uitest.Harness) { h.Golden(tc.name) })
		})
	}
}

func TestThemesStyledGolden(t *testing.T) {
	for _, th := range theme.All() {
		t.Run(string(th.Name), func(t *testing.T) {
			h := uitest.New(t, uitest.WithTheme(th.Name))
			h.Resize(120, 40)
			h.Check()
			if !strings.Contains(h.Styled(), "\x1b[") {
				t.Fatal("Styled() must carry the theme's SGR sequences")
			}
			golden.RequireEqual(t, h.Styled())
		})
	}
}

func TestTooSmallMessage(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 19, H: 10}, {W: 30, H: 5}} {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := uitest.New(t)
			h.Resize(sz.W, sz.H)
			h.Check()
			lines := strings.Split(h.Screen(), "\n")
			if len(lines) != sz.H {
				t.Fatalf("%d lines, want %d", len(lines), sz.H)
			}
			mid := lines[sz.H/2]
			full := "창을 조금만 키워 주세요"
			want := textwidth.Truncate(full, sz.W, "…")
			if !strings.Contains(mid, want) {
				t.Fatalf("middle row %q does not contain %q", mid, want)
			}
			if textwidth.Width(full) > sz.W && !strings.Contains(mid, "…") {
				t.Fatalf("middle row %q must be truncated with …", mid)
			}
			for i, l := range lines {
				if i != sz.H/2 && strings.TrimSpace(l) != "" {
					t.Fatalf("row %d = %q, want blank (no status line in TooSmall)", i, l)
				}
			}
			if _, ok := h.Model.Frame().Layout.Pane(layout.SlotStatus); ok {
				t.Fatal("TooSmall must not lay out a status line")
			}
		})
	}
}

func TestPaneSizesAndResize(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	key := devRoute.Key()
	ch, ok := h.Instance(key).(*stub.Chat)
	if !ok {
		t.Fatalf("Instance(%q) = %T, want *stub.Chat", key, h.Instance(key))
	}
	last := func() ui.Size { return ch.Resizes[len(ch.Resizes)-1] }
	if got := last(); got != (ui.Size{W: 76, H: 56}) { // row 0 is the tab bar
		t.Fatalf("200x60 main inner = %+v, want {76 56}", got)
	}
	n := len(ch.Resizes)
	h.Resize(200, 60) // same size: no Resize call
	if len(ch.Resizes) != n {
		t.Fatalf("same-size relayout resized again: %v", ch.Resizes[n:])
	}
	serial := ch.Serial
	h.Resize(120, 40)
	if got := last(); got != (ui.Size{W: 92, H: 36}) {
		t.Fatalf("120x40 main inner = %+v, want {92 36}", got)
	}
	if ch2 := h.Instance(key).(*stub.Chat); ch2 != ch || ch2.Serial != serial {
		t.Fatalf("instance changed across resize: serial %d -> %d", serial, ch2.Serial)
	}
	h.Check()
}

func TestWindowTitleAndViewFlags(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	v := h.Model.View()
	if !v.AltScreen {
		t.Error("AltScreen must be true")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want CellMotion", v.MouseMode)
	}
	if !v.ReportFocus {
		t.Error("ReportFocus must be true")
	}
	if v.WindowTitle != "masil · #개발" {
		t.Errorf("WindowTitle = %q, want %q", v.WindowTitle, "masil · #개발")
	}
	amber := theme.Get(theme.Amber)
	if v.BackgroundColor != amber.P.BG || v.ForegroundColor != amber.P.FG {
		t.Errorf("Background/Foreground = %v/%v, want amber %v/%v", v.BackgroundColor, v.ForegroundColor, amber.P.BG, amber.P.FG)
	}

	h.Context().Config.UI.Mouse = false
	h.Context().Config.UI.PaintBackground = false
	v = h.Model.View()
	if v.MouseMode != tea.MouseModeNone {
		t.Errorf("ui.mouse=false: MouseMode = %v, want None", v.MouseMode)
	}
	if v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Errorf("paint_background=false: colors must be nil, got %v/%v", v.BackgroundColor, v.ForegroundColor)
	}

	home := uitest.New(t)
	if got := home.Model.View().WindowTitle; got != "masil · 홈" {
		t.Errorf("home WindowTitle = %q, want %q", got, "masil · 홈")
	}
}

// fakeRT is a ui.Realtime with a settable state.
type fakeRT struct{ st realtime.State }

func (f *fakeRT) Typing(string, *string) {}
func (f *fakeRT) SetIdle(bool)           {}
func (f *fakeRT) Watch([]string)         {}
func (f *fakeRT) State() realtime.State  { return f.st }

func TestStatusConnectionItem(t *testing.T) {
	h := uitest.New(t)
	status := func() string {
		lines := strings.Split(h.Screen(), "\n")
		return lines[len(lines)-1]
	}
	if s := status(); !strings.Contains(s, "표준") || !strings.Contains(s, "f 피드 · t 스레드 · ctrl+k") || !strings.Contains(s, "● 온라인") {
		t.Fatalf("status = %q, want pill 표준, the standard hint and ● 온라인", s)
	}
	rt := &fakeRT{st: realtime.State{Phase: realtime.PhaseLive}}
	app.SetRealtimeForTest(h.Model, rt)
	h.Context().Store.Self.Status = "away"
	if s := status(); !strings.Contains(s, "◐ 자리비움") {
		t.Fatalf("away: status = %q", s)
	}
	h.Context().Store.Self.Status = "dnd"
	if s := status(); !strings.Contains(s, "⊘ 방해금지") {
		t.Fatalf("dnd: status = %q", s)
	}
	for ph, want := range map[realtime.Phase]string{realtime.PhaseConnecting: "◌ 연결 중…", realtime.PhaseSyncing: "◌ 동기화 중…"} {
		rt.st.Phase = ph
		if s := status(); !strings.Contains(s, want) {
			t.Fatalf("phase %v: status = %q, want %q", ph, s, want)
		}
	}
	for _, ph := range []realtime.Phase{realtime.PhaseIdle, realtime.PhaseBackoff, realtime.PhaseStopped} {
		rt.st.Phase = ph
		if s := status(); !strings.Contains(s, "✕ 오프라인") {
			t.Fatalf("phase %v: status = %q", ph, s)
		}
	}
	h.Check()

	for _, tc := range []struct {
		w, h int
		hint string
	}{
		{200, 60, "ctrl+k 이동 · ctrl+t 도구 · tab 패널 전환 · t 스레드 · ? 도움말"}, // ctrl+t: the release turns the tools on (0.1.3 patch)
		{120, 30, "[ ] 탭 이동 · enter 스레드"},
		{80, 24, "tab 탭"},
	} {
		h.Resize(tc.w, tc.h)
		if s := status(); !strings.Contains(s, tc.hint) || !strings.Contains(s, h.Model.Frame().Layout.Mode.Label()) {
			t.Fatalf("%dx%d: status = %q, want hint %q", tc.w, tc.h, s, tc.hint)
		}
	}
	h.Resize(50, 20) // tiny: pill and connection dot only
	if s := status(); !strings.Contains(s, "초소형") || strings.Contains(s, "탭") || !strings.Contains(s, "✕") {
		t.Fatalf("tiny: status = %q", s)
	}
}

func TestStatusInputHintAndTruncation(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Press("i")
	lines := strings.Split(h.Screen(), "\n")
	if s := lines[len(lines)-1]; !strings.Contains(s, "enter 보내기 · shift+enter 줄바꿈 · esc 나가기") {
		t.Fatalf("capturing: status = %q", s)
	}
	h.Press("esc")
	h.Resize(100, 40) // narrow standard: everything still exactly 100 wide
	h.Check()
	lines = strings.Split(h.Screen(), "\n")
	if s := lines[len(lines)-1]; !strings.Contains(s, "● 온라인") {
		t.Fatalf("status = %q", s)
	}
}

func TestTabBars(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}})
	h.Resize(120, 30) // compact
	top := strings.Split(h.Screen(), "\n")[0]
	for _, want := range []string{"홈", "#개발", "#일반", "피드", "알림"} {
		if !strings.Contains(top, want) {
			t.Fatalf("compact tab bar %q lacks %q", top, want)
		}
	}
	if strings.Index(top, "#개발") > strings.Index(top, "#일반") || strings.Index(top, "#일반") > strings.Index(top, "피드") {
		t.Fatalf("compact tab order wrong: %q", top)
	}
	h.Resize(80, 24) // widget
	lines := strings.Split(h.Screen(), "\n")
	for _, want := range []string{"채팅", "피드", "알림"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("widget tab bar %q lacks %q", lines[0], want)
		}
	}
	if !strings.Contains(lines[1], "#일반 · 노바 개발방 ▾") {
		t.Fatalf("widget context line = %q", lines[1])
	}
	h.Resize(50, 20) // tiny
	top = strings.Split(h.Screen(), "\n")[0]
	for _, want := range []string{"챗", "피", "알"} {
		if !strings.Contains(top, want) {
			t.Fatalf("tiny tab bar %q lacks %q", top, want)
		}
	}
	h.Check()
}

func TestUnicodeCoreModeReport(t *testing.T) {
	h := uitest.New(t, uitest.WithUnicodeCore(true))
	if !h.Context().Caps.UnicodeCore {
		t.Fatal("WithUnicodeCore(true) => Caps.UnicodeCore must be true")
	}
	if uitest.New(t).Context().Caps.UnicodeCore {
		t.Fatal("default harness => Caps.UnicodeCore must be false")
	}
	h.Send(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset})
	if h.Context().Caps.UnicodeCore {
		t.Fatal("ModeReset report => Caps.UnicodeCore must be false again")
	}
}
