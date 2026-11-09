package api_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

// endpointsB is every CA 4.3.3 method from "5.10 messages" through "5.22 admin".
var endpointsB = []string{
	// 5.10 messages
	"ListMessages", "SendMessage", "GetMessage", "EditMessage", "DeleteMessage",
	// 5.11 threads
	"GetThread", "ReplyThread",
	// 5.12 reactions / pins
	"AddReaction", "RemoveReaction", "ListReactionUsers", "Pin", "Unpin", "ListPins",
	// 5.13 search
	"SearchMessages", "SearchPosts", "SearchHashtags",
	// 5.14 uploads / media
	"Upload", "DeleteUpload", "FetchMedia",
	// 5.15 posts
	"CreatePost", "GetPost", "GetPostContext", "ListReplies", "DeletePost", "Like", "Unlike",
	"ListLikes", "Repost", "Unrepost", "ListReposts", "ListQuotes",
	// 5.16 timelines / hashtags / trends
	"FollowingTimeline", "DiscoverTimeline", "HashtagPosts", "Trends",
	// 5.17 stories
	"CreateStory", "StoryFeed", "ListUserStories", "ViewStory", "ListStoryViewers", "DeleteStory",
	// 5.18 notifications
	"ListNotifications", "NotificationUnreadCount", "MarkNotificationsRead",
	// 5.19 polls
	"GetPoll", "Vote", "RetractVote", "ClosePoll",
	// 5.20 guestbook
	"ListGuestbook", "WriteGuestbook", "DeleteGuestbookEntry",
	// 5.21 reports
	"Report",
	// 5.22 admin
	"AdminListReports", "AdminGetReport", "AdminResolveReport", "AdminRemoveContent",
	"AdminSuspend", "AdminUnsuspend", "AdminListUsers", "AdminListActions",
	"AdminSetLeetDaily",
}

type msgResult struct {
	msg     *api.Message
	created bool
}

type postResult struct {
	post    *api.Post
	created bool
}

func checkMsgCreated(want bool) func(t *testing.T, got any) {
	return func(t *testing.T, got any) {
		r := got.(msgResult)
		if r.created != want || r.msg == nil || r.msg.ID == "" {
			t.Errorf("created=%v msg=%+v, want created=%v and a message", r.created, r.msg, want)
		}
	}
}

func checkInt(want int) func(t *testing.T, got any) {
	return func(t *testing.T, got any) {
		if got.(int) != want {
			t.Errorf("got %v, want %d", got, want)
		}
	}
}

