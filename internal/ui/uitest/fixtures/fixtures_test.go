package fixtures_test

import (
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func at(h, m int) time.Time { return time.Date(2026, 11, 5, h, m, 0, 0, time.UTC) }

func seeded() *store.Store {
	s := store.New()
	fixtures.Seed(s)
	return s
}

func mustValid(t *testing.T, what, id string) {
	t.Helper()
	if !ids.Valid(id) {
		t.Errorf("%s: invalid id %q", what, id)
	}
}

func optValid(t *testing.T, what string, id *string) {
	t.Helper()
	if id != nil {
		mustValid(t, what, *id)
	}
}

// storeIDs checks every ID the Store holds.
func storeIDs(t *testing.T, s *store.Store) {
	t.Helper()
	mustValid(t, "me", s.Me.ID)
	for id, u := range s.Users {
		mustValid(t, "users key", id)
		mustValid(t, "user", u.ID)
	}
	for id := range s.Presence {
		mustValid(t, "presence", id)
	}
	for _, id := range s.SpaceOrder {
		mustValid(t, "space order", id)
	}
	for id, sp := range s.Spaces {
		mustValid(t, "spaces key", id)
		mustValid(t, "space", sp.ID)
		mustValid(t, "space owner", sp.OwnerID)
		mustValid(t, "space default channel", sp.DefaultChannelID)
		for _, ch := range sp.Channels {
			mustValid(t, "space channel", ch.ID)
		}
	}
	for id, ch := range s.Channels {
		mustValid(t, "channels key", id)
		mustValid(t, "channel", ch.ID)
		optValid(t, "channel space", ch.SpaceID)
		optValid(t, "channel last message", ch.LastMessageID)
		optValid(t, "channel last read", ch.My.LastReadMessageID)
		for _, m := range ch.Members {
			mustValid(t, "channel member", m.ID)
		}
	}
	for _, id := range s.DMOrder {
		mustValid(t, "dm order", id)
	}
	for id, f := range s.Friends {
		mustValid(t, "friends key", id)
		mustValid(t, "friend", f.User.ID)
	}
}

func messageIDs(t *testing.T, what, channelID string, ms []api.Message) {
	t.Helper()
	for i, m := range ms {
		mustValid(t, what+" message", m.ID)
		mustValid(t, what+" author", m.Author.ID)
		if m.ChannelID != channelID {
			t.Errorf("%s: message %s channel %q, want %q", what, m.ID, m.ChannelID, channelID)
		}
		if i > 0 && !(ms[i-1].ID < m.ID) {
			t.Errorf("%s: ids not ascending at %d: %s then %s", what, i, ms[i-1].ID, m.ID)
		}
		for _, mn := range m.Mentions {
			mustValid(t, what+" mention", mn.UserID)
		}
		if m.Poll != nil {
			mustValid(t, what+" poll", m.Poll.ID)
		}
	}
}

func TestSeed(t *testing.T) {
	s := seeded()
	if s.Me == nil || s.Me.Handle != "sam" || s.Me.ID != fixtures.UserSam {
		t.Fatalf("Me = %+v", s.Me)
	}
	if sp := s.Spaces[fixtures.SpaceNova]; sp == nil || sp.Name != "노바 개발방" {
		t.Fatalf("Spaces[SpaceNova] = %+v", sp)
	}
	if !slices.Equal(s.SpaceOrder, []string{fixtures.SpaceNova}) {
		t.Errorf("SpaceOrder = %v", s.SpaceOrder)
	}
	var labels []string
	for _, ch := range s.SpaceChannels(fixtures.SpaceNova) {
		labels = append(labels, s.ChannelLabel(ch.ID))
	}
	if want := []string{"#일반", "#잡담", "#개발", "#배포알림"}; !slices.Equal(labels, want) {
		t.Errorf("channel labels = %v, want %v", labels, want)
	}
	if n := len(s.Spaces[fixtures.SpaceNova].Channels); n != 4 {
		t.Errorf("SpaceDetail.Channels = %d, want 4", n)
	}
	if got := s.Channels[fixtures.ChGeneral].My.UnreadCount; got != 3 {
		t.Errorf("#일반 unread = %d, want 3", got)
	}
	for _, id := range []string{fixtures.ChChat, fixtures.ChDev, fixtures.ChDeploy} {
		if got := s.Channels[id].My.UnreadCount; got != 0 {
			t.Errorf("%s unread = %d, want 0", s.ChannelLabel(id), got)
		}
	}
	if want := []string{fixtures.DMMinji, fixtures.DMJunho}; !slices.Equal(s.DMOrder, want) {
		t.Errorf("DMOrder = %v, want %v", s.DMOrder, want)
	}
	if got := s.Presence[fixtures.UserMinji].Status; got != api.PresenceOnline {
		t.Errorf("minji presence = %q", got)
	}
	if got := s.Presence[fixtures.UserJunho].Status; got != api.PresenceOffline {
		t.Errorf("junho presence = %q", got)
	}
	if got := s.ChannelLabel(fixtures.DMMinji); got != "민지" {
		t.Errorf("ChannelLabel(DMMinji) = %q", got)
	}
	if got := s.ChannelLabel(fixtures.DMJunho); got != "준호" {
		t.Errorf("ChannelLabel(DMJunho) = %q", got)
	}
	names := map[string]string{}
	for _, u := range s.Users {
		names[u.Handle] = u.DisplayName
	}
	for h, n := range map[string]string{"sam": "sam", "minji": "민지", "junho": "준호", "hana": "하나", "dev_kim": "김개발"} {
		if names[h] != n {
			t.Errorf("user %s display name = %q, want %q", h, names[h], n)
		}
	}
	if s.NotifUnread != 2 {
		t.Errorf("NotifUnread = %d, want 2", s.NotifUnread)
	}
	storeIDs(t, s)
}

func TestMessagesDev(t *testing.T) {
	ms := fixtures.Messages(fixtures.ChDev)
	if len(ms) != 4 {
		t.Fatalf("Messages(ChDev) = %d, want 4", len(ms))
	}
	messageIDs(t, "#개발", fixtures.ChDev, ms)

	m := ms[0]
	if m.Author.ID != fixtures.UserMinji || !m.CreatedAt.Equal(at(2, 2)) || m.Body != "오늘 배포 몇 시에 해요?" {
		t.Errorf("msg 0 = %s %v %q", m.Author.Handle, m.CreatedAt, m.Body)
	}
	if want := []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}, {Emoji: "👀", Count: 1}}; !slices.Equal(m.Reactions, want) {
		t.Errorf("msg 0 reactions = %+v", m.Reactions)
	}
	if m.Thread == nil || m.Thread.ReplyCount != 3 {
		t.Errorf("msg 0 thread = %+v", m.Thread)
	}

	m = ms[1]
	if m.Author.ID != fixtures.UserJunho || !m.CreatedAt.Equal(at(2, 4)) || !strings.Contains(m.Body, "```\n$ wrangler deploy --env prod\n```") {
		t.Errorf("msg 1 = %s %v %q", m.Author.Handle, m.CreatedAt, m.Body)
	}

	m = ms[2]
	if m.Author.ID != fixtures.UserSam || !m.CreatedAt.Equal(at(2, 5)) || m.Body != "@minji 점심 먹고 ㄱㄱ" {
		t.Errorf("msg 2 = %s %v %q", m.Author.Handle, m.CreatedAt, m.Body)
	}
	if want := []api.MentionRef{{UserID: fixtures.UserMinji, Handle: "minji"}}; !slices.Equal(m.Mentions, want) {
		t.Errorf("msg 2 mentions = %+v", m.Mentions)
	}

	m = ms[3]
	if m.Author.ID != fixtures.UserMinji || m.Kind != "poll" || m.Poll == nil || m.Poll.Question != "점심 메뉴" {
		t.Fatalf("msg 3 = %s %q %+v", m.Author.Handle, m.Kind, m.Poll)
	}
	if want := []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 4}, {Idx: 1, Text: "돈까스", VoteCount: 2}}; !slices.Equal(m.Poll.Options, want) {
		t.Errorf("poll options = %+v", m.Poll.Options)
	}

	// The last message of #개발 is what the Store points at.
	s := seeded()
	if lm := s.Channels[fixtures.ChDev].LastMessageID; lm == nil || *lm != ms[3].ID {
		t.Errorf("#개발 last_message_id = %v, want %s", lm, ms[3].ID)
	}

	// Every seeded channel has valid ascending history; fresh copies each call.
	for id := range s.Channels {
		messageIDs(t, s.ChannelLabel(id), id, fixtures.Messages(id))
	}
	ms[0].Body = "changed"
	if fixtures.Messages(fixtures.ChDev)[0].Body == "changed" {
		t.Error("Messages returns shared state")
	}
	if got := fixtures.Messages("01JB0000000000000000NOSUCH"); len(got) != 0 {
		t.Errorf("unknown channel messages = %d", len(got))
	}
}

