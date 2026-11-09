package app_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

var generalRoute = ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}

// devChat returns the stub instance of #개발.
func devChat(t *testing.T, h *uitest.Harness) *stub.Chat {
	t.Helper()
	c, ok := h.Instance(devRoute.Key()).(*stub.Chat)
	if !ok {
		t.Fatalf("Instance(%q) = %T, want *stub.Chat", devRoute.Key(), h.Instance(devRoute.Key()))
	}
	return c
}

// stubList returns the stub List instance for key.
func stubList(t *testing.T, h *uitest.Harness, key string) *stub.List {
	t.Helper()
	l, ok := h.Instance(key).(*stub.List)
	if !ok {
		t.Fatalf("Instance(%q) = %T, want *stub.List", key, h.Instance(key))
	}
	return l
}

func focusSlot(h *uitest.Harness) layout.SlotID { return h.Model.Focus().Slot }

func TestFocusRingAndTab(t *testing.T) {
	h := uitest.New(t)
	h.Resize(200, 60)
	if f := h.Model.Focus(); f != (app.FocusIntent{Slot: layout.SlotMain, RouteKey: "home//"}) {
		t.Fatalf("initial focus = %+v", f)
	}
	var changes []ui.FocusChangedMsg
	stubList(t, h, "home//").OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if m, ok := msg.(ui.FocusChangedMsg); ok {
			changes = append(changes, m)
		}
	}
	// CA 5.3.5 ring order (no thread open): rail → sidebar → main → feed → members → notifications.
	forward := []layout.SlotID{layout.SlotFeed, layout.SlotMembers, layout.SlotNotifications, layout.SlotRail, layout.SlotSidebar, layout.SlotMain}
	for i, want := range forward {
		h.Press("tab")
		if got := focusSlot(h); got != want {
			t.Fatalf("tab #%d: focus = %s, want %s", i+1, got, want)
		}
	}
	if len(changes) != len(forward) || changes[0] != (ui.FocusChangedMsg{Slot: layout.SlotFeed, RouteKey: "feed//following"}) ||
		changes[1] != (ui.FocusChangedMsg{Slot: layout.SlotMembers}) {
		t.Fatalf("FocusChangedMsg broadcasts = %+v", changes)
	}
	backward := []layout.SlotID{layout.SlotSidebar, layout.SlotRail, layout.SlotNotifications, layout.SlotMembers, layout.SlotFeed, layout.SlotMain}
	for i, want := range backward {
		h.Press("shift+tab")
		if got := focusSlot(h); got != want {
			t.Fatalf("shift+tab #%d: focus = %s, want %s", i+1, got, want)
		}
	}
	h.Check()

	// Compact: the ring has one pane, so tab is tab.next (Nav changes, focus stays on Main).
	h.Resize(120, 30)
	nav0 := h.Nav()
	h.Press("tab")
	if reflect.DeepEqual(h.Nav(), nav0) {
		t.Fatal("compact tab must act as tab.next (Nav unchanged)")
	}
	if !h.Nav().InDM || h.Nav().Front != app.FrontTab || focusSlot(h) != layout.SlotMain {
		t.Fatalf("compact tab: front %v in DM %v focus %s, want the DM tab / main", h.Nav().Front, h.Nav().InDM, focusSlot(h))
	}
	h.Press("shift+tab")
	if !reflect.DeepEqual(h.Nav().Front, nav0.Front) || h.Nav().InDM || h.Nav().Active != nav0.Active || focusSlot(h) != layout.SlotMain {
		t.Fatalf("compact shift+tab must go back: %+v", h.Nav())
	}
}