func endpointsBCases(t *testing.T) []epCase {
	msgEnv := env(t, "message", "Message")
	postEnv := env(t, "post", "Post")
	pollEnv := env(t, "poll", "Poll")
	yes := true
	return []epCase{
		// 5.10 messages
		{name: "ListMessages", route: "GET /v1/channels/{id}/messages", wantPath: "/v1/channels/C/messages", resp: sample(t, "MessageWindow"),
			call: func(c *api.Client) (any, error) {
				return c.ListMessages(ctx, "C", api.MessageQuery{Around: "M", Limit: 50})
			},
			wantQuery: "around=M&limit=50"},
		{name: "ListMessages/latest", route: "GET /v1/channels/{id}/messages", wantPath: "/v1/channels/C/messages",
			resp: `{"items":[],"has_more_before":false,"has_more_after":false}`,
			call: func(c *api.Client) (any, error) { return c.ListMessages(ctx, "C", api.MessageQuery{}) },
			check: func(t *testing.T, got any) {
				w := got.(*api.MessageWindow)
				if w.Items == nil || len(w.Items) != 0 {
					t.Errorf("items = %#v, want empty non-nil", w.Items)
				}
			}},
		{name: "ListMessages/before", route: "GET /v1/channels/{id}/messages", wantPath: "/v1/channels/C/messages", resp: sample(t, "MessageWindow"),
			call:      func(c *api.Client) (any, error) { return c.ListMessages(ctx, "C", api.MessageQuery{Before: "B"}) },
			wantQuery: "before=B"},
		{name: "ListMessages/after", route: "GET /v1/channels/{id}/messages", wantPath: "/v1/channels/C/messages", resp: sample(t, "MessageWindow"),
			call: func(c *api.Client) (any, error) {
				return c.ListMessages(ctx, "C", api.MessageQuery{After: "A", Limit: 100})
			},
			wantQuery: "after=A&limit=100"},
		{name: "SendMessage", route: "POST /v1/channels/{id}/messages", wantPath: "/v1/channels/C/messages", status: 201, resp: msgEnv, limiter: "RL_MESSAGE",
			call: func(c *api.Client) (any, error) {
				m, created, err := c.SendMessage(ctx, "C", api.SendMessageRequest{ClientNonce: "n1", Kind: "poll",
					Poll: &api.PollInput{Question: "점심", Options: []string{"국밥", "돈까스"}}})
				return msgResult{m, created}, err
			},
			wantBody: `{"client_nonce":"n1","kind":"poll","body":"","poll":{"question":"점심","options":["국밥","돈까스"]}}`,
			check:    checkMsgCreated(true)},
		{name: "SendMessage/dup", route: "POST /v1/channels/{id}/messages", wantPath: "/v1/channels/C/messages", status: 200, resp: msgEnv, limiter: "RL_MESSAGE",
			call: func(c *api.Client) (any, error) {
				m, created, err := c.SendMessage(ctx, "C", api.SendMessageRequest{ClientNonce: "n1", Body: "안녕"})
				return msgResult{m, created}, err
			},
			wantBody: `{"client_nonce":"n1","body":"안녕"}`,
			check:    checkMsgCreated(false)},
		{name: "GetMessage", route: "GET /v1/messages/{id}", wantPath: "/v1/messages/M", resp: msgEnv,
			call: func(c *api.Client) (any, error) { return c.GetMessage(ctx, "M") }},
		{name: "EditMessage", route: "PATCH /v1/messages/{id}", wantPath: "/v1/messages/M", resp: msgEnv, limiter: "RL_MESSAGE",
			call:     func(c *api.Client) (any, error) { return c.EditMessage(ctx, "M", "고친 글") },
			wantBody: `{"body":"고친 글"}`},
		{name: "DeleteMessage", route: "DELETE /v1/messages/{id}", wantPath: "/v1/messages/M",
			call: func(c *api.Client) (any, error) { return err0(c.DeleteMessage(ctx, "M")) }},

		// 5.11 threads
		{name: "GetThread", route: "GET /v1/messages/{id}/thread", wantPath: "/v1/messages/R/thread", resp: sample(t, "ThreadWindow"),
			call: func(c *api.Client) (any, error) {
				return c.GetThread(ctx, "R", api.ThreadQuery{Before: "B", Limit: 30})
			},
			wantQuery: "before=B&limit=30",
			check: func(t *testing.T, got any) {
				w := got.(*api.ThreadWindow)
				if w.Root.ID == "" || len(w.Items) == 0 {
					t.Errorf("thread = %+v", w)
				}
			}},
		{name: "ReplyThread", route: "POST /v1/messages/{id}/thread", wantPath: "/v1/messages/R/thread", status: 201, resp: msgEnv, limiter: "RL_MESSAGE",
			call: func(c *api.Client) (any, error) {
				m, created, err := c.ReplyThread(ctx, "R", api.ThreadReplyRequest{ClientNonce: "n2", Kind: "me", Body: "춤춘다"})
				return msgResult{m, created}, err
			},
			wantBody: `{"client_nonce":"n2","kind":"me","body":"춤춘다"}`,
			check:    checkMsgCreated(true)},
		{name: "ReplyThread/dup", route: "POST /v1/messages/{id}/thread", wantPath: "/v1/messages/R/thread", status: 200, resp: msgEnv, limiter: "RL_MESSAGE",
			call: func(c *api.Client) (any, error) {
				m, created, err := c.ReplyThread(ctx, "R", api.ThreadReplyRequest{ClientNonce: "n2", Body: "ㅋㅋ"})
				return msgResult{m, created}, err
			},
			wantBody: `{"client_nonce":"n2","body":"ㅋㅋ"}`,
			check:    checkMsgCreated(false)},

		// 5.12 reactions / pins
		{name: "AddReaction", route: "PUT /v1/messages/{id}/reactions/{emoji}", wantPath: "/v1/messages/M/reactions/%F0%9F%91%8D", limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return err0(c.AddReaction(ctx, "M", "👍")) }},
		{name: "RemoveReaction", route: "DELETE /v1/messages/{id}/reactions/{emoji}", wantPath: "/v1/messages/M/reactions/%F0%9F%91%8D", limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return err0(c.RemoveReaction(ctx, "M", "👍")) }},
		{name: "ListReactionUsers", route: "GET /v1/messages/{id}/reactions/{emoji}", wantPath: "/v1/messages/M/reactions/%F0%9F%91%8D",
			resp: pageOf(t, "UserSummary"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListReactionUsers(ctx, "M", "👍", pg) }},
		{name: "Pin", route: "PUT /v1/messages/{id}/pin", wantPath: "/v1/messages/M/pin", limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return err0(c.Pin(ctx, "M")) }},
		{name: "Unpin", route: "DELETE /v1/messages/{id}/pin", wantPath: "/v1/messages/M/pin", limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return err0(c.Unpin(ctx, "M")) }},
		{name: "ListPins", route: "GET /v1/channels/{id}/pins", wantPath: "/v1/channels/C/pins", resp: itemsOf(t, "Message"),
			call: func(c *api.Client) (any, error) { return c.ListPins(ctx, "C") }},

		// 5.13 search
		{name: "SearchMessages", route: "GET /v1/search/messages", resp: pageOf(t, "MessageSearchHit"), limiter: "RL_SEARCH",
			call: func(c *api.Client) (any, error) {
				return c.SearchMessages(ctx, api.MessageSearchQuery{Q: "배포", ChannelID: "C", SpaceID: "S", Author: api.Handle("MinJi"), PageOpts: pg})
			},
			wantQuery: "q=%EB%B0%B0%ED%8F%AC&channel_id=C&space_id=S&author=%40minji&" + pgQuery},
		{name: "SearchMessages/qOnly", route: "GET /v1/search/messages", resp: pageOf(t, "MessageSearchHit"), limiter: "RL_SEARCH",
			call:      func(c *api.Client) (any, error) { return c.SearchMessages(ctx, api.MessageSearchQuery{Q: "점심"}) },
			wantQuery: "q=%EC%A0%90%EC%8B%AC"},
		{name: "SearchPosts", route: "GET /v1/search/posts", resp: pageOf(t, "PostSearchHit"), limiter: "RL_SEARCH",
			call: func(c *api.Client) (any, error) {
				return c.SearchPosts(ctx, api.PostSearchQuery{Q: "첫눈", Author: api.UserID("01JA"), PageOpts: pg})
			},
			wantQuery: "q=%EC%B2%AB%EB%88%88&author=01JA&" + pgQuery},
		{name: "SearchHashtags", route: "GET /v1/hashtags/search", resp: itemsOf(t, "HashtagSuggestion"), limiter: "RL_SEARCH",
			call:      func(c *api.Client) (any, error) { return c.SearchHashtags(ctx, "첫", 10) },
			wantQuery: "q=%EC%B2%AB&limit=10"},

		// 5.14 uploads / media
		{name: "Upload", route: "POST /v1/uploads", status: 201, resp: env(t, "upload", "Upload"), limiter: "RL_UPLOAD",
			call: func(c *api.Client) (any, error) {
				return c.Upload(ctx, "post", "image/png", bytes.NewReader([]byte{1, 2, 3, 4}), 4)
			},
			wantQuery: "purpose=post", wantRaw: "\x01\x02\x03\x04",
			reqHeader: map[string]string{"Content-Type": "image/png", "Content-Length": "4"},
			check: func(t *testing.T, got any) {
				u := got.(*api.Upload)
				if u.ID == "" || u.Purpose == "" {
					t.Errorf("upload = %+v", u)
				}
			}},
		{name: "Upload/plainReader", route: "POST /v1/uploads", status: 201, resp: env(t, "upload", "Upload"), limiter: "RL_UPLOAD",
			call: func(c *api.Client) (any, error) {
				// a reader net/http cannot size by itself: Content-Length comes from size
				return c.Upload(ctx, "avatar", "image/jpeg", io.MultiReader(strings.NewReader("ab"), strings.NewReader("c")), 3)
			},
			wantQuery: "purpose=avatar", wantRaw: "abc",
			reqHeader: map[string]string{"Content-Type": "image/jpeg", "Content-Length": "3"}},
		{name: "DeleteUpload", route: "DELETE /v1/uploads/{id}", wantPath: "/v1/uploads/U",
			call: func(c *api.Client) (any, error) { return err0(c.DeleteUpload(ctx, "U")) }},
		{name: "FetchMedia", route: "GET /v1/media/{id}", wantPath: "/v1/media/U", resp: "\x89PNG",
			respHeader: map[string]string{"Content-Type": "image/png", "ETag": `"e2"`},
			call:       func(c *api.Client) (any, error) { return c.FetchMedia(ctx, "/v1/media/U", "") },
			check: func(t *testing.T, got any) {
				b := got.(*api.MediaBlob)
				if b.NotModified || string(b.Data) != "\x89PNG" || b.ContentType != "image/png" || b.ETag != `"e2"` {
					t.Errorf("blob = %+v", b)
				}
			}},
		{name: "FetchMedia/notModified", route: "GET /v1/media/{id}", wantPath: "/v1/media/U", status: http.StatusNotModified,
			respHeader: map[string]string{"ETag": "e1"},
			reqHeader:  map[string]string{"If-None-Match": "e1"},
			call:       func(c *api.Client) (any, error) { return c.FetchMedia(ctx, "/v1/media/U", "e1") },
			check: func(t *testing.T, got any) {
				b := got.(*api.MediaBlob)
				if !b.NotModified || b.Data != nil {
					t.Errorf("blob = %+v, want NotModified with no data", b)
				}
			}},

		// 5.15 posts
		{name: "CreatePost", route: "POST /v1/posts", status: 201, resp: postEnv, limiter: "RL_POST",
			call: func(c *api.Client) (any, error) {
				p, created, err := c.CreatePost(ctx, api.CreatePostRequest{ClientNonce: "n", Body: "오늘 첫 눈 ❄️ #첫눈", MediaUploadIDs: []string{"U"}})
				return postResult{p, created}, err
			},
			wantBody: `{"client_nonce":"n","body":"오늘 첫 눈 ❄️ #첫눈","media_upload_ids":["U"]}`,
			check: func(t *testing.T, got any) {
				r := got.(postResult)
				if !r.created || r.post == nil || r.post.ID == "" {
					t.Errorf("created=%v post=%+v", r.created, r.post)
				}
			}},
		{name: "CreatePost/dupReply", route: "POST /v1/posts", status: 200, resp: postEnv, limiter: "RL_POST",
			call: func(c *api.Client) (any, error) {
				p, created, err := c.CreatePost(ctx, api.CreatePostRequest{ClientNonce: "n", Body: "동의", ReplyToID: "P"})
				return postResult{p, created}, err
			},
			wantBody: `{"client_nonce":"n","body":"동의","reply_to_id":"P"}`,
			check: func(t *testing.T, got any) {
				if r := got.(postResult); r.created || r.post == nil {
					t.Errorf("created=%v post=%+v", r.created, r.post)
				}
			}},
		{name: "GetPost", route: "GET /v1/posts/{id}", wantPath: "/v1/posts/P", resp: postEnv,
			call: func(c *api.Client) (any, error) { return c.GetPost(ctx, "P") }},
		{name: "GetPostContext", route: "GET /v1/posts/{id}/context", wantPath: "/v1/posts/P/context", resp: sample(t, "PostContext"),
			call: func(c *api.Client) (any, error) { return c.GetPostContext(ctx, "P") },
			check: func(t *testing.T, got any) {
				if pc := got.(*api.PostContext); pc.Post.ID == "" || len(pc.Ancestors) == 0 {
					t.Errorf("context = %+v", pc)
				}
			}},
		{name: "ListReplies", route: "GET /v1/posts/{id}/replies", wantPath: "/v1/posts/P/replies", resp: pageOf(t, "Post"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListReplies(ctx, "P", pg) }},
		{name: "DeletePost", route: "DELETE /v1/posts/{id}", wantPath: "/v1/posts/P",
			call: func(c *api.Client) (any, error) { return err0(c.DeletePost(ctx, "P")) }},
		{name: "Like", route: "PUT /v1/posts/{id}/like", wantPath: "/v1/posts/P/like", resp: sample(t, "LikeResult"), limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return c.Like(ctx, "P") }},
		{name: "Unlike", route: "DELETE /v1/posts/{id}/like", wantPath: "/v1/posts/P/like", resp: `{"post_id":"P","liked":false,"like_count":3}`, limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return c.Unlike(ctx, "P") },
			check: func(t *testing.T, got any) {
				if r := got.(*api.LikeResult); r.PostID != "P" || r.Liked || r.LikeCount != 3 {
					t.Errorf("unlike = %+v", r)
				}
			}},
		{name: "ListLikes", route: "GET /v1/posts/{id}/likes", wantPath: "/v1/posts/P/likes", resp: pageOf(t, "UserSummary"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListLikes(ctx, "P", pg) }},
		{name: "Repost", route: "PUT /v1/posts/{id}/repost", wantPath: "/v1/posts/P/repost", resp: sample(t, "RepostResult"), limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return c.Repost(ctx, "P") }},
		{name: "Unrepost", route: "DELETE /v1/posts/{id}/repost", wantPath: "/v1/posts/P/repost", resp: `{"post_id":"P","reposted":false,"repost_count":1}`, limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return c.Unrepost(ctx, "P") },
			check: func(t *testing.T, got any) {
				if r := got.(*api.RepostResult); r.PostID != "P" || r.Reposted || r.RepostCount != 1 {
					t.Errorf("unrepost = %+v", r)
				}
			}},
		{name: "ListReposts", route: "GET /v1/posts/{id}/reposts", wantPath: "/v1/posts/P/reposts", resp: pageOf(t, "UserSummary"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListReposts(ctx, "P", pg) }},
		{name: "ListQuotes", route: "GET /v1/posts/{id}/quotes", wantPath: "/v1/posts/P/quotes", resp: pageOf(t, "Post"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListQuotes(ctx, "P", pg) }},

		// 5.16 timelines / hashtags / trends
		{name: "FollowingTimeline", route: "GET /v1/timeline/following", resp: `{"items":[],"next_cursor":null,"gap":true}`,
			call: func(c *api.Client) (any, error) {
				return c.FollowingTimeline(ctx, api.TimelineQuery{Since: "I", PageOpts: api.PageOpts{Limit: 30}})
			},
			wantQuery: "since=I&limit=30",
			check: func(t *testing.T, got any) {
				p := got.(*api.TimelinePage)
				if !p.Gap || p.Items == nil || len(p.Items) != 0 || p.NextCursor != nil {
					t.Errorf("page = %+v, want gap with empty non-nil items", p)
				}
			}},
		{name: "FollowingTimeline/page", route: "GET /v1/timeline/following", resp: sample(t, "TimelinePage"),
			call:      func(c *api.Client) (any, error) { return c.FollowingTimeline(ctx, api.TimelineQuery{PageOpts: pg}) },
			wantQuery: pgQuery},
		{name: "DiscoverTimeline", route: "GET /v1/timeline/discover", resp: sample(t, "TimelinePage"),
			call:      func(c *api.Client) (any, error) { return c.DiscoverTimeline(ctx, api.TimelineQuery{Since: "I"}) },
			wantQuery: "since=I"},
		{name: "HashtagPosts", route: "GET /v1/hashtags/{tag}/posts", wantPath: "/v1/hashtags/%EC%B2%AB%EB%88%88/posts", resp: pageOf(t, "Post"),
			call: func(c *api.Client) (any, error) { return c.HashtagPosts(ctx, "첫눈", api.PageOpts{}) }},
		{name: "HashtagPosts/hash", route: "GET /v1/hashtags/{tag}/posts", wantPath: "/v1/hashtags/%EC%B2%AB%EB%88%88/posts", resp: pageOf(t, "Post"),
			call:      func(c *api.Client) (any, error) { return c.HashtagPosts(ctx, "#첫눈", pg) },
			wantQuery: pgQuery},
		{name: "Trends", route: "GET /v1/trends", resp: sample(t, "Trends"),
			call: func(c *api.Client) (any, error) { return c.Trends(ctx) }},

		// 5.17 stories
		{name: "CreateStory", route: "POST /v1/stories", status: 201, resp: env(t, "story", "Story"), limiter: "RL_POST",
			call: func(c *api.Client) (any, error) {
				return c.CreateStory(ctx, api.CreateStoryRequest{Body: "출근 중", BG: "accent2"})
			},
			wantBody: `{"body":"출근 중","bg":"accent2"}`},
		{name: "CreateStory/media", route: "POST /v1/stories", status: 201, resp: env(t, "story", "Story"), limiter: "RL_POST",
			call: func(c *api.Client) (any, error) {
				return c.CreateStory(ctx, api.CreateStoryRequest{MediaUploadID: "U"})
			},
			wantBody: `{"media_upload_id":"U"}`},
		{name: "StoryFeed", route: "GET /v1/stories/feed", resp: itemsOf(t, "StoryGroup"),
			call: func(c *api.Client) (any, error) { return c.StoryFeed(ctx) }},
		{name: "ListUserStories", route: "GET /v1/users/{user}/stories", wantPath: "/v1/users/@minji/stories", resp: itemsOf(t, "Story"),
			call: func(c *api.Client) (any, error) { return c.ListUserStories(ctx, api.Handle("minji")) }},
		{name: "ViewStory", route: "POST /v1/stories/{id}/view", wantPath: "/v1/stories/ST/view",
			call: func(c *api.Client) (any, error) { return err0(c.ViewStory(ctx, "ST")) }},
		{name: "ListStoryViewers", route: "GET /v1/stories/{id}/viewers", wantPath: "/v1/stories/ST/viewers", resp: pageOf(t, "StoryViewer"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListStoryViewers(ctx, "ST", pg) }},
		{name: "DeleteStory", route: "DELETE /v1/stories/{id}", wantPath: "/v1/stories/ST",
			call: func(c *api.Client) (any, error) { return err0(c.DeleteStory(ctx, "ST")) }},

		// 5.18 notifications
		{name: "ListNotifications", route: "GET /v1/notifications", resp: sample(t, "NotificationPage"),
			call: func(c *api.Client) (any, error) {
				return c.ListNotifications(ctx, api.NotificationQuery{PageOpts: pg, UnreadOnly: true})
			},
			wantQuery: pgQuery + "&unread_only=true",
			check: func(t *testing.T, got any) {
				if p := got.(*api.NotificationPage); len(p.Items) == 0 || p.UnreadCount == 0 {
					t.Errorf("page = %+v", p)
				}
			}},
		{name: "ListNotifications/all", route: "GET /v1/notifications", resp: sample(t, "NotificationPage"),
			call: func(c *api.Client) (any, error) { return c.ListNotifications(ctx, api.NotificationQuery{}) }},
		{name: "NotificationUnreadCount", route: "GET /v1/notifications/unread-count", resp: `{"unread_count":7}`,
			call:  func(c *api.Client) (any, error) { return c.NotificationUnreadCount(ctx) },
			check: checkInt(7)},
		{name: "MarkNotificationsRead", route: "POST /v1/notifications/read", resp: `{"unread_count":0}`,
			call: func(c *api.Client) (any, error) {
				return c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{All: true})
			},
			wantBody: `{"all":true}`, check: checkInt(0)},
		{name: "MarkNotificationsRead/ids", route: "POST /v1/notifications/read", resp: `{"unread_count":2}`,
			call: func(c *api.Client) (any, error) {
				return c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{IDs: []string{"N1", "N2"}})
			},
			wantBody: `{"ids":["N1","N2"]}`, check: checkInt(2)},
		{name: "MarkNotificationsRead/before", route: "POST /v1/notifications/read", resp: `{"unread_count":1}`,
			call: func(c *api.Client) (any, error) {
				return c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{Before: "N9"})
			},
			wantBody: `{"before":"N9"}`, check: checkInt(1)},

		// 5.19 polls
		{name: "GetPoll", route: "GET /v1/polls/{id}", wantPath: "/v1/polls/PL", resp: pollEnv,
			call: func(c *api.Client) (any, error) { return c.GetPoll(ctx, "PL") }},
		{name: "Vote", route: "PUT /v1/polls/{id}/vote", wantPath: "/v1/polls/PL/vote", resp: pollEnv, limiter: "RL_REACT",
			call:     func(c *api.Client) (any, error) { return c.Vote(ctx, "PL", []int{0, 2}) },
			wantBody: `{"option_idxs":[0,2]}`},
		{name: "RetractVote", route: "DELETE /v1/polls/{id}/vote", wantPath: "/v1/polls/PL/vote", resp: pollEnv, limiter: "RL_REACT",
			call: func(c *api.Client) (any, error) { return c.RetractVote(ctx, "PL") }},
		{name: "ClosePoll", route: "POST /v1/polls/{id}/close", wantPath: "/v1/polls/PL/close", resp: pollEnv,
			call: func(c *api.Client) (any, error) { return c.ClosePoll(ctx, "PL") }},

		// 5.20 guestbook
		{name: "ListGuestbook", route: "GET /v1/users/{user}/guestbook", wantPath: "/v1/users/@minji/guestbook", resp: pageOf(t, "GuestbookEntry"), wantQuery: pgQuery,
			call: func(c *api.Client) (any, error) { return c.ListGuestbook(ctx, api.Handle("minji"), pg) }},
		{name: "WriteGuestbook", route: "POST /v1/users/{user}/guestbook", wantPath: "/v1/users/@minji/guestbook", status: 201,
			resp: env(t, "entry", "GuestbookEntry"), limiter: "RL_POST",
			call: func(c *api.Client) (any, error) {
				return c.WriteGuestbook(ctx, api.Handle("minji"), "놀러 왔어요", true)
			},
			wantBody: `{"body":"놀러 왔어요","is_secret":true}`},
		{name: "WriteGuestbook/public", route: "POST /v1/users/{user}/guestbook", wantPath: "/v1/users/@me/guestbook", status: 201,
			resp: env(t, "entry", "GuestbookEntry"), limiter: "RL_POST",
			call:     func(c *api.Client) (any, error) { return c.WriteGuestbook(ctx, api.MeRef, "메모", false) },
			wantBody: `{"body":"메모"}`},
		{name: "DeleteGuestbookEntry", route: "DELETE /v1/guestbook/{id}", wantPath: "/v1/guestbook/G",
			call: func(c *api.Client) (any, error) { return err0(c.DeleteGuestbookEntry(ctx, "G")) }},

		// 5.21 reports
		{name: "Report", route: "POST /v1/reports", status: 201, resp: `{"report_id":"R"}`, limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.Report(ctx, api.ReportRequest{TargetType: "post", TargetID: "P", Reason: "spam", Note: "광고"})
			},
			wantBody: `{"target_type":"post","target_id":"P","reason":"spam","note":"광고"}`,
			check: func(t *testing.T, got any) {
				if got.(string) != "R" {
					t.Errorf("report id = %v, want R", got)
				}
			}},
		{name: "Report/noNote", route: "POST /v1/reports", status: 201, resp: `{"report_id":"R2"}`, limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.Report(ctx, api.ReportRequest{TargetType: "user", TargetID: "U", Reason: "harassment"})
			},
			wantBody: `{"target_type":"user","target_id":"U","reason":"harassment"}`},

		// 5.22 admin
		{name: "AdminListReports", route: "GET /v1/admin/reports", resp: pageOf(t, "Report"),
			call: func(c *api.Client) (any, error) {
				return c.AdminListReports(ctx, api.AdminReportQuery{Status: "open", TargetType: "post", PageOpts: pg})
			},
			wantQuery: "status=open&target_type=post&" + pgQuery},
		{name: "AdminListReports/default", route: "GET /v1/admin/reports", resp: pageOf(t, "Report"),
			call: func(c *api.Client) (any, error) { return c.AdminListReports(ctx, api.AdminReportQuery{}) }},
		{name: "AdminGetReport", route: "GET /v1/admin/reports/{id}", wantPath: "/v1/admin/reports/R", resp: env(t, "report", "Report"),
			call: func(c *api.Client) (any, error) { return c.AdminGetReport(ctx, "R") }},
		{name: "AdminResolveReport", route: "POST /v1/admin/reports/{id}/resolve", wantPath: "/v1/admin/reports/R/resolve", resp: env(t, "report", "Report"),
			call:     func(c *api.Client) (any, error) { return c.AdminResolveReport(ctx, "R", "no_action", "문제 없음") },
			wantBody: `{"resolution":"no_action","note":"문제 없음"}`},
		{name: "AdminResolveReport/noNote", route: "POST /v1/admin/reports/{id}/resolve", wantPath: "/v1/admin/reports/R/resolve", resp: env(t, "report", "Report"),
			call:     func(c *api.Client) (any, error) { return c.AdminResolveReport(ctx, "R", "content_removed", "") },
			wantBody: `{"resolution":"content_removed"}`},
		{name: "AdminRemoveContent", route: "POST /v1/admin/content/remove", resp: env(t, "action", "AdminAction"),
			call: func(c *api.Client) (any, error) {
				return c.AdminRemoveContent(ctx, api.AdminRemoveRequest{TargetType: "post", TargetID: "P", Reason: "spam", ReportID: "R"})
			},
			wantBody: `{"target_type":"post","target_id":"P","reason":"spam","report_id":"R"}`},
		{name: "AdminRemoveContent/noReport", route: "POST /v1/admin/content/remove", resp: env(t, "action", "AdminAction"),
			call: func(c *api.Client) (any, error) {
				return c.AdminRemoveContent(ctx, api.AdminRemoveRequest{TargetType: "upload", TargetID: "U", Reason: "illegal"})
			},
			wantBody: `{"target_type":"upload","target_id":"U","reason":"illegal"}`},
		{name: "AdminSuspend", route: "POST /v1/admin/users/{user}/suspend", wantPath: "/v1/admin/users/@spammer/suspend", resp: env(t, "user", "AdminUserView"),
			call: func(c *api.Client) (any, error) {
				return c.AdminSuspend(ctx, api.Handle("spammer"), api.AdminSuspendRequest{Reason: "spam", DurationSec: api.Some(604800)})
			},
			wantBody: `{"reason":"spam","duration_sec":604800}`},
		{name: "AdminSuspend/forever", route: "POST /v1/admin/users/{user}/suspend", wantPath: "/v1/admin/users/01JA/suspend", resp: env(t, "user", "AdminUserView"),
			call: func(c *api.Client) (any, error) {
				return c.AdminSuspend(ctx, api.UserID("01JA"), api.AdminSuspendRequest{Reason: "abuse", DurationSec: api.Null[int](), ReportID: "R"})
			},
			wantBody: `{"reason":"abuse","duration_sec":null,"report_id":"R"}`},
		{name: "AdminUnsuspend", route: "POST /v1/admin/users/{user}/unsuspend", wantPath: "/v1/admin/users/@spammer/unsuspend", resp: env(t, "user", "AdminUserView"),
			call: func(c *api.Client) (any, error) { return c.AdminUnsuspend(ctx, api.Handle("spammer")) }},
		{name: "AdminListUsers", route: "GET /v1/admin/users", resp: pageOf(t, "AdminUserView"),
			call: func(c *api.Client) (any, error) {
				return c.AdminListUsers(ctx, api.AdminUserQuery{Q: "kim", Suspended: &yes})
			},
			wantQuery: "q=kim&suspended=true"},
		{name: "AdminListUsers/notSuspended", route: "GET /v1/admin/users", resp: pageOf(t, "AdminUserView"),
			call: func(c *api.Client) (any, error) {
				no := false
				return c.AdminListUsers(ctx, api.AdminUserQuery{Suspended: &no, PageOpts: pg})
			},
			wantQuery: "suspended=false&" + pgQuery},
		{name: "AdminListActions", route: "GET /v1/admin/actions", resp: pageOf(t, "AdminAction"),
			call: func(c *api.Client) (any, error) {
				return c.AdminListActions(ctx, api.AdminActionQuery{TargetType: "post", TargetID: "P", PageOpts: pg})
			},
			wantQuery: "target_type=post&target_id=P&" + pgQuery},
		{name: "AdminSetLeetDaily", route: "PUT /v1/admin/leet/daily/{date}", wantPath: "/v1/admin/leet/daily/2026-11-05", resp: env(t, "daily", "LeetDaily"),
			call: func(c *api.Client) (any, error) {
				return c.AdminSetLeetDaily(ctx, "2026-11-05", api.LeetDailyFacts{FrontendID: "1606", Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy"})
			},
			wantBody: `{"frontend_id":"1606","slug":"two-sum","title":"Two Sum","difficulty":"Easy","tags":[],"is_paid_only":false}`,
			check: func(t *testing.T, got any) {
				if d := got.(*api.LeetDaily); d.Date == "" || d.Slug == "" {
					t.Errorf("daily = %+v", d)
				}
			}},
	}
}

