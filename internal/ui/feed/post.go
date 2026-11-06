package feed

import (
	"context"
	"reflect"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RoutePost, newPost)
}

// Post detail tuning.
const (
	maxReplyDepth = 6  // deepest reply level drawn as a card; deeper => "↳ 이어서 보기"
	treeIndent    = 3  // cells per reply level ("├─ ", "│  ")
	minReplyCardW = 10 // the tree prefix shrinks before a reply card gets narrower than this
)

// Post detail copy.
const (
	textPostTitle      = "글"
	textMoreReplies    = "답글 더 보기"
	textContinue       = "↳ 이어서 보기"
	textNoReplies      = "아직 답글이 없어요 · r 로 답글 쓰기"
	textPostLoadFailed = "글을 불러오지 못했어요"
	hintPostLoadFailed = "esc 로 돌아가기"
	errContextPost     = "글"
)

// Post detail result tags (ui.Do).
const (
	tagPostContext = "feed.post_context:" // + post id
	tagReplies     = "feed.replies:"      // + parent id: first page (replaces the window)
	tagRepliesMore = "feed.replies_more:" // + parent id: next page (appended)
)

// rowKind is what a post detail row shows.
type rowKind int

const (
	rowAncestor rowKind = iota // one line of the ancestor chain (root first)
	rowMain                    // the post itself, with the "── 답글 N ──" rule
	rowReply                   // a reply card in the tree
	rowMore                    // "답글 더 보기": the next page of ParentID's replies
	rowDeep                    // "↳ 이어서 보기": opens PostID (a reply at maxReplyDepth) as its own RoutePost
)

// treeRow is one row of the post detail list.
type treeRow struct {
	Kind     rowKind
	PostID   string // ancestor / main / reply post; rowDeep: the reply it continues; rowMore: ""
	Depth    int    // reply level (1 = direct reply of the main post); 0 for ancestors and main
	ParentID string // reply / more: the parent whose window holds it; deep: = PostID
	Cursor   string // rowMore: next_cursor of ParentID's replies ("" = unknown: refetch the first page)
	Lasts    []bool // per level 1..Depth: whether that level's node is its parent's last row (tree lines)
}

// key is the row's List item ID.
func (r treeRow) key() string {
	switch r.Kind {
	case rowAncestor:
		return "a:" + r.PostID
	case rowMain:
		return "m:" + r.PostID
	case rowMore:
		return "more:" + r.ParentID
	case rowDeep:
		return "deep:" + r.PostID
	}
	return "r:" + r.PostID
}

// flattenTree is the main post rootID followed by its reply tree, depth first in
// window (ID ascending) order: the replies of rootID always, those of a reply
// only while expanded. A level with more pages ends with a rowMore; a reply at
// maxReplyDepth that has replies is followed by a rowDeep instead of its subtree.
// Ancestors and the block filter are the caller's.
func flattenTree(f *feedstore.FeedStore, rootID string, expanded map[string]bool) []treeRow {
	rows := []treeRow{{Kind: rowMain, PostID: rootID}}
	if f == nil {
		return rows
	}
	var walk func(parent string, depth int, lasts []bool)
	walk = func(parent string, depth int, lasts []bool) {
		if !f.HasReplies(parent) {
			return
		}
		w := f.Replies(parent)
		items := w.Items()
		more := w.HasMoreAfter()
		for i, p := range items {
			l := appendLast(lasts, i == len(items)-1 && !more)
			rows = append(rows, treeRow{Kind: rowReply, PostID: p.ID, Depth: depth, ParentID: parent, Lasts: l})
			if depth >= maxReplyDepth {
				if hasReplies(f, p) {
					rows = append(rows, treeRow{Kind: rowDeep, PostID: p.ID, Depth: depth + 1, ParentID: p.ID, Lasts: appendLast(l, true)})
				}
				continue
			}
			if expanded[p.ID] {
				walk(p.ID, depth+1, l)
			}
		}
		if more {
			rows = append(rows, treeRow{Kind: rowMore, Depth: depth, ParentID: parent, Lasts: appendLast(lasts, true)})
		}
	}
	walk(rootID, 1, nil)
	return rows
}

