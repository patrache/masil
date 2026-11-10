package feed

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteFeed, newTimeline)
}

// Timeline tuning.
const (
	discoverPoll    = 30 * time.Second // discover new-posts check period
	discoverVisible = 31 * time.Second // a check runs only if View ran this recently
	discoverLimit   = 100              // since-page size of the check
	bannerCap       = 100              // "100+ new posts"
	moreThreshold   = 5                // load the next page when the selection is this close to the end
	fadeSpan        = 1200 * time.Millisecond
)

// Result tags (ui.Do): "feed.<action>:<target>".
const (
	tagTimeline     = "feed.timeline:"      // + kind: first page
	tagTimelineMore = "feed.timeline_more:" // + kind: next page
	tagDiscoverNew  = "feed.discover_new:"  // + kind: since-check
	tagStories      = "feed.stories:feed"
)

// widgetsPkg is the package of the widget tick messages (spinner, fade).
var widgetsPkg = reflect.TypeOf(widgets.Tab{}).PkgPath()

// timelineIDs numbers timeline instances so their timer msgs never mix.
var timelineIDs atomic.Int64

// pollMsg is the discover new-posts timer of timeline id, current while gen matches.
type pollMsg struct{ id, gen int }

// storiesRetryMsg wakes timeline id when its story-ring retry delay is over.
type storiesRetryMsg struct{ id int }

// storiesBackoff are the delays before refetching a stale story ring after
// failed GET /v1/stories/feed calls in a row; the last one repeats.
var storiesBackoff = []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}

// timelineState is the Persistent state.
type timelineState struct {
	Selected string
	Anchor   widgets.ScrollAnchor
}

// viewKey identifies one cached cardView.
type viewKey struct {
	id       string
	width    int
	density  ui.Density
	selected bool
	pollSel  int
}

// timeline is RouteFeed: the following or discover timeline with the feed tabs,
// the story ring and (discover) the new-posts banner above a card list.
type timeline struct {
	id   int
	kind string // feedstore.KindFollowing | KindDiscover
	key  string // route key (FocusChangedMsg)

	list  *widgets.List[api.TimelineItem]
	items []api.TimelineItem // newest first, blocked authors filtered
	size  ui.Size
	views map[viewKey]cardView
	imgs  imageSlots
	spin  *widgets.Spinner
	fade  widgets.Fade

	fadeUntil      time.Time
	loadingFirst   bool
	loadErr        bool // the last first-page request failed (retry notice instead of the empty state)
	retryOnShow    bool // Resize while loadErr: retry on the next Update
	quietFail      bool // the first-page request in flight is an automatic retry: a failure does not toast
	loadingMore    bool
	storiesLoading bool
	storiesFails   int       // failed story-ring loads in a row
	storiesRetryAt time.Time // no stale-ring refetch before this
	selectFirst    bool      // after a banner refresh: select the first card once the items are in
	bannerSel      bool
	gPending       bool
	pending        *timelineState

	pollGen    int
	polling    bool
	lastViewAt time.Time

	// geometry of the last View (pane-local rows; -1 = not drawn)
	tabY, ringY, bannerY, listY, listH int
	tabHit                             func(x int) (ui.Route, bool)
	ringHit                            func(x int) (string, bool)
	visible                            []string
}

var (
	_ ui.Component    = (*timeline)(nil)
	_ ui.KeyHandler   = (*timeline)(nil)
	_ ui.MouseHandler = (*timeline)(nil)
	_ ui.Titled       = (*timeline)(nil)
	_ ui.Helper       = (*timeline)(nil)
	_ ui.Watcher      = (*timeline)(nil)
	_ ui.Persistent   = (*timeline)(nil)
)

// newTimeline builds RouteFeed (Arg "following" | "discover"; anything else is following).
func newTimeline(_ *ui.Context, r ui.Route) ui.Component {
	kind := feedstore.KindFollowing
	if r.Arg == feedstore.KindDiscover {
		kind = feedstore.KindDiscover
	}
	t := &timeline{
		id:   int(timelineIDs.Add(1)),
		kind: kind,
		key:  r.Key(),
		spin: widgets.NewSpinner(),
		tabY: -1, ringY: -1, bannerY: -1,
	}
	t.list = widgets.NewList(func(it api.TimelineItem) string { return it.ID }, t.renderItem)
	return t
}

