package app_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// ---- focus-summary probe (registered in TestMain as ui.OverlayFocusSummary) ----

// summaryProbe is the test stand-in for sns-fun's focus-summary overlay.
type summaryProbe struct {
	rectOverlay
	args string
}

// summaryOpened records the args of every summaryProbe built (newest last).
var summaryOpened []string

func newSummaryProbe(_ *ui.Context, args string) ui.Overlay {
	summaryOpened = append(summaryOpened, args)
	return &summaryProbe{rectOverlay: rectOverlay{want: layout.Rect{X: 2, Y: 2, W: 20, H: 5}, modal: true}, args: args}
}

// ---- helpers ----

func sp(s string) *string { return &s }

// mentionAlert is a fresh mention alert on channel ch (no thread).
func mentionAlert(id, ch string, created time.Time) api.Alert {
	return api.Alert{
		ID: id, Kind: api.AlertMention, Title: "민지님의 멘션", Body: "확인해 주세요",
		Target:    api.AlertTarget{Type: "message", ID: "M1", ChannelID: sp(ch), MessageID: sp("M1")},
		CreatedAt: created,
	}
}

func sendAlert(h *uitest.Harness, a api.Alert) {
	h.T.Helper()
	h.Event("alert", map[string]any{"alert": a})
}

func storeHasAlert(h *uitest.Harness, id string) bool {
	for _, a := range h.Context().Store.Alerts.Items() {
		if a.ID == id {
			return true
		}
	}
	return false
}

func hasAlertToast(h *uitest.Harness) bool {
	return strings.Contains(h.Screen(), "★")
}

// navShows reports whether nav now shows r on the surface navigate put it on.
func navShows(n app.NavState, r ui.Route) bool {
	t := n.Tabs[n.Active]
	switch r.Kind {
	case ui.RouteNotifications:
		return n.Front == app.FrontNotifications
	case ui.RouteChannel:
		return n.Front == app.FrontTab && reflect.DeepEqual(t.Stack[0], r)
	case ui.RouteThread:
		return n.Front == app.FrontTab && t.Thread != nil && reflect.DeepEqual(*t.Thread, r)
	}
	if n.Front == app.FrontFeed {
		return reflect.DeepEqual(n.Feed[len(n.Feed)-1], r)
	}
	return n.Front == app.FrontTab && reflect.DeepEqual(t.Stack[len(t.Stack)-1], r)
}

// ---- tests ----

func TestAlertPopsOSNotificationOnce(t *testing.T) {
	h := uitest.New(t)
	a := mentionAlert("AL1", fixtures.ChGeneral, h.Clock().Now())
	sendAlert(h, a)
	if len(h.Notify.Shown) != 1 {
		t.Fatalf("OS notifications = %d, want 1", len(h.Notify.Shown))
	}
	if got := h.Notify.Shown[0]; got.Title != a.Title || got.Body != a.Body || got.AlertID != "AL1" {
		t.Fatalf("notification = %+v", got)
	}
	if !hasAlertToast(h) || !strings.Contains(h.Screen(), "민지님의 멘션 — 확인해 주세요") {
		t.Fatalf("want a ToastAlert with title — body\n%s", h.Screen())
	}
	if !storeHasAlert(h, "AL1") {
		t.Fatal("Store.Alerts lacks the alert")
	}
	h.Check()
	// The same alert again (replay after reconnect): the ledger keeps it to one popup.
	sendAlert(h, a)
	if len(h.Notify.Shown) != 1 {
		t.Fatalf("replayed alert: OS notifications = %d, want still 1", len(h.Notify.Shown))
	}
}