func TestCtrlWPrefix(t *testing.T) {
	h := uitest.New(t)
	h.Resize(200, 60)
	steps := []struct {
		key  string
		want layout.SlotID
	}{
		{"l", layout.SlotFeed},
		{"h", layout.SlotMain},
		{"right", layout.SlotFeed},
		{"l", layout.SlotMembers},
		{"j", layout.SlotNotifications},
		{"k", layout.SlotMembers},
		{"down", layout.SlotNotifications},
		{"left", layout.SlotFeed},
		{"w", layout.SlotMembers}, // next pane
		{"h", layout.SlotFeed},
		{"h", layout.SlotMain},
		{"h", layout.SlotSidebar},
		{"h", layout.SlotRail},
		{"h", layout.SlotRail}, // nothing further left
		{"w", layout.SlotSidebar},
		{"w", layout.SlotMain},
	}
	for i, s := range steps {
		h.Press("ctrl+w", s.key)
		if got := focusSlot(h); got != s.want {
			t.Fatalf("step %d ctrl+w %s: focus = %s, want %s", i, s.key, got, s.want)
		}
	}

	// Any other key cancels the prefix and is dropped (not delivered as a soft global).
	nav0 := h.Nav()
	h.Press("ctrl+w", "f")
	if !reflect.DeepEqual(h.Nav(), nav0) || focusSlot(h) != layout.SlotMain {
		t.Fatalf("ctrl+w f must be dropped: nav %+v focus %s", h.Nav(), focusSlot(h))
	}
	h.Press("ctrl+w", "esc")
	h.Press("l") // prefix gone: "l" is an ordinary key again (home stub ignores it)
	if focusSlot(h) != layout.SlotMain {
		t.Fatalf("cancelled prefix still active: focus %s", focusSlot(h))
	}

	// Pins: 1..5 pin a mode, 0 returns to auto.
	h.Press("ctrl+w", "2")
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeStandard {
		t.Fatalf("ctrl+w 2: mode = %v, want standard", m)
	}
	if s := statusRow(h); !strings.Contains(s, "📌") {
		t.Fatalf("pinned: status = %q, want 📌", s)
	}
	h.Check()
	for key, want := range map[string]layout.Mode{"3": layout.ModeCompact, "4": layout.ModeWidget, "5": layout.ModeTiny, "1": layout.ModeCommand} {
		h.Press("ctrl+w", key)
		if m := h.Model.Frame().Layout.Mode; m != want {
			t.Fatalf("ctrl+w %s: mode = %v, want %v", key, m, want)
		}
		h.Check()
	}
	h.Press("ctrl+w", "0")
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeCommand {
		t.Fatalf("ctrl+w 0: mode = %v, want command (auto)", m)
	}
	if s := statusRow(h); strings.Contains(s, "📌") {
		t.Fatalf("auto: status = %q still shows 📌", s)
	}
}

func TestCtrlWWhileTypingIsComposer(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	h.Press("i")
	ch := devChat(t, h)
	nav0, f0 := h.Nav(), h.Model.Focus()
	h.Press("ctrl+w")
	if !reflect.DeepEqual(h.Nav(), nav0) || h.Model.Focus() != f0 {
		t.Fatalf("ctrl+w while typing changed nav/focus: %+v / %+v", h.Nav(), h.Model.Focus())
	}
	h.Press("l") // no pending prefix: the letter is typed
	if v := ch.Composer.Value(); v != "l" {
		t.Fatalf("draft = %q, want %q (ctrl+w must not arm the window prefix while typing)", v, "l")
	}
	if h.Model.Focus() != f0 {
		t.Fatalf("focus moved: %+v", h.Model.Focus())
	}
}

func TestCapturedInputBlocksSoftGlobals(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Press("i")
	nav0, f0 := h.Nav(), h.Model.Focus()
	h.Type("f?[]")
	if v := devChat(t, h).Composer.Value(); v != "f?[]" {
		t.Fatalf("draft = %q, want %q", v, "f?[]")
	}
	if !reflect.DeepEqual(h.Nav(), nav0) || h.Model.Focus() != f0 {
		t.Fatalf("soft globals leaked: nav %+v focus %+v", h.Nav(), h.Model.Focus())
	}
	if n := len(app.OverlaysForTest(h.Model)); n != 0 {
		t.Fatalf("%d overlays opened while typing", n)
	}
}

