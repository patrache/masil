package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

var testTS = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

var testSeq int64

func ev(t *testing.T, typ string, data any) realtime.Event {
	t.Helper()
	testSeq++
	seq := testSeq
	return realtime.Event{Type: typ, Seq: &seq, TS: testTS, Data: mustJSON(t, data)}
}

func sp(s string) *string { return &s }

var (
	uMe    = api.UserSummary{ID: "U0ME", Handle: "sam", DisplayName: "샘"}
	uMinji = api.UserSummary{ID: "U1MJ", Handle: "minji", DisplayName: "민지"}
	uJunho = api.UserSummary{ID: "U2JH", Handle: "junho", DisplayName: "준호"}
	uHana  = api.UserSummary{ID: "U3HN", Handle: "hana", DisplayName: "하나"}
)

func textChannel(id, spaceID, name string, pos int) api.Channel {
	return api.Channel{ID: id, Kind: api.ChannelText, SpaceID: sp(spaceID), Name: sp(name), Position: pos,
		My: api.ChannelMyState{Notify: "all"}, CreatedAt: testTS}
}

func dmChannel(id string, members ...api.UserSummary) api.Channel {
	return api.Channel{ID: id, Kind: api.ChannelDM, Members: members, MemberCount: len(members),
		My: api.ChannelMyState{Notify: "all"}, CreatedAt: testTS}
}

func groupChannel(id string, name *string, members ...api.UserSummary) api.Channel {
	return api.Channel{ID: id, Kind: api.ChannelGroupDM, Name: name, Members: members, MemberCount: len(members),
		My: api.ChannelMyState{Notify: "all"}, CreatedAt: testTS}
}

// meStore is a Store with me set and a few channels.
func meStore(t *testing.T) *Store {
	t.Helper()
	s := New()
	s.Me = &api.Me{UserSummary: uMe}
	s.ObserveUser(uMe)
	for _, ch := range []api.Channel{
		textChannel("C1DEV", "S1", "개발", 1),
		textChannel("C0GEN", "S1", "일반", 0),
		dmChannel("D1MJ", uMe, uMinji),
		dmChannel("D2JH", uMe, uJunho),
	} {
		s.ObserveChannel(ch)
	}
	return s
}

func msg(id, ch string, author api.UserSummary) api.Message {
	return api.Message{ID: id, ChannelID: ch, Author: author, Kind: "text", Body: "hi", CreatedAt: testTS}
}

func TestChannelUpdatedKeepsPerViewerFields(t *testing.T) {
	s := meStore(t)
	c := s.Channels["C1DEV"]
	c.My.UnreadCount = 3
	c.My.MentionCount = 1
	c.My.Muted = true
	c.My.LastReadMessageID = sp("M0")
	c.ReadOnly = true
	c.ReadOnlyReason = sp("blocked")

	upd := textChannel("C1DEV", "S1", "개발팀", 1)
	upd.Topic = "백엔드"
	s.Apply(ev(t, "channel.updated", map[string]any{"channel": upd}))

	got := s.Channels["C1DEV"]
	if got.My.UnreadCount != 3 || got.My.MentionCount != 1 || !got.My.Muted || got.My.LastReadMessageID == nil {
		t.Errorf("My overwritten: %+v", got.My)
	}
	if !got.ReadOnly || got.ReadOnlyReason == nil || *got.ReadOnlyReason != "blocked" {
		t.Errorf("ReadOnly overwritten: %v %v", got.ReadOnly, got.ReadOnlyReason)
	}
	if got.Name == nil || *got.Name != "개발팀" || got.Topic != "백엔드" {
		t.Errorf("Name/Topic not updated: %v %q", got.Name, got.Topic)
	}

	// channel.created for a new channel takes the payload as-is.
	s.Apply(ev(t, "channel.created", map[string]any{"channel": textChannel("C2NEW", "S1", "새채널", 2)}))
	if s.Channels["C2NEW"] == nil {
		t.Fatal("channel.created not stored")
	}
}

