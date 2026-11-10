package feed

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
)

// Post action result tags (ui.Do): "feed.<action>:<target>".
const (
	tagLike           = "feed.like:"            // + post id
	tagRepost         = "feed.repost:"          // + post id
	tagBlock          = "feed.block:"           // + user id
	tagDeletePost     = "feed.delete_post:"     // + post id
	tagRemoveFollower = "feed.remove_follower:" // + user id
)

// overlayMedia is the photo viewer (Task 18); args "<postID> <media idx>".
// Post action copy: the feed catalog area.
const overlayMedia = "feed.media"

// claim lets exactly one of the components that receive a broadcast result do
// the local apply (every ui/feed component calls applyResults, and the app
// broadcasts a ResultMsg to all of them; the outcome pointer is shared).
type claim struct{ done bool }

func (c *claim) take() bool {
	if c == nil || c.done {
		return false
	}
	c.done = true
	return true
}

// toggleOutcome is a like / repost request: the optimistic state it replaced
// (restored on error) and the server's answer.
type toggleOutcome struct {
	claim
	repost    bool
	post      *api.Post // the post flipped optimistically
	postID    string
	prevOn    bool
	prevCount int
	on        bool // server result
	count     int
}

// blockOutcome is a PUT /v1/blocks/{user} request.
type blockOutcome struct {
	claim
	userID string
}

// deleteOutcome is a DELETE /v1/posts/{id} request.
type deleteOutcome struct {
	claim
	postID string
}

// errNoClient stands in for a missing API client (never in the app).
var errNoClient = &api.Error{Code: "network"}

// eachCopy runs fn on p and on the FeedStore copy of the same post when that is
// a different value, so an optimistic change shows wherever the post is drawn.
func eachCopy(ctx *ui.Context, p *api.Post, fn func(*api.Post)) {
	fn(p)
	if ctx == nil || ctx.Store == nil {
		return
	}
	if fs := feedstore.Get(ctx.Store); fs != nil {
		if c := fs.Post(p.ID); c != nil && c != p {
			fn(c)
		}
	}
}

func setLike(on bool, count int) func(*api.Post) {
	return func(p *api.Post) { p.Viewer.Liked, p.LikeCount = on, max(count, 0) }
}

func setRepost(on bool, count int) func(*api.Post) {
	return func(p *api.Post) { p.Viewer.Reposted, p.RepostCount = on, max(count, 0) }
}

// toggleLike flips viewer.liked and like_count ±1 at once, then sends PUT or
// DELETE /v1/posts/{id}/like (tag "feed.like:<id>"); applyResults confirms the
// count from the answer or restores the previous state on error.
func toggleLike(ctx *ui.Context, p *api.Post) tea.Cmd {
	if p == nil {
		return nil
	}
	if p.Deleted {
		return ui.Toast(cat(ctx).Feed.CardDeleted(), ui.ToastInfo)
	}
	o := &toggleOutcome{post: p, postID: p.ID, prevOn: p.Viewer.Liked, prevCount: p.LikeCount}
	on, count := !p.Viewer.Liked, p.LikeCount+1
	if !on {
		count = p.LikeCount - 1
	}
	eachCopy(ctx, p, setLike(on, count))
	return ui.Do(ctx, tagLike+p.ID, func(c context.Context, a *api.Client) (*toggleOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		call := a.Unlike
		if on {
			call = a.Like
		}
		r, err := call(c, o.postID)
		if r != nil {
			o.on, o.count = r.Liked, r.LikeCount
		}
		return o, err
	})
}

