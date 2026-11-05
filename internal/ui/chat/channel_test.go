package chat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const testChan = "01JBTESTCHAN0000000000000A"

func TestChannelGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		scr := h.Screen()
		// too_small draws only the "창을 조금만 키워 주세요" notice (no route at all), so
		// there is no conversation text to look for; the golden still pins it.
		if h.Context().Mode != layout.ModeTooSmall {
			switch h.Context().Density() {
			case ui.DensityFull:
				mustContain(t, scr, "민지", "11:02", "오늘 배포 몇 시에 해요?", "답글 3개", "$ wrangler deploy --env prod", "국밥")
			case ui.DensityLine:
				mustContain(t, scr, "11:02 민지 오늘 배포 몇 시에 해요?", "💬3")
			case ui.DensityBubble:
				mustContain(t, scr, "민지 11:02", "11:05 나")
			case ui.DensityMinimal:
				mustContain(t, scr, "민지")
			}
		}
		h.Golden("channel")
	})
}

func TestChannelHostileIntegrity(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() })
}

func TestChannelInfiniteScrollBack(t *testing.T) {
	srv := newAPI(t)
	ch := testChan
	// ONE exact-path handler that switches on the query (re-registering the same shape would REPLACE the first, not add a second — apitest rule (a))
	srv.Handle("GET /v1/channels/"+ch+"/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("before") == msgID(51) {
			json.NewEncoder(w).Encode(pageBody(genMsgs(ch, 1, 50), false, true)) // ids msgID(1..50)
			return
		}
		json.NewEncoder(w).Encode(pageBody(genMsgs(ch, 51, 100), true, false)) // latest page: msgID(51..100)
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(withTestChannel(ch, 0)))
	if sel := selectedID(t, h); sel != msgID(100) {
		t.Fatalf("opens on the latest message, got %q", sel)
	}
	h.Press("home")
	if len(reqs(srv, "GET", "/v1/channels/"+ch+"/messages")) != 1 {
		t.Fatal("home alone only moves to the first loaded message")
	}
	h.Press("k")
	if q := lastQuery(srv, "/v1/channels/"+ch+"/messages"); q != "before="+msgID(51)+"&limit=50" {
		t.Fatalf("query %q", q)
	}
	if n := chatstore.Get(h.Context().Store).Window(ch).Len(); n != 100 {
		t.Fatalf("len %d", n)
	}
	if selectedID(t, h) != msgID(51) {
		t.Fatal("selection stays on the old first item after prepend")
	}
	if !rowVisible(t, h, msgID(51)) {
		t.Fatal("the old first item stays in view")
	}
	// no more before: k at the very top requests nothing
	h.Press("home", "k")
	if n := len(reqs(srv, "GET", "/v1/channels/"+ch+"/messages")); n != 2 {
		t.Fatalf("has_more_before=false must not page again (%d requests)", n)
	}
	if selectedID(t, h) != msgID(1) {
		t.Fatalf("home selects the first message, got %q", selectedID(t, h))
	}
}

func TestChannelNewMessageWhileScrolledUp(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("k", "k", "k", "ctrl+u")
	before := channelOf(t, h).pane.list.Anchor()
	sel := selectedID(t, h)
	h.Event("message.created", realtime.MessageEvent{Message: mkMsg(devID(t, h), 900, userBy(t, h.Context().Store, "minji"), "새 글", h.Context().Now())})
	if channelOf(t, h).pane.list.Anchor() != before || selectedID(t, h) != sel {
		t.Fatal("must not jump")
	}
	mustContain(t, h.Screen(), "새 메시지 1개 ↓")
	h.Press("G")
	if strings.Contains(h.Screen(), "새 메시지 1개") || !channelOf(t, h).pane.list.AtBottom() {
		t.Fatal("G jumps to bottom and clears banner")
	}
	if selectedID(t, h) != msgID(900) {
		t.Fatalf("G selects the newest message, got %q", selectedID(t, h))
	}
}

