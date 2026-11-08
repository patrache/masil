package stub_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

type nopTimers struct{}

func (nopTimers) After(time.Duration, tea.Msg) tea.Cmd { return nil }

func newCtx(t *testing.T, mode layout.Mode) *ui.Context {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	s := store.New()
	fixtures.Seed(s)
	now := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	return &ui.Context{
		Theme:  theme.Get(theme.Default),
		Mode:   mode,
		Keys:   keymap.Default(),
		Store:  s,
		Now:    func() time.Time { return now },
		Timers: nopTimers{},
		Loc:    loc,
	}
}

// install gives the test a fresh registry with the stubs installed.
func install(t *testing.T) ui.Registry {
	t.Helper()
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	stub.Install()
	stub.Install() // idempotent
	return ui.Registered()
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func press(t *testing.T, ctx *ui.Context, h ui.KeyHandler, s string) (bool, tea.Cmd) {
	t.Helper()
	if s != key(s).String() {
		t.Fatalf("key helper: %q prints as %q", s, key(s).String())
	}
	return h.HandleKey(ctx, key(s))
}

func typeText(t *testing.T, ctx *ui.Context, c *stub.Chat, s string) {
	t.Helper()
	for _, r := range s {
		if ok, _ := c.HandleKey(ctx, tea.KeyPressMsg{Code: r, Text: string(r)}); !ok {
			t.Fatalf("typing %q not handled", string(r))
		}
	}
}

func selID(c *stub.Chat) string {
	m, ok := c.List.Selected()
	if !ok {
		return ""
	}
	return m.ID
}

func checkView(t *testing.T, what, v string, size ui.Size) {
	t.Helper()
	lines := strings.Split(v, "\n")
	if len(lines) != size.H {
		t.Fatalf("%s: %d lines, want %d", what, len(lines), size.H)
	}
	for i, l := range lines {
		if w, wc := ansi.StringWidth(l), ansi.StringWidthWc(l); w != size.W || wc != size.W {
			t.Errorf("%s: line %d width %d/%d, want %d: %q", what, i, w, wc, size.W, ansi.Strip(l))
		}
	}
}

func newChat(t *testing.T, ctx *ui.Context, reg ui.Registry, r ui.Route) *stub.Chat {
	t.Helper()
	f := reg.Routes[r.Kind]
	if f == nil {
		t.Fatalf("no factory for %s", r.Kind)
	}
	c, ok := f(ctx, r).(*stub.Chat)
	if !ok {
		t.Fatalf("factory for %s did not build *stub.Chat", r.Kind)
	}
	c.Init(ctx)
	c.Resize(ctx, ui.Size{W: 60, H: 20})
	c.View(ctx, true)
	return c
}

func TestStubChat(t *testing.T) {
	reg := install(t)
	ctx := newCtx(t, layout.ModeStandard)
	route := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
	c := newChat(t, ctx, reg, route)
	c2 := newChat(t, ctx, reg, route)
	if c.Serial == 0 || c.Serial == c2.Serial {
		t.Fatalf("serials %d, %d not unique", c.Serial, c2.Serial)
	}
	if c.Route.Key() != route.Key() {
		t.Errorf("Route = %+v", c.Route)
	}
	ms := fixtures.Messages(fixtures.ChDev)

	// title + viewing
	if title, _ := c.Title(ctx); title != "#개발" {
		t.Errorf("title = %q", title)
	}
	if got := c.ViewingChannels(); !slices.Equal(got, []string{fixtures.ChDev}) {
		t.Errorf("ViewingChannels = %v", got)
	}
	gen := newChat(t, ctx, reg, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral})
	if title, badge := gen.Title(ctx); title != "#일반" || badge.Count != 3 {
		t.Errorf("#일반 title = %q %+v", title, badge)
	}

	// j/k move the list selection
	start := selID(c)
	if start == "" {
		t.Fatal("no initial selection")
	}
	if ok, _ := press(t, ctx, c, "k"); !ok {
		t.Fatal("k not handled")
	}
	up := selID(c)
	if up == start {
		t.Errorf("k did not move selection from %s", start)
	}
	if ok, _ := press(t, ctx, c, "j"); !ok || selID(c) != start {
		t.Errorf("j: handled=%v sel=%s, want %s", ok, selID(c), start)
	}
	press(t, ctx, c, "k")
	press(t, ctx, c, "k")
	press(t, ctx, c, "k")
	if selID(c) != ms[0].ID {
		t.Fatalf("selection = %s, want first message", selID(c))
	}

	// t (and enter) on the selected message navigates to its thread
	for _, k := range []string{"t", "enter"} {
		ok, cmd := press(t, ctx, c, k)
		if !ok || cmd == nil {
			t.Fatalf("%s: handled=%v cmd=%v", k, ok, cmd)
		}
		nav, isNav := cmd().(ui.NavigateMsg)
		if want := (ui.Route{Kind: ui.RouteThread, ID: ms[0].ID, Arg: fixtures.ChDev}); !isNav || nav.Route.Key() != want.Key() {
			t.Errorf("%s yields %#v, want NavigateMsg %+v", k, nav, want)
		}
	}

	// not capturing: esc and ctrl+c fall through, as do soft globals
	for _, k := range []string{"esc", "ctrl+c", "f", "?", "["} {
		if ok, _ := press(t, ctx, c, k); ok {
			t.Errorf("%s handled while not capturing", k)
		}
	}
	if c.CapturesInput() || c.Cursor() != nil {
		t.Error("capturing before i")
	}

	// i focuses the composer
	if ok, _ := press(t, ctx, c, "i"); !ok || !c.CapturesInput() {
		t.Fatalf("i: handled=%v capturing=%v", ok, c.CapturesInput())
	}
	checkView(t, "capturing", c.View(ctx, true), ui.Size{W: 60, H: 20})
	if c.Cursor() == nil {
		t.Fatal("Cursor nil while capturing")
	}
	typeText(t, ctx, c, "jk배포")
	if c.Composer.Value() != "jk배포" {
		t.Fatalf("draft = %q", c.Composer.Value())
	}
	if selID(c) != ms[0].ID {
		t.Error("typing j/k moved the selection")
	}

	// ctrl+c with a draft clears it, still capturing
	if ok, _ := press(t, ctx, c, "ctrl+c"); !ok || c.Composer.Value() != "" || !c.CapturesInput() {
		t.Errorf("ctrl+c with draft: handled=%v draft=%q capturing=%v", ok, c.Composer.Value(), c.CapturesInput())
	}
	if ok, _ := press(t, ctx, c, "ctrl+c"); ok {
		t.Error("ctrl+c on empty draft handled")
	}

	// enter sends: pending line appended, composer reset
	typeText(t, ctx, c, "abc")
	if ok, _ := press(t, ctx, c, "enter"); !ok || c.Composer.Value() != "" {
		t.Fatalf("enter: handled=%v draft=%q", ok, c.Composer.Value())
	}
	if !slices.Equal(c.Pending, []string{"abc"}) {
		t.Errorf("Pending = %v", c.Pending)
	}
	if !strings.Contains(ansi.Strip(c.View(ctx, true)), "abc") {
		t.Error("pending line not drawn")
	}

	// completion hook: tab / shift+tab go to the component while a popup is "open"
	c.Completion = true
	if !c.Completing() {
		t.Error("Completing false while capturing with Completion")
	}
	if ok, _ := press(t, ctx, c, "tab"); !ok || c.Completion || !slices.Equal(c.Completed, []string{"tab"}) {
		t.Errorf("tab: handled=%v completion=%v completed=%v", ok, c.Completion, c.Completed)
	}
	c.Completion = true
	if ok, _ := press(t, ctx, c, "shift+tab"); !ok || !slices.Equal(c.Completed, []string{"tab", "shift+tab"}) {
		t.Errorf("shift+tab: handled=%v completed=%v", ok, c.Completed)
	}
	if c.Completing() {
		t.Error("Completing true after accept")
	}

	// esc leaves the input (handled)
	if ok, _ := press(t, ctx, c, "esc"); !ok || c.CapturesInput() || c.Cursor() != nil {
		t.Errorf("esc capturing: handled=%v capturing=%v", ok, c.CapturesInput())
	}
	c.Completion = true
	if c.Completing() {
		t.Error("Completing true while not capturing")
	}
	if ok, _ := press(t, ctx, c, "tab"); ok {
		t.Error("tab handled while not capturing")
	}
	c.Completion = false

	// OnUpdate sees every message before Update handles it
	var seen []tea.Msg
	c.OnUpdate = func(gotCtx *ui.Context, msg tea.Msg) {
		if gotCtx != ctx {
			t.Error("OnUpdate got another ctx")
		}
		seen = append(seen, msg)
	}
	c.Update(ctx, ui.ResyncDoneMsg{})
	c.Update(ctx, ui.ModeChangedMsg{From: layout.ModeStandard, To: layout.ModeCompact})
	if len(seen) != 2 {
		t.Errorf("OnUpdate saw %d msgs", len(seen))
	}
	c.OnUpdate = nil
	c.Update(ctx, ui.ResyncDoneMsg{})

	// persistence round trip: draft + anchor + selection
	press(t, ctx, c, "j")
	press(t, ctx, c, "i")
	typeText(t, ctx, c, "배포 몇 시")
	c.View(ctx, true)
	saved := c.SaveState()
	d := newChat(t, ctx, reg, route)
	d.LoadState(saved)
	d.View(ctx, true)
	if d.Composer.Value() != "배포 몇 시" || selID(d) != selID(c) || d.List.Anchor() != c.List.Anchor() {
		t.Errorf("LoadState: draft %q sel %s anchor %+v, want %q %s %+v", d.Composer.Value(), selID(d), d.List.Anchor(), "배포 몇 시", selID(c), c.List.Anchor())
	}
	d.LoadState("garbage") // ignored, no panic

	// thread route
	th := newChat(t, ctx, reg, ui.Route{Kind: ui.RouteThread, ID: ms[0].ID, Arg: fixtures.ChDev})
	if got := th.ViewingChannels(); got != nil {
		t.Errorf("thread ViewingChannels = %v, want nil", got)
	}
	if title, _ := th.Title(ctx); title != "스레드" {
		t.Errorf("thread title = %q", title)
	}
	if selID(th) != ms[0].ID {
		t.Errorf("thread list selection = %q, want the root", selID(th))
	}
	if len(th.Help(ctx)) == 0 || len(c.Help(ctx)) == 0 {
		t.Error("no help bindings")
	}
}