func TestTimelineAndNotifications(t *testing.T) {
	tl := fixtures.Timeline()
	if len(tl) != 2 {
		t.Fatalf("Timeline = %d items, want 2", len(tl))
	}
	p := tl[0].Post
	if p.Author.Handle != "hana" || p.Body != "오늘 첫 눈 ❄️ 사무실 창밖" || len(p.Media) != 1 || p.LikeCount != 12 || p.RepostCount != 3 || p.ReplyCount != 4 {
		t.Errorf("item 0 = %s %q media=%d ♥%d ↻%d 💬%d", p.Author.Handle, p.Body, len(p.Media), p.LikeCount, p.RepostCount, p.ReplyCount)
	}
	p = tl[1].Post
	if p.Author.Handle != "dev_kim" || p.Body != "bubbletea v2 써보신 분? #golang" || p.LikeCount != 5 || p.ReplyCount != 2 || !slices.Equal(p.Hashtags, []string{"golang"}) {
		t.Errorf("item 1 = %s %q ♥%d 💬%d %v", p.Author.Handle, p.Body, p.LikeCount, p.ReplyCount, p.Hashtags)
	}
	for i, it := range tl {
		mustValid(t, "timeline item", it.ID)
		mustValid(t, "timeline post", it.Post.ID)
		if it.Type != "post" {
			t.Errorf("item %d type %q", i, it.Type)
		}
		if i > 0 && it.At.After(tl[i-1].At) {
			t.Errorf("timeline not newest first at %d", i)
		}
	}

	ns := fixtures.Notifications()
	if len(ns) != 2 {
		t.Fatalf("Notifications = %d, want 2", len(ns))
	}
	for _, n := range ns {
		mustValid(t, "notification", n.ID)
		mustValid(t, "notification subject", n.Subject.ID)
		if n.Actor == nil || n.Read {
			t.Errorf("notification %s: actor %v read %v", n.ID, n.Actor, n.Read)
		}
	}
	if ns[0].Actor.Handle != "hana" || ns[0].Type != "post_like" || ns[1].Actor.Handle != "minji" || ns[1].Type != "message_mention" {
		t.Errorf("notifications = %s/%s, %s/%s", ns[0].Actor.Handle, ns[0].Type, ns[1].Actor.Handle, ns[1].Type)
	}
}

