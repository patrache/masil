package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Routes of the scenarios.
var (
	devRoute     = ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
	feedRoute    = ui.Route{Kind: ui.RouteFeed, Arg: "following"}
	hostileRoute = ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChHostile}
)

// hostileHandle is the 32-char handle of the hostile set (fixtures.Hostile).
const hostileHandle = "hostile_abcdefghijklmnopqrstuvwx"

// joined is the fixed joined_at of every canned member.
var joined = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// fullSeed is fixtures.Seed + fixtures.Hostile, bootstrapped (the sweeps' Store).
func fullSeed(s *store.Store) {
	fixtures.Seed(s)
	fixtures.Hostile(s)
	s.Sync.Bootstrapped = true
}

// productSeed is fixtures.Seed, bootstrapped (the goldens' Store).
func productSeed(s *store.Store) {
	fixtures.Seed(s)
	s.Sync.Bootstrapped = true
}

// newHarness is a logged-in harness on the product API at sz.
func newHarness(t *testing.T, sz uitest.Size, seed func(*store.Store), opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	all := append([]uitest.Option{uitest.WithAPI(productAPI(t)), uitest.WithSeed(seed)}, opts...)
	h := uitest.New(t, all...)
	h.Resize(sz.W, sz.H)
	return h
}

// productAPI is fixtures.RegisterAPI plus the read-only GETs (and the read
// marker PUT) the real chat and sns-fun screens send while they are only
// looked at. Anything else is a test failure: the sweeps press no write keys.
func productAPI(t *testing.T) *apitest.Server {
	t.Helper()
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)

	s := store.New()
	fullSeed(s)
	users := map[string]api.UserSummary{}
	for _, u := range s.Users {
		users[u.ID] = *u
	}
	userRef := func(ref string) (api.UserSummary, bool) {
		if ref == string(api.MeRef) {
			ref = fixtures.UserSam
		}
		for _, u := range users {
			if u.ID == ref || "@"+u.Handle == ref {
				return u, true
			}
		}
		return api.UserSummary{}, false
	}
	posts := map[string]api.Post{}
	var order []api.Post // newest first, deterministic
	byAuthor := map[string][]api.TimelineItem{}
	for _, it := range append(fixtures.Timeline(), fixtures.HostileTimeline()...) {
		if _, dup := posts[it.Post.ID]; dup {
			continue
		}
		posts[it.Post.ID] = it.Post
		order = append(order, it.Post)
		byAuthor[it.Post.Author.ID] = append(byAuthor[it.Post.Author.ID], it)
	}

	srv.Handle("PUT /v1/channels/{id}/read", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			MessageID string `json:"message_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		st := api.UnreadState{ChannelID: r.PathValue("id"), LastReadMessageID: &in.MessageID}
		if ch := s.Channels[r.PathValue("id")]; ch != nil {
			st.LastMessageID = ch.LastMessageID
		}
		writeJSON(w, map[string]any{"state": st})
	})
	srv.Handle("GET /v1/channels/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		items := []api.ChannelMember{}
		if ch := s.Channels[r.PathValue("id")]; ch != nil && len(ch.Members) > 0 {
			for _, m := range ch.Members {
				items = append(items, api.ChannelMember{User: m, JoinedAt: joined})
			}
		} else {
			for _, id := range []string{fixtures.UserSam, fixtures.UserMinji, fixtures.UserJunho, fixtures.UserHana, fixtures.UserDevKim} {
				items = append(items, api.ChannelMember{User: users[id], JoinedAt: joined})
			}
		}
		writeJSON(w, map[string]any{"items": items, "next_cursor": nil})
	})
	srv.Handle("GET /v1/presence", func(w http.ResponseWriter, r *http.Request) {
		items := []api.PresenceItem{}
		for _, id := range strings.Split(r.URL.Query().Get("user_ids"), ",") {
			if p, ok := s.Presence[id]; ok {
				items = append(items, api.PresenceItem{UserID: id, Status: p.Status, LastSeenAt: p.LastSeenAt})
			}
		}
		writeJSON(w, map[string]any{"items": items})
	})
	srv.JSON("GET /v1/stories/feed", http.StatusOK, map[string]any{"items": []api.StoryGroup{}})
	img := testImagePNG(t)
	srv.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(img)
	})
	srv.Handle("GET /v1/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := posts[r.PathValue("id")]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, map[string]any{"post": p})
	})
	srv.Handle("GET /v1/posts/{id}/context", func(w http.ResponseWriter, r *http.Request) {
		p, ok := posts[r.PathValue("id")]
		if !ok {
			notFound(w)
			return
		}
		anc := []api.Post{}
		for cur := p; cur.ReplyToID != nil; {
			parent, ok := posts[*cur.ReplyToID]
			if !ok {
				break
			}
			anc = append([]api.Post{parent}, anc...)
			cur = parent
		}
		writeJSON(w, api.PostContext{Ancestors: anc, Post: p})
	})
	srv.Handle("GET /v1/posts/{id}/replies", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := posts[id]; !ok {
			notFound(w)
			return
		}
		items := []api.Post{}
		for _, p := range order {
			if p.ReplyToID != nil && *p.ReplyToID == id {
				items = append(items, p)
			}
		}
		writeJSON(w, map[string]any{"items": items, "next_cursor": nil})
	})
	srv.Handle("GET /v1/users/{user}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := userRef(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, map[string]any{"user": profileOf(u)})
	})
	srv.Handle("GET /v1/users/{user}/posts", func(w http.ResponseWriter, r *http.Request) {
		u, ok := userRef(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		items := append([]api.TimelineItem{}, byAuthor[u.ID]...)
		writeJSON(w, map[string]any{"items": items, "next_cursor": nil})
	})
	srv.Handle("GET /v1/users/{user}/stories", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": []api.Story{}})
	})
	srv.Handle("GET /v1/users/{user}/guestbook", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": []api.GuestbookEntry{}, "next_cursor": nil})
	})
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("productAPI: unregistered %s %s", r.Method, r.URL.EscapedPath())
		notFound(w)
	})
	return srv
}

// profileOf is the canned profile of u: the hostile users get a control-laden
// bio and huge counts, everyone else a short one.
func profileOf(u api.UserSummary) api.Profile {
	p := api.Profile{UserSummary: u, Bio: u.DisplayName + "의 소개", CreatedAt: joined,
		FollowerCount: 12, FollowingCount: 8, PostCount: 3,
		Relationship: &api.Relationship{Following: "active", FollowedBy: "none", CanDM: true, CanViewPosts: true, SharedSpaceIDs: []string{}}}
	if strings.HasPrefix(u.Handle, "hostile") {
		p.Bio = "소개\x1b[31m빨강\x1b[0m\t탭\r\u0085‮뒤집기⁦ " + strings.Repeat("공백없는아주긴소개문장", 20) + "\n둘째 줄 ❄️ 👨‍👩‍👧"
		p.FollowerCount, p.FollowingCount, p.PostCount = 123456789, 98765, 4321
	}
	if u.ID == fixtures.UserSam {
		p.Relationship = nil
	}
	return p
}

func testImagePNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, fixtures.TestImage()); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
	return b.Bytes()
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"not_found","details":{}}}`))
}

