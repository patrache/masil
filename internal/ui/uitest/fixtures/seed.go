package fixtures

import (
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// now is the fixed test clock (CA 10.1): 2026-11-05 11:05 KST.
var now = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// at is 2026-11-05 h:m:s UTC (KST = UTC+9, so at(2, 2, 0) is 11:02 in Seoul).
func at(h, m, s int) time.Time { return time.Date(2026, 11, 5, h, m, s, 0, time.UTC) }

func ptr[T any](v T) *T { return &v }

func user(id, handle, name string) api.UserSummary {
	return api.UserSummary{ID: id, Handle: handle, DisplayName: name}
}

func sam() api.UserSummary    { return user(UserSam, "sam", "sam") }
func minji() api.UserSummary  { return user(UserMinji, "minji", "민지") }
func junho() api.UserSummary  { return user(UserJunho, "junho", "준호") }
func hana() api.UserSummary   { return user(UserHana, "hana", "하나") }
func devKim() api.UserSummary { return user(UserDevKim, "dev_kim", "김개발") }

func users() []api.UserSummary {
	return []api.UserSummary{sam(), minji(), junho(), hana(), devKim()}
}

func me() api.Me {
	return api.Me{
		UserSummary:    sam(),
		Bio:            "터미널에서 삽니다",
		CreatedAt:      time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC),
		Role:           "user",
		FollowerCount:  12,
		FollowingCount: 8,
		PostCount:      3,
		Settings: api.Settings{
			DMPolicy: "friends_and_spaces",
			Notify: api.NotifySettings{
				DM: true, Mention: true, Invite: true, FriendRequest: true, ThreadReply: true, Social: true,
			},
			FocusDefaultMin: 25,
		},
		Presence: api.PresenceState{Status: api.PresenceOnline, Mode: "auto"},
	}
}

var spaceCreated = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func novaSpace() api.SpaceDetail {
	return api.SpaceDetail{Space: api.Space{
		SpacePreview: api.SpacePreview{
			ID:          SpaceNova,
			Name:        "노바 개발방",
			Description: "노바 팀 개발 이야기",
			IconEmoji:   ptr("🚀"),
			MemberCount: 5,
		},
		OwnerID:          UserSam,
		DefaultChannelID: ChGeneral,
		CreatedAt:        spaceCreated,
		MyRole:           api.RoleOwner,
	}}
}

// textChannel builds a text channel; its last message is the last of Messages(id).
func textChannel(id, spaceID, name string, pos int, my api.ChannelMyState) api.Channel {
	ch := api.Channel{
		ID:          id,
		Kind:        api.ChannelText,
		SpaceID:     ptr(spaceID),
		Name:        ptr(name),
		Position:    pos,
		MemberCount: 5,
		CreatedAt:   spaceCreated,
		My:          my,
	}
	setLast(&ch)
	return ch
}

func dmChannel(id string, other api.UserSummary) api.Channel {
	ch := api.Channel{
		ID:          id,
		Kind:        api.ChannelDM,
		MemberCount: 2,
		Members:     []api.UserSummary{sam(), other},
		CreatedAt:   spaceCreated,
		My:          api.ChannelMyState{Notify: "all"},
	}
	setLast(&ch)
	return ch
}

// setLast points the channel at its newest canned message (and marks it read).
func setLast(ch *api.Channel) {
	ms := Messages(ch.ID)
	if len(ms) == 0 {
		return
	}
	last := ms[len(ms)-1]
	ch.LastMessageID = ptr(last.ID)
	ch.LastMessageAt = ptr(last.CreatedAt)
	if ch.My.LastReadMessageID == nil {
		ch.My.LastReadMessageID = ptr(last.ID)
	}
}

func seedChannels() []api.Channel {
	all := api.ChannelMyState{Notify: "all"}
	general := all
	general.UnreadCount = 3
	general.LastReadMessageID = ptr(msgGeneral(1)) // messages 2..4 unread
	return []api.Channel{
		textChannel(ChGeneral, SpaceNova, "일반", 0, general),
		textChannel(ChChat, SpaceNova, "잡담", 1, all),
		textChannel(ChDev, SpaceNova, "개발", 2, all),
		textChannel(ChDeploy, SpaceNova, "배포알림", 3, api.ChannelMyState{Notify: "mentions"}),
	}
}

func seedDMs() []api.Channel {
	return []api.Channel{dmChannel(DMMinji, minji()), dmChannel(DMJunho, junho())}
}

func seedFriends() []api.Friend {
	return []api.Friend{
		{User: minji(), Since: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), Presence: api.PresenceOnline},
		{User: junho(), Since: time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC), Presence: api.PresenceOffline},
		{User: hana(), Since: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), Presence: api.PresenceOnline},
	}
}

// Seed fills core entities.
func Seed(s *store.Store) {
	m := me()
	s.Me = &m
	s.Settings = m.Settings
	s.Self = m.Presence
	for _, u := range users() {
		s.ObserveUser(u)
	}

	nova := novaSpace()
	s.Spaces[SpaceNova] = &nova
	s.SpaceOrder = append(s.SpaceOrder, SpaceNova)
	for _, ch := range seedChannels() {
		s.ObserveChannel(ch)
	}
	for _, ch := range seedDMs() {
		s.ObserveChannel(ch)
	}

	for _, f := range seedFriends() {
		s.Friends[f.User.ID] = f
	}
	s.Presence[UserMinji] = store.PresenceEntry{Status: api.PresenceOnline, FetchedAt: now}
	s.Presence[UserJunho] = store.PresenceEntry{Status: api.PresenceOffline, LastSeenAt: ptr(at(1, 40, 0)), FetchedAt: now}
	s.Presence[UserHana] = store.PresenceEntry{Status: api.PresenceOnline, FetchedAt: now}
	s.Presence[UserDevKim] = store.PresenceEntry{Status: api.PresenceAway, FetchedAt: now}
	s.NotifUnread = len(Notifications())
}