func TestAlertSilentWhenViewingFocusedTerminal(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}))
	h.Send(tea.FocusMsg{})
	if !h.Context().Caps.FocusReports {
		t.Fatal("FocusMsg must set Caps.FocusReports")
	}
	sendAlert(h, mentionAlert("AL2", fixtures.ChDev, h.Clock().Now()))
	if len(h.Notify.Shown) != 0 {
		t.Fatalf("viewing + focused: OS notifications = %d, want 0", len(h.Notify.Shown))
	}
	if hasAlertToast(h) || h.Model.Frame().Toast != nil {
		t.Fatalf("viewing: no toast expected\n%s", h.Screen())
	}
	if !storeHasAlert(h, "AL2") {
		t.Fatal("Store.Alerts lacks the alert")
	}
}

func TestAlertBlurredStillPops(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}))
	h.Send(tea.FocusMsg{})
	h.Send(tea.BlurMsg{})
	sendAlert(h, mentionAlert("AL3", fixtures.ChDev, h.Clock().Now()))
	if len(h.Notify.Shown) != 1 {
		t.Fatalf("blurred: OS notifications = %d, want 1", len(h.Notify.Shown))
	}
	if hasAlertToast(h) {
		t.Fatalf("target visible: no toast expected\n%s", h.Screen())
	}
}

func TestAlertFocusUnknownPops(t *testing.T) {
	// No focus report ever arrived: the terminal might be in the background => popup.
	h := uitest.New(t, uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}))
	sendAlert(h, mentionAlert("AL4", fixtures.ChDev, h.Clock().Now()))
	if len(h.Notify.Shown) != 1 {
		t.Fatalf("focus unknown: OS notifications = %d, want 1", len(h.Notify.Shown))
	}
}

func TestAlertViewingThreadAndPost(t *testing.T) {
	var root string
	for _, m := range fixtures.Messages(fixtures.ChDev) {
		if m.Thread != nil {
			root = m.ID
		}
	}
	if root == "" {
		t.Fatal("fixture: no thread root in #개발")
	}
	thr := ui.Route{Kind: ui.RouteThread, ID: root, Arg: fixtures.ChDev}
	h := uitest.New(t, uitest.WithRoute(thr))
	h.Send(tea.FocusMsg{})
	reply := mentionAlert("AT1", fixtures.ChDev, h.Clock().Now())
	reply.Kind = api.AlertThreadReply
	reply.Target.ThreadRootID = sp(root)
	sendAlert(h, reply)
	if len(h.Notify.Shown) != 0 || hasAlertToast(h) {
		t.Fatalf("visible thread: shown=%d toast=%v", len(h.Notify.Shown), hasAlertToast(h))
	}
	other := reply
	other.ID, other.Target.ThreadRootID = "AT2", sp("OTHER")
	sendAlert(h, other)
	if len(h.Notify.Shown) != 1 || !hasAlertToast(h) {
		t.Fatalf("other thread: shown=%d toast=%v, want popup + toast", len(h.Notify.Shown), hasAlertToast(h))
	}

	// A visible Watcher topic "post:A" (TestMain's RouteTrends probe).
	h2 := uitest.New(t)
	h2.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTrends}})
	h2.Send(tea.FocusMsg{})
	like := api.Alert{ID: "AP1", Kind: api.AlertSocial, Title: "좋아요", Body: "글",
		Target: api.AlertTarget{Type: "post", ID: "A", PostID: sp("A")}, CreatedAt: h2.Clock().Now()}
	sendAlert(h2, like)
	if len(h2.Notify.Shown) != 0 || hasAlertToast(h2) {
		t.Fatalf("visible post: shown=%d toast=%v", len(h2.Notify.Shown), hasAlertToast(h2))
	}
}

func TestAlertDesktopOff(t *testing.T) {
	h := uitest.New(t)
	h.Context().Config.Notify.Desktop = false
	sendAlert(h, mentionAlert("AL5", fixtures.ChGeneral, h.Clock().Now()))
	if len(h.Notify.Shown) != 0 {
		t.Fatalf("desktop off: OS notifications = %d, want 0", len(h.Notify.Shown))
	}
	if !hasAlertToast(h) {
		t.Fatalf("desktop off: the in-app toast still shows\n%s", h.Screen())
	}
}

