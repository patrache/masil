package feed

// Shared ui/feed test fixtures (plan 05 Task 5). Every later ui/feed test refers to
// these; none defines its own. Clock reference = harness clock 2026-11-05T02:05:00Z.

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// fixtureNow is the harness clock start (uitest.Start).
var fixtureNow = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// storyExpiry is every fixture story's expires_at.
var storyExpiry = time.Date(2026, 11, 5, 22, 5, 0, 0, time.UTC)

// devThreadReply3 is the third #개발 thread reply (fixtures msgThread(3), unexported there).
const devThreadReply3 = "01JDTHR0000000000000000003"

// sid is a 26-char sns-fun fixture ID ("01JDSNS" + 19 digits).
func sid(n int) string { return fmt.Sprintf("01JDSNS%019d", n) }

func ptr[T any](v T) *T { return &v }

// at is 02:05Z minus minAgo minutes (negative = future).
func at(minAgo int) time.Time { return fixtureNow.Add(-time.Duration(minAgo) * time.Minute) }

// seeded is a fresh Store with the core fixtures.
func seeded() *store.Store {
	s := store.New()
	fixtures.Seed(s)
	return s
}

var fixtureUsers = sync.OnceValue(func() map[string]api.UserSummary {
	out := map[string]api.UserSummary{}
	for _, u := range seeded().Users {
		out[u.Handle] = *u
	}
	return out
})

// userOf is the seeded user with handle (sam, minji, junho, hana, dev_kim).
func userOf(handle string) api.UserSummary {
	u, ok := fixtureUsers()[handle]
	if !ok {
		panic("snsfix: no seeded user @" + handle)
	}
	return u
}

// postBy is the fixtures.Timeline() post by hana or dev_kim.
func postBy(handle string) api.Post {
	for _, it := range fixtures.Timeline() {
		if it.Post.Author.Handle == handle {
			return it.Post
		}
	}
	panic("snsfix: no fixtures.Timeline post by @" + handle)
}

// hashtagsOf is the lowercased "#word" tokens of body (no '#').
func hashtagsOf(body string) []string {
	out := []string{}
	for _, f := range strings.Fields(body) {
		if !strings.HasPrefix(f, "#") {
			continue
		}
		tag := strings.TrimRightFunc(f[1:], func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
		})
		if tag != "" {
			out = append(out, strings.ToLower(tag))
		}
	}
	return out
}

func mkPost(n int, handle, body string, minAgo int) api.Post {
	return api.Post{
		ID:        sid(n),
		Author:    userOf(handle),
		Body:      body,
		Media:     []api.Media{},
		Hashtags:  hashtagsOf(body),
		Mentions:  []api.MentionRef{},
		CreatedAt: at(minAgo),
	}
}

func itemOf(p api.Post) api.TimelineItem {
	return api.TimelineItem{ID: p.ID, Type: "post", Post: p, At: p.CreatedAt}
}

func quotePost() api.Post {
	p := mkPost(10, "minji", "이거 보세요", 5)
	p.Quote = &api.QuotedPost{Available: true, Post: ptr(postBy("hana"))}
	return p
}

func pollPost() api.Post {
	p := mkPost(11, "minji", "", 6)
	p.Poll = &api.Poll{
		ID:          sid(12),
		ContextType: "post",
		ContextID:   sid(11),
		CreatorID:   userOf("minji").ID,
		Question:    "점심 메뉴",
		Multi:       false,
		Options:     []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 4}, {Idx: 1, Text: "돈까스", VoteCount: 2}},
		VoterCount:  6,
		ClosesAt:    ptr(at(-60)),
		MyVotes:     []int{0},
	}
	return p
}

// clonePoll deep-copies p's poll so variants never share slices.
func clonePoll(p *api.Poll) *api.Poll {
	c := *p
	c.Options = append([]api.PollOption(nil), p.Options...)
	c.MyVotes = append([]int{}, p.MyVotes...)
	if p.ClosesAt != nil {
		c.ClosesAt = ptr(*p.ClosesAt)
	}
	return &c
}