func TestMessageCreatedUnreadRules(t *testing.T) {
	s := meStore(t)

	// other author, top-level, text, not viewing => +1, last_message_* updated.
	m := msg("M100", "C1DEV", uMinji)
	m.CreatedAt = testTS.Add(time.Minute)
	s.Apply(ev(t, "message.created", map[string]any{"message": m}))
	c := s.Channels["C1DEV"]
	if c.My.UnreadCount != 1 {
		t.Errorf("unread = %d, want 1", c.My.UnreadCount)
	}
	if c.LastMessageID == nil || *c.LastMessageID != "M100" || c.LastMessageAt == nil || !c.LastMessageAt.Equal(m.CreatedAt) {
		t.Errorf("last message = %v %v", c.LastMessageID, c.LastMessageAt)
	}
	if s.User(uMinji.ID) == nil {
		t.Error("author not observed")
	}

	// own message => no change, last message updated.
	s.Apply(ev(t, "message.created", map[string]any{"message": msg("M101", "C1DEV", uMe)}))
	if c.My.UnreadCount != 1 || *c.LastMessageID != "M101" {
		t.Errorf("own: unread %d last %v", c.My.UnreadCount, *c.LastMessageID)
	}

	// thread reply => no change, last message NOT updated.
	r := msg("M102", "C1DEV", uMinji)
	r.ThreadRootID = sp("M100")
	s.Apply(ev(t, "message.created", map[string]any{"message": r}))
	if c.My.UnreadCount != 1 || *c.LastMessageID != "M101" {
		t.Errorf("reply: unread %d last %v", c.My.UnreadCount, *c.LastMessageID)
	}

	// system => no unread change, last message updated.
	sys := msg("M103", "C1DEV", uMinji)
	sys.Kind = "system"
	s.Apply(ev(t, "message.created", map[string]any{"message": sys}))
	if c.My.UnreadCount != 1 || *c.LastMessageID != "M103" {
		t.Errorf("system: unread %d last %v", c.My.UnreadCount, *c.LastMessageID)
	}

	// viewing => no unread change.
	s.SetViewing("C1DEV")
	if !s.Viewing("C1DEV") || s.Viewing("C0GEN") {
		t.Fatal("Viewing set wrong")
	}
	s.Apply(ev(t, "message.created", map[string]any{"message": msg("M104", "C1DEV", uMinji)}))
	if c.My.UnreadCount != 1 || *c.LastMessageID != "M104" {
		t.Errorf("viewing: unread %d last %v", c.My.UnreadCount, *c.LastMessageID)
	}
	s.SetViewing()
	if s.Viewing("C1DEV") {
		t.Fatal("SetViewing() must clear")
	}

	// DM channel => moved to DMOrder[0].
	s.DMOrder = []string{"D1MJ", "D2JH"}
	s.Apply(ev(t, "message.created", map[string]any{"message": msg("M105", "D2JH", uJunho)}))
	if s.DMOrder[0] != "D2JH" || len(s.DMOrder) != 2 {
		t.Errorf("DMOrder = %v", s.DMOrder)
	}
	if s.Channels["D2JH"].My.UnreadCount != 1 {
		t.Errorf("dm unread = %d", s.Channels["D2JH"].My.UnreadCount)
	}

	// mention of me => mention +1; same message id twice => once.
	mm := msg("M106", "C0GEN", uMinji)
	mm.Mentions = []api.MentionRef{{UserID: uMe.ID, Handle: "sam"}}
	s.Apply(ev(t, "message.created", map[string]any{"message": mm}))
	s.Apply(ev(t, "message.created", map[string]any{"message": mm}))
	g := s.Channels["C0GEN"]
	if g.My.MentionCount != 1 {
		t.Errorf("mention = %d, want 1", g.My.MentionCount)
	}
	if g.My.UnreadCount != 1 {
		t.Errorf("duplicate message.created counted twice: unread %d", g.My.UnreadCount)
	}

	// thread reply mentioning me => no mention count (top-level only).
	tm := msg("M107", "C0GEN", uMinji)
	tm.ThreadRootID = sp("M106")
	tm.Mentions = []api.MentionRef{{UserID: uMe.ID, Handle: "sam"}}
	s.Apply(ev(t, "message.created", map[string]any{"message": tm}))
	if g.My.MentionCount != 1 {
		t.Errorf("thread mention counted: %d", g.My.MentionCount)
	}

	// unknown channel => no panic.
	s.Apply(ev(t, "message.created", map[string]any{"message": msg("M108", "CXXX", uMinji)}))
}

