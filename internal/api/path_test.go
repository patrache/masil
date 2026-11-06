package api_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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
// never fetched, so a hostile Media.URL cannot aim the bearer token at
// another endpoint of the API origin.
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

// pathMethods lists the exported *Client methods whose request path takes a
// caller value: they call escPath with a non-literal argument, directly or
// through a package helper that does (likeOrRepost), plus FetchMedia.
func pathMethods(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
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
	// calls reports whether fd calls a function named in names (pred) or
	// escPath with a non-literal argument.
	usesValue := func(fd *ast.FuncDecl, helpers map[string]bool) bool {
		found := false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := call.Fun
			if ix, ok := fn.(*ast.IndexExpr); ok { // generic helper[T](...)
				fn = ix.X
			}
			id, ok := fn.(*ast.Ident)
			if !ok {
				return true
			}
			if helpers[id.Name] {
				found = true
			}
			if id.Name == "escPath" {
				for _, a := range call.Args {
					if _, lit := a.(*ast.BasicLit); !lit {
						found = true
					}
				}
			}
			return true
		})
		return found
	}
	helpers := map[string]bool{}
	for _, fd := range decls {
		if fd.Recv == nil && fd.Name.Name != "escPath" && !ast.IsExported(fd.Name.Name) && usesValue(fd, nil) {
			helpers[fd.Name.Name] = true
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
	if len(out) < 80 {
		t.Fatalf("found only %d path methods: %v (scanner broken?)", len(out), out)
	}
	return out
}
