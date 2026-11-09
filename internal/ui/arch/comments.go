package arch

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Comments of a room document (P3 Task 25, spec C2.11): the server keeps them
// (P0 Task 29, /v1/docs/{id}/comments); the canvas anchors each to a node, an
// edge, a box or an empty spot with the anchor strings of arch.md 2.5.

// Comment limits (api.md DocComment).
const (
	anchorMax      = 128       // DocComment.anchor 1..128
	commentBodyMax = 2000      // DocComment.body 1..2000 (characters)
	anchorCoordMax = 1_000_000 // |x|, |y| of an "at:" spot
	mentionRows    = 5         // @ suggestions shown
	commentPages   = 20        // ListDocComments pages read at most (100 each)
)

// anchorKind is what a comment is attached to.
type anchorKind int

const (
	anchorUnknown anchorKind = iota // a shape this client does not know
	anchorNode
	anchorEdge
	anchorBox
	anchorAt // an empty spot (world coordinates)
)

// commentAnchor is a decoded DocComment.anchor.
type commentAnchor struct {
	kind anchorKind
	id   string // node, edge or box id
	x, y int    // anchorAt
}

var anchorIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,62}$`)

// encodeAnchor is a's anchor string: node:<id>, edge:<id>, box:<id> or
// at:<x>,<y> ("" for an unknown anchor).
func encodeAnchor(a commentAnchor) string {
	switch a.kind {
	case anchorNode:
		return "node:" + a.id
	case anchorEdge:
		return "edge:" + a.id
	case anchorBox:
		return "box:" + a.id
	case anchorAt:
		return "at:" + strconv.Itoa(a.x) + "," + strconv.Itoa(a.y)
	}
	return ""
}

// decodeAnchor reads an anchor string; false for any other shape (the panel
// lists those under "위치 모름").
func decodeAnchor(s string) (commentAnchor, bool) {
	if s == "" || len(s) > anchorMax {
		return commentAnchor{}, false
	}
	pre, rest, ok := strings.Cut(s, ":")
	if !ok {
		return commentAnchor{}, false
	}
	switch pre {
	case "node", "edge", "box":
		if !anchorIDRe.MatchString(rest) {
			return commentAnchor{}, false
		}
		k := map[string]anchorKind{"node": anchorNode, "edge": anchorEdge, "box": anchorBox}[pre]
		return commentAnchor{kind: k, id: rest}, true
	case "at":
		xs, ys, ok := strings.Cut(rest, ",")
		if !ok {
			return commentAnchor{}, false
		}
		x, ex := strconv.Atoi(xs)
		y, ey := strconv.Atoi(ys)
		// canonical integers only (no "+1", "01"), so a spot has one string
		if ex != nil || ey != nil || strconv.Itoa(x) != xs || strconv.Itoa(y) != ys ||
			x < -anchorCoordMax || x > anchorCoordMax || y < -anchorCoordMax || y > anchorCoordMax {
			return commentAnchor{}, false
		}
		return commentAnchor{kind: anchorAt, x: x, y: y}, true
	}
	return commentAnchor{}, false
}

// commentsState is the comments of the canvas's room document and the panel.
type commentsState struct {
	items  []api.DocComment // oldest first (created_at, then id)
	loaded bool
	failed bool // the list could not be read

	members      []api.UserSummary // the room's members (@ suggestions)
	membersAsked bool

	cur    int  // the selected thread (commentThreads order)
	pulled bool // the comments pulled the panel drawer out (pushed back on close)

	// the comment being written
	composing bool
	ti        textinput.Model
	anchor    string // where it goes
	rootID    string // a reply: the thread's first comment
	sugg      []api.UserSummary
	suggSel   int
	curX      int // the real cursor, set by the last frame (curY -1 = none)
	curY      int
}

// Messages of the comments (each carries its canvas instance).
type (
	commentsLoadedMsg struct {
		instance string
		items    []api.DocComment
		err      error
	}
	commentDoneMsg struct { // a POST or PATCH answered
		instance string
		c        *api.DocComment
		err      error
	}
	membersLoadedMsg struct {
		instance string
		members  []api.UserSummary
	}
	// openCommentsMsg opens the comments of instance (a mention alert for a
	// document whose canvas is already open).
	openCommentsMsg struct{ instance string }
)

// queryComments "1": open the comments panel first (a mention alert).
const queryComments = "comments"

// commentMsg handles the comment messages; ok = msg was one of them.
func (m *canvasModel) commentMsg(ctx *ui.Context, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case commentsLoadedMsg:
		if msg.instance != m.route.ID {
			return nil, true
		}
		m.cmt.loaded = true
		m.cmt.failed = msg.err != nil
		for _, c := range msg.items {
			m.addComment(c)
		}
		return nil, true
	case commentDoneMsg:
		if msg.instance != m.route.ID {
			return nil, true
		}
		if msg.err != nil || msg.c == nil {
			return ui.Toast(ctx.T.Archcanvas.CommentsFailed(), ui.ToastError), true
		}
		m.addComment(*msg.c)
		m.selectThread(threadOf(*msg.c))
		return nil, true
	case membersLoadedMsg:
		if msg.instance == m.route.ID {
			m.cmt.members = msg.members
		}
		return nil, true
	case openCommentsMsg:
		if msg.instance != m.route.ID {
			return nil, true
		}
		if m.shared() == nil {
			return nil, true
		}
		return m.openComments(ctx), true
	case ui.EventMsg:
		if msg.Event.Type != "doc.comment_created" {
			return nil, false
		}
		sh := m.shared()
		if sh == nil {
			return nil, true
		}
		v, err := msg.Event.Decode()
		if err != nil {
			return nil, true
		}
		if ev, ok := v.(realtime.DocCommentCreatedEvent); ok && ev.DocID == sh.docID {
			m.addComment(ev.Comment)
		}
		return nil, true
	}
	return nil, false
}

// threadOf is the id of the thread comment c belongs to.
func threadOf(c api.DocComment) string {
	if c.RootID != nil && *c.RootID != "" {
		return *c.RootID
	}
	return c.ID
}

// addComment takes c (a newer copy of a known comment replaces it).
func (m *canvasModel) addComment(c api.DocComment) {
	if i := slices.IndexFunc(m.cmt.items, func(o api.DocComment) bool { return o.ID == c.ID }); i >= 0 {
		m.cmt.items[i] = c
		return
	}
	m.cmt.items = append(m.cmt.items, c)
	slices.SortStableFunc(m.cmt.items, func(a, b api.DocComment) int {
		if d := a.CreatedAt.Compare(b.CreatedAt); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// loadComments reads every comment of document docID.
func (m *canvasModel) loadComments(ctx *ui.Context, docID string) tea.Cmd {
	cl, inst := ctx.API, m.route.ID
	if cl == nil {
		return nil
	}
	return func() tea.Msg {
		var all []api.DocComment
		p := api.PageOpts{Limit: 100}
		for range commentPages {
			pg, err := cl.ListDocComments(context.Background(), docID, p)
			if err != nil {
				return commentsLoadedMsg{instance: inst, err: err}
			}
			all = append(all, pg.Items...)
			if pg.Done() {
				break
			}
			p.Cursor = *pg.NextCursor
		}
		return commentsLoadedMsg{instance: inst, items: all}
	}
}

// askMembers reads the room's members once (the @ suggestions).
func (m *canvasModel) askMembers(ctx *ui.Context) tea.Cmd {
	sh := m.shared()
	if sh == nil || sh.meta == nil || sh.meta.SpaceID == "" || m.cmt.membersAsked || ctx.API == nil {
		return nil
	}
	m.cmt.membersAsked = true
	cl, inst, space := ctx.API, m.route.ID, sh.meta.SpaceID
	return func() tea.Msg {
		pg, err := cl.ListSpaceMembers(context.Background(), space, api.SpaceMembersQuery{PageOpts: api.PageOpts{Limit: 100}})
		if err != nil {
			return membersLoadedMsg{instance: inst}
		}
		out := make([]api.UserSummary, 0, len(pg.Items))
		for _, mb := range pg.Items {
			out = append(out, mb.User)
		}
		return membersLoadedMsg{instance: inst, members: out}
	}
}

// commentThread is a first comment and its replies.
type commentThread struct {
	root    api.DocComment
	replies []api.DocComment
}

// open: not resolved and something in it is not deleted.
func (th commentThread) open() bool {
	if th.root.ResolvedAt != nil {
		return false
	}
	if !th.root.Deleted {
		return true
	}
	return slices.ContainsFunc(th.replies, func(c api.DocComment) bool { return !c.Deleted })
}

// live counts the comments of the thread that are not deleted.
func (th commentThread) live() int {
	n := 0
	for _, c := range append([]api.DocComment{th.root}, th.replies...) {
		if !c.Deleted {
			n++
		}
	}
	return n
}

// commentGroup is the threads of one anchor (or of the orphan / unknown sets).
type commentGroup struct {
	title   string
	threads []commentThread
}

// threadsByRoot builds the threads (a reply whose root is unknown becomes a thread of its own).
func (m *canvasModel) threadsByRoot() []commentThread {
	idx := map[string]int{}
	var out []commentThread
	for _, c := range m.cmt.items {
		if c.RootID == nil || *c.RootID == "" {
			idx[c.ID] = len(out)
			out = append(out, commentThread{root: c})
		}
	}
	for _, c := range m.cmt.items {
		if c.RootID == nil || *c.RootID == "" {
			continue
		}
		if i, ok := idx[*c.RootID]; ok {
			out[i].replies = append(out[i].replies, c)
			continue
		}
		if c.Deleted {
			continue
		}
		idx[c.ID] = len(out)
		out = append(out, commentThread{root: c})
	}
	return out
}

// anchorLive reports whether a's object is in the design now.
func (m *canvasModel) anchorLive(a commentAnchor) bool {
	switch a.kind {
	case anchorNode:
		return m.node(a.id) != nil
	case anchorEdge:
		return m.edge(a.id) != nil
	case anchorBox:
		return m.box(a.id) != nil
	case anchorAt:
		return true
	}
	return false
}

// anchorTitle names a live anchor.
func (m *canvasModel) anchorTitle(ctx *ui.Context, a commentAnchor) string {
	switch a.kind {
	case anchorNode, anchorBox:
		return m.label(a.id)
	case anchorEdge:
		if e := m.edge(a.id); e != nil {
			return ctx.T.Archcanvas.PanelEdge(m.label(e.From), m.label(e.To))
		}
	case anchorAt:
		return ctx.T.Archcanvas.CommentsGroupAt(a.x, a.y)
	}
	return ""
}

// threadSet is the threads sorted for the panel: per live anchor (in the
// order their first comment came), then the ones on deleted objects, then the
// unknown anchors.
type threadSet struct {
	anchors []commentAnchor
	live    [][]commentThread
	orphans []commentThread
	unknown []commentThread
}

func (m *canvasModel) sortThreads() threadSet {
	var ts threadSet
	byAnchor := map[string]int{}
	for _, th := range m.threadsByRoot() {
		an, ok := decodeAnchor(th.root.Anchor)
		switch {
		case !ok:
			ts.unknown = append(ts.unknown, th)
		case m.design == nil || !m.anchorLive(an):
			ts.orphans = append(ts.orphans, th)
		default:
			key := encodeAnchor(an)
			i, seen := byAnchor[key]
			if !seen {
				i = len(ts.live)
				byAnchor[key] = i
				ts.anchors = append(ts.anchors, an)
				ts.live = append(ts.live, nil)
			}
			ts.live[i] = append(ts.live[i], th)
		}
	}
	return ts
}

// commentGroups are the panel's groups with their titles.
func (m *canvasModel) commentGroups(ctx *ui.Context) []commentGroup {
	ts := m.sortThreads()
	groups := make([]commentGroup, 0, len(ts.live)+2)
	for i, an := range ts.anchors {
		groups = append(groups, commentGroup{title: m.anchorTitle(ctx, an), threads: ts.live[i]})
	}
	if len(ts.orphans) > 0 {
		groups = append(groups, commentGroup{title: ctx.T.Archcanvas.DeletedNodeComment(), threads: ts.orphans})
	}
	if len(ts.unknown) > 0 {
		groups = append(groups, commentGroup{title: ctx.T.Archcanvas.CommentsGroupUnknown(), threads: ts.unknown})
	}
	return groups
}

// threadsOrdered are the threads in panel order (the cursor indexes it).
func (m *canvasModel) threadsOrdered() []commentThread {
	ts := m.sortThreads()
	var out []commentThread
	for _, g := range ts.live {
		out = append(out, g...)
	}
	return append(append(out, ts.orphans...), ts.unknown...)
}

// selectThread puts the panel cursor on thread id (when listed).
func (m *canvasModel) selectThread(id string) {
	if i := slices.IndexFunc(m.threadsOrdered(), func(th commentThread) bool { return th.root.ID == id }); i >= 0 {
		m.cmt.cur = i
	}
}

// commentCounts is the canvas's ✎n (spec 💬n): per live node, the comments of its open threads.
func (m *canvasModel) commentCounts() map[string]int {
	out := map[string]int{}
	if m.shared() == nil || m.design == nil {
		return out
	}
	for _, th := range m.threadsByRoot() {
		an, ok := decodeAnchor(th.root.Anchor)
		if !ok || an.kind != anchorNode || m.node(an.id) == nil || !th.open() {
			continue
		}
		out[an.id] += th.live()
	}
	return out
}

// selectionAnchor is where C puts a comment: the selected node, edge or box,
// else the empty spot at the middle of the canvas.
func (m *canvasModel) selectionAnchor() string {
	switch {
	case m.node(m.sel) != nil:
		return encodeAnchor(commentAnchor{kind: anchorNode, id: m.sel})
	case m.edge(m.sel) != nil:
		return encodeAnchor(commentAnchor{kind: anchorEdge, id: m.sel})
	case m.box(m.sel) != nil:
		return encodeAnchor(commentAnchor{kind: anchorBox, id: m.sel})
	}
	x, y := m.vp.ToWorld(m.vp.W/2, m.vp.H/2)
	return spotAnchor(x, y)
}

// spotAnchor is the anchor of empty spot (x, y), clamped into range.
func spotAnchor(x, y int) string {
	c := func(v int) int { return min(max(v, -anchorCoordMax), anchorCoordMax) }
	return encodeAnchor(commentAnchor{kind: anchorAt, x: c(x), y: c(y)})
}

// openComments shows the comments panel (the panel's comments tab; a drawer
// layout pulls the panel drawer out) and reads what it lacks.
func (m *canvasModel) openComments(ctx *ui.Context) tea.Cmd {
	sh := m.shared()
	if sh == nil {
		return ui.Toast(ctx.T.Archcanvas.CommentsPersonal(), ui.ToastInfo)
	}
	m.cancelGesture()
	m.menu, m.faults = nil, nil
	if m.mode == modePanel {
		m.closePanel()
	}
	if m.mode == modeMulti {
		m.leaveMulti()
	}
	if m.mode == modeFind || m.mode == modePick || m.mode == modeMove || m.mode == modeConnect || m.mode == modeHelp {
		if m.mode == modePick {
			m.endPick()
		}
		m.cursor, m.moveDX, m.moveDY = "", 0, 0
	}
	m.mode = modeComments
	m.showComments()
	var cmds []tea.Cmd
	if !m.cmt.loaded {
		cmds = append(cmds, m.loadComments(ctx, sh.docID))
	}
	cmds = append(cmds, m.askMembers(ctx))
	return tea.Batch(cmds...)
}

// showComments pulls the panel drawer out for the comments (drawer layouts).
func (m *canvasModel) showComments() {
	if m.mode != modeComments || !m.regs.PanelDrawer || m.regs.View == viewList || m.drawer.panel {
		return
	}
	m.drawer.panel, m.cmt.pulled = true, true
	m.regs = computeRegions(m.layMode, m.size.W, m.size.H, m.drawer)
	m.vp.W, m.vp.H = m.regs.Canvas.W, m.regs.Canvas.H
}

// closeComments gives the keys back to the canvas.
func (m *canvasModel) closeComments() {
	m.stopComment()
	if m.mode == modeComments {
		m.mode = modeNormal
	}
	if m.cmt.pulled {
		m.cmt.pulled = false
		m.drawer.panel = false
		m.layout()
	}
}

// commentOn starts a new comment on anchor (C, the menu's 댓글).
func (m *canvasModel) commentOn(ctx *ui.Context, anchor string) tea.Cmd {
	cmd := m.openComments(ctx)
	if m.mode != modeComments {
		return cmd
	}
	m.startComment(anchor, "")
	for i, th := range m.threadsOrdered() {
		if th.root.Anchor == anchor {
			m.cmt.cur = i
			break
		}
	}
	return cmd
}

// newCommentInput is the one-line comment input (the panel's textinput setup).
func newCommentInput() textinput.Model {
	ti := newPanelInput("")
	ti.CharLimit = commentBodyMax
	return ti
}

func (m *canvasModel) startComment(anchor, rootID string) {
	m.cmt.composing, m.cmt.anchor, m.cmt.rootID = true, anchor, rootID
	m.cmt.ti = newCommentInput()
	m.cmt.sugg, m.cmt.suggSel = nil, 0
}

func (m *canvasModel) stopComment() {
	m.cmt.composing, m.cmt.anchor, m.cmt.rootID = false, "", ""
	m.cmt.ti = textinput.Model{}
	m.cmt.sugg, m.cmt.suggSel = nil, 0
}

// sendComment posts the comment being written.
func (m *canvasModel) sendComment(ctx *ui.Context) tea.Cmd {
	body := strings.TrimSpace(m.cmt.ti.Value())
	sh := m.shared()
	if body == "" || sh == nil || ctx.API == nil {
		return nil
	}
	req := api.CreateDocCommentRequest{Anchor: m.cmt.anchor, Body: body}
	if m.cmt.rootID != "" {
		root := m.cmt.rootID
		req.RootID = &root
	}
	m.stopComment()
	cl, inst, doc := ctx.API, m.route.ID, sh.docID
	return func() tea.Msg {
		c, err := cl.CreateDocComment(context.Background(), doc, req)
		return commentDoneMsg{instance: inst, c: c, err: err}
	}
}

// toggleResolved resolves the selected thread (or opens it again).
func (m *canvasModel) toggleResolved(ctx *ui.Context) tea.Cmd {
	ths := m.threadsOrdered()
	sh := m.shared()
	if m.cmt.cur >= len(ths) || sh == nil || ctx.API == nil {
		return nil
	}
	root := ths[m.cmt.cur].root
	resolved := root.ResolvedAt == nil
	cl, inst, doc := ctx.API, m.route.ID, sh.docID
	return func() tea.Msg {
		c, err := cl.UpdateDocComment(context.Background(), doc, root.ID, api.UpdateDocCommentRequest{Resolved: &resolved})
		return commentDoneMsg{instance: inst, c: c, err: err}
	}
}

// commentKey is the keys while the comments panel has them.
func (m *canvasModel) commentKey(ctx *ui.Context, k tea.KeyPressMsg, s string) (bool, tea.Cmd) {
	if m.cmt.composing {
		switch s {
		case "esc":
			m.stopComment()
			return true, nil
		case "enter":
			if len(m.cmt.sugg) > 0 {
				m.takeSuggestion()
				return true, nil
			}
			return true, m.sendComment(ctx)
		case "up", "down":
			if n := len(m.cmt.sugg); n > 0 {
				d := 1
				if s == "up" {
					d = -1
				}
				m.cmt.suggSel = (m.cmt.suggSel + d + n) % n
				return true, nil
			}
		}
		if reservedGlobal(s) {
			return false, nil
		}
		cmd := m.typeComment(k)
		m.suggest(ctx)
		return true, cmd
	}
	ths := m.threadsOrdered()
	switch s {
	case "j", "down":
		m.cmt.cur = min(m.cmt.cur+1, max(len(ths)-1, 0))
	case "k", "up":
		m.cmt.cur = max(m.cmt.cur-1, 0)
	case "enter", "r":
		if m.cmt.cur < len(ths) {
			th := ths[m.cmt.cur]
			m.startComment(th.root.Anchor, th.root.ID)
		}
	case "n", "C":
		m.startComment(m.selectionAnchor(), "")
	case "x":
		return true, m.toggleResolved(ctx)
	case "esc":
		m.closeComments()
	default:
		return !reservedGlobal(s), nil
	}
	return true, nil
}

// typeComment feeds k to the comment input: typed text is one sanitized line.
func (m *canvasModel) typeComment(k tea.KeyPressMsg) tea.Cmd {
	var msg tea.Msg = k
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0 {
		msg = tea.PasteMsg{Content: strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(textwidth.Sanitize(k.Text))}
	}
	var cmd tea.Cmd
	m.cmt.ti, cmd = m.cmt.ti.Update(msg)
	return cmd
}

// mentionWord is the "@…" word that ends at the cursor (ok false: none).
func (m *canvasModel) mentionWord() (start int, word string, ok bool) {
	rs := []rune(m.cmt.ti.Value())
	pos := min(m.cmt.ti.Position(), len(rs))
	i := pos
	for i > 0 && rs[i-1] != ' ' {
		i--
	}
	w := string(rs[i:pos])
	if !strings.HasPrefix(w, "@") {
		return 0, "", false
	}
	return i, w, true
}

// mentionPeople are the people @ can name: the room's members, else everyone
// the store knows; never me.
func (m *canvasModel) mentionPeople(ctx *ui.Context) []api.UserSummary {
	var src []api.UserSummary
	if len(m.cmt.members) > 0 {
		src = m.cmt.members
	} else if ctx.Store != nil {
		for _, u := range ctx.Store.Users {
			if u != nil {
				src = append(src, *u)
			}
		}
	}
	me := myID(ctx)
	out := make([]api.UserSummary, 0, len(src))
	for _, u := range src {
		if u.ID != me && u.Handle != "" {
			out = append(out, u)
		}
	}
	slices.SortFunc(out, func(a, b api.UserSummary) int { return strings.Compare(a.Handle, b.Handle) })
	return out
}

// suggest fills the @ suggestions for the word at the cursor.
func (m *canvasModel) suggest(ctx *ui.Context) {
	m.cmt.sugg, m.cmt.suggSel = nil, 0
	_, w, ok := m.mentionWord()
	if !ok {
		return
	}
	q := strings.ToLower(strings.TrimPrefix(w, "@"))
	for _, u := range m.mentionPeople(ctx) {
		if strings.HasPrefix(strings.ToLower(u.Handle), q) || (q != "" && strings.Contains(strings.ToLower(u.DisplayName), q)) {
			m.cmt.sugg = append(m.cmt.sugg, u)
			if len(m.cmt.sugg) == mentionRows {
				break
			}
		}
	}
	// a word that already names the only match exactly needs no list
	if len(m.cmt.sugg) == 1 && strings.EqualFold(m.cmt.sugg[0].Handle, q) {
		m.cmt.sugg = nil
	}
}

// takeSuggestion puts "@handle " in place of the word at the cursor.
func (m *canvasModel) takeSuggestion() {
	start, _, ok := m.mentionWord()
	if !ok || m.cmt.suggSel >= len(m.cmt.sugg) {
		m.cmt.sugg = nil
		return
	}
	rs := []rune(m.cmt.ti.Value())
	pos := min(m.cmt.ti.Position(), len(rs))
	ins := []rune("@" + m.cmt.sugg[m.cmt.suggSel].Handle + " ")
	v := string(rs[:start]) + string(ins) + string(rs[pos:])
	if len([]rune(v)) <= commentBodyMax {
		m.cmt.ti.SetValue(v)
		m.cmt.ti.SetCursor(start + len(ins))
	}
	m.cmt.sugg, m.cmt.suggSel = nil, 0
}

// authorName is a comment author's name (the store's copy first).
func authorName(ctx *ui.Context, u api.UserSummary) string {
	if ctx.Store != nil && ctx.Store.User(u.ID) != nil || (ctx.Me() != nil && ctx.Me().ID == u.ID) {
		return personName(ctx, u.ID)
	}
	if n := cleanText(u.DisplayName); n != "" {
		return n
	}
	if n := cleanText(u.Handle); n != "" {
		return n
	}
	return personName(ctx, u.ID)
}

// commentRow is one drawn line of the panel.
type commentRow struct {
	s      string
	st     sty
	thread int // the thread it belongs to (-1 = a title)
}

// commentRows are the panel's lines at inner width w.
func (m *canvasModel) commentRows(ctx *ui.Context, w int) []commentRow {
	a := ctx.T.Archcanvas
	var rows []commentRow
	wrapped := func(prefix, s string, st sty, th int) {
		pw := textwidth.Width(prefix)
		for i, part := range textwidth.Wrap(s, max(w-pw, 1)) {
			p := prefix
			if i > 0 {
				p = strings.Repeat(" ", pw)
			}
			rows = append(rows, commentRow{s: p + part, st: st, thread: th})
		}
	}
	n := 0
	for _, g := range m.commentGroups(ctx) {
		rows = append(rows, commentRow{s: "─ " + g.title + " ─", st: sDim, thread: -1})
		for _, th := range g.threads {
			st := sBase
			if th.root.ResolvedAt != nil {
				st = sDim
			}
			if n == m.cmt.cur {
				st = sSel
			}
			mark := "  "
			if n == m.cmt.cur {
				mark = "› "
			}
			body := cleanText(th.root.Body)
			if th.root.Deleted {
				body = a.CommentsDeleted()
			}
			text := authorName(ctx, th.root.Author) + ": " + body
			if th.root.ResolvedAt != nil {
				text = "✓ " + a.CommentsResolved() + " · " + text
			}
			wrapped(mark, text, st, n)
			for _, r := range th.replies {
				if r.Deleted {
					continue
				}
				rst := sBase
				if th.root.ResolvedAt != nil {
					rst = sDim
				}
				wrapped("  ↳ ", authorName(ctx, r.Author)+": "+cleanText(r.Body), rst, n)
			}
			n++
		}
	}
	return rows
}

// openThreads counts the threads not resolved.
func (m *canvasModel) openThreads() int {
	n := 0
	for _, th := range m.threadsByRoot() {
		if th.open() {
			n++
		}
	}
	return n
}

// drawComments draws the comments panel in r (x, w = the text column).
func (m *canvasModel) drawComments(ctx *ui.Context, b *buf, r layout.Rect, x, w int) {
	m.cmt.curY = -1
	if w <= 0 || r.H <= 0 {
		return
	}
	a := ctx.T.Archcanvas
	y, bottom := r.Y, r.Y+r.H
	b.line(x, y, w, a.CommentsTitle(m.openThreads()), sAccBold)
	y++
	// the input and its suggestions take the bottom rows
	var foot []commentRow
	if m.cmt.composing {
		head := a.CommentsNew(m.anchorName(ctx, m.cmt.anchor))
		if m.cmt.rootID != "" {
			head = a.CommentsReply(m.rootAuthor(ctx))
		}
		foot = append(foot, commentRow{s: head, st: sAcc})
		foot = append(foot, commentRow{s: "", st: sBase}) // the input line
		for i, u := range m.cmt.sugg {
			st := sDim
			if i == m.cmt.suggSel {
				st = sSel
			}
			foot = append(foot, commentRow{s: "@" + cleanText(u.Handle) + " " + cleanText(u.DisplayName), st: st})
		}
	}
	listH := max(bottom-y-len(foot), 0)
	var rows []commentRow
	switch {
	case !m.cmt.loaded:
		rows = []commentRow{{s: "…", st: sDim, thread: -1}}
	case m.cmt.failed && len(m.cmt.items) == 0:
		for _, s := range textwidth.Wrap(a.CommentsLoadFailed(), w) {
			rows = append(rows, commentRow{s: s, st: sErr, thread: -1})
		}
	case len(m.cmt.items) == 0:
		for _, s := range textwidth.Wrap(a.CommentsEmpty(), w) {
			rows = append(rows, commentRow{s: s, st: sDim, thread: -1})
		}
	default:
		rows = m.commentRows(ctx, w)
	}
	// keep the selected thread's first line in view
	off := 0
	if first := slices.IndexFunc(rows, func(c commentRow) bool { return c.thread == m.cmt.cur }); first >= listH && listH > 0 {
		off = first - listH/2
	}
	for i := off; i < len(rows) && y < r.Y+1+listH; i++ {
		b.line(x, y, w, rows[i].s, rows[i].st)
		y++
	}
	fy := max(bottom-len(foot), r.Y+1)
	for i, f := range foot {
		if fy+i >= bottom {
			break
		}
		if i == 1 {
			m.drawCommentInput(b, x, fy+i, w)
			continue
		}
		b.line(x, fy+i, w, f.s, f.st)
	}
}

// drawCommentInput draws the input line ("> " and the text around the cursor).
func (m *canvasModel) drawCommentInput(b *buf, x, y, w int) {
	const prompt = "> "
	b.line(x, y, w, "", sBase)
	used := b.text(x, y, prompt, w, sAcc)
	room := w - used - 1
	if room <= 0 {
		return
	}
	rs := []rune(m.cmt.ti.Value())
	pos := min(m.cmt.ti.Position(), len(rs))
	before, after := string(rs[:pos]), string(rs[pos:])
	if textwidth.Width(before) > room {
		before = textwidth.TruncateLeft(before, room, "…")
	}
	n := b.text(x+used, y, before, room, sBase)
	m.cmt.curX, m.cmt.curY = x+used+n, y
	b.text(x+used+n, y, textwidth.Truncate(after, room-n, "…"), room-n, sBase)
}

// anchorName names where a new comment goes.
func (m *canvasModel) anchorName(ctx *ui.Context, anchor string) string {
	an, ok := decodeAnchor(anchor)
	if !ok {
		return ctx.T.Archcanvas.CommentsGroupUnknown()
	}
	if !m.anchorLive(an) {
		return ctx.T.Archcanvas.DeletedNodeComment()
	}
	return m.anchorTitle(ctx, an)
}

// rootAuthor is the name of the thread being answered.
func (m *canvasModel) rootAuthor(ctx *ui.Context) string {
	for _, c := range m.cmt.items {
		if c.ID == m.cmt.rootID {
			return authorName(ctx, c.Author)
		}
	}
	return ""
}
