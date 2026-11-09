package feed

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// The 알림함's "안 읽은 대화" section (client-architecture.md, 알림함): DMs and
// channel messages make no inbox rows, so the section lists the conversations
// with unread messages from the Store's live read state, and thread replies
// grouped by thread from the unread thread notifications.

const groupLunch = "01JB0000000000000000000GD1" // a group DM the tests add

// tabTop is where a chat route landed: the chat surface's (the DM surface when
// InDM, else the active tab's) open thread, else the top of its stack.
func tabTop(h *uitest.Harness) ui.Route {
	nav := h.Nav()
	tab := nav.Tabs[nav.Active]
	if nav.InDM { // DMs open on the DM surface (CA 5.3.3)
		tab = nav.DM
	}
	if tab.Thread != nil {
		return *tab.Thread
	}
	return tab.Stack[len(tab.Stack)-1]
}

// convSrv answers GET /v1/notifications with items (nil = none).
func convSrv(t *testing.T, items []api.Notification) *apitest.Server {
	t.Helper()
	srv := snsAPI(t, false)
	if items == nil {
		items = []api.Notification{}
	}
	unread := 0
	for _, x := range items {
		if !x.Read {
			unread++
		}
	}
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: items, UnreadCount: unread})
	srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]int{"unread_count": 0})
	return srv
}

// setUnread gives channel id n unread messages (m of them mentions) after lastRead.
func setUnread(s *store.Store, id string, n, m int, lastRead string) {
	ch := s.Channels[id]
	ch.My.UnreadCount, ch.My.MentionCount = n, m
	ch.My.LastReadMessageID = nil
	if lastRead != "" {
		ch.My.LastReadMessageID = &lastRead
	}
}

// addGroup adds the 점심팟 group DM (sam, 민지, 준호), last message at last.
func addGroup(s *store.Store, last time.Time) {
	s.ObserveChannel(api.Channel{ID: groupLunch, Kind: api.ChannelGroupDM, Name: ptr("점심팟"), MemberCount: 3,
		Members:   []api.UserSummary{userOf("sam"), userOf("minji"), userOf("junho")},
		CreatedAt: last.Add(-time.Hour), LastMessageAt: &last, LastMessageID: ptr("01JDGRP0000000000000000009"),
		My: api.ChannelMyState{Notify: "all"}})
}

func rowIDs(h *uitest.Harness) []string { return NotifRowIDs(notifInstance(h)) }

func hasRow(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestInboxConversationsSection(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, nil), false, nil)...)
	h.Resize(120, 40)
	s := h.Context().Store
	setUnread(s, fixtures.DMMinji, 2, 0, *s.Channels[fixtures.DMMinji].My.LastReadMessageID)
	addGroup(s, at(1))
	setUnread(s, groupLunch, 1, 0, "")
	setUnread(s, fixtures.ChDeploy, 1, 1, "") // mentions-only channel with a mention
	setUnread(s, fixtures.ChChat, 5, 0, "")   // notify all but muted
	s.Channels[fixtures.ChChat].My.Muted = true
	setUnread(s, fixtures.ChDev, 0, 0, "") // read
	scr := h.Screen()
	ids := rowIDs(h)
	for _, want := range []string{"conv:" + fixtures.ChGeneral, "conv:" + fixtures.DMMinji, "conv:" + groupLunch, "conv:" + fixtures.ChDeploy} {
		if !hasRow(ids, want) {
			t.Fatalf("section lacks %s: %v\n%s", want, ids, scr)
		}
	}
	for _, not := range []string{"conv:" + fixtures.ChChat, "conv:" + fixtures.ChDev, "conv:" + fixtures.DMJunho} {
		if hasRow(ids, not) {
			t.Fatalf("section lists %s (muted / read): %v", not, ids)
		}
	}
	if ids[0] != "conv:"+groupLunch {
		t.Fatalf("newest conversation first: rows %v", ids)
	}
	for _, want := range []string{"안 읽은 대화", "✉ 점심팟", "✉ 민지님", "# 일반 · 노바 개발방", "# 배포알림 · 노바 개발방"} {
		if !strings.Contains(scr, want) {
			t.Fatalf("screen lacks %q:\n%s", want, scr)
		}
	}
	if i, j := strings.Index(scr, "안 읽은 대화"), strings.Index(scr, "✉ 점심팟"); i < 0 || j < i {
		t.Fatalf("header must precede the rows:\n%s", scr)
	}
	// a mentions-only channel whose unread holds no mention stays out
	setUnread(s, fixtures.ChDeploy, 4, 0, "")
	h.Screen()
	if ids := rowIDs(h); hasRow(ids, "conv:"+fixtures.ChDeploy) {
		t.Fatalf("mentions-only channel without a mention listed: %v", ids)
	}
}

