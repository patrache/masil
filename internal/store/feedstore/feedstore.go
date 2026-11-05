// Package feedstore is the sns-fun domain store (CA 6.2): timelines, the post
// cache, reply windows, stories, the notification inbox and trends. It is a
// store.Reducer named "feed" and, like the rest of the Store, is mutated only on
// the Bubble Tea Update goroutine.
//
// The post cache is the source of truth: windows (timeline items, reply windows)
// and per-screen ID lists only carry order, and screens draw Post(id). Cached
// posts are updated in place, so a *api.Post a screen holds stays valid for as
// long as the entry lives.
package feedstore

import (
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// Name is the reducer name (store.Reducer).
const Name = "feed"

// Timeline kinds.
const (
	KindFollowing = "following"
	KindDiscover  = "discover"
)

// maxOpenPosts is how many open post details SetOpen remembers for resync.
const maxOpenPosts = 8

func init() {
	store.RegisterReducer(func(*store.Store) store.Reducer { return newFeedStore() })
}

// FeedStore holds timelines, posts by id, reply windows, stories, the
// notification list and trends.
type FeedStore struct {
	timelines    map[string]*store.Window[api.TimelineItem]
	posts        map[string]*api.Post
	replies      map[string]*store.Window[api.Post]
	notifs       *store.Window[api.Notification]
	stories      []api.StoryGroup
	storiesStale bool
	trends       *api.Trends
	discoverNew  int
	open         map[string]bool
	openPosts    []string // "post:<id>" keys, oldest first (at most maxOpenPosts)
}

func newFeedStore() *FeedStore {
	f := &FeedStore{
		timelines: map[string]*store.Window[api.TimelineItem]{},
		notifs:    store.NewWindow(func(n api.Notification) string { return n.ID }, func(n api.Notification) time.Time { return n.CreatedAt }),
	}
	f.clear()
	return f
}

func newTimelineWindow() *store.Window[api.TimelineItem] {
	return store.NewWindow(func(it api.TimelineItem) string { return it.ID }, func(it api.TimelineItem) time.Time { return it.At })
}

func newRepliesWindow() *store.Window[api.Post] {
	return store.NewWindow(func(p api.Post) string { return p.ID }, func(p api.Post) time.Time { return p.CreatedAt })
}

// clear empties every window in place (held window pointers see the reset) and
// drops everything else.
func (f *FeedStore) clear() {
	for _, w := range f.timelines {
		w.Clear()
	}
	for _, w := range f.replies {
		w.Clear()
	}
	f.notifs.Clear()
	f.posts = map[string]*api.Post{}
	f.replies = map[string]*store.Window[api.Post]{}
	f.stories = nil
	f.storiesStale = false
	f.trends = nil
	f.discoverNew = 0
	f.open = map[string]bool{}
	f.openPosts = nil
}

// Get returns the store's FeedStore (s.Reducer("feed")).
func Get(s *store.Store) *FeedStore {
	f, _ := s.Reducer(Name).(*FeedStore)
	return f
}

// Name implements store.Reducer.
func (f *FeedStore) Name() string { return Name }

// Reset implements store.Reducer (logout / user switch).
func (f *FeedStore) Reset() { f.clear() }

// Timeline returns the "following" or "discover" window (created on first use).
func (f *FeedStore) Timeline(kind string) *store.Window[api.TimelineItem] {
	w := f.timelines[kind]
	if w == nil {
		w = newTimelineWindow()
		f.timelines[kind] = w
	}
	return w
}

// Post returns the cached post (a pointer into the cache — optimistic changes go
// here directly) or nil.
func (f *FeedStore) Post(id string) *api.Post { return f.posts[id] }

// Notifications returns the notification inbox window.
func (f *FeedStore) Notifications() *store.Window[api.Notification] { return f.notifs }

// Stories returns the story ring groups (nil until SetStories).
func (f *FeedStore) Stories() []api.StoryGroup { return f.stories }

// Trends returns the last fetched trends or nil.
func (f *FeedStore) Trends() *api.Trends { return f.trends }

// NewDiscoverCount is the "새 글 N개" banner count of the discover timeline.
func (f *FeedStore) NewDiscoverCount() int { return f.discoverNew }

// SetTrends stores the trends result.
func (f *FeedStore) SetTrends(t *api.Trends) { f.trends = t }

// SetDiscoverNew sets the discover banner count.
func (f *FeedStore) SetDiscoverNew(n int) { f.discoverNew = n }

// Replies returns the reply window of parentID, creating it on first call.
func (f *FeedStore) Replies(parentID string) *store.Window[api.Post] {
	w := f.replies[parentID]
	if w == nil {
		w = newRepliesWindow()
		f.replies[parentID] = w
	}
	return w
}

// HasReplies reports whether a reply window exists for parentID.
func (f *FeedStore) HasReplies(parentID string) bool {
	_, ok := f.replies[parentID]
	return ok
}

// ObservePosts caches REST results (and each available quote.post). REST is
// authoritative for every field, including viewer.liked/reposted and
// poll.my_votes, which it may lower.
func (f *FeedStore) ObservePosts(ps ...api.Post) {
	for _, p := range ps {
		if p.Quote != nil && p.Quote.Available && p.Quote.Post != nil {
			f.ObservePosts(*p.Quote.Post)
		}
		f.put(p)
	}
}

// observeEventPost caches a post carried by an event (timeline.item_created,
// post.reply_created): body, media, counts and author come from the event, but
// the cached viewer and poll.my_votes are kept (events.md 4.4 default values).
// A post not cached yet is stored as given.
func (f *FeedStore) observeEventPost(p api.Post) {
	if p.Quote != nil && p.Quote.Available && p.Quote.Post != nil {
		f.observeEventPost(*p.Quote.Post)
	}
	if cur := f.posts[p.ID]; cur != nil {
		p.Viewer = cur.Viewer
		if p.Poll != nil && cur.Poll != nil && cur.Poll.ID == p.Poll.ID {
			pl := *p.Poll
			pl.MyVotes = cur.Poll.MyVotes
			p.Poll = &pl
		}
	}
	f.put(p)
}

// put stores p, overwriting an existing entry in place.
func (f *FeedStore) put(p api.Post) {
	if p.ID == "" {
		return
	}
	p.Poll = copyPoll(p.Poll)
	if cur := f.posts[p.ID]; cur != nil {
		*cur = p
		return
	}
	cp := p
	f.posts[p.ID] = &cp
}

// copyPoll deep-copies a poll so the cache never aliases caller slices.
func copyPoll(p *api.Poll) *api.Poll {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Options = append([]api.PollOption(nil), p.Options...)
	if p.MyVotes != nil {
		cp.MyVotes = append([]int{}, p.MyVotes...)
	}
	return &cp
}

// SetPoll applies a REST poll result to its post (authoritative incl. my_votes).
// Only post polls whose post is cached are stored.
func (f *FeedStore) SetPoll(p api.Poll) {
	if p.ContextType != "post" {
		return
	}
	if post := f.posts[p.ContextID]; post != nil {
		post.Poll = copyPoll(&p)
	}
}

// HideAuthor removes userID's posts, reposts, replies (with their subtrees),
// notifications and stories from every window. It is what block.updated
// {blocked:true} runs, and what the UI calls right after PUT /v1/blocks succeeds.
// Cached posts stay (screens filter their own ID lists); quotes of the author's
// posts become unavailable.
func (f *FeedStore) HideAuthor(userID string) {
	if userID == "" {
		return
	}
	for _, w := range f.timelines {
		for _, it := range w.Items() {
			if it.Post.Author.ID == userID || (it.RepostedBy != nil && it.RepostedBy.ID == userID) {
				w.Remove(it.ID)
			}
		}
	}
	for _, w := range f.replies { // deleting entries while ranging is safe; dropped ones are skipped
		for _, p := range w.Items() {
			if p.Author.ID == userID {
				w.Remove(p.ID)
				f.dropSubtree(p.ID)
			}
		}
	}
	for _, n := range f.notifs.Items() {
		if n.Actor != nil && n.Actor.ID == userID {
			f.notifs.Remove(n.ID)
		}
	}
	if f.stories != nil {
		out := f.stories[:0:0]
		for _, g := range f.stories {
			if g.User.ID != userID {
				out = append(out, g)
			}
		}
		f.stories = out
	}
	for _, p := range f.posts {
		if q := p.Quote; q != nil && q.Available && q.Post != nil && q.Post.Author.ID == userID {
			p.Quote = &api.QuotedPost{Available: false, ID: q.Post.ID, Reason: "not_visible"}
		}
	}
}

// dropSubtree removes the reply window of id and, recursively, of its replies.
func (f *FeedStore) dropSubtree(id string) {
	w := f.replies[id]
	if w == nil {
		return
	}
	delete(f.replies, id)
	for _, p := range w.Items() {
		f.dropSubtree(p.ID)
	}
	w.Clear()
}

// SetStories replaces the story ring (copied, so later in-place changes never
// reach the caller's slices) and clears the stale flag.
func (f *FeedStore) SetStories(gs []api.StoryGroup) {
	f.storiesStale = false
	if gs == nil {
		f.stories = nil
		return
	}
	out := make([]api.StoryGroup, len(gs))
	for i, g := range gs {
		g.Stories = append([]api.Story(nil), g.Stories...)
		out[i] = g
	}
	f.stories = out
}

// MarkStorySeen marks a story seen and recomputes its group's has_unseen.
func (f *FeedStore) MarkStorySeen(storyID string) {
	for gi := range f.stories {
		g := &f.stories[gi]
		for si := range g.Stories {
			if g.Stories[si].ID == storyID {
				g.Stories[si].Seen = true
				g.HasUnseen = anyUnseen(g.Stories)
				return
			}
		}
	}
}

// PruneStories drops stories with expires_at <= now, then empty groups.
func (f *FeedStore) PruneStories(now time.Time) {
	f.filterStories(func(st api.Story) bool { return st.ExpiresAt.After(now) })
}

// StoriesStale reports that a story.created arrived for a group the ring does
// not have: the ring must refetch (GET /v1/stories/feed).
func (f *FeedStore) StoriesStale() bool { return f.storiesStale }

// filterStories keeps the stories for which keep is true, drops empty groups and
// recomputes has_unseen of changed groups.
func (f *FeedStore) filterStories(keep func(api.Story) bool) {
	if f.stories == nil {
		return
	}
	out := f.stories[:0:0]
	for _, g := range f.stories {
		kept := make([]api.Story, 0, len(g.Stories))
		for _, st := range g.Stories {
			if keep(st) {
				kept = append(kept, st)
			}
		}
		if len(kept) == 0 {
			continue
		}
		if len(kept) != len(g.Stories) {
			g.Stories = kept
			g.HasUnseen = anyUnseen(kept)
		}
		out = append(out, g)
	}
	f.stories = out
}

func anyUnseen(ss []api.Story) bool {
	for _, s := range ss {
		if !s.Seen {
			return true
		}
	}
	return false
}

// SetOpen records which windows are open, for Resync: "timeline:following",
// "timeline:discover", "post:<id>" (the last 8 opened are kept), "stories",
// "notifications".
func (f *FeedStore) SetOpen(key string, open bool) {
	isPost := strings.HasPrefix(key, "post:")
	if isPost {
		for i, k := range f.openPosts {
			if k == key {
				f.openPosts = append(f.openPosts[:i], f.openPosts[i+1:]...)
				break
			}
		}
	}
	if !open {
		delete(f.open, key)
		return
	}
	f.open[key] = true
	if isPost {
		f.openPosts = append(f.openPosts, key)
		for len(f.openPosts) > maxOpenPosts {
			delete(f.open, f.openPosts[0])
			f.openPosts = f.openPosts[1:]
		}
	}
}

// IsOpen reports whether key is open (see SetOpen).
func (f *FeedStore) IsOpen(key string) bool { return f.open[key] }
