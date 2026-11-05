package app_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/widgets"
)

// rectOverlay is a test overlay asking for a fixed outer rect; it records its keys,
// sizes and mouse calls and draws Base blanks.
type rectOverlay struct {
	want    layout.Rect
	modal   bool
	keys    []string
	sizes   []ui.Size
	mouse   []ui.Point
	size    ui.Size
	inited  bool
	updates []tea.Msg
}

func (o *rectOverlay) Init(*ui.Context) tea.Cmd { o.inited = true; return nil }
func (o *rectOverlay) Update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	o.updates = append(o.updates, msg)
	return nil
}
func (o *rectOverlay) Resize(_ *ui.Context, s ui.Size) { o.size = s; o.sizes = append(o.sizes, s) }
func (o *rectOverlay) View(ctx *ui.Context, _ bool) string {
	base := ctx.Theme.S.Base
	lines := make([]string, max(0, o.size.H))
	for i := range lines {
		lines[i] = base.Render(strings.Repeat(" ", max(0, o.size.W)))
	}
	return strings.Join(lines, "\n")
}
func (o *rectOverlay) Bounds(layout.Rect) layout.Rect { return o.want }
func (o *rectOverlay) Modal() bool                    { return o.modal }
func (o *rectOverlay) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	o.keys = append(o.keys, msg.String())
	return false, nil
}
func (o *rectOverlay) HandleMouse(_ *ui.Context, _ tea.MouseMsg, local ui.Point) tea.Cmd {
	o.mouse = append(o.mouse, local)
	return nil
}

func openOverlay(h *uitest.Harness, o ui.Overlay) {
	h.T.Helper()
	h.Send(ui.OpenOverlayMsg{Overlay: o})
}

func overlayBoxes(h *uitest.Harness) []app.Box { return h.Model.Frame().Overlays }

func TestOverlayBoundsClampAndDegrade(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 40)
	body := layout.Rect{X: 0, Y: 0, W: 120, H: 39}

	huge := &rectOverlay{want: layout.Rect{X: -10, Y: -10, W: 1000, H: 1000}, modal: true}
	openOverlay(h, huge)
	if !huge.inited {
		t.Fatal("opened overlay was not Init'ed")
	}
	if got := overlayBoxes(h); !reflect.DeepEqual(got, []app.Box{{Rect: body, Bordered: true}}) {
		t.Fatalf("huge bounds: Overlays = %+v, want body bordered", got)
	}
	h.Check()
	h.Send(ui.CloseOverlayMsg{})

	box := &rectOverlay{want: layout.Rect{X: 30, Y: 10, W: 60, H: 12}, modal: true}
	openOverlay(h, box)
	if got := overlayBoxes(h); !reflect.DeepEqual(got, []app.Box{{Rect: box.want, Bordered: true}}) {
		t.Fatalf("60x12: Overlays = %+v", got)
	}
	if box.size != (ui.Size{W: 58, H: 10}) {
		t.Fatalf("60x12: inner size = %+v, want 58x10", box.size)
	}
	h.Check()
	h.Send(ui.CloseOverlayMsg{})

	small := &rectOverlay{want: layout.Rect{X: 50, Y: 10, W: 20, H: 5}, modal: true}
	openOverlay(h, small)
	if got := overlayBoxes(h); !reflect.DeepEqual(got, []app.Box{{Rect: body, Bordered: false}}) {
		t.Fatalf("20x5 (W<24): Overlays = %+v, want body unbordered", got)
	}
	if small.size != (ui.Size{W: 120, H: 39}) {
		t.Fatalf("degraded: size = %+v, want the body", small.size)
	}
	h.Check()
	h.Send(ui.CloseOverlayMsg{})

	short := &rectOverlay{want: layout.Rect{X: 10, Y: 10, W: 40, H: 5}, modal: true}
	openOverlay(h, short)
	if got := overlayBoxes(h); !reflect.DeepEqual(got, []app.Box{{Rect: body, Bordered: false}}) {
		t.Fatalf("40x5 (H<6): Overlays = %+v, want body unbordered", got)
	}
	h.Send(ui.CloseOverlayMsg{})

	tiny := &rectOverlay{want: layout.Rect{X: 5, Y: 5, W: 30, H: 8}, modal: true}
	openOverlay(h, tiny)
	h.Resize(50, 20)
	if got := overlayBoxes(h); !reflect.DeepEqual(got, []app.Box{{Rect: layout.Rect{W: 50, H: 19}, Bordered: false}}) {
		t.Fatalf("tiny 50x20: Overlays = %+v, want body unbordered", got)
	}
	h.Check()
	h.Resize(19, 10)
	if got := overlayBoxes(h); len(got) != 0 {
		t.Fatalf("TooSmall: Overlays = %+v, want none drawn", got)
	}
	if !strings.Contains(h.Screen(), textwidth.Truncate("창을 조금만 키워 주세요", 19, "…")) {
		t.Fatal("TooSmall must show only the notice")
	}
	if n := len(app.OverlaysForTest(h.Model)); n != 1 {
		t.Fatalf("TooSmall must keep the overlay open, have %d", n)
	}
	h.Resize(120, 40)
	if got := overlayBoxes(h); !reflect.DeepEqual(got, []app.Box{{Rect: tiny.want, Bordered: true}}) {
		t.Fatalf("back to 120x40: Overlays = %+v", got)
	}
	h.Check()
}