// A message that arrives live shows its preview; reading the conversation
// (read_state.updated, or a local read result that lands after the inbox
// updated) takes the row out.
func TestInboxConversationLiveAndRead(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, nil), false, nil)...)
	h.Resize(120, 40)
	m := api.Message{ID: "01JDDMM0000000000000000099", ChannelID: fixtures.DMMinji, Author: userOf("minji"), Kind: "text",
		Body: "저녁 **뭐** 먹지?", CreatedAt: at(0)}
	h.Event("message.created", map[string]any{"message": m})
	scr := h.Screen()
	if !hasRow(rowIDs(h), "conv:"+fixtures.DMMinji) || !strings.Contains(scr, `"저녁 뭐 먹지?"`) {
		t.Fatalf("live DM not listed with its preview: %v\n%s", rowIDs(h), scr)
	}
	h.Event("read_state.updated", map[string]any{"state": api.UnreadState{ChannelID: fixtures.DMMinji,
		LastMessageID: ptr(m.ID), LastReadMessageID: ptr(m.ID)}})
	if hasRow(rowIDs(h), "conv:"+fixtures.DMMinji) {
		t.Fatalf("read DM still listed: %v", rowIDs(h))
	}
	// a read applied to the Store outside the inbox's Update (markReadLocal)
	setUnread(h.Context().Store, fixtures.ChGeneral, 0, 0, "")
	if scr := h.Screen(); strings.Contains(scr, "일반 · 노바 개발방") {
		t.Fatalf("locally read channel still drawn:\n%s", scr)
	}
	if hasRow(rowIDs(h), "conv:"+fixtures.ChGeneral) {
		t.Fatalf("locally read channel still listed: %v", rowIDs(h))
	}
}

func TestInboxConversationsHiddenWhenEmpty(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, fixtures.Notifications()), false, nil)...)
	h.Resize(120, 40)
	setUnread(h.Context().Store, fixtures.ChGeneral, 0, 0, "")
	scr := h.Screen()
	if strings.Contains(scr, "안 읽은 대화") {
		t.Fatalf("empty section drawn:\n%s", scr)
	}
	if ids := rowIDs(h); len(ids) != 2 || ids[0] != fixtures.Notifications()[0].ID {
		t.Fatalf("rows = %v, want only the two notifications", ids)
	}
	if _, b := notifInstance(h).(ui.Titled).Title(h.Context()); b.Count != 2 {
		t.Fatalf("badge = %d, want 2 (notifications only)", b.Count)
	}
}