// ---- navigation ----

// openDev opens #개발.
func openDev(h *uitest.Harness) { h.Send(ui.NavigateMsg{Route: devRoute}) }

// selectDevRoot selects the 민지 11:02 thread root of #개발 (the first message:
// k clamps at the top).
func selectDevRoot(h *uitest.Harness) {
	for range 12 {
		h.Press("k")
	}
}

// openDevThread opens #개발, selects the 민지 message and opens its thread.
func openDevThread(h *uitest.Harness) {
	openDev(h)
	selectDevRoot(h)
	h.Press("enter")
}

// openFeed opens the following feed surface.
func openFeed(h *uitest.Harness) { h.Send(ui.NavigateMsg{Route: feedRoute}) }

// openNotifications opens the notification inbox (ctrl+n).
func openNotifications(h *uitest.Harness) { h.Press("ctrl+n") }

// openPalette opens the command palette over #개발.
func openPalette(h *uitest.Harness) {
	openDev(h)
	h.Press("ctrl+k")
}

// ---- observations through public surfaces only (no chat/feed internals) ----

// devRootID is the id of the #개발 thread root.
func devRootID(t *testing.T) string {
	t.Helper()
	for _, m := range fixtures.Messages(fixtures.ChDev) {
		if m.Thread != nil && m.Thread.ReplyCount > 0 {
			return m.ID
		}
	}
	t.Fatal("no #개발 thread root")
	return ""
}