func (t *timeline) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init loads the first page unless one already filled the FeedStore window
// (items that only events brought do not count) and the story ring unless it
// is loaded, and marks both open for resync; discover arms its new-posts check.
func (t *timeline) Init(ctx *ui.Context) tea.Cmd {
	fs := t.fs(ctx)
	if fs == nil {
		return nil
	}
	fs.SetOpen("timeline:"+t.kind, true)
	fs.SetOpen("stories", true)
	var cmds []tea.Cmd
	if !fs.TimelineLoaded(t.kind) {
		cmds = append(cmds, t.loadFirst(ctx))
	}
	if fs.Stories() == nil {
		cmds = append(cmds, t.loadStories(ctx))
	}
	if t.kind == feedstore.KindDiscover {
		cmds = append(cmds, t.armPoll(ctx))
	}
	cmds = append(cmds, t.sync(ctx, true))
	return tea.Batch(cmds...)
}

func (t *timeline) fetch(kind string) func(*api.Client, context.Context, api.TimelineQuery) (*api.TimelinePage, error) {
	if kind == feedstore.KindDiscover {
		return (*api.Client).DiscoverTimeline
	}
	return (*api.Client).FollowingTimeline
}

func (t *timeline) loadFirst(ctx *ui.Context) tea.Cmd {
	t.loadingFirst = true
	t.retryOnShow = false
	t.quietFail = false
	get := t.fetch(t.kind)
	return tea.Batch(t.spin.Start(ctx), ui.Do(ctx, tagTimeline+t.kind, func(c context.Context, a *api.Client) (*api.TimelinePage, error) {
		return get(a, c, api.TimelineQuery{})
	}))
}

// retry re-requests a failed first page (nil unless the last one failed and
// none is in flight): the r key, being shown again, a mode change or resize,
// and a successful resync. Automatic retries (quiet) fail silently: the pane
// keeps its retry notice, and only the first failure and the user's r toast
// (offline, a toast per resync / resize step was noise).
func (t *timeline) retry(ctx *ui.Context, quiet bool) tea.Cmd {
	if !t.loadErr || t.loadingFirst {
		return nil
	}
	cmd := t.loadFirst(ctx)
	t.quietFail = quiet
	return cmd
}

// retryHint is "<text> · r <retry>" (feed.retry_hint; the retry key is post.reply's).
func retryHint(ctx *ui.Context, text string) string {
	k := "r"
	if ctx != nil && ctx.Keys != nil {
		if hk := ctx.Keys.Binding(keymap.PostReply).Help().Key; hk != "" {
			k = hk
		}
	}
	return cat(ctx).Feed.RetryHint(text, k)
}

// cursor is the FeedStore's next_cursor of the oldest loaded page (it outlives
// this instance).
func (t *timeline) cursor(ctx *ui.Context) *string {
	if fs := t.fs(ctx); fs != nil {
		return fs.TimelineCursor(t.kind)
	}
	return nil
}

func (t *timeline) loadMore(ctx *ui.Context) tea.Cmd {
	c := t.cursor(ctx)
	if c == nil || t.loadingMore || t.loadingFirst {
		return nil
	}
	t.loadingMore = true
	cur := *c
	get := t.fetch(t.kind)
	return tea.Batch(t.spin.Start(ctx), ui.Do(ctx, tagTimelineMore+t.kind, func(c context.Context, a *api.Client) (*api.TimelinePage, error) {
		return get(a, c, api.TimelineQuery{PageOpts: api.PageOpts{Cursor: cur}})
	}))
}

func (t *timeline) loadStories(ctx *ui.Context) tea.Cmd {
	t.storiesLoading = true
	return ui.Do(ctx, tagStories, func(c context.Context, a *api.Client) ([]api.StoryGroup, error) {
		return a.StoryFeed(c)
	})
}

// storiesFailed backs off after a failed story-ring load: a stale ring is
// refetched only once the next storiesBackoff delay is over (not from every
// Update, which spun offline and spent the RL_API budget on 429).
func (t *timeline) storiesFailed(ctx *ui.Context) tea.Cmd {
	d := storiesBackoff[min(t.storiesFails, len(storiesBackoff)-1)]
	t.storiesFails++
	t.storiesRetryAt = ctx.Now().Add(d)
	return ui.After(ctx, d, storiesRetryMsg{id: t.id})
}

// maybeMore loads the next page when the selection is within moreThreshold of the end.
func (t *timeline) maybeMore(ctx *ui.Context) tea.Cmd {
	if t.cursor(ctx) == nil || len(t.items) == 0 {
		return nil
	}
	if i := t.selectedIndex(); i >= len(t.items)-moreThreshold {
		return t.loadMore(ctx)
	}
	return nil
}

func (t *timeline) selectedIndex() int {
	it, ok := t.list.Selected()
	if !ok {
		return -1
	}
	for i, x := range t.items {
		if x.ID == it.ID {
			return i
		}
	}
	return -1
}