func TestTabGoesToCompletingComponent(t *testing.T) { // CR-A (CA 5.3.4 2.3)
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}} { // multi-pane ring (focus.next) and single-pane ring (tab.next)
		t.Run(sizeName(sz), func(t *testing.T) {
			h := uitest.New(t, uitest.WithRoute(devRoute))
			h.Send(ui.NavigateMsg{Route: generalRoute}) // a second tab (#일반)
			h.Send(ui.NavigateMsg{Route: devRoute})     // back to #개발
			h.Resize(sz.W, sz.H)
			h.Press("i")
			ch := devChat(t, h)
			h.Type("@mi")
			ch.Completion = true
			nav0, focus0 := h.Nav(), h.Model.Focus()
			h.Press("tab")
			if !slices.Equal(ch.Completed, []string{"tab"}) {
				t.Fatalf("Completed = %v, want [tab]", ch.Completed)
			}
			if !reflect.DeepEqual(h.Nav(), nav0) || h.Model.Focus() != focus0 || ch.Composer.Value() != "@mi" {
				t.Fatalf("tab during completion changed state: nav %+v focus %+v draft %q", h.Nav(), h.Model.Focus(), ch.Composer.Value())
			}
			ch.Completion = true
			h.Press("shift+tab")
			if !slices.Equal(ch.Completed, []string{"tab", "shift+tab"}) {
				t.Fatalf("Completed = %v, want [tab shift+tab]", ch.Completed)
			}
			if !reflect.DeepEqual(h.Nav(), nav0) || h.Model.Focus() != focus0 {
				t.Fatalf("shift+tab during completion changed state")
			}

			// Popup closed: tab is the hard global again.
			h.Press("tab")
			if len(ch.Completed) != 2 {
				t.Fatalf("Completed = %v after the popup closed", ch.Completed)
			}
			if sz.W == 120 {
				if focusSlot(h) == layout.SlotMain {
					t.Fatal("120x40: tab must move focus off main (focus.next)")
				}
			} else {
				if n := h.Nav(); n.Front == nav0.Front && n.Active == nav0.Active && n.InDM == nav0.InDM {
					t.Fatal("80x24: tab must act as tab.next (active tab / Front unchanged)")
				}
			}
		})
		t.Run(sizeName(sz)+"/not_capturing", func(t *testing.T) {
			h := uitest.New(t, uitest.WithRoute(devRoute))
			h.Send(ui.NavigateMsg{Route: generalRoute})
			h.Send(ui.NavigateMsg{Route: devRoute})
			h.Resize(sz.W, sz.H)
			ch := devChat(t, h)
			ch.Completion = true // not capturing => Completing() == false
			nav0 := h.Nav()
			h.Press("tab")
			if len(ch.Completed) != 0 {
				t.Fatalf("Completed = %v: a non-capturing component must not get tab", ch.Completed)
			}
			if focusSlot(h) == layout.SlotMain && reflect.DeepEqual(h.Nav(), nav0) {
				t.Fatal("tab must be the hard global (focus.next / tab.next)")
			}
		})
	}
}

func sizeName(s uitest.Size) string { return fmt.Sprintf("%dx%d", s.W, s.H) }

// probeOverlay is a modal test overlay recording its keys.
type probeOverlay struct {
	keys  []string
	modal bool
}

func (p *probeOverlay) Init(*ui.Context) tea.Cmd            { return nil }
func (p *probeOverlay) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (p *probeOverlay) Resize(*ui.Context, ui.Size)         {}
func (p *probeOverlay) View(_ *ui.Context, _ bool) string   { return "" }
func (p *probeOverlay) Bounds(body layout.Rect) layout.Rect { return body }
func (p *probeOverlay) Modal() bool                         { return p.modal }
func (p *probeOverlay) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	p.keys = append(p.keys, msg.String())
	return false, nil
}

