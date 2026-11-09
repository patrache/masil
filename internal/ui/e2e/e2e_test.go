package e2e

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// productStates are the shell states the product goldens and the sweeps open,
// in a fixed order (CA 10.5).
var productStates = []struct {
	name string
	nav  func(h *uitest.Harness)
}{
	{"home", func(*uitest.Harness) {}},
	{"dev", openDev},
	{"dev_thread", openDevThread},
	{"feed", openFeed},
	{"notifications", openNotifications},
}

// TestProductGoldens: home, #개발, #개발 + thread, feed, notifications with the
// real chat and sns-fun screens x AllSizes (testdata/TestProductGoldens/<WxH>/<state>.golden).
func TestProductGoldens(t *testing.T) {
	for _, sz := range uitest.AllSizes() {
		t.Run(sizeName(sz), func(t *testing.T) {
			for _, st := range productStates {
				h := newHarness(t, sz, productSeed)
				st.nav(h)
				h.Golden(st.name)
			}
		})
	}
}

// TestProductStatesShowRealScreens guards the goldens against silently
// recording stand-ins or load failures: each state shows its real content.
func TestProductStatesShowRealScreens(t *testing.T) {
	want := map[string][]string{
		"home":          {"피드 보기", "친구 목록"},
		"dev":           {"# 개발", "오늘 배포 몇 시에 해요?", "wrangler deploy --env prod", "점심 메뉴"},
		"dev_thread":    {"스레드", "답글 3개"},
		"feed":          {"오늘 첫 눈", "bubbletea v2 써보신 분?"},
		"notifications": {"하나님이 좋아함", "민지님이 멘션"},
	}
	for _, st := range productStates {
		t.Run(st.name, func(t *testing.T) {
			h := newHarness(t, uitest.Size{W: 200, H: 60}, productSeed)
			st.nav(h)
			s := h.Screen()
			for _, w := range want[st.name] {
				if !strings.Contains(s, w) {
					t.Fatalf("%s lacks %q:\n%s", st.name, w, s)
				}
			}
			for _, bad := range []string{"불러오지 못했어요", "불러오는 중", "서버에 연결할 수 없어요"} {
				if strings.Contains(s, bad) {
					t.Fatalf("%s shows %q:\n%s", st.name, bad, s)
				}
			}
		})
	}
	h := newHarness(t, uitest.Size{W: 200, H: 60}, productSeed)
	openDevThread(h)
	if thr := activeThread(h); thr == nil || thr.ID != devRootID(t) {
		t.Fatalf("dev_thread opened %+v, want the 민지 root %s", thr, devRootID(t))
	}
}

// preservation is the CA 10.3 scenario on the real #개발 screen: draft, scroll
// up, select a message.
type preservation struct {
	inst ui.Component // the #개발 instance
	sel  string       // selected message id
	text string       // a short piece of the selected message (visible check)
}

const devDraft = "배포 몇 시"

func preservationScenario(t *testing.T, h *uitest.Harness) preservation {
	t.Helper()
	openDev(h)
	h.Press("i")
	h.Type(devDraft)
	h.Press("esc")
	h.Press("ctrl+u") // scroll up
	h.Press("k")      // select a message
	p := preservation{inst: h.Instance(devRoute.Key())}
	if p.inst == nil {
		t.Fatal("no #개발 instance")
	}
	p.sel = probeSelection(t, h)
	if p.sel == "" {
		t.Fatal("k selected no message")
	}
	r := []rune(devMessage(t, p.sel).Body)
	p.text = string(r[:min(5, len(r))])
	return p
}

// assertPreserved checks (a) draft (b) selection (c) the selected message is
// on screen (e) same instance, then (d) integrity.
func assertPreserved(t *testing.T, h *uitest.Harness, p preservation) {
	t.Helper()
	if got := h.Instance(devRoute.Key()); got != p.inst {
		t.Fatalf("(e) #개발 instance changed: %p -> %p", p.inst, got)
	}
	if c, ok := p.inst.(ui.InputCapturer); ok && c.CapturesInput() {
		t.Fatal("composer still captures input")
	}
	s := h.Screen()
	if !strings.Contains(s, devDraft) {
		t.Fatalf("(a) draft %q not shown:\n%s", devDraft, s)
	}
	if !strings.Contains(s, p.text) {
		t.Fatalf("(c) selected message %q not on screen:\n%s", p.text, s)
	}
	if got := probeSelection(t, h); got != p.sel {
		t.Fatalf("(b) selection = %q, want %q", got, p.sel)
	}
	h.Check()
}