func TestHereMentionCountedViaNotification(t *testing.T) {
	s := meStore(t)
	m := msg("M200", "C1DEV", uMinji)
	m.MentionHere = true
	s.Apply(ev(t, "message.created", map[string]any{"message": m}))
	c := s.Channels["C1DEV"]
	if c.My.MentionCount != 0 {
		t.Fatalf("@here guessed: mention %d", c.My.MentionCount)
	}
	n := api.Notification{ID: "N1", Type: "message_mention", Actor: &uMinji,
		Subject: api.NotificationSubject{Type: "message", ID: "M200"},
		Context: api.NotificationContext{ChannelID: sp("C1DEV"), MessageID: sp("M200")}, CreatedAt: testTS}
	s.Apply(ev(t, "notification.created", map[string]any{"notification": n, "unread_count": 4}))
	if c.My.MentionCount != 1 {
		t.Errorf("mention after notification = %d, want 1", c.My.MentionCount)
	}
	if s.NotifUnread != 4 {
		t.Errorf("NotifUnread = %d", s.NotifUnread)
	}
	s.Apply(ev(t, "notification.created", map[string]any{"notification": n, "unread_count": 4}))
	if c.My.MentionCount != 1 {
		t.Errorf("mention after repeated notification = %d, want 1", c.My.MentionCount)
	}

	// direct mention counted by message.created, then its notification => no double count.
	dm := msg("M201", "C1DEV", uMinji)
	dm.Mentions = []api.MentionRef{{UserID: uMe.ID}}
	s.Apply(ev(t, "message.created", map[string]any{"message": dm}))
	n2 := n
	n2.ID, n2.Context.MessageID = "N2", sp("M201")
	s.Apply(ev(t, "notification.created", map[string]any{"notification": n2, "unread_count": 5}))
	if c.My.MentionCount != 2 {
		t.Errorf("mention = %d, want 2", c.My.MentionCount)
	}

	// thread reply notification => not counted.
	n3 := n
	n3.ID, n3.Context.MessageID, n3.Context.ThreadRootID = "N3", sp("M202"), sp("M200")
	s.Apply(ev(t, "notification.created", map[string]any{"notification": n3, "unread_count": 6}))
	if c.My.MentionCount != 2 {
		t.Errorf("thread notification counted: %d", c.My.MentionCount)
	}
}

func TestReadStateAndPrefs(t *testing.T) {
	s := meStore(t)
	c := s.Channels["C1DEV"]
	c.My.UnreadCount = 7
	c.My.MentionCount = 2
	st := api.UnreadState{ChannelID: "C1DEV", UnreadCount: 1, MentionCount: 0, LastMessageID: sp("M300"), LastReadMessageID: sp("M299")}
	s.Apply(ev(t, "read_state.updated", map[string]any{"state": st}))
	if c.My.UnreadCount != 1 || c.My.MentionCount != 0 || c.My.LastReadMessageID == nil || *c.My.LastReadMessageID != "M299" {
		t.Errorf("read state: %+v", c.My)
	}
	if c.LastMessageID == nil || *c.LastMessageID != "M300" {
		t.Errorf("last message = %v", c.LastMessageID)
	}

	my := api.ChannelMyState{Notify: "mentions", Muted: true}
	s.Apply(ev(t, "channel.prefs_updated", map[string]any{"channel_id": "C1DEV", "my": my}))
	if c.My.Notify != "mentions" || !c.My.Muted {
		t.Errorf("prefs: %+v", c.My)
	}
	if c.My.UnreadCount != 1 {
		t.Errorf("prefs clobbered counts: %+v", c.My)
	}
}

