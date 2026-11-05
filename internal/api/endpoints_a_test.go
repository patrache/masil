package api_test

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

// endpointsA is every CA 4.3.3 method from "4 meta" through "5.9 channels".
var endpointsA = []string{
	// 4 meta
	"Meta", "Health",
	// 1.4 auth
	"Signup", "Login", "Logout", "ListSessions", "RevokeSession", "ChangePassword",
	// 5.1 me
	"GetMe", "UpdateProfile", "GetSettings", "UpdateSettings", "SetStatus", "ClearStatus",
	"SetPresenceMode", "GetPresence", "StartFocus", "GetFocus", "EndFocus", "ListUnreads",
	// 5.2 users
	"GetUser", "GetUsers", "SearchUsers", "ListUserPosts", "ListFollowers", "ListFollowing",
	// 5.3 friends
	"ListFriends", "RemoveFriend", "ListFriendRequests", "SendFriendRequest",
	"AcceptFriendRequest", "DeclineFriendRequest", "CancelFriendRequest",
	// 5.4 blocks
	"ListBlocks", "Block", "Unblock",
	// 5.5 follow
	"Follow", "Unfollow", "RemoveFollower", "ListFollowRequests", "AcceptFollowRequest", "DeclineFollowRequest",
	// 5.6 DMs / channel members
	"ListDMs", "OpenDM", "AddChannelMembers", "RemoveChannelMember",
	// 5.7 spaces
	"ListSpaces", "CreateSpace", "GetSpace", "UpdateSpace", "DeleteSpace", "TransferSpace",
	"ListSpaceMembers", "SetSpaceMemberRole", "RemoveSpaceMember", "ListSpaceBans", "Unban",
	// 5.8 invites
	"ListInvites", "CreateInvite", "RevokeInvite", "GetInvite", "AcceptInvite", "SendDirectInvite",
	"ListMyDirectInvites", "AcceptDirectInvite", "DeclineDirectInvite", "CancelDirectInvite",
	// 5.9 channels
	"ListChannels", "CreateChannel", "GetChannel", "UpdateChannel", "DeleteChannel",
	"ListChannelMembers", "SetChannelPrefs", "MarkRead",
}

// err0 adapts an error-only method to the harness.
func err0(err error) (any, error) { return nil, err }

type openDMResult struct {
	ch      *api.Channel
	created bool
}

var pg = api.PageOpts{Cursor: "c1", Limit: 20}

const pgQuery = "cursor=c1&limit=20"