// Enter on a conversation row jumps to its first unread message: the channel
// anchored on my read pointer with jump=unread, a thread on its oldest unread
// reply (the thread's notifications are marked read).
func TestInboxConversationEnterJumps(t *testing.T) {
	jump := map[string]string{ui.QueryJump: ui.JumpUnread}
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	threadNotif := func(id int, msg string, typ api.NotificationType, minAgo int) api.Notification {
		return api.Notification{ID: sid(id), Type: typ, Actor: ptr(userOf("minji")),
			Subject: api.NotificationSubject{Type: "message", ID: msg}, Preview: "넵 2시에 할게요",
			Context: api.NotificationContext{ChannelID: ptr(fixtures.ChDev), MessageID: ptr(msg), ThreadRootID: ptr(devRoot),
				SpaceID: ptr(fixtures.SpaceNova)}, CreatedAt: at(minAgo)}
	}
	older := threadNotif(70, "01JDTHR0000000000000000002", "thread_reply", 9)
	newer := threadNotif(71, devThreadReply3, "message_mention", 8)
	read := threadNotif(72, "01JDTHR0000000000000000001", "thread_reply", 20)
	read.Read = true
	cases := []struct {
		name  string
		row   string
		setup func(s *store.Store)
		want  ui.Route
		dm    bool     // lands on the DM surface (CA 5.3.3), not in a tab
		read  []string // notification ids enter marks read
	}{
		{name: "channel", row: "conv:" + fixtures.ChGeneral,
			want: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral, Anchor: fixtures.Messages(fixtures.ChGeneral)[0].ID, Query: jump}},
		{name: "dm", row: "conv:" + fixtures.DMMinji,
			setup: func(s *store.Store) { setUnread(s, fixtures.DMMinji, 1, 0, fixtures.Messages(fixtures.DMMinji)[0].ID) },
			want:  ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji, Anchor: fixtures.Messages(fixtures.DMMinji)[0].ID, Query: jump}, dm: true},
		{name: "group never read", row: "conv:" + groupLunch,
			setup: func(s *store.Store) { addGroup(s, at(0)); setUnread(s, groupLunch, 2, 0, "") },
			want:  ui.Route{Kind: ui.RouteChannel, ID: groupLunch}, dm: true},
		{name: "thread", row: "conv:thread:" + devRoot,
			want: ui.Route{Kind: ui.RouteThread, ID: devRoot, Arg: fixtures.ChDev, Anchor: older.Subject.ID},
			read: []string{newer.ID, older.ID}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := uitest.New(t, notifOptsWith(convSrv(t, []api.Notification{newer, older, read}), false, nil)...)
			h.Resize(120, 40)
			if c.setup != nil {
				c.setup(h.Context().Store)
			}
			h.Screen()
			if !SelectNotifRow(notifInstance(h), c.row) {
				t.Fatalf("no row %s in %v", c.row, rowIDs(h))
			}
			h.Press("enter")
			if got := tabTop(h); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("enter opened %+v, want %+v", got, c.want)
			}
			if got := h.Nav().InDM; got != c.dm {
				t.Fatalf("enter: InDM = %v, want %v (DMs open on the DM surface)", got, c.dm)
			}
			reqs := reqsTo(h, "POST", "/v1/notifications/read")
			if len(c.read) == 0 {
				if len(reqs) != 0 {
					t.Fatalf("a conversation row marked notifications read: %+v", reqs)
				}
				return
			}
			if len(reqs) != 1 {
				t.Fatalf("POST /v1/notifications/read sent %d times, want 1", len(reqs))
			}
			var body struct{ IDs []string }
			if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.IDs, c.read) {
				t.Fatalf("marked %v, want %v", body.IDs, c.read)
			}
		})
	}
}

// The thread row groups every unread reply of one thread; read ones do not count.
func TestInboxThreadConversationRow(t *testing.T) {
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	h := uitest.New(t, notifOpts(t, false, nil)...) // notificationsAll: one unread thread_reply in #개발
	h.Resize(120, 40)
	scr := h.Screen()
	if !hasRow(rowIDs(h), "conv:thread:"+devRoot) {
		t.Fatalf("no thread row: %v", rowIDs(h))
	}
	if !strings.Contains(scr, "↩ 스레드 · #개발") || !strings.Contains(scr, `민지님: 넵 2시에 할게요`) {
		t.Fatalf("thread row copy missing:\n%s", scr)
	}
}

// The 알림 badge (pane title, tab bar) adds the conversations no notification
// row already counts: here #일반 (notify all) on top of the 2 notifications.
func TestInboxBadgeCountsConversations(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, fixtures.Notifications()), false, nil)...)
	h.Resize(120, 40)
	if _, b := notifInstance(h).(ui.Titled).Title(h.Context()); b.Count != 3 {
		t.Fatalf("title badge = %d, want 3 (2 notifications + #일반)", b.Count)
	}
	s := h.Context().Store
	setUnread(s, fixtures.ChChat, 1, 1, "") // only a mention, and its unread message_mention row already counts
	if got := s.InboxBadge(); got != 3 {
		t.Fatalf("InboxBadge = %d, want 3", got)
	}
	setUnread(s, fixtures.DMJunho, 4, 0, fixtures.Messages(fixtures.DMJunho)[0].ID)
	if got := s.InboxBadge(); got != 4 {
		t.Fatalf("InboxBadge = %d, want 4 with the DM", got)
	}
}

