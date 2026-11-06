package fixtures

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// Hostile-set building blocks (CA 10.4).
const (
	hostileHandle = "hostile_abcdefghijklmnopqrstuvwx" // 32 chars
	syllables     = "가나다라마바사아자차카타파하"
	// emojiSoup mixes every grapheme kind the width rules care about (CA 8.2).
	emojiSoup = "❄️ ⚠️ 👍🏽 👨‍👩‍👧 🇰 🇰🇷 1️⃣ क्षि 가 é"
	// controlSoup mixes tab, CR, C0, C1, SGR and OSC 8 into text, plus the
	// string sequences the app's lipgloss compositor lets through untouched
	// (escapeSoup), so a frame-level sweep fails if a widget stops sanitizing.
	controlSoup = "탭\t문자\r캐리지 \x01\x02\x07 C0 \u0085\u009b C1 \x1b[31m빨강\x1b[0m ANSI " +
		"\x1b]8;;https://" + InjectedLinkHost + "/\x07링크\x1b]8;;\x07 OSC8 " + escapeSoup + " \x00끝"
	// escapeSoup: APC (kitty graphics), DCS (DECRQSS), SOS, PM, 8-bit CSI,
	// OSC 52 and CSI 2J. lipgloss v2.0.6's Compositor strips the last two
	// (and OSC 0/2) but passes APC, DCS, SOS, PM, C1 and OSC 8 to the
	// terminal (pending-fixes 0-b review).
	escapeSoup = "\x1b_Ga=T,f=100;iVBORw0KGgo=\x1b\\APC \x1bP1$qm\x1b\\DCS \x1bXsos\x1b\\SOS " +
		"\x1b^pm\x1b\\PM \u009b2J\u009d0;C1\u009c \x1b]52;c;UFdO\x07 \x1b[2J"
	// InjectedLinkHost is the target of the raw OSC 8 hyperlink the hostile
	// set hides in text. No frame may ever hyperlink to it (uitest check 6):
	// it only appears behind control bytes, so it is never a link of its own.
	InjectedLinkHost = "evil.example"
)

var hostileAt = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// hangul returns n Hangul syllables.
func hangul(n int) string {
	rs := []rune(syllables)
	var b strings.Builder
	for i := range n {
		b.WriteRune(rs[i%len(rs)])
	}
	return b.String()
}

// runesOf repeats chunk and cuts the result to exactly n runes.
func runesOf(chunk string, n int) string {
	s := strings.Repeat(chunk, n/utf8.RuneCountInString(chunk)+1)
	return string([]rune(s)[:n])
}

// longURL is a 300-char URL with no spaces.
func longURL() string {
	return "https://example.com/" + strings.Repeat("abcdefghij", 28)
}

func hostileLong() api.UserSummary {
	return api.UserSummary{
		ID:          userHostileLong,
		Handle:      hostileHandle,
		DisplayName: hangul(16) + "ABCDEFGHIJKLMNOP", // 32 runes, 16 Hangul
		Status: &api.UserStatus{
			Emoji: ptr("🎧"),
			Text:  runesOf("아주 긴 상태 메시지 ❄️ 집중하는 중이에요 so please do not disturb ", 120),
		},
	}
}

func hostileCtrl() api.UserSummary {
	return api.UserSummary{
		ID:          userHostileCtrl,
		Handle:      "ctrl_user",
		DisplayName: "제어\x1b[31m빨강\x1b[0m\t탭\r\u0085이름\x1b_Gf=100;AAAA\x1b\\\x1bP1$qm\x1b\\\u009b2J",
	}
}

func hostileSpace() api.SpaceDetail {
	name := "☠ 적대적 데이터 "
	name += hangul(50 - utf8.RuneCountInString(name)) // 50 runes total
	return api.SpaceDetail{Space: api.Space{
		SpacePreview: api.SpacePreview{
			ID:          SpaceHostile,
			Name:        name,
			Description: runesOf(controlSoup+" "+emojiSoup+" ", 300),
			IconEmoji:   ptr("☠"),
			MemberCount: 9999,
		},
		OwnerID:          userHostileLong,
		DefaultChannelID: ChHostile,
		CreatedAt:        hostileAt,
		MyRole:           api.RoleMember,
	}}
}

func hostileChannels() []api.Channel {
	main := textChannel(ChHostile, SpaceHostile, hangul(50), 0, api.ChannelMyState{
		Notify: "all", UnreadCount: 9999, MentionCount: 9999,
	})
	main.Topic = runesOf(emojiSoup+" "+controlSoup+" ", 250)
	main.MemberCount = 9999
	main.My.LastReadMessageID = nil
	ctrl := textChannel(chHostileCtrl, SpaceHostile, "제어\x1b[31m문자\t채널\r\x07\x1b_Gf=100;AAAA\x1b\\\x1bP1$qm\x1b\\\u009b2J", 1, api.ChannelMyState{Notify: "all", UnreadCount: 42})
	ctrl.My.LastReadMessageID = nil
	return []api.Channel{main, ctrl}
}

func hostileDM() api.Channel {
	ch := dmChannel(dmHostile, hostileCtrl())
	ch.My.UnreadCount = 9999
	ch.My.LastReadMessageID = nil
	return ch
}

