package feed

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// hostileNotifications is one notification per fixtures.HostileTimeline item:
// hostile actors, the post bodies as previews, a hostile space name.
func hostileNotifications() []api.Notification {
	types := []api.NotificationType{"post_like", "message_mention", "space_invite", "post_reply", "follow_request", "guestbook_entry"}
	var out []api.Notification
	for i, it := range fixtures.HostileTimeline() {
		actor := it.Post.Author
		if it.RepostedBy != nil {
			actor = *it.RepostedBy
		}
		out = append(out, api.Notification{
			ID:      sid(60 + i),
			Type:    types[i%len(types)],
			Actor:   ptr(actor),
			Subject: api.NotificationSubject{Type: "post", ID: it.Post.ID},
			Preview: it.Post.Body,
			Context: api.NotificationContext{PostID: ptr(it.Post.ID), ChannelID: ptr(fixtures.ChGeneral),
				SpaceID: ptr(fixtures.SpaceNova), SpaceName: ptr("제어\x1b[31m빨강\u202e" + strings.Repeat("긴방이름", 20))},
			CreatedAt: at(i),
		})
	}
	return out
}

// notifOptsWith opens r (default RouteNotifications) on srv.
func notifOptsWith(srv *apitest.Server, hostile bool, r *ui.Route) []uitest.Option {
	seed := fixtures.Seed
	if hostile {
		seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }
	}
	route := ui.Route{Kind: ui.RouteNotifications}
	if r != nil {
		route = *r
	}
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(route)}
}

// notifOpts is the canned sns server (hostile: plus hostileNotifications) opening r.
func notifOpts(t *testing.T, hostile bool, r *ui.Route) []uitest.Option {
	t.Helper()
	srv := snsAPI(t, hostile)
	if hostile {
		items := append(hostileNotifications(), notificationsAll()...)
		srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: items, UnreadCount: 9999})
	}
	return notifOptsWith(srv, hostile, r)
}

func notifInstance(h *uitest.Harness) ui.Component {
	return h.Instance(app.NotifRoute.Key())
}

func TestNotificationsGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "notifications", notifOpts(t, false, nil), nil)

	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(200, 60)
	if got := h.Model.Frame().Routes[layout.SlotNotifications]; got.Kind != ui.RouteNotifications {
		t.Fatalf("200x60 SlotNotifications = %+v, want RouteNotifications", got)
	}
	if _, ok := lastReq(h, "GET", "/v1/notifications"); !ok {
		t.Fatalf("no GET /v1/notifications; requests %+v", h.API.Requests())
	}
}

func TestNotificationsHostile(t *testing.T) {
	sweep(t, uitest.AllSizes(), "", notifOpts(t, true, nil), nil)
}

func TestNotificationsEmpty(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: []api.Notification{}})
	h := uitest.New(t, notifOptsWith(srv, false, nil)...)
	if s := h.Screen(); !strings.Contains(s, "새 알림이 없어요") {
		t.Fatalf("empty inbox state missing:\n%s", s)
	}
	h.Check()
}

func TestNotificationsCommandPane(t *testing.T) {
	h := uitest.New(t, notifOpts(t, false, &ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev})...)
	h.Resize(200, 60)
	f := h.Model.Frame()
	if got := f.Routes[layout.SlotNotifications]; got.Kind != ui.RouteNotifications {
		t.Fatalf("SlotNotifications = %+v, want RouteNotifications", got)
	}
	pane, ok := f.Layout.Pane(layout.SlotNotifications)
	if !ok {
		t.Fatal("no notifications pane at 200x60")
	}
	s := h.Screen()
	body := strings.Join(region(s, pane.Rect.Inner()), "\n")
	for _, want := range []string{"♥ 하나님이 좋아함", "@ 민지님이 멘션"} {
		if !strings.Contains(body, want) {
			t.Fatalf("notifications pane lacks %q:\n%s", want, body)
		}
	}
	if top := region(s, pane.Rect)[0]; !strings.Contains(top, "알림") || !strings.Contains(top, "2") {
		t.Fatalf("notifications pane title %q, want 알림 with badge 2", top)
	}
	if i, j := strings.Index(body, "하나님이 좋아함"), strings.Index(body, "민지님이 멘션"); i < 0 || j < 0 || i > j {
		t.Fatalf("newest first: like (%d) must precede mention (%d):\n%s", i, j, body)
	}
}

func TestNotificationsWideFormat(t *testing.T) {
	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(120, 40)
	s := h.Screen()
	for _, want := range []string{"● ♥ 하나님이 내 글을 좋아해요 · \"주말에 캠핑", "@ 민지님이 노바 개발방에서 나를 멘션했어요", "2분"} {
		if !strings.Contains(s, want) {
			t.Fatalf("wide inbox lacks %q:\n%s", want, s)
		}
	}
}