// Narrow and tiny inboxes keep every conversation row on one line inside the pane.
func TestInboxConversationsNarrow(t *testing.T) {
	h := uitest.New(t, notifOpts(t, true, nil)...)
	for _, sz := range uitest.AllSizes() {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

// The palette's 알림함 command carries the same badge, and the tab bar too.
func TestInboxBadgeInPaletteAndTabs(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, fixtures.Notifications()), false, nil)...)
	h.Resize(80, 24) // widget mode: 채팅 / 피드 / 알림 tabs
	if scr := h.Screen(); !strings.Contains(strings.Split(scr, "\n")[0], "알림  3") {
		t.Fatalf("tab bar lacks 알림 3:\n%s", scr)
	}
	h.Resize(120, 40)
	h.Press("ctrl+k")
	h.Type("알림함")
	for _, ln := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(ln, "알림함") && strings.Contains(ln, "ctrl+n") {
			if !strings.Contains(ln, "ctrl+n  3 ") {
				t.Fatalf("palette 알림함 row lacks the badge: %q", ln)
			}
			return
		}
	}
	t.Fatalf("palette shows no 알림함:\n%s", h.Screen())
}

// The empty query's command list (palette discovery) draws the 알림함 badge too.
func TestInboxBadgeInEmptyPalette(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, fixtures.Notifications()), false, nil)...)
	h.Resize(120, 40)
	h.Press("ctrl+k")
	for _, ln := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(ln, "알림함") && strings.Contains(ln, "ctrl+n") {
			if !strings.Contains(ln, "ctrl+n  3 ") {
				t.Fatalf("empty palette 알림함 row lacks the badge: %q", ln)
			}
			return
		}
	}
	t.Fatalf("empty palette shows no 알림함:\n%s", h.Screen())
}

// threadReplyID is notificationsAll's unread thread_reply (in #개발).
func threadReplyID(t *testing.T) string {
	t.Helper()
	for _, x := range notificationsAll() {
		if x.Type == "thread_reply" {
			return x.ID
		}
	}
	t.Fatal("notificationsAll has no thread_reply")
	return ""
}

// A thread reply grouped into a 안 읽은 대화 thread row is not listed again
// under 알림; once read it is back there as history.
func TestInboxThreadReplyShownOnce(t *testing.T) {
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	reply := threadReplyID(t)
	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(200, 49)
	scr := h.Screen()
	ids := rowIDs(h)
	if !hasRow(ids, "conv:thread:"+devRoot) {
		t.Fatalf("no thread row: %v", ids)
	}
	if hasRow(ids, reply) {
		t.Fatalf("thread reply listed twice (thread row and notification): %v", ids)
	}
	if c := strings.Count(scr, "넵 2시에 할게요"); c != 1 {
		t.Fatalf("reply preview drawn %d times, want 1:\n%s", c, scr)
	}
	h.Event("notifications.read", map[string]any{"ids": []string{reply}, "unread_count": 15})
	ids = rowIDs(h)
	if hasRow(ids, "conv:thread:"+devRoot) || !hasRow(ids, reply) {
		t.Fatalf("after read: thread row must go and the notification come back: %v", ids)
	}
}

// When every shown notification is grouped into the section, the "알림" part
// still draws its header and state line under the section.
func TestInboxAllNotificationsGrouped(t *testing.T) {
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	n := api.Notification{ID: sid(70), Type: "thread_reply", Actor: ptr(userOf("minji")),
		Subject: api.NotificationSubject{Type: "message", ID: devThreadReply3}, Preview: "넵 2시에 할게요",
		Context: api.NotificationContext{ChannelID: ptr(fixtures.ChDev), MessageID: ptr(devThreadReply3), ThreadRootID: ptr(devRoot),
			SpaceID: ptr(fixtures.SpaceNova)}, CreatedAt: at(9)}
	h := uitest.New(t, notifOptsWith(convSrv(t, []api.Notification{n}), false, nil)...)
	h.Resize(120, 40)
	scr := h.Screen()
	if hasRow(rowIDs(h), n.ID) {
		t.Fatalf("grouped notification listed: %v", rowIDs(h))
	}
	if !strings.Contains(scr, "새 알림이 없어요") {
		t.Fatalf("no 알림 state line under the section:\n%s", scr)
	}
}