func TestOverlayStackAndModal(t *testing.T) {
	t.Run("max three", func(t *testing.T) {
		h := uitest.New(t)
		var ps []*rectOverlay
		for i := range 4 {
			p := &rectOverlay{want: layout.Rect{X: 10 + 4*i, Y: 5 + 2*i, W: 50, H: 12}, modal: true}
			ps = append(ps, p)
			openOverlay(h, p)
		}
		ov := app.OverlaysForTest(h.Model)
		if len(ov) != 3 || ov[2] != ui.Overlay(ps[2]) || ps[3].inited {
			t.Fatalf("4th push must be ignored: %d overlays, 4th inited %v", len(ov), ps[3].inited)
		}
		if got := overlayBoxes(h); len(got) != 3 || got[2].Rect != ps[2].want {
			t.Fatalf("Overlays = %+v", got)
		}
		h.Check()
		h.Press("j")
		if !slices.Equal(ps[2].keys, []string{"j"}) || len(ps[1].keys) != 0 {
			t.Fatalf("only the top overlay gets keys: top %v below %v", ps[2].keys, ps[1].keys)
		}
	})
	t.Run("modal swallows", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		ch := devChat(t, h)
		sel := selectedID(ch)
		nav0, focus0 := h.Nav(), h.Model.Focus()
		p := &rectOverlay{want: layout.Rect{X: 30, Y: 10, W: 60, H: 12}, modal: true}
		openOverlay(h, p)
		h.Press("j", "f", "]")
		if !slices.Equal(p.keys, []string{"j", "f", "]"}) {
			t.Fatalf("modal keys = %v", p.keys)
		}
		if selectedID(ch) != sel || !reflect.DeepEqual(h.Nav(), nav0) || h.Model.Focus() != focus0 {
			t.Fatal("a modal overlay must swallow unhandled keys")
		}
	})
	t.Run("non-modal passes", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Resize(120, 30) // compact: f toggles Front
		p := &rectOverlay{want: layout.Rect{X: 30, Y: 5, W: 60, H: 12}}
		openOverlay(h, p)
		h.Press("f")
		if !slices.Equal(p.keys, []string{"f"}) || h.Nav().Front != app.FrontFeed {
			t.Fatalf("non-modal: keys %v, Front %v (want FrontFeed)", p.keys, h.Nav().Front)
		}
		if len(app.OverlaysForTest(h.Model)) != 1 {
			t.Fatal("non-modal overlay must stay open")
		}
	})
	t.Run("esc closes top", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSettings}})
		nav0 := h.Nav()
		a := &rectOverlay{want: layout.Rect{X: 30, Y: 10, W: 60, H: 12}, modal: true}
		b := &rectOverlay{want: layout.Rect{X: 34, Y: 12, W: 50, H: 10}}
		openOverlay(h, a)
		openOverlay(h, b)
		h.Press("esc")
		if ov := app.OverlaysForTest(h.Model); len(ov) != 1 || ov[0] != ui.Overlay(a) {
			t.Fatalf("esc must close only the top overlay, left %d", len(ov))
		}
		h.Press("esc")
		if len(app.OverlaysForTest(h.Model)) != 0 || !reflect.DeepEqual(h.Nav(), nav0) {
			t.Fatal("esc on the last overlay must close it without going back")
		}
	})
	t.Run("mouse", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		focus0 := h.Model.Focus()
		p := &rectOverlay{want: layout.Rect{X: 30, Y: 10, W: 60, H: 12}, modal: true}
		openOverlay(h, p)
		h.Click(35, 13)
		if !slices.Equal(p.mouse, []ui.Point{{X: 4, Y: 2}}) || len(app.OverlaysForTest(h.Model)) != 1 {
			t.Fatalf("click inside: mouse %v", p.mouse)
		}
		h.Click(30, 10) // border: stays open, not passed on
		if len(p.mouse) != 1 || len(app.OverlaysForTest(h.Model)) != 1 {
			t.Fatalf("border click: mouse %v", p.mouse)
		}
		h.Click(2, 2)
		if len(app.OverlaysForTest(h.Model)) != 0 || h.Model.Focus() != focus0 {
			t.Fatal("click outside must close the top overlay and not reach the panes")
		}
	})
	t.Run("ctrl+c bypasses", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		p := &rectOverlay{want: layout.Rect{X: 30, Y: 10, W: 60, H: 12}, modal: true}
		openOverlay(h, p)
		h.Press("ctrl+c")
		if len(p.keys) != 0 {
			t.Fatalf("ctrl+c reached the overlay: %v", p.keys)
		}
		if !strings.Contains(h.Screen(), quitToast) {
			t.Fatal("ctrl+c with an overlay open must arm quitting")
		}
		if len(app.OverlaysForTest(h.Model)) != 1 {
			t.Fatal("ctrl+c must leave the overlay open")
		}
	})
	t.Run("ctrl+k closes and opens the palette", func(t *testing.T) {
		h := uitest.New(t)
		pal := &rectOverlay{want: layout.Rect{X: 20, Y: 5, W: 70, H: 20}, modal: true}
		app.SetPaletteOpenerForTest(h.Model, func(*ui.Context, string) ui.Overlay { return pal })
		openOverlay(h, widgets.ReportDialog("message", "M1", "민지의 메시지"))
		h.Press("ctrl+k")
		if ov := app.OverlaysForTest(h.Model); len(ov) != 1 || ov[0] != ui.Overlay(pal) {
			t.Fatalf("ctrl+k in a dialog: overlays %v, want only the palette", ov)
		}
		h.Press("ctrl+k") // unhandled by the palette probe: closes it, opens nothing
		if n := len(app.OverlaysForTest(h.Model)); n != 0 {
			t.Fatalf("ctrl+k on the palette must close it, %d open", n)
		}
		openOverlay(h, widgets.Confirm("지울까요?", "", "삭제", "취소", nil))
		if !strings.Contains(strings.Split(h.Screen(), "\n")[39], "y 삭제") {
			t.Fatal("status hints must come from the top overlay's Helper")
		}
	})
	t.Run("broadcast and registered", func(t *testing.T) {
		t.Cleanup(ui.SnapshotRegistryForTest())
		var args []string
		p := &rectOverlay{want: layout.Rect{X: 30, Y: 10, W: 60, H: 12}, modal: true}
		ui.RegisterOverlay("core.test.probe", func(_ *ui.Context, a string) ui.Overlay {
			args = append(args, a)
			return p
		})
		h := uitest.New(t)
		h.Send(ui.OpenOverlayMsg{ID: "core.test.nope", Args: "x"})
		if len(app.OverlaysForTest(h.Model)) != 0 {
			t.Fatal("unknown overlay id must be ignored")
		}
		h.Send(ui.OpenOverlayMsg{ID: "core.test.probe", Args: "a1"})
		if ov := app.OverlaysForTest(h.Model); len(ov) != 1 || ov[0] != ui.Overlay(p) || !slices.Equal(args, []string{"a1"}) {
			t.Fatalf("registered overlay: %d open, args %q", len(ov), args)
		}
		type ping struct{}
		h.Send(ping{})
		if !slices.ContainsFunc(p.updates, func(m tea.Msg) bool { _, ok := m.(ping); return ok }) {
			t.Fatal("open overlays must receive broadcast messages")
		}
	})
}

