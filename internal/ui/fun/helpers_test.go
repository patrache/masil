package fun

// Shared ui/fun test helpers (plan 05 Task 12): the canned bootstrap API, size
// sweeps and screen/request lookups, with the same signatures as ui/feed's
// (test code cannot be shared across packages). Write endpoints are registered
// by each test.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// pageOf wraps items as {"items": [...], "next_cursor": null}.
func pageOf[T any](items []T) map[string]any {
	if items == nil {
		items = []T{}
	}
	return map[string]any{"items": items, "next_cursor": nil}
}

func itemsOf[T any](items []T) map[string]any {
	if items == nil {
		items = []T{}
	}
	return map[string]any{"items": items}
}

// seedStore is the Store a test's uitest.WithSeed fills (fixtures.Seed, + Hostile).
func seedStore(hostile bool) *store.Store {
	s := store.New()
	fixtures.Seed(s)
	if hostile {
		fixtures.Hostile(s)
	}
	return s
}

// funAPI is the canned ui/fun server: the 10 bootstrap GETs answered from the
// seeded Store (so a REST bootstrap never changes it) and a catch-all that
// fails the test on any other request. No sns GETs, no fixtures.RegisterAPI.
func funAPI(t *testing.T) *apitest.Server {
	t.Helper()
	srv := apitest.New(t)
	registerBootstrap(srv, seedStore(false))
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("funAPI: unregistered %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"not_found","details":{}}}`))
	})
	return srv
}

// registerBootstrap answers the events.md 5.3 step-1 GETs from s.
func registerBootstrap(srv *apitest.Server, s *store.Store) {
	srv.JSON("GET /v1/me", http.StatusOK, map[string]any{"user": s.Me})
	spaces := []api.Space{}
	details := map[string]api.SpaceDetail{}
	for _, id := range s.SpaceOrder {
		sd := *s.Spaces[id]
		sd.Channels = []api.Channel{}
		for _, ch := range s.SpaceChannels(id) {
			sd.Channels = append(sd.Channels, *ch)
		}
		spaces = append(spaces, sd.Space)
		details[id] = sd
	}
	srv.JSON("GET /v1/spaces", http.StatusOK, itemsOf(spaces))
	srv.Handle("GET /v1/spaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		sd, ok := details[r.PathValue("id")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"space": sd})
	})
	dms := []api.Channel{}
	for _, id := range s.DMOrder {
		dms = append(dms, *s.Channels[id])
	}
	srv.JSON("GET /v1/dms", http.StatusOK, pageOf(dms))
	unreads := []api.UnreadState{}
	for _, ch := range s.Channels {
		if ch.My.UnreadCount > 0 || ch.My.MentionCount > 0 {
			unreads = append(unreads, api.UnreadState{
				ChannelID: ch.ID, UnreadCount: ch.My.UnreadCount, MentionCount: ch.My.MentionCount,
				LastMessageID: ch.LastMessageID, LastReadMessageID: ch.My.LastReadMessageID,
			})
		}
	}
	sort.Slice(unreads, func(i, j int) bool { return unreads[i].ChannelID < unreads[j].ChannelID })
	srv.JSON("GET /v1/me/unreads", http.StatusOK, itemsOf(unreads))
	srv.JSON("GET /v1/notifications/unread-count", http.StatusOK, map[string]int{"unread_count": s.NotifUnread})
	friends := []api.Friend{}
	for _, f := range s.Friends {
		friends = append(friends, f)
	}
	sort.Slice(friends, func(i, j int) bool { return friends[i].User.Handle < friends[j].User.Handle })
	srv.JSON("GET /v1/friends", http.StatusOK, pageOf(friends))
	incoming := append([]api.FriendRequest{}, s.FriendReqIn...)
	srv.Handle("GET /v1/friends/requests", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("direction") == "outgoing" {
			writeJSON(w, itemsOf([]api.FriendRequest{}))
			return
		}
		writeJSON(w, itemsOf(incoming))
	})
	srv.JSON("GET /v1/me/direct-invites", http.StatusOK, itemsOf(append([]api.DirectInvite{}, s.DirectInvites...)))
	blocks := []api.BlockEntry{}
	for id, since := range s.Blocks {
		if u := s.Users[id]; u != nil {
			blocks = append(blocks, api.BlockEntry{User: *u, CreatedAt: since})
		}
	}
	srv.JSON("GET /v1/blocks", http.StatusOK, itemsOf(blocks))
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

