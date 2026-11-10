package app_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// overlayTypes is the dynamic type of every open overlay, bottom -> top.
func overlayTypes(m *app.Model) []string {
	var out []string
	for _, o := range app.OverlaysForTest(m) {
		out = append(out, fmt.Sprintf("%T", o))
	}
	return out
}

const paletteType = "*palette.overlay"

func TestPaletteOpenCloseRun(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest()) // forget the routes chosen here (recent list)
	h := uitest.New(t)
	h.Press("ctrl+k")
	if got := overlayTypes(h.Model); !reflect.DeepEqual(got, []string{paletteType}) {
		t.Fatalf("ctrl+k: overlays %v, want the palette", got)
	}
	h.Check()
	h.Press("ctrl+k")
	if got := overlayTypes(h.Model); len(got) != 0 {
		t.Fatalf("ctrl+k again must close the palette: %v", got)
	}
	h.Press("ctrl+k")
	h.Press("esc")
	if got := overlayTypes(h.Model); len(got) != 0 {
		t.Fatalf("esc must close the palette: %v", got)
	}

	h.Press("ctrl+k")
	h.Type("개발")
	h.Press("enter")
	if got := overlayTypes(h.Model); len(got) != 0 {
		t.Fatalf("enter on #개발 must close the palette: %v", got)
	}
	nav := h.Nav()
	if root := nav.Tabs[nav.Active].Stack[0]; root.Kind != ui.RouteChannel || root.ID != fixtures.ChDev {
		t.Fatalf("enter on #개발: active tab root %+v, want channel ChDev", root)
	}

	// The palette can be reopened after choosing (m.palette was forgotten).
	h.Press("ctrl+k")
	h.Type("도움말")
	h.Press("enter")
	if got := overlayTypes(h.Model); !reflect.DeepEqual(got, []string{"*app.helpView"}) {
		t.Fatalf("enter on 도움말: overlays %v, want only the help overlay", got)
	}
	h.Press("esc")
	h.Press("ctrl+k")
	if got := overlayTypes(h.Model); !reflect.DeepEqual(got, []string{paletteType}) {
		t.Fatalf("palette not reopened: %v", got)
	}

	// ↓ moves the selection; enter opens the selected item.
	h.Press("esc")
	h.Press("ctrl+k")
	h.Type("ㅁㅈ") // DM 민지 first, then the friend 민지
	h.Press("down", "enter")
	feed := h.Nav().Feed
	if top := feed[len(feed)-1]; top.Kind != ui.RouteProfile || top.ID != "@minji" {
		t.Fatalf("down+enter: feed top %+v, want profile @minji", top)
	}
}

func TestKeysOverrideOpensPalette(t *testing.T) {
	cfg := config.Defaults()
	cfg.Keys = map[string][]string{"palette.open": {"ctrl+p"}}
	m := newModel(t, cfg)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if n := len(m.Frame().Overlays); n != 1 || overlayTypes(m)[0] != paletteType {
		t.Fatalf("ctrl+p ([keys] palette.open) must open the palette: %v", overlayTypes(m))
	}
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if n := len(m.Frame().Overlays); n != 0 {
		t.Fatalf("ctrl+p in the palette must close it: %v", overlayTypes(m))
	}

	fresh := newModel(t, cfg)
	fresh.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	fresh.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if n := len(fresh.Frame().Overlays); n != 0 {
		t.Fatalf("ctrl+k must not open the palette once palette.open is overridden: %v", overlayTypes(fresh))
	}
}