// Hostile adds the hostile set to a seeded Store (separate space "☠ 적대적 데이터").
func Hostile(s *store.Store) {
	for _, u := range []api.UserSummary{hostileLong(), hostileCtrl()} {
		s.ObserveUser(u)
	}
	sp := hostileSpace()
	if _, dup := s.Spaces[SpaceHostile]; !dup {
		s.SpaceOrder = append(s.SpaceOrder, SpaceHostile)
	}
	s.Spaces[SpaceHostile] = &sp
	for _, ch := range hostileChannels() {
		s.ObserveChannel(ch)
	}
	s.ObserveChannel(hostileDM())
	s.Presence[userHostileLong] = store.PresenceEntry{Status: api.PresenceDND, FetchedAt: now}
	s.Presence[userHostileCtrl] = store.PresenceEntry{Status: api.PresenceOnline, FetchedAt: now}
}

// twentyReactions is one reaction per key the width rules care about (20 = the cap).
func twentyReactions() []api.ReactionSummary {
	keys := []string{"👍", "👀", "❄️", "⚠️", "👍🏽", "👨‍👩‍👧", "🇰", "🇰🇷", "1️⃣", "क्षि",
		"가", "é", "😀", "🎉", "🔥", "❤️", "💯", "🙏", "✅", "☠"}
	out := make([]api.ReactionSummary, len(keys))
	for i, k := range keys {
		out[i] = api.ReactionSummary{Emoji: k, Count: 999 - i, Me: i%3 == 0}
	}
	return out
}

// HostileMessages returns the hostile message set.
func HostileMessages(channelID string) []api.Message {
	long, ctrl := hostileLong(), hostileCtrl()
	t := func(n int) time.Time { return hostileAt.Add(time.Duration(n) * time.Minute) }
	huge := msg(msgHostile(1), channelID, long,
		runesOf("한글 문장과 English words 가 섞인 아주 긴 메시지입니다 "+strings.Repeat("공백없는긴단어", 30)+" "+strings.Repeat("x", 200)+" ", 4000), t(1))
	controls := msg(msgHostile(2), channelID, ctrl, controlSoup, t(2))
	emoji := msg(msgHostile(3), channelID, long, "́ 맨 앞 결합 문자, "+emojiSoup+" � 대체 문자 \xff\xfe 깨진 UTF-8", t(3))
	url := msg(msgHostile(4), channelID, sam(), "링크: "+longURL()+" 끝", t(4))
	reactions := msg(msgHostile(5), channelID, minji(), "리액션 폭탄 💣", t(5))
	reactions.Reactions = twentyReactions()
	reactions.Thread = &api.ThreadSummary{ReplyCount: 9999, LastReplyAt: ptr(t(6)), ParticipantIDs: []string{userHostileLong, userHostileCtrl, UserMinji, UserJunho, UserSam}}
	here := msg(msgHostile(6), channelID, ctrl, "@here @"+hostileHandle+" 확인해 주세요 "+emojiSoup, t(7))
	here.MentionHere = true
	here.Mentions = []api.MentionRef{{UserID: userHostileLong, Handle: hostileHandle}}
	poll := msg(msgHostile(7), channelID, long, hangul(100), t(8))
	poll.Kind = "poll"
	opts := make([]api.PollOption, 10)
	for i := range opts {
		opts[i] = api.PollOption{Idx: i, Text: runesOf(emojiSoup+" "+hangul(20), 80), VoteCount: 99999 * (i + 1)}
	}
	poll.Poll = &api.Poll{ID: pollID(9), ContextType: "message", ContextID: msgHostile(7), CreatorID: userHostileLong,
		Question: runesOf(controlSoup, 200), Multi: true, Options: opts, VoterCount: 999999, MyVotes: []int{0, 9}}
	return []api.Message{huge, controls, emoji, url, reactions, here, poll}
}

// HostileTimeline returns the hostile feed set, newest first.
func HostileTimeline() []api.TimelineItem {
	long, ctrl := hostileLong(), hostileCtrl()
	t := func(n int) time.Time { return hostileAt.Add(-time.Duration(n) * time.Minute) }
	post := func(n int, author api.UserSummary, body string) api.Post {
		// newer posts get larger IDs (IDs ascend with time, like ULIDs)
		return api.Post{ID: hostilePostID(10 - n), Author: author, Body: body, Media: []api.Media{},
			Hashtags: []string{}, Mentions: []api.MentionRef{}, CreatedAt: t(n)}
	}
	big := post(1, long, runesOf("오백 자짜리 글 "+emojiSoup+" "+strings.Repeat("긴단어", 20)+" ", 500))
	big.Media = []api.Media{photo(11, 64, 48), photo(12, 1, 1000), photo(13, 1000, 1), photo(14, 64, 48)}
	big.Hashtags = []string{hangul(50), "golang"}
	big.Mentions = []api.MentionRef{{UserID: userHostileLong, Handle: hostileHandle}}
	big.LikeCount, big.RepostCount, big.ReplyCount, big.QuoteCount = 9999999, 999999, 99999, 9999
	controls := post(2, ctrl, controlSoup)
	emoji := post(3, long, "́ "+emojiSoup+" �")
	url := post(4, ctrl, longURL())
	quoted := post(5, long, "@"+hostileHandle+" 인용 "+hangul(40))
	quoted.Quote = &api.QuotedPost{Available: false, ID: hostilePostID(1), Reason: "deleted"}
	quoted.ReplyToID = ptr(big.ID)
	quoted.RootID = ptr(big.ID)
	quoted.ReplyToAuthor = ptr(long)
	repost := api.TimelineItem{ID: repostID(1), Type: "repost", Post: big, RepostedBy: ptr(ctrl), At: t(6)}
	return []api.TimelineItem{postItem(big), postItem(controls), postItem(emoji), postItem(url), postItem(quoted), repost}
}