// The preview never shows a deleted message or one at or before the read
// pointer, even when a stored alert still carries its text.
func TestInboxConversationPreviewSkipsStaleAlerts(t *testing.T) {
	h := uitest.New(t, notifOptsWith(convSrv(t, nil), false, nil)...)
	h.Resize(160, 40)
	s := h.Context().Store
	addGroup(s, at(30))
	setUnread(s, groupLunch, 0, 0, "")
	alert := func(id, body string) map[string]any {
		return map[string]any{"alert": api.Alert{ID: "AL" + id, Kind: "message", Title: "점심팟", Body: body, Actor: ptr(userOf("minji")),
			Target: api.AlertTarget{Type: "message", ID: id, ChannelID: ptr(groupLunch), MessageID: ptr(id)}, CreatedAt: at(1)}}
	}
	msgAt := func(id, body string, minAgo int) api.Message {
		return api.Message{ID: id, ChannelID: groupLunch, Author: userOf("minji"), Kind: "text", Body: body, CreatedAt: at(minAgo)}
	}
	mB := msgAt("01JDGRP0000000000000000011", "비빔밥 어때요", 4)
	mC := msgAt("01JDGRP0000000000000000012", "아니면 국수", 3)
	mD := msgAt("01JDGRP0000000000000000013", "지워질 메시지", 2)
	for _, m := range []api.Message{mB, mC, mD} {
		h.Event("message.created", map[string]any{"message": m})
		h.Event("alert", alert(m.ID, m.Body))
	}
	h.Event("message.deleted", map[string]any{"channel_id": groupLunch, "message_id": mD.ID, "tombstone": true})
	preview := func() string {
		h.Screen()
		body, ok := NotifConvPreview(notifInstance(h), "conv:"+groupLunch)
		if !ok {
			t.Fatalf("no 점심팟 row: %v", rowIDs(h))
		}
		return body
	}
	if got := preview(); got != mC.Body {
		t.Fatalf("preview = %q, want the newest remaining unread message %q (not the deleted one)", got, mC.Body)
	}
	// read up to C (the count still says unread): no alert at or before the pointer
	setUnread(s, groupLunch, 1, 0, mC.ID)
	if got := preview(); got != "" {
		t.Fatalf("preview = %q, want the 새 메시지 fallback (alerts at or before the read pointer, or deleted)", got)
	}
}

// A conversation listed only for its mentions counts in the badge unless an
// unread message_mention row for it is loaded: x (all read) or a missing row
// (mention notifications off) leaves it counted.
func TestInboxBadgeMentionsWithoutRows(t *testing.T) {
	gm := api.Notification{ID: sid(70), Type: "message_mention", Actor: ptr(userOf("minji")),
		Subject: api.NotificationSubject{Type: "message", ID: "01JDGRP0000000000000000011"}, Preview: "@sam 점심?",
		Context: api.NotificationContext{ChannelID: ptr(groupLunch), MessageID: ptr("01JDGRP0000000000000000011")}, CreatedAt: at(3)}
	h := uitest.New(t, notifOptsWith(convSrv(t, []api.Notification{gm}), false, nil)...)
	h.Resize(120, 40)
	s := h.Context().Store
	setUnread(s, fixtures.ChGeneral, 0, 0, "")
	addGroup(s, at(3))
	setUnread(s, groupLunch, 1, 1, "")
	h.Screen()
	if got := s.InboxBadge(); got != 1 {
		t.Fatalf("InboxBadge = %d, want 1 (the mention row covers the group DM)", got)
	}
	h.Press("x")
	h.Screen()
	if !hasRow(rowIDs(h), "conv:"+groupLunch) {
		t.Fatalf("group DM left the section after x: %v", rowIDs(h))
	}
	if got := s.InboxBadge(); got != 1 {
		t.Fatalf("InboxBadge after x = %d, want 1 (the listed group DM)", got)
	}
	if _, b := notifInstance(h).(ui.Titled).Title(h.Context()); b.Count != 1 {
		t.Fatalf("title badge after x = %d, want 1", b.Count)
	}
	// a mentions-only channel with a mention but no row (mention notifications off)
	setUnread(s, fixtures.ChDeploy, 1, 1, "")
	if got := s.InboxBadge(); got != 2 {
		t.Fatalf("InboxBadge = %d, want 2 with #배포알림 (no mention row loaded)", got)
	}
}

