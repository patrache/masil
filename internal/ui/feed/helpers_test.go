package feed

// Shared ui/feed test helpers (plan 05 Task 6): the canned sns API, size sweeps
// and screen/request lookups. Later tasks reuse these; none re-registers the
// table below except on purpose (srv.JSON / srv.Handle after snsAPI).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
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

// snsCfg is the snsAPI configuration built from snsOpts.
type snsCfg struct {
	discoverSince *api.TimelinePage
	following     []api.TimelineItem
	edits         *postEdits
}

type snsOpt func(*snsCfg)

// withDiscoverSince is the GET /v1/timeline/discover response when "since" is
// present (default: discoverNew()).
func withDiscoverSince(p api.TimelinePage) snsOpt {
	return func(c *snsCfg) { c.discoverSince = &p }
}

// withFollowing is the first page of GET /v1/timeline/following (default fixtures.Timeline()).
func withFollowing(items []api.TimelineItem) snsOpt {
	return func(c *snsCfg) { c.following = items }
}

// postEdits lets a test change server data after the screen loaded: GET
// /v1/posts/{id} (and every post in /replies) gets Body[id] when set, and
// /replies of id gets ExtraReplies[id] appended. Read at request time.
type postEdits struct {
	mu           sync.Mutex
	Body         map[string]string
	ExtraReplies map[string][]api.Post
}

// withPostEdits makes e's maps non-nil and serves posts through it.
func withPostEdits(e *postEdits) snsOpt {
	return func(c *snsCfg) {
		e.mu.Lock()
		if e.Body == nil {
			e.Body = map[string]string{}
		}
		if e.ExtraReplies == nil {
			e.ExtraReplies = map[string][]api.Post{}
		}
		e.mu.Unlock()
		c.edits = e
	}
}

// discoverNewID is a 26-char ID greater than every fixture ID.
func discoverNewID(n int) string { return fmt.Sprintf("01J%s%02d", strings.Repeat("Z", 21), n) }

// discoverNew is 2 new items (junho "새 글 하나", minji "새 글 둘", at(0)) plus the
// window's newest fixtures.Timeline item (the overlap); Gap false.
func discoverNew() api.TimelinePage {
	one := mkPost(0, "junho", "새 글 하나", 0)
	one.ID = discoverNewID(1)
	two := mkPost(0, "minji", "새 글 둘", 0)
	two.ID = discoverNewID(2)
	return api.TimelinePage{Items: []api.TimelineItem{itemOf(two), itemOf(one), fixtures.Timeline()[0]}}
}

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

// testImagePNG is fixtures.TestImage() encoded once.
var testImagePNG = sync.OnceValue(func() []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, fixtures.TestImage()); err != nil {
		panic(err)
	}
	return b.Bytes()
})

// seedStore is the Store a test's uitest.WithSeed fills (fixtures.Seed, + Hostile).
func seedStore(hostile bool) *store.Store {
	s := store.New()
	fixtures.Seed(s)
	if hostile {
		fixtures.Hostile(s)
	}
	return s
}

