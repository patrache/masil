package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Navigation history (CA 5.3.3 "기록"): the owner's flows in the single-pane modes a
// terminal at 1/8 of the screen lands in — widget (80x30) and tiny (40x14).

var historySizes = []uitest.Size{{W: 80, H: 30}, {W: 40, H: 14}}

// historyReplyID is the #개발 thread reply the notification and the search hit point at.
const historyReplyID = "01JDTHR0000000000000000003"

// historyAPI is productAPI plus a thread_reply notification on the #개발 thread and a
// message search hit on the same reply.
func historyAPI(t *testing.T) uitest.Option {
	t.Helper()
	srv := productAPI(t)
	root := devRootID(t)
	ch, msgID, sp, spn := fixtures.ChDev, historyReplyID, fixtures.SpaceNova, "노바 개발방"
	n := api.Notification{
		ID: "01JFNTF0000000000000000009", Type: "thread_reply",
		Actor:     &api.UserSummary{ID: fixtures.UserJunho, Handle: "junho", DisplayName: "준호"},
		Subject:   api.NotificationSubject{Type: "message", ID: msgID},
		Preview:   "넵 2시에 할게요",
		Context:   api.NotificationContext{ChannelID: &ch, MessageID: &msgID, ThreadRootID: &root, SpaceID: &sp, SpaceName: &spn},
		CreatedAt: time.Date(2026, 11, 5, 2, 10, 0, 0, time.UTC),
	}
	items := append([]api.Notification{n}, fixtures.Notifications()...)
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: items, UnreadCount: 3})
	srv.JSON("POST /v1/notifications/read", http.StatusOK, map[string]any{"unread_count": 2})
	srv.Handle("GET /v1/search/messages", func(w http.ResponseWriter, _ *http.Request) {
		nm := "개발"
		m := api.Message{ID: msgID, ChannelID: fixtures.ChDev, Author: api.UserSummary{ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"},
			Kind: "text", Body: "넵 2시에 배포할게요", ThreadRootID: &root, CreatedAt: time.Date(2026, 11, 5, 2, 4, 30, 0, time.UTC)}
		hits := []api.MessageSearchHit{{Message: m, Channel: api.ChannelRef{ID: fixtures.ChDev, Kind: api.ChannelText, Name: &nm}, Highlights: []api.SearchHighlight{}}}
		writeJSON(w, map[string]any{"items": hits, "next_cursor": nil})
	})
	srv.Handle("POST /v1/messages/{id}/thread", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClientNonce string `json:"client_nonce"`
			Body        string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		rootID := r.PathValue("id")
		writeJSON(w, api.Message{ID: "01JDTHR0000000000000000099", ChannelID: fixtures.ChDev, ThreadRootID: &rootID,
			Author: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "샘"}, Kind: "text", Body: in.Body,
			Mentions: []api.MentionRef{}, Reactions: []api.ReactionSummary{}, ClientNonce: &in.ClientNonce,
			CreatedAt: time.Date(2026, 11, 5, 2, 20, 0, 0, time.UTC)})
	})
	return uitest.WithAPI(srv)
}

func newHistoryHarness(t *testing.T, sz uitest.Size, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	h := uitest.New(t, append([]uitest.Option{historyAPI(t), uitest.WithSeed(productSeed)}, opts...)...)
	h.Resize(sz.W, sz.H)
	want := layout.ModeWidget
	if sz.H < 24 {
		want = layout.ModeTiny
	}
	if got := h.Model.Frame().Layout.Mode; got != want {
		t.Fatalf("%s: mode %v, want %v", sizeName(sz), got, want)
	}
	return h
}

func forHistorySizes(t *testing.T, fn func(t *testing.T, sz uitest.Size)) {
	for _, sz := range historySizes {
		t.Run(sizeName(sz), func(t *testing.T) { fn(t, sz) })
	}
}

// mainKey is the route key Main shows.
func mainKey(h *uitest.Harness) string {
	return h.Model.Frame().Routes[layout.SlotMain].Key()
}

func mustMain(t *testing.T, h *uitest.Harness, step, want string) {
	t.Helper()
	if got := mainKey(h); got != want {
		t.Fatalf("%s: Main shows %q, want %q\n%s", step, got, want, h.Screen())
	}
}

var (
	homeKey  = ui.Route{Kind: ui.RouteHome}.Key()
	dmKey    = ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}.Key()
	devKey   = devRoute.Key()
	chatKey  = ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChChat}.Key()
	notifKey = app.NotifRoute.Key()
)

func devThreadKey(t *testing.T) string {
	return ui.Route{Kind: ui.RouteThread, ID: devRootID(t), Arg: fixtures.ChDev}.Key()
}

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("missing %q in\n%s", sub, s)
		}
	}
}

func openDM(h *uitest.Harness) {
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}})
}