// toggleRepost is toggleLike for viewer.reposted / repost_count (tag "feed.repost:<id>").
func toggleRepost(ctx *ui.Context, p *api.Post) tea.Cmd {
	if p == nil {
		return nil
	}
	if p.Deleted {
		return ui.Toast(cat(ctx).Feed.CardDeleted(), ui.ToastInfo)
	}
	if p.Author.IsPrivate {
		return ui.Toast(cat(ctx).Feed.PrivateNoShare(), ui.ToastInfo)
	}
	o := &toggleOutcome{repost: true, post: p, postID: p.ID, prevOn: p.Viewer.Reposted, prevCount: p.RepostCount}
	on, count := !p.Viewer.Reposted, p.RepostCount+1
	if !on {
		count = p.RepostCount - 1
	}
	eachCopy(ctx, p, setRepost(on, count))
	return ui.Do(ctx, tagRepost+p.ID, func(c context.Context, a *api.Client) (*toggleOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		call := a.Unrepost
		if on {
			call = a.Repost
		}
		r, err := call(c, o.postID)
		if r != nil {
			o.on, o.count = r.Reposted, r.RepostCount
		}
		return o, err
	})
}

// blockUser is the Cmd behind "작성자 차단": PUT /v1/blocks/{user} (tag "feed.block:<id>").
func blockUser(ctx *ui.Context, userID string) tea.Cmd {
	o := &blockOutcome{userID: userID}
	return ui.Do(ctx, tagBlock+userID, func(c context.Context, a *api.Client) (*blockOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		return o, a.Block(c, api.UserID(userID))
	})
}

// deletePost is the Cmd behind "글 삭제": DELETE /v1/posts/{id} (tag "feed.delete_post:<id>").
func deletePost(ctx *ui.Context, postID string) tea.Cmd {
	o := &deleteOutcome{postID: postID}
	return ui.Do(ctx, tagDeletePost+postID, func(c context.Context, a *api.Client) (*deleteOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		return o, a.DeletePost(c, postID)
	})
}

// applyResults does the local apply of a post action result, once per request
// however many components see the broadcast. Every ui/feed component (and
// ui/feed overlay) calls it in Update, right after forwardResync.
//
//   - like / repost: the server count confirms the optimistic state; an error
//     restores it and reports ui.ErrorMsg.
//   - block: right after the 204, without waiting for block.updated —
//     Store.Blocks[id] = now, FeedStore.HideAuthor(id), toast "차단했어요". The
//     event that follows only rebuilds the same state.
//   - delete: FeedStore.MarkDeleted (the post.deleted rule).
//   - poll vote / retract / close / refresh: applyPoll.
func applyResults(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[*pollOutcome]:
		return applyPoll(ctx, m)
	case ui.ResultMsg[*toggleOutcome]:
		o := m.Value
		if (!strings.HasPrefix(m.Tag, tagLike) && !strings.HasPrefix(m.Tag, tagRepost)) || !o.take() {
			return nil
		}
		set, errCtx := setLike, cat(ctx).Feed.ErrCtxLike()
		if o.repost {
			set, errCtx = setRepost, cat(ctx).Feed.ErrCtxRepost()
		}
		if m.Err != nil {
			eachCopy(ctx, o.post, set(o.prevOn, o.prevCount))
			return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errCtx} }
		}
		eachCopy(ctx, o.post, set(o.on, o.count))
	case ui.ResultMsg[*blockOutcome]:
		o := m.Value
		if !strings.HasPrefix(m.Tag, tagBlock) || !o.take() {
			return nil
		}
		if m.Err != nil {
			where := cat(ctx).Feed.ErrCtxBlock()
			return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: where} }
		}
		if ctx != nil && ctx.Store != nil {
			if ctx.Store.Blocks == nil {
				ctx.Store.Blocks = map[string]time.Time{}
			}
			ctx.Store.Blocks[o.userID] = ctx.Now()
			if fs := feedstore.Get(ctx.Store); fs != nil {
				fs.HideAuthor(o.userID)
			}
		}
		return ui.Toast(cat(ctx).Feed.BlockedDone(), ui.ToastSuccess)
	case ui.ResultMsg[*deleteOutcome]:
		o := m.Value
		if !strings.HasPrefix(m.Tag, tagDeletePost) || !o.take() {
			return nil
		}
		if m.Err != nil {
			where := cat(ctx).Feed.ErrCtxDeletePost()
			return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: where} }
		}
		if ctx != nil && ctx.Store != nil {
			if fs := feedstore.Get(ctx.Store); fs != nil {
				fs.MarkDeleted(o.postID)
			}
		}
		return ui.Toast(cat(ctx).Feed.PostDeleted(), ui.ToastSuccess)
	}
	return nil
}