// Threads in muted or notify-none channels get no 안 읽은 대화 row; their
// notifications stay in the 알림 list.
func TestInboxThreadRowSkipsQuietChannels(t *testing.T) {
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	reply := threadReplyID(t)
	for _, c := range []struct {
		name string
		set  func(ch *api.Channel)
	}{
		{"muted", func(ch *api.Channel) { ch.My.Muted = true }},
		{"notify none", func(ch *api.Channel) { ch.My.Notify = "none" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := uitest.New(t, notifOpts(t, false, nil)...)
			h.Resize(120, 40)
			c.set(h.Context().Store.Channels[fixtures.ChDev])
			h.Screen()
			ids := rowIDs(h)
			if hasRow(ids, "conv:thread:"+devRoot) {
				t.Fatalf("thread of a %s channel listed: %v", c.name, ids)
			}
			if !hasRow(ids, reply) {
				t.Fatalf("its notification left the 알림 list: %v", ids)
			}
		})
	}
}

// The selection rests on the top row while the rows arrive: the thread row
// built from the notification page lands above the Store-derived #일반, and
// enter right after ctrl+n opens the newest conversation. A cursor the user
// moved stays where it is.
func TestInboxSelectionStaysOnTopWhileRowsArrive(t *testing.T) {
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	reply := api.Notification{ID: sid(80), Type: "thread_reply", Actor: ptr(userOf("minji")),
		Subject: api.NotificationSubject{Type: "message", ID: devThreadReply3}, Preview: "넵 2시에 할게요",
		Context: api.NotificationContext{ChannelID: ptr(fixtures.ChDev), MessageID: ptr(devThreadReply3),
			ThreadRootID: ptr(devRoot), SpaceID: ptr(fixtures.SpaceNova)}, CreatedAt: at(0)}
	h := uitest.New(t, notifOptsWith(convSrv(t, []api.Notification{reply}), false, nil)...) // #일반 unread too
	h.Resize(120, 40)
	h.Screen()
	ids := rowIDs(h)
	if len(ids) < 2 || ids[0] != "conv:thread:"+devRoot {
		t.Fatalf("rows = %v, want the #개발 thread row first", ids)
	}
	if got := NotifSelectedRow(notifInstance(h)); got != ids[0] {
		t.Fatalf("selected %q after the page arrived, want the top row %q", got, ids[0])
	}
	// a moved cursor is kept when the rows change
	if !SelectNotifRow(notifInstance(h), ids[1]) {
		t.Fatalf("no row %s", ids[1])
	}
	n := api.Notification{
		ID: discoverNewID(5), Type: "post_reply", Actor: ptr(userOf("junho")),
		Subject: api.NotificationSubject{Type: "post", ID: myPost().ID}, Preview: "저도 갈래요",
		Context: api.NotificationContext{PostID: ptr(myPost().ID)}, CreatedAt: at(0),
	}
	h.Event("notification.created", map[string]any{"notification": n, "unread_count": 3})
	h.Screen()
	if got := NotifSelectedRow(notifInstance(h)); got != ids[1] {
		t.Fatalf("selected %q after a live notification, want the user's %q", got, ids[1])
	}
}

// The top-row selection rule never undoes a wheel scroll: the selection stays
// on the top row while the view scrolls, and the next sync must not reveal it.
func TestInboxWheelScrollSurvivesSync(t *testing.T) {
	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(120, 14)
	before := h.Screen()
	h.Wheel(60, 8, 3)
	scrolled := h.Screen()
	if scrolled == before {
		t.Fatalf("wheel did not scroll:\n%s", scrolled)
	}
	h.Event("read_state.updated", map[string]any{"state": api.UnreadState{ChannelID: fixtures.ChChat}})
	if got := h.Screen(); got != scrolled {
		t.Fatalf("a sync undid the wheel scroll:\n%s\nwant\n%s", got, scrolled)
	}
}
