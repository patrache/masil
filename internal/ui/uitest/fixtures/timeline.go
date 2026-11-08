package fixtures

import (
	"time"

	"github.com/patrache/masil/internal/api"
)

// postSam is sam's own post that hana liked (notification target, not in Timeline).
var postSam = postID(1)

func postItem(p api.Post) api.TimelineItem {
	return api.TimelineItem{ID: p.ID, Type: "post", Post: p, At: p.CreatedAt}
}

func photo(n, w, h int) api.Media {
	id := mediaID(n)
	return api.Media{ID: id, URL: "/v1/media/" + id, ContentType: "image/png", Size: 4096, Width: ptr(w), Height: ptr(h)}
}

// Timeline returns canned feed items, newest first.
func Timeline() []api.TimelineItem {
	snow := api.Post{
		ID:          postID(3),
		Author:      hana(),
		Body:        "오늘 첫 눈 ❄️ 사무실 창밖",
		Media:       []api.Media{photo(1, 64, 48)},
		Hashtags:    []string{},
		Mentions:    []api.MentionRef{},
		LikeCount:   12,
		RepostCount: 3,
		ReplyCount:  4,
		Viewer:      api.PostViewer{Liked: true},
		CreatedAt:   at(2, 2, 0), // 3분 전
	}
	bubble := api.Post{
		ID:         postID(2),
		Author:     devKim(),
		Body:       "bubbletea v2 써보신 분? #golang",
		Media:      []api.Media{},
		Hashtags:   []string{"golang"},
		Mentions:   []api.MentionRef{},
		LikeCount:  5,
		ReplyCount: 2,
		CreatedAt:  at(1, 55, 0), // 10분 전
	}
	return []api.TimelineItem{postItem(snow), postItem(bubble)}
}

// Notifications returns canned notifications, newest first (both unread).
func Notifications() []api.Notification {
	return []api.Notification{
		{
			ID:        notifID(2),
			Type:      "post_like",
			Actor:     ptr(hana()),
			Subject:   api.NotificationSubject{Type: "post", ID: postSam},
			Preview:   "주말에 캠핑 다녀왔어요 ⛺",
			Context:   api.NotificationContext{PostID: ptr(postSam)},
			CreatedAt: at(2, 3, 0),
		},
		{
			ID:      notifID(1),
			Type:    "message_mention",
			Actor:   ptr(minji()),
			Subject: api.NotificationSubject{Type: "message", ID: msgChat(1)},
			Preview: "@sam 이따 점심 같이 가요",
			Context: api.NotificationContext{
				ChannelID: ptr(ChChat),
				MessageID: ptr(msgChat(1)),
				SpaceID:   ptr(SpaceNova),
				SpaceName: ptr("노바 개발방"),
			},
			CreatedAt: time.Date(2026, 11, 5, 1, 30, 0, 0, time.UTC),
		},
	}
}