func endpointsACases(t *testing.T) []epCase {
	authResult := sample(t, "AuthResult")
	friendJSON := sample(t, "Friend")
	friendReqJSON := sample(t, "FriendRequest")
	return []epCase{
		// 4 meta
		{name: "Meta", route: "GET /v1/meta", resp: sample(t, "MetaInfo"), noAuth: true,
			call: func(c *api.Client) (any, error) { return c.Meta(ctx) },
			check: func(t *testing.T, got any) {
				m := got.(*api.MetaInfo)
				if m.APIVersion == 0 || m.Limits.MessageMaxChars == 0 || m.MinClientVersion == "" {
					t.Errorf("meta = %+v", m)
				}
			}},
		{name: "Health", route: "GET /v1/health", resp: `{"ok":true}`, noAuth: true,
			call:  func(c *api.Client) (any, error) { return err0(c.Health(ctx)) },
			check: func(t *testing.T, got any) {}},

		// 1.4 auth
		{name: "Signup", route: "POST /v1/auth/signup", status: 201, resp: authResult, noAuth: true, limiter: "RL_SIGNUP",
			call: func(c *api.Client) (any, error) {
				return c.Signup(ctx, api.SignupRequest{Handle: "minji", Password: "correct horse battery", DisplayName: "민지", Client: "tui", DeviceName: "mbp"})
			},
			wantBody: `{"handle":"minji","password":"correct horse battery","display_name":"민지","client":"tui","device_name":"mbp"}`,
			check: func(t *testing.T, got any) {
				r := got.(*api.AuthResult)
				if r.Token == "" || r.Session.ID == "" || r.User.ID == "" {
					t.Errorf("auth result = %+v", r)
				}
			}},
		{name: "Signup/minimal", route: "POST /v1/auth/signup", status: 201, resp: authResult, noAuth: true, limiter: "RL_SIGNUP",
			call: func(c *api.Client) (any, error) {
				return c.Signup(ctx, api.SignupRequest{Handle: "minji", Password: "correct horse battery"})
			},
			wantBody: `{"handle":"minji","password":"correct horse battery"}`},
		{name: "Login", route: "POST /v1/auth/login", resp: authResult, noAuth: true, limiter: "RL_LOGIN",
			call: func(c *api.Client) (any, error) {
				return c.Login(ctx, api.LoginRequest{Handle: "minji", Password: "pw123456", Client: "tui", DeviceName: "mbp"})
			},
			wantBody: `{"handle":"minji","password":"pw123456","client":"tui","device_name":"mbp"}`},
		{name: "Logout", route: "POST /v1/auth/logout",
			call: func(c *api.Client) (any, error) { return err0(c.Logout(ctx)) }},
		{name: "ListSessions", route: "GET /v1/auth/sessions", resp: itemsOf(t, "Session"),
			call: func(c *api.Client) (any, error) { return c.ListSessions(ctx) }},
		{name: "RevokeSession", route: "DELETE /v1/auth/sessions/{id}", wantPath: "/v1/auth/sessions/S1",
			call: func(c *api.Client) (any, error) { return err0(c.RevokeSession(ctx, "S1")) }},
		{name: "ChangePassword", route: "POST /v1/auth/password",
			call:     func(c *api.Client) (any, error) { return err0(c.ChangePassword(ctx, "a", "b")) },
			wantBody: `{"current_password":"a","new_password":"b"}`},

		// 5.1 me
		{name: "GetMe", route: "GET /v1/me", resp: env(t, "user", "Me"),
			call: func(c *api.Client) (any, error) { return c.GetMe(ctx) }},
		{name: "UpdateProfile", route: "PATCH /v1/me/profile", resp: env(t, "user", "Me"),
			call: func(c *api.Client) (any, error) {
				return c.UpdateProfile(ctx, api.UpdateProfileRequest{DisplayName: api.Some("민지"), AvatarUploadID: api.Null[string]()})
			},
			wantBody: `{"display_name":"민지","avatar_upload_id":null}`},
		{name: "GetSettings", route: "GET /v1/me/settings", resp: env(t, "settings", "Settings"),
			call: func(c *api.Client) (any, error) { return c.GetSettings(ctx) }},
		{name: "UpdateSettings", route: "PATCH /v1/me/settings", resp: env(t, "settings", "Settings"),
			call: func(c *api.Client) (any, error) {
				return c.UpdateSettings(ctx, api.UpdateSettingsRequest{Notify: &api.NotifySettingsPatch{Social: api.Some(true)}, FocusDefaultMin: api.Some(30)})
			},
			wantBody: `{"notify":{"social":true},"focus_default_min":30}`},
		{name: "UpdateSettings/all", route: "PATCH /v1/me/settings", resp: env(t, "settings", "Settings"),
			call: func(c *api.Client) (any, error) {
				return c.UpdateSettings(ctx, api.UpdateSettingsRequest{
					DMPolicy: api.Some("friends_only"), IsPrivate: api.Some(false),
					Notify: &api.NotifySettingsPatch{DM: api.Some(true), Mention: api.Some(false), Invite: api.Some(true),
						FriendRequest: api.Some(false), ThreadReply: api.Some(true), Social: api.Some(false)},
				})
			},
			wantBody: `{"dm_policy":"friends_only","is_private":false,"notify":{"dm":true,"mention":false,"invite":true,"friend_request":false,"thread_reply":true,"social":false}}`},
		{name: "SetStatus", route: "PUT /v1/me/status", resp: env(t, "status", "UserStatus"),
			call: func(c *api.Client) (any, error) {
				return c.SetStatus(ctx, api.SetStatusRequest{Emoji: api.Null[string](), Text: "회의 중", ExpiresInSec: api.Some(3600)})
			},
			wantBody: `{"emoji":null,"text":"회의 중","expires_in_sec":3600}`},
		{name: "SetStatus/textOnly", route: "PUT /v1/me/status", resp: env(t, "status", "UserStatus"),
			call:     func(c *api.Client) (any, error) { return c.SetStatus(ctx, api.SetStatusRequest{Text: ""}) },
			wantBody: `{"text":""}`},
		{name: "ClearStatus", route: "DELETE /v1/me/status",
			call: func(c *api.Client) (any, error) { return err0(c.ClearStatus(ctx)) }},
		{name: "SetPresenceMode", route: "PUT /v1/me/presence", resp: env(t, "presence", "PresenceState"),
			call:     func(c *api.Client) (any, error) { return c.SetPresenceMode(ctx, "dnd") },
			wantBody: `{"mode":"dnd"}`},
		{name: "GetPresence", route: "GET /v1/presence", resp: itemsOf(t, "PresenceItem"),
			call:      func(c *api.Client) (any, error) { return c.GetPresence(ctx, []string{"A", "B"}) },
			wantQuery: "user_ids=A%2CB"},
		{name: "StartFocus", route: "POST /v1/me/focus", resp: env(t, "focus", "Focus"),
			call:     func(c *api.Client) (any, error) { return c.StartFocus(ctx, api.Some(1500)) },
			wantBody: `{"duration_sec":1500}`},
		{name: "StartFocus/default", route: "POST /v1/me/focus", resp: env(t, "focus", "Focus"),
			call:     func(c *api.Client) (any, error) { return c.StartFocus(ctx, api.Opt[int]{}) },
			wantBody: `{}`},
		{name: "GetFocus", route: "GET /v1/me/focus", resp: env(t, "focus", "Focus"),
			call: func(c *api.Client) (any, error) { return c.GetFocus(ctx) }},
		{name: "GetFocus/null", route: "GET /v1/me/focus", resp: `{"focus":null}`,
			call: func(c *api.Client) (any, error) { return c.GetFocus(ctx) },
			check: func(t *testing.T, got any) {
				if got.(*api.Focus) != nil {
					t.Errorf("focus = %+v, want nil", got)
				}
			}},
		{name: "EndFocus", route: "DELETE /v1/me/focus", resp: env(t, "summary", "FocusSummary"),
			call: func(c *api.Client) (any, error) { return c.EndFocus(ctx) }},
		{name: "ListUnreads", route: "GET /v1/me/unreads", resp: itemsOf(t, "UnreadState"),
			call: func(c *api.Client) (any, error) { return c.ListUnreads(ctx) }},

		// 5.2 users
		{name: "GetUser", route: "GET /v1/users/{user}", wantPath: "/v1/users/@minji", resp: env(t, "user", "Profile"),
			call: func(c *api.Client) (any, error) { return c.GetUser(ctx, api.Handle("MinJi")) }},
		{name: "GetUser/id", route: "GET /v1/users/{user}", wantPath: "/v1/users/01JA0000000000000000000001", resp: env(t, "user", "Profile"),
			call: func(c *api.Client) (any, error) { return c.GetUser(ctx, api.UserID("01JA0000000000000000000001")) }},
		{name: "GetUsers", route: "GET /v1/users", resp: itemsOf(t, "UserSummary"),
			call:      func(c *api.Client) (any, error) { return c.GetUsers(ctx, []string{"A", "B", "C"}) },
			wantQuery: "ids=A%2CB%2CC"},
		{name: "SearchUsers", route: "GET /v1/users/search", resp: itemsOf(t, "UserSummary"), limiter: "RL_SEARCH",
			call:      func(c *api.Client) (any, error) { return c.SearchUsers(ctx, "민 지", 5) },
			wantQuery: "q=%EB%AF%BC+%EC%A7%80&limit=5"},
		{name: "SearchUsers/defaultLimit", route: "GET /v1/users/search", resp: itemsOf(t, "UserSummary"), limiter: "RL_SEARCH",
			call:      func(c *api.Client) (any, error) { return c.SearchUsers(ctx, "mi", 0) },
			wantQuery: "q=mi"},
		{name: "ListUserPosts", route: "GET /v1/users/{user}/posts", wantPath: "/v1/users/@minji/posts", resp: pageOf(t, "TimelineItem"),
			call:      func(c *api.Client) (any, error) { return c.ListUserPosts(ctx, api.Handle("minji"), "media", pg) },
			wantQuery: "filter=media&" + pgQuery,
			check: func(t *testing.T, got any) {
				p := got.(*api.Page[api.TimelineItem])
				if len(p.Items) != 1 || p.Done() || *p.NextCursor != "c2" {
					t.Errorf("page = %+v", p)
				}
			}},
		{name: "ListUserPosts/defaults", route: "GET /v1/users/{user}/posts", wantPath: "/v1/users/@me/posts", resp: pageOf(t, "TimelineItem"),
			call: func(c *api.Client) (any, error) {
				return c.ListUserPosts(ctx, api.MeRef, "", api.PageOpts{})
			}},
		{name: "ListFollowers", route: "GET /v1/users/{user}/followers", wantPath: "/v1/users/@minji/followers", resp: pageOf(t, "UserSummary"),
			call:      func(c *api.Client) (any, error) { return c.ListFollowers(ctx, api.Handle("minji"), pg) },
			wantQuery: pgQuery},
		{name: "ListFollowing", route: "GET /v1/users/{user}/following", wantPath: "/v1/users/@minji/following", resp: pageOf(t, "UserSummary"),
			call:      func(c *api.Client) (any, error) { return c.ListFollowing(ctx, api.Handle("minji"), pg) },
			wantQuery: pgQuery},

		// 5.3 friends
		{name: "ListFriends", route: "GET /v1/friends", resp: pageOf(t, "Friend"),
			call:      func(c *api.Client) (any, error) { return c.ListFriends(ctx, pg) },
			wantQuery: pgQuery},
		{name: "RemoveFriend", route: "DELETE /v1/friends/{user}", wantPath: "/v1/friends/@junho",
			call: func(c *api.Client) (any, error) { return err0(c.RemoveFriend(ctx, api.Handle("junho"))) }},
		{name: "ListFriendRequests", route: "GET /v1/friends/requests", resp: itemsOf(t, "FriendRequest"),
			call:      func(c *api.Client) (any, error) { return c.ListFriendRequests(ctx, "incoming") },
			wantQuery: "direction=incoming"},
		{name: "SendFriendRequest", route: "POST /v1/friends/requests", status: 200, limiter: "RL_SOCIAL",
			resp:     fmt.Sprintf(`{"request":%s,"friend":%s}`, friendReqJSON, friendJSON),
			call:     func(c *api.Client) (any, error) { return c.SendFriendRequest(ctx, api.Handle("junho")) },
			wantBody: `{"user":"@junho"}`,
			check: func(t *testing.T, got any) {
				r := got.(*api.FriendRequestResult)
				if !r.AutoAccepted || r.Friend == nil || r.Request.ID == "" {
					t.Errorf("auto-accept result = %+v", r)
				}
			}},
		{name: "SendFriendRequest/pending", route: "POST /v1/friends/requests", status: 201, limiter: "RL_SOCIAL",
			resp:     fmt.Sprintf(`{"request":%s,"friend":null}`, friendReqJSON),
			call:     func(c *api.Client) (any, error) { return c.SendFriendRequest(ctx, api.Handle("junho")) },
			wantBody: `{"user":"@junho"}`,
			check: func(t *testing.T, got any) {
				r := got.(*api.FriendRequestResult)
				if r.AutoAccepted || r.Friend != nil || r.Request.ID == "" {
					t.Errorf("pending result = %+v", r)
				}
			}},
		{name: "AcceptFriendRequest", route: "POST /v1/friends/requests/{id}/accept", wantPath: "/v1/friends/requests/R1/accept",
			resp: env(t, "friend", "Friend"),
			call: func(c *api.Client) (any, error) { return c.AcceptFriendRequest(ctx, "R1") }},
		{name: "DeclineFriendRequest", route: "POST /v1/friends/requests/{id}/decline", wantPath: "/v1/friends/requests/R1/decline",
			call: func(c *api.Client) (any, error) { return err0(c.DeclineFriendRequest(ctx, "R1")) }},
		{name: "CancelFriendRequest", route: "DELETE /v1/friends/requests/{id}", wantPath: "/v1/friends/requests/R1",
			call: func(c *api.Client) (any, error) { return err0(c.CancelFriendRequest(ctx, "R1")) }},

		// 5.4 blocks
		{name: "ListBlocks", route: "GET /v1/blocks", resp: itemsOf(t, "BlockEntry"),
			call: func(c *api.Client) (any, error) { return c.ListBlocks(ctx) }},
		{name: "Block", route: "PUT /v1/blocks/{user}", wantPath: "/v1/blocks/@spam", limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) { return err0(c.Block(ctx, api.Handle("spam"))) }},
		{name: "Unblock", route: "DELETE /v1/blocks/{user}", wantPath: "/v1/blocks/@spam",
			call: func(c *api.Client) (any, error) { return err0(c.Unblock(ctx, api.Handle("spam"))) }},

		// 5.5 follow
		{name: "Follow", route: "PUT /v1/users/{user}/follow", wantPath: "/v1/users/@hana/follow", resp: `{"state":"pending"}`, limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) { return c.Follow(ctx, api.Handle("hana")) },
			check: func(t *testing.T, got any) {
				if got.(string) != "pending" {
					t.Errorf("state = %q, want pending", got)
				}
			}},
		{name: "Unfollow", route: "DELETE /v1/users/{user}/follow", wantPath: "/v1/users/@hana/follow",
			call: func(c *api.Client) (any, error) { return err0(c.Unfollow(ctx, api.Handle("hana"))) }},
		{name: "RemoveFollower", route: "DELETE /v1/users/{user}/follower", wantPath: "/v1/users/@hana/follower",
			call: func(c *api.Client) (any, error) { return err0(c.RemoveFollower(ctx, api.Handle("hana"))) }},
		{name: "ListFollowRequests", route: "GET /v1/me/follow-requests", resp: pageOf(t, "FollowRequestEntry"),
			call:      func(c *api.Client) (any, error) { return c.ListFollowRequests(ctx, pg) },
			wantQuery: pgQuery},
		{name: "AcceptFollowRequest", route: "POST /v1/me/follow-requests/{user}/accept", wantPath: "/v1/me/follow-requests/@hana/accept",
			call: func(c *api.Client) (any, error) { return err0(c.AcceptFollowRequest(ctx, api.Handle("hana"))) }},
		{name: "DeclineFollowRequest", route: "POST /v1/me/follow-requests/{user}/decline", wantPath: "/v1/me/follow-requests/@hana/decline",
			call: func(c *api.Client) (any, error) { return err0(c.DeclineFollowRequest(ctx, api.Handle("hana"))) }},

		// 5.6 DMs / channel members
		{name: "ListDMs", route: "GET /v1/dms", resp: pageOf(t, "Channel"),
			call:      func(c *api.Client) (any, error) { return c.ListDMs(ctx, pg) },
			wantQuery: pgQuery},
		{name: "OpenDM", route: "POST /v1/dms", status: 200, resp: env(t, "channel", "Channel"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				ch, created, err := c.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle("junho")}})
				return openDMResult{ch, created}, err
			},
			wantBody: `{"users":["@junho"]}`,
			check: func(t *testing.T, got any) {
				r := got.(openDMResult)
				if r.created || r.ch == nil || r.ch.ID == "" {
					t.Errorf("existing DM: created=%v ch=%+v", r.created, r.ch)
				}
			}},
		{name: "OpenDM/group", route: "POST /v1/dms", status: 201, resp: env(t, "channel", "Channel"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				ch, created, err := c.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle("junho"), api.Handle("hana")}, Name: api.Some("점심팟")})
				return openDMResult{ch, created}, err
			},
			wantBody: `{"users":["@junho","@hana"],"name":"점심팟"}`,
			check: func(t *testing.T, got any) {
				r := got.(openDMResult)
				if !r.created || r.ch == nil || r.ch.ID == "" {
					t.Errorf("new DM: created=%v ch=%+v", r.created, r.ch)
				}
			}},
		{name: "AddChannelMembers", route: "POST /v1/channels/{id}/members", wantPath: "/v1/channels/C/members", resp: env(t, "channel", "Channel"),
			call: func(c *api.Client) (any, error) {
				return c.AddChannelMembers(ctx, "C", []api.UserRef{api.Handle("hana")})
			},
			wantBody: `{"users":["@hana"]}`},
		{name: "RemoveChannelMember", route: "DELETE /v1/channels/{id}/members/{user}", wantPath: "/v1/channels/C/members/@me",
			call: func(c *api.Client) (any, error) { return err0(c.RemoveChannelMember(ctx, "C", api.MeRef)) }},

		// 5.7 spaces
		{name: "ListSpaces", route: "GET /v1/spaces", resp: itemsOf(t, "Space"),
			call: func(c *api.Client) (any, error) { return c.ListSpaces(ctx) }},
		{name: "CreateSpace", route: "POST /v1/spaces", status: 201, resp: env(t, "space", "SpaceDetail"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.CreateSpace(ctx, api.CreateSpaceRequest{Name: "노바 개발방", IconEmoji: "🚀"})
			},
			wantBody: `{"name":"노바 개발방","icon_emoji":"🚀"}`},
		{name: "GetSpace", route: "GET /v1/spaces/{id}", wantPath: "/v1/spaces/S", resp: env(t, "space", "SpaceDetail"),
			call: func(c *api.Client) (any, error) { return c.GetSpace(ctx, "S") }},
		{name: "UpdateSpace", route: "PATCH /v1/spaces/{id}", wantPath: "/v1/spaces/S", resp: env(t, "space", "Space"),
			call: func(c *api.Client) (any, error) {
				return c.UpdateSpace(ctx, "S", api.UpdateSpaceRequest{Description: api.Some("설명"), IconEmoji: api.Null[string]()})
			},
			wantBody: `{"description":"설명","icon_emoji":null}`},
		{name: "DeleteSpace", route: "DELETE /v1/spaces/{id}", wantPath: "/v1/spaces/S",
			call: func(c *api.Client) (any, error) { return err0(c.DeleteSpace(ctx, "S")) }},
		{name: "TransferSpace", route: "POST /v1/spaces/{id}/transfer", wantPath: "/v1/spaces/S/transfer", resp: env(t, "space", "Space"),
			call:     func(c *api.Client) (any, error) { return c.TransferSpace(ctx, "S", api.Handle("hana")) },
			wantBody: `{"user":"@hana"}`},
		{name: "ListSpaceMembers", route: "GET /v1/spaces/{id}/members", wantPath: "/v1/spaces/S/members", resp: pageOf(t, "SpaceMember"),
			call: func(c *api.Client) (any, error) {
				return c.ListSpaceMembers(ctx, "S", api.SpaceMembersQuery{Q: "하나", Role: "admin", PageOpts: pg})
			},
			wantQuery: "q=%ED%95%98%EB%82%98&role=admin&" + pgQuery},
		{name: "ListSpaceMembers/defaults", route: "GET /v1/spaces/{id}/members", wantPath: "/v1/spaces/S/members", resp: pageOf(t, "SpaceMember"),
			call: func(c *api.Client) (any, error) { return c.ListSpaceMembers(ctx, "S", api.SpaceMembersQuery{}) }},
		{name: "SetSpaceMemberRole", route: "PATCH /v1/spaces/{id}/members/{user}", wantPath: "/v1/spaces/S/members/@hana",
			resp: env(t, "member", "SpaceMember"),
			call: func(c *api.Client) (any, error) {
				return c.SetSpaceMemberRole(ctx, "S", api.Handle("hana"), api.RoleAdmin)
			},
			wantBody: `{"role":"admin"}`},
		{name: "RemoveSpaceMember", route: "DELETE /v1/spaces/{id}/members/{user}", wantPath: "/v1/spaces/S/members/@me",
			call: func(c *api.Client) (any, error) {
				return err0(c.RemoveSpaceMember(ctx, "S", api.MeRef, api.RemoveMemberOpts{Ban: true, Reason: "spam"}))
			},
			wantQuery: "ban=true&reason=spam"},
		{name: "RemoveSpaceMember/kick", route: "DELETE /v1/spaces/{id}/members/{user}", wantPath: "/v1/spaces/S/members/@hana",
			call: func(c *api.Client) (any, error) {
				return err0(c.RemoveSpaceMember(ctx, "S", api.Handle("hana"), api.RemoveMemberOpts{}))
			}},
		{name: "ListSpaceBans", route: "GET /v1/spaces/{id}/bans", wantPath: "/v1/spaces/S/bans", resp: itemsOf(t, "SpaceBan"),
			call: func(c *api.Client) (any, error) { return c.ListSpaceBans(ctx, "S") }},
		{name: "Unban", route: "DELETE /v1/spaces/{id}/bans/{user}", wantPath: "/v1/spaces/S/bans/@spam",
			call: func(c *api.Client) (any, error) { return err0(c.Unban(ctx, "S", api.Handle("spam"))) }},

		// 5.8 invites
		{name: "ListInvites", route: "GET /v1/spaces/{id}/invites", wantPath: "/v1/spaces/S/invites", resp: itemsOf(t, "Invite"),
			call: func(c *api.Client) (any, error) { return c.ListInvites(ctx, "S") }},
		{name: "CreateInvite", route: "POST /v1/spaces/{id}/invites", wantPath: "/v1/spaces/S/invites", status: 201,
			resp: env(t, "invite", "Invite"), limiter: "RL_SOCIAL",
			call:     func(c *api.Client) (any, error) { return c.CreateInvite(ctx, "S", api.CreateInviteRequest{}) },
			wantBody: `{}`},
		{name: "CreateInvite/limits", route: "POST /v1/spaces/{id}/invites", wantPath: "/v1/spaces/S/invites", status: 201,
			resp: env(t, "invite", "Invite"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.CreateInvite(ctx, "S", api.CreateInviteRequest{MaxUses: api.Some(5), ExpiresInSec: api.Null[int]()})
			},
			wantBody: `{"max_uses":5,"expires_in_sec":null}`},
		{name: "RevokeInvite", route: "DELETE /v1/invites/{code}", wantPath: "/v1/invites/AbC-12",
			call: func(c *api.Client) (any, error) { return err0(c.RevokeInvite(ctx, "AbC-12")) }},
		{name: "GetInvite", route: "GET /v1/invites/{code}", wantPath: "/v1/invites/AbC-12", resp: env(t, "invite", "InvitePreview"),
			call: func(c *api.Client) (any, error) { return c.GetInvite(ctx, "AbC-12") }},
		{name: "GetInvite/escaped", route: "GET /v1/invites/{code}", wantPath: "/v1/invites/a%2Fb%3Fc", resp: env(t, "invite", "InvitePreview"),
			call: func(c *api.Client) (any, error) { return c.GetInvite(ctx, "a/b?c") }},
		{name: "AcceptInvite", route: "POST /v1/invites/{code}/accept", wantPath: "/v1/invites/AbC-12/accept", resp: env(t, "space", "SpaceDetail"),
			call: func(c *api.Client) (any, error) { return c.AcceptInvite(ctx, "AbC-12") }},
		{name: "SendDirectInvite", route: "POST /v1/spaces/{id}/direct-invites", wantPath: "/v1/spaces/S/direct-invites", status: 201,
			resp: env(t, "invite", "DirectInvite"), limiter: "RL_SOCIAL",
			call:     func(c *api.Client) (any, error) { return c.SendDirectInvite(ctx, "S", api.Handle("hana")) },
			wantBody: `{"user":"@hana"}`},
		{name: "ListMyDirectInvites", route: "GET /v1/me/direct-invites", resp: itemsOf(t, "DirectInvite"),
			call: func(c *api.Client) (any, error) { return c.ListMyDirectInvites(ctx) }},
		{name: "AcceptDirectInvite", route: "POST /v1/direct-invites/{id}/accept", wantPath: "/v1/direct-invites/I1/accept",
			resp: env(t, "space", "SpaceDetail"),
			call: func(c *api.Client) (any, error) { return c.AcceptDirectInvite(ctx, "I1") }},
		{name: "DeclineDirectInvite", route: "POST /v1/direct-invites/{id}/decline", wantPath: "/v1/direct-invites/I1/decline",
			call: func(c *api.Client) (any, error) { return err0(c.DeclineDirectInvite(ctx, "I1")) }},
		{name: "CancelDirectInvite", route: "DELETE /v1/direct-invites/{id}", wantPath: "/v1/direct-invites/I1",
			call: func(c *api.Client) (any, error) { return err0(c.CancelDirectInvite(ctx, "I1")) }},

		// 5.9 channels
		{name: "ListChannels", route: "GET /v1/spaces/{id}/channels", wantPath: "/v1/spaces/S/channels", resp: itemsOf(t, "Channel"),
			call: func(c *api.Client) (any, error) { return c.ListChannels(ctx, "S") }},
		{name: "CreateChannel", route: "POST /v1/spaces/{id}/channels", wantPath: "/v1/spaces/S/channels", status: 201,
			resp: env(t, "channel", "Channel"),
			call: func(c *api.Client) (any, error) {
				return c.CreateChannel(ctx, "S", api.CreateChannelRequest{Name: "배포알림", IsPrivate: true, Members: []api.UserRef{api.Handle("hana")}})
			},
			wantBody: `{"name":"배포알림","is_private":true,"members":["@hana"]}`},
		{name: "CreateChannel/public", route: "POST /v1/spaces/{id}/channels", wantPath: "/v1/spaces/S/channels", status: 201,
			resp: env(t, "channel", "Channel"),
			call: func(c *api.Client) (any, error) {
				return c.CreateChannel(ctx, "S", api.CreateChannelRequest{Name: "잡담", Topic: "아무 얘기"})
			},
			wantBody: `{"name":"잡담","topic":"아무 얘기"}`},
		{name: "GetChannel", route: "GET /v1/channels/{id}", wantPath: "/v1/channels/C", resp: env(t, "channel", "Channel"),
			call: func(c *api.Client) (any, error) { return c.GetChannel(ctx, "C") }},
		{name: "UpdateChannel", route: "PATCH /v1/channels/{id}", wantPath: "/v1/channels/C", resp: env(t, "channel", "Channel"),
			call: func(c *api.Client) (any, error) {
				return c.UpdateChannel(ctx, "C", api.UpdateChannelRequest{Topic: api.Some(""), Position: api.Some(0)})
			},
			wantBody: `{"topic":"","position":0}`},
		{name: "DeleteChannel", route: "DELETE /v1/channels/{id}", wantPath: "/v1/channels/C",
			call: func(c *api.Client) (any, error) { return err0(c.DeleteChannel(ctx, "C")) }},
		{name: "ListChannelMembers", route: "GET /v1/channels/{id}/members", wantPath: "/v1/channels/C/members", resp: pageOf(t, "ChannelMember"),
			call:      func(c *api.Client) (any, error) { return c.ListChannelMembers(ctx, "C", pg) },
			wantQuery: pgQuery},
		{name: "SetChannelPrefs", route: "PUT /v1/channels/{id}/prefs", wantPath: "/v1/channels/C/prefs", resp: env(t, "my", "ChannelMyState"),
			call: func(c *api.Client) (any, error) {
				return c.SetChannelPrefs(ctx, "C", api.ChannelPrefsRequest{Notify: api.Some("mentions"), Muted: api.Some(false)})
			},
			wantBody: `{"notify":"mentions","muted":false}`},
		{name: "MarkRead", route: "PUT /v1/channels/C/read", resp: env(t, "state", "UnreadState"),
			call:     func(c *api.Client) (any, error) { return c.MarkRead(ctx, "C", "M") },
			wantBody: `{"message_id":"M"}`},
	}
}