func TestSpaceEvents(t *testing.T) {
	s := meStore(t)
	sd := api.SpaceDetail{
		Space: api.Space{SpacePreview: api.SpacePreview{ID: "S2", Name: "노바", MemberCount: 3}, MyRole: api.RoleAdmin},
		Channels: []api.Channel{
			textChannel("C20", "S2", "공지", 0),
			textChannel("C21", "S2", "잡담", 1),
		},
	}
	s.SpaceOrder = []string{"S1"}
	s.Spaces["S1"] = &api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: "S1", Name: "기본"}}}
	s.Apply(ev(t, "space.joined", map[string]any{"space": sd}))
	if s.Spaces["S2"] == nil || s.Spaces["S2"].MyRole != api.RoleAdmin {
		t.Fatalf("space.joined: %+v", s.Spaces["S2"])
	}
	if len(s.SpaceOrder) != 2 || s.SpaceOrder[1] != "S2" {
		t.Errorf("SpaceOrder = %v", s.SpaceOrder)
	}
	if s.Channels["C20"] == nil || s.Channels["C21"] == nil {
		t.Error("space channels not stored")
	}
	s.Apply(ev(t, "space.joined", map[string]any{"space": sd}))
	if len(s.SpaceOrder) != 2 {
		t.Errorf("space.joined twice duplicated order: %v", s.SpaceOrder)
	}

	upd := api.Space{SpacePreview: api.SpacePreview{ID: "S2", Name: "노바 개발방", MemberCount: 3}, MyRole: api.RoleMember}
	s.Apply(ev(t, "space.updated", map[string]any{"space": upd}))
	if s.Spaces["S2"].Name != "노바 개발방" || s.Spaces["S2"].MyRole != api.RoleAdmin {
		t.Errorf("space.updated: name %q role %q", s.Spaces["S2"].Name, s.Spaces["S2"].MyRole)
	}
	if len(s.Spaces["S2"].Channels) != 2 {
		t.Errorf("space.updated dropped channels")
	}

	s.Apply(ev(t, "space.member_updated", map[string]any{"space_id": "S2", "member": api.SpaceMember{User: uMe, Role: api.RoleOwner}}))
	if s.Spaces["S2"].MyRole != api.RoleOwner {
		t.Errorf("member_updated for me: %q", s.Spaces["S2"].MyRole)
	}
	s.Apply(ev(t, "space.member_updated", map[string]any{"space_id": "S2", "member": api.SpaceMember{User: uMinji, Role: api.RoleAdmin}}))
	if s.Spaces["S2"].MyRole != api.RoleOwner {
		t.Errorf("member_updated for other changed my role: %q", s.Spaces["S2"].MyRole)
	}

	s.Apply(ev(t, "channel.deleted", map[string]any{"channel_id": "C21", "space_id": "S2", "reason": "deleted"}))
	if s.Channels["C21"] != nil {
		t.Error("channel.deleted kept channel")
	}
	if len(s.SpaceChannels("S2")) != 1 {
		t.Errorf("SpaceChannels after delete = %d", len(s.SpaceChannels("S2")))
	}

	s.Apply(ev(t, "space.removed", map[string]any{"space_id": "S2", "reason": "kicked"}))
	if s.Spaces["S2"] != nil || s.Channels["C20"] != nil {
		t.Error("space.removed kept space or channels")
	}
	if len(s.SpaceOrder) != 1 || s.SpaceOrder[0] != "S1" {
		t.Errorf("SpaceOrder = %v", s.SpaceOrder)
	}

	s.Apply(ev(t, "space.deleted", map[string]any{"space_id": "S1"}))
	if s.Spaces["S1"] != nil || s.Channels["C1DEV"] != nil || s.Channels["C0GEN"] != nil || len(s.SpaceOrder) != 0 {
		t.Error("space.deleted kept space or channels")
	}
	if s.Channels["D1MJ"] == nil {
		t.Error("space.deleted removed a DM")
	}
}