func TestEndpointsB(t *testing.T) {
	cases := endpointsBCases(t)
	checkCoverage(t, endpointsB, cases)
	runCases(t, cases)
}

// TestEndpointsBClientValidation: argument combinations the server would
// reject are a client-side 400 invalid_request with no request sent.
func TestEndpointsBClientValidation(t *testing.T) {
	ids101 := make([]string, 101)
	for i := range ids101 {
		ids101[i] = "N"
	}
	for name, call := range map[string]func(c *api.Client) error{
		"ListMessages/beforeAfter": func(c *api.Client) error {
			_, err := c.ListMessages(ctx, "C", api.MessageQuery{Before: "B", After: "A"})
			return err
		},
		"ListMessages/allThree": func(c *api.Client) error {
			_, err := c.ListMessages(ctx, "C", api.MessageQuery{Before: "B", After: "A", Around: "M"})
			return err
		},
		"ListMessages/afterAround": func(c *api.Client) error {
			_, err := c.ListMessages(ctx, "C", api.MessageQuery{After: "A", Around: "M"})
			return err
		},
		"GetThread/beforeAfter": func(c *api.Client) error {
			_, err := c.GetThread(ctx, "R", api.ThreadQuery{Before: "B", After: "A"})
			return err
		},
		"MarkNotificationsRead/none": func(c *api.Client) error {
			_, err := c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{})
			return err
		},
		"MarkNotificationsRead/idsAndAll": func(c *api.Client) error {
			_, err := c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{IDs: []string{"N"}, All: true})
			return err
		},
		"MarkNotificationsRead/allAndBefore": func(c *api.Client) error {
			_, err := c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{All: true, Before: "N"})
			return err
		},
		"MarkNotificationsRead/101ids": func(c *api.Client) error {
			_, err := c.MarkNotificationsRead(ctx, api.MarkNotificationsRead{IDs: ids101})
			return err
		},
		"Upload/noSize": func(c *api.Client) error {
			_, err := c.Upload(ctx, "post", "image/png", strings.NewReader("x"), 0)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := apitest.New(t)
			e := asError(t, call(s.Client()))
			if e.Status != 400 || e.Code != "invalid_request" {
				t.Errorf("err = %+v, want 400 invalid_request", e)
			}
			if n := len(s.Requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
		})
	}
}