func TestPaletteCursorAndPaste(t *testing.T) {
	dev := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
	h := uitest.New(t, uitest.WithRoute(dev))
	h.Press("i")
	h.Type("초안")
	chat := h.Instance(dev.Key()).(*stub.Chat)
	before := h.Cursor()
	if before == nil {
		t.Fatal("composer cursor missing")
	}

	h.Press("ctrl+k")
	h.Type("ㄱㅂ")
	boxes := h.Model.Frame().Overlays
	if len(boxes) != 1 || !boxes[0].Bordered {
		t.Fatalf("palette box = %+v", boxes)
	}
	r := boxes[0].Rect
	c := h.Cursor()
	wantX := r.X + 1 + textwidth.Width("› ") + textwidth.Width("ㄱㅂ")
	if c == nil || c.X != wantX || c.Y != r.Y+1 {
		t.Fatalf("palette cursor = %+v, want (%d,%d)", c, wantX, r.Y+1)
	}

	h.Paste("개발")
	if s := h.Screen(); !strings.Contains(s, "› ㄱㅂ개발") {
		t.Fatalf("paste must go to the palette query:\n%s", s)
	}
	if v := chat.Composer.Value(); v != "초안" {
		t.Fatalf("composer draft = %q, want 초안 (paste went below the overlay)", v)
	}
	h.Paste("\x1b[31m빨강\r\n")
	if s := h.Screen(); !strings.Contains(s, "› ㄱㅂ개발빨강") {
		t.Fatalf("paste must be sanitized to one line:\n%s", s)
	}
	h.Check()

	h.Press("ctrl+k")
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("ctrl+k must close the palette, %d open", n)
	}
	if after := h.Cursor(); after == nil || *after != *before {
		t.Fatalf("cursor after closing = %+v, want back on the composer %+v", after, before)
	}
}

func TestPaletteGolden(t *testing.T) {
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		h.Press("ctrl+k")
		h.Type("개")
		h.Golden("palette")
	})
}

func TestPaletteEmptyGolden(t *testing.T) {
	uitest.ForEachSize(t, uitest.Required, func(t *testing.T, h *uitest.Harness) {
		h.Press("ctrl+k")
		h.Golden("palette_empty")
	})
}

func TestRunCommandMsgDispatch(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest()) // keeps ui/app init() commands for later tests
	var calls []string
	avail := true
	palette.Register(palette.Command{
		ID: "core.test.echo", Title: "시험 echo", Group: palette.GroupCommand,
		Available: func(*ui.Context) bool { return avail },
		Run: func(_ *ui.Context, args string) tea.Cmd {
			calls = append(calls, args)
			return ui.Toast("ran:"+args, ui.ToastInfo)
		},
	})
	var nilCalls int
	palette.Register(palette.Command{ID: "core.test.nilavail", Title: "시험 nil", Group: palette.GroupCommand,
		Run: func(*ui.Context, string) tea.Cmd { nilCalls++; return nil }})
	palette.Register(palette.Command{ID: "core.test.norun", Title: "시험 norun", Group: palette.GroupCommand})

	dev := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
	h := uitest.New(t, uitest.WithRoute(dev))
	var seen int
	h.Instance(dev.Key()).(*stub.Chat).OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if _, ok := msg.(ui.RunCommandMsg); ok {
			seen++
		}
	}
	toastText := func() string { return h.Screen() }

	h.Send(ui.RunCommandMsg{ID: "core.test.echo", Args: "🎧 듣는 중 "})
	if len(calls) != 1 || calls[0] != "🎧 듣는 중 " {
		t.Fatalf("Run calls = %q, want exactly the raw args once", calls)
	}
	if !strings.Contains(toastText(), "ran:🎧 듣는 중") {
		t.Fatalf("the Run Cmd did not run (toast):\n%s", toastText())
	}

	navBefore := h.Nav()
	avail = false
	h.Send(ui.RunCommandMsg{ID: "core.test.echo"})
	if len(calls) != 1 {
		t.Fatalf("Available=false must not Run: %q", calls)
	}
	if !strings.Contains(toastText(), "지금은 쓸 수 없는 명령이에요") {
		t.Fatalf("Available=false toast missing:\n%s", toastText())
	}
	if !reflect.DeepEqual(h.Nav(), navBefore) || len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("Available=false must not change Nav or overlays")
	}

	h.Send(ui.RunCommandMsg{ID: "core.test.nilavail"})
	if nilCalls != 1 {
		t.Fatalf("nil Available must run: %d", nilCalls)
	}
	h.Send(ui.RunCommandMsg{ID: "core.test.norun"}) // nil Run: ignored, no panic

	teaBefore := len(h.TeaMsgs())
	screenBefore := h.Screen()
	h.Send(ui.RunCommandMsg{ID: "nope.x"})
	if !reflect.DeepEqual(h.Nav(), navBefore) || len(h.Model.Frame().Overlays) != 0 || len(h.TeaMsgs()) != teaBefore {
		t.Fatal("unknown id must change nothing")
	}
	if h.Screen() != screenBefore {
		t.Fatalf("unknown id must not toast:\n%s", h.Screen())
	}
	if seen != 0 {
		t.Fatalf("RunCommandMsg was broadcast to components %d times", seen)
	}

	// Run from an open palette: the palette closes first, the Confirm is not stacked on it.
	h.Press("ctrl+k")
	h.Send(ui.RunCommandMsg{ID: "logout"})
	// The release has tools on, so logout asks with the tool-login wipe option (P0 Task 23).
	if got := overlayTypes(h.Model); !reflect.DeepEqual(got, []string{"*widgets.confirmCheck"}) {
		t.Fatalf("logout from the palette: overlays %v, want only the Confirm", got)
	}
	h.Press("esc")
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("esc must close the Confirm: %v", overlayTypes(h.Model))
	}

	h.Press("ctrl+k")
	h.Type("로그아웃")
	h.Press("enter")
	if got := overlayTypes(h.Model); !reflect.DeepEqual(got, []string{"*widgets.confirmCheck"}) {
		t.Fatalf("palette enter on 로그아웃: overlays %v, want only the Confirm", got)
	}
	h.Press("esc")

	// palette enter re-checks Available through the same dispatch.
	avail = true
	h.Press("ctrl+k")
	h.Type("시험 echo")
	avail = false // listed while available, unavailable by the time it runs
	h.Press("enter")
	if len(calls) != 1 || !strings.Contains(toastText(), "지금은 쓸 수 없는 명령이에요") {
		t.Fatalf("palette enter must go through the dispatch (Available re-check): calls %q", calls)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("palette must be closed after enter: %v", overlayTypes(h.Model))
	}
	if seen != 0 {
		t.Fatalf("RunCommandMsg was broadcast to components %d times", seen)
	}
}