func appendLast(lasts []bool, last bool) []bool {
	out := make([]bool, len(lasts), len(lasts)+1)
	copy(out, lasts)
	return append(out, last)
}

// hasReplies reports whether reply p has replies of its own (reply_count, or a
// loaded non-empty window).
func hasReplies(f *feedstore.FeedStore, p api.Post) bool {
	if c := f.Post(p.ID); c != nil {
		p = *c
	}
	return p.ReplyCount > 0 || (f.HasReplies(p.ID) && f.Replies(p.ID).Len() > 0)
}

// treePrefix is the tree lines of a row at len(lasts) levels: the first line's
// ("│  ├─ ") and the continuation lines' ("│  │  ").
func treePrefix(lasts []bool) (first, cont string) {
	if len(lasts) == 0 {
		return "", ""
	}
	var b strings.Builder
	for _, last := range lasts[:len(lasts)-1] {
		if last {
			b.WriteString("   ")
		} else {
			b.WriteString("│  ")
		}
	}
	if lasts[len(lasts)-1] {
		return b.String() + "└─ ", b.String() + "   "
	}
	return b.String() + "├─ ", b.String() + "│  "
}

// rowView is a rendered row: Lines exactly the list width, Hits relative to the
// row's card, which starts DX cells from the row's left edge.
type rowView struct {
	Lines []string
	Hits  []cardHit
	DX    int
}

// postView is RoutePost: the ancestor chain, the post and its expandable reply tree.
type postView struct {
	id string

	list      *widgets.List[treeRow]
	rows      []treeRow
	ancestors []string
	expanded  map[string]bool   // reply id -> its subtree is shown
	cursors   map[string]string // parent id -> next_cursor of its replies
	loading   map[string]bool   // parent id -> a replies page is in flight (requested here)
	views     map[viewKey]rowView
	imgs      imageSlots
	spin      *widgets.Spinner
	size      ui.Size

	ctxLoading    bool
	loadFailed    bool // the post could not be loaded and is not cached
	notFound      bool // 404: the post is gone
	repliesLoaded bool // the main post's first reply page arrived
	noReplies     bool
	touched       bool // the user moved the selection / scrolled (stop re-placing the view)
	visible       []string
}

var (
	_ ui.Component    = (*postView)(nil)
	_ ui.KeyHandler   = (*postView)(nil)
	_ ui.MouseHandler = (*postView)(nil)
	_ ui.Titled       = (*postView)(nil)
	_ ui.Helper       = (*postView)(nil)
	_ ui.Watcher      = (*postView)(nil)
)

// newPost builds RoutePost (ID = post id).
func newPost(_ *ui.Context, r ui.Route) ui.Component {
	v := &postView{
		id:       r.ID,
		expanded: map[string]bool{},
		cursors:  map[string]string{},
		loading:  map[string]bool{},
		spin:     widgets.NewSpinner(),
	}
	v.list = widgets.NewList(treeRow.key, v.renderRow)
	return v
}

func (v *postView) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init marks the post open for resync and loads its context and first reply page.
func (v *postView) Init(ctx *ui.Context) tea.Cmd {
	fs := v.fs(ctx)
	if fs == nil {
		return nil
	}
	fs.SetOpen("post:"+v.id, true)
	return tea.Batch(v.loadContext(ctx), v.loadReplies(ctx, v.id, ""), v.sync(ctx, true))
}

func postErr(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: errContextPost} }
}

func (v *postView) loadContext(ctx *ui.Context) tea.Cmd {
	v.ctxLoading = true
	id := v.id
	return tea.Batch(v.spin.Start(ctx), ui.Do(ctx, tagPostContext+id, func(c context.Context, a *api.Client) (*api.PostContext, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.GetPostContext(c, id)
	}))
}

// loadReplies requests a page of parent's replies (cursor "" = the first page,
// which replaces the window). One request per parent at a time.
func (v *postView) loadReplies(ctx *ui.Context, parent, cursor string) tea.Cmd {
	if v.loading[parent] {
		return nil
	}
	v.loading[parent] = true
	tag := tagReplies + parent
	if cursor != "" {
		tag = tagRepliesMore + parent
	}
	return ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.Post], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListReplies(c, parent, api.PageOpts{Cursor: cursor})
	})
}