func TestNotificationTargets(t *testing.T) {
	minji := userOf("minji").ID
	hanaPost := postBy("hana").ID
	general := fixtures.Messages(fixtures.ChGeneral)[2].ID
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	toPost := ui.Route{Kind: ui.RoutePost, ID: hanaPost}
	toMinji := ui.Route{Kind: ui.RouteProfile, ID: minji}
	want := map[api.NotificationType]struct {
		r       ui.Route
		overlay string
	}{
		"follow":          {r: toMinji},
		"follow_request":  {overlay: "feed.follow_requests"},
		"follow_accepted": {r: toMinji},
		"friend_request":  {r: ui.Route{Kind: ui.RouteFriends}},
		"friend_accepted": {r: toMinji},
		"space_invite":    {r: ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: sid(94)}},
		"post_like":       {r: toPost},
		"post_repost":     {r: toPost},
		"post_reply":      {r: toPost},
		"post_quote":      {r: toPost},
		"post_mention":    {r: toPost},
		"message_mention": {r: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral, Anchor: general}},
		"thread_reply":    {r: ui.Route{Kind: ui.RouteThread, ID: devRoot, Arg: fixtures.ChDev, Anchor: devThreadReply3}},
		"guestbook_entry": {r: ui.Route{Kind: ui.RouteProfile, ID: "@me", Query: map[string]string{"tab": "guestbook"}}},
	}
	all := notificationsAll()
	if len(all) != 16 {
		t.Fatalf("notificationsAll() = %d items, want 2 + 14", len(all))
	}
	seen := map[api.NotificationType]bool{}
	for _, n := range all[2:] {
		w, ok := want[n.Type]
		if !ok {
			t.Fatalf("unexpected type %q", n.Type)
		}
		seen[n.Type] = true
		r, overlay, ok := notifTarget(n)
		if !ok || overlay != w.overlay || !reflect.DeepEqual(r, w.r) {
			t.Errorf("%s: notifTarget = %+v %q %v, want %+v %q true", n.Type, r, overlay, ok, w.r, w.overlay)
		}
	}
	if len(seen) != 14 {
		t.Fatalf("covered %d types, want 14", len(seen))
	}
	// the fixtures pair
	if r, _, ok := notifTarget(all[0]); !ok || r.Kind != ui.RoutePost || r.ID != *all[0].Context.PostID {
		t.Errorf("fixtures like -> %+v %v", r, ok)
	}
	if r, _, ok := notifTarget(all[1]); !ok || r.Kind != ui.RouteChannel || r.ID != fixtures.ChChat || r.Anchor != *all[1].Context.MessageID {
		t.Errorf("fixtures mention -> %+v %v", r, ok)
	}
	// missing context: nowhere to go
	bare := all[8]
	bare.Context = api.NotificationContext{}
	if _, _, ok := notifTarget(bare); ok {
		t.Errorf("%s without post_id: ok = true", bare.Type)
	}
}

func TestNotificationEnterMarksReadAndRoutes(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]int{"unread_count": 1})
	h := uitest.New(t, notifOptsWith(srv, false, nil)...)
	first := fixtures.Notifications()[0]
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/notifications/read")
	if !ok {
		t.Fatalf("enter sent no POST /v1/notifications/read; requests %+v", h.API.Requests())
	}
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string]any{"ids": []any{first.ID}}) {
		t.Fatalf("mark-read body = %s, want {\"ids\":[%q]}", r.Body, first.ID)
	}
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != *first.Context.PostID {
		t.Fatalf("enter opened %+v, want RoutePost %s", top, *first.Context.PostID)
	}
	if n := h.Context().Store.NotifUnread; n != 1 {
		t.Fatalf("NotifUnread = %d after mark read, want 1 (server count)", n)
	}
}

func TestNotificationEnterReadSendsNothing(t *testing.T) {
	srv := snsAPI(t, false)
	items := notificationsAll()
	items[0].Read = true
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: items, UnreadCount: 1})
	h := uitest.New(t, notifOptsWith(srv, false, nil)...)
	h.Press("enter")
	if rs := reqsTo(h, "POST", "/v1/notifications/read"); len(rs) != 0 {
		t.Fatalf("enter on a read notification sent %+v", rs)
	}
	if top := feedTop(h); top.Kind != ui.RoutePost {
		t.Fatalf("enter opened %+v, want RoutePost", top)
	}
}

func TestNotificationEnterFollowRequestOpensOverlay(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]int{"unread_count": 1})
	var fr api.Notification
	for _, n := range notificationsAll() {
		if n.Type == "follow_request" {
			fr = n
		}
	}
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: []api.Notification{fr}, UnreadCount: 1})
	h := uitest.New(t, notifOptsWith(srv, false, nil)...)
	h.Press("enter")
	if len(h.Model.Frame().Overlays) != 1 {
		t.Fatalf("overlays = %+v, want feed.follow_requests", h.Model.Frame().Overlays)
	}
	if _, ok := lastReq(h, "GET", "/v1/me/follow-requests"); !ok {
		t.Fatalf("feed.follow_requests not opened (no GET /v1/me/follow-requests); requests %+v", h.API.Requests())
	}
}

