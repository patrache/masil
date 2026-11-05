package feedstore

import (
	"sort"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// First-page replaces and realtime events (events.md 5.3 step 5: events that
// arrive while a page is in flight are still applied).
//
// evItems remembers, per window ("timeline:<kind>", "post:<parent id>"), the
// IDs that realtime events upserted. A first page (REST or a resync restart)
// replaces the window but keeps those items when they are not older than the
// page's oldest item (or all of them for an empty page): they raced the
// request, or are newer than it. Items that only an earlier page brought are
// dropped, so a refetch still removes what the server no longer has. The set
// only ever holds IDs present in its window.

// noteEvent records that an event upserted id into the window key.
func (f *FeedStore) noteEvent(key, id string) {
	s := f.evItems[key]
	if s == nil {
		s = map[string]bool{}
		f.evItems[key] = s
	}
	s[id] = true
}

// replacePage replaces w with the page items (inserted by put, in ascending ID
// order) plus the event items of key not older than the page's oldest ID (all
// of them when the page is empty), and prunes key's event set to what stayed.
func replacePage[T any](f *FeedStore, key string, w *store.Window[T], idOf func(T) string, page []T, hasBefore, hasAfter bool, put func(*store.Window[T], T)) {
	oldest := ""
	for i, it := range page {
		if id := idOf(it); i == 0 || id < oldest {
			oldest = id
		}
	}
	ev := f.evItems[key]
	var keep []T
	for _, it := range w.Items() {
		if id := idOf(it); ev[id] && (len(page) == 0 || id >= oldest) {
			keep = append(keep, it)
		}
	}
	sorted := append([]T(nil), page...)
	sort.SliceStable(sorted, func(i, j int) bool { return idOf(sorted[i]) < idOf(sorted[j]) })
	w.Replace(nil, hasBefore, hasAfter)
	inPage := make(map[string]bool, len(page))
	for _, it := range sorted {
		inPage[idOf(it)] = true
		put(w, it)
	}
	next := map[string]bool{}
	for _, it := range keep {
		id := idOf(it)
		if !inPage[id] { // the page's copy wins
			put(w, it)
		}
		if _, ok := w.Get(id); ok {
			next[id] = true
		}
	}
	if len(next) == 0 {
		delete(f.evItems, key)
	} else {
		f.evItems[key] = next
	}
}

func timelineItemID(it api.TimelineItem) string { return it.ID }
func postID(p api.Post) string                  { return p.ID }

func upsertPost(w *store.Window[api.Post], p api.Post) { w.Upsert(p) }

// TimelineLoaded reports whether a first page (SetTimelinePage or a resync
// restart) filled the kind's window. timeline.item_created alone never sets it,
// so a feed opened after events still fetches its first page.
func (f *FeedStore) TimelineLoaded(kind string) bool { return f.tlLoaded[kind] }

// TimelineCursor is the next_cursor of the kind's oldest loaded page (nil = no
// older page). It lives here, not in a screen, so a timeline screen built again
// keeps paging.
func (f *FeedStore) TimelineCursor(kind string) *string { return f.tlCursor[kind] }

// SetTimelinePage caches the page's posts, replaces the kind's window with this
// first page (keeping raced event items, see replacePage), moves the cursor and
// marks the kind loaded.
func (f *FeedStore) SetTimelinePage(kind string, p *api.TimelinePage) {
	if p == nil {
		return
	}
	for _, it := range p.Items {
		f.ObservePosts(it.Post)
	}
	f.setTimelineCursor(kind, p)
	replacePage(f, "timeline:"+kind, f.Timeline(kind), timelineItemID, p.Items, f.tlCursor[kind] != nil, false, upsertTimelineItem)
	f.tlLoaded[kind] = true
}

// AddOlderTimeline caches and merges an older page of kind and moves the cursor
// (an empty page ends paging).
func (f *FeedStore) AddOlderTimeline(kind string, p *api.TimelinePage) {
	if p == nil {
		return
	}
	for _, it := range p.Items {
		f.ObservePosts(it.Post)
	}
	f.setTimelineCursor(kind, p)
	f.Timeline(kind).PrependOlder(p.Items, f.tlCursor[kind] != nil)
}

func (f *FeedStore) setTimelineCursor(kind string, p *api.TimelinePage) {
	if p.NextCursor == nil || len(p.Items) == 0 {
		delete(f.tlCursor, kind)
		return
	}
	c := *p.NextCursor
	f.tlCursor[kind] = &c
}

// SetRepliesPage caches the replies and replaces parentID's reply window with
// this first page, keeping post.reply_created replies that raced it.
func (f *FeedStore) SetRepliesPage(parentID string, p *api.Page[api.Post]) {
	if p == nil {
		return
	}
	f.ObservePosts(p.Items...)
	replacePage(f, keyPostPrefix+parentID, f.Replies(parentID), postID, p.Items, false, p.NextCursor != nil, upsertPost)
}