// Beside its channel (standard: a thread pane) c keeps the thread open and focuses the
// channel; the title and the hints stay as they were.
func TestHistoryThreadToParentChannelStandard(t *testing.T) {
	h := uitest.New(t, historyAPI(t), uitest.WithSeed(productSeed))
	h.Resize(120, 40)
	openDevThread(h)
	if h.Model.Focus().Slot != layout.SlotThread {
		t.Fatalf("focus %v, want the thread pane", h.Model.Focus().Slot)
	}
	if strings.Contains(h.Screen(), "#개발 › 스레드") {
		t.Fatal("a thread pane must not repeat its conversation")
	}
	h.Press("c")
	if h.Model.Focus().Slot != layout.SlotMain || mainKey(h) != devKey {
		t.Fatalf("c: focus %v Main %q", h.Model.Focus().Slot, mainKey(h))
	}
	if th := h.Nav().Tabs[h.Nav().Active].Thread; th == nil {
		t.Fatal("c must keep a thread pane open")
	}
}

// The thread view's c opens its channel on the root message; back returns to the thread.
func TestHistoryThreadToParentChannel(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		h := newHistoryHarness(t, sz)
		openDev(h)
		selectDevRoot(h)
		h.Press("t")
		thread := devThreadKey(t)
		mustMain(t, h, "t", thread)
		mustContain(t, h.Screen(), "#개발 › 스레드") // the thread names its conversation
		if sz.H >= 24 {
			mustContain(t, statusRow(h), "c #개발로")
		}
		h.Press("c")
		mustMain(t, h, "c", devKey)
		if th := h.Nav().Tabs[h.Nav().Active].Thread; th != nil {
			t.Fatalf("c must close the thread in a single-pane mode: %+v", th)
		}
		mustContain(t, h.Screen(), "오늘 배포") // anchored on the root message
		h.Press("ctrl+o")
		mustMain(t, h, "back", thread)
		h.Press("alt+right")
		mustMain(t, h, "forward", devKey)
	})
}

// DM → a thread of another channel (link, 알림함, palette, search) → back returns to
// where the thread was opened from, then to the DM.
func TestHistoryThreadOfAnotherChannelBackToDM(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		thread := devThreadKey(t)
		t.Run("link", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDM(h)
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: devRootID(t), Arg: fixtures.ChDev}})
			mustMain(t, h, "thread", thread)
			h.Press("ctrl+o")
			mustMain(t, h, "back", dmKey)
			h.Press("alt+right")
			mustMain(t, h, "forward", thread)
		})
		t.Run("notifications", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDM(h)
			h.Press("ctrl+n")
			mustMain(t, h, "ctrl+n", notifKey)
			h.Press("enter")
			mustMain(t, h, "enter", thread)
			h.Press("ctrl+o")
			mustMain(t, h, "back 1", notifKey)
			h.Press("ctrl+o")
			mustMain(t, h, "back 2", dmKey)
		})
		t.Run("palette", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDM(h)
			h.Press("ctrl+k")
			h.Type("개발")
			h.Press("enter")
			mustMain(t, h, "palette", devKey)
			selectDevRoot(h)
			h.Press("t")
			mustMain(t, h, "t", thread)
			h.Press("alt+left")
			mustMain(t, h, "back 1", devKey)
			h.Press("alt+b") // macOS Terminal.app option+left
			mustMain(t, h, "back 2", dmKey)
		})
		t.Run("search", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDM(h)
			h.Press("ctrl+f")
			h.Type("배포")
			h.Press("enter")
			results := mainKey(h)
			if !strings.HasPrefix(results, "search/") {
				t.Fatalf("Main %q, want the search results", results)
			}
			h.Press("enter")
			mustMain(t, h, "hit", thread)
			h.Press("ctrl+o")
			mustMain(t, h, "back 1", results)
			mustContain(t, h.Screen(), "배포")
			h.Press("esc") // the results were pushed on the DM tab: esc pops them
			mustMain(t, h, "esc", dmKey)
		})
	})
}

// Back/forward walk across tabs, the feed and 알림함; the ends say so.
func TestHistoryChainAcrossSurfaces(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		h := newHistoryHarness(t, sz)
		mustMain(t, h, "start", homeKey)
		openDM(h)
		h.Press("f")
		feed := mainKey(h)
		if !strings.HasPrefix(feed, "feed/") {
			t.Fatalf("f: Main %q, want the feed", feed)
		}
		h.Press("ctrl+n")
		mustMain(t, h, "ctrl+n", notifKey)

		for i, want := range []string{feed, dmKey, homeKey} {
			h.Press("ctrl+o")
			mustMain(t, h, "back "+string(rune('1'+i)), want)
		}
		h.Press("ctrl+o")
		mustMain(t, h, "back at the start", homeKey)
		mustContain(t, h.Screen(), "더 뒤로 갈 곳이 없어요")

		for i, want := range []string{dmKey, feed, notifKey} {
			h.Press("alt+right")
			mustMain(t, h, "forward "+string(rune('1'+i)), want)
		}
		h.Press("alt+f")
		mustMain(t, h, "forward at the end", notifKey)
		mustContain(t, h.Screen(), "더 앞으로 갈 곳이 없어요")

		// a new place after going back drops the forward entries
		h.Press("ctrl+o", "ctrl+o")
		mustMain(t, h, "back twice", dmKey)
		openDev(h)
		h.Press("alt+right")
		mustMain(t, h, "no forward after a new place", devKey)
		h.Press("ctrl+o")
		mustMain(t, h, "back from #개발", dmKey)
	})
}