func TestEndpointsA(t *testing.T) {
	cases := endpointsACases(t)
	checkCoverage(t, endpointsA, cases)
	runCases(t, cases)
}

// TestEndpointsAIDListLimits: GetPresence/GetUsers take at most 100 IDs
// (api.md 5.1/5.2, caller chunks); over the limit is a client-side
// invalid_request with no request sent, and an empty list needs no request.
func TestEndpointsAIDListLimits(t *testing.T) {
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("U%03d", i)
	}
	for name, call := range map[string]func(c *api.Client, ids []string) (int, error){
		"GetPresence": func(c *api.Client, ids []string) (int, error) {
			r, err := c.GetPresence(ctx, ids)
			return len(r), err
		},
		"GetUsers": func(c *api.Client, ids []string) (int, error) {
			r, err := c.GetUsers(ctx, ids)
			return len(r), err
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := apitest.New(t)
			c := s.Client()
			_, err := call(c, ids)
			e := asError(t, err)
			if e.Status != 400 || e.Code != "invalid_request" {
				t.Errorf("101 ids: err = %+v, want 400 invalid_request", e)
			}
			if n, err := call(c, nil); err != nil || n != 0 {
				t.Errorf("no ids: n=%d err=%v, want 0, nil", n, err)
			}
			if n := len(s.Requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
		})
	}
}
