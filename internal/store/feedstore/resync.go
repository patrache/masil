package feedstore

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// resyncOverlap is the events.md 5.3 "겹쳐 받기" overlap for the since anchor.
const resyncOverlap = 5 * time.Second

// Open-window keys (SetOpen).
const (
	keyFollowing     = "timeline:" + KindFollowing
	keyDiscover      = "timeline:" + KindDiscover
	keyStories       = "stories"
	keyNotifications = "notifications"
	keyPostPrefix    = "post:"
)

// ResyncMsg is one refetched open window (events.md 5.3 step 4). The app
// broadcasts it; every ui/feed component hands it to ApplyResync, which applies
// each Seq at most once.
type ResyncMsg struct {
	Key      string // the SetOpen key this result refreshes
	Timeline *api.TimelinePage
	Restart  bool // the page is a since-less first page (gap=true, or no anchor): replace the window
	Post     *api.Post
	Replies  *api.Page[api.Post]
	Stories  []api.StoryGroup
	Notifs   *api.NotificationPage
	Err      error
	Seq      uint64 // assigned by Resync (per FeedStore counter, one per result message)
}

// Resync implements store.Resyncer: it returns one command per open window
// ("timeline:following", "timeline:discover", each open "post:<id>",
// "stories", "notifications"), batched, or nil when nothing is open (or there
// is no client). It runs on the Update goroutine; the commands only call the
// API and never touch the store. Results land through ApplyResync.
func (f *FeedStore) Resync(_ *store.Store, c *api.Client) tea.Cmd {
	if c == nil {
		return nil
	}
	var cmds []tea.Cmd
	if f.open[keyFollowing] {
		cmds = append(cmds, resyncFollowing(c, f.nextSeq(), f.Timeline(KindFollowing).OverlapAnchor(resyncOverlap)))
	}
	if f.open[keyDiscover] {
		cmds = append(cmds, resyncDiscover(c, f.nextSeq()))
	}
	for _, key := range f.openPosts {
		if f.open[key] {
			cmds = append(cmds, resyncPost(c, f.nextSeq(), key))
		}
	}
	if f.open[keyStories] {
		cmds = append(cmds, resyncStories(c, f.nextSeq()))
	}
	if f.open[keyNotifications] {
		cmds = append(cmds, resyncNotifications(c, f.nextSeq()))
	}
	return tea.Batch(cmds...)
}

func (f *FeedStore) nextSeq() uint64 {
	f.seq++
	return f.seq
}

// resyncFollowing fetches since=anchor; on gap (or no anchor) the first page,
// to replace the window.
func resyncFollowing(c *api.Client, seq uint64, anchor string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		m := ResyncMsg{Key: keyFollowing, Seq: seq}
		if anchor != "" {
			page, err := c.FollowingTimeline(ctx, api.TimelineQuery{Since: anchor})
			if err != nil {
				m.Err = err
				return m
			}
			if !page.Gap {
				m.Timeline = page
				return m
			}
		}
		m.Timeline, m.Err = c.FollowingTimeline(ctx, api.TimelineQuery{})
		m.Restart = m.Err == nil
		return m
	}
}

func resyncDiscover(c *api.Client, seq uint64) tea.Cmd {
	return func() tea.Msg {
		page, err := c.DiscoverTimeline(context.Background(), api.TimelineQuery{})
		return ResyncMsg{Key: keyDiscover, Seq: seq, Timeline: page, Restart: err == nil, Err: err}
	}
}

// resyncPost refetches a post and its first reply page. Err is the first
// failure; whatever succeeded is still carried.
func resyncPost(c *api.Client, seq uint64, key string) tea.Cmd {
	id := strings.TrimPrefix(key, keyPostPrefix)
	return func() tea.Msg {
		ctx := context.Background()
		m := ResyncMsg{Key: key, Seq: seq}
		p, err := c.GetPost(ctx, id)
		if err != nil {
			m.Err = err
		} else {
			m.Post = p
		}
		replies, err := c.ListReplies(ctx, id, api.PageOpts{})
		if err != nil {
			if m.Err == nil {
				m.Err = err
			}
		} else {
			m.Replies = replies
		}
		return m
	}
}

func resyncStories(c *api.Client, seq uint64) tea.Cmd {
	return func() tea.Msg {
		gs, err := c.StoryFeed(context.Background())
		return ResyncMsg{Key: keyStories, Seq: seq, Stories: gs, Err: err}
	}
}

func resyncNotifications(c *api.Client, seq uint64) tea.Cmd {
	return func() tea.Msg {
		page, err := c.ListNotifications(context.Background(), api.NotificationQuery{})
		return ResyncMsg{Key: keyNotifications, Seq: seq, Notifs: page, Err: err}
	}
}

// ApplyResync applies a Resync result on the Update goroutine. Each Seq is
// applied at most once (several components forward the same broadcast); a Seq
// that Resync never handed out — 0, or one issued before the last Reset — is
// ignored. A failed fetch leaves its window as it was, except that an open post
// the server no longer has (404 not_found) becomes the deleted placeholder.
func (f *FeedStore) ApplyResync(m ResyncMsg) {
	if !f.markApplied(m.Seq) {
		return
	}
	switch {
	case m.Key == keyFollowing:
		f.applyTimeline(KindFollowing, m)
	case m.Key == keyDiscover:
		f.applyTimeline(KindDiscover, m)
		if m.Timeline != nil {
			f.SetDiscoverNew(0)
		}
	case strings.HasPrefix(m.Key, keyPostPrefix):
		id := strings.TrimPrefix(m.Key, keyPostPrefix)
		if m.Post != nil {
			f.ObservePosts(*m.Post)
		} else if api.IsCode(m.Err, "not_found") {
			f.postDeleted(id)
		}
		if m.Replies != nil {
			f.SetRepliesPage(id, m.Replies)
		}
	case m.Key == keyStories:
		if m.Err == nil {
			f.SetStories(m.Stories)
		}
	case m.Key == keyNotifications:
		f.SetNotificationsPage(m.Notifs)
	}
}

// applyTimeline replaces the window with a Restart page (SetTimelinePage:
// cursor, loaded flag, raced events kept) or caches a since page and merges it
// into the window by ID. Items are merged oldest first so the newest repost of
// a post wins.
func (f *FeedStore) applyTimeline(kind string, m ResyncMsg) {
	if m.Timeline == nil {
		return
	}
	if m.Restart {
		f.SetTimelinePage(kind, m.Timeline)
		return
	}
	items := append([]api.TimelineItem(nil), m.Timeline.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, it := range items {
		f.ObservePosts(it.Post)
	}
	w := f.Timeline(kind)
	for _, it := range items {
		upsertTimelineItem(w, it)
	}
}

// markApplied records seq and reports whether it was not applied before.
func (f *FeedStore) markApplied(seq uint64) bool {
	if seq <= f.appliedMark || seq > f.seq || f.appliedAbove[seq] {
		return false
	}
	f.appliedAbove[seq] = true
	for f.appliedAbove[f.appliedMark+1] {
		delete(f.appliedAbove, f.appliedMark+1)
		f.appliedMark++
	}
	return true
}
