package api_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

// badSegments are path values a URL normaliser rewrites before routing
// (pending-fixes 5): WHATWG URL parsing (Workers, Hono) and Cloudflare URL
// normalisation remove "." / ".." segments, and Cloudflare also merges the
// "//" an empty value leaves, so DELETE /v1/invites/.. would reach /v1/.
var badSegments = map[string]string{"empty": "", "dot": ".", "dotdot": ".."}

// pathCalls is every Client method that puts a caller value into the path,
// called with that value set to v (other path values are valid IDs).
var pathCalls = map[string]func(c *api.Client, v string) error{
	"AdminGetReport": func(c *api.Client, v string) error { _, err := c.AdminGetReport(ctx, v); return err },
	"AdminResolveReport": func(c *api.Client, v string) error {
		_, err := c.AdminResolveReport(ctx, v, "dismissed", "")
		return err
	},
	"AdminSuspend": func(c *api.Client, v string) error {
		_, err := c.AdminSuspend(ctx, api.UserRef(v), api.AdminSuspendRequest{})
		return err
	},
	"AdminUnsuspend": func(c *api.Client, v string) error { _, err := c.AdminUnsuspend(ctx, api.UserRef(v)); return err },
	"RevokeSession":  func(c *api.Client, v string) error { return c.RevokeSession(ctx, v) },
	"ListChannels":   func(c *api.Client, v string) error { _, err := c.ListChannels(ctx, v); return err },
	"CreateChannel": func(c *api.Client, v string) error {
		_, err := c.CreateChannel(ctx, v, api.CreateChannelRequest{})
		return err
	},
	"GetChannel": func(c *api.Client, v string) error { _, err := c.GetChannel(ctx, v); return err },
	"UpdateChannel": func(c *api.Client, v string) error {
		_, err := c.UpdateChannel(ctx, v, api.UpdateChannelRequest{})
		return err
	},
	"DeleteChannel": func(c *api.Client, v string) error { return c.DeleteChannel(ctx, v) },
	"ListChannelMembers": func(c *api.Client, v string) error {
		_, err := c.ListChannelMembers(ctx, v, api.PageOpts{})
		return err
	},
	"SetChannelPrefs": func(c *api.Client, v string) error {
		_, err := c.SetChannelPrefs(ctx, v, api.ChannelPrefsRequest{})
		return err
	},
	"MarkRead": func(c *api.Client, v string) error { _, err := c.MarkRead(ctx, v, "M"); return err },
	"AddChannelMembers": func(c *api.Client, v string) error {
		_, err := c.AddChannelMembers(ctx, v, []api.UserRef{"U"})
		return err
	},
	"RemoveChannelMember": func(c *api.Client, v string) error { return c.RemoveChannelMember(ctx, "C", api.UserRef(v)) },
	"RemoveChannelMember/channel": func(c *api.Client, v string) error {
		return c.RemoveChannelMember(ctx, v, "U")
	},
	"ListGuestbook": func(c *api.Client, v string) error {
		_, err := c.ListGuestbook(ctx, api.UserRef(v), api.PageOpts{})
		return err
	},
	"WriteGuestbook": func(c *api.Client, v string) error {
		_, err := c.WriteGuestbook(ctx, api.UserRef(v), "hi", false)
		return err
	},
	"DeleteGuestbookEntry": func(c *api.Client, v string) error { return c.DeleteGuestbookEntry(ctx, v) },
	"DeleteUpload":         func(c *api.Client, v string) error { return c.DeleteUpload(ctx, v) },
	"ListMessages":         func(c *api.Client, v string) error { _, err := c.ListMessages(ctx, v, api.MessageQuery{}); return err },
	"SendMessage": func(c *api.Client, v string) error {
		_, _, err := c.SendMessage(ctx, v, api.SendMessageRequest{Kind: "text", Body: "hi"})
		return err
	},
	"GetMessage":    func(c *api.Client, v string) error { _, err := c.GetMessage(ctx, v); return err },
	"EditMessage":   func(c *api.Client, v string) error { _, err := c.EditMessage(ctx, v, "hi"); return err },
	"DeleteMessage": func(c *api.Client, v string) error { return c.DeleteMessage(ctx, v) },
	"GetThread":     func(c *api.Client, v string) error { _, err := c.GetThread(ctx, v, api.ThreadQuery{}); return err },
	"ReplyThread": func(c *api.Client, v string) error {
		_, _, err := c.ReplyThread(ctx, v, api.ThreadReplyRequest{Kind: "text", Body: "hi"})
		return err
	},
	"AddReaction":          func(c *api.Client, v string) error { return c.AddReaction(ctx, v, "👍") },
	"AddReaction/emoji":    func(c *api.Client, v string) error { return c.AddReaction(ctx, "M", v) },
	"RemoveReaction":       func(c *api.Client, v string) error { return c.RemoveReaction(ctx, v, "👍") },
	"RemoveReaction/emoji": func(c *api.Client, v string) error { return c.RemoveReaction(ctx, "M", v) },
	"ListReactionUsers": func(c *api.Client, v string) error {
		_, err := c.ListReactionUsers(ctx, v, "👍", api.PageOpts{})
		return err
	},
	"ListReactionUsers/emoji": func(c *api.Client, v string) error {
		_, err := c.ListReactionUsers(ctx, "M", v, api.PageOpts{})
		return err
	},
	"Pin":            func(c *api.Client, v string) error { return c.Pin(ctx, v) },
	"Unpin":          func(c *api.Client, v string) error { return c.Unpin(ctx, v) },
	"ListPins":       func(c *api.Client, v string) error { _, err := c.ListPins(ctx, v); return err },
	"GetPoll":        func(c *api.Client, v string) error { _, err := c.GetPoll(ctx, v); return err },
	"Vote":           func(c *api.Client, v string) error { _, err := c.Vote(ctx, v, []int{0}); return err },
	"RetractVote":    func(c *api.Client, v string) error { _, err := c.RetractVote(ctx, v); return err },
	"ClosePoll":      func(c *api.Client, v string) error { _, err := c.ClosePoll(ctx, v); return err },
	"GetPost":        func(c *api.Client, v string) error { _, err := c.GetPost(ctx, v); return err },
	"GetPostContext": func(c *api.Client, v string) error { _, err := c.GetPostContext(ctx, v); return err },
	"ListReplies":    func(c *api.Client, v string) error { _, err := c.ListReplies(ctx, v, api.PageOpts{}); return err },
	"DeletePost":     func(c *api.Client, v string) error { return c.DeletePost(ctx, v) },
	"Like":           func(c *api.Client, v string) error { _, err := c.Like(ctx, v); return err },
	"Unlike":         func(c *api.Client, v string) error { _, err := c.Unlike(ctx, v); return err },
	"Repost":         func(c *api.Client, v string) error { _, err := c.Repost(ctx, v); return err },
	"Unrepost":       func(c *api.Client, v string) error { _, err := c.Unrepost(ctx, v); return err },
	"ListLikes":      func(c *api.Client, v string) error { _, err := c.ListLikes(ctx, v, api.PageOpts{}); return err },
	"ListReposts":    func(c *api.Client, v string) error { _, err := c.ListReposts(ctx, v, api.PageOpts{}); return err },
	"ListQuotes":     func(c *api.Client, v string) error { _, err := c.ListQuotes(ctx, v, api.PageOpts{}); return err },
	"HashtagPosts":   func(c *api.Client, v string) error { _, err := c.HashtagPosts(ctx, v, api.PageOpts{}); return err },
	"HashtagPosts/hash": func(c *api.Client, v string) error {
		_, err := c.HashtagPosts(ctx, "#"+v, api.PageOpts{})
		return err
	},
	"RemoveFriend":         func(c *api.Client, v string) error { return c.RemoveFriend(ctx, api.UserRef(v)) },
	"AcceptFriendRequest":  func(c *api.Client, v string) error { _, err := c.AcceptFriendRequest(ctx, v); return err },
	"DeclineFriendRequest": func(c *api.Client, v string) error { return c.DeclineFriendRequest(ctx, v) },
	"CancelFriendRequest":  func(c *api.Client, v string) error { return c.CancelFriendRequest(ctx, v) },
	"Block":                func(c *api.Client, v string) error { return c.Block(ctx, api.UserRef(v)) },
	"Unblock":              func(c *api.Client, v string) error { return c.Unblock(ctx, api.UserRef(v)) },
	"Follow":               func(c *api.Client, v string) error { _, err := c.Follow(ctx, api.UserRef(v)); return err },
	"Unfollow":             func(c *api.Client, v string) error { return c.Unfollow(ctx, api.UserRef(v)) },
	"RemoveFollower":       func(c *api.Client, v string) error { return c.RemoveFollower(ctx, api.UserRef(v)) },
	"AcceptFollowRequest":  func(c *api.Client, v string) error { return c.AcceptFollowRequest(ctx, api.UserRef(v)) },
	"DeclineFollowRequest": func(c *api.Client, v string) error { return c.DeclineFollowRequest(ctx, api.UserRef(v)) },
	"GetSpace":             func(c *api.Client, v string) error { _, err := c.GetSpace(ctx, v); return err },
	"UpdateSpace": func(c *api.Client, v string) error {
		_, err := c.UpdateSpace(ctx, v, api.UpdateSpaceRequest{})
		return err
	},
	"DeleteSpace":   func(c *api.Client, v string) error { return c.DeleteSpace(ctx, v) },
	"TransferSpace": func(c *api.Client, v string) error { _, err := c.TransferSpace(ctx, v, "U"); return err },
	"ListSpaceMembers": func(c *api.Client, v string) error {
		_, err := c.ListSpaceMembers(ctx, v, api.SpaceMembersQuery{})
		return err
	},
	"SetSpaceMemberRole": func(c *api.Client, v string) error {
		_, err := c.SetSpaceMemberRole(ctx, v, "U", api.RoleMember)
		return err
	},
	"SetSpaceMemberRole/user": func(c *api.Client, v string) error {
		_, err := c.SetSpaceMemberRole(ctx, "S", api.UserRef(v), api.RoleMember)
		return err
	},
	"RemoveSpaceMember": func(c *api.Client, v string) error {
		return c.RemoveSpaceMember(ctx, v, "U", api.RemoveMemberOpts{})
	},
	"RemoveSpaceMember/user": func(c *api.Client, v string) error {
		return c.RemoveSpaceMember(ctx, "S", api.UserRef(v), api.RemoveMemberOpts{Ban: true, Reason: "r"})
	},
	"ListSpaceBans": func(c *api.Client, v string) error { _, err := c.ListSpaceBans(ctx, v); return err },
	"Unban":         func(c *api.Client, v string) error { return c.Unban(ctx, v, "U") },
	"Unban/user":    func(c *api.Client, v string) error { return c.Unban(ctx, "S", api.UserRef(v)) },
	"ListInvites":   func(c *api.Client, v string) error { _, err := c.ListInvites(ctx, v); return err },
	"CreateInvite": func(c *api.Client, v string) error {
		_, err := c.CreateInvite(ctx, v, api.CreateInviteRequest{})
		return err
	},
	"RevokeInvite":        func(c *api.Client, v string) error { return c.RevokeInvite(ctx, v) },
	"GetInvite":           func(c *api.Client, v string) error { _, err := c.GetInvite(ctx, v); return err },
	"AcceptInvite":        func(c *api.Client, v string) error { _, err := c.AcceptInvite(ctx, v); return err },
	"SendDirectInvite":    func(c *api.Client, v string) error { _, err := c.SendDirectInvite(ctx, v, "U"); return err },
	"AcceptDirectInvite":  func(c *api.Client, v string) error { _, err := c.AcceptDirectInvite(ctx, v); return err },
	"DeclineDirectInvite": func(c *api.Client, v string) error { return c.DeclineDirectInvite(ctx, v) },
	"CancelDirectInvite":  func(c *api.Client, v string) error { return c.CancelDirectInvite(ctx, v) },
	"ListUserStories":     func(c *api.Client, v string) error { _, err := c.ListUserStories(ctx, api.UserRef(v)); return err },
	"ViewStory":           func(c *api.Client, v string) error { return c.ViewStory(ctx, v) },
	"ListStoryViewers":    func(c *api.Client, v string) error { _, err := c.ListStoryViewers(ctx, v, api.PageOpts{}); return err },
	"DeleteStory":         func(c *api.Client, v string) error { return c.DeleteStory(ctx, v) },
	"GetUser":             func(c *api.Client, v string) error { _, err := c.GetUser(ctx, api.UserRef(v)); return err },
	"ListUserPosts": func(c *api.Client, v string) error {
		_, err := c.ListUserPosts(ctx, api.UserRef(v), "", api.PageOpts{})
		return err
	},
	"ListFollowers": func(c *api.Client, v string) error {
		_, err := c.ListFollowers(ctx, api.UserRef(v), api.PageOpts{})
		return err
	},
	"ListFollowing": func(c *api.Client, v string) error {
		_, err := c.ListFollowing(ctx, api.UserRef(v), api.PageOpts{})
		return err
	},
	// 5.24 room documents (P0 Task 30)
	"ListDocs":  func(c *api.Client, v string) error { _, err := c.ListDocs(ctx, v, "", api.PageOpts{}); return err },
	"CreateDoc": func(c *api.Client, v string) error { _, err := c.CreateDoc(ctx, v, api.CreateDocRequest{}); return err },
	"GetDoc":    func(c *api.Client, v string) error { _, err := c.GetDoc(ctx, v); return err },
	"UpdateDoc": func(c *api.Client, v string) error { _, err := c.UpdateDoc(ctx, v, api.UpdateDocRequest{}); return err },
	"DeleteDoc": func(c *api.Client, v string) error { return c.DeleteDoc(ctx, v) },
	"ListDocVersions": func(c *api.Client, v string) error {
		_, err := c.ListDocVersions(ctx, v)
		return err
	},
	"CreateDocVersion": func(c *api.Client, v string) error {
		_, err := c.CreateDocVersion(ctx, v, "n")
		return err
	},
	"GetDocVersion": func(c *api.Client, v string) error { _, err := c.GetDocVersion(ctx, v, 1); return err },
	"RestoreDocVersion": func(c *api.Client, v string) error {
		_, err := c.RestoreDocVersion(ctx, v, 1)
		return err
	},
	"ListDocComments": func(c *api.Client, v string) error {
		_, err := c.ListDocComments(ctx, v, api.PageOpts{})
		return err
	},
	"CreateDocComment": func(c *api.Client, v string) error {
		_, err := c.CreateDocComment(ctx, v, api.CreateDocCommentRequest{})
		return err
	},
	"UpdateDocComment": func(c *api.Client, v string) error {
		_, err := c.UpdateDocComment(ctx, v, "K", api.UpdateDocCommentRequest{})
		return err
	},
	"UpdateDocComment/comment": func(c *api.Client, v string) error {
		_, err := c.UpdateDocComment(ctx, "D", v, api.UpdateDocCommentRequest{})
		return err
	},
	"DeleteDocComment":         func(c *api.Client, v string) error { return c.DeleteDocComment(ctx, v, "K") },
	"DeleteDocComment/comment": func(c *api.Client, v string) error { return c.DeleteDocComment(ctx, "D", v) },
	"GetWorkspaceShare":        func(c *api.Client, v string) error { _, err := c.GetWorkspaceShare(ctx, v); return err },
	"InviteWorkspaceShare": func(c *api.Client, v string) error {
		_, err := c.InviteWorkspaceShare(ctx, v, []api.UserRef{"@hana"})
		return err
	},
	"RemoveWorkspaceParticipant":      func(c *api.Client, v string) error { return c.RemoveWorkspaceParticipant(ctx, v, "@hana") },
	"RemoveWorkspaceParticipant/user": func(c *api.Client, v string) error { return c.RemoveWorkspaceParticipant(ctx, "W", api.UserRef(v)) },
	"EndWorkspaceShare":               func(c *api.Client, v string) error { return c.EndWorkspaceShare(ctx, v) },
	// FetchMedia takes a server-supplied URL (Media.URL) instead of a segment.
	"FetchMedia": func(c *api.Client, v string) error { _, err := c.FetchMedia(ctx, "/v1/media/"+v, ""); return err },
	"FetchMedia/abs": func(c *api.Client, v string) error {
		_, err := c.FetchMedia(ctx, c.BaseURL()+"/v1/media/"+v, "")
		return err
	},
}