func TestModeSwitchPreservationReal(t *testing.T) {
	h := newHarness(t, uitest.Size{W: 200, H: 60}, productSeed)
	p := preservationScenario(t, h)
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}, {W: 200, H: 60}} {
		t.Run(sizeName(sz), func(t *testing.T) {
			h.Resize(sz.W, sz.H)
			h.Check() // (d)
			assertPreserved(t, h, p)
			if f := h.Model.Focus(); f.Slot != layout.SlotMain || f.RouteKey != devRoute.Key() {
				t.Fatalf("focus = %+v, want main/#개발", f)
			}
		})
	}

	// Pin (ctrl+w 1): applied at its minimum size, fallback notice below it.
	h.Press("ctrl+w", "1")
	h.Resize(160, 40)
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeCommand {
		t.Fatalf("pinned command at 160x40: mode = %v", m)
	}
	h.Check()
	h.Resize(150, 40)
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeStandard {
		t.Fatalf("pinned command at 150x40: mode = %v, want the auto fallback (standard)", m)
	}
	if s := statusRow(h); !strings.Contains(s, "창이 작아") {
		t.Fatalf("fallback notice missing: status = %q", s)
	}
	assertPreserved(t, h, p)
}

func TestCompactThreadToCommandReal(t *testing.T) { // CA 10.3 bullet 1, real screens
	h := newHarness(t, uitest.Size{W: 120, H: 30}, productSeed)
	if h.Context().Mode != layout.ModeCompact {
		t.Fatalf("120x30 = %v", h.Context().Mode)
	}
	openDev(h)
	ch := h.Instance(devRoute.Key())
	selectDevRoot(h)
	h.Press("enter")
	thr := activeThread(h)
	if thr == nil || thr.ID != devRootID(t) {
		t.Fatalf("enter: thread = %+v, want root %s", thr, devRootID(t))
	}
	mustSlot(t, h, layout.SlotMain, thr.Key())
	tc := h.Instance(thr.Key())
	if tc == nil {
		t.Fatal("no thread instance")
	}
	h.Press("i")
	h.Type("2시?")

	h.Resize(200, 60)
	h.Check()
	mustSlot(t, h, layout.SlotMain, devRoute.Key())
	mustSlot(t, h, layout.SlotThread, thr.Key())
	if h.Instance(thr.Key()) != tc || h.Instance(devRoute.Key()) != ch {
		t.Fatal("command: thread or channel instance replaced")
	}
	if !strings.Contains(h.Screen(), "2시?") {
		t.Fatalf("thread draft lost:\n%s", h.Screen())
	}
	if focusSlot(h) != layout.SlotThread {
		t.Fatalf("command focus = %s, want thread", focusSlot(h))
	}
	uniqueRouteKeys(t, h)

	h.Resize(120, 30)
	h.Check()
	mustSlot(t, h, layout.SlotMain, thr.Key())
	if h.Instance(thr.Key()) != tc || !strings.Contains(h.Screen(), "2시?") {
		t.Fatal("compact: the same thread instance with its draft must be back in Main")
	}
	if focusSlot(h) != layout.SlotMain {
		t.Fatalf("compact focus = %s", focusSlot(h))
	}
	h.Press("esc") // leave the thread composer
	h.Press("esc") // back: closes the thread
	if activeThread(h) != nil {
		t.Fatalf("esc esc: thread still open %+v", activeThread(h))
	}
	mustSlot(t, h, layout.SlotMain, devRoute.Key())
	if h.Instance(devRoute.Key()) != ch {
		t.Fatal("channel instance replaced")
	}
	if got := probeSelection(t, h); got != thr.ID {
		t.Fatalf("channel selection = %q, want the thread root %q", got, thr.ID)
	}
	h.Check()
}