// Places that are gone (deleted channel, blocked DM) are skipped.
func TestHistorySkipsGonePlaces(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		t.Run("deleted channel", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			st := h.Context().Store
			st.Sync.Bootstrapped = true
			openDM(h)
			openDev(h)
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChChat}})
			mustMain(t, h, "#잡담", chatKey)
			delete(st.Channels, fixtures.ChDev)
			h.Press("ctrl+o")
			mustMain(t, h, "back skips #개발", dmKey)
		})
		t.Run("blocked DM", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			st := h.Context().Store
			st.Sync.Bootstrapped = true
			openDM(h)
			openDev(h)
			if st.Blocks == nil {
				st.Blocks = map[string]time.Time{}
			}
			st.Blocks[fixtures.UserMinji] = uitest.Start
			h.Press("ctrl+o")
			mustMain(t, h, "back skips the blocked DM", homeKey)
		})
	})
}

// esc in a reply box with nothing typed leaves the input and the thread at once; with
// a draft the first esc only leaves the input (the draft is kept).
func TestHistoryEscFromReplyBox(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		thread := devThreadKey(t)
		t.Run("empty", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDevThread(h)
			h.Press("i")
			h.Press("esc")
			mustMain(t, h, "esc", devKey)
		})
		t.Run("after sending", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDevThread(h)
			h.Press("i")
			h.Type("ok")
			h.Press("enter")
			h.Press("esc")
			mustMain(t, h, "esc", devKey)
		})
		t.Run("draft", func(t *testing.T) {
			h := newHistoryHarness(t, sz)
			openDevThread(h)
			h.Press("i")
			h.Type("초안")
			h.Press("esc")
			mustMain(t, h, "esc 1", thread)
			h.Press("esc")
			mustMain(t, h, "esc 2", devKey)
		})
	})
}

// ctrl+o goes back while a chat message is being typed (the draft is kept);
// alt+left keeps moving the cursor by word there.
func TestHistoryBackWhileTyping(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		h := newHistoryHarness(t, sz)
		openDM(h)
		openDevThread(h)
		thread := devThreadKey(t)
		h.Press("i")
		h.Type("초안 하나")
		h.Press("alt+left")
		mustMain(t, h, "alt+left while typing", thread)
		h.Press("ctrl+o")
		mustMain(t, h, "ctrl+o while typing", devKey)
		h.Press("alt+right")
		mustMain(t, h, "forward", thread)
		mustContain(t, h.Screen(), "초안 하나")
	})
}

// [keys] nav.history_back / thread.channel overrides replace the defaults.
func TestHistoryRebinding(t *testing.T) {
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		h := newHistoryHarness(t, sz, uitest.WithKeys(map[string][]string{
			"nav.history_back":    {"H"},
			"nav.history_forward": {"L"},
			"thread.channel":      {"u"},
		}))
		openDM(h)
		openDevThread(h)
		thread := devThreadKey(t)
		h.Press("c")
		mustMain(t, h, "c unbound", thread)
		h.Press("u")
		mustMain(t, h, "u", devKey)
		h.Press("ctrl+o")
		mustMain(t, h, "ctrl+o unbound", devKey)
		h.Press("H")
		mustMain(t, h, "H", thread)
		h.Press("H", "H")
		mustMain(t, h, "H H", dmKey)
		h.Press("L")
		mustMain(t, h, "L", devKey)
		if sz.H >= 24 {
			h.Press("L")
			mustContain(t, statusRow(h), "u #개발로")
		}
	})
}

// The status line shows the back key once there is somewhere to go back to; a narrow
// tiny line drops it whole.
func TestHistoryStatusHint(t *testing.T) {
	const hint = "ctrl+o 이전 위치"
	forHistorySizes(t, func(t *testing.T, sz uitest.Size) {
		h := newHistoryHarness(t, sz)
		if s := statusRow(h); strings.Contains(s, "이전 위치") {
			t.Fatalf("fresh start: %q", s)
		}
		openDM(h)
		mustContain(t, statusRow(h), hint)
		h.Press("ctrl+o")
		if s := statusRow(h); strings.Contains(s, "이전 위치") {
			t.Fatalf("at the start again: %q", s)
		}
	})
	h := newHistoryHarness(t, uitest.Size{W: 24, H: 14})
	openDM(h)
	if s := statusRow(h); strings.Contains(s, "ctrl+o") || strings.Contains(s, "이전") {
		t.Fatalf("narrow tiny keeps a cut hint: %q", s)
	}
}