func TestSocialEvents(t *testing.T) {
	s := meStore(t)
	req := api.FriendRequest{ID: "FR1", From: uMinji, To: uMe, Status: "pending", CreatedAt: testTS}
	s.Apply(ev(t, "friend.request_received", map[string]any{"request": req}))
	s.Apply(ev(t, "friend.request_received", map[string]any{"request": req}))
	if len(s.FriendReqIn) != 1 || s.FriendReqIn[0].ID != "FR1" {
		t.Fatalf("FriendReqIn = %+v", s.FriendReqIn)
	}
	s.Apply(ev(t, "friend.request_removed", map[string]any{"request_id": "FR1", "reason": "canceled"}))
	if len(s.FriendReqIn) != 0 {
		t.Errorf("request_removed: %+v", s.FriendReqIn)
	}

	s.Apply(ev(t, "friend.added", map[string]any{"friend": api.Friend{User: uJunho, Since: testTS, Presence: api.PresenceOnline}}))
	if _, ok := s.Friends[uJunho.ID]; !ok {
		t.Error("friend.added missing")
	}
	s.Apply(ev(t, "friend.removed", map[string]any{"user_id": uJunho.ID}))
	if _, ok := s.Friends[uJunho.ID]; ok {
		t.Error("friend.removed kept")
	}

	s.Apply(ev(t, "block.updated", map[string]any{"user_id": uHana.ID, "blocked": true}))
	if !s.IsBlocked(uHana.ID) || s.IsBlocked(uMinji.ID) {
		t.Error("block.updated true")
	}
	s.Apply(ev(t, "block.updated", map[string]any{"user_id": uHana.ID, "blocked": false}))
	if s.IsBlocked(uHana.ID) {
		t.Error("block.updated false")
	}

	inv := api.DirectInvite{ID: "DI1", Space: api.SpacePreview{ID: "S9", Name: "점심"}, Inviter: uMinji, Invitee: uMe, Status: "pending", CreatedAt: testTS}
	s.Apply(ev(t, "space.invite_received", map[string]any{"invite": inv}))
	s.Apply(ev(t, "space.invite_received", map[string]any{"invite": inv}))
	if len(s.DirectInvites) != 1 {
		t.Fatalf("DirectInvites = %+v", s.DirectInvites)
	}
	s.Apply(ev(t, "space.invite_removed", map[string]any{"invite_id": "DI1", "reason": "declined"}))
	if len(s.DirectInvites) != 0 {
		t.Errorf("invite_removed: %+v", s.DirectInvites)
	}
}

func TestUserAndSettings(t *testing.T) {
	s := meStore(t)
	me2 := uMe
	me2.DisplayName = "샘샘"
	s.Apply(ev(t, "user.updated", map[string]any{"user": me2}))
	if s.Me.DisplayName != "샘샘" || s.User(uMe.ID).DisplayName != "샘샘" {
		t.Errorf("user.updated me: %q %q", s.Me.DisplayName, s.User(uMe.ID).DisplayName)
	}
	mj := uMinji
	mj.DisplayName = "민지씨"
	s.Apply(ev(t, "user.updated", map[string]any{"user": mj}))
	if s.User(uMinji.ID).DisplayName != "민지씨" || s.Me.DisplayName != "샘샘" {
		t.Error("user.updated other")
	}

	st := api.Settings{DMPolicy: "friends_only", FocusDefaultMin: 25, Notify: api.NotifySettings{DM: true}}
	s.Apply(ev(t, "settings.updated", map[string]any{"settings": st}))
	if s.Settings != st || s.Me.Settings != st {
		t.Errorf("settings.updated: %+v / %+v", s.Settings, s.Me.Settings)
	}
}