func (v *postView) setCursor(parent string, next *string) {
	if next == nil || *next == "" {
		delete(v.cursors, parent)
		return
	}
	v.cursors[parent] = *next
}

// ---- Update ----

func (v *postView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if forwardResync(ctx, msg) {
		m := msg.(feedstore.ResyncMsg)
		if m.Key == "post:"+v.id && m.Replies != nil {
			v.setCursor(v.id, m.Replies.NextCursor)
			v.repliesLoaded = true
		}
		if parent, ok := feedstore.RepliesResyncParent(m); ok && m.Replies != nil {
			v.setCursor(parent, m.Replies.NextCursor) // the window is its first page again
		}
	}
	fs := v.fs(ctx)
	if fs == nil {
		return nil
	}
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	force := true
	imagesChanged := false
	switch m := msg.(type) {
	case ui.ResultMsg[*api.PostContext]:
		cmds = append(cmds, v.onContext(fs, m))
	case ui.ResultMsg[*api.Page[api.Post]]:
		cmds = append(cmds, v.onReplies(fs, m))
	case ui.ModeChangedMsg, ui.ThemeChangedMsg:
		imagesChanged = true
	default:
		if rt := reflect.TypeOf(msg); rt != nil && rt.PkgPath() == widgetsPkg {
			force = false // spinner ticks
		}
	}
	if id, _, ok := v.imgs.loaded(msg); ok {
		cmds = append(cmds, v.renderMedia(ctx, id))
	}
	cmds = append(cmds, v.spin.Update(ctx, msg))
	if v.imgs.hasWants() {
		cmds = append(cmds, v.imgs.flush(ctx))
		force = true
	}
	cmds = append(cmds, v.sync(ctx, force))
	if imagesChanged {
		cmds = append(cmds, v.rerenderImages(ctx))
	}
	return tea.Batch(cmds...)
}

// onContext caches the post and its ancestors. A 404 turns the post into the
// deleted placeholder.
func (v *postView) onContext(fs *feedstore.FeedStore, m ui.ResultMsg[*api.PostContext]) tea.Cmd {
	if m.Tag != tagPostContext+v.id || !v.ctxLoading {
		return nil
	}
	v.ctxLoading = false
	v.spin.Stop()
	if m.Err != nil || m.Value == nil {
		if api.IsCode(m.Err, "not_found") {
			v.notFound = true
			fs.MarkDeleted(v.id)
			return nil
		}
		if fs.Post(v.id) == nil {
			v.loadFailed = true
		}
		return postErr(m.Err)
	}
	fs.ObservePosts(m.Value.Ancestors...)
	fs.ObservePosts(m.Value.Post)
	v.ancestors = v.ancestors[:0]
	for _, a := range m.Value.Ancestors {
		if a.ID != "" && a.ID != v.id {
			v.ancestors = append(v.ancestors, a.ID)
		}
	}
	v.loadFailed = false
	return nil
}

// onReplies stores a replies page this instance requested.
func (v *postView) onReplies(fs *feedstore.FeedStore, m ui.ResultMsg[*api.Page[api.Post]]) tea.Cmd {
	var parent string
	more := false
	switch {
	case strings.HasPrefix(m.Tag, tagRepliesMore):
		parent, more = strings.TrimPrefix(m.Tag, tagRepliesMore), true
	case strings.HasPrefix(m.Tag, tagReplies):
		parent = strings.TrimPrefix(m.Tag, tagReplies)
	default:
		return nil
	}
	if !v.loading[parent] {
		return nil // another instance's request: the FeedStore window is shared
	}
	delete(v.loading, parent)
	if m.Err != nil || m.Value == nil {
		return postErr(m.Err)
	}
	page := m.Value
	if more {
		fs.ObservePosts(page.Items...)
		fs.Replies(parent).AppendNewer(page.Items, page.NextCursor != nil)
	} else {
		fs.SetRepliesPage(parent, page) // keeps post.reply_created replies that raced it
	}
	v.setCursor(parent, page.NextCursor)
	if parent == v.id {
		v.repliesLoaded = true
	}
	return nil
}