func TestHardGlobals(t *testing.T) {
	t.Run("ctrl+n compact", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Resize(120, 30)
		h.Press("ctrl+n")
		if h.Nav().Front != app.FrontNotifications || h.Model.Frame().Routes[layout.SlotMain].Key() != app.NotifRoute.Key() {
			t.Fatalf("ctrl+n: front %v main %v", h.Nav().Front, h.Model.Frame().Routes[layout.SlotMain])
		}
		h.Press("ctrl+n")
		if h.Nav().Front != app.FrontTab {
			t.Fatalf("second ctrl+n: front %v, want FrontTab", h.Nav().Front)
		}
	})
	t.Run("ctrl+n command", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Resize(200, 60)
		nav0 := h.Nav()
		h.Press("ctrl+n")
		if f := h.Model.Focus(); f != (app.FocusIntent{Slot: layout.SlotNotifications, RouteKey: app.NotifRoute.Key()}) {
			t.Fatalf("ctrl+n command: focus = %+v", f)
		}
		if !reflect.DeepEqual(h.Nav(), nav0) {
			t.Fatal("ctrl+n command must not change Nav")
		}
	})
	t.Run("ctrl+f", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Press("i") // hard global: works while typing too
		h.Press("ctrl+f")
		n := h.Nav()
		st := n.Tabs[n.Active].Stack
		if len(st) != 2 || st[0].Key() != devRoute.Key() || st[1].Kind != ui.RouteSearch {
			t.Fatalf("ctrl+f: active stack = %+v, want [#개발, search]", st)
		}
		if f := h.Model.Focus(); f.Slot != layout.SlotMain || f.RouteKey != st[1].Key() {
			t.Fatalf("ctrl+f focus = %+v", f)
		}
		if v := devChat(t, h).Composer.Value(); v != "" {
			t.Fatalf("ctrl+f leaked into the draft: %q", v)
		}
	})
	t.Run("ctrl+k", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		probe := &probeOverlay{modal: true}
		var initial []string
		app.SetPaletteOpenerForTest(h.Model, func(_ *ui.Context, in string) ui.Overlay {
			initial = append(initial, in)
			return probe
		})
		h.Press("i")
		h.Press("ctrl+k")
		if ov := app.OverlaysForTest(h.Model); len(ov) != 1 || ov[0] != ui.Overlay(probe) || !slices.Equal(initial, []string{""}) {
			t.Fatalf("ctrl+k: overlays %v, opener calls %q", ov, initial)
		}
		// the modal overlay now gets keys first; nothing reaches the composer below
		nav0 := h.Nav()
		h.Press("f", "j")
		if !slices.Equal(probe.keys, []string{"f", "j"}) || devChat(t, h).Composer.Value() != "" || !reflect.DeepEqual(h.Nav(), nav0) {
			t.Fatalf("overlay keys = %v, draft %q", probe.keys, devChat(t, h).Composer.Value())
		}
	})
}

