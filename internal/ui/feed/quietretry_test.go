package feed

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// toastBox is the text of the toast box, "" when none is drawn.
func toastBox(h *uitest.Harness) string {
	tb := h.Model.Frame().Toast
	if tb == nil {
		return ""
	}
	return squash(strings.Join(region(h.Screen(), tb.Rect.Inner()), ""))
}

// expireToasts lets every shown toast run out (toastTTL is 4s).
func expireToasts(h *uitest.Harness) {
	h.Advance(5 * time.Second)
}

var errOffline = errors.New("dial tcp: connection refused")

// Offline, the bootstrap retries every 10s and broadcasts ResyncDoneMsg{Err}
// each time: the feed must not refetch (nor toast) on a failed resync.
func TestTimelineFailedResyncDoesNotRetryOrToast(t *testing.T) {
	h, _ := failingTimeline(t, "following", 1000)
	path := "/v1/timeline/following"
	if s := toastBox(h); !strings.Contains(s, "피드:") {
		t.Fatalf("the first failure must toast once, toast box %q", s)
	}
	expireToasts(h)
	base := len(reqsTo(h, "GET", path))
	for i := 0; i < 4; i++ {
		h.Send(ui.ResyncDoneMsg{Err: errOffline})
		if s := toastBox(h); s != "" {
			t.Fatalf("failed resync %d toasted %q", i, s)
		}
		h.Advance(10 * time.Second)
	}
	if n := len(reqsTo(h, "GET", path)); n != base {
		t.Fatalf("failed resyncs sent %d timeline requests, want 0", n-base)
	}
	if s := h.Screen(); !strings.Contains(s, "피드를 불러오지 못했어요") {
		t.Fatalf("the in-pane notice must stay:\n%s", s)
	}
}

// Automatic retries (successful resync, mode change, resize, being shown
// again) that fail again are silent: the in-pane notice says it; only the
// first failure and a user r toast.
func TestTimelineAutomaticRetriesAreSilent(t *testing.T) {
	h, _ := failingTimeline(t, "following", 1000)
	path := "/v1/timeline/following"
	expireToasts(h)
	n := len(reqsTo(h, "GET", path))
	step := func(name string, act func()) {
		t.Helper()
		act()
		got := len(reqsTo(h, "GET", path))
		if got != n+1 {
			t.Fatalf("%s: %d timeline requests, want 1 (retry)", name, got-n)
		}
		n = got
		if s := toastBox(h); s != "" {
			t.Fatalf("%s: a failed automatic retry toasted %q", name, s)
		}
		if s := h.Screen(); !strings.Contains(s, "피드를 불러오지 못했어요") {
			t.Fatalf("%s: the in-pane notice must stay:\n%s", name, s)
		}
		expireToasts(h)
	}
	step("resync", func() { h.Send(ui.ResyncDoneMsg{}) })
	step("resize", func() { h.Resize(100, 30) })
	step("mode change", func() { h.Send(ui.ModeChangedMsg{}) })
	step("reshow", func() {
		h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}})
		h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	})
	// the user's r says it again
	h.Press("r")
	if got := len(reqsTo(h, "GET", path)); got != n+1 {
		t.Fatalf("r: %d timeline requests, want 1", got-n)
	}
	if s := toastBox(h); !strings.Contains(s, "피드:") {
		t.Fatalf("a failed r must toast, toast box %q", s)
	}
}

// failingInbox makes the first `fails` GET /v1/notifications answer 503 and
// later ones answer `ok`.
func failingInbox(t *testing.T, fails int32, ok api.NotificationPage) (*uitest.Harness, *atomic.Int32) {
	t.Helper()
	srv := snsAPI(t, false)
	var left atomic.Int32
	left.Store(fails)
	srv.Handle("GET /v1/notifications", func(w http.ResponseWriter, r *http.Request) {
		if left.Add(-1) >= 0 {
			write503(w)
			return
		}
		writeJSON(w, ok)
	})
	return uitest.New(t, notifOptsWith(srv, false, nil)...), &left
}