// devMessage is the #개발 fixture message with id.
func devMessage(t *testing.T, id string) api.Message {
	t.Helper()
	for _, m := range fixtures.Messages(fixtures.ChDev) {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("%s is not a #개발 message", id)
	return api.Message{}
}

// activeThread is the thread open in the active tab (nil when none).
func activeThread(h *uitest.Harness) *ui.Route {
	n := h.Nav()
	if n.Active < 0 || n.Active >= len(n.Tabs) {
		return nil
	}
	return n.Tabs[n.Active].Thread
}

// probeSelection reads the selected #개발 message the way a person does: `t`
// opens the selected message's thread, `esc` closes it again. It returns the
// thread root id ("" when nothing was selected) and checks the channel is the
// focused Main again afterwards.
func probeSelection(t *testing.T, h *uitest.Harness) string {
	t.Helper()
	if activeThread(h) != nil {
		t.Fatalf("probeSelection with a thread already open: %+v", activeThread(h))
	}
	h.Press("t")
	thr := activeThread(h)
	if thr == nil {
		return ""
	}
	id := thr.ID
	h.Press("esc")
	if activeThread(h) != nil {
		t.Fatalf("esc did not close the thread: %+v", activeThread(h))
	}
	return id
}

// mustSlot fails unless slot shows the route key want.
func mustSlot(t *testing.T, h *uitest.Harness, slot layout.SlotID, want string) {
	t.Helper()
	if got := h.Model.Frame().Routes[slot].Key(); got != want {
		t.Fatalf("slot %s = %q, want %q (routes %v)", slot, got, want, h.Model.Frame().Routes)
	}
}

// focusSlot is the focused slot.
func focusSlot(h *uitest.Harness) layout.SlotID { return h.Model.Focus().Slot }

// uniqueRouteKeys fails when one route key is projected into two slots.
func uniqueRouteKeys(t *testing.T, h *uitest.Harness) {
	t.Helper()
	seen := map[string]layout.SlotID{}
	for s, r := range h.Model.Frame().Routes {
		if prev, dup := seen[r.Key()]; dup {
			t.Fatalf("route %q in two slots: %s and %s", r.Key(), prev, s)
		}
		seen[r.Key()] = s
	}
}

// savedSelected is the Selected field of a Persistent component's saved state
// (for side-effect-free SaveState implementations such as the feed timeline).
func savedSelected(t *testing.T, c ui.Component) string {
	t.Helper()
	p, ok := c.(ui.Persistent)
	if !ok {
		t.Fatalf("%T is not Persistent", c)
	}
	v := reflect.ValueOf(p.SaveState())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	f := v.FieldByName("Selected")
	if !f.IsValid() || f.Kind() != reflect.String {
		t.Fatalf("%T saved state %T has no Selected string", c, p.SaveState())
	}
	return f.String()
}

// sizeName is the "WxH" subtest name.
func sizeName(s uitest.Size) string { return fmt.Sprintf("%dx%d", s.W, s.H) }

// statusRow is the last screen line.
func statusRow(h *uitest.Harness) string {
	lines := strings.Split(h.Screen(), "\n")
	return lines[len(lines)-1]
}