// TestFetchMediaAbsoluteURL: FetchMedia accepts an absolute URL on the API
// origin (token sent) and never sends the token to another origin.
func TestFetchMediaAbsoluteURL(t *testing.T) {
	s := apitest.New(t)
	var auth string
	s.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("ETag", "e3")
		w.Write([]byte("GIF89a"))
	})
	c := s.Client()
	b, err := c.FetchMedia(ctx, s.URL+"/v1/media/U", "")
	if err != nil {
		t.Fatalf("FetchMedia: %v", err)
	}
	if string(b.Data) != "GIF89a" || b.ContentType != "image/gif" || b.ETag != "e3" || b.NotModified {
		t.Errorf("blob = %+v", b)
	}
	if auth != "Bearer msl_test" {
		t.Errorf("same-origin Authorization = %q", auth)
	}
	if got := s.Requests(); len(got) != 1 || got[0].Path != "/v1/media/U" {
		t.Errorf("requests = %+v", got)
	}
	// MediaURL output round-trips.
	if _, err := c.FetchMedia(ctx, c.MediaURL("/v1/media/U"), ""); err != nil {
		t.Fatalf("FetchMedia(MediaURL): %v", err)
	}

	other := apitest.New(t)
	var otherAuth = "unset"
	other.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		otherAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("x"))
	})
	if _, err := c.FetchMedia(ctx, other.URL+"/v1/media/U", ""); err != nil {
		t.Fatalf("FetchMedia(other origin): %v", err)
	}
	if otherAuth != "" {
		t.Errorf("token sent to a foreign origin: %q", otherAuth)
	}
}

