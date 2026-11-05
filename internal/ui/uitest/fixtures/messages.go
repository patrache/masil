package fixtures

import (
	"time"

	"github.com/patrache/masil/internal/api"
)

func msg(id, channelID string, author api.UserSummary, body string, t time.Time) api.Message {
	return api.Message{ID: id, ChannelID: channelID, Author: author, Kind: "text", Body: body, CreatedAt: t}
}

// devRoot is the #개발 thread root (민지 11:02).
func devRoot() api.Message {
	m := msg(msgDev(1), ChDev, minji(), "오늘 배포 몇 시에 해요?", at(2, 2, 0))
	m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}, {Emoji: "👀", Count: 1}}
	m.Thread = &api.ThreadSummary{
		ReplyCount:     3,
		LastReplyAt:    ptr(at(2, 4, 30)),
		ParticipantIDs: []string{UserMinji, UserSam, UserJunho}, // most recent first
	}
	return m
}

// devThread is the reply list of devRoot (mockup: 준호 "2시?", sam "ㅇㅋ").
func devThread() []api.Message {
	root := msgDev(1)
	reply := func(n int, u api.UserSummary, body string, t time.Time) api.Message {
		m := msg(msgThread(n), ChDev, u, body, t)
		m.ThreadRootID = ptr(root)
		return m
	}
	return []api.Message{
		reply(1, junho(), "2시?", at(2, 3, 0)),
		reply(2, sam(), "ㅇㅋ", at(2, 3, 30)),
		reply(3, minji(), "넵 2시에 할게요", at(2, 4, 30)),
	}
}

func devMessages() []api.Message {
	code := msg(msgDev(2), ChDev, junho(), "이거 한 번만 돌리면 돼요\n```\n$ wrangler deploy --env prod\n```", at(2, 4, 0))
	lunch := msg(msgDev(3), ChDev, sam(), "@minji 점심 먹고 ㄱㄱ", at(2, 5, 0))
	lunch.Mentions = []api.MentionRef{{UserID: UserMinji, Handle: "minji"}}
	poll := msg(msgDev(4), ChDev, minji(), "점심 메뉴", at(2, 5, 0))
	poll.Kind = "poll"
	poll.Poll = &api.Poll{
		ID:          pollID(1),
		ContextType: "message",
		ContextID:   msgDev(4),
		CreatorID:   UserMinji,
		Question:    "점심 메뉴",
		Options:     []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 4}, {Idx: 1, Text: "돈까스", VoteCount: 2}},
		VoterCount:  6,
		MyVotes:     []int{},
	}
	return []api.Message{devRoot(), code, lunch, poll}
}

// Messages returns canned history for a channel (ID ascending, a fresh copy per call).
// Hostile channels return HostileMessages; unknown channels return nil.
func Messages(channelID string) []api.Message {
	switch channelID {
	case ChGeneral:
		return []api.Message{
			msg(msgGeneral(1), ChGeneral, hana(), "다들 좋은 아침이에요 ☀️", at(1, 40, 0)),
			msg(msgGeneral(2), ChGeneral, junho(), "어제 회의록 올려뒀어요", at(1, 50, 0)),
			msg(msgGeneral(3), ChGeneral, minji(), "오후 회의는 3시로 옮길게요", at(1, 58, 0)),
			msg(msgGeneral(4), ChGeneral, hana(), "커피 사러 가실 분?", at(2, 1, 0)),
		}
	case ChChat:
		mention := msg(msgChat(1), ChChat, minji(), "@sam 이따 점심 같이 가요", at(1, 30, 0))
		mention.Mentions = []api.MentionRef{{UserID: UserSam, Handle: "sam"}}
		return []api.Message{mention, msg(msgChat(2), ChChat, sam(), "좋아요!", at(1, 31, 0))}
	case ChDev:
		return devMessages()
	case ChDeploy:
		return []api.Message{msg(msgDeploy(1), ChDeploy, junho(), "✅ prod 배포 완료 (v0.3.1)", at(1, 0, 0))}
	case DMMinji:
		return []api.Message{msg(msgDMMinji(1), DMMinji, minji(), "점심 국밥 어때요?", at(2, 3, 0))}
	case DMJunho:
		return []api.Message{
			msg(msgDMJunho(1), DMJunho, junho(), "자료 공유드렸어요", at(1, 30, 0)),
			msg(msgDMJunho(2), DMJunho, sam(), "감사합니다", at(1, 32, 0)),
		}
	case ChHostile, chHostileCtrl, dmHostile:
		return HostileMessages(channelID)
	}
	return nil
}

// threadOf returns the canned thread for a root (nil replies if it has none) and
// whether the ID is a known message at all.
func threadOf(rootID string) (root api.Message, replies []api.Message, ok bool) {
	if rootID == msgDev(1) {
		return devRoot(), devThread(), true
	}
	for _, r := range devThread() { // a reply resolves to its root (api.md 5.11)
		if r.ID == rootID {
			return devRoot(), devThread(), true
		}
	}
	for _, id := range []string{ChGeneral, ChChat, ChDev, ChDeploy, DMMinji, DMJunho, ChHostile, chHostileCtrl, dmHostile} {
		for _, m := range Messages(id) {
			if m.ID == rootID {
				return m, []api.Message{}, true
			}
		}
	}
	return api.Message{}, []api.Message{}, false
}
