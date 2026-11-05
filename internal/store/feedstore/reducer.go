package feedstore

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

// Apply implements store.Reducer: events.md 4.7 (inbox list), 4.8 (feed),
// block.updated and user.updated. The core has already applied the event, so
// s.IsBlocked reflects block.updated.
func (f *FeedStore) Apply(s *store.Store, ev realtime.Event) {
	v, err := ev.Decode()
	if err != nil || v == nil {
		return
	}
	switch d := v.(type) {
	case realtime.TimelineItemCreated:
		f.itemCreated(s, d.Item)
	case realtime.PostDeleted:
		f.postDeleted(d.PostID)
	case realtime.PostStats:
		if p := f.posts[d.PostID]; p != nil {
			p.LikeCount, p.RepostCount, p.ReplyCount, p.QuoteCount = d.LikeCount, d.RepostCount, d.ReplyCount, d.QuoteCount
		}
	case realtime.PostReplyCreated:
		if blockedPost(s, d.Post) {
			return
		}
		f.observeEventPost(d.Post)
		if w := f.replies[d.ParentID]; w != nil {
			w.Upsert(d.Post)
		}
	case realtime.PollUpdated:
		f.pollUpdated(d.Poll)
	case realtime.StoryCreated:
		f.storyCreated(s, d)
	case realtime.StoryDeleted:
		f.filterStories(func(st api.Story) bool { return st.ID != d.StoryID })
	case realtime.NotificationCreated:
		if n := d.Notification; n.Actor == nil || !s.IsBlocked(n.Actor.ID) {
			f.notifs.Upsert(n)
		}
	case realtime.NotificationsRead:
		f.notificationsRead(d)
	case realtime.BlockUpdated:
		if d.Blocked {
			f.HideAuthor(d.UserID)
		}
	case realtime.UserUpdated:
		f.userUpdated(d.User)
	}
}

// blockedPost reports a post whose author, or available quoted post's author, I blocked.
func blockedPost(s *store.Store, p api.Post) bool {
	if s.IsBlocked(p.Author.ID) {
		return true
	}
	return p.Quote != nil && p.Quote.Available && p.Quote.Post != nil && s.IsBlocked(p.Quote.Post.Author.ID)
}

// itemCreated inserts a following-timeline item (events.md 4.8). A repost
// replaces older repost items of the same post (api.md 5.16: most recent only).
func (f *FeedStore) itemCreated(s *store.Store, it api.TimelineItem) {
	if blockedPost(s, it.Post) || (it.RepostedBy != nil && s.IsBlocked(it.RepostedBy.ID)) {
		return
	}
	f.observeEventPost(it.Post)
	w := f.Timeline(KindFollowing)
	if it.Type == "repost" {
		for _, old := range w.Items() {
			if old.Type == "repost" && old.Post.ID == it.Post.ID && old.ID != it.ID {
				w.Remove(old.ID)
			}
		}
	}
	w.Upsert(it)
}

// postDeleted drops every timeline item of the post, turns the cached post into
// a placeholder, keeps a deleted reply that has replies of its own (api.md 5.15)
// and marks quotes of it unavailable.
func (f *FeedStore) postDeleted(id string) {
	for _, w := range f.timelines {
		for _, it := range w.Items() {
			if it.Post.ID == id {
				w.Remove(it.ID)
			}
		}
	}
	p := f.posts[id]
	if p != nil {
		tombstone(p)
	}
	for _, w := range f.replies {
		r, ok := w.Get(id)
		if !ok {
			continue
		}
		replyCount := r.ReplyCount
		if p != nil {
			replyCount = p.ReplyCount
		}
		if replyCount > 0 {
			w.Update(id, tombstone)
		} else {
			w.Remove(id)
		}
	}
	for _, q := range f.posts {
		if qp := q.Quote; qp != nil && qp.Available && qp.Post != nil && qp.Post.ID == id {
			q.Quote = &api.QuotedPost{Available: false, ID: id, Reason: "deleted"}
		}
	}
}

// tombstone turns p into the deleted placeholder.
func tombstone(p *api.Post) {
	p.Deleted = true
	p.Body = ""
	p.Media = nil
	p.Poll = nil
	p.Quote = nil
	p.Hashtags = nil
	p.Mentions = nil
}

// pollUpdated applies a post poll's counts; my_votes (always [] in the event)
// stays as cached. Message polls are chatstore's.
func (f *FeedStore) pollUpdated(pl api.Poll) {
	if pl.ContextType != "post" {
		return
	}
	p := f.posts[pl.ContextID]
	if p == nil {
		return
	}
	np := copyPoll(&pl)
	if p.Poll != nil && p.Poll.ID == pl.ID {
		np.MyVotes = p.Poll.MyVotes
	}
	p.Poll = np
}

// storyCreated marks a known group unseen, or the ring stale for an unknown one.
func (f *FeedStore) storyCreated(s *store.Store, d realtime.StoryCreated) {
	if s.IsBlocked(d.UserID) {
		return
	}
	for i := range f.stories {
		g := &f.stories[i]
		if g.User.ID != d.UserID {
			continue
		}
		g.HasUnseen = true
		return
	}
	f.storiesStale = true
}

// notificationsRead marks matching inbox items read: ids | all | before (ID <= before).
func (f *FeedStore) notificationsRead(d realtime.NotificationsRead) {
	ids := make(map[string]bool, len(d.IDs))
	for _, id := range d.IDs {
		ids[id] = true
	}
	for _, n := range f.notifs.Items() {
		if d.All || ids[n.ID] || (d.Before != nil && n.ID <= *d.Before) {
			f.notifs.Update(n.ID, func(n *api.Notification) { n.Read = true })
		}
	}
}

// userUpdated refreshes every embedded copy of u (post authors, reply-to
// authors, reposters, notification actors, story groups).
func (f *FeedStore) userUpdated(u api.UserSummary) {
	if u.ID == "" {
		return
	}
	for _, p := range f.posts {
		if p.Author.ID == u.ID {
			p.Author = u
		}
		if p.ReplyToAuthor != nil && p.ReplyToAuthor.ID == u.ID {
			cp := u
			p.ReplyToAuthor = &cp
		}
	}
	for _, w := range f.timelines {
		for _, it := range w.Items() {
			if it.Post.Author.ID == u.ID || (it.RepostedBy != nil && it.RepostedBy.ID == u.ID) {
				w.Update(it.ID, func(it *api.TimelineItem) {
					if it.Post.Author.ID == u.ID {
						it.Post.Author = u
					}
					if it.RepostedBy != nil && it.RepostedBy.ID == u.ID {
						cp := u
						it.RepostedBy = &cp
					}
				})
			}
		}
	}
	for _, w := range f.replies {
		for _, p := range w.Items() {
			if p.Author.ID == u.ID {
				w.Update(p.ID, func(p *api.Post) { p.Author = u })
			}
		}
	}
	for _, n := range f.notifs.Items() {
		if n.Actor != nil && n.Actor.ID == u.ID {
			f.notifs.Update(n.ID, func(n *api.Notification) {
				cp := u
				n.Actor = &cp
			})
		}
	}
	for gi := range f.stories {
		g := &f.stories[gi]
		if g.User.ID != u.ID {
			continue
		}
		g.User = u
		for si := range g.Stories {
			g.Stories[si].Author = u
		}
	}
}