func multiPollPost() api.Post {
	p := pollPost()
	p.ID = sid(13)
	p.Poll = clonePoll(p.Poll)
	p.Poll.ID = sid(14)
	p.Poll.ContextID = sid(13)
	p.Poll.Multi = true
	p.Poll.MyVotes = []int{}
	return p
}

func myPollPost() api.Post {
	p := pollPost()
	p.ID = sid(19)
	p.Author = userOf("sam")
	p.Poll = clonePoll(p.Poll)
	p.Poll.ID = sid(25)
	p.Poll.ContextID = sid(19)
	p.Poll.CreatorID = userOf("sam").ID
	return p
}

func deletedPost() api.Post {
	p := mkPost(15, "junho", "", 20)
	p.Deleted = true
	return p
}

func repostItem() api.TimelineItem {
	return api.TimelineItem{ID: sid(16), Type: "repost", Post: postBy("hana"), RepostedBy: ptr(userOf("dev_kim")), At: at(1)}
}

func myPost() api.Post { return mkPost(17, "sam", "점심 먹고 배포합니다", 2) }

func privatePost() api.Post {
	p := mkPost(18, "junho", "비공개 계정 글", 4)
	p.Author.IsPrivate = true
	return p
}

// longTimeline is five minji filler posts then dev_kim's post (6 items, dev_kim last).
func longTimeline() []api.TimelineItem {
	out := make([]api.TimelineItem, 0, 6)
	for i := 0; i < 5; i++ {
		out = append(out, itemOf(mkPost(20+i, "minji", fmt.Sprintf("채우기 글 %d", i+1), 1+i)))
	}
	return append(out, itemOf(postBy("dev_kim")))
}

// reply builds a reply of parent in the thread rooted at root.
func reply(n int, handle, body string, minAgo int, parent api.Post, rootID string) api.Post {
	p := mkPost(n, handle, body, minAgo)
	p.ReplyToID = ptr(parent.ID)
	p.RootID = ptr(rootID)
	p.ReplyToAuthor = ptr(parent.Author)
	return p
}

// replyTree is the reply tree under hana's post: three top-level replies, two under sid(31).
func replyTree() (top []api.Post, children map[string][]api.Post) {
	parent := postBy("hana")
	root := parent.ID
	mid := reply(31, "junho", "사진 예쁘다", 2, parent, root)
	mid.ReplyCount = 2
	top = []api.Post{
		reply(30, "minji", "우와 벌써?", 2, parent, root),
		mid,
		reply(32, "sam", "출근길 조심!", 1, parent, root),
	}
	children = map[string][]api.Post{
		sid(31): {
			reply(33, "hana", "고마워요", 1, mid, root),
			reply(34, "minji", "나도 찍었는데", 1, mid, root),
		},
	}
	return top, children
}

// deepChain is a 7-deep reply chain under hana's post (minji/junho alternating).
func deepChain() []api.Post {
	parent := postBy("hana")
	root := parent.ID
	out := make([]api.Post, 0, 7)
	for d := 1; d <= 7; d++ {
		handle := "minji"
		if d%2 == 0 {
			handle = "junho"
		}
		p := reply(39+d, handle, fmt.Sprintf("깊이 %d", d), 1, parent, root)
		out = append(out, p)
		parent = p
	}
	return out
}

func profileOf(handle string, rel api.Relationship) api.Profile {
	p := api.Profile{
		UserSummary:    userOf(handle),
		CreatedAt:      time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC),
		FollowerCount:  10,
		FollowingCount: 10,
		PostCount:      3,
	}
	if handle == "hana" {
		p.Bio = "눈 오는 날 좋아함"
		p.FollowerCount, p.FollowingCount, p.PostCount = 120, 80, 42
		p.Presence = ptr("online")
		p.Status = &api.UserStatus{Emoji: ptr("🎧"), Text: "듣는 중"}
	}
	if handle != "sam" {
		p.Relationship = &rel
	}
	return p
}

func relFollowing() api.Relationship {
	return api.Relationship{Following: "active", FollowedBy: "active", CanDM: true, CanViewPosts: true}
}