func hangulCount(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			n++
		}
	}
	return n
}

func TestHostileSet(t *testing.T) {
	s := seeded()
	fixtures.Hostile(s)
	storeIDs(t, s)

	sp := s.Spaces[fixtures.SpaceHostile]
	if sp == nil || !strings.HasPrefix(sp.Name, "☠ 적대적 데이터") || utf8.RuneCountInString(sp.Name) != 50 {
		t.Fatalf("hostile space = %+v", sp)
	}
	if s.Spaces[fixtures.SpaceNova] == nil || !slices.Equal(s.SpaceOrder, []string{fixtures.SpaceNova, fixtures.SpaceHostile}) {
		t.Errorf("SpaceOrder = %v", s.SpaceOrder)
	}
	ch := s.Channels[fixtures.ChHostile]
	if ch == nil || ch.Name == nil || utf8.RuneCountInString(*ch.Name) != 50 || hangulCount(*ch.Name) != 50 {
		t.Fatalf("hostile channel = %+v", ch)
	}
	if ch.My.UnreadCount != 9999 {
		t.Errorf("hostile unread = %d", ch.My.UnreadCount)
	}
	if !slices.ContainsFunc(s.SpaceChannels(fixtures.SpaceHostile), func(c *api.Channel) bool { return c.ID == fixtures.ChHostile }) {
		t.Error("ChHostile not in the hostile space")
	}
	// the seed is untouched
	if got := s.Channels[fixtures.ChGeneral].My.UnreadCount; got != 3 {
		t.Errorf("#일반 unread after Hostile = %d", got)
	}

	var long32, name32, status, ctrlName bool
	var handle32 string
	for _, u := range s.Users {
		if len(u.Handle) == 32 {
			long32, handle32 = true, u.Handle
		}
		if utf8.RuneCountInString(u.DisplayName) == 32 && hangulCount(u.DisplayName) == 16 {
			name32 = true
		}
		if u.Status != nil && u.Status.Emoji != nil && utf8.RuneCountInString(u.Status.Text) >= 60 {
			status = true
		}
		if strings.Contains(u.DisplayName, "\x1b[") {
			ctrlName = true
		}
	}
	if !long32 || !name32 || !status || !ctrlName {
		t.Errorf("hostile users: handle32=%v name32=%v status=%v ctrlName=%v", long32, name32, status, ctrlName)
	}

	ms := fixtures.HostileMessages(fixtures.ChHostile)
	messageIDs(t, "hostile", fixtures.ChHostile, ms)
	var all strings.Builder
	var has4000, has20, has9999, hasHere, hasURL, loneMark bool
	for _, m := range ms {
		all.WriteString(m.Body + "\n")
		if utf8.RuneCountInString(m.Body) == 4000 {
			has4000 = true
		}
		if len(m.Reactions) == 20 {
			has20 = true
		}
		if m.Thread != nil && m.Thread.ReplyCount == 9999 {
			has9999 = true
		}
		if m.MentionHere && slices.ContainsFunc(m.Mentions, func(r api.MentionRef) bool { return r.Handle == handle32 }) && strings.Contains(m.Body, "@"+handle32) {
			hasHere = true
		}
		for _, w := range strings.Fields(m.Body) {
			if strings.HasPrefix(w, "https://") && len(w) == 300 {
				hasURL = true
			}
		}
		if strings.HasPrefix(m.Body, "́") {
			loneMark = true
		}
	}
	if !has4000 || !has20 || !has9999 || !hasHere || !hasURL || !loneMark {
		t.Errorf("hostile messages: 4000=%v reactions20=%v replies9999=%v here=%v url300=%v loneMark=%v", has4000, has20, has9999, hasHere, hasURL, loneMark)
	}
	body := all.String()
	for _, want := range []string{"\t", "\r", "\x07", "\u0085", "\x1b[31m", "\x1b]8;;", "❄️", "⚠️", "👍🏽", "👨‍👩‍👧", "🇰 ", "🇰🇷", "1️⃣", "क्षि", "가"} {
		if !strings.Contains(body, want) {
			t.Errorf("hostile messages lack %q", want)
		}
	}
	// Bidi controls, invisible format characters and line separators reach
	// a message body, a display name, a status and a channel name, so every
	// hostile sweep proves those paths sanitize them (pending-fixes 1).
	if !strings.Contains(body, fixtures.FormatSoup) {
		t.Error("hostile messages lack FormatSoup")
	}
	for _, r := range []rune{0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069,
		0x200e, 0x200f, 0x061c, 0x200b, 0x200c, 0x2060, 0x2061, 0x2062, 0x2063, 0x2064, 0xfeff, 0x00ad,
		0xe0041, 0x2028, 0x2029} {
		if !strings.ContainsRune(fixtures.FormatSoup, r) {
			t.Errorf("FormatSoup lacks %U", r)
		}
	}
	var spoofName, spoofStatus bool
	for _, u := range s.Users {
		if strings.ContainsRune(u.DisplayName, 0x202e) && strings.ContainsRune(u.DisplayName, 0x2028) {
			spoofName = true
		}
		if u.Status != nil && strings.Contains(u.Status.Text, fixtures.FormatSoup) {
			spoofStatus = true
		}
	}
	spoofChannel := false
	for _, c := range s.SpaceChannels(fixtures.SpaceHostile) {
		if c.Name != nil && strings.ContainsRune(*c.Name, 0x202e) && strings.ContainsRune(*c.Name, 0x2028) {
			spoofChannel = true
		}
	}
	if !spoofName || !spoofStatus || !spoofChannel {
		t.Errorf("hostile format runes: name=%v status=%v channel=%v", spoofName, spoofStatus, spoofChannel)
	}
	if got := fixtures.Messages(fixtures.ChHostile); len(got) != len(ms) {
		t.Errorf("Messages(ChHostile) = %d, want the hostile set (%d)", len(got), len(ms))
	}

	tl := fixtures.HostileTimeline()
	if len(tl) == 0 {
		t.Fatal("HostileTimeline empty")
	}
	var has500 bool
	for _, it := range tl {
		mustValid(t, "hostile item", it.ID)
		mustValid(t, "hostile post", it.Post.ID)
		if utf8.RuneCountInString(it.Post.Body) == 500 {
			has500 = true
		}
	}
	if !has500 {
		t.Error("no 500-rune hostile post")
	}
}