// ---- discover new-posts check ----

func (t *timeline) armPoll(ctx *ui.Context) tea.Cmd {
	t.pollGen++
	t.polling = true
	return ui.After(ctx, discoverPoll, pollMsg{id: t.id, gen: t.pollGen})
}

// shownRecently reports whether View (or Resize, which precedes it) ran within discoverVisible.
func (t *timeline) shownRecently(ctx *ui.Context) bool {
	return !t.lastViewAt.IsZero() && ctx.Now().Sub(t.lastViewAt) <= discoverVisible
}

// rearm restarts a stopped discover check once the screen is shown again.
func (t *timeline) rearm(ctx *ui.Context) tea.Cmd {
	if t.kind != feedstore.KindDiscover || t.polling || !t.shownRecently(ctx) {
		return nil
	}
	return t.armPoll(ctx)
}

func (t *timeline) onPoll(ctx *ui.Context, m pollMsg) tea.Cmd {
	if m.gen != t.pollGen {
		return nil
	}
	t.polling = false
	if !t.shownRecently(ctx) {
		return nil // stopped; rearm restarts it when the screen is shown again
	}
	var cmds []tea.Cmd
	if fs := t.fs(ctx); fs != nil {
		since := discoverSince(fs.Timeline(t.kind))
		get := t.fetch(t.kind)
		cmds = append(cmds, ui.Do(ctx, tagDiscoverNew+t.kind, func(c context.Context, a *api.Client) (*api.TimelinePage, error) {
			return get(a, c, api.TimelineQuery{Since: since, PageOpts: api.PageOpts{Limit: discoverLimit}})
		}))
	}
	return tea.Batch(append(cmds, t.armPoll(ctx))...)
}

// discoverSince is the since of a discover check: the 5 s overlap anchor, else
// (one item, or all within 5 s of the newest) the oldest item's ID, else (empty
// window) "" so the check asks for the newest page and counts all of it.
// countNew drops the overlap either way.
func discoverSince(w *store.Window[api.TimelineItem]) string {
	if since := w.OverlapAnchor(5 * time.Second); since != "" {
		return since
	}
	if items := w.Items(); len(items) > 0 {
		return items[0].ID
	}
	return ""
}

// countNew is the banner count of a since-check: items newer than the window's
// newest ID and not in the window (the overlap is not counted); gap => 100+.
func countNew(fs *feedstore.FeedStore, kind string, p *api.TimelinePage) int {
	if p.Gap {
		return bannerCap
	}
	w := fs.Timeline(kind)
	items := w.Items()
	newest := ""
	if len(items) > 0 {
		newest = items[len(items)-1].ID
	}
	n := 0
	for _, it := range p.Items {
		if _, have := w.Get(it.ID); it.ID > newest && !have {
			n++
		}
	}
	return n
}

func (t *timeline) bannerVisible(ctx *ui.Context) bool {
	fs := t.fs(ctx)
	return t.kind == feedstore.KindDiscover && fs != nil && fs.NewDiscoverCount() > 0
}

func (t *timeline) setBanner(sel bool) {
	if t.bannerSel != sel {
		t.bannerSel = sel
		t.redraw()
	}
}

// ---- Update ----

// forwardResync hands a feedstore.ResyncMsg to ApplyResync (applied once per
// Seq however many components forward it). Every ui/feed component calls it
// first in Update.
func forwardResync(ctx *ui.Context, msg tea.Msg) bool {
	m, ok := msg.(feedstore.ResyncMsg)
	if !ok {
		return false
	}
	if ctx != nil && ctx.Store != nil {
		if fs := feedstore.Get(ctx.Store); fs != nil {
			fs.ApplyResync(m)
		}
	}
	return true
}

func errCmd(ctx *ui.Context, err error) tea.Cmd {
	where := cat(ctx).Feed.ErrCtxFeed()
	return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: where} }
}