// TestPathSegmentsRejected (pending-fixes 5): an empty, "." or ".." path
// value is a client-side 400 invalid_request {"fields":{"path":"invalid"}}
// and no request reaches the network.
func TestPathSegmentsRejected(t *testing.T) {
	for name, call := range pathCalls {
		for vname, v := range badSegments {
			t.Run(name+"/"+vname, func(t *testing.T) {
				s := apitest.New(t)
				e := asError(t, call(s.Client(), v))
				if e.Status != 400 || e.Code != "invalid_request" || e.Fields()["path"] != "invalid" {
					t.Errorf("err = %+v (fields %v), want 400 invalid_request path=invalid", e, e.Fields())
				}
				if n := len(s.Requests()); n != 0 {
					t.Errorf("requests = %d, want 0: %+v", n, s.Requests())
				}
			})
		}
	}
}

// TestPathCallsCoverEveryPathMethod keeps pathCalls in step with the client:
// every exported method whose path takes a caller value must be listed.
func TestPathCallsCoverEveryPathMethod(t *testing.T) {
	for _, m := range pathMethods(t) {
		if _, ok := pathCalls[m]; !ok {
			t.Errorf("pathCalls has no row for %s", m)
		}
	}
}

// TestPathSegmentsLookingLikeDotsAreEscaped: values that only resemble dot
// segments are sent, escaped so no normaliser can turn them into one.
func TestPathSegmentsLookingLikeDotsAreEscaped(t *testing.T) {
	s := apitest.New(t)
	s.Error("DELETE /v1/invites/{code}", 404, "not_found")
	c := s.Client()
	for _, v := range []string{"...", ".a", "a.", "%2e%2e", ".%2E", `..\..`, "../x", "a/../b"} {
		if err := c.RevokeInvite(ctx, v); !api.IsCode(err, "not_found") {
			t.Errorf("RevokeInvite(%q) err = %v, want not_found from the server", v, err)
		}
	}
	reqs := s.Requests()
	want := []string{
		"/v1/invites/...", "/v1/invites/.a", "/v1/invites/a.", "/v1/invites/%252e%252e",
		"/v1/invites/.%252E", "/v1/invites/..%5C..", "/v1/invites/..%2Fx", "/v1/invites/a%2F..%2Fb",
	}
	if len(reqs) != len(want) {
		t.Fatalf("requests = %d, want %d: %+v", len(reqs), len(want), reqs)
	}
	for i, r := range reqs {
		if r.Path != want[i] {
			t.Errorf("request %d path = %q, want %q", i, r.Path, want[i])
		}
	}
}