func TestChannelFollowsBottom(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Screen()
	m := mkMsg(devID(t, h), 902, userBy(t, h.Context().Store, "minji"), "따라가는 글", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	if selectedID(t, h) != m.ID {
		t.Fatalf("at the bottom with the last row selected, the new message becomes selected (got %q)", selectedID(t, h))
	}
	scr := h.Screen()
	if !rowVisible(t, h, m.ID) || !strings.Contains(scr, "따라가는 글") || strings.Contains(scr, "새 메시지 1개") {
		t.Fatal("followed: visible, no banner")
	}
}

func TestChannelNewMessageFade(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Context().Config.UI.Animations = true
	m := mkMsg(devID(t, h), 901, userBy(t, h.Context().Store, "minji"), "하이", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	th := h.Context().Theme
	f := &channelOf(t, h).pane.fade
	if !sameColor(f.Style(th, m.ID).GetBackground(), th.P.SelBG) {
		t.Fatal("starts at selbg")
	}
	h.Advance(time.Second)
	if !sameColor(f.Style(th, m.ID).GetBackground(), th.P.BG) {
		t.Fatal("faded after 4x250ms")
	}
	// my own message never fades
	me := *h.Context().Store.User(h.Context().Store.Me.ID)
	mine := mkMsg(devID(t, h), 903, me, "내 글", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: mine})
	if !sameColor(f.Style(th, mine.ID).GetBackground(), th.P.BG) {
		t.Fatal("my own message is not highlighted")
	}
}

func TestChannelMarksRead(t *testing.T) {
	srv := newAPI(t)
	ch := testChan
	srv.JSON("GET /v1/channels/"+ch+"/messages", 200, pageBody(genMsgs(ch, 1, 5), false, false))
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(withTestChannel(ch, 3)))
	if len(reqs(srv, "PUT", "/v1/channels/"+ch+"/read")) != 0 {
		t.Fatal("debounced 1s")
	}
	h.Advance(time.Second)
	r := reqs(srv, "PUT", "/v1/channels/"+ch+"/read")
	if len(r) != 1 || jsonField(r[0].Body, "message_id") != msgID(5) {
		t.Fatalf("%v", r)
	}
	if h.Context().Store.Channels[ch].My.UnreadCount != 0 {
		t.Fatal("local badge cleared")
	}
	h.Press("k", "j", "G")
	h.Advance(2 * time.Second)
	if n := len(reqs(srv, "PUT", "/v1/channels/"+ch+"/read")); n != 1 {
		t.Fatalf("already read: no second PUT (%d)", n)
	}
}

func TestChannelReadOnlyWhileViewing(t *testing.T) {
	srv := newAPI(t)
	ch := testChan
	srv.JSON("GET /v1/channels/"+ch+"/messages", 200, pageBody(genMsgs(ch, 1, 5), false, false))
	put := func() int { return len(reqs(srv, "PUT", "/v1/channels/"+ch+"/read")) }
	// 120x30: compact, Main only
	h := openWith(t, uitest.Size{W: 120, H: 30}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(withTestChannel(ch, 2)))
	if h.Context().Mode != layout.ModeCompact {
		t.Fatalf("mode %v", h.Context().Mode)
	}
	h.Advance(time.Second)
	if put() != 1 {
		t.Fatalf("first read: %d", put())
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
	if h.Context().Store.Viewing(ch) {
		t.Fatal("home replaces the channel in compact")
	}
	m := mkMsg(ch, 6, userBy(t, h.Context().Store, "minji"), "안 보는 동안", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	h.Advance(2 * time.Second)
	if put() != 1 {
		t.Fatalf("armed by the event, dropped at fire time (not viewing): %d", put())
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: ch}})
	if !h.Context().Store.Viewing(ch) {
		t.Fatal("channel shown again")
	}
	h.Press("G")
	h.Advance(time.Second)
	r := reqs(srv, "PUT", "/v1/channels/"+ch+"/read")
	if len(r) != 2 || jsonField(r[1].Body, "message_id") != m.ID {
		t.Fatalf("exactly one more read of %s: %d", m.ID, len(r))
	}
}

func TestChannelUnreadDivider(t *testing.T) {
	srv := newAPI(t)
	ch := testChan
	srv.JSON("GET /v1/channels/"+ch+"/messages", 200, pageBody(genMsgs(ch, 1, 5), false, false))
	seed := func(s *store.Store) {
		withTestChannel(ch, 2)(s)
		id := msgID(3)
		s.Channels[ch].My.LastReadMessageID = &id
	}
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(seed))
	mustContainInOrder(t, h.Screen(), "메시지 3", "새 메시지", "메시지 4")
	h.Advance(time.Second)
	if r := reqs(srv, "PUT", "/v1/channels/"+ch+"/read"); len(r) != 1 {
		t.Fatalf("marked read: %d", len(r))
	}
	if id := h.Context().Store.Channels[ch].My.LastReadMessageID; id == nil || *id != msgID(5) {
		t.Fatal("read pointer moved")
	}
	mustContainInOrder(t, h.Screen(), "메시지 3", "새 메시지", "메시지 4") // frozen while the view stays open
}

func TestChannelAnchorJump(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	ms := fixtures.Messages(dev)
	x, y := ms[1].ID, ms[2].ID
	h2 := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: dev, Anchor: x}, uitest.WithSeed(bootstrappedSeed))
	if q := lastQuery(h2.API, "/v1/channels/"+dev+"/messages"); q != "around="+x+"&limit=50" {
		t.Fatalf("query %q", q)
	}
	if selectedID(t, h2) != x {
		t.Fatalf("selected %q, want %q", selectedID(t, h2), x)
	}
	h2.Send(ui.RouteUpdatedMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: dev, Anchor: y}})
	r := reqs(h2.API, "GET", "/v1/channels/"+dev+"/messages")
	if len(r) != 2 || r[1].Query != "around="+y+"&limit=50" {
		t.Fatalf("second around request: %+v", r)
	}
	if selectedID(t, h2) != y {
		t.Fatalf("selected %q, want %q", selectedID(t, h2), y)
	}
	// a RouteUpdatedMsg of another route is not ours
	h2.Send(ui.RouteUpdatedMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral, Anchor: x}})
	if len(reqs(h2.API, "GET", "/v1/channels/"+dev+"/messages")) != 2 || selectedID(t, h2) != y {
		t.Fatal("other route's update ignored")
	}
}