func bootstrapped(t *testing.T, srv *apitest.Server) *store.Store {
	t.Helper()
	msg, ok := store.Bootstrap(srv.Client())().(store.BootstrapMsg)
	if !ok {
		t.Fatal("Bootstrap did not yield BootstrapMsg")
	}
	if msg.Err != nil {
		t.Fatalf("bootstrap error: %v", msg.Err)
	}
	s := store.New()
	s.ApplyBootstrap(msg)
	return s
}

func TestRegisterAPIServesBootstrap(t *testing.T) {
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	got := bootstrapped(t, srv)

	if got.Me == nil || got.Me.ID != fixtures.UserSam {
		t.Fatalf("Me = %+v", got.Me)
	}
	if got.Spaces[fixtures.SpaceNova] == nil || got.Spaces[fixtures.SpaceHostile] == nil {
		t.Fatalf("spaces = %v", got.SpaceOrder)
	}
	want := seeded()
	for id, ch := range want.Channels {
		g := got.Channels[id]
		if g == nil {
			t.Errorf("channel %s missing after bootstrap", want.ChannelLabel(id))
			continue
		}
		if g.My.UnreadCount != ch.My.UnreadCount || g.My.MentionCount != ch.My.MentionCount {
			t.Errorf("%s unread %d/%d, want %d/%d", want.ChannelLabel(id), g.My.UnreadCount, g.My.MentionCount, ch.My.UnreadCount, ch.My.MentionCount)
		}
	}
	if got.Channels[fixtures.ChHostile] == nil || got.Channels[fixtures.ChHostile].My.UnreadCount != 9999 {
		t.Errorf("hostile channel after bootstrap = %+v", got.Channels[fixtures.ChHostile])
	}
	if got.NotifUnread != want.NotifUnread {
		t.Errorf("NotifUnread = %d, want %d", got.NotifUnread, want.NotifUnread)
	}
	if len(got.DMOrder) < 2 || got.DMOrder[0] != fixtures.DMMinji || got.DMOrder[1] != fixtures.DMJunho {
		t.Errorf("DMOrder = %v", got.DMOrder)
	}
	for id := range want.Friends {
		if _, ok := got.Friends[id]; !ok {
			t.Errorf("friend %s missing", id)
		}
	}
	if got.ChannelLabel(fixtures.DMMinji) != "민지" {
		t.Errorf("DM label = %q", got.ChannelLabel(fixtures.DMMinji))
	}
}