// sweep runs act at every size (t.Run "WxH", fresh harness) and then
// h.Golden(golden), or h.Check() when golden is "".
func sweep(t *testing.T, sizes []uitest.Size, golden string, opts []uitest.Option, act func(h *uitest.Harness)) {
	t.Helper()
	if len(sizes) == 0 {
		t.Fatal("sweep: no sizes")
	}
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := uitest.New(t, opts...)
			h.Resize(s.W, s.H)
			if act != nil {
				act(h)
			}
			if golden == "" {
				h.Check()
				return
			}
			h.Golden(golden)
		})
	}
}

// region cuts r out of an (ansi-stripped) screen, one string per row.
func region(screen string, r layout.Rect) []string {
	lines := strings.Split(screen, "\n")
	out := make([]string, 0, r.H)
	for y := r.Y; y < r.Y+r.H && y < len(lines); y++ {
		if y < 0 {
			continue
		}
		out = append(out, ansi.Cut(lines[y], r.X, r.X+r.W))
	}
	return out
}

// lastReq is the last recorded request with method and (unescaped) path.
func lastReq(h *uitest.Harness, method, path string) (apitest.Recorded, bool) {
	reqs := h.API.Requests()
	for i := len(reqs) - 1; i >= 0; i-- {
		r := reqs[i]
		p, err := url.PathUnescape(r.Path)
		if err != nil {
			p = r.Path
		}
		if r.Method == method && (p == path || r.Path == path) {
			return r, true
		}
	}
	return apitest.Recorded{}, false
}

// reqsTo is every recorded request with method and (unescaped) path.
func reqsTo(h *uitest.Harness, method, path string) []apitest.Recorded {
	var out []apitest.Recorded
	for _, r := range h.API.Requests() {
		p, err := url.PathUnescape(r.Path)
		if err != nil {
			p = r.Path
		}
		if r.Method == method && p == path {
			out = append(out, r)
		}
	}
	return out
}

// sendCmd runs cmd, flattens tea.BatchMsg recursively and Sends every msg.
func sendCmd(h *uitest.Harness, cmd tea.Cmd) {
	h.T.Helper()
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			sendCmd(h, c)
		}
	default:
		h.Send(m)
	}
}

// cellOf is the cell position of the first occurrence of s in screen.
func cellOf(t *testing.T, screen, s string) (x, y int) {
	t.Helper()
	for i, line := range strings.Split(screen, "\n") {
		if j := strings.Index(line, s); j >= 0 {
			return ansi.StringWidth(line[:j]), i
		}
	}
	t.Fatalf("cellOf: %q not on screen:\n%s", s, screen)
	return 0, 0
}

// writeStatus writes v as JSON with status.
func writeStatus(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(b)
}

// squash drops spaces and box-drawing characters.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || (r >= 0x2500 && r <= 0x257F) {
			return -1
		}
		return r
	}, s)
}

// overlayText is the ansi-stripped text of the top overlay box.
func overlayText(h *uitest.Harness) string {
	f := h.Model.Frame()
	if len(f.Overlays) == 0 {
		return ""
	}
	return strings.Join(region(h.Screen(), f.Overlays[len(f.Overlays)-1].Rect), "\n")
}

// toastText is the squashed text of the toast box (the whole screen when the
// toast shows in the status line).
func toastText(h *uitest.Harness) string {
	s := h.Screen()
	if tb := h.Model.Frame().Toast; tb != nil {
		return squash(strings.Join(region(s, tb.Rect.Inner()), ""))
	}
	return squash(s)
}

// postBody decodes a recorded JSON request body.
func postBody(t *testing.T, r apitest.Recorded) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("request body %q: %v", r.Body, err)
	}
	return m
}