func TestOldReplayedAlertSilent(t *testing.T) {
	h := uitest.New(t)
	a := mentionAlert("AL6", fixtures.ChGeneral, h.Clock().Now().Add(-11*time.Minute))
	data, err := json.Marshal(map[string]any{"alert": a})
	if err != nil {
		t.Fatal(err)
	}
	h.Send(ui.EventMsg{Event: realtime.Event{Type: "alert", TS: h.Clock().Now(), Data: data, Replayed: true}})
	if len(h.Notify.Shown) != 0 {
		t.Fatalf("stale replay: OS notifications = %d, want 0", len(h.Notify.Shown))
	}
	if !storeHasAlert(h, "AL6") {
		t.Fatal("Store.Alerts lacks the stale alert")
	}
}

func TestToastAlertRoutes(t *testing.T) {
	type row struct {
		name   string
		kind   api.AlertKind
		target api.AlertTarget
		unreg  bool // ui.OverlayFocusSummary unregistered for this row
		want   ui.Route
	}
	const ch, msgID, root = fixtures.ChGeneral, "MSG1", "ROOT1"
	invites := []api.DirectInvite{
		{ID: "INV-A", Space: api.SpacePreview{ID: "SPACE-A"}, Status: "pending"},
		{ID: "INV-B", Space: api.SpacePreview{ID: "SPACE-B"}, Status: "pending"},
	}
	rows := []row{
		{"dm message", api.AlertDM, api.AlertTarget{Type: "message", ID: msgID, ChannelID: sp(fixtures.DMMinji), MessageID: sp(msgID)},
			false, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji, Anchor: msgID}},
		{"mention message", api.AlertMention, api.AlertTarget{Type: "message", ID: msgID, ChannelID: sp(ch), MessageID: sp(msgID)},
			false, ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: msgID}},
		{"channel_message", api.AlertChannelMessage, api.AlertTarget{Type: "message", ID: msgID, ChannelID: sp(ch), MessageID: sp(msgID)},
			false, ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: msgID}},
		{"thread_reply", api.AlertThreadReply, api.AlertTarget{Type: "message", ID: msgID, ChannelID: sp(ch), MessageID: sp(msgID), ThreadRootID: sp(root)},
			false, ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch, Anchor: msgID}},
		{"mention in a thread", api.AlertMention, api.AlertTarget{Type: "message", ID: msgID, ChannelID: sp(ch), MessageID: sp(msgID), ThreadRootID: sp(root)},
			false, ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch, Anchor: msgID}},
		{"channel target with channel_id", api.AlertChannelMessage, api.AlertTarget{Type: "channel", ID: ch, ChannelID: sp(ch)},
			false, ui.Route{Kind: ui.RouteChannel, ID: ch}},
		{"channel target id only", api.AlertChannelMessage, api.AlertTarget{Type: "channel", ID: ch},
			false, ui.Route{Kind: ui.RouteChannel, ID: ch}},
		{"post like (post_id)", api.AlertSocial, api.AlertTarget{Type: "post", ID: "P1", PostID: sp("P1")},
			false, ui.Route{Kind: ui.RoutePost, ID: "P1"}},
		{"post mention (id only)", api.AlertMention, api.AlertTarget{Type: "post", ID: "P2"},
			false, ui.Route{Kind: ui.RoutePost, ID: "P2"}},
		{"invite by space_id", api.AlertInvite, api.AlertTarget{Type: "space", ID: "SPACE-B", SpaceID: sp("SPACE-B")},
			false, ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: "INV-B"}},
		{"invite by target id", api.AlertInvite, api.AlertTarget{Type: "space", ID: "SPACE-A"},
			false, ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: "INV-A"}},
		{"invite no match", api.AlertInvite, api.AlertTarget{Type: "space", ID: "SPACE-Z", SpaceID: sp("SPACE-Z")},
			false, ui.Route{Kind: ui.RouteInvite, Arg: "direct"}},
		{"friend_request", api.AlertFriendRequest, api.AlertTarget{Type: "user", ID: fixtures.UserMinji},
			false, ui.Route{Kind: ui.RouteFriends}},
		{"follow accepted", api.AlertSocial, api.AlertTarget{Type: "user", ID: fixtures.UserJunho},
			false, ui.Route{Kind: ui.RouteProfile, ID: fixtures.UserJunho}},
		{"guestbook entry", api.AlertSocial, api.AlertTarget{Type: "user", ID: fixtures.UserSam},
			false, ui.Route{Kind: ui.RouteProfile, ID: fixtures.UserSam}},
		{"notification target", api.AlertSocial, api.AlertTarget{Type: "notification", ID: "N1"},
			false, app.NotifRoute},
		{"focus_summary unregistered", api.AlertFocusSummary, api.AlertTarget{Type: "message", ID: msgID, ChannelID: sp(ch), MessageID: sp(msgID)},
			true, app.NotifRoute},
		{"unknown target type", api.AlertSocial, api.AlertTarget{Type: "weird", ID: "X"},
			false, app.NotifRoute},
		{"message without channel", api.AlertMention, api.AlertTarget{Type: "message", MessageID: sp(msgID)},
			false, app.NotifRoute},
		{"channel without ids", api.AlertChannelMessage, api.AlertTarget{Type: "channel"},
			false, app.NotifRoute},
		{"post without id", api.AlertSocial, api.AlertTarget{Type: "post"},
			false, app.NotifRoute},
	}
	for i, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			if r.unreg {
				t.Cleanup(ui.SnapshotRegistryForTest())
				ui.ResetRegistryForTest()
				stub.Install()
			}
			h := uitest.New(t)
			h.Context().Store.DirectInvites = append([]api.DirectInvite(nil), invites...)
			before := h.Nav()
			a := api.Alert{ID: "R" + string(rune('A'+i)), Kind: r.kind, Title: "알림", Body: r.name, Target: r.target, CreatedAt: h.Clock().Now()}
			sendAlert(h, a)
			if !hasAlertToast(h) {
				t.Fatalf("no ToastAlert\n%s", h.Screen())
			}
			if !reflect.DeepEqual(h.Nav(), before) {
				t.Fatal("the alert itself must not navigate")
			}
			h.Check()
			h.Press("enter")
			if n := h.Nav(); !navShows(n, r.want) {
				t.Fatalf("after enter: nav = %+v, want it to show %+v", n, r.want)
			}
			if hasAlertToast(h) {
				t.Fatal("the opened toast is dismissed")
			}
		})
	}
}