func TestWidgetFeedToCommandReal(t *testing.T) { // CA 10.3 bullet 2, real screens
	h := newHarness(t, uitest.Size{W: 80, H: 24}, productSeed)
	openDev(h)
	if h.Context().Mode != layout.ModeWidget {
		t.Fatalf("80x24 = %v", h.Context().Mode)
	}
	h.Press("[", "[") // 알림, then the 피드 tab (not through DM: Main keeps the 채팅 tab)
	if h.Nav().Front != app.FrontFeed {
		t.Fatalf("[ [ at widget: front = %v", h.Nav().Front)
	}
	key := feedRoute.Key()
	mustSlot(t, h, layout.SlotMain, key)
	feed := h.Instance(key)
	first := savedSelected(t, feed)
	h.Press("j", "j", "j")
	sel := savedSelected(t, feed)
	if sel == "" || sel == first {
		t.Fatalf("j moved nothing: %q -> %q", first, sel)
	}

	h.Resize(200, 60)
	h.Check()
	mustSlot(t, h, layout.SlotFeed, key)
	mustSlot(t, h, layout.SlotMain, devRoute.Key())
	if h.Instance(key) != feed || savedSelected(t, feed) != sel {
		t.Fatalf("command: feed instance replaced or selection %q, want %q", savedSelected(t, h.Instance(key)), sel)
	}
	if focusSlot(h) != layout.SlotFeed {
		t.Fatalf("command focus = %s, want feed", focusSlot(h))
	}
	uniqueRouteKeys(t, h)

	h.Resize(80, 24)
	h.Check()
	if h.Nav().Front != app.FrontFeed {
		t.Fatalf("back at widget: front %v", h.Nav().Front)
	}
	mustSlot(t, h, layout.SlotMain, key)
	if h.Instance(key) != feed || savedSelected(t, feed) != sel || focusSlot(h) != layout.SlotMain {
		t.Fatalf("widget: same %v selection %q focus %s", h.Instance(key) == feed, savedSelected(t, feed), focusSlot(h))
	}
}

func TestStandardFeedSwappedAndNotificationsToCommandReal(t *testing.T) { // CA 10.3 bullet 3, real screens
	h := newHarness(t, uitest.Size{W: 120, H: 40}, productSeed)
	openDev(h)
	h.Press("f") // standard: feed swapped into Main
	key := feedRoute.Key()
	if h.Nav().Front != app.FrontFeed {
		t.Fatalf("f at standard: front %v", h.Nav().Front)
	}
	mustSlot(t, h, layout.SlotMain, key)
	feed := h.Instance(key)
	h.Press("j")
	sel := savedSelected(t, feed)
	h.Resize(200, 60)
	h.Check()
	uniqueRouteKeys(t, h)
	mustSlot(t, h, layout.SlotFeed, key)
	if h.Instance(key) != feed || savedSelected(t, feed) != sel || focusSlot(h) != layout.SlotFeed {
		t.Fatalf("feed in command: same %v selection %q focus %s", h.Instance(key) == feed, savedSelected(t, feed), focusSlot(h))
	}
	h.Resize(120, 40)
	mustSlot(t, h, layout.SlotMain, key)
	if savedSelected(t, feed) != sel {
		t.Fatal("standard again: feed selection lost")
	}

	h.Resize(120, 30)
	h.Press("ctrl+n")
	nk := app.NotifRoute.Key()
	mustSlot(t, h, layout.SlotMain, nk)
	notif := h.Instance(nk)
	h.Resize(200, 60)
	h.Check()
	uniqueRouteKeys(t, h)
	mustSlot(t, h, layout.SlotNotifications, nk)
	if h.Instance(nk) != notif || focusSlot(h) != layout.SlotNotifications {
		t.Fatalf("command: notifications same %v focus %s", h.Instance(nk) == notif, focusSlot(h))
	}
	h.Resize(120, 30)
	mustSlot(t, h, layout.SlotMain, nk)
	if h.Instance(nk) != notif {
		t.Fatal("compact again: notifications instance replaced")
	}
}

func TestRandomResizeReal(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			h := newHarness(t, uitest.Size{W: 200, H: 60}, productSeed)
			p := preservationScenario(t, h)
			rng := rand.New(rand.NewPCG(seed, seed))
			for i := range 200 {
				w, hh := 1+rng.IntN(330), 1+rng.IntN(100)
				h.Resize(w, hh)
				h.Check()
				if t.Failed() {
					t.Fatalf("step %d: %dx%d", i, w, hh)
				}
			}
			h.Resize(200, 60)
			assertPreserved(t, h, p)
		})
	}
}

// hostileThreadRoot is the hostile message with 9,999 replies.
func hostileThreadRoot(t *testing.T) string {
	t.Helper()
	for _, m := range fixtures.HostileMessages(fixtures.ChHostile) {
		if m.Thread != nil && m.Thread.ReplyCount == 9999 {
			return m.ID
		}
	}
	t.Fatal("no hostile thread root")
	return ""
}
