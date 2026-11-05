package fixtures

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
)

// Users search limits (api.md 5.2: default 10, max 20).
const (
	searchDefaultLimit = 10
	searchMaxLimit     = 20
)

// RegisterAPI registers canned responses for the above (normal + hostile).
//
// It registers EXACTLY these 17 GET patterns (plan 03 Task 23 Step 3; other plans
// register on top — a later registration of the same shape wins, and a concrete
// path beats a wildcard):
//
//	GET /v1/me, /v1/spaces, /v1/spaces/{space_id}, /v1/dms, /v1/me/unreads,
//	/v1/notifications/unread-count, /v1/friends, /v1/friends/requests,
//	/v1/me/direct-invites, /v1/blocks (store bootstrap, events.md 5.3 step 1),
//	/v1/channels/{channel_id}/messages, /v1/messages/{message_id}/thread,
//	/v1/timeline/following, /v1/timeline/discover, /v1/notifications,
//	/v1/users/search, /v1/meta.
//
// Bootstrap answers are read from a Store filled by Seed + Hostile, so a bootstrap
// against this server reproduces that Store's core entities. No catch-all, write
// method, /v1/presence or member list is registered.
func RegisterAPI(srv *apitest.Server) {
	s := store.New()
	Seed(s)
	Hostile(s)

	srv.JSON("GET /v1/me", http.StatusOK, map[string]any{"user": s.Me})

	spaces := make([]api.Space, 0, len(s.SpaceOrder))
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
	srv.JSON("GET /v1/spaces", http.StatusOK, items(spaces))
	srv.Handle("GET /v1/spaces/{space_id}", func(w http.ResponseWriter, r *http.Request) {
		sd, ok := details[r.PathValue("space_id")]
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, map[string]any{"space": sd})
	})

	dms := make([]api.Channel, 0, len(s.DMOrder))
	for _, id := range s.DMOrder {
		dms = append(dms, *s.Channels[id])
	}
	srv.JSON("GET /v1/dms", http.StatusOK, page(dms)) // cursor ignored: one full page

	unreads := []api.UnreadState{}
	for _, ch := range s.Channels {
		if ch.My.UnreadCount > 0 || ch.My.MentionCount > 0 {
			unreads = append(unreads, api.UnreadState{
				ChannelID:         ch.ID,
				UnreadCount:       ch.My.UnreadCount,
				MentionCount:      ch.My.MentionCount,
				LastMessageID:     ch.LastMessageID,
				LastReadMessageID: ch.My.LastReadMessageID,
			})
		}
	}
	sort.Slice(unreads, func(i, j int) bool { return unreads[i].ChannelID < unreads[j].ChannelID })
	srv.JSON("GET /v1/me/unreads", http.StatusOK, items(unreads))
	srv.JSON("GET /v1/notifications/unread-count", http.StatusOK, map[string]int{"unread_count": s.NotifUnread})

	friends := make([]api.Friend, 0, len(s.Friends))
	for _, f := range s.Friends {
		friends = append(friends, f)
	}
	sort.Slice(friends, func(i, j int) bool { return friends[i].User.Handle < friends[j].User.Handle })
	srv.JSON("GET /v1/friends", http.StatusOK, page(friends))
	incoming := append([]api.FriendRequest{}, s.FriendReqIn...)
	srv.Handle("GET /v1/friends/requests", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("direction") == "outgoing" {
			writeJSON(w, items([]api.FriendRequest{}))
			return
		}
		writeJSON(w, items(incoming))
	})
	srv.JSON("GET /v1/me/direct-invites", http.StatusOK, items(append([]api.DirectInvite{}, s.DirectInvites...)))
	srv.JSON("GET /v1/blocks", http.StatusOK, items([]api.BlockEntry{}))

	srv.Handle("GET /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		ms := Messages(r.PathValue("channel_id")) // around/before/after/limit ignored: full history
		if ms == nil {
			ms = []api.Message{}
		}
		writeJSON(w, api.MessageWindow{Items: ms})
	})
	srv.Handle("GET /v1/messages/{message_id}/thread", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("message_id")
		root, replies, ok := threadOf(id)
		if !ok {
			root = api.Message{ID: id}
		}
		writeJSON(w, api.ThreadWindow{Root: root, Items: replies})
	})

	srv.JSON("GET /v1/timeline/following", http.StatusOK, api.TimelinePage{Items: Timeline()})
	srv.JSON("GET /v1/timeline/discover", http.StatusOK, api.TimelinePage{Items: append(Timeline(), HostileTimeline()...)})
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: Notifications(), UnreadCount: s.NotifUnread})

	people := make([]api.UserSummary, 0, len(s.Users))
	for _, u := range s.Users {
		people = append(people, *u)
	}
	sort.Slice(people, func(i, j int) bool { return people[i].Handle < people[j].Handle })
	srv.Handle("GET /v1/users/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, items(searchUsers(people, r.URL.Query().Get("q"), r.URL.Query().Get("limit"))))
	})

	srv.JSON("GET /v1/meta", http.StatusOK, api.MetaInfo{
		APIVersion:       1,
		ServerTime:       now,
		MinClientVersion: "0.1.0",
		Limits: api.MetaLimits{
			MessageMaxChars: 4000, PostMaxChars: 500, PostMaxMedia: 4, UploadMaxBytes: 5242880,
			GroupDMMaxMembers: 10, StoryTTLSec: 86400, PollMaxOptions: 10, ReactionsPerMessage: 20,
			WatchTopicsMax: 50,
		},
	})
}

// searchUsers is a case-insensitive substring match on handle or display name
// (suspended users excluded), at most limit (default 10, max 20) results.
func searchUsers(people []api.UserSummary, q, limitStr string) []api.UserSummary {
	out := []api.UserSummary{}
	q = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q), "@"))
	if q == "" {
		return out
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = searchDefaultLimit
	}
	limit = min(limit, searchMaxLimit)
	for _, u := range people {
		if len(out) == limit {
			break
		}
		if !u.Suspended && (strings.Contains(strings.ToLower(u.Handle), q) || strings.Contains(strings.ToLower(u.DisplayName), q)) {
			out = append(out, u)
		}
	}
	return out
}

func items[T any](v []T) map[string]any {
	if v == nil {
		v = []T{}
	}
	return map[string]any{"items": v}
}

func page[T any](v []T) map[string]any {
	if v == nil {
		v = []T{}
	}
	return map[string]any{"items": v, "next_cursor": nil}
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

func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"error":{"code":"not_found","message":"not_found","details":{}}}`))
}