// TestFetchMediaRejectsNormalisedURLs: a server-supplied media URL whose path
// a normaliser would rewrite (dot segments, encoded dots, "//", raw "\") is
// never fetched. The encoded-dot cases here carry an extra segment, so
// checkMediaPath already refuses them; the single-segment ones only
// checkPath stops are TestFetchMediaRejectsSingleSegmentEncodedDots, a raw
// "\" off the API origin TestFetchMediaRejectsRawBackslashOffOrigin.
// checkPath alone only stops paths a normaliser changes; that
// a Media.URL cannot name another API endpoint at all is
// TestFetchMediaSameOriginOnlyMediaPath.
func TestFetchMediaRejectsNormalisedURLs(t *testing.T) {
	s := apitest.New(t)
	c := s.Client()
	for _, u := range []string{
		"/v1/media/../auth/sessions", "/v1/media/%2e%2e/auth/sessions", "/v1/media/.%2E/x",
		"/v1/media/%2E/x", "/v1/./media/x", "/v1//media/x", `/v1/media/..\auth`, `/v1/media\..\auth`,
		s.URL + "/v1/media/../auth/sessions", s.URL + "/v1/media/x/",
	} {
		e := asError(t, func() error { _, err := c.FetchMedia(ctx, u, ""); return err }())
		if e.Code != "invalid_request" || !strings.Contains(e.Message, "path") {
			t.Errorf("FetchMedia(%q) err = %+v, want invalid_request", u, e)
		}
	}
	if n := len(s.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0: %+v", n, s.Requests())
	}
}