func TestFocusSummaryOpensOnce(t *testing.T) {
	summaryOpened = nil
	h := uitest.New(t)
	sum := api.Alert{ID: "F1", Kind: api.AlertFocusSummary, Title: "집중 끝", Body: "놓친 알림 3개",
		Target: api.AlertTarget{Type: "notification", ID: "F1"}, CreatedAt: h.Clock().Now()}
	sendAlert(h, sum)
	if !reflect.DeepEqual(summaryOpened, []string{"F1"}) {
		t.Fatalf("summary opened = %v, want [F1]", summaryOpened)
	}
	ovs := app.OverlaysForTest(h.Model)
	if len(ovs) != 1 {
		t.Fatalf("overlays = %d, want 1", len(ovs))
	}
	if p, ok := ovs[0].(*summaryProbe); !ok || p.args != "F1" {
		t.Fatalf("top overlay = %T %+v, want the summary probe with args F1", ovs[0], ovs[0])
	}
	if hasAlertToast(h) || h.Model.Frame().Toast != nil {
		t.Fatalf("focus summary: overlay instead of a toast\n%s", h.Screen())
	}
	h.Check()

	h.Send(ui.CloseOverlayMsg{})
	sendAlert(h, sum) // replayed
	if len(summaryOpened) != 1 || len(app.OverlaysForTest(h.Model)) != 0 {
		t.Fatalf("same id must not reopen: opened=%v overlays=%d", summaryOpened, len(app.OverlaysForTest(h.Model)))
	}

	h.Event("focus.ended", map[string]any{"summary": api.FocusSummary{StartedAt: h.Clock().Now().Add(-time.Hour), EndedAt: h.Clock().Now()}})
	if len(summaryOpened) != 1 || len(app.OverlaysForTest(h.Model)) != 0 {
		t.Fatalf("focus.ended must not open anything: opened=%v", summaryOpened)
	}
	if h.Context().Store.LastFocusSummary == nil {
		t.Fatal("focus.ended updates Store.LastFocusSummary")
	}

	// A different summary id opens again; a stale replayed one does not.
	sum2 := sum
	sum2.ID = "F2"
	sendAlert(h, sum2)
	if !reflect.DeepEqual(summaryOpened, []string{"F1", "F2"}) {
		t.Fatalf("summary opened = %v, want [F1 F2]", summaryOpened)
	}
	h.Send(ui.CloseOverlayMsg{})
	old := sum
	old.ID, old.CreatedAt = "F3", h.Clock().Now().Add(-11*time.Minute)
	data, _ := json.Marshal(map[string]any{"alert": old})
	h.Send(ui.EventMsg{Event: realtime.Event{Type: "alert", TS: h.Clock().Now(), Data: data, Replayed: true}})
	if len(summaryOpened) != 2 {
		t.Fatalf("stale replayed summary must not open: %v", summaryOpened)
	}
}