// snsAPI is the canned sns server: every GET of the Task 6 table, the 10
// bootstrap GETs answered from the seeded Store (so a REST bootstrap never
// changes it) and a catch-all that fails the test on any other request.
func snsAPI(t *testing.T, hostile bool, opts ...snsOpt) *apitest.Server {
	t.Helper()
	cfg := snsCfg{}
	for _, o := range opts {
		o(&cfg)
	}
	srv := apitest.New(t)
	registerBootstrap(srv, seedStore(hostile))

	base := fixtures.Timeline()
	if hostile {
		base = append(base, fixtures.HostileTimeline()...)
	}
	following := base
	if cfg.following != nil {
		following = cfg.following
	}
	since := discoverNew()
	if cfg.discoverSince != nil {
		since = *cfg.discoverSince
	}
	posts := allPosts()
	for _, it := range following {
		posts[it.Post.ID] = it.Post
	}
	for _, it := range base {
		posts[it.Post.ID] = it.Post
	}
	edit := func(p api.Post) api.Post {
		if cfg.edits == nil {
			return p
		}
		cfg.edits.mu.Lock()
		defer cfg.edits.mu.Unlock()
		if b, ok := cfg.edits.Body[p.ID]; ok {
			p.Body = b
		}
		return p
	}
	top, children := replyTree()
	deep := deepChain()
	repliesOf := func(id string) []api.Post {
		var out []api.Post
		switch {
		case id == postBy("hana").ID:
			out = append(out, top...)
		case children[id] != nil:
			out = append(out, children[id]...)
		default:
			for i, p := range deep {
				if p.ID == id && i+1 < len(deep) {
					out = append(out, deep[i+1])
				}
			}
		}
		if cfg.edits != nil {
			cfg.edits.mu.Lock()
			out = append(out, cfg.edits.ExtraReplies[id]...)
			cfg.edits.mu.Unlock()
		}
		for i := range out {
			out[i] = edit(out[i])
		}
		return out
	}
	userRef := func(ref string) (string, bool) { // -> handle
		switch {
		case ref == "@me":
			return "sam", true
		case strings.HasPrefix(ref, "@"):
			_, ok := fixtureUsers()[ref[1:]]
			return ref[1:], ok
		}
		for h, u := range fixtureUsers() {
			if u.ID == ref {
				return h, true
			}
		}
		return "", false
	}
	notFound := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"not_found","details":{}}}`))
	}

	srv.JSON("GET /v1/timeline/following", http.StatusOK, api.TimelinePage{Items: following})
	srv.Handle("GET /v1/timeline/discover", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("since") {
			writeJSON(w, since)
			return
		}
		writeJSON(w, api.TimelinePage{Items: base})
	})
	srv.JSON("GET /v1/stories/feed", http.StatusOK, itemsOf(storyGroups()))
	srv.JSON("GET /v1/trends", http.StatusOK, trendsTop10())
	srv.Handle("GET /v1/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := posts[r.PathValue("id")]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, map[string]any{"post": edit(p)})
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
			anc = append([]api.Post{edit(parent)}, anc...)
			cur = parent
		}
		writeJSON(w, api.PostContext{Ancestors: anc, Post: edit(p)})
	})
	srv.Handle("GET /v1/posts/{id}/replies", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := posts[r.PathValue("id")]; !ok {
			notFound(w)
			return
		}
		writeJSON(w, pageOf(repliesOf(r.PathValue("id"))))
	})
	srv.Handle("GET /v1/users/{user}", func(w http.ResponseWriter, r *http.Request) {
		h, ok := userRef(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, map[string]any{"user": profileOf(h, relFollowing())})
	})
	srv.Handle("GET /v1/users/{user}/posts", func(w http.ResponseWriter, r *http.Request) {
		h, ok := userRef(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		filter := r.URL.Query().Get("filter")
		if filter == "" {
			filter = "posts"
		}
		writeJSON(w, pageOf(profilePosts(h, filter)))
	})
	srv.Handle("GET /v1/users/{user}/stories", func(w http.ResponseWriter, r *http.Request) {
		h, ok := userRef(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, itemsOf(userStories(h)))
	})
	srv.Handle("POST /v1/stories/{id}/view", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	srv.Handle("GET /v1/users/{user}/guestbook", func(w http.ResponseWriter, r *http.Request) {
		h, ok := userRef(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, pageOf(guestbookEntries(userOf(h).ID)))
	})
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: notificationsAll(), UnreadCount: len(fixtures.Notifications())})
	srv.Handle("GET /v1/hashtags/{tag}/posts", func(w http.ResponseWriter, r *http.Request) {
		var ps []api.Post
		for _, it := range hashtagPosts(r.PathValue("tag")) {
			ps = append(ps, edit(it.Post))
		}
		writeJSON(w, pageOf(ps))
	})
	srv.JSON("GET /v1/me/follow-requests", http.StatusOK, pageOf(followRequests()))
	srv.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		etag := `"` + r.PathValue("id") + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(testImagePNG())
	})
	srv.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("snsAPI: unregistered %s %s", r.Method, r.URL.Path)
		notFound(w)
	})
	return srv
}

// userStories is GET /v1/users/{user}/stories of handle: its storyGroups()
// group, devKimStories() for dev_kim, else none.
func userStories(handle string) []api.Story {
	if handle == "dev_kim" {
		return devKimStories()
	}
	for _, g := range storyGroups() {
		if g.User.Handle == handle {
			return g.Stories
		}
	}
	return []api.Story{}
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