func TestNotificationsMarkAll(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]int{"unread_count": 0})
	h := uitest.New(t, notifOptsWith(srv, false, nil)...)
	h.Resize(120, 40)
	if !strings.Contains(h.Screen(), "● ♥") {
		t.Fatalf("unread dot missing before x:\n%s", h.Screen())
	}
	h.Press("x")
	r, ok := lastReq(h, "POST", "/v1/notifications/read")
	if !ok {
		t.Fatalf("x sent no POST /v1/notifications/read; requests %+v", h.API.Requests())
	}
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string]any{"all": true}) {
		t.Fatalf("mark-all body = %s, want {\"all\":true}", r.Body)
	}
	if s := h.Screen(); strings.Contains(s, "● ♥") || strings.Contains(s, "● @") {
		t.Fatalf("unread dots remain after x:\n%s", s)
	}
	if n := h.Context().Store.NotifUnread; n != 0 {
		t.Fatalf("NotifUnread = %d after mark all, want 0", n)
	}
}

func TestNotificationCreatedEventPrepends(t *testing.T) {
	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(120, 40)
	n := api.Notification{
		ID: discoverNewID(5), Type: "post_reply", Actor: ptr(userOf("junho")),
		Subject: api.NotificationSubject{Type: "post", ID: myPost().ID}, Preview: "저도 갈래요",
		Context: api.NotificationContext{PostID: ptr(myPost().ID)}, CreatedAt: at(0),
	}
	h.Event("notification.created", map[string]any{"notification": n, "unread_count": 3})
	ids := NotifIDs(notifInstance(h))
	if len(ids) == 0 || ids[0] != n.ID {
		t.Fatalf("rows = %v, want %s first", ids, n.ID)
	}
	if !strings.Contains(h.Screen(), "준호님이 답글을 남겼어요") {
		t.Fatalf("new notification not drawn:\n%s", h.Screen())
	}
	if _, badge := notifInstance(h).(ui.Titled).Title(h.Context()); badge.Count != 3 {
		t.Fatalf("title badge = %+v, want Count 3", badge)
	}
}

func TestNotificationsHideBlockedActor(t *testing.T) {
	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(120, 40)
	h.Event("block.updated", map[string]any{"user_id": userOf("minji").ID, "blocked": true})
	if s := h.Screen(); strings.Contains(s, "민지님") {
		t.Fatalf("blocked actor's notifications still shown:\n%s", s)
	}
	if ids := NotifIDs(notifInstance(h)); len(ids) != 1 || ids[0] != fixtures.Notifications()[0].ID {
		t.Fatalf("rows after block = %v, want only hana's like", ids)
	}
}

func TestNotificationClickRoutes(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]int{"unread_count": 1})
	h := uitest.New(t, notifOptsWith(srv, false, nil)...)
	h.Resize(120, 40)
	x, y := cellOf(t, h.Screen(), "민지님이")
	h.Click(x, y)
	if _, ok := lastReq(h, "POST", "/v1/notifications/read"); !ok {
		t.Fatalf("click sent no mark-read; requests %+v", h.API.Requests())
	}
	nav := h.Nav()
	top := nav.Tabs[nav.Active].Stack[len(nav.Tabs[nav.Active].Stack)-1]
	if top.Kind != ui.RouteChannel || top.ID != fixtures.ChChat {
		t.Fatalf("click opened %+v, want RouteChannel %s", top, fixtures.ChChat)
	}
}

func TestNotificationSpaceInviteRoutes(t *testing.T) {
	var inv api.Notification
	for _, n := range notificationsAll() {
		if n.Type == "space_invite" {
			inv = n
		}
	}
	for _, pending := range []bool{true, false} {
		srv := snsAPI(t, false)
		srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]int{"unread_count": 0})
		srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: []api.Notification{inv}, UnreadCount: 1})
		h := uitest.New(t, notifOptsWith(srv, false, nil)...)
		want := ""
		if pending {
			want = inv.Subject.ID
			s := h.Context().Store
			s.DirectInvites = append(s.DirectInvites, api.DirectInvite{ID: want, Status: "pending",
				Space: api.SpacePreview{ID: fixtures.SpaceNova, Name: "노바 개발방"}})
		}
		h.Press("enter")
		nav := h.Nav()
		stack := nav.Tabs[nav.Active].Stack
		if top := stack[len(stack)-1]; top.Kind != ui.RouteInvite || top.Arg != "direct" || top.ID != want {
			t.Fatalf("pending=%v: enter opened %+v, want RouteInvite direct %q", pending, top, want)
		}
	}
}
