package chat

import (
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// pinsPath is GET …/pins of channel ch.
func pinsPath(ch string) string { return "/v1/channels/" + ch + "/pins" }

// pinsText is the inside of the top overlay box (fails when none is open).
func pinsText(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	scr := h.Screen()
	os := h.Model.Frame().Overlays
	if len(os) == 0 {
		t.Fatalf("no overlay open:\n%s", scr)
	}
	return columnText(scr, os[len(os)-1].Rect) // border included: it carries the title
}

// openPins opens #개발 with pins served for it and runs pins.show.
func openPins(t *testing.T, sz uitest.Size, pins ...string) (*uitest.Harness, []api.Message) {
	t.Helper()
	h := devChannel(t, sz)
	ms := []api.Message{}
	for _, body := range pins {
		m := fixtureMsg(t, h, body)
		m.Pinned = true
		ms = append(ms, m)
	}
	h.API.JSON("GET "+pinsPath(devID(t, h)), http.StatusOK, map[string]any{"items": ms})
	h.Send(ui.RunCommandMsg{ID: "pins.show"})
	return h, ms
}

func TestPinsGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		// exact path on a fresh per-size server; neither RegisterAPI nor chatRoutes registers GET …/pins, so no conflict
		m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
		m.Pinned = true
		h.API.JSON("GET "+pinsPath(devID(t, h)), http.StatusOK, map[string]any{"items": []api.Message{m}})
		h.Send(ui.RunCommandMsg{ID: "pins.show"})
		h.Golden("pins")
	})
}

func TestPinsLoadsOnceAndTitle(t *testing.T) {
	h, _ := openPins(t, uitest.Size{W: 120, H: 40}, "오늘 배포 몇 시에 해요?", "@minji 점심 먹고 ㄱㄱ")
	dev := devID(t, h)
	if n := len(reqs(h.API, "GET", pinsPath(dev))); n != 1 {
		t.Fatalf("GET pins %d times, want 1", n)
	}
	scr := h.Screen()
	if !strings.Contains(scr, "📌 고정 메시지 2") {
		t.Fatalf("title:\n%s", scr)
	}
	mustContainInOrder(t, scr, "오늘 배포 몇 시에 해요?", "점심 먹고")
	if pins, loaded := chatOf(h.Context()).Pins(dev); !loaded || len(pins) != 2 {
		t.Fatalf("store pins %d loaded=%v", len(pins), loaded)
	}

	// loaded pins are shown at once, without another request
	h.Press("esc")
	if overlays(h) != 0 {
		t.Fatal("esc closes")
	}
	h.Send(ui.RunCommandMsg{ID: "pins.show"})
	if n := len(reqs(h.API, "GET", pinsPath(dev))); n != 1 || overlays(h) != 1 {
		t.Fatalf("reopen: GET pins %d times, %d overlays", n, overlays(h))
	}
	if !strings.Contains(h.Screen(), "📌 고정 메시지 2") {
		t.Fatalf("reopened:\n%s", h.Screen())
	}
}

