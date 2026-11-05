package feed

import (
	"context"
	"reflect"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteHashtag, newHashtag)
}

// Hashtag copy.
const (
	textHashtagEmpty = "이 태그의 글이 아직 없어요"
	iconHashtagEmpty = "#"
)

// Hashtag result tags (ui.Do).
const (
	tagHashtag     = "feed.hashtag:"      // + tag: first page
	tagHashtagMore = "feed.hashtag_more:" // + tag: next page
)

// ---- cardList: a per-screen card list ----

// cardList is a per-screen list of post cards (hashtag feed, post search):
// the server items in server order (raw) and the shown ones (blocked authors
// and reposters dropped, Task 5 block filter). Cards draw the FeedStore copy of
// each post, so in-place updates (likes, edits, deletes) show.
type cardList struct {
	raw     []api.TimelineItem
	items   []api.TimelineItem
	list    *widgets.List[api.TimelineItem]
	views   map[viewKey]cardView
	imgs    *imageSlots
	noSel   bool // draw no selection (the owner's focus is elsewhere)
	width   int  // width of the last View
	height  int  // height of the last View
	visible []string
}

func newCardList(imgs *imageSlots) *cardList {
	l := &cardList{imgs: imgs}
	l.list = widgets.NewList(func(it api.TimelineItem) string { return it.ID }, l.renderItem)
	return l
}

// postItem wraps a post as a timeline item (the hashtag and search endpoints
// answer posts).
func postItem(p api.Post) api.TimelineItem {
	return api.TimelineItem{ID: p.ID, Type: "post", Post: p, At: p.CreatedAt}
}

// setPosts replaces (more = false) or extends (more = true, duplicates dropped)
// raw with ps and caches the posts in the FeedStore.
func (l *cardList) setPosts(ctx *ui.Context, ps []api.Post, more bool) {
	if ctx != nil && ctx.Store != nil {
		if fs := feedstore.Get(ctx.Store); fs != nil {
			fs.ObservePosts(ps...)
		}
	}
	if !more {
		l.raw = l.raw[:0:0]
	}
	seen := make(map[string]bool, len(l.raw))
	for _, it := range l.raw {
		seen[it.ID] = true
	}
	for _, p := range ps {
		if !seen[p.ID] {
			seen[p.ID] = true
			l.raw = append(l.raw, postItem(p))
		}
	}
}

// clear drops every item.
func (l *cardList) clear() {
	l.raw, l.items, l.views, l.visible = nil, nil, nil, nil
	l.list.SetItems(nil)
}

// sync re-reads raw into the List minus blocked authors (unless force, only
// when the IDs changed); a list that was empty starts at the top.
func (l *cardList) sync(ctx *ui.Context, force bool) {
	items := make([]api.TimelineItem, 0, len(l.raw))
	for _, it := range l.raw {
		if !hiddenByBlock(ctx, it) {
			items = append(items, it)
		}
	}
	if !force && sameIDs(items, l.items) {
		return
	}
	wasEmpty := len(l.items) == 0
	l.items = items
	l.views = nil
	l.list.SetItems(items)
	if wasEmpty && len(items) > 0 {
		l.list.Restore(widgets.ScrollAnchor{ItemID: items[0].ID})
	}
}

// redraw drops the cached renders without re-reading the items.
func (l *cardList) redraw() {
	l.views = nil
	l.list.SetItems(l.items)
}

// setNoSel turns the selection highlight off (on) and redraws when it changed.
func (l *cardList) setNoSel(v bool) {
	if l.noSel != v {
		l.noSel = v
		l.redraw()
	}
}

// post is the cached copy of it's post, else it's own.
func (l *cardList) post(ctx *ui.Context, it api.TimelineItem) *api.Post {
	if ctx != nil && ctx.Store != nil {
		if fs := feedstore.Get(ctx.Store); fs != nil {
			if p := fs.Post(it.Post.ID); p != nil {
				return p
			}
		}
	}
	p := it.Post
	return &p
}

func (l *cardList) selected() (api.TimelineItem, bool) { return l.list.Selected() }

func (l *cardList) selectedID() string {
	if it, ok := l.list.Selected(); ok {
		return it.ID
	}
	return ""
}

