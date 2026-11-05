package chat

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- store lookups (other plans find fixture entities through the Store) ----

// seeded is a fresh Store with fixtures.Seed applied.
func seeded() *store.Store {
	s := store.New()
	fixtures.Seed(s)
	return s
}

// chanID returns the id of the text channel named name.
func chanID(t testing.TB, s *store.Store, name string) string {
	t.Helper()
	for _, ch := range s.Channels {
		if ch.Kind == api.ChannelText && ch.Name != nil && *ch.Name == name {
			return ch.ID
		}
	}
	t.Fatalf("no text channel %q", name)
	return ""
}

// dmWith returns the id of the 1:1 DM with the user whose handle is handle.
func dmWith(t testing.TB, s *store.Store, handle string) string {
	t.Helper()
	for _, ch := range s.Channels {
		if ch.Kind != api.ChannelDM {
			continue
		}
		for _, m := range ch.Members {
			if m.Handle == handle && !s.IsMe(m.ID) {
				return ch.ID
			}
		}
	}
	t.Fatalf("no DM with %q", handle)
	return ""
}

// userBy returns the cached user with handle.
func userBy(t testing.TB, s *store.Store, handle string) api.UserSummary {
	t.Helper()
	for _, u := range s.Users {
		if u.Handle == handle {
			return *u
		}
	}
	t.Fatalf("no user %q", handle)
	return api.UserSummary{}
}

// spaceByName returns the id of the space named name.
func spaceByName(t testing.TB, s *store.Store, name string) string {
	t.Helper()
	for _, id := range s.SpaceOrder {
		if sd := s.Spaces[id]; sd != nil && sd.Name == name {
			return id
		}
	}
	t.Fatalf("no space %q", name)
	return ""
}

// hostileChannel returns the first text channel of the hostile space (s must hold fixtures.Hostile).
func hostileChannel(t testing.TB, s *store.Store) string {
	t.Helper()
	if chs := s.SpaceChannels(fixtures.SpaceHostile); len(chs) > 0 {
		return chs[0].ID
	}
	t.Fatal("no hostile channel (seed fixtures.Hostile first)")
	return ""
}

// ---- test API server ----

// chatRoute is one chat-only route: its pattern, a concrete sample request
// ("METHOD /path?query") and the handler.
type chatRoute struct {
	Pattern, Sample string
	H               http.HandlerFunc
}

// routeNow is the time stamped on echoed messages (the harness start clock).
var routeNow = func() time.Time { return uitest.Start }

// echoSeq numbers echoed message ids; "01JZ…" sorts after every fixture id.
var echoSeq atomic.Int64

func echoID() string { return fmt.Sprintf("01JZECH%019d", echoSeq.Add(1)) }

// chatRouteTable lists the routes fixtures.RegisterAPI does not serve (Global
// Constraints test API rule (c)); TestChatRoutesDisjointFromFixtures keeps it so.
var chatRouteTable = []chatRoute{
	{"POST /v1/channels/{id}/messages", "POST /v1/channels/01JBPROBE00000000000000000/messages", echoSend(false)},
	{"POST /v1/messages/{id}/thread", "POST /v1/messages/01JDPROBE00000000000000000/thread", echoSend(true)},
	{"PATCH /v1/messages/{id}", "PATCH /v1/messages/01JDPROBE00000000000000000", echoEdit},
	{"DELETE /v1/messages/{id}", "DELETE /v1/messages/01JDPROBE00000000000000000", noContent},
	{"PUT /v1/messages/{id}/reactions/{emoji}", "PUT /v1/messages/01JDPROBE00000000000000000/reactions/%F0%9F%91%8D", noContent},
	{"DELETE /v1/messages/{id}/reactions/{emoji}", "DELETE /v1/messages/01JDPROBE00000000000000000/reactions/%F0%9F%91%8D", noContent},
	{"PUT /v1/messages/{id}/pin", "PUT /v1/messages/01JDPROBE00000000000000000/pin", noContent},
	{"DELETE /v1/messages/{id}/pin", "DELETE /v1/messages/01JDPROBE00000000000000000/pin", noContent},
	{"PUT /v1/channels/{id}/read", "PUT /v1/channels/01JBPROBE00000000000000000/read", echoRead},
	{"GET /v1/channels/{id}/members", "GET /v1/channels/01JBPROBE00000000000000000/members", channelMembers},
	{"GET /v1/presence", "GET /v1/presence?user_ids=" + fixtures.UserMinji + "," + fixtures.UserJunho, presence},
}