func profilePosts(handle, filter string) []api.TimelineItem {
	if filter != "posts" {
		return []api.TimelineItem{}
	}
	switch handle {
	case "dev_kim":
		return []api.TimelineItem{
			itemOf(postBy("dev_kim")),
			{ID: sid(71), Type: "repost", Post: postBy("hana"), RepostedBy: ptr(userOf("dev_kim")), At: at(2)},
		}
	case "hana":
		return []api.TimelineItem{itemOf(postBy("hana"))}
	case "sam":
		return []api.TimelineItem{itemOf(myPost())}
	}
	return []api.TimelineItem{}
}

func hashtagPosts(tag string) []api.TimelineItem {
	switch tag {
	case "첫눈":
		return []api.TimelineItem{itemOf(postBy("hana")), itemOf(mkPost(70, "minji", "#첫눈 왔다", 8))}
	case "golang":
		return []api.TimelineItem{itemOf(postBy("dev_kim"))}
	}
	return []api.TimelineItem{}
}

func guestbookEntries(ownerID string) []api.GuestbookEntry {
	return []api.GuestbookEntry{
		{ID: sid(50), OwnerID: ownerID, Author: ptr(userOf("minji")), Body: "놀러 왔어요 ✨", CreatedAt: at(3)},
		{ID: sid(51), OwnerID: ownerID, Author: nil, Body: "", IsSecret: true, Hidden: true, CreatedAt: at(10)},
		{ID: sid(52), OwnerID: ownerID, Author: ptr(userOf("sam")), Body: "또 올게요", CreatedAt: at(60)},
	}
}

func followRequests() []api.FollowRequestEntry {
	return []api.FollowRequestEntry{
		{User: userOf("minji"), CreatedAt: at(30)},
		{User: userOf("junho"), CreatedAt: at(90)},
	}
}

// storyGroups is [sam, hana, junho]; LatestAt = the group's last story time.
func storyGroups() []api.StoryGroup {
	story := func(n int, handle, body, bg string, minAgo int, seen bool) api.Story {
		return api.Story{ID: sid(n), Author: userOf(handle), Body: body, BG: bg, CreatedAt: at(minAgo), ExpiresAt: storyExpiry, Seen: seen}
	}
	samStory := story(60, "sam", "출근 완료", "plain", 30, true)
	samStory.ViewCount = ptr(3)
	hanaPhoto := story(62, "hana", "", "accent2", 5, false)
	hanaPhoto.Media = &api.Media{ID: sid(63), URL: "/v1/media/" + sid(63), ContentType: "image/png", Width: ptr(64), Height: ptr(48)}
	hanaStories := []api.Story{story(61, "hana", "눈 온다 ❄", "accent", 15, false), hanaPhoto}
	junhoStory := story(64, "junho", "커피 ☕", "plain", 60, true)
	return []api.StoryGroup{
		{User: userOf("sam"), Stories: []api.Story{samStory}, HasUnseen: false, LatestAt: samStory.CreatedAt},
		{User: userOf("hana"), Stories: hanaStories, HasUnseen: true, LatestAt: hanaPhoto.CreatedAt},
		{User: userOf("junho"), Stories: []api.Story{junhoStory}, HasUnseen: false, LatestAt: junhoStory.CreatedAt},
	}
}

func trendsTop10() *api.Trends {
	type row struct {
		tag            string
		posts, authors int
	}
	rows := []row{
		{"첫눈", 42, 30}, {"golang", 31, 12}, {"점심", 28, 20}, {"배포", 19, 9}, {"bubbletea", 15, 7},
		{"출근길", 12, 10}, {"커피", 11, 9}, {"고양이", 9, 8}, {"야근", 7, 6}, {"주말", 5, 5},
	}
	items := make([]api.Trend, len(rows))
	for i, r := range rows {
		items[i] = api.Trend{Rank: i + 1, Tag: r.tag, PostCount: r.posts, AuthorCount: r.authors}
	}
	return &api.Trends{ComputedAt: ptr(time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)), Items: items}
}