// post is the cached post id, else the copy in its parent's reply window.
func (v *postView) post(fs *feedstore.FeedStore, id, parent string) *api.Post {
	if fs == nil {
		return nil
	}
	if p := fs.Post(id); p != nil {
		return p
	}
	if parent != "" && fs.HasReplies(parent) {
		if p, ok := fs.Replies(parent).Get(id); ok {
			return &p
		}
	}
	return nil
}

// rowPost is the post a row's post keys act on (nil for more / continue rows).
func (v *postView) rowPost(ctx *ui.Context, r treeRow) *api.Post {
	fs := v.fs(ctx)
	switch r.Kind {
	case rowMain:
		if p := v.post(fs, v.id, ""); p != nil {
			return p
		}
		if v.notFound {
			return &api.Post{ID: v.id, Deleted: true}
		}
		return nil
	case rowAncestor, rowReply:
		return v.post(fs, r.PostID, r.ParentID)
	}
	return nil
}

func (v *postView) blocked(ctx *ui.Context, p *api.Post) bool {
	return p != nil && ctx != nil && ctx.Store != nil && ctx.Store.IsBlocked(p.Author.ID)
}

// build is ancestors + flattenTree, minus replies (with their subtrees) whose
// author I blocked; rowMore rows get their cursor. Empty while the post is not
// loaded yet.
func (v *postView) build(ctx *ui.Context) []treeRow {
	fs := v.fs(ctx)
	if fs == nil || (fs.Post(v.id) == nil && !v.notFound) {
		return nil
	}
	rows := make([]treeRow, 0, len(v.ancestors)+8)
	for _, a := range v.ancestors {
		rows = append(rows, treeRow{Kind: rowAncestor, PostID: a})
	}
	skip := -1 // depth of a dropped reply: its deeper rows go too
	replies := 0
	for _, r := range flattenTree(fs, v.id, v.expanded) {
		if skip >= 0 {
			if r.Depth > skip {
				continue
			}
			skip = -1
		}
		if r.Kind == rowReply && v.blocked(ctx, v.post(fs, r.PostID, r.ParentID)) {
			skip = r.Depth
			continue
		}
		switch r.Kind {
		case rowMore:
			r.Cursor = v.cursors[r.ParentID]
		case rowReply:
			replies++
		}
		rows = append(rows, r)
	}
	v.noReplies = v.repliesLoaded && replies == 0
	return rows
}

func sameRows(a, b []treeRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].key() != b[i].key() || a[i].Cursor != b[i].Cursor || len(a[i].Lasts) != len(b[i].Lasts) {
			return false
		}
		for j := range a[i].Lasts {
			if a[i].Lasts[j] != b[i].Lasts[j] {
				return false
			}
		}
	}
	return true
}

// sync rebuilds the rows into the List (unless force, only when they changed),
// keeping the selection by row. Until the user moves, the view starts at the top
// with the main post selected (and revealed).
func (v *postView) sync(ctx *ui.Context, force bool) tea.Cmd {
	rows := v.build(ctx)
	if !force && sameRows(rows, v.rows) {
		return nil
	}
	v.rows = rows
	v.views = nil
	v.list.SetItems(rows)
	if !v.touched && len(rows) > 0 {
		v.list.Restore(widgets.ScrollAnchor{ItemID: rows[0].key()})
		v.list.Select(treeRow{Kind: rowMain, PostID: v.id}.key())
	}
	return v.requestImages(ctx)
}

// redraw drops every cached render without re-reading the store.
func (v *postView) redraw() {
	v.views = nil
	v.list.SetItems(v.rows)
}

// ---- images ----

// prefixWidth is the tree prefix width of a row at depth in a list of width w.
func prefixWidth(depth, w int) int {
	return max(min(treeIndent*depth, w-minReplyCardW), 0)
}