// TestPaletteHostileNames (pending-fixes 1): palette results built from other
// users' channel and display names, and a pasted query, show no bidi
// control, invisible format character or line separator (uitest check 3).
func TestPaletteHostileNames(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		fixtures.Hostile(s)
	}
	for _, sz := range uitest.AllSizes() {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := uitest.New(t, uitest.WithSeed(seed))
			h.Resize(sz.W, sz.H)
			h.Press("ctrl+k")
			h.Type("제어")
			bp := layout.DefaultBreakpoints()
			if s := h.Screen(); sz.W >= bp.MinCols && sz.H >= bp.MinRows && !strings.Contains(s, "제어문자") {
				t.Fatalf("no hostile channel in the results for 제어:\n%s", s)
			}
			h.Check()
			h.Paste(fixtures.FormatSoup)
			h.Check()
		})
	}
}

// The empty query lists an unread DM; choosing it opens the DM on the DM surface
// (CA 5.3.3), not as a tab.
func TestPaletteEmptyQueryDMOpensDMSurface(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	h := uitest.New(t)
	h.Resize(120, 40)
	s := h.Context().Store
	for _, ch := range s.Channels {
		ch.My.UnreadCount, ch.My.MentionCount = 0, 0
	}
	s.Channels[fixtures.DMMinji].My.UnreadCount = 2
	tabs := len(h.Nav().Tabs)
	h.Press("ctrl+k")
	h.Press("enter") // the first item: the only unread conversation
	nav := h.Nav()
	if !nav.InDM || len(nav.DM.Stack) < 2 || nav.DM.Stack[1].ID != fixtures.DMMinji {
		t.Fatalf("empty palette → unread DM: in DM %v, DM stack %+v", nav.InDM, nav.DM.Stack)
	}
	if len(nav.Tabs) != tabs {
		t.Fatalf("the DM opened a tab: %+v", nav.Tabs)
	}
}