// notificationsAll is fixtures.Notifications() then one notification per contract
// type (14, IDs sid(80..93), actor minji, older than the fixtures pair), each with
// the context its Task 15 target needs.
func notificationsAll() []api.Notification {
	minji := userOf("minji")
	hanaPost := postBy("hana").ID
	general := fixtures.Messages(fixtures.ChGeneral)[2].ID
	devRoot := fixtures.Messages(fixtures.ChDev)[0].ID
	postCtx := api.NotificationContext{PostID: ptr(hanaPost)}
	user := api.NotificationSubject{Type: "user", ID: minji.ID}
	post := api.NotificationSubject{Type: "post", ID: hanaPost}
	type spec struct {
		typ     api.NotificationType
		subject api.NotificationSubject
		preview string
		ctx     api.NotificationContext
	}
	specs := []spec{
		{"follow", user, "", api.NotificationContext{}},
		{"follow_request", user, "", api.NotificationContext{}},
		{"follow_accepted", user, "", api.NotificationContext{}},
		{"friend_request", api.NotificationSubject{Type: "friend_request", ID: sid(95)}, "", api.NotificationContext{}},
		{"friend_accepted", user, "", api.NotificationContext{}},
		{"space_invite", api.NotificationSubject{Type: "space_direct_invite", ID: sid(94)}, "",
			api.NotificationContext{SpaceID: ptr(fixtures.SpaceNova), SpaceName: ptr("노바 개발방")}},
		{"post_like", post, "오늘 첫 눈 ❄️ 사무실 창밖", postCtx},
		{"post_repost", post, "오늘 첫 눈 ❄️ 사무실 창밖", postCtx},
		{"post_reply", post, "우와 벌써?", postCtx},
		{"post_quote", post, "이거 보세요", postCtx},
		{"post_mention", post, "@sam 이것 좀", postCtx},
		{"message_mention", api.NotificationSubject{Type: "message", ID: general}, "오후 회의는 3시로 옮길게요",
			api.NotificationContext{ChannelID: ptr(fixtures.ChGeneral), MessageID: ptr(general),
				SpaceID: ptr(fixtures.SpaceNova), SpaceName: ptr("노바 개발방")}},
		{"thread_reply", api.NotificationSubject{Type: "message", ID: devThreadReply3}, "넵 2시에 할게요",
			api.NotificationContext{ChannelID: ptr(fixtures.ChDev), MessageID: ptr(devThreadReply3), ThreadRootID: ptr(devRoot),
				SpaceID: ptr(fixtures.SpaceNova), SpaceName: ptr("노바 개발방")}},
		{"guestbook_entry", api.NotificationSubject{Type: "guestbook_entry", ID: sid(50)}, "놀러 왔어요 ✨", api.NotificationContext{}},
	}
	out := fixtures.Notifications()
	for i, s := range specs {
		out = append(out, api.Notification{
			ID:        sid(93 - i),
			Type:      s.typ,
			Actor:     ptr(minji),
			Subject:   s.subject,
			Preview:   s.preview,
			Context:   s.ctx,
			CreatedAt: at(40 + i), // newest first, all older than the fixtures pair
		})
	}
	return out
}

// allPosts is every post above plus the fixtures.Timeline posts, by ID.
func allPosts() map[string]api.Post {
	out := map[string]api.Post{}
	add := func(ps ...api.Post) {
		for _, p := range ps {
			out[p.ID] = p
		}
	}
	for _, it := range fixtures.Timeline() {
		add(it.Post)
	}
	add(quotePost(), pollPost(), multiPollPost(), myPollPost(), deletedPost(), myPost(), privatePost())
	for _, it := range longTimeline() {
		add(it.Post)
	}
	top, children := replyTree()
	add(top...)
	for _, cs := range children {
		add(cs...)
	}
	add(deepChain()...)
	for _, tag := range []string{"첫눈", "golang"} {
		for _, it := range hashtagPosts(tag) {
			add(it.Post)
		}
	}
	return out
}