// A failed first inbox page says so with the retry key (not a bare error),
// hides x (nothing is loaded to mark), and r retries.
func TestNotificationsFirstPageFailureRetries(t *testing.T) {
	h, _ := failingInbox(t, 1, api.NotificationPage{Items: notificationsAll(), UnreadCount: 2})
	path := "/v1/notifications"
	s := h.Screen()
	if !strings.Contains(s, "알림을 불러오지 못했어요") || !strings.Contains(s, "r 다시 시도") {
		t.Fatalf("failed inbox must offer r:\n%s", s)
	}
	if strings.Contains(s, "모두 읽음") {
		t.Fatalf("failed inbox must not offer x 모두 읽음:\n%s", s)
	}
	h.Check()
	before := len(h.API.Requests())
	h.Press("x")
	for _, r := range h.API.Requests()[before:] {
		if r.Path == "/v1/notifications/read" {
			t.Fatalf("x on a failed inbox marked everything read: %+v", r)
		}
	}
	h.Press("r")
	if n := len(reqsTo(h, "GET", path)); n != 2 {
		t.Fatalf("inbox requests after r = %d, want 2", n)
	}
	if s := h.Screen(); strings.Contains(s, "불러오지 못했어요") || len(NotifIDs(notifInstance(h))) != len(notificationsAll()) {
		t.Fatalf("retry must show the inbox:\n%s", s)
	}
}

// A cached inbox whose first page failed retries when it is shown again and
// after a successful resync, silently; a failed resync sends nothing.
func TestNotificationsFailureRetriesOnReshowAndResync(t *testing.T) {
	h, _ := failingInbox(t, 2, api.NotificationPage{Items: notificationsAll(), UnreadCount: 2})
	path := "/v1/notifications"
	expireToasts(h)
	for i := 0; i < 3; i++ {
		h.Send(ui.ResyncDoneMsg{Err: errOffline})
		h.Advance(10 * time.Second)
	}
	if n := len(reqsTo(h, "GET", path)); n != 1 {
		t.Fatalf("inbox requests after failed resyncs = %d, want 1", n)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	h.Send(ui.NavigateMsg{Route: app.NotifRoute})
	if n := len(reqsTo(h, "GET", path)); n != 2 {
		t.Fatalf("inbox requests after reopening = %d, want 2", n)
	}
	if s := toastBox(h); s != "" {
		t.Fatalf("a failed automatic retry toasted %q", s)
	}
	if s := h.Screen(); !strings.Contains(s, "알림을 불러오지 못했어요") {
		t.Fatalf("second failure must keep the notice:\n%s", s)
	}
	h.Send(ui.ResyncDoneMsg{})
	if n := len(reqsTo(h, "GET", path)); n != 3 {
		t.Fatalf("inbox requests after a resync = %d, want 3", n)
	}
	if got := len(NotifIDs(notifInstance(h))); got != len(notificationsAll()) {
		t.Fatalf("resync retry rows = %d, want %d:\n%s", got, len(notificationsAll()), h.Screen())
	}
	// loaded: reopening and resyncs do not refetch the first page
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	h.Send(ui.NavigateMsg{Route: app.NotifRoute})
	h.Send(ui.ResyncDoneMsg{})
	if n := len(reqsTo(h, "GET", path)); n != 3 {
		t.Fatalf("inbox requests once loaded = %d, want 3", n)
	}
}

// A resync that brings an (empty) inbox page replaces a stale load error.
func TestNotificationsResyncClearsStaleError(t *testing.T) {
	h, _ := failingInbox(t, 1, api.NotificationPage{Items: []api.Notification{}})
	if s := h.Screen(); !strings.Contains(s, "알림을 불러오지 못했어요") {
		t.Fatalf("want the failure notice first:\n%s", s)
	}
	ctx := h.Context()
	sendCmd(h, feedstore.Get(ctx.Store).Resync(ctx.Store, ctx.API))
	if n := len(reqsTo(h, "GET", "/v1/notifications")); n != 2 {
		t.Fatalf("inbox requests after the resync = %d, want 2", n)
	}
	s := h.Screen()
	if strings.Contains(s, "불러오지 못했어요") || !strings.Contains(s, textNotifEmpty) {
		t.Fatalf("a resynced empty inbox must show the empty state:\n%s", s)
	}
}