func (t *timeline) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	fs := t.fs(ctx)
	if fs == nil {
		return nil
	}
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	force := true
	imagesChanged := false
	var before map[string]bool
	switch m := msg.(type) {
	case pollMsg:
		force = false
		if m.id == t.id {
			cmds = append(cmds, t.onPoll(ctx, m))
		}
	case ui.ResultMsg[*api.TimelinePage]:
		cmds = append(cmds, t.onPage(ctx, fs, m))
	case ui.ResultMsg[[]api.StoryGroup]:
		if m.Tag == tagStories {
			t.storiesLoading = false
			if m.Err == nil {
				fs.SetStories(m.Value)
				t.storiesFails, t.storiesRetryAt = 0, time.Time{}
			} else {
				cmds = append(cmds, t.storiesFailed(ctx))
			}
		}
	case storiesRetryMsg:
		force = false // the stale-ring check below does the work
	case ui.ModeChangedMsg:
		imagesChanged = true
		cmds = append(cmds, t.retry(ctx, true))
	case ui.ThemeChangedMsg, ui.LocaleChangedMsg:
		imagesChanged = true
	case ui.FocusChangedMsg:
		if m.RouteKey == t.key {
			cmds = append(cmds, t.retry(ctx, true))
		}
	case ui.ResyncDoneMsg:
		if m.Err == nil { // a failed bootstrap (offline) retries itself every 10s
			cmds = append(cmds, t.retry(ctx, true))
		}
	case ui.EventMsg:
		if m.Event.Type == "timeline.item_created" {
			before = make(map[string]bool, len(t.items))
			for _, it := range t.items {
				before[it.ID] = true
			}
		}
	default:
		if rt := reflect.TypeOf(msg); rt != nil && rt.PkgPath() == widgetsPkg {
			force = false // spinner / fade ticks
		}
	}
	if t.retryOnShow {
		t.retryOnShow = false
		cmds = append(cmds, t.retry(ctx, true))
	}
	if id, _, ok := t.imgs.loaded(msg); ok {
		cmds = append(cmds, t.renderMedia(ctx, id))
	}
	cmds = append(cmds, t.spin.Update(ctx, msg), t.fade.Update(ctx, msg))
	if ctx.Now().Before(t.fadeUntil) {
		force = true
	}
	if fs.StoriesStale() && !t.storiesLoading && !ctx.Now().Before(t.storiesRetryAt) {
		cmds = append(cmds, t.loadStories(ctx))
	}
	fs.PruneStories(ctx.Now())
	cmds = append(cmds, t.rearm(ctx))
	if t.imgs.hasWants() {
		cmds = append(cmds, t.imgs.flush(ctx))
		force = true
	}
	cmds = append(cmds, t.sync(ctx, force))
	if imagesChanged {
		cmds = append(cmds, t.rerenderImages(ctx))
	}
	if before != nil {
		for _, it := range t.items {
			if !before[it.ID] {
				if c := t.fade.Start(ctx, it.ID); c != nil {
					t.fadeUntil = ctx.Now().Add(fadeSpan)
					cmds = append(cmds, c)
				}
			}
		}
	}
	return tea.Batch(cmds...)
}

// onPage applies a timeline page result of this timeline's kind.
func (t *timeline) onPage(ctx *ui.Context, fs *feedstore.FeedStore, m ui.ResultMsg[*api.TimelinePage]) tea.Cmd {
	switch m.Tag {
	case tagTimeline + t.kind:
		t.loadingFirst = false
		t.stopSpinner()
		if m.Err != nil || m.Value == nil {
			t.loadErr = true
			if t.quietFail {
				return nil // the retry notice is still up
			}
			return errCmd(ctx, m.Err)
		}
		t.loadErr = false
		fs.SetTimelinePage(t.kind, m.Value)
		if t.kind == feedstore.KindDiscover {
			fs.SetDiscoverNew(0)
		}
		if t.bannerSel {
			t.bannerSel = false
			t.selectFirst = true
		}
	case tagTimelineMore + t.kind:
		t.loadingMore = false
		t.stopSpinner()
		if m.Err != nil || m.Value == nil {
			return errCmd(ctx, m.Err)
		}
		fs.AddOlderTimeline(t.kind, m.Value)
	case tagDiscoverNew + t.kind:
		if m.Err == nil && m.Value != nil {
			fs.SetDiscoverNew(countNew(fs, t.kind, m.Value))
		}
	}
	return nil
}

func (t *timeline) stopSpinner() {
	if !t.loadingFirst && !t.loadingMore {
		t.spin.Stop()
	}
}