// cardWidth is the width of a row's card (main: the whole list; reply: after the tree prefix).
func (v *postView) cardWidth(r treeRow, w int) int {
	if r.Kind == rowReply {
		return w - prefixWidth(r.Depth, w)
	}
	return w
}

// imageRows are the rows that draw their post's first image, with that media
// (only rows on or near the screen when near is set).
func (v *postView) imageRows(ctx *ui.Context, near ...[]bool) []struct {
	r treeRow
	m api.Media
} {
	if d := ctx.Density(); d != ui.DensityFull && d != ui.DensityBubble {
		return nil
	}
	var out []struct {
		r treeRow
		m api.Media
	}
	for i, r := range v.rows {
		if r.Kind != rowMain && r.Kind != rowReply {
			continue
		}
		if len(near) > 0 && !near[0][i] {
			continue
		}
		if m, ok := firstMedia(v.rowPost(ctx, r)); ok {
			out = append(out, struct {
				r treeRow
				m api.Media
			}{r, m})
		}
	}
	return out
}

// requestImages loads the first image of each row on or near the screen and
// forgets every other image (imageSlots.track).
func (v *postView) requestImages(ctx *ui.Context) tea.Cmd {
	w, h := viewSize(v.size.W, v.size.H)
	sel := v.selectedKey()
	near := nearItems(v.list, v.rows, treeRow.key, h, func(r treeRow) int {
		return len(v.view(ctx, r, r.key() == sel, w).Lines)
	})
	var ms []api.Media
	for _, x := range v.imageRows(ctx, near) {
		ms = append(ms, x.m)
	}
	return v.imgs.track(ctx, ms...)
}