// blockSignal reports a message after which per-screen ID lists must drop
// blocked users (Task 5 block filter): block.updated, or a successful
// "feed.block:<id>" result. Lists then re-filter with Store.IsBlocked.
func blockSignal(msg tea.Msg) bool {
	switch m := msg.(type) {
	case ui.EventMsg:
		return m.Event.Type == "block.updated"
	case ui.ResultMsg[*blockOutcome]:
		return strings.HasPrefix(m.Tag, tagBlock) && m.Err == nil
	}
	return false
}

// composeRoute is the composer for a reply to / quote of postID (one instance:
// both routes share Route.Key()).
func composeRoute(postID, mode string) ui.Route {
	return ui.Route{Kind: ui.RouteCompose, Arg: "post", ID: postID, Query: map[string]string{"mode": mode}}
}

// handlePostKey runs the per-post keys on p: l like, b repost, q quote, r reply,
// o image viewer, u author, m menu, # hashtag. A deleted post answers every one
// with "삭제된 글이에요"; a private author's post refuses b and q without a request.
func handlePostKey(ctx *ui.Context, msg tea.KeyPressMsg, p *api.Post) (bool, tea.Cmd) {
	if p == nil || ctx == nil || ctx.Keys == nil {
		return false, nil
	}
	km := ctx.Keys
	var act keymap.Action
	for _, a := range []keymap.Action{keymap.PostLike, keymap.PostRepost, keymap.PostQuote, keymap.PostReply,
		keymap.PostOpenMedia, keymap.PostAuthor, keymap.MsgActions} {
		if km.Matches(msg, a) {
			act = a
			break
		}
	}
	hash := act == "" && msg.String() == "#"
	if act == "" && !hash {
		return false, nil
	}
	if p.Deleted {
		return true, ui.Toast(cat(ctx).Feed.CardDeleted(), ui.ToastInfo)
	}
	if hash {
		switch len(p.Hashtags) {
		case 0:
			return true, nil
		case 1:
			return true, ui.Navigate(ui.Route{Kind: ui.RouteHashtag, ID: p.Hashtags[0]})
		}
		return true, ui.OpenRegisteredOverlay(overlayHashtagPick, p.ID)
	}
	switch act {
	case keymap.PostLike:
		return true, toggleLike(ctx, p)
	case keymap.PostRepost:
		return true, toggleRepost(ctx, p)
	case keymap.PostQuote:
		if p.Author.IsPrivate {
			return true, ui.Toast(cat(ctx).Feed.PrivateNoShare(), ui.ToastInfo)
		}
		return true, ui.Navigate(composeRoute(p.ID, "quote"))
	case keymap.PostReply:
		return true, ui.Navigate(composeRoute(p.ID, "reply"))
	case keymap.PostOpenMedia:
		if len(p.Media) == 0 {
			return true, nil
		}
		return true, ui.OpenRegisteredOverlay(overlayMedia, p.ID+" 0")
	case keymap.PostAuthor:
		return true, ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: p.Author.ID})
	case keymap.MsgActions:
		return true, ui.OpenRegisteredOverlay(overlayPostActions, p.ID)
	}
	return false, nil
}

// handlePostHit handles a click on a card region of p: hashtag -> RouteHashtag,
// author -> RouteProfile, image -> feed.media, quote -> RoutePost, poll option
// -> vote (single choice) or feed.poll_vote pre-checked (multi).
func handlePostHit(ctx *ui.Context, h cardHit, p *api.Post) tea.Cmd {
	switch h.Kind {
	case hitPollOption:
		return pollHit(ctx, p, h.Value)
	case hitHashtag:
		return ui.Navigate(ui.Route{Kind: ui.RouteHashtag, ID: h.Value})
	case hitAuthor:
		return ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: h.Value})
	case hitQuote:
		return ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: h.Value})
	case hitImage:
		if p == nil || p.Deleted || len(p.Media) == 0 {
			return nil
		}
		return ui.OpenRegisteredOverlay(overlayMedia, p.ID+" "+h.Value)
	}
	return nil
}