func TestStubChatViews(t *testing.T) {
	reg := install(t)
	modes := []layout.Mode{layout.ModeCommand, layout.ModeStandard, layout.ModeCompact, layout.ModeWidget, layout.ModeTiny}
	sizes := []ui.Size{{W: 60, H: 20}, {W: 30, H: 8}, {W: 7, H: 3}, {W: 1, H: 1}, {W: 120, H: 40}}
	for _, m := range modes {
		ctx := newCtx(t, m)
		for _, route := range []ui.Route{
			{Kind: ui.RouteChannel, ID: fixtures.ChDev},
			{Kind: ui.RouteChannel, ID: fixtures.ChHostile},
			{Kind: ui.RouteThread, ID: fixtures.Messages(fixtures.ChDev)[0].ID, Arg: fixtures.ChDev},
		} {
			fixtures.Hostile(ctx.Store)
			c := newChat(t, ctx, reg, route)
			for _, capture := range []bool{false, true} {
				if capture {
					press(t, ctx, c, "i")
					typeText(t, ctx, c, "❄️ 첫눈")
				}
				for _, sz := range sizes {
					c.Resize(ctx, sz)
					checkView(t, fmt.Sprintf("density%d %s %dx%d capture=%v", ctx.Density(), route.Key(), sz.W, sz.H, capture), c.View(ctx, true), sz)
					if cur := c.Cursor(); cur != nil && (cur.Position.X < 0 || cur.Position.X >= sz.W || cur.Position.Y < 0 || cur.Position.Y >= sz.H) {
						t.Errorf("cursor %v outside %dx%d", cur.Position, sz.W, sz.H)
					}
				}
			}
		}
	}
}