// quitSeen reports whether a tea.QuitMsg was produced.
func quitSeen(h *uitest.Harness) bool {
	for _, m := range h.TeaMsgs() {
		if _, ok := m.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

const quitToast = "한 번 더 누르면 종료해요"

func TestCtrlCTwice(t *testing.T) {
	t.Run("capturing", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Press("i")
		h.Type("abc")
		ch := devChat(t, h)
		h.Press("ctrl+c")
		if v := ch.Composer.Value(); v != "" {
			t.Fatalf("first ctrl+c: draft = %q, want cleared", v)
		}
		if strings.Contains(h.Screen(), quitToast) || len(h.Clock().Pending()) != 0 || !ch.CapturesInput() {
			t.Fatalf("clearing the draft must not arm quit: pending %v capturing %v", h.Clock().Pending(), ch.CapturesInput())
		}
		h.Press("ctrl+c") // empty draft: the stub does not handle it => armed
		if !strings.Contains(h.Screen(), quitToast) {
			t.Fatalf("second ctrl+c: no %q toast", quitToast)
		}
		if quitSeen(h) {
			t.Fatal("quit after arming only")
		}
		h.Advance(500 * time.Millisecond)
		h.Press("ctrl+c")
		if !quitSeen(h) {
			t.Fatal("ctrl+c within 1s of arming must quit")
		}
	})
	t.Run("disarms", func(t *testing.T) {
		h := uitest.New(t)
		h.Press("ctrl+c")
		if !strings.Contains(h.Screen(), quitToast) {
			t.Fatal("not capturing: ctrl+c must arm immediately")
		}
		h.Advance(time.Second)
		h.Press("ctrl+c")
		if quitSeen(h) {
			t.Fatal("ctrl+c after the 1s window must arm again, not quit")
		}
		h.Advance(300 * time.Millisecond)
		h.Press("ctrl+c")
		if !quitSeen(h) {
			t.Fatal("re-armed ctrl+c must quit on the next press")
		}
	})
}

// probePane is a pane-factory component recording mouse calls.
type probePane struct{ mouse []stub.MouseCall }

func (p *probePane) Init(*ui.Context) tea.Cmd            { return nil }
func (p *probePane) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (p *probePane) Resize(*ui.Context, ui.Size)         {}
func (p *probePane) View(ctx *ui.Context, _ bool) string { return "" }
func (p *probePane) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	p.mouse = append(p.mouse, stub.MouseCall{Msg: msg, Local: local})
	return nil
}

func TestMouseClickFocusAndWheel(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	side := &probePane{}
	ui.RegisterPane(layout.SlotSidebar, func(*ui.Context) ui.Component { return side })

	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	l := h.Model.Frame().Layout
	feedPane, _ := l.Pane(layout.SlotFeed)
	feed := stubList(t, h, "feed//following")
	fr := feedPane.Rect
	h.Click(fr.X+5, fr.Y+3)
	if focusSlot(h) != layout.SlotFeed {
		t.Fatalf("click in feed: focus = %s", focusSlot(h))
	}
	if len(feed.Mouse) != 1 || feed.Mouse[0].Local != (ui.Point{X: 4, Y: 2}) {
		t.Fatalf("feed HandleMouse = %+v, want one call at local {4 2}", feed.Mouse)
	}
	if _, ok := feed.Mouse[0].Msg.(tea.MouseClickMsg); !ok {
		t.Fatalf("feed got %T, want MouseClickMsg", feed.Mouse[0].Msg)
	}

	// border click: focus moves, the component gets nothing
	mainPane, _ := l.Pane(layout.SlotMain)
	h.Click(mainPane.Rect.X, mainPane.Rect.Y+4)
	if focusSlot(h) != layout.SlotMain {
		t.Fatalf("border click on main: focus = %s", focusSlot(h))
	}
	h.Click(fr.X+fr.W-1, fr.Y+1)
	if focusSlot(h) != layout.SlotFeed || len(feed.Mouse) != 1 {
		t.Fatalf("border click on feed: focus %s, calls %d", focusSlot(h), len(feed.Mouse))
	}
	h.Click(mainPane.Rect.X+2, mainPane.Rect.Y+2)
	if focusSlot(h) != layout.SlotMain {
		t.Fatalf("click in main: focus = %s", focusSlot(h))
	}

	// wheel goes to the pane under the pointer; focus stays
	sp, _ := l.Pane(layout.SlotSidebar)
	h.Wheel(sp.Rect.X+3, sp.Rect.Y+4, 2)
	if focusSlot(h) != layout.SlotMain {
		t.Fatalf("wheel moved focus to %s", focusSlot(h))
	}
	if len(side.mouse) != 2 || side.mouse[0].Local != (ui.Point{X: 2, Y: 3}) {
		t.Fatalf("sidebar HandleMouse = %+v, want two wheel calls at local {2 3}", side.mouse)
	}
	if _, ok := side.mouse[1].Msg.(tea.MouseWheelMsg); !ok {
		t.Fatalf("sidebar got %T, want MouseWheelMsg", side.mouse[1].Msg)
	}
	h.Wheel(fr.X+3, fr.Y+3, -1)
	if len(feed.Mouse) != 2 || focusSlot(h) != layout.SlotMain {
		t.Fatalf("wheel over feed: calls %d focus %s", len(feed.Mouse), focusSlot(h))
	}

	// compact tab bar: clicking a tab selects it
	h.Resize(120, 30)
	top := strings.Split(h.Screen(), "\n")[0]
	i := strings.Index(top, "피드")
	if i < 0 {
		t.Fatalf("tab bar %q has no 피드", top)
	}
	h.Click(textwidth.Width(top[:i])+1, 0)
	if h.Nav().Front != app.FrontFeed {
		t.Fatalf("click on 피드 tab: front = %v", h.Nav().Front)
	}
	i = strings.Index(top, "#개발")
	h.Click(textwidth.Width(top[:i])+1, 0)
	if n := h.Nav(); n.Front != app.FrontTab || n.Tabs[n.Active].Stack[0].Key() != devRoute.Key() {
		t.Fatalf("click on #개발 tab: %+v", n)
	}
}