// encodedDotIDs are single media-path segments that only checkPath's
// percent-decoding stops: checkMediaPath sees one segment holding no "/"
// once decoded, yet WHATWG URL parsing decodes them as "." / ".." (Go sends
// them as they are), so "/v1/media/%2e%2e" lands on "/v1/".
var encodedDotIDs = []string{"%2e%2e", ".%2E", "%2E", "%2e.", "%2E%2E", "%2e"}

// wantPathInvalid reports err unless it is the client-side 400
// invalid_request {"fields":{"path":"invalid"}} (not t.Fatal, so a request
// that did go out is still counted and shown afterwards).
func wantPathInvalid(t *testing.T, what string, err error) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) || e.Status != 400 || e.Code != "invalid_request" || e.Fields()["path"] != "invalid" {
		t.Errorf("%s err = %v, want 400 invalid_request path=invalid (nothing sent)", what, err)
	}
}

// TestFetchMediaRejectsSingleSegmentEncodedDots (pending-fixes 5): a
// server-supplied Media.URL whose one media segment is a percent-encoded dot
// segment is refused before any request, relative or absolute, on the API
// origin (where the bearer token would be attached) or off it.
func TestFetchMediaRejectsSingleSegmentEncodedDots(t *testing.T) {
	s := apitest.New(t)
	foreign := apitest.New(t)
	var auth []string
	s.Handle("GET /", func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
	})
	c := s.Client()
	for _, id := range encodedDotIDs {
		for _, u := range []string{"/v1/media/" + id, "v1/media/" + id, s.URL + "/v1/media/" + id, "/v1/media/" + id + "?v=1", foreign.URL + "/v1/media/" + id} {
			_, err := c.FetchMedia(ctx, u, "")
			wantPathInvalid(t, "FetchMedia("+u+")", err)
		}
	}
	if n := len(s.Requests()); n != 0 {
		t.Errorf("API origin requests = %d (Authorization %q), want 0: %+v", n, auth, s.Requests())
	}
	if n := len(foreign.Requests()); n != 0 {
		t.Errorf("foreign origin requests = %d, want 0: %+v", n, foreign.Requests())
	}
}