func TestPinsJumpAndUnpin(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}

	// enter → overlay closed, Navigate RouteChannel Anchor=<id> (RouteUpdatedMsg on same instance → around= request)
	h, ms := openPins(t, sz, "오늘 배포 몇 시에 해요?")
	dev := devID(t, h)
	v := channelOf(t, h)
	h.Press("enter")
	if overlays(h) != 0 {
		t.Fatal("enter closes the overlay")
	}
	want := ui.Route{Kind: ui.RouteChannel, ID: dev, Anchor: ms[0].ID}
	if got := top(h); got.Key() != want.Key() || got.Anchor != want.Anchor {
		t.Fatalf("top %+v, want %+v", got, want)
	}
	if channelOf(t, h) != v {
		t.Fatal("the jump reuses the open conversation")
	}
	if q := lastQuery(h.API, "/v1/channels/"+dev+"/messages"); !strings.HasPrefix(q, "around="+ms[0].ID) {
		t.Fatalf("history query %q, want around=%s", q, ms[0].ID)
	}

	// owner "p" → DELETE /v1/messages/<id>/pin, row removed
	h, ms = openPins(t, sz, "오늘 배포 몇 시에 해요?", "@minji 점심 먹고 ㄱㄱ")
	h.Press("p")
	if n := len(reqs(h.API, "DELETE", "/v1/messages/"+ms[0].ID+"/pin")); n != 1 {
		t.Fatalf("DELETE pin %d times", n)
	}
	if txt := pinsText(t, h); !strings.Contains(txt, "📌 고정 메시지 1") || strings.Contains(txt, "오늘 배포 몇 시에 해요?") || !strings.Contains(txt, "점심 먹고") {
		t.Fatalf("after unpin:\n%s", txt)
	}
	if got, _ := chatOf(h.Context()).FindMessage(ms[0].ID); got.Pinned {
		t.Fatal("the held copy follows the unpin")
	}

	// member "p" → toast "고정할 권한이 없어요", no request
	h, ms = openPins(t, sz, "오늘 배포 몇 시에 해요?")
	setRole(h, api.RoleMember)
	h.Press("p")
	if tt := toastText(t, h); !strings.Contains(tt, "고정할 권한이 없어요") {
		t.Fatalf("toast %q", tt)
	}
	if n := len(reqs(h.API, "DELETE", "/v1/messages/"+ms[0].ID+"/pin")); n != 0 {
		t.Fatal("no request without permission")
	}
	if txt := pinsText(t, h); !strings.Contains(txt, "오늘 배포 몇 시에 해요?") {
		t.Fatalf("the row stays:\n%s", txt)
	}

	// empty → "고정된 메시지가 없어요"
	h, _ = openPins(t, sz)
	if scr := h.Screen(); !strings.Contains(scr, "고정된 메시지가 없어요") {
		t.Fatalf("empty:\n%s", scr)
	}
	h.Press("enter", "p") // nothing selected: no-ops
	if overlays(h) != 1 {
		t.Fatal("enter on an empty list keeps the overlay")
	}
}

func TestPinsFollowEvents(t *testing.T) {
	h, ms := openPins(t, uitest.Size{W: 120, H: 40}, "오늘 배포 몇 시에 해요?")
	dev := devID(t, h)
	other := fixtureMsg(t, h, "@minji 점심 먹고 ㄱㄱ")
	h.Event("message.pinned", map[string]any{"channel_id": dev, "message_id": other.ID, "pinned_by": fixtures.UserSam})
	txt := pinsText(t, h)
	if !strings.Contains(txt, "📌 고정 메시지 2") {
		t.Fatalf("a new pin is added:\n%s", txt)
	}
	mustContainInOrder(t, txt, "점심 먹고", "오늘 배포 몇 시에 해요?") // newest pin first
	h.Event("message.unpinned", map[string]any{"channel_id": dev, "message_id": ms[0].ID, "unpinned_by": fixtures.UserSam})
	if txt := pinsText(t, h); !strings.Contains(txt, "📌 고정 메시지 1") || strings.Contains(txt, "오늘 배포 몇 시에 해요?") {
		t.Fatalf("an unpin drops the row:\n%s", txt)
	}
}

func TestPinsLoadFailure(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.API.Error("GET "+pinsPath(devID(t, h)), http.StatusInternalServerError, "internal")
	h.Send(ui.RunCommandMsg{ID: "pins.show"})
	if scr := h.Screen(); !strings.Contains(scr, pinsFailed) {
		t.Fatalf("failure:\n%s", scr)
	}
	if _, loaded := chatOf(h.Context()).Pins(devID(t, h)); loaded {
		t.Fatal("a failed load stores nothing")
	}
}

func TestPinsShowNeedsChannel(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "pins.show"})
	if overlays(h) != 0 {
		t.Fatal("pins.show is unavailable without a viewed channel")
	}
	if newPins(h.Context(), "01JB0000000000000000NOSUCH") != nil {
		t.Fatal("an unknown channel opens nothing")
	}
}