func TestPresenceFocusNotifAlert(t *testing.T) {
	s := meStore(t)
	seen := testTS.Add(-time.Hour)
	s.Apply(ev(t, "presence.snapshot", map[string]any{"items": []api.PresenceItem{
		{UserID: uMinji.ID, Status: api.PresenceOnline},
		{UserID: uJunho.ID, Status: api.PresenceOffline, LastSeenAt: &seen},
	}}))
	if s.Presence[uMinji.ID].Status != api.PresenceOnline || s.Presence[uJunho.ID].LastSeenAt == nil {
		t.Errorf("snapshot: %+v", s.Presence)
	}
	s.Apply(ev(t, "presence.updated", map[string]any{"user_id": uMinji.ID, "status": "away", "last_seen_at": nil}))
	if s.Presence[uMinji.ID].Status != api.PresenceAway || !s.Presence[uMinji.ID].FetchedAt.Equal(testTS) {
		t.Errorf("updated: %+v", s.Presence[uMinji.ID])
	}
	s.Apply(ev(t, "presence.self", map[string]any{"presence": api.PresenceState{Status: api.PresenceDND, Mode: "dnd"}}))
	if s.Self.Status != api.PresenceDND || s.Self.Mode != "dnd" {
		t.Errorf("self: %+v", s.Self)
	}

	f := api.Focus{StartedAt: testTS, EndsAt: testTS.Add(25 * time.Minute)}
	s.Apply(ev(t, "focus.started", map[string]any{"focus": f}))
	if s.Focus == nil || !s.Focus.EndsAt.Equal(f.EndsAt) {
		t.Fatalf("focus.started: %+v", s.Focus)
	}
	sum := api.FocusSummary{StartedAt: testTS, EndedAt: testTS.Add(25 * time.Minute), Counts: api.FocusCounts{Mention: 3, DM: 2}}
	s.Apply(ev(t, "focus.ended", map[string]any{"summary": sum}))
	if s.Focus != nil || s.LastFocusSummary == nil || s.LastFocusSummary.Counts.Mention != 3 {
		t.Errorf("focus.ended: %+v %+v", s.Focus, s.LastFocusSummary)
	}

	n := api.Notification{ID: "N9", Type: "post_like", Actor: &uHana, CreatedAt: testTS}
	s.Apply(ev(t, "notification.created", map[string]any{"notification": n, "unread_count": 9}))
	if s.NotifUnread != 9 {
		t.Errorf("NotifUnread = %d", s.NotifUnread)
	}
	if s.User(uHana.ID) == nil {
		t.Error("notification actor not observed")
	}
	s.Apply(ev(t, "notifications.read", map[string]any{"ids": nil, "all": true, "before": nil, "unread_count": 0}))
	if s.NotifUnread != 0 {
		t.Errorf("NotifUnread after read = %d", s.NotifUnread)
	}

	a1 := api.Alert{ID: "A1", Kind: api.AlertMention, Title: "t1", CreatedAt: testTS}
	a2 := api.Alert{ID: "A2", Kind: api.AlertDM, Title: "t2", CreatedAt: testTS}
	s.Apply(ev(t, "alert", map[string]any{"alert": a1}))
	s.Apply(ev(t, "alert", map[string]any{"alert": a2}))
	items := s.Alerts.Items()
	if len(items) != 2 || items[0].ID != "A2" {
		t.Errorf("alerts = %+v", items)
	}
}

type recReducer struct {
	s       *Store
	seen    []string
	unread  []int // C1DEV unread at the time the reducer saw the event
	resets  int
	resyncs int
}

func (r *recReducer) Name() string { return "rec" }
func (r *recReducer) Apply(s *Store, e realtime.Event) {
	r.seen = append(r.seen, e.Type)
	if c := s.Channels["C1DEV"]; c != nil {
		r.unread = append(r.unread, c.My.UnreadCount)
	}
}
func (r *recReducer) Reset() { r.resets++ }