func (l *cardList) selectedIndex() int {
	id := l.selectedID()
	for i, it := range l.items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// nearEnd reports a selection within moreThreshold of the end.
func (l *cardList) nearEnd() bool {
	i := l.selectedIndex()
	return len(l.items) > 0 && i >= len(l.items)-moreThreshold
}

func (l *cardList) view(ctx *ui.Context, it api.TimelineItem, selected bool, width int) cardView {
	selected = selected && !l.noSel
	k := viewKey{id: it.ID, width: width, density: ctx.Density(), selected: selected, pollSel: -1}
	if v, ok := l.views[k]; ok {
		return v
	}
	v := renderCard(ctx, it, l.post(ctx, it), cardOpts{
		Width: width, Density: ctx.Density(), Selected: selected, PollSel: -1,
		Image: func(m api.Media, cols, rows int) []string { return l.imgs.cell(ctx, m, cols, rows) },
	})
	if l.views == nil {
		l.views = map[viewKey]cardView{}
	}
	l.views[k] = v
	return v
}

func (l *cardList) renderItem(ctx *ui.Context, it api.TimelineItem, selected bool, width int) []string {
	return l.view(ctx, it, selected, width).Lines
}

func (l *cardList) viewsFn(ctx *ui.Context) func(api.TimelineItem) cardView {
	sel := l.selectedID()
	return func(it api.TimelineItem) cardView { return l.view(ctx, it, it.ID == sel, l.width) }
}

// View draws the list in w x h and records the cards on screen.
func (l *cardList) View(ctx *ui.Context, w, h int) []string {
	l.width, l.height = w, h
	l.visible = nil
	if w <= 0 || h <= 0 {
		return nil
	}
	out := strings.Split(l.list.View(ctx, ui.Size{W: w, H: h}), "\n")
	l.visible = l.visibleTopics(ctx)
	return out
}

// visibleTopics is "post:<id>" of every card on screen.
func (l *cardList) visibleTopics(ctx *ui.Context) []string {
	a := l.list.Anchor()
	views := l.viewsFn(ctx)
	start := 0
	for i, x := range l.items {
		if x.ID == a.ItemID {
			start = i
			break
		}
	}
	seen := map[string]bool{}
	var out []string
	for i, top := start, -a.LineOffset; i < len(l.items) && top < l.height; i++ {
		if k := "post:" + l.items[i].Post.ID; !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
		top += max(len(views(l.items[i]).Lines), 1)
	}
	return out
}

// media is the first image of every card when the density draws images.
func (l *cardList) media(ctx *ui.Context) []api.Media {
	if d := ctx.Density(); d != ui.DensityFull && d != ui.DensityBubble {
		return nil
	}
	var out []api.Media
	for _, it := range l.items {
		if m, ok := firstMedia(l.post(ctx, it)); ok {
			out = append(out, m)
		}
	}
	return out
}

// requestImages loads the first image of each card on or near the screen (as
// of the last View's size) and forgets every other image (imageSlots.track).
func (l *cardList) requestImages(ctx *ui.Context) tea.Cmd {
	if d := ctx.Density(); d != ui.DensityFull && d != ui.DensityBubble {
		return l.imgs.track(ctx)
	}
	w, h := viewSize(l.width, l.height)
	sel := l.selectedID()
	near := nearItems(l.list, l.items, func(it api.TimelineItem) string { return it.ID }, h, func(it api.TimelineItem) int {
		return len(l.view(ctx, it, it.ID == sel, w).Lines)
	})
	var ms []api.Media
	for i, it := range l.items {
		if !near[i] {
			continue
		}
		if m, ok := firstMedia(l.post(ctx, it)); ok {
			ms = append(ms, m)
		}
	}
	return l.imgs.track(ctx, ms...)
}

// renderMedia renders a newly arrived image for every card showing it.
func (l *cardList) renderMedia(ctx *ui.Context, id string, width int) tea.Cmd {
	var cmds []tea.Cmd
	for _, m := range l.media(ctx) {
		if m.ID == id {
			cols, rows := imageBox(m, width-gutterW, ctx.Density())
			cmds = append(cmds, l.imgs.render(ctx, m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// rerenderImages renders every loaded image for the current box and theme.
func (l *cardList) rerenderImages(ctx *ui.Context, width int) tea.Cmd {
	var cmds []tea.Cmd
	for _, m := range l.media(ctx) {
		if l.imgs.image(m.ID) != nil {
			cols, rows := imageBox(m, width-gutterW, ctx.Density())
			cmds = append(cmds, l.imgs.render(ctx, m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// handleKey is enter (RoutePost), the per-post keys and list navigation.
func (l *cardList) handleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		if it, ok := l.list.Selected(); ok {
			return true, ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: it.Post.ID})
		}
		return true, nil
	}
	if isPostKey(ctx, msg) {
		if it, ok := l.list.Selected(); ok {
			if handled, cmd := handlePostKey(ctx, msg, l.post(ctx, it)); handled {
				l.redraw() // optimistic like / repost changes show at once
				return true, cmd
			}
		}
	}
	handled, cmd := l.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, l.requestImages(ctx))
}

// handleMouse is a wheel or a click at list row local.Y: a card region runs its
// action, a click elsewhere on the selected card opens it (RoutePost, like
// enter; CA 5.5) when canOpen, and any other row selects the card. canOpen is
// false while the selection is not shown as such (trends: the search input
// has the focus).
func (l *cardList) handleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point, canOpen bool) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		l.list.HandleMouse(ctx, msg, local)
		return l.requestImages(ctx)
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
	default:
		return nil
	}
	views := l.viewsFn(ctx)
	if it, line, ok := cardAt(l.list, l.items, views, local.Y); ok {
		for _, hit := range views(it).Hits {
			if hit.Line == line && local.X >= hit.X0 && local.X < hit.X1 {
				return handlePostHit(ctx, hit, l.post(ctx, it))
			}
		}
		if sel, ok := l.list.Selected(); ok && canOpen && sel.ID == it.ID {
			return ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: it.Post.ID})
		}
	}
	l.list.HandleMouse(ctx, msg, local)
	return l.requestImages(ctx)
}

// ---- RouteHashtag ----

// hashtag is RouteHashtag: the posts of one tag as cards, newest first in
// server order, with infinite scroll.
type hashtag struct {
	tag   string
	cards *cardList
	imgs  imageSlots
	spin  *widgets.Spinner
	size  ui.Size

	cursor      *string
	loading     bool
	loadingMore bool
	loaded      bool
	err         error

	listY, listH int // geometry of the last View (-1 / 0 = not drawn)
}

var (
	_ ui.Component    = (*hashtag)(nil)
	_ ui.KeyHandler   = (*hashtag)(nil)
	_ ui.MouseHandler = (*hashtag)(nil)
	_ ui.Titled       = (*hashtag)(nil)
	_ ui.Helper       = (*hashtag)(nil)
	_ ui.Watcher      = (*hashtag)(nil)
)

// newHashtag builds RouteHashtag (ID = tag without '#').
func newHashtag(_ *ui.Context, r ui.Route) ui.Component {
	h := &hashtag{tag: strings.TrimPrefix(r.ID, "#"), spin: widgets.NewSpinner(), listY: -1}
	h.cards = newCardList(&h.imgs)
	return h
}

func (h *hashtag) Init(ctx *ui.Context) tea.Cmd { return h.load(ctx, "") }

// load requests a page (cursor "" = the first page, replacing the list).
func (h *hashtag) load(ctx *ui.Context, cursor string) tea.Cmd {
	if h.loading || h.loadingMore {
		return nil
	}
	tag, label := tagHashtag+h.tag, h.tag
	if cursor != "" {
		h.loadingMore, tag = true, tagHashtagMore+h.tag
	} else {
		h.loading = true
	}
	return tea.Batch(h.spin.Start(ctx), ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.Post], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.HashtagPosts(c, label, api.PageOpts{Cursor: cursor})
	}))
}