func TestChannelFetchesMissingInRange(t *testing.T) {
	srv := newAPI(t)
	dev := chanID(t, seeded(), "개발")
	id := "01JDDEV0000000000000000009" // after the last canned #개발 message: inside the latest window
	missing := api.Message{ID: id, ChannelID: dev, Author: userBy(t, seeded(), "junho"), Kind: "text", Body: "놓친 글", CreatedAt: uitest.Start}
	srv.JSON("GET /v1/messages/"+id, 200, map[string]any{"message": missing})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: dev}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Event("reaction.added", realtime.ReactionEvent{ChannelID: dev, MessageID: id, Emoji: "👍", UserID: fixtures.UserJunho, Count: 1})
	if len(reqs(srv, "GET", "/v1/messages/"+id)) != 1 {
		t.Fatal("missing message fetched")
	}
	if !strings.Contains(h.Screen(), "놓친 글") || !rowVisible(t, h, id) {
		t.Fatal("fetched message shown")
	}
}

func TestChannelAppliesResync(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	m := mkMsg(dev, 904, userBy(t, h.Context().Store, "hana"), "재동기화 결과", h.Context().Now())
	h.Send(chatstore.ResyncResultMsg{Gen: 1, Channels: map[string]*api.MessageWindow{dev: {Items: []api.Message{m}}}})
	scr := h.Screen()
	if !strings.Contains(scr, "재동기화 결과") || strings.Contains(scr, "오늘 배포 몇 시에 해요?") {
		t.Fatalf("only the resynced window is shown:\n%s", scr)
	}
	if selectedID(t, h) != m.ID {
		t.Fatalf("selection falls on the remaining message, got %q", selectedID(t, h))
	}
}

func TestChannelGoneWhileViewing(t *testing.T) {
	forEachSize(t, uitest.Required, devChannel, func(t *testing.T, h *uitest.Harness) { // devChannel seeds Sync.Bootstrapped=true (the notice depends on it)
		id := devID(t, h)
		delete(h.Context().Store.Channels, id)
		h.Event("channel.deleted", realtime.ChannelDeleted{ChannelID: id, Reason: "deleted"})
		mustContain(t, h.Screen(), "삭제되었거나 볼 수 없는 채널이에요")
		h.Press("i")
		if channelOf(t, h).CapturesInput() {
			t.Fatal("composer disabled")
		}
		h.Press("k", "j", "G", "t", "enter")
		h.Check()
	})
}

func TestChannelBlockedFolds(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	minji := userBy(t, h.Context().Store, "minji")
	h.Context().Store.Blocks[minji.ID] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: minji.ID, Blocked: true})
	scr := h.Screen()
	if strings.Contains(scr, "오늘 배포 몇 시에 해요?") || !strings.Contains(scr, "차단한 사용자의 메시지") {
		t.Fatal("fold")
	}
}

func TestChannelThreadKeyAndClick(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	root := fixtures.Messages(dev)[0].ID
	h.Press("home")
	if selectedID(t, h) != root {
		t.Fatalf("home selects the root, got %q", selectedID(t, h))
	}
	h.Press("t")
	if r, ok := threadRoute(h); !ok || r.ID != root || r.Arg != dev {
		t.Fatalf("t opens the thread of the selected message: %+v", h.Model.Frame().Routes)
	}

	// mouse: a click selects, a click on the selected row opens its thread
	h = devChannel(t, uitest.Size{W: 120, H: 40})
	clickText(t, h, layout.SlotMain, "점심 먹고")
	lunch := fixtures.Messages(dev)[2].ID
	if selectedID(t, h) != lunch {
		t.Fatalf("click selects, got %q", selectedID(t, h))
	}
	if _, ok := threadRoute(h); ok {
		t.Fatal("first click only selects")
	}
	clickText(t, h, layout.SlotMain, "점심 먹고")
	if r, ok := threadRoute(h); !ok || r.ID != lunch {
		t.Fatalf("second click opens the thread: %+v", h.Model.Frame().Routes)
	}
}

func TestChannelTitleHelpViewing(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}, uitest.WithSeed(bootstrappedSeed))
	v := channelOf(t, h)
	title, badge := v.Title(h.Context())
	if title != "# 일반" || badge.Count != 3 {
		t.Fatalf("title %q badge %+v", title, badge)
	}
	if got := v.ViewingChannels(); len(got) != 1 || got[0] != fixtures.ChGeneral {
		t.Fatalf("viewing %v", got)
	}
	if hs := v.Help(h.Context()); len(hs) != 4 || hs[0].Help().Desc != "스레드" {
		t.Fatalf("help %+v", hs)
	}
	st, ok := v.SaveState().(channelSaved)
	if !ok || st.Selected == "" {
		t.Fatalf("saved %#v", v.SaveState())
	}
}