func TestReducersRunAfterCore(t *testing.T) {
	withFactories(t)
	var rr *recReducer
	RegisterReducer(func(s *Store) Reducer { rr = &recReducer{s: s}; return rr })
	s := meStore(t)
	s.Apply(ev(t, "message.created", map[string]any{"message": msg("M400", "C1DEV", uMinji)}))
	s.Apply(ev(t, "post.deleted", map[string]any{"post_id": "P1"})) // not core: still passed on
	s.Apply(realtime.Event{Type: "some.future_event"})
	if len(rr.seen) != 3 || rr.seen[0] != "message.created" || rr.seen[2] != "some.future_event" {
		t.Fatalf("reducer saw %v", rr.seen)
	}
	if rr.unread[0] != 1 {
		t.Errorf("reducer ran before core: unread %d", rr.unread[0])
	}

	s.Spaces["S1"] = &api.SpaceDetail{}
	s.SpaceOrder = []string{"S1"}
	s.Friends[uMinji.ID] = api.Friend{User: uMinji}
	s.Blocks[uHana.ID] = testTS
	s.NotifUnread = 3
	s.Alerts.Push(api.Alert{ID: "A"})
	s.Focus = &api.Focus{}
	s.SetViewing("C1DEV")
	s.Reset()
	if rr.resets != 1 {
		t.Errorf("reducer Reset calls = %d", rr.resets)
	}
	if s.Me != nil || len(s.Channels) != 0 || len(s.Spaces) != 0 || len(s.SpaceOrder) != 0 || len(s.DMOrder) != 0 ||
		len(s.Friends) != 0 || len(s.Blocks) != 0 || len(s.Users) != 0 || s.NotifUnread != 0 || s.Focus != nil ||
		len(s.Alerts.Items()) != 0 || s.Viewing("C1DEV") || s.Sync.Bootstrapped {
		t.Errorf("Reset left state: %+v", s)
	}
	if s.Channels == nil || s.Users == nil || s.Presence == nil || s.Friends == nil || s.Blocks == nil || s.Spaces == nil {
		t.Error("Reset must leave usable (non-nil) maps")
	}
	if s.Reducer("rec") != rr {
		t.Error("Reset must keep reducer instances")
	}
	// After reset, a previously counted message id counts again.
	s.Me = &api.Me{UserSummary: uMe}
	s.ObserveChannel(textChannel("C1DEV", "S1", "개발", 0))
	m := msg("M500", "C1DEV", uMinji)
	m.Mentions = []api.MentionRef{{UserID: uMe.ID}}
	s.Apply(ev(t, "message.created", map[string]any{"message": m}))
	s.Reset()
	s.Me = &api.Me{UserSummary: uMe}
	s.ObserveChannel(textChannel("C1DEV", "S1", "개발", 0))
	s.Apply(ev(t, "message.created", map[string]any{"message": m}))
	if s.Channels["C1DEV"].My.MentionCount != 1 {
		t.Errorf("dedupe set survived Reset: mention %d", s.Channels["C1DEV"].My.MentionCount)
	}
}

func TestChannelLabel(t *testing.T) {
	s := meStore(t)
	s.ObserveChannel(groupChannel("G1", sp("점심팟"), uMe, uMinji, uJunho))
	s.ObserveChannel(groupChannel("G2", nil, uMinji, uMe, uJunho, uHana))
	cases := map[string]string{
		"C1DEV": "#개발",
		"D1MJ":  "민지",
		"G1":    "점심팟",
		"G2":    "민지, 준호, 하나",
		"NOPE":  "",
	}
	for id, want := range cases {
		if got := s.ChannelLabel(id); got != want {
			t.Errorf("ChannelLabel(%s) = %q, want %q", id, got, want)
		}
	}
	// a renamed user shows up in DM labels via the user cache.
	mj := uMinji
	mj.DisplayName = "민지씨"
	s.ObserveUser(mj)
	if got := s.ChannelLabel("D1MJ"); got != "민지씨" {
		t.Errorf("after rename = %q", got)
	}
}

// A display name the screen shows as nothing (only Hangul fillers, zero-width,
// bidi runes, noncharacters or U+2800 BRAILLE PATTERN BLANK; Sanitize drops them all) falls back to the handle, so a DM
// label never looks nameless (pending-fixes 1).
func TestChannelLabelInvisibleNameFallsBack(t *testing.T) {
	s := meStore(t)
	for _, name := range []string{"\u3164", "\u3164\uffa0\u115f\u1160", "\u200b\u202e", "\u034f\U000e0100",
		"\ufffe", "\uffff\ufdd0", "\u2800", "\u2800 \u2800", "\U0001d159"} {
		mj := uMinji
		mj.DisplayName = name
		s.ObserveUser(mj)
		if got := s.ChannelLabel("D1MJ"); got != uMinji.Handle {
			t.Errorf("DisplayName %q: ChannelLabel = %q, want the handle %q", name, got, uMinji.Handle)
		}
	}
}