func TestOverlayPasteGoesToTop(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Press("i")
	h.Type("초안")
	openOverlay(h, widgets.ReportDialog("message", "M1", "민지의 메시지"))
	h.Press("tab") // reasons -> note
	h.Paste("광고 링크")
	if !strings.Contains(overlayText(h), "광고 링크") {
		t.Fatalf("paste did not reach the note:\n%s", overlayText(h))
	}
	if v := devChat(t, h).Composer.Value(); v != "초안" {
		t.Fatalf("paste leaked below the overlay: draft %q", v)
	}
}

func TestOverlayCursor(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Press("i") // the composer below has a cursor
	openOverlay(h, widgets.ReportDialog("message", "M1", "민지의 메시지"))
	if h.Cursor() != nil {
		t.Fatalf("reasons focused: cursor = %+v, want nil", h.Cursor())
	}
	h.Press("tab")
	h.Type("광고")
	ov := app.OverlaysForTest(h.Model)
	c := ov[len(ov)-1].(ui.Cursorer).Cursor()
	if c == nil {
		t.Fatal("note focused: dialog has no cursor")
	}
	r := overlayBoxes(h)[0].Rect
	got := h.Cursor()
	if got == nil || got.X != c.X+r.X+1 || got.Y != c.Y+r.Y+1 {
		t.Fatalf("cursor = %+v, want dialog %+v + (%d,%d)", got, c, r.X+1, r.Y+1)
	}
	h.Type("가")
	if got2 := h.Cursor(); got2 == nil || got2.X != got.X+2 {
		t.Fatalf("typing 가 must move the cursor 2 cells: %+v -> %+v", got, got2)
	}
	h.Send(ui.CloseOverlayMsg{})
	if h.Cursor() == nil {
		t.Fatal("closed: the composer cursor must come back")
	}
	openOverlay(h, widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소", nil))
	if h.Cursor() != nil {
		t.Fatalf("Confirm open: cursor = %+v, want nil", h.Cursor())
	}
}

// overlayText is the top overlay's inner area of the screen, rows joined by "\n".
func overlayText(h *uitest.Harness) string {
	boxes := overlayBoxes(h)
	if len(boxes) == 0 {
		return ""
	}
	b := boxes[len(boxes)-1]
	r := b.Rect
	if b.Bordered {
		r = r.Inner()
	}
	rows := strings.Split(h.Screen(), "\n")
	var out []string
	for y := r.Y; y < r.Y+r.H && y < len(rows); y++ {
		out = append(out, ansi.Cut(rows[y], r.X, r.X+r.W))
	}
	return strings.Join(out, "\n")
}

const (
	noticeEmoji = "이 클라이언트에서 입력한 글의 일부 이모지 수식자(피부색·결합 이모지 등)는 화면 폭을 맞추려고 정규화돼요."
	noticeWidth = "모호 폭 문자를 2칸으로 그리는 터미널 설정은 지원하지 않아요."
)

func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func TestHelpOverlay(t *testing.T) {
	h := uitest.New(t)
	h.Press("?")
	ov := app.OverlaysForTest(h.Model)
	if len(ov) != 1 {
		t.Fatalf("? opened %d overlays", len(ov))
	}
	tt, ok := ov[0].(ui.Titled)
	if !ok {
		t.Fatal("help overlay is not ui.Titled")
	}
	if title, _ := tt.Title(h.Context()); title != "도움말" {
		t.Fatalf("help title = %q", title)
	}
	b := overlayBoxes(h)[0]
	if want := (layout.Rect{X: 22, Y: 5, W: 76, H: 28}); b.Rect != want || !b.Bordered {
		t.Fatalf("help box = %+v, want %+v bordered", b, want)
	}
	if top := strings.Split(h.Screen(), "\n")[b.Rect.Y]; !strings.Contains(top, "도움말") {
		t.Fatalf("help border title missing: %q", top)
	}
	text := overlayText(h)
	for _, want := range []string{"ctrl+k", "이동"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help lacks %q:\n%s", want, text)
		}
	}
	for _, n := range []string{noticeEmoji, noticeWidth} {
		if !strings.Contains(squash(text), squash(n)) {
			t.Fatalf("help lacks the notice %q:\n%s", n, text)
		}
	}
	h.Check()
	h.Press("j")
	if after := overlayText(h); after == text {
		t.Fatal("j did not scroll the help")
	}
	h.Press("k")
	if back := overlayText(h); back != text {
		t.Fatal("k did not scroll the help back")
	}
	h.Press("G")
	if end := overlayText(h); !strings.Contains(end, "글쓰기") {
		t.Fatalf("G must show the last group (글):\n%s", end)
	}
	h.Check()
	h.Press("esc")
	if len(app.OverlaysForTest(h.Model)) != 0 {
		t.Fatal("esc must close the help")
	}
	h.Press("?")
	h.Press("?")
	if len(app.OverlaysForTest(h.Model)) != 0 {
		t.Fatal("? must toggle the help closed")
	}
	if _, ok := ui.Registered().Routes[ui.RouteHelp]; !ok {
		t.Fatal("ui/app must register RouteHelp")
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHelp}})
	if !strings.Contains(squash(h.Screen()), squash("ctrl+k")) {
		t.Fatal("help route must draw the key table")
	}
	h.Check()
}

func TestHelpGolden(t *testing.T) {
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) { h.Press("?"); h.Golden("help") })
}

var dialogSizes = append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6}, uitest.Size{W: 19, H: 10})

func TestDialogGoldens(t *testing.T) {
	for name, open := range map[string]func(h *uitest.Harness){
		"confirm": func(h *uitest.Harness) {
			h.Send(ui.OpenOverlayMsg{Overlay: widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소", nil)})
		},
		"emoji": func(h *uitest.Harness) { h.Send(ui.OpenOverlayMsg{Overlay: widgets.EmojiPicker(nil)}) },
		"report": func(h *uitest.Harness) {
			h.Send(ui.OpenOverlayMsg{Overlay: widgets.ReportDialog("message", "M1", "민지의 메시지")})
		},
	} {
		// one subtest per dialog: map order is random, so sizes must not share a parent
		t.Run(name, func(t *testing.T) {
			uitest.ForEachSize(t, dialogSizes, func(t *testing.T, h *uitest.Harness) { open(h); h.Golden(name) })
		})
	}
}