// build is the window's items newest first (by At, then ID), blocked authors
// and reposters skipped (Task 5 block filter).
func (t *timeline) build(ctx *ui.Context) []api.TimelineItem {
	fs := t.fs(ctx)
	if fs == nil {
		return nil
	}
	src := fs.Timeline(t.kind).Items()
	out := make([]api.TimelineItem, 0, len(src))
	for _, it := range src {
		if !hiddenByBlock(ctx, it) {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func sameIDs(a, b []api.TimelineItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// sync re-reads the window into the List. Unless force, nothing happens when
// the item IDs are unchanged. A list scrolled to the top stays at the top, so a
// newly inserted item shows.
func (t *timeline) sync(ctx *ui.Context, force bool) tea.Cmd {
	items := t.build(ctx)
	if !force && sameIDs(items, t.items) {
		return nil
	}
	a := t.list.Anchor()
	atTop := len(t.items) == 0 || a.ItemID == "" || (a.ItemID == t.items[0].ID && a.LineOffset == 0)
	t.items = items
	t.views = nil
	t.list.SetItems(items)
	if len(items) > 0 {
		switch {
		case t.pending != nil:
			t.list.Select(t.pending.Selected)
			t.list.Restore(t.pending.Anchor)
			t.pending = nil
		case t.selectFirst:
			t.selectFirst = false
			t.list.Select(items[0].ID)
			t.list.Restore(widgets.ScrollAnchor{ItemID: items[0].ID})
		case atTop:
			t.list.Restore(widgets.ScrollAnchor{ItemID: items[0].ID})
		}
	}
	return t.requestImages(ctx)
}

// redraw drops every cached render (List and cardViews) without re-reading the window.
func (t *timeline) redraw() {
	t.views = nil
	t.list.SetItems(t.items)
}

// post is the cached copy of it's post (in-place updates show), else it's own.
func (t *timeline) post(ctx *ui.Context, it api.TimelineItem) *api.Post {
	if fs := t.fs(ctx); fs != nil {
		if p := fs.Post(it.Post.ID); p != nil {
			return p
		}
	}
	p := it.Post
	return &p
}

// firstMedia is the media a card draws, if any.
func firstMedia(p *api.Post) (api.Media, bool) {
	if p == nil || p.Deleted || len(p.Media) == 0 {
		return api.Media{}, false
	}
	return p.Media[0], true
}

// requestImages loads the first image of each card on or near the screen when
// the density draws images, and forgets every other image (imageSlots.track).
func (t *timeline) requestImages(ctx *ui.Context) tea.Cmd {
	if d := ctx.Density(); d != ui.DensityFull && d != ui.DensityBubble {
		return t.imgs.track(ctx)
	}
	w, h := viewSize(t.size.W, t.listH)
	if t.listH <= 0 && t.size.H > 0 {
		h = t.size.H
	}
	sel := t.selectedID()
	near := nearItems(t.list, t.items, func(it api.TimelineItem) string { return it.ID }, h, func(it api.TimelineItem) int {
		return len(t.view(ctx, it, it.ID == sel && !t.bannerSel, w).Lines)
	})
	var ms []api.Media
	for i, it := range t.items {
		if !near[i] {
			continue
		}
		if m, ok := firstMedia(t.post(ctx, it)); ok {
			ms = append(ms, m)
		}
	}
	return t.imgs.track(ctx, ms...)
}

// renderMedia renders a newly arrived image for every card that shows it, so a
// kitty Setup leaves with this Update (Review Focus 6).
func (t *timeline) renderMedia(ctx *ui.Context, id string) tea.Cmd {
	var cmds []tea.Cmd
	for _, it := range t.items {
		if m, ok := firstMedia(t.post(ctx, it)); ok && m.ID == id {
			cols, rows := imageBox(m, t.size.W-gutterW, ctx.Density())
			cmds = append(cmds, t.imgs.render(ctx, m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// rerenderImages renders every loaded image for the current box and theme
// (after a mode or theme change).
func (t *timeline) rerenderImages(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	for _, it := range t.items {
		if m, ok := firstMedia(t.post(ctx, it)); ok && t.imgs.image(m.ID) != nil {
			cols, rows := imageBox(m, t.size.W-gutterW, ctx.Density())
			cmds = append(cmds, t.imgs.render(ctx, m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// ---- rendering ----

func (t *timeline) selectedID() string {
	if it, ok := t.list.Selected(); ok {
		return it.ID
	}
	return ""
}

// view is the cached card of it (the same render the List shows).
func (t *timeline) view(ctx *ui.Context, it api.TimelineItem, selected bool, width int) cardView {
	k := viewKey{id: it.ID, width: width, density: ctx.Density(), selected: selected, pollSel: -1}
	if v, ok := t.views[k]; ok {
		return v
	}
	v := renderCard(ctx, it, t.post(ctx, it), cardOpts{
		Width: width, Density: ctx.Density(), Selected: selected, PollSel: -1,
		Image: func(m api.Media, cols, rows int) []string { return t.imgs.cell(ctx, m, cols, rows) },
	})
	if t.views == nil {
		t.views = map[viewKey]cardView{}
	}
	t.views[k] = v
	return v
}

// renderItem is the List render callback.
func (t *timeline) renderItem(ctx *ui.Context, it api.TimelineItem, selected bool, width int) []string {
	sel := selected && !t.bannerSel
	v := t.view(ctx, it, sel, width)
	if sel || !ctx.Now().Before(t.fadeUntil) || width <= gutterW {
		return v.Lines
	}
	st := t.fade.Style(themeOf(ctx), it.ID)
	out := make([]string, len(v.Lines))
	for i, l := range v.Lines {
		if isRule(l) { // the closing rule has no gutter: keep it full width
			out[i] = l
			continue
		}
		out[i] = st.Render(strings.Repeat(" ", gutterW)) + ansi.Cut(l, gutterW, width)
	}
	return out
}

// viewsFn is cardAt's view lookup at the current list width and selection.
func (t *timeline) viewsFn(ctx *ui.Context) func(api.TimelineItem) cardView {
	sel := t.selectedID()
	return func(it api.TimelineItem) cardView {
		return t.view(ctx, it, it.ID == sel && !t.bannerSel, t.size.W)
	}
}

// cardAt finds the card drawn on list row y (0 = the list's first row) from the
// List's anchor: the anchor card starts LineOffset rows above the top, each card
// is as tall as its cardView. line is the row inside that card.
func cardAt(l *widgets.List[api.TimelineItem], items []api.TimelineItem, views func(api.TimelineItem) cardView, y int) (it api.TimelineItem, line int, ok bool) {
	if y < 0 || len(items) == 0 {
		return api.TimelineItem{}, 0, false
	}
	a := l.Anchor()
	start := 0
	for i, x := range items {
		if x.ID == a.ItemID {
			start = i
			break
		}
	}
	top := -a.LineOffset
	for i := start; i < len(items); i++ {
		h := max(len(views(items[i]).Lines), 1)
		if y < top+h {
			return items[i], y - top, true
		}
		top += h
	}
	return api.TimelineItem{}, 0, false
}

func (t *timeline) bannerLine(ctx *ui.Context, width int) string {
	th := themeOf(ctx)
	n := 0
	if fs := t.fs(ctx); fs != nil {
		n = fs.NewDiscoverCount()
	}
	text := cat(ctx).Feed.NewPostsBanner(n)
	if n >= bannerCap {
		text = cat(ctx).Feed.NewPostsBannerCap(bannerCap)
	}
	if t.bannerSel {
		return th.S.Base.Foreground(th.P.Acc).Render("▎") + th.S.Selected.Render(textwidth.PadRight(" "+text, max(width-1, 0)))
	}
	return fit(th, th.S.Base.Render("  ")+th.S.Base.Foreground(th.P.Acc).Bold(true).Render(text), width)
}

func (t *timeline) loadingLine(ctx *ui.Context, width int) string {
	th := themeOf(ctx)
	return fit(th, th.S.Base.Render("  ")+t.spin.View(th)+th.S.Dim.Render(" "+cat(ctx).Feed.Loading()), width)
}

func (t *timeline) View(ctx *ui.Context, _ bool) string {
	t.lastViewAt = ctx.Now()
	th := themeOf(ctx)
	w, h := t.size.W, t.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t.tabY, t.ringY, t.bannerY = -1, -1, -1
	t.tabHit, t.ringHit = nil, nil
	var out []string
	d := ctx.Density()
	if d != ui.DensityLine && d != ui.DensityMinimal {
		line, hit := feedTabs(ctx, t.kind, w)
		t.tabY, t.tabHit = len(out), hit
		out = append(out, line)
	}
	if fs := t.fs(ctx); fs != nil {
		fs.PruneStories(ctx.Now()) // an idle feed gets no Update when a story expires
	}
	if fs := t.fs(ctx); fs != nil && len(fs.Stories()) > 0 && d != ui.DensityMinimal {
		lines, hit := storyRing(ctx, fs.Stories(), w)
		t.ringY, t.ringHit = len(out), hit
		out = append(out, lines...)
	}
	if t.bannerVisible(ctx) {
		t.bannerY = len(out)
		out = append(out, t.bannerLine(ctx, w))
	} else {
		t.bannerSel = false
	}
	footer := 0
	if t.loadingMore {
		footer = 1
	}
	t.listY = len(out)
	t.listH = max(h-len(out)-footer, 0)
	t.visible = nil
	if t.listH > 0 {
		switch {
		case len(t.items) == 0 && t.loadingFirst:
			out = append(out, t.loadingLine(ctx, w))
		case len(t.items) == 0 && t.loadErr:
			out = append(out, fit(th, th.S.Base.Render("  ")+th.S.Error.Render(retryHint(ctx, cat(ctx).Feed.FeedLoadFailed())), w))
		case len(t.items) == 0:
			c := cat(ctx)
			title, hint := c.Feed.EmptyFollowing(), c.Feed.EmptyFollowingHint()
			if t.kind == feedstore.KindDiscover {
				title, hint = c.Feed.EmptyDiscover(), c.Feed.EmptyDiscoverHint()
			}
			out = append(out, strings.Split(widgets.Empty(th, ui.Size{W: w, H: t.listH}, "🌱", title, hint), "\n")...)
		default:
			out = append(out, strings.Split(t.list.View(ctx, ui.Size{W: w, H: t.listH}), "\n")...)
			t.visible = t.visibleTopics(ctx)
		}
	}
	for len(out) < t.listY+t.listH {
		out = append(out, fit(th, "", w))
	}
	if footer > 0 {
		out = append(out, t.loadingLine(ctx, w))
	}
	for len(out) < h {
		out = append(out, fit(th, "", w))
	}
	out = out[:h]
	for i, l := range out {
		out[i] = fit(th, l, w)
	}
	return strings.Join(out, "\n")
}

// visibleTopics is "post:<id>" of every card on screen (cardAt's walk from the anchor).
func (t *timeline) visibleTopics(ctx *ui.Context) []string {
	a := t.list.Anchor()
	views := t.viewsFn(ctx)
	start := 0
	for i, x := range t.items {
		if x.ID == a.ItemID {
			start = i
			break
		}
	}
	seen := map[string]bool{}
	var out []string
	for i, top := start, -a.LineOffset; i < len(t.items) && top < t.listH; i++ {
		if k := "post:" + t.items[i].Post.ID; !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
		top += max(len(views(t.items[i]).Lines), 1)
	}
	return out
}

func (t *timeline) Resize(ctx *ui.Context, size ui.Size) {
	t.size = size
	t.lastViewAt = ctx.Now() // the app resizes only placed (shown) instances
	t.retryOnShow = t.loadErr
}

// ---- input ----

func (t *timeline) navigate(r ui.Route) tea.Cmd { return ui.Navigate(r) }

// isPostKey reports the per-post keys (ignored while the banner is selected).
func isPostKey(ctx *ui.Context, msg tea.KeyPressMsg) bool {
	km := ctx.Keys
	for _, a := range []keymap.Action{keymap.PostLike, keymap.PostRepost, keymap.PostQuote, keymap.PostReply,
		keymap.PostOpenMedia, keymap.PostAuthor, keymap.MsgActions} {
		if km.Matches(msg, a) {
			return true
		}
	}
	return msg.String() == "#"
}

// openStories opens the first unseen group (not mine), else the first other group.
func (t *timeline) openStories(ctx *ui.Context) tea.Cmd {
	fs := t.fs(ctx)
	if fs == nil {
		return nil
	}
	var fallback string
	for _, g := range fs.Stories() {
		if ctx.Store.IsMe(g.User.ID) {
			continue
		}
		if g.HasUnseen {
			return t.navigate(ui.Route{Kind: ui.RouteStories, ID: g.User.ID})
		}
		if fallback == "" {
			fallback = g.User.ID
		}
	}
	if fallback != "" {
		return t.navigate(ui.Route{Kind: ui.RouteStories, ID: fallback})
	}
	return nil
}

func (t *timeline) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	s := msg.String()
	bannerVis := t.bannerVisible(ctx)
	if !bannerVis {
		t.bannerSel = false
	}
	if t.loadErr && len(t.items) == 0 && km.Matches(msg, keymap.PostReply) {
		return true, t.retry(ctx, false)
	}
	switch s {
	case "1":
		return true, t.navigate(ui.Route{Kind: ui.RouteFeed, Arg: feedstore.KindDiscover})
	case "2":
		return true, t.navigate(ui.Route{Kind: ui.RouteFeed, Arg: feedstore.KindFollowing})
	case "3":
		return true, t.navigate(ui.Route{Kind: ui.RouteTrends})
	case "s":
		return true, t.openStories(ctx)
	}
	if km.Matches(msg, keymap.PostCompose) {
		return true, t.navigate(ui.Route{Kind: ui.RouteCompose, Arg: "post"})
	}
	if km.Matches(msg, keymap.ListOpen) {
		if t.bannerSel {
			return true, t.loadFirst(ctx)
		}
		if it, ok := t.list.Selected(); ok {
			return true, t.navigate(ui.Route{Kind: ui.RoutePost, ID: it.Post.ID})
		}
		return true, nil
	}

	isTop := km.Matches(msg, keymap.ListTop)
	if s == "g" {
		isTop = t.gPending
		t.gPending = !t.gPending
	} else {
		t.gPending = false
	}
	if t.bannerSel {
		switch {
		case km.Matches(msg, keymap.ListDown):
			t.setBanner(false)
			if len(t.items) > 0 {
				t.list.Select(t.items[0].ID)
			}
			return true, nil
		case km.Matches(msg, keymap.ListUp), isTop, s == "g":
			if s == "g" {
				t.list.HandleKey(ctx, msg) // keep the List's "g g" state in step
			}
			return true, nil
		case isPostKey(ctx, msg):
			return true, nil
		}
	}
	if isPostKey(ctx, msg) {
		if it, ok := t.list.Selected(); ok {
			if handled, cmd := handlePostKey(ctx, msg, t.post(ctx, it)); handled {
				t.redraw() // optimistic like / repost changes show at once
				return true, cmd
			}
		}
	}
	if bannerVis && !t.bannerSel && km.Matches(msg, keymap.ListUp) && t.selectedIndex() <= 0 {
		t.setBanner(true)
		return true, nil
	}
	handled, cmd := t.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	switch {
	case bannerVis && isTop:
		t.setBanner(true)
	case t.bannerSel:
		t.setBanner(false)
	}
	return true, tea.Batch(cmd, t.maybeMore(ctx), t.rearm(ctx), t.requestImages(ctx))
}

func (t *timeline) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	mo := msg.Mouse()
	inList := local.Y >= t.listY && local.Y < t.listY+t.listH && len(t.items) > 0
	switch msg.(type) {
	case tea.MouseWheelMsg:
		if inList {
			t.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - t.listY})
			return tea.Batch(t.rearm(ctx), t.requestImages(ctx))
		}
		return nil
	case tea.MouseClickMsg:
	default:
		return nil
	}
	if mo.Button != tea.MouseLeft {
		return nil
	}
	switch y := local.Y; {
	case y == t.tabY && t.tabHit != nil:
		if r, ok := t.tabHit(local.X); ok {
			return t.navigate(r)
		}
	case y == t.ringY && t.ringHit != nil:
		if id, ok := t.ringHit(local.X); ok {
			if id == "" {
				return t.navigate(ui.Route{Kind: ui.RouteCompose, Arg: "story"})
			}
			return t.navigate(ui.Route{Kind: ui.RouteStories, ID: id})
		}
	case y == t.bannerY:
		t.setBanner(true)
	case inList:
		ly := y - t.listY
		views := t.viewsFn(ctx)
		if it, line, ok := cardAt(t.list, t.items, views, ly); ok {
			for _, hit := range views(it).Hits {
				if hit.Line == line && local.X >= hit.X0 && local.X < hit.X1 {
					return handlePostHit(ctx, hit, t.post(ctx, it))
				}
			}
			// a click on the selected card opens it, like enter (CA 5.5)
			if sel, ok := t.list.Selected(); ok && !t.bannerSel && sel.ID == it.ID {
				return t.navigate(ui.Route{Kind: ui.RoutePost, ID: it.Post.ID})
			}
		}
		t.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: ly})
		t.setBanner(false)
		return tea.Batch(t.maybeMore(ctx), t.rearm(ctx), t.requestImages(ctx))
	}
	return nil
}

// ---- optional capabilities ----

func (t *timeline) Title(ctx *ui.Context) (string, ui.Badge) {
	return cat(ctx).Feed.FeedTitle(), ui.Badge{}
}

func (t *timeline) Help(ctx *ui.Context) []key.Binding {
	relabel := func(a keymap.Action, desc string) key.Binding {
		b := ctx.Keys.Binding(a)
		b.SetHelp(b.Help().Key, desc)
		return b
	}
	c := cat(ctx)
	if t.loadErr && len(t.items) == 0 {
		return []key.Binding{relabel(keymap.PostReply, c.Feed.HelpRetry()), relabel(keymap.PostCompose, c.Feed.HelpNewPost())}
	}
	return []key.Binding{
		relabel(keymap.PostLike, c.Feed.HelpLike()),
		relabel(keymap.PostRepost, c.Feed.HelpRepost()),
		relabel(keymap.PostReply, c.Feed.HelpReply()),
		relabel(keymap.PostCompose, c.Feed.HelpNewPost()),
	}
}

func (t *timeline) WatchTopics() []string { return append([]string(nil), t.visible...) }

func (t *timeline) SaveState() any {
	return timelineState{Selected: t.selectedID(), Anchor: t.list.Anchor()}
}

func (t *timeline) LoadState(v any) {
	if s, ok := v.(timelineState); ok {
		t.pending = &s
	}
}