func TestTotalUnreadSkipsMuted(t *testing.T) {
	s := meStore(t)
	s.Channels["C1DEV"].My = api.ChannelMyState{UnreadCount: 5, MentionCount: 1}
	s.Channels["C0GEN"].My = api.ChannelMyState{UnreadCount: 7, MentionCount: 2, Muted: true}
	s.Channels["D1MJ"].My = api.ChannelMyState{UnreadCount: 2}
	u, m := s.TotalUnread()
	if u != 7 || m != 3 {
		t.Errorf("TotalUnread = %d,%d want 7,3", u, m)
	}
}

func TestSpaceChannelsByPosition(t *testing.T) {
	s := meStore(t)
	s.ObserveChannel(textChannel("C9LAST", "S1", "끝", 5))
	s.ObserveChannel(textChannel("C5OTHER", "S2", "남의방", 0))
	got := s.SpaceChannels("S1")
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	want := []string{"C0GEN", "C1DEV", "C9LAST"}
	if len(ids) != len(want) {
		t.Fatalf("SpaceChannels = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("SpaceChannels = %v, want %v", ids, want)
		}
	}
	if got[0] != s.Channels["C0GEN"] {
		t.Error("SpaceChannels must return the store's pointers")
	}
	if len(s.SpaceChannels("NOPE")) != 0 {
		t.Error("unknown space")
	}
}

func TestObserveUserAndIdentity(t *testing.T) {
	s := meStore(t)
	if !s.IsMe(uMe.ID) || s.IsMe(uMinji.ID) || s.IsMe("") {
		t.Error("IsMe")
	}
	s.ObserveUser(uMinji)
	p := s.User(uMinji.ID)
	mj := uMinji
	mj.Handle = "minji2"
	s.ObserveUser(mj)
	if s.User(uMinji.ID).Handle != "minji2" || p.Handle != "minji2" {
		t.Error("ObserveUser: last observed value must win (in place)")
	}
	if s.User("NOPE") != nil {
		t.Error("unknown user")
	}
	if New().IsMe(uMe.ID) {
		t.Error("IsMe without Me")
	}
}

func TestFocusEndedDirect(t *testing.T) {
	s := New()
	s.Focus = &api.Focus{}
	s.Self.Focus = &api.Focus{}
	s.FocusEnded(api.FocusSummary{Counts: api.FocusCounts{DM: 1}})
	if s.Focus != nil || s.Self.Focus != nil || s.LastFocusSummary == nil || s.LastFocusSummary.Counts.DM != 1 {
		t.Errorf("FocusEnded: %+v %+v", s.Focus, s.LastFocusSummary)
	}
}

func TestChannelMembersAndDMOrder(t *testing.T) {
	s := meStore(t)
	s.Apply(ev(t, "channel.member_added", map[string]any{"channel_id": "D1MJ", "user": uHana}))
	c := s.Channels["D1MJ"]
	if len(c.Members) != 3 || c.MemberCount != 3 || s.User(uHana.ID) == nil {
		t.Errorf("member_added: %d %d", len(c.Members), c.MemberCount)
	}
	s.Apply(ev(t, "channel.member_added", map[string]any{"channel_id": "D1MJ", "user": uHana}))
	if len(c.Members) != 3 {
		t.Error("member_added duplicated")
	}
	s.Apply(ev(t, "channel.member_removed", map[string]any{"channel_id": "D1MJ", "user_id": uHana.ID}))
	if len(c.Members) != 2 || c.MemberCount != 2 {
		t.Errorf("member_removed: %d %d", len(c.Members), c.MemberCount)
	}

	// new DM via channel.created goes to the top of DMOrder (no last message yet => by created).
	before := len(s.DMOrder)
	s.Apply(ev(t, "channel.created", map[string]any{"channel": dmChannel("D3HN", uMe, uHana)}))
	if len(s.DMOrder) != before+1 || s.DMOrder[0] != "D3HN" {
		t.Errorf("DMOrder after channel.created = %v", s.DMOrder)
	}
	s.Apply(ev(t, "channel.deleted", map[string]any{"channel_id": "D3HN", "space_id": nil, "reason": "removed"}))
	for _, id := range s.DMOrder {
		if id == "D3HN" {
			t.Error("deleted DM still in DMOrder")
		}
	}
}