func (h *hashtag) maybeMore(ctx *ui.Context) tea.Cmd {
	if h.cursor == nil || !h.cards.nearEnd() {
		return nil
	}
	return h.load(ctx, *h.cursor)
}

func (h *hashtag) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	force := true
	imagesChanged := false
	switch m := msg.(type) {
	case ui.ResultMsg[*api.Page[api.Post]]:
		cmds = append(cmds, h.onPage(ctx, m))
	case ui.ModeChangedMsg, ui.ThemeChangedMsg:
		imagesChanged = true
	default:
		if rt := reflect.TypeOf(msg); rt != nil && rt.PkgPath() == widgetsPkg {
			force = false // spinner ticks
		}
	}
	if id, _, ok := h.imgs.loaded(msg); ok {
		cmds = append(cmds, h.cards.renderMedia(ctx, id, h.size.W))
	}
	cmds = append(cmds, h.spin.Update(ctx, msg))
	if h.imgs.hasWants() {
		cmds = append(cmds, h.imgs.flush(ctx))
		force = true
	}
	h.cards.sync(ctx, force)
	cmds = append(cmds, h.cards.requestImages(ctx))
	if imagesChanged {
		cmds = append(cmds, h.cards.rerenderImages(ctx, h.size.W))
	}
	return tea.Batch(cmds...)
}