// renderMedia renders a newly arrived image for every row that shows it, so a
// kitty Setup leaves with this Update.
func (v *postView) renderMedia(ctx *ui.Context, id string) tea.Cmd {
	var cmds []tea.Cmd
	for _, x := range v.imageRows(ctx) {
		if x.m.ID == id {
			cols, rows := imageBox(x.m, v.cardWidth(x.r, v.size.W)-gutterW, ctx.Density())
			cmds = append(cmds, v.imgs.render(ctx, x.m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// rerenderImages renders every loaded image for the current boxes and theme.
func (v *postView) rerenderImages(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	for _, x := range v.imageRows(ctx) {
		if v.imgs.image(x.m.ID) != nil {
			cols, rows := imageBox(x.m, v.cardWidth(x.r, v.size.W)-gutterW, ctx.Density())
			cmds = append(cmds, v.imgs.render(ctx, x.m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// ---- rendering ----

func (v *postView) renderRow(ctx *ui.Context, r treeRow, selected bool, width int) []string {
	return v.view(ctx, r, selected, width).Lines
}

// view is the cached render of row r (the same render the List shows).
func (v *postView) view(ctx *ui.Context, r treeRow, selected bool, width int) rowView {
	k := viewKey{id: r.key(), width: width, density: ctx.Density(), selected: selected, pollSel: -1}
	if rv, ok := v.views[k]; ok {
		return rv
	}
	var rv rowView
	switch r.Kind {
	case rowAncestor:
		rv = v.ancestorView(ctx, r, selected, width)
	case rowMain:
		rv = v.mainView(ctx, selected, width)
	case rowReply:
		rv = v.replyView(ctx, r, selected, width)
	default:
		rv = v.linkView(ctx, r, selected, width)
	}
	if v.views == nil {
		v.views = map[viewKey]rowView{}
	}
	v.views[k] = rv
	return rv
}

func (v *postView) imageCell(ctx *ui.Context) func(api.Media, int, int) []string {
	return func(m api.Media, cols, rows int) []string { return v.imgs.cell(ctx, m, cols, rows) }
}

// ancestorView is one dim line: gutter, "│ ", then the one-line card content.
func (v *postView) ancestorView(ctx *ui.Context, r treeRow, selected bool, width int) rowView {
	th := themeOf(ctx)
	bar := th.S.Dim.Render("│ ")
	cw := width - 2
	if cw <= gutterW {
		return rowView{Lines: []string{fit(th, bar, width)}}
	}
	p := v.rowPost(ctx, r)
	if p == nil || v.blocked(ctx, p) {
		g := th.S.Base.Render("  ")
		if selected {
			g = th.S.Base.Foreground(th.P.Acc).Render("▎") + th.S.Base.Render(" ")
		}
		return rowView{Lines: []string{fit(th, g+bar+th.S.Deleted.Render(textNotVisible), width)}}
	}
	c := renderCard(ctx, api.TimelineItem{ID: p.ID, Type: "post", Post: *p}, p, cardOpts{
		Width: cw, Density: ui.DensityLine, Selected: selected, PollSel: -1,
	})
	if len(c.Lines) == 0 {
		return rowView{Lines: []string{fit(th, bar, width)}}
	}
	line := c.Lines[0]
	content := th.S.Dim.Render(ansi.Strip(ansi.Cut(line, gutterW, cw)))
	return rowView{Lines: []string{fit(th, ansi.Cut(line, 0, gutterW)+bar+content, width)}, Hits: c.Hits, DX: 2}
}

// isRule reports a card's closing "┄" line.
func isRule(line string) bool {
	s := strings.TrimRight(ansi.Strip(line), " ")
	return s != "" && strings.Trim(s, "┄") == ""
}

// mainView is the post card (exact time) whose closing rule becomes "── 답글 N ──".
func (v *postView) mainView(ctx *ui.Context, selected bool, width int) rowView {
	th := themeOf(ctx)
	p := v.rowPost(ctx, treeRow{Kind: rowMain, PostID: v.id})
	if p == nil {
		return rowView{Lines: []string{fit(th, "", width)}}
	}
	c := renderCard(ctx, api.TimelineItem{ID: p.ID, Type: "post", Post: *p}, p, cardOpts{
		Width: width, Density: ctx.Density(), Selected: selected, PollSel: -1, ExactTime: true, Image: v.imageCell(ctx),
	})
	lines := c.Lines
	if v.notFound && fsPost(v.fs(ctx), v.id) == nil {
		return rowView{Lines: lines, Hits: c.Hits}
	}
	rule := fit(th, th.S.Dim.Render("── 답글 "+humanize.Count(p.ReplyCount)+" ──"), width)
	if n := len(lines); n > 0 && isRule(lines[n-1]) {
		lines = append(lines[:n-1:n-1], rule)
	} else {
		lines = append(lines, rule)
	}
	if v.noReplies {
		lines = append(lines, fit(th, th.S.Base.Render("  ")+th.S.Dim.Render(textNoReplies), width))
	}
	return rowView{Lines: lines, Hits: c.Hits}
}

func fsPost(fs *feedstore.FeedStore, id string) *api.Post {
	if fs == nil {
		return nil
	}
	return fs.Post(id)
}

// replyView is the tree prefix ("│  ├─ ") then the reply card on every line.
func (v *postView) replyView(ctx *ui.Context, r treeRow, selected bool, width int) rowView {
	th := themeOf(ctx)
	pw := prefixWidth(r.Depth, width)
	p := v.rowPost(ctx, r)
	if p == nil {
		return rowView{Lines: []string{fit(th, "", width)}}
	}
	c := renderCard(ctx, api.TimelineItem{ID: p.ID, Type: "post", Post: *p}, p, cardOpts{
		Width: width - pw, Density: ctx.Density(), Selected: selected, PollSel: -1, Image: v.imageCell(ctx),
	})
	first, cont := treePrefix(r.Lasts)
	st := th.S.Base.Foreground(th.P.Line)
	out := make([]string, len(c.Lines))
	for i, l := range c.Lines {
		pre := cont
		if i == 0 {
			pre = first
		}
		out[i] = fit(th, fit(th, st.Render(pre), pw)+l, width)
	}
	return rowView{Lines: out, Hits: c.Hits, DX: pw}
}

// linkView is a "답글 더 보기" / "↳ 이어서 보기" row under the tree prefix.
func (v *postView) linkView(ctx *ui.Context, r treeRow, selected bool, width int) rowView {
	th := themeOf(ctx)
	pw := prefixWidth(r.Depth, width)
	first, _ := treePrefix(r.Lasts)
	g := th.S.Base.Render("  ")
	if selected {
		g = th.S.Base.Foreground(th.P.Acc).Render("▎") + th.S.Base.Render(" ")
	}
	text := textContinue
	if r.Kind == rowMore {
		text = textMoreReplies
		if v.loading[r.ParentID] {
			text = textLoading
		}
	}
	line := fit(th, th.S.Base.Foreground(th.P.Line).Render(first), pw) + fit(th, g+th.S.Base.Foreground(th.P.Acc).Render(text), width-pw)
	return rowView{Lines: []string{fit(th, line, width)}, DX: pw}
}

func (v *postView) View(ctx *ui.Context, _ bool) string {
	th := themeOf(ctx)
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	var out []string
	v.visible = nil
	switch {
	case len(v.rows) == 0 && v.loadFailed:
		out = strings.Split(widgets.Empty(th, v.size, "⚠", textPostLoadFailed, hintPostLoadFailed), "\n")
	case len(v.rows) == 0:
		out = []string{fit(th, th.S.Base.Render("  ")+v.spin.View(th)+th.S.Dim.Render(" "+textLoading), w)}
	default:
		out = strings.Split(v.list.View(ctx, v.size), "\n")
		v.visible = v.visibleTopics(ctx)
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

func (v *postView) selectedKey() string {
	if r, ok := v.list.Selected(); ok {
		return r.key()
	}
	return ""
}

// rowAt finds the row drawn on list row y from the List's anchor (cardAt's walk).
func (v *postView) rowAt(ctx *ui.Context, y int) (r treeRow, line int, ok bool) {
	if y < 0 || len(v.rows) == 0 {
		return treeRow{}, 0, false
	}
	a := v.list.Anchor()
	sel := v.selectedKey()
	start := 0
	for i, x := range v.rows {
		if x.key() == a.ItemID {
			start = i
			break
		}
	}
	top := -a.LineOffset
	for i := start; i < len(v.rows); i++ {
		h := max(len(v.view(ctx, v.rows[i], v.rows[i].key() == sel, v.size.W).Lines), 1)
		if y < top+h {
			return v.rows[i], y - top, true
		}
		top += h
	}
	return treeRow{}, 0, false
}

// visibleTopics is "post:<id>" of the post and of every reply on screen.
func (v *postView) visibleTopics(ctx *ui.Context) []string {
	out := []string{"post:" + v.id}
	a := v.list.Anchor()
	sel := v.selectedKey()
	start := 0
	for i, x := range v.rows {
		if x.key() == a.ItemID {
			start = i
			break
		}
	}
	for i, top := start, -a.LineOffset; i < len(v.rows) && top < v.size.H; i++ {
		r := v.rows[i]
		if r.Kind == rowReply {
			out = append(out, "post:"+r.PostID)
		}
		top += max(len(v.view(ctx, r, r.key() == sel, v.size.W).Lines), 1)
	}
	return out
}

func (v *postView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

// ---- input ----

// expandable reports a reply row whose subtree can be shown here.
func (v *postView) expandable(ctx *ui.Context, r treeRow) bool {
	if r.Kind != rowReply || r.Depth >= maxReplyDepth {
		return false
	}
	p := v.rowPost(ctx, r)
	return p != nil && hasReplies(v.fs(ctx), *p)
}

// expand shows r's replies, loading the first page the first time.
func (v *postView) expand(ctx *ui.Context, r treeRow) tea.Cmd {
	if !v.expandable(ctx, r) || v.expanded[r.PostID] {
		return nil
	}
	v.expanded[r.PostID] = true
	var load tea.Cmd
	if fs := v.fs(ctx); fs != nil && !fs.HasReplies(r.PostID) {
		load = v.loadReplies(ctx, r.PostID, "")
	}
	return tea.Batch(load, v.sync(ctx, true))
}

// collapse hides r's replies; on a row that is not expanded it collapses (and
// selects) the reply it hangs under.
func (v *postView) collapse(ctx *ui.Context, r treeRow) tea.Cmd {
	if r.Kind == rowReply && v.expanded[r.PostID] {
		delete(v.expanded, r.PostID)
		return v.sync(ctx, true)
	}
	parent := r.ParentID
	if r.Kind == rowDeep {
		parent = r.PostID
	}
	if r.Kind == rowMain || r.Kind == rowAncestor || parent == "" || parent == v.id {
		return nil
	}
	if r.Kind != rowDeep {
		delete(v.expanded, parent)
	}
	cmd := v.sync(ctx, true)
	v.list.Select(treeRow{Kind: rowReply, PostID: parent}.key())
	return cmd
}

// open is enter on a row: ancestor / reply => its RoutePost, more => next page,
// continue => the deep reply's RoutePost.
func (v *postView) open(ctx *ui.Context, r treeRow) tea.Cmd {
	switch r.Kind {
	case rowAncestor, rowReply, rowDeep:
		return ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: r.PostID})
	case rowMore:
		cmd := v.loadReplies(ctx, r.ParentID, r.Cursor)
		v.redraw() // "불러오는 중…"
		return cmd
	}
	return nil
}

func (v *postView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	if r, ok := v.list.Selected(); ok {
		switch s := msg.String(); {
		case km.Matches(msg, keymap.ListOpen):
			return true, v.open(ctx, r)
		case s == "space":
			v.touched = true
			if v.expanded[r.PostID] && r.Kind == rowReply {
				return true, v.collapse(ctx, r)
			}
			return true, v.expand(ctx, r)
		case s == "right":
			v.touched = true
			return true, v.expand(ctx, r)
		case s == "left":
			v.touched = true
			return true, v.collapse(ctx, r)
		}
		if isPostKey(ctx, msg) {
			p := v.rowPost(ctx, r)
			if p == nil {
				return true, nil
			}
			if handled, cmd := handlePostKey(ctx, msg, p); handled {
				v.redraw() // optimistic like / repost changes show at once
				return true, cmd
			}
		}
	}
	handled, cmd := v.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	v.touched = true
	return true, tea.Batch(cmd, v.requestImages(ctx))
}

func (v *postView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	switch msg.(type) {
	case tea.MouseWheelMsg:
		v.touched = true
		return tea.Batch(v.list.HandleMouse(ctx, msg, local), v.requestImages(ctx))
	case tea.MouseClickMsg:
	default:
		return nil
	}
	if msg.Mouse().Button != tea.MouseLeft {
		return nil
	}
	v.touched = true
	if r, line, ok := v.rowAt(ctx, local.Y); ok {
		selected := r.key() == v.selectedKey()
		rv := v.view(ctx, r, selected, v.size.W)
		x := local.X - rv.DX
		for _, h := range rv.Hits {
			if h.Line == line && x >= h.X0 && x < h.X1 {
				return handlePostHit(ctx, h, v.rowPost(ctx, r))
			}
		}
		if r.Kind == rowMore || r.Kind == rowDeep {
			v.list.Select(r.key())
			return v.open(ctx, r)
		}
		// a click on the selected ancestor / reply opens it, like enter (CA 5.5)
		if selected && (r.Kind == rowAncestor || r.Kind == rowReply) {
			return v.open(ctx, r)
		}
	}
	return tea.Batch(v.list.HandleMouse(ctx, msg, local), v.requestImages(ctx))
}

// ---- optional capabilities ----

func (v *postView) Title(*ui.Context) (string, ui.Badge) { return textPostTitle, ui.Badge{} }

func (v *postView) Help(ctx *ui.Context) []key.Binding {
	relabel := func(a keymap.Action, desc string) key.Binding {
		b := ctx.Keys.Binding(a)
		b.SetHelp(b.Help().Key, desc)
		return b
	}
	return []key.Binding{
		relabel(keymap.ListOpen, "열기"),
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "펼치기")),
		relabel(keymap.PostLike, "좋아요"),
		relabel(keymap.PostReply, "답글"),
	}
}

func (v *postView) WatchTopics() []string {
	if len(v.visible) == 0 {
		return []string{"post:" + v.id}
	}
	return append([]string(nil), v.visible...)
}
