package chat

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// Sending while an older page is shown: the latest page (read before the send
// committed) replaces the window, and the sent message is still shown.
func TestChannelSendWhileScrolledUpShown(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	c := chatOf(h.Context())
	w := c.Window(dev)
	w.Replace(w.Items(), true, true) // as if an older page were shown
	h.Press("i")
	h.Type("위에서 보낸 글")
	h.Press("enter")
	if n := len(reqs(h.API, "GET", "/v1/channels/"+dev+"/messages")); n < 2 {
		t.Fatalf("the latest page is fetched after sending (%d GETs)", n)
	}
	if !strings.Contains(h.Screen(), "위에서 보낸 글") {
		t.Fatalf("my message is shown:\n%s", h.Screen())
	}
	if p := c.Pending(dev); len(p) != 0 {
		t.Fatalf("no pending left: %+v", p)
	}
}

// A resync refetch that failed drops the window: the conversation says so and r retries.
func TestChannelResyncFailureRetries(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	c := chatOf(h.Context())
	h.API.Error("GET /v1/channels/"+dev+"/messages", http.StatusServiceUnavailable, "unavailable")
	cmd := c.Resync(h.Context().Store, h.Context().API)
	if cmd == nil {
		t.Fatal("the open channel is refetched")
	}
	h.Send(cmd())
	if c.Loaded(dev) {
		t.Fatal("the failed window is dropped")
	}
	h.Advance(5 * time.Second) // the error toast no longer covers the pane
	mustContain(t, h.Screen(), "메시지를 불러오지 못했어요")
	h.API.Handle("GET /v1/channels/"+dev+"/messages", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, pageBody(genMsgs(dev, 1, 3), false, false))
	})
	h.Press("r")
	if !c.Fresh(dev) || !strings.Contains(h.Screen(), "메시지 3") {
		t.Fatalf("r refetches:\n%s", h.Screen())
	}
}

// A resync result nobody applied (no conversation was alive) leaves the
// window stale: the next conversation view refetches it.
func TestChannelRefetchesStaleWindow(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	dev := chanID(t, seeded(), "개발")
	c := chatOf(h.Context())
	old := mkMsg(dev, 5, userBy(t, h.Context().Store, "minji"), "끊기기 전 글", h.Context().Now())
	c.ApplyPage(dev, api.MessageQuery{}, &api.MessageWindow{Items: []api.Message{old}})
	c.SetOpen(dev, "", true)
	if c.Resync(h.Context().Store, h.Context().API) == nil { // its result is never delivered
		t.Fatal("precondition: refetch started")
	}
	before := len(reqs(h.API, "GET", "/v1/channels/"+dev+"/messages"))
	h.Send(ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: dev})())
	if n := len(reqs(h.API, "GET", "/v1/channels/"+dev+"/messages")); n != before+1 {
		t.Fatalf("the stale window is refetched on open (%d → %d GETs)", before, n)
	}
	if !c.Fresh(dev) || strings.Contains(h.Screen(), "끊기기 전 글") || !strings.Contains(h.Screen(), "오늘 배포 몇 시에 해요?") {
		t.Fatalf("the fresh page replaced it:\n%s", h.Screen())
	}
}

// A released conversation stops being open (resync no longer refetches it);
// a dropped window of a live view is refetched after the resync.
func TestChannelReleaseClosesWindow(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	c := chatOf(h.Context())
	if !c.IsOpen(dev) {
		t.Fatal("open while shown")
	}
	v := channelOf(t, h)
	_ = v.SaveState() // the app releases the instance
	if c.IsOpen(dev) {
		t.Fatal("closed on release")
	}
	before := len(reqs(h.API, "GET", "/v1/channels/"+dev+"/messages"))
	if c.Resync(h.Context().Store, h.Context().API) != nil {
		t.Fatal("nothing open: nothing refetched")
	}
	if c.Loaded(dev) {
		t.Fatal("closed cache dropped")
	}
	h.Send(ui.ResyncDoneMsg{}) // the instance is still alive here: it reloads
	if n := len(reqs(h.API, "GET", "/v1/channels/"+dev+"/messages")); n != before+1 || !c.Loaded(dev) {
		t.Fatalf("a live view reloads its dropped window (%d → %d GETs)", before, n)
	}
}

// A released thread stops being open; a failed thread refetch offers the retry.
func TestThreadReleaseAndResyncFailure(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	m := selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("t")
	r, ok := threadRoute(h)
	if !ok {
		t.Fatal("thread open")
	}
	tv, ok := h.Instance(r.Key()).(*threadView)
	if !ok {
		t.Fatal("thread instance")
	}
	c := chatOf(h.Context())
	h.API.Error("GET /v1/messages/"+m.ID+"/thread", http.StatusServiceUnavailable, "unavailable")
	h.Send(c.Resync(h.Context().Store, h.Context().API)())
	if root, _ := c.Thread(m.ID); root != nil {
		t.Fatal("a failed thread refetch drops the thread")
	}
	if tv.pane.failed == "" {
		t.Fatal("the thread offers the retry")
	}
	_ = tv.SaveState() // the app releases the instance
	h.API.Handle("GET /v1/messages/"+m.ID+"/thread", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"root": m, "items": []api.Message{}, "has_more_before": false, "has_more_after": false})
	})
	threadGets := func() int { return len(reqs(h.API, "GET", "/v1/messages/"+m.ID+"/thread")) }
	before := threadGets()
	if cmd := c.Resync(h.Context().Store, h.Context().API); cmd != nil {
		_ = cmd() // the channel only
	}
	if threadGets() != before {
		t.Fatal("a released thread is not refetched")
	}
}