func TestCursorFollowsFocusedComposer(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	if c := h.Cursor(); c != nil {
		t.Fatalf("cursor before typing = %+v, want nil", c)
	}
	h.Press("i")
	ch := devChat(t, h)
	cc := ch.Cursor()
	if cc == nil {
		t.Fatal("stub composer cursor is nil while capturing")
	}
	inner := func() layout.Rect {
		p, _ := h.Model.Frame().Layout.Pane(layout.SlotMain)
		return p.Rect.Inner()
	}
	in := inner()
	got := h.Cursor()
	if got == nil || got.X != cc.X+in.X || got.Y != cc.Y+in.Y {
		t.Fatalf("cursor = %+v, want composer %+v + main inner origin (%d,%d)", got, cc.Position, in.X, in.Y)
	}
	h.Type("가나")
	if c := h.Cursor(); c == nil || c.X != got.X+4 || c.Y != got.Y {
		t.Fatalf("after 가나: cursor = %+v, want X %d", c, got.X+4)
	}
	before := ch.Cursor().Position
	h.Cursor()
	h.Cursor()
	if after := ch.Cursor().Position; after != before || after.X != cc.X+4 {
		t.Fatalf("View must not mutate the component's cursor: %+v -> %+v (typed from %+v)", before, after, cc.Position)
	}
	h.Resize(200, 60) // follows the pane across a mode change
	in = inner()
	if c, cc := h.Cursor(), ch.Cursor(); c == nil || c.X != cc.X+in.X || c.Y != cc.Y+in.Y {
		t.Fatalf("200x60 cursor = %+v, want %+v + (%d,%d)", c, cc, in.X, in.Y)
	}
	h.Press("esc")
	if c := h.Cursor(); c != nil {
		t.Fatalf("after esc: cursor = %+v, want nil", c)
	}
}

func TestFocusIntentFollowsRoute(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(120, 30)
	h.Press("enter") // thread of the selected message replaces Main (compact)
	n := h.Nav()
	thr := n.Tabs[n.Active].Thread
	if thr == nil {
		t.Fatal("enter did not open a thread")
	}
	if f := h.Model.Focus(); f != (app.FocusIntent{Slot: layout.SlotMain, RouteKey: thr.Key()}) {
		t.Fatalf("compact focus = %+v", f)
	}
	h.Resize(200, 60)
	if f := h.Model.Focus(); f != (app.FocusIntent{Slot: layout.SlotThread, RouteKey: thr.Key()}) {
		t.Fatalf("command focus = %+v, want the thread slot", f)
	}
	h.Resize(120, 30)
	if f := h.Model.Focus(); f != (app.FocusIntent{Slot: layout.SlotMain, RouteKey: thr.Key()}) {
		t.Fatalf("back to compact: focus = %+v", f)
	}
	h.Check()
}