func (h *hashtag) onPage(ctx *ui.Context, m ui.ResultMsg[*api.Page[api.Post]]) tea.Cmd {
	more := false
	switch m.Tag {
	case tagHashtag + h.tag:
		if !h.loading {
			return nil
		}
		h.loading = false
	case tagHashtagMore + h.tag:
		if !h.loadingMore {
			return nil
		}
		h.loadingMore, more = false, true
	default:
		return nil
	}
	if !h.loading && !h.loadingMore {
		h.spin.Stop()
	}
	if m.Err != nil || m.Value == nil {
		if !more {
			h.err, h.loaded = m.Err, true
		}
		return errCmd(m.Err)
	}
	h.err, h.loaded = nil, true
	h.cards.setPosts(ctx, m.Value.Items, more)
	h.cursor = m.Value.NextCursor
	if len(m.Value.Items) == 0 {
		h.cursor = nil // an empty page never loops
	}
	return nil
}

func (h *hashtag) loadingLine(ctx *ui.Context, w int) string {
	t := themeOf(ctx)
	return fit(t, t.S.Base.Render("  ")+h.spin.View(t)+t.S.Dim.Render(" "+textLoading), w)
}

func (h *hashtag) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, ht := h.size.W, h.size.H
	if w <= 0 || ht <= 0 {
		return ""
	}
	h.listY, h.listH = -1, 0
	h.cards.visible = nil
	var out []string
	switch items := h.cards.items; {
	case len(items) == 0 && (h.loading || !h.loaded):
		out = append(out, h.loadingLine(ctx, w))
	case len(items) == 0 && h.err != nil:
		out = strings.Split(widgets.Empty(t, h.size, "⚠", textwidth.Sanitize(api.Describe(h.err)), ""), "\n")
	case len(items) == 0:
		out = strings.Split(widgets.Empty(t, h.size, iconHashtagEmpty, textHashtagEmpty, ""), "\n")
	default:
		footer := 0
		if h.loadingMore {
			footer = 1
		}
		h.listY, h.listH = 0, max(ht-footer, 0)
		out = append(out, h.cards.View(ctx, w, h.listH)...)
		if footer > 0 {
			out = append(out, h.loadingLine(ctx, w))
		}
	}
	return blankRows(t, out, w, ht)
}

func (h *hashtag) Resize(_ *ui.Context, size ui.Size) { h.size = size }

func (h *hashtag) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if ctx.Keys.Matches(msg, keymap.PostCompose) {
		return true, ui.Navigate(ui.Route{Kind: ui.RouteCompose, Arg: "post"})
	}
	if len(h.cards.items) == 0 {
		return false, nil
	}
	handled, cmd := h.cards.handleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, h.maybeMore(ctx))
}

func (h *hashtag) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if h.listY < 0 || local.Y < h.listY || local.Y >= h.listY+h.listH {
		return nil
	}
	cmd := h.cards.handleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - h.listY}, true)
	return tea.Batch(cmd, h.maybeMore(ctx))
}

// Title is "#tag".
func (h *hashtag) Title(*ui.Context) (string, ui.Badge) {
	return "#" + textwidth.Sanitize(h.tag), ui.Badge{}
}

func (h *hashtag) Help(ctx *ui.Context) []key.Binding {
	relabel := func(a keymap.Action, desc string) key.Binding {
		b := ctx.Keys.Binding(a)
		b.SetHelp(b.Help().Key, desc)
		return b
	}
	return []key.Binding{
		relabel(keymap.PostLike, "좋아요"),
		relabel(keymap.PostRepost, "리포스트"),
		relabel(keymap.PostReply, "답글"),
		relabel(keymap.ListOpen, "글 열기"),
	}
}

func (h *hashtag) WatchTopics() []string { return append([]string(nil), h.cards.visible...) }