// TestFetchMediaRejectsRawBackslashOffOrigin (pending-fixes 5): a raw "\" in
// a request URL is refused by checkPath itself. Off the API origin
// checkMediaPath does not run, so this is the only check that sees it.
func TestFetchMediaRejectsRawBackslashOffOrigin(t *testing.T) {
	s := apitest.New(t)
	foreign := apitest.New(t)
	c := s.Client()
	for _, u := range []string{foreign.URL + `/v1/media\..\me`, foreign.URL + `/img/a\b.png`, foreign.URL + `/a\`} {
		_, err := c.FetchMedia(ctx, u, "")
		wantPathInvalid(t, "FetchMedia("+u+")", err)
	}
	if n := len(foreign.Requests()) + len(s.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0: %+v %+v", n, foreign.Requests(), s.Requests())
	}

	// A well-formed foreign media URL still goes out (without the token).
	foreign.Handle("GET /img/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	})
	if _, err := c.FetchMedia(ctx, foreign.URL+"/img/a.png", ""); err != nil {
		t.Errorf("FetchMedia(foreign /img/a.png): %v", err)
	}
}

// TestFetchMediaSameOriginOnlyMediaPath: on the API origin (where the bearer
// token is attached) FetchMedia only GETs <base path>/v1/media/<one segment>.
// A hostile Media.URL naming another endpoint ("/v1/me", "/v1/auth/sessions")
// is a client-side invalid_request and nothing is sent, so the token never
// reaches it. A foreign origin gets no token and keeps working.
func TestFetchMediaSameOriginOnlyMediaPath(t *testing.T) {
	s := apitest.New(t)
	c := s.Client()
	for _, u := range []string{
		"/v1/me", "/v1/auth/sessions", "v1/me", "/v1/media", "/v1/mediax/U", "/v2/media/U",
		"/v1/media/U/extra", "/v1/media/U%2F..", "/V1/media/U", "/x/v1/media/U",
		s.URL + "/v1/me", s.URL + "/v1/auth/sessions", s.URL + "/v1/media/U/x",
		strings.ToUpper(s.URL[:4]) + s.URL[4:] + "/v1/me",
	} {
		e := asError(t, func() error { _, err := c.FetchMedia(ctx, u, ""); return err }())
		if e.Status != 400 || e.Code != "invalid_request" || e.Fields()["path"] != "invalid" {
			t.Errorf("FetchMedia(%q) err = %+v (fields %v), want 400 invalid_request path=invalid", u, e, e.Fields())
		}
	}
	if n := len(s.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0: %+v", n, s.Requests())
	}

	// A base URL with a path prefix: only <prefix>/v1/media/<id> is the API's.
	pc := api.New(s.URL+"/pre/", api.WithToken("msl_test"))
	for _, u := range []string{s.URL + "/v1/media/U", s.URL + "/pre/v1/me", s.URL + "/prev1/media/U"} {
		e := asError(t, func() error { _, err := pc.FetchMedia(ctx, u, ""); return err }())
		if e.Code != "invalid_request" {
			t.Errorf("prefixed base: FetchMedia(%q) err = %+v, want invalid_request", u, e)
		}
	}
	if n := len(s.Requests()); n != 0 {
		t.Errorf("prefixed base: requests = %d, want 0: %+v", n, s.Requests())
	}

	// Well-formed media URLs still go out, relative or absolute, with a query.
	s.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	})
	for _, u := range []string{"/v1/media/U", "v1/media/U", s.URL + "/v1/media/U", "/v1/media/U?v=2", "/v1/media/%E2%9C%93"} {
		if _, err := c.FetchMedia(ctx, u, ""); err != nil {
			t.Errorf("FetchMedia(%q): %v", u, err)
		}
	}
}

// TestPathMethodsScanSeesMethodHelpers: the pathMethods scanner also counts
// an exported method that reaches escPath through an unexported method
// helper (c.helper(...)) or a chain of helpers, not only package funcs.
func TestPathMethodsScanSeesMethodHelpers(t *testing.T) {
	got := map[string]bool{}
	for _, m := range scanPathMethods(t, "testdata/pathscan") {
		got[m] = true
	}
	for _, m := range []string{"Direct", "ViaFunc", "ViaMethod", "ViaChain", "ViaGeneric"} {
		if !got[m] {
			t.Errorf("scanner missed %s (got %v)", m, got)
		}
	}
	for _, m := range []string{"Constant", "NoPath"} {
		if got[m] {
			t.Errorf("scanner listed %s, which puts no caller value in a path", m)
		}
	}
}

// pathMethods lists the exported *Client methods whose request path takes a
// caller value: they call escPath with a non-literal argument, directly or
// through unexported helpers that do — package funcs (likeOrRepost, generic
// one[T]) or methods on the receiver (c.helper(...)), to any depth — plus
// FetchMedia.
func pathMethods(t *testing.T) []string {
	t.Helper()
	out := scanPathMethods(t, ".")
	if len(out) < 80 {
		t.Fatalf("found only %d path methods: %v (scanner broken?)", len(out), out)
	}
	return out
}

// scanPathMethods is pathMethods over the Go package in dir.
func scanPathMethods(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var decls []*ast.FuncDecl
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
					decls = append(decls, fd)
				}
			}
		}
	}
	// key names a callee: "f" for package func f, ".m" for method m.
	key := func(fd *ast.FuncDecl) string {
		if fd.Recv != nil {
			return "." + fd.Name.Name
		}
		return fd.Name.Name
	}
	recvName := func(fd *ast.FuncDecl) string {
		if fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
			return ""
		}
		return fd.Recv.List[0].Names[0].Name
	}
	// usesValue reports whether fd calls escPath with a non-literal argument
	// or calls a helper already known to (helpers, by key).
	usesValue := func(fd *ast.FuncDecl, helpers map[string]bool) bool {
		recv := recvName(fd)
		found := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := call.Fun
			switch ix := fn.(type) { // generic helper[T](...) / helper[K, V](...)
			case *ast.IndexExpr:
				fn = ix.X
			case *ast.IndexListExpr:
				fn = ix.X
			}
			switch f := fn.(type) {
			case *ast.Ident:
				if helpers[f.Name] {
					found = true
				}
				if f.Name == "escPath" {
					for _, a := range call.Args {
						if _, lit := a.(*ast.BasicLit); !lit {
							found = true
						}
					}
				}
			case *ast.SelectorExpr: // c.helper(...) on this method's receiver
				if x, ok := f.X.(*ast.Ident); ok && recv != "" && x.Name == recv && helpers["."+f.Sel.Name] {
					found = true
				}
			}
			return true
		})
		return found
	}
	// Unexported helpers that reach escPath, closed over helper chains.
	helpers := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			k := key(fd)
			if fd.Name.Name == "escPath" || ast.IsExported(fd.Name.Name) || helpers[k] {
				continue
			}
			if usesValue(fd, helpers) {
				helpers[k] = true
				changed = true
			}
		}
	}
	var out []string
	for _, fd := range decls {
		if fd.Recv == nil || !ast.IsExported(fd.Name.Name) {
			continue
		}
		if usesValue(fd, helpers) || fd.Name.Name == "FetchMedia" {
			out = append(out, fd.Name.Name)
		}
	}
	return out
}