func TestFocusSummaryUnregisteredFallsBackToToast(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	stub.Install()
	summaryOpened = nil
	h := uitest.New(t)
	sendAlert(h, api.Alert{ID: "F9", Kind: api.AlertFocusSummary, Title: "집중 끝", Body: "놓친 알림 1개",
		Target: api.AlertTarget{Type: "notification", ID: "F9"}, CreatedAt: h.Clock().Now()})
	if len(summaryOpened) != 0 || len(app.OverlaysForTest(h.Model)) != 0 {
		t.Fatalf("unregistered: no overlay expected (opened=%v)", summaryOpened)
	}
	if !hasAlertToast(h) || !strings.Contains(h.Screen(), "집중 끝") {
		t.Fatalf("unregistered: want a ToastAlert\n%s", h.Screen())
	}
}

// The ledger flock (up to 2s), its atomic write and the OS notifier (osascript
// on darwin) run inside a tea.Cmd, never on the Update goroutine.
func TestAlertOSNotificationOffUpdate(t *testing.T) {
	h := uitest.New(t)
	a := mentionAlert("ALCMD", fixtures.ChGeneral, h.Clock().Now())
	raw, err := json.Marshal(map[string]any{"alert": a})
	if err != nil {
		t.Fatal(err)
	}
	seq := int64(1)
	_, cmd := h.Model.Update(ui.EventMsg{Event: realtime.Event{Type: "alert", Seq: &seq, TS: h.Clock().Now(), Data: raw}})
	if len(h.Notify.Shown) != 0 {
		t.Fatal("Update showed the OS notification itself; it must return a Cmd")
	}
	if cmd == nil {
		t.Fatal("no Cmd for a popup alert")
	}
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		default:
			if msg != nil {
				h.Send(msg) // e.g. the result msg, logged by Update
			}
		}
	}
	run(cmd)
	if len(h.Notify.Shown) != 1 || h.Notify.Shown[0].AlertID != "ALCMD" {
		t.Fatalf("OS notifications after the Cmd = %+v, want ALCMD once", h.Notify.Shown)
	}
}