func get(t *testing.T, srv *apitest.Server, method, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestRegisterAPIPatterns(t *testing.T) {
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	samples := []string{
		"/v1/me",
		"/v1/spaces",
		"/v1/spaces/" + fixtures.SpaceNova,
		"/v1/dms?cursor=abc&limit=100",
		"/v1/me/unreads",
		"/v1/notifications/unread-count",
		"/v1/friends?limit=100",
		"/v1/friends/requests?direction=incoming",
		"/v1/me/direct-invites",
		"/v1/blocks",
		"/v1/channels/" + fixtures.ChDev + "/messages?around=x&limit=50",
		"/v1/messages/" + devRoot + "/thread",
		"/v1/timeline/following",
		"/v1/timeline/discover",
		"/v1/notifications",
		"/v1/users/search?q=min",
		"/v1/meta",
	}
	if len(samples) != 17 {
		t.Fatalf("sample table has %d entries, want the 17 RegisterAPI patterns", len(samples))
	}
	for _, p := range samples {
		if st, b := get(t, srv, "GET", p); st != http.StatusOK || !json.Valid(b) {
			t.Errorf("GET %s = %d %s", p, st, b)
		}
	}

	// content spot checks
	_, b := get(t, srv, "GET", "/v1/channels/"+fixtures.ChDev+"/messages?before=zzz")
	var win api.MessageWindow
	if err := json.Unmarshal(b, &win); err != nil || len(win.Items) != 4 {
		t.Errorf("messages window = %d items (%v)", len(win.Items), err)
	}
	_, b = get(t, srv, "GET", "/v1/messages/"+devRoot+"/thread")
	var th api.ThreadWindow
	if err := json.Unmarshal(b, &th); err != nil || th.Root.ID != devRoot || len(th.Items) != 3 {
		t.Errorf("thread = root %s, %d replies (%v)", th.Root.ID, len(th.Items), err)
	}
	other := fixtures.Messages(fixtures.ChDev)[1].ID
	_, b = get(t, srv, "GET", "/v1/messages/"+other+"/thread")
	th = api.ThreadWindow{}
	if err := json.Unmarshal(b, &th); err != nil || th.Root.ID != other || len(th.Items) != 0 {
		t.Errorf("other thread = root %s, %d replies (%v)", th.Root.ID, len(th.Items), err)
	}
	c := srv.Client()
	us, err := c.SearchUsers(t.Context(), "min", 0)
	if err != nil || len(us) != 1 || us[0].Handle != "minji" {
		t.Errorf("search min = %+v (%v)", us, err)
	}
	us, err = c.SearchUsers(t.Context(), "", 0)
	if err != nil || len(us) != 0 {
		t.Errorf("search empty = %d (%v)", len(us), err)
	}
	us, err = c.SearchUsers(t.Context(), "a", 2) // sam, hana, dev_kim, ... match "a"
	if err != nil || len(us) != 2 {
		t.Errorf("search a limit 2 = %d (%v)", len(us), err)
	}
	us, err = c.SearchUsers(t.Context(), "민", 0) // display name match
	if err != nil || len(us) != 1 || us[0].ID != fixtures.UserMinji {
		t.Errorf("search 민 = %+v (%v)", us, err)
	}
	if st, _ := get(t, srv, "GET", "/v1/spaces/01JB0000000000000000NOSUCH"); st != http.StatusNotFound {
		t.Errorf("unknown space = %d, want 404", st)
	}

	// unlisted routes are not served
	for _, mp := range [][2]string{
		{"GET", "/v1/presence?ids=" + fixtures.UserMinji},
		{"GET", "/v1/channels/" + fixtures.ChDev + "/members"},
		{"POST", "/v1/channels/" + fixtures.ChDev + "/messages"},
		{"GET", "/v1/posts/" + fixtures.Timeline()[0].Post.ID},
		{"GET", "/"},
	} {
		st, b := get(t, srv, mp[0], mp[1])
		var env struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.Unmarshal(b, &env)
		if st != http.StatusNotFound || env.Error.Code != "not_found" {
			t.Errorf("%s %s = %d %s, want 404 not_found", mp[0], mp[1], st, b)
		}
	}

	// a later registration of the same shape replaces the fixture handler
	srv.JSON("GET /v1/friends/requests", http.StatusOK, map[string]any{"items": []any{}, "marker": "override"})
	if _, b := get(t, srv, "GET", "/v1/friends/requests?direction=incoming"); !strings.Contains(string(b), "override") {
		t.Errorf("override not used: %s", b)
	}
}

func near(c color.Color, r, g, b uint8) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(a uint32, x uint8) bool {
		v := int(a>>8) - int(x)
		return v >= -24 && v <= 24
	}
	return d(cr, r) && d(cg, g) && d(cb, b)
}

func TestTestImage(t *testing.T) {
	img := fixtures.TestImage()
	if img == nil {
		t.Fatal("nil image")
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 48 || b.Min.X != 0 || b.Min.Y != 0 {
		t.Fatalf("bounds = %v, want 64x48", b)
	}
	if c := img.At(0, 0); !near(c, 0xfb, 0x49, 0x34) {
		t.Errorf("top-left = %v, want ~#fb4934", c)
	}
	if c := img.At(63, 47); !near(c, 0x45, 0x85, 0x88) {
		t.Errorf("bottom-right = %v, want ~#458588", c)
	}
	if fixtures.TestImage().At(10, 10) != img.At(10, 10) {
		t.Error("TestImage not deterministic")
	}
}