// TestFetchMediaErrors: a non-2xx/304 answer is the usual *Error envelope.
func TestFetchMediaErrors(t *testing.T) {
	s := apitest.New(t)
	s.Error("GET /v1/media/{id}", http.StatusNotFound, "not_found")
	_, err := s.Client().FetchMedia(ctx, "/v1/media/U", "e1")
	if e := asError(t, err); e.Status != 404 || e.Code != "not_found" {
		t.Errorf("err = %+v", e)
	}
}

// TestReplyThreadRetriesOnceWithSameNonce mirrors the SendMessage retry rule
// (CA 4.3.1: only SendMessage/ReplyThread auto-retry once).
func TestReplyThreadRetriesOnceWithSameNonce(t *testing.T) {
	s := apitest.New(t)
	n := 0
	s.Handle("POST /v1/messages/{id}/thread", func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow","details":{"limiter":"RL_MESSAGE","retry_after_sec":0.01}}}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"message":{"id":"M2"}}`))
	})
	m, created, err := s.Client().ReplyThread(ctx, "R", api.ThreadReplyRequest{Body: "답"})
	if err != nil || !created || m.ID != "M2" {
		t.Fatalf("ReplyThread = %+v, %v, %v", m, created, err)
	}
	reqs := s.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if !bytes.Equal(reqs[0].Body, reqs[1].Body) || !bytes.Contains(reqs[0].Body, []byte(`"client_nonce":"`)) || bytes.Contains(reqs[0].Body, []byte(`"client_nonce":""`)) {
		t.Errorf("retry bodies differ or lack a nonce: %s vs %s", reqs[0].Body, reqs[1].Body)
	}
}

// TestNonRetryingEndpointsDoNotRetry: EditMessage and CreatePost are not on
// the auto-retry list.
func TestNonRetryingEndpointsDoNotRetry(t *testing.T) {
	s := apitest.New(t)
	for _, route := range []string{"PATCH /v1/messages/{id}", "POST /v1/posts"} {
		s.Handle(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow","details":{"retry_after_sec":0.01}}}`))
		})
	}
	c := s.Client()
	if _, err := c.EditMessage(ctx, "M", "x"); !api.IsCode(err, "rate_limited") {
		t.Fatalf("EditMessage err = %v", err)
	}
	if _, _, err := c.CreatePost(ctx, api.CreatePostRequest{Body: "x"}); !api.IsCode(err, "rate_limited") {
		t.Fatalf("CreatePost err = %v", err)
	}
	if n := len(s.Requests()); n != 2 {
		t.Errorf("requests = %d, want 2 (no retries)", n)
	}
}