// newAPI is apitest.New + fixtures.RegisterAPI + chatRoutes, in this order (tests overlay after it).
func newAPI(t testing.TB) *apitest.Server {
	t.Helper()
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	chatRoutes(t, srv)
	return srv
}

// chatRoutes registers chatRouteTable on srv (same shape again = replace, no panic).
func chatRoutes(t testing.TB, srv *apitest.Server) {
	t.Helper()
	for _, r := range chatRouteTable {
		srv.Handle(r.Pattern, r.H)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

// fixtureMessage finds a canned top-level message by id.
func fixtureMessage(id string) (api.Message, bool) {
	for cid := range seeded().Channels {
		for _, m := range fixtures.Messages(cid) {
			if m.ID == id {
				return m, true
			}
		}
	}
	return api.Message{}, false
}

// echoSend answers a send with the posted client_nonce, kind, body and poll (201, created).
func echoSend(thread bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClientNonce string         `json:"client_nonce"`
			Kind        string         `json:"kind"`
			Body        string         `json:"body"`
			Poll        *api.PollInput `json:"poll"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.Kind == "" {
			in.Kind = "text"
		}
		me := seeded().Me.UserSummary
		nonce := in.ClientNonce
		m := api.Message{
			ID: echoID(), ChannelID: r.PathValue("id"), Author: me, Kind: in.Kind, Body: in.Body,
			Mentions: []api.MentionRef{}, Reactions: []api.ReactionSummary{}, ClientNonce: &nonce, CreatedAt: routeNow(),
		}
		if thread {
			root := r.PathValue("id")
			m.ThreadRootID = &root
			m.ChannelID = ""
			if rm, ok := fixtureMessage(root); ok {
				m.ChannelID = rm.ChannelID
			}
		}
		if in.Kind == "poll" && in.Poll != nil {
			p := &api.Poll{ID: "01JGPECH" + m.ID[8:], ContextType: "message", ContextID: m.ID, CreatorID: me.ID,
				Question: in.Poll.Question, Multi: in.Poll.Multi, MyVotes: []int{}}
			for i, o := range in.Poll.Options {
				p.Options = append(p.Options, api.PollOption{Idx: i, Text: o})
			}
			m.Poll = p
		}
		writeJSON(w, http.StatusCreated, map[string]any{"message": m, "created": true})
	}
}

// echoEdit answers PATCH /v1/messages/{id} with the canned message (or a stub) carrying the new body.
func echoEdit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	id := r.PathValue("id")
	m, ok := fixtureMessage(id)
	if !ok {
		m = api.Message{ID: id, Author: seeded().Me.UserSummary, Kind: "text", CreatedAt: routeNow()}
	}
	at := routeNow()
	m.Body, m.EditedAt = in.Body, &at
	writeJSON(w, http.StatusOK, map[string]any{"message": m})
}

// echoRead answers PUT /v1/channels/{id}/read: counters 0, read pointer = the posted message_id.
func echoRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MessageID string `json:"message_id"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	id := r.PathValue("id")
	st := api.UnreadState{ChannelID: id, LastReadMessageID: &in.MessageID}
	if ch := seeded().Channels[id]; ch != nil {
		st.LastMessageID = ch.LastMessageID
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": st})
}

// memberHandles is the channel member list of every channel (seeded users).
var memberHandles = []string{"sam", "minji", "junho", "hana", "dev_kim"}

func channelMembers(w http.ResponseWriter, _ *http.Request) {
	s := seeded()
	items := []api.ChannelMember{}
	for _, h := range memberHandles {
		for _, u := range s.Users {
			if u.Handle == h {
				items = append(items, api.ChannelMember{User: *u, JoinedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}

// presence answers GET /v1/presence?user_ids= from the fixture presence (me = my own state).
func presence(w http.ResponseWriter, r *http.Request) {
	s := seeded()
	items := []api.PresenceItem{}
	for _, id := range strings.Split(r.URL.Query().Get("user_ids"), ",") {
		if s.IsMe(id) {
			items = append(items, api.PresenceItem{UserID: id, Status: s.Self.Status})
			continue
		}
		if p, ok := s.Presence[id]; ok {
			items = append(items, api.PresenceItem{UserID: id, Status: p.Status, LastSeenAt: p.LastSeenAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---- harness helpers ----

// open is a harness on newAPI with route r first, resized to sz.
func open(t *testing.T, sz uitest.Size, r ui.Route, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	return openWith(t, sz, r, newAPI(t), opts...)
}

// openWith is open on a given server.
func openWith(t *testing.T, sz uitest.Size, r ui.Route, srv *apitest.Server, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	all := append([]uitest.Option{uitest.WithAPI(srv), uitest.WithRoute(r)}, opts...)
	h := uitest.New(t, all...)
	h.Resize(sz.W, sz.H)
	return h
}

// openHome opens RouteHome.
func openHome(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return open(t, sz, ui.Route{Kind: ui.RouteHome})
}

// forEachSize runs fn in a "WxH" subtest per size with a harness built by mk.
func forEachSize(t *testing.T, sizes []uitest.Size, mk func(t *testing.T, sz uitest.Size) *uitest.Harness, fn func(t *testing.T, h *uitest.Harness)) {
	t.Helper()
	if len(sizes) == 0 {
		t.Fatal("forEachSize: no sizes")
	}
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			fn(t, mk(t, sz))
		})
	}
}

// bootstrappedSeed is fixtures.Seed plus Sync.Bootstrapped (Seed does not promise
// it; "the channel is gone" notices rely on it).
func bootstrappedSeed(s *store.Store) {
	fixtures.Seed(s)
	s.Sync.Bootstrapped = true
}

// devChannel opens the #개발 channel on the bootstrapped fixture seed.
func devChannel(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return open(t, sz, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, uitest.WithSeed(bootstrappedSeed))
}

// hostileOpen opens the hostile channel on the fixture seed plus fixtures.Hostile.
func hostileOpen(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	s := seeded()
	fixtures.Hostile(s)
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		fixtures.Hostile(s)
	}
	return open(t, sz, ui.Route{Kind: ui.RouteChannel, ID: hostileChannel(t, s)}, uitest.WithSeed(seed))
}

// top is the top route of the active tab's stack.
func top(h *uitest.Harness) ui.Route {
	nav := h.Nav()
	if nav.Active < 0 || nav.Active >= len(nav.Tabs) || len(nav.Tabs[nav.Active].Stack) == 0 {
		return ui.Route{}
	}
	st := nav.Tabs[nav.Active].Stack
	return st[len(st)-1]
}

// cropRect cuts r out of a plain screen (cell columns via ansi.Cut), one string per row.
func cropRect(screen string, r layout.Rect) []string {
	lines := strings.Split(screen, "\n")
	var out []string
	for y := r.Y; y < r.Y+r.H; y++ {
		if y < 0 || y >= len(lines) {
			out = append(out, "")
			continue
		}
		out = append(out, ansi.Cut(lines[y], r.X, r.X+r.W))
	}
	return out
}

// columnText is the rows of screen inside r, joined by newlines.
func columnText(screen string, r layout.Rect) string {
	return strings.Join(cropRect(screen, r), "\n")
}

// mustContainInOrder fails unless s contains every sub, in this order.
func mustContainInOrder(t testing.TB, s string, subs ...string) {
	t.Helper()
	rest := s
	for _, sub := range subs {
		i := strings.Index(rest, sub)
		if i < 0 {
			t.Fatalf("%q not found (in order %q) in:\n%s", sub, subs, s)
		}
		rest = rest[i+len(sub):]
	}
}

// paneGolden checks the whole shell (h.Check), then stores only the slot's OUTER
// rect (border included) as testdata/<Test>/<WxH>/<name>.golden — "(no pane)"
// when this size has no such slot — so changes around the pane never touch it.
func paneGolden(t *testing.T, h *uitest.Harness, slot layout.SlotID, name string) {
	t.Helper()
	h.Check()
	t.Run(name, func(t *testing.T) {
		t.Helper()
		screen := h.Screen()
		out := "(no pane)"
		if p, ok := h.Model.Frame().Layout.Pane(slot); ok {
			out = strings.Join(cropRect(screen, p.Rect), "\n")
		}
		golden.RequireEqual(t, out)
	})
}

// clickText clicks the first cell of text inside slot's inner rect.
func clickText(t testing.TB, h *uitest.Harness, slot layout.SlotID, text string) {
	t.Helper()
	screen := h.Screen()
	p, ok := h.Model.Frame().Layout.Pane(slot)
	if !ok {
		t.Fatalf("no %s pane", slot)
	}
	in := p.Rect
	if p.Bordered {
		in = in.Inner()
	}
	for i, row := range cropRect(screen, in) {
		if j := strings.Index(row, text); j >= 0 {
			h.Click(in.X+textwidth.Width(row[:j]), in.Y+i)
			return
		}
	}
	t.Fatalf("%q not in the %s pane:\n%s", text, slot, columnText(screen, in))
}

// overlaySizes are the overlay golden sizes: Required + 44x16 + 20x6.
var overlaySizes = append(slices.Clone(uitest.Required), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})

// recordRT is a ui.Realtime that records calls (Typed: "<ch>|<root>").
type recordRT struct {
	Typed   []string
	Watched [][]string
	Idle    []bool
}

var _ ui.Realtime = (*recordRT)(nil)

func (r *recordRT) Typing(channelID string, threadRootID *string) {
	root := ""
	if threadRootID != nil {
		root = *threadRootID
	}
	r.Typed = append(r.Typed, channelID+"|"+root)
}
func (r *recordRT) SetIdle(idle bool)     { r.Idle = append(r.Idle, idle) }
func (r *recordRT) Watch(topics []string) { r.Watched = append(r.Watched, slices.Clone(topics)) }
func (r *recordRT) State() realtime.State { return realtime.State{} }

// reqs returns the recorded requests with method and (escaped) path.
func reqs(srv *apitest.Server, method, path string) []apitest.Recorded {
	var out []apitest.Recorded
	for _, r := range srv.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// lineWith returns the row and cell column of the first occurrence of s on screen.
func lineWith(t testing.TB, screen, s string) (row, col int) {
	t.Helper()
	for i, l := range strings.Split(screen, "\n") {
		if j := strings.Index(l, s); j >= 0 {
			return i, textwidth.Width(l[:j])
		}
	}
	t.Fatalf("%q not on screen:\n%s", s, screen)
	return -1, -1
}

// msgID is a test message id ("01JTTEST" + 18 digits). The prefix sorts after
// every canned fixture message id ("01JD…"), so a test message created "now" in a
// fixture channel lands inside its latest window (Window.Covers), and before the
// echoed sends ("01JZ…").
func msgID(i int) string { return fmt.Sprintf("01JTTEST%018d", i) }

// mkMsg builds a text message.
func mkMsg(ch string, i int, author api.UserSummary, body string, at time.Time) api.Message {
	return api.Message{ID: msgID(i), ChannelID: ch, Author: author, Kind: "text", Body: body, CreatedAt: at}
}

// sameColor compares two colors by RGBA.
func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// ---- tests of the helpers ----

func TestChatRoutesDisjointFromFixtures(t *testing.T) {
	// apitest re-registration of the same shape REPLACES silently (plan 03 Task 8), so overlap with RegisterAPI is caught here, not at Handle time.
	probe := apitest.New(t)
	fixtures.RegisterAPI(probe) // separate server: probe requests never show up in a test server's Requests()
	for _, r := range chatRouteTable {
		method, path, _ := strings.Cut(r.Sample, " ")
		req, _ := http.NewRequest(method, probe.URL+path, strings.NewReader("{}"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var env struct {
			Error struct{ Code string } `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&env)
		res.Body.Close()
		if res.StatusCode != 404 || env.Error.Code != "not_found" {
			t.Fatalf("fixtures.RegisterAPI now serves %s — drop it from chatRoutes", r.Pattern)
		}
	}
}

func TestChatRoutesServe(t *testing.T) {
	srv := newAPI(t)
	for _, r := range chatRouteTable {
		method, path, _ := strings.Cut(r.Sample, " ")
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader("{}"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode/100 != 2 {
			t.Fatalf("%s: status %d", r.Sample, res.StatusCode)
		}
	}

	cl := srv.Client()
	ctx := t.Context()
	m, created, err := cl.SendMessage(ctx, fixtures.ChDev, api.SendMessageRequest{ClientNonce: "01JCNONCE0000000000000000A", Body: "안녕"})
	if err != nil || !created || m.ClientNonce == nil || *m.ClientNonce != "01JCNONCE0000000000000000A" || m.Body != "안녕" || m.ChannelID != fixtures.ChDev {
		t.Fatalf("echo: %+v created=%v err=%v", m, created, err)
	}
	if !seeded().IsMe(m.Author.ID) || !m.CreatedAt.Equal(uitest.Start) {
		t.Fatalf("echo author/time: %+v", m)
	}
	r, _, err := cl.ReplyThread(ctx, "01JDDEV0000000000000000001", api.ThreadReplyRequest{ClientNonce: "N2", Body: "답글"})
	if err != nil || r.ThreadRootID == nil || *r.ThreadRootID != "01JDDEV0000000000000000001" || r.ChannelID != fixtures.ChDev {
		t.Fatalf("thread echo: %+v err=%v", r, err)
	}
	st, err := cl.MarkRead(ctx, fixtures.ChDev, "01JDDEV0000000000000000004")
	if err != nil || st.UnreadCount != 0 || st.MentionCount != 0 || st.LastReadMessageID == nil || *st.LastReadMessageID != "01JDDEV0000000000000000004" {
		t.Fatalf("read: %+v err=%v", st, err)
	}
	mem, err := cl.ListChannelMembers(ctx, fixtures.ChDev, api.PageOpts{})
	if err != nil || len(mem.Items) != 5 || mem.Items[0].User.Handle != "sam" || mem.Items[4].User.Handle != "dev_kim" {
		t.Fatalf("members: %+v err=%v", mem, err)
	}
	pr, err := cl.GetPresence(ctx, []string{fixtures.UserMinji, fixtures.UserJunho, "01JA0000000000000000NOBODY"})
	if err != nil || len(pr) != 2 || pr[0].Status != api.PresenceOnline || pr[1].Status != api.PresenceOffline {
		t.Fatalf("presence: %+v err=%v", pr, err)
	}

	// rule (b): an exact path wins for that id only.
	srv.JSON("POST /v1/channels/"+fixtures.ChDev+"/messages", http.StatusCreated, map[string]any{"message": api.Message{ID: "01JZEXACT0000000000000000A", ChannelID: fixtures.ChDev}})
	if m, _, err := cl.SendMessage(ctx, fixtures.ChDev, api.SendMessageRequest{Body: "x"}); err != nil || m.ID != "01JZEXACT0000000000000000A" {
		t.Fatalf("exact override: %+v err=%v", m, err)
	}
	if m, _, err := cl.SendMessage(ctx, fixtures.ChGeneral, api.SendMessageRequest{ClientNonce: "N3", Body: "y"}); err != nil || m.ClientNonce == nil || *m.ClientNonce != "N3" {
		t.Fatalf("other id must still echo: %+v err=%v", m, err)
	}
	// rule (a): the same wildcard shape replaces it for every id, no panic.
	srv.Error("POST /v1/channels/{channel_id}/messages", http.StatusInternalServerError, "internal") // replaces the whole echo route for this test
	if _, _, err := cl.SendMessage(ctx, fixtures.ChGeneral, api.SendMessageRequest{Body: "z"}); !api.IsCode(err, "internal") {
		t.Fatalf("wildcard replace: err=%v", err)
	}
	if n := len(reqs(srv, "PUT", "/v1/channels/"+fixtures.ChDev+"/read")); n != 1 {
		t.Fatalf("reqs read = %d", n)
	}
}

// paneText is the text inside slot's inner rect (rows joined by newlines); fails without the pane.
func paneText(t testing.TB, h *uitest.Harness, slot layout.SlotID) string {
	t.Helper()
	p, ok := h.Model.Frame().Layout.Pane(slot)
	if !ok {
		t.Fatalf("no %s pane", slot)
	}
	in := p.Rect
	if p.Bordered {
		in = in.Inner()
	}
	return columnText(h.Screen(), in)
}

// ---- channel view helpers (Task 8) ----

// devID is the id of #개발 in h's Store.
func devID(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	return chanID(t, h.Context().Store, "개발")
}

// channelOf is the conversation instance of the active tab: the top-most
// RouteChannel of its stack.
func channelOf(t testing.TB, h *uitest.Harness) *channelView {
	t.Helper()
	nav := h.Nav()
	if nav.Active >= 0 && nav.Active < len(nav.Tabs) {
		st := nav.Tabs[nav.Active].Stack
		for i := len(st) - 1; i >= 0; i-- {
			if st[i].Kind != ui.RouteChannel {
				continue
			}
			if v, ok := h.Instance(st[i].Key()).(*channelView); ok {
				return v
			}
		}
	}
	t.Fatalf("no channel view in the active tab: %+v", nav)
	return nil
}

// selectedID is the id of the selected row of the channel view ("" = none).
func selectedID(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	r, ok := channelOf(t, h).pane.selected()
	if !ok {
		return ""
	}
	return r.ID
}

// genMsgs builds messages msgID(from..to) in ch, one minute apart, ending at
// the harness start (bodies "메시지 <i>", authors minji/junho alternating).
func genMsgs(ch string, from, to int) []api.Message {
	s := seeded()
	authors := []api.UserSummary{*s.User(fixtures.UserMinji), *s.User(fixtures.UserJunho)}
	var out []api.Message
	for i := from; i <= to; i++ {
		at := uitest.Start.Add(-time.Duration(100-i) * time.Minute)
		out = append(out, mkMsg(ch, i, authors[i%2], fmt.Sprintf("메시지 %d", i), at))
	}
	return out
}

// pageBody is a GET /v1/channels/{id}/messages answer.
func pageBody(items []api.Message, hasMoreBefore, hasMoreAfter bool) api.MessageWindow {
	if items == nil {
		items = []api.Message{}
	}
	return api.MessageWindow{Items: items, HasMoreBefore: hasMoreBefore, HasMoreAfter: hasMoreAfter}
}

// withTestChannel is the bootstrapped fixture seed plus a text channel "테스트"
// (id) in 노바 개발방 with My.UnreadCount = unread and no read pointer.
func withTestChannel(id string, unread int) func(*store.Store) {
	return func(s *store.Store) {
		bootstrappedSeed(s)
		space := fixtures.SpaceNova
		name := "테스트"
		s.ObserveChannel(api.Channel{
			ID: id, Kind: api.ChannelText, SpaceID: &space, Name: &name, Position: 9, MemberCount: 5,
			CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			My:        api.ChannelMyState{Notify: "all", UnreadCount: unread},
		})
		s.Sync.Bootstrapped = true
	}
}

// jsonField is the string field key of a JSON object body ("" when absent).
func jsonField(body []byte, key string) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// lastQuery is the raw query of the last GET request recorded for path.
func lastQuery(srv *apitest.Server, path string) string {
	r := reqs(srv, "GET", path)
	if len(r) == 0 {
		return ""
	}
	return r[len(r)-1].Query
}

// plainOf is the first 2 runes of the untruncated plain body of message id (an
// ellipsis-free prefix every density draws).
func plainOf(t testing.TB, h *uitest.Harness, id string) string {
	t.Helper()
	m, ok := chatOf(h.Context()).FindMessage(id)
	if !ok {
		t.Fatalf("message %s not held", id)
	}
	r := []rune(markup.Plain(m.Body, 1<<16))
	return string(r[:min(2, len(r))])
}

// rowVisible reports whether row id is drawn on some line of the channel list
// (after a fresh View).
func rowVisible(t testing.TB, h *uitest.Harness, id string) bool {
	t.Helper()
	h.Screen()
	v := channelOf(t, h)
	for y := 0; y < v.pane.size.H; y++ {
		if rid, _, ok := v.pane.lineAt(h.Context(), y); ok && rid == id {
			return true
		}
	}
	return false
}

// threadRoute is the RouteThread placed in some slot of the last frame.
func threadRoute(h *uitest.Harness) (ui.Route, bool) {
	h.Screen()
	for _, r := range h.Model.Frame().Routes {
		if r.Kind == ui.RouteThread {
			return r, true
		}
	}
	return ui.Route{}, false
}