func TestStubList(t *testing.T) {
	reg := install(t)
	ctx := newCtx(t, layout.ModeStandard)
	for kind, title := range map[ui.RouteKind]string{ui.RouteFeed: "피드", ui.RouteNotifications: "알림", ui.RouteHome: "홈"} {
		f := reg.Routes[kind]
		if f == nil {
			t.Fatalf("no factory for %s", kind)
		}
		l, ok := f(ctx, ui.Route{Kind: kind, Arg: "following"}).(*stub.List)
		if !ok {
			t.Fatalf("%s factory did not build *stub.List", kind)
		}
		other := f(ctx, ui.Route{Kind: kind}).(*stub.List)
		if l.Serial == 0 || l.Serial == other.Serial {
			t.Errorf("%s serials %d %d", kind, l.Serial, other.Serial)
		}
		l.Init(ctx)
		l.Resize(ctx, ui.Size{W: 20, H: 10})
		v := l.View(ctx, true)
		checkView(t, string(kind), v, ui.Size{W: 20, H: 10})
		if !strings.Contains(ansi.Strip(v), "row 1") {
			t.Errorf("%s view lacks row 1", kind)
		}
		if got, _ := l.Title(ctx); got != title {
			t.Errorf("%s title = %q, want %q", kind, got, title)
		}
		if s, ok := l.List.Selected(); !ok || s != "row 1" {
			t.Errorf("%s initial selection = %q", kind, s)
		}
		for range 3 {
			if ok, _ := l.HandleKey(ctx, key("j")); !ok {
				t.Fatal("j not handled")
			}
		}
		if s, _ := l.List.Selected(); s != "row 4" {
			t.Errorf("after jjj selection = %q", s)
		}
		l.HandleKey(ctx, key("k"))
		if s, _ := l.List.Selected(); s != "row 3" {
			t.Errorf("after k selection = %q", s)
		}
		if ok, _ := l.HandleKey(ctx, key("f")); ok {
			t.Error("f handled by stub list")
		}
		for range 40 {
			l.HandleKey(ctx, key("j"))
		}
		if s, _ := l.List.Selected(); s != "row 30" {
			t.Errorf("bottom selection = %q", s)
		}
		checkView(t, string(kind)+" bottom", l.View(ctx, true), ui.Size{W: 20, H: 10})

		// persistence
		saved := l.SaveState()
		n := f(ctx, ui.Route{Kind: kind}).(*stub.List)
		n.LoadState(saved)
		n.Resize(ctx, ui.Size{W: 20, H: 10})
		n.View(ctx, true)
		if s, _ := n.List.Selected(); s != "row 30" {
			t.Errorf("restored selection = %q", s)
		}

		var seen int
		l.OnUpdate = func(*ui.Context, tea.Msg) { seen++ }
		l.Update(ctx, ui.ResyncDoneMsg{})
		if seen != 1 {
			t.Errorf("OnUpdate called %d times", seen)
		}

		// mouse: recorded (local coords) and forwarded to the List (click selects the row)
		var mh ui.MouseHandler = l
		l.List.Select("row 1")
		l.View(ctx, true)
		click := tea.MouseClickMsg{X: 40, Y: 41, Button: tea.MouseLeft}
		mh.HandleMouse(ctx, click, ui.Point{X: 2, Y: 3})
		if len(l.Mouse) != 1 || l.Mouse[0].Local != (ui.Point{X: 2, Y: 3}) || l.Mouse[0].Msg != tea.MouseMsg(click) {
			t.Errorf("Mouse = %+v", l.Mouse)
		}
		if s, _ := l.List.Selected(); s == "row 1" {
			t.Errorf("click on row 3 not forwarded to the List (selection %q)", s)
		}
	}
}

func TestInstallAfterReset(t *testing.T) {
	install(t)
	ui.ResetRegistryForTest()
	stub.Install()
	reg := ui.Registered()
	for _, k := range []ui.RouteKind{ui.RouteChannel, ui.RouteThread, ui.RouteFeed, ui.RouteNotifications, ui.RouteHome} {
		if reg.Routes[k] == nil {
			t.Errorf("%s not registered after reset + Install", k)
		}
	}
}
