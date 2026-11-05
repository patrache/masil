package feed

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Overlay ids (ui.RegisterOverlay, "feed." prefix).
const (
	overlayPostActions = "feed.post_actions" // args <postID>
	overlayUserList    = "feed.user_list"    // args "<kind> <ref>"
	overlayQuotes      = "feed.quotes"       // args <postID>
	overlayHashtagPick = "feed.hashtag_pick" // args <postID> (2+ hashtags)
)

// feed.user_list kinds (args "<kind> <ref>").
const (
	listLikes        = "likes"         // ref: post id
	listReposts      = "reposts"       // ref: post id
	listFollowers    = "followers"     // ref: user ref
	listFollowing    = "following"     // ref: user ref
	listStoryViewers = "story_viewers" // ref: story id
)

// Overlay copy.
const (
	textMenuTitle      = "글 메뉴"
	textMenuLikes      = "좋아요 누른 사람"
	textMenuReposts    = "리포스트한 사람"
	textMenuQuotes     = "인용한 글"
	textMenuAuthor     = "작성자 프로필"
	textMenuDelete     = "글 삭제"
	textMenuReport     = "글 신고"
	textMenuBlock      = "작성자 차단"
	textDeleteAsk      = "이 글을 삭제할까요?"
	textDeleteYes      = "삭제"
	textBlockAsk       = "님을 차단할까요?"
	textBlockBody      = "서로의 글이 보이지 않게 돼요"
	textBlockYes       = "차단"
	textCancel         = "취소"
	textFollowersTitle = "팔로워"
	textFollowingTitle = "팔로잉"
	textViewersTitle   = "본 사람"
	textHashtagTitle   = "해시태그"
	textNobody         = "아직 아무도 없어요"
	textNoQuotes       = "아직 인용한 글이 없어요"
	textRemoveAsk      = "님을 팔로워에서 뺄까요?"
	textRemoveBody     = "내 글이 그 사람 팔로잉 피드에 더는 오지 않아요."
	textRemoveYes      = "빼기"
	textRemoved        = "팔로워에서 뺐어요"
	errContextList     = "목록"
	errContextRemove   = "팔로워 삭제"
)

// Overlay sizes (outer, border included; the app clamps them to the body).
const (
	menuW         = 30
	userListW     = 48
	userListH     = 20
	quotesW       = 60
	listPageLimit = 30
)

func init() {
	ui.RegisterOverlay(overlayPostActions, func(ctx *ui.Context, args string) ui.Overlay {
		if m := newPostActions(ctx, strings.TrimSpace(args)); m != nil {
			return m
		}
		return nil
	})
	ui.RegisterOverlay(overlayUserList, func(ctx *ui.Context, args string) ui.Overlay {
		if l := newUserList(ctx, args); l != nil {
			return l
		}
		return nil
	})
	ui.RegisterOverlay(overlayQuotes, func(ctx *ui.Context, args string) ui.Overlay {
		if q := newQuotes(strings.TrimSpace(args)); q != nil {
			return q
		}
		return nil
	})
	ui.RegisterOverlay(overlayHashtagPick, func(ctx *ui.Context, args string) ui.Overlay {
		if m := newHashtagPick(ctx, strings.TrimSpace(args)); m != nil {
			return m
		}
		return nil
	})
}

func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

// centeredIn is a w x h rect centered in body (w, h clamped to body).
func centeredIn(body layout.Rect, w, h int) layout.Rect {
	w, h = max(min(w, body.W), 0), max(min(h, body.H), 0)
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-h)/2, W: w, H: h}
}

// selectedLine draws a menu / list row: "› " + text on the Selected background
// when sel, "  " + text otherwise; exactly w cells.
func selectedLine(t *theme.Theme, text string, sel bool, w int) string {
	if w <= 0 {
		return ""
	}
	if sel {
		return t.S.Base.Foreground(t.P.Acc).Render("›") + t.S.Selected.Render(textwidth.PadRight(" "+textwidth.Truncate(text, max(w-2, 0), "…"), max(w-1, 0)))
	}
	return fit(t, t.S.Base.Render("  ")+text, w)
}

// blankRows fills out to h lines of w cells.
func blankRows(t *theme.Theme, out []string, w, h int) string {
	for len(out) < h {
		out = append(out, fit(t, "", w))
	}
	out = out[:max(h, 0)]
	for i, l := range out {
		out[i] = fit(t, l, w)
	}
	return strings.Join(out, "\n")
}

// ---- menu: a fixed list of actions that can hand its box to the next dialog ----

// menuItem is one menu row: open replaces the menu in place with another
// overlay (so the stack never holds the finished menu: closing one overlay and
// opening another would be two messages whose order tea.Batch does not keep);
// otherwise run's Cmd follows closing the menu.
type menuItem struct {
	label string
	open  func(ctx *ui.Context) ui.Overlay
	run   func(ctx *ui.Context) tea.Cmd
}

// menu is feed.post_actions and feed.hashtag_pick. After a swap every method
// delegates to inner (the report dialog, a confirm, a user list, ...).
type menu struct {
	title string
	width int
	items []menuItem
	sel   int
	top   int
	size  ui.Size
	sized bool
	inner ui.Overlay
}

var (
	_ ui.Overlay      = (*menu)(nil)
	_ ui.KeyHandler   = (*menu)(nil)
	_ ui.MouseHandler = (*menu)(nil)
	_ ui.Cursorer     = (*menu)(nil)
	_ ui.Titled       = (*menu)(nil)
	_ ui.Helper       = (*menu)(nil)
)

func (m *menu) Init(*ui.Context) tea.Cmd { return nil }

func (m *menu) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if m.inner != nil {
		return m.inner.Update(ctx, msg)
	}
	return applyResults(ctx, msg)
}

func (m *menu) Resize(ctx *ui.Context, size ui.Size) {
	m.size, m.sized = size, true
	if m.inner != nil {
		m.inner.Resize(ctx, size)
	}
}

func (m *menu) Bounds(body layout.Rect) layout.Rect {
	if m.inner != nil {
		return m.inner.Bounds(body)
	}
	return centeredIn(body, min(m.width, body.W-4), len(m.items)+2)
}

func (m *menu) Modal() bool {
	if m.inner != nil {
		return m.inner.Modal()
	}
	return true
}

func (m *menu) Title(ctx *ui.Context) (string, ui.Badge) {
	if m.inner != nil {
		if t, ok := m.inner.(ui.Titled); ok {
			return t.Title(ctx)
		}
		return "", ui.Badge{}
	}
	return m.title, ui.Badge{}
}

func (m *menu) Help(ctx *ui.Context) []key.Binding {
	if m.inner != nil {
		if h, ok := m.inner.(ui.Helper); ok {
			return h.Help(ctx)
		}
		return nil
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "실행")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
	}
}

func (m *menu) Cursor() *tea.Cursor {
	if c, ok := m.inner.(ui.Cursorer); ok {
		return c.Cursor()
	}
	return nil
}

// swap hands the box to next (Init, and Resize to the current size; the app
// resizes it again when its Bounds differ).
func (m *menu) swap(ctx *ui.Context, next ui.Overlay) tea.Cmd {
	if next == nil {
		return closeOverlay
	}
	m.inner = next
	cmd := next.Init(ctx)
	if m.sized {
		next.Resize(ctx, m.size)
	}
	return cmd
}

func (m *menu) choose(ctx *ui.Context, i int) tea.Cmd {
	if i < 0 || i >= len(m.items) {
		return nil
	}
	it := m.items[i]
	if it.open != nil {
		return m.swap(ctx, it.open(ctx))
	}
	if it.run != nil {
		return tea.Batch(closeOverlay, it.run(ctx))
	}
	return closeOverlay
}

func (m *menu) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.inner != nil {
		if kh, ok := m.inner.(ui.KeyHandler); ok {
			return kh.HandleKey(ctx, msg)
		}
		return false, nil
	}
	km := ctx.Keys
	switch {
	case km.Matches(msg, keymap.ListUp):
		m.sel = max(m.sel-1, 0)
	case km.Matches(msg, keymap.ListDown):
		m.sel = min(m.sel+1, len(m.items)-1)
	case km.Matches(msg, keymap.ListTop):
		m.sel = 0
	case km.Matches(msg, keymap.ListBottom):
		m.sel = len(m.items) - 1
	case km.Matches(msg, keymap.ListOpen), msg.String() == "space":
		return true, m.choose(ctx, m.sel)
	default:
		return false, nil
	}
	return true, nil
}

func (m *menu) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if m.inner != nil {
		if mh, ok := m.inner.(ui.MouseHandler); ok {
			return mh.HandleMouse(ctx, msg, local)
		}
		return nil
	}
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft {
		return nil
	}
	if i := m.top + local.Y; local.Y >= 0 && i < len(m.items) {
		m.sel = i
		return m.choose(ctx, i)
	}
	return nil
}

func (m *menu) View(ctx *ui.Context, focused bool) string {
	if m.inner != nil {
		return m.inner.View(ctx, focused)
	}
	t := themeOf(ctx)
	w, h := m.size.W, m.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+h {
		m.top = m.sel - h + 1
	}
	var out []string
	for i := m.top; i < len(m.items) && len(out) < h; i++ {
		out = append(out, selectedLine(t, t.S.Base.Render(textwidth.Sanitize(m.items[i].label)), i == m.sel, w))
	}
	return blankRows(t, out, w, h)
}

// newPostActions is feed.post_actions for postID: who liked / reposted /
// quoted, the author's profile, then delete (mine) or report + block (others').
func newPostActions(ctx *ui.Context, postID string) *menu {
	if ctx == nil || ctx.Store == nil || postID == "" {
		return nil
	}
	fs := feedstore.Get(ctx.Store)
	if fs == nil {
		return nil
	}
	p := fs.Post(postID)
	if p == nil || p.Deleted {
		return nil
	}
	author := p.Author
	name := displayName(author)
	items := []menuItem{
		{label: textMenuLikes, open: func(ctx *ui.Context) ui.Overlay { return newUserList(ctx, listLikes+" "+postID) }},
		{label: textMenuReposts, open: func(ctx *ui.Context) ui.Overlay { return newUserList(ctx, listReposts+" "+postID) }},
		{label: textMenuQuotes, open: func(*ui.Context) ui.Overlay { return newQuotes(postID) }},
		{label: textMenuAuthor, run: func(*ui.Context) tea.Cmd {
			return ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: author.ID})
		}},
	}
	if ctx.Store.IsMe(author.ID) {
		items = append(items, menuItem{label: textMenuDelete, open: func(ctx *ui.Context) ui.Overlay {
			return widgets.Confirm(textDeleteAsk, "", textDeleteYes, textCancel, deletePost(ctx, postID))
		}})
	} else {
		items = append(items,
			menuItem{label: textMenuReport, open: func(*ui.Context) ui.Overlay {
				return widgets.ReportDialog("post", postID, name+"의 글")
			}},
			menuItem{label: textMenuBlock, open: func(ctx *ui.Context) ui.Overlay {
				return widgets.Confirm(name+textBlockAsk, textBlockBody, textBlockYes, textCancel, blockUser(ctx, author.ID))
			}},
		)
	}
	return &menu{title: textMenuTitle, width: menuW, items: items}
}

// newHashtagPick is feed.hashtag_pick: the post's hashtags, enter = RouteHashtag.
func newHashtagPick(ctx *ui.Context, postID string) *menu {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	fs := feedstore.Get(ctx.Store)
	if fs == nil {
		return nil
	}
	p := fs.Post(postID)
	if p == nil || p.Deleted || len(p.Hashtags) == 0 {
		return nil
	}
	m := &menu{title: textHashtagTitle, width: menuW}
	for _, tag := range p.Hashtags {
		m.items = append(m.items, menuItem{label: "#" + tag, run: func(*ui.Context) tea.Cmd {
			return ui.Navigate(ui.Route{Kind: ui.RouteHashtag, ID: tag})
		}})
	}
	return m
}

// ---- feed.user_list ----

// removeFollowerOutcome is a DELETE /v1/users/{user}/follower request.
type removeFollowerOutcome struct {
	claim
	userID string
}

// userList is feed.user_list: likes / reposts of a post, followers / following
// of a user, viewers of my story. enter = RouteProfile; on my own followers x
// removes the selected follower.
type userList struct {
	kind, ref   string
	title       string
	mine        bool // followers of me: x removes
	users       []api.UserSummary
	shown       []api.UserSummary // users minus blocked ones
	list        *widgets.List[api.UserSummary]
	cursor      *string
	loading     bool
	loadingMore bool
	err         error
	spin        *widgets.Spinner
	size        ui.Size
}

var (
	_ ui.Overlay      = (*userList)(nil)
	_ ui.KeyHandler   = (*userList)(nil)
	_ ui.MouseHandler = (*userList)(nil)
	_ ui.Titled       = (*userList)(nil)
	_ ui.Helper       = (*userList)(nil)
)

// newUserList parses args "<kind> <ref>"; nil when malformed.
func newUserList(ctx *ui.Context, args string) *userList {
	kind, ref, ok := strings.Cut(strings.TrimSpace(args), " ")
	ref = strings.TrimSpace(ref)
	if !ok || ref == "" {
		return nil
	}
	l := &userList{kind: kind, ref: ref, spin: widgets.NewSpinner()}
	switch kind {
	case listLikes:
		l.title = textMenuLikes
	case listReposts:
		l.title = textMenuReposts
	case listFollowers:
		l.title = textFollowersTitle
		me := ctx.Me()
		l.mine = ref == string(api.MeRef) || (me != nil && ref == me.ID)
	case listFollowing:
		l.title = textFollowingTitle
	case listStoryViewers:
		l.title = textViewersTitle
	default:
		return nil
	}
	l.list = widgets.NewList(func(u api.UserSummary) string { return u.ID }, l.renderRow)
	return l
}

func (l *userList) tag() string     { return "feed.user_list:" + l.kind + " " + l.ref }
func (l *userList) tagMore() string { return "feed.user_list_more:" + l.kind + " " + l.ref }

// fetch loads one page of the list.
func (l *userList) fetch(ctx *ui.Context, tag, cursor string) tea.Cmd {
	kind, ref := l.kind, l.ref
	opts := api.PageOpts{Cursor: cursor, Limit: listPageLimit}
	return ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.UserSummary], error) {
		if a == nil {
			return nil, errNoClient
		}
		switch kind {
		case listLikes:
			return a.ListLikes(c, ref, opts)
		case listReposts:
			return a.ListReposts(c, ref, opts)
		case listFollowers:
			return a.ListFollowers(c, api.UserRef(ref), opts)
		case listFollowing:
			return a.ListFollowing(c, api.UserRef(ref), opts)
		}
		vs, err := a.ListStoryViewers(c, ref, opts)
		if err != nil || vs == nil {
			return nil, err
		}
		out := &api.Page[api.UserSummary]{Items: make([]api.UserSummary, 0, len(vs.Items)), NextCursor: vs.NextCursor}
		for _, v := range vs.Items {
			out.Items = append(out.Items, v.User)
		}
		return out, nil
	})
}

func (l *userList) Init(ctx *ui.Context) tea.Cmd {
	l.loading = true
	return tea.Batch(l.spin.Start(ctx), l.fetch(ctx, l.tag(), ""))
}

// refilter re-reads users into the List without blocked users.
func (l *userList) refilter(ctx *ui.Context) {
	l.shown = l.shown[:0:0]
	for _, u := range l.users {
		if ctx == nil || ctx.Store == nil || !ctx.Store.IsBlocked(u.ID) {
			l.shown = append(l.shown, u)
		}
	}
	l.list.SetItems(l.shown)
}

func (l *userList) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	cmds := []tea.Cmd{applyResults(ctx, msg), l.spin.Update(ctx, msg)}
	switch m := msg.(type) {
	case ui.ResultMsg[*api.Page[api.UserSummary]]:
		switch m.Tag {
		case l.tag():
			l.loading = false
		case l.tagMore():
			l.loadingMore = false
		default:
			return tea.Batch(cmds...)
		}
		if !l.loading && !l.loadingMore {
			l.spin.Stop()
		}
		if m.Err != nil || m.Value == nil {
			l.err = m.Err
			cmds = append(cmds, func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextList} })
			break
		}
		if m.Tag == l.tag() {
			l.users = nil
		}
		seen := map[string]bool{}
		for _, u := range l.users {
			seen[u.ID] = true
		}
		for _, u := range m.Value.Items {
			if !seen[u.ID] {
				seen[u.ID] = true
				l.users = append(l.users, u)
			}
		}
		l.cursor = m.Value.NextCursor
		if len(m.Value.Items) == 0 {
			l.cursor = nil
		}
		l.refilter(ctx)
	case ui.ResultMsg[*removeFollowerOutcome]:
		if !l.mine || !strings.HasPrefix(m.Tag, tagRemoveFollower) || m.Value == nil {
			break
		}
		first := m.Value.take()
		if m.Err != nil {
			if first {
				cmds = append(cmds, func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextRemove} })
			}
			break
		}
		out := l.users[:0:0]
		for _, u := range l.users {
			if u.ID != m.Value.userID {
				out = append(out, u)
			}
		}
		l.users = out
		l.refilter(ctx)
		if first {
			cmds = append(cmds, ui.Toast(textRemoved, ui.ToastSuccess))
		}
	default:
		if blockSignal(msg) {
			l.refilter(ctx)
		}
	}
	return tea.Batch(cmds...)
}

func (l *userList) Resize(_ *ui.Context, size ui.Size) { l.size = size }

func (l *userList) Bounds(body layout.Rect) layout.Rect {
	return centeredIn(body, min(userListW, body.W-4), min(userListH, body.H-2))
}

func (l *userList) Modal() bool { return true }

func (l *userList) Title(*ui.Context) (string, ui.Badge) { return l.title, ui.Badge{} }

func (l *userList) Help(ctx *ui.Context) []key.Binding {
	b := []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "프로필"))}
	if l.mine {
		b = append(b, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "팔로워에서 빼기")))
	}
	return b
}

// maybeMore loads the next page when the selection is near the end.
func (l *userList) maybeMore(ctx *ui.Context) tea.Cmd {
	if l.cursor == nil || l.loading || l.loadingMore {
		return nil
	}
	sel, ok := l.list.Selected()
	if !ok {
		return nil
	}
	for i, u := range l.shown {
		if u.ID == sel.ID && i >= len(l.shown)-moreThreshold {
			l.loadingMore = true
			return tea.Batch(l.spin.Start(ctx), l.fetch(ctx, l.tagMore(), *l.cursor))
		}
	}
	return nil
}

func (l *userList) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		u, ok := l.list.Selected()
		if !ok {
			return true, nil
		}
		return true, tea.Batch(closeOverlay, ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: u.ID}))
	}
	if msg.String() == "x" {
		u, ok := l.list.Selected()
		if !l.mine || !ok {
			return true, nil
		}
		id := u.ID
		return true, ui.OpenOverlay(widgets.Confirm("@"+textwidth.Sanitize(u.Handle)+textRemoveAsk, textRemoveBody, textRemoveYes, textCancel,
			ui.Do(ctx, tagRemoveFollower+id, func(c context.Context, a *api.Client) (*removeFollowerOutcome, error) {
				o := &removeFollowerOutcome{userID: id}
				if a == nil {
					return o, errNoClient
				}
				return o, a.RemoveFollower(c, api.UserID(id))
			})))
	}
	handled, cmd := l.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, l.maybeMore(ctx))
}

func (l *userList) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	return tea.Batch(l.list.HandleMouse(ctx, msg, local), l.maybeMore(ctx))
}

func (l *userList) renderRow(ctx *ui.Context, u api.UserSummary, selected bool, width int) []string {
	t := themeOf(ctx)
	isMe := ctx != nil && ctx.Store != nil && ctx.Store.IsMe(u.ID)
	text := t.S.Bold.Render(handleOf(u))
	if n := displayName(u); n != "" {
		named := u
		named.DisplayName = n
		text += t.S.Base.Render(" ") + widgets.UserName(t, named, isMe)
	}
	return []string{selectedLine(t, text, selected, width)}
}

// statusLine is the spinner / error line shown instead of an empty list.
func statusLine(t *theme.Theme, spin *widgets.Spinner, loading bool, err error, w int) string {
	if loading {
		return fit(t, t.S.Base.Render("  ")+spin.View(t)+t.S.Dim.Render(" "+textLoading), w)
	}
	return fit(t, t.S.Base.Render("  ")+t.S.Error.Render(textwidth.Sanitize(api.Describe(err))), w)
}

func (l *userList) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := l.size.W, l.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	switch {
	case len(l.shown) == 0 && (l.loading || l.err != nil):
		return blankRows(t, []string{statusLine(t, l.spin, l.loading, l.err, w)}, w, h)
	case len(l.shown) == 0:
		return widgets.Empty(t, l.size, "", textNobody, "")
	}
	listH := h
	if l.loadingMore {
		listH = max(h-1, 0)
	}
	out := strings.Split(l.list.View(ctx, ui.Size{W: w, H: listH}), "\n")
	if l.loadingMore {
		out = append(out, statusLine(t, l.spin, true, nil, w))
	}
	return blankRows(t, out, w, h)
}

// ---- feed.quotes ----

// quotes is feed.quotes: posts quoting postID as one-line cards; enter = RoutePost.
type quotes struct {
	postID      string
	posts       []api.Post
	shown       []api.Post
	list        *widgets.List[api.Post]
	cursor      *string
	loading     bool
	loadingMore bool
	err         error
	spin        *widgets.Spinner
	size        ui.Size
}

var (
	_ ui.Overlay      = (*quotes)(nil)
	_ ui.KeyHandler   = (*quotes)(nil)
	_ ui.MouseHandler = (*quotes)(nil)
	_ ui.Titled       = (*quotes)(nil)
	_ ui.Helper       = (*quotes)(nil)
)

func newQuotes(postID string) *quotes {
	if postID == "" {
		return nil
	}
	q := &quotes{postID: postID, spin: widgets.NewSpinner()}
	q.list = widgets.NewList(func(p api.Post) string { return p.ID }, q.renderRow)
	return q
}

func (q *quotes) tag() string     { return "feed.quotes:" + q.postID }
func (q *quotes) tagMore() string { return "feed.quotes_more:" + q.postID }

func (q *quotes) fetch(ctx *ui.Context, tag, cursor string) tea.Cmd {
	id := q.postID
	return ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.Post], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListQuotes(c, id, api.PageOpts{Cursor: cursor, Limit: listPageLimit})
	})
}

func (q *quotes) Init(ctx *ui.Context) tea.Cmd {
	q.loading = true
	return tea.Batch(q.spin.Start(ctx), q.fetch(ctx, q.tag(), ""))
}

// cachedPost is the FeedStore copy of p (in-place updates show), else p.
func cachedPost(ctx *ui.Context, p api.Post) *api.Post {
	if ctx != nil && ctx.Store != nil {
		if fs := feedstore.Get(ctx.Store); fs != nil {
			if c := fs.Post(p.ID); c != nil {
				return c
			}
		}
	}
	return &p
}

// refilter re-reads posts into the List without blocked authors and deleted posts.
func (q *quotes) refilter(ctx *ui.Context) {
	q.shown = q.shown[:0:0]
	for _, p := range q.posts {
		cp := cachedPost(ctx, p)
		if !cp.Deleted && !hiddenByBlock(ctx, api.TimelineItem{Post: *cp}) {
			q.shown = append(q.shown, p)
		}
	}
	q.list.SetItems(q.shown)
}

func (q *quotes) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	cmds := []tea.Cmd{applyResults(ctx, msg), q.spin.Update(ctx, msg)}
	switch m := msg.(type) {
	case ui.ResultMsg[*api.Page[api.Post]]:
		switch m.Tag {
		case q.tag():
			q.loading = false
		case q.tagMore():
			q.loadingMore = false
		default:
			return tea.Batch(cmds...)
		}
		if !q.loading && !q.loadingMore {
			q.spin.Stop()
		}
		if m.Err != nil || m.Value == nil {
			q.err = m.Err
			cmds = append(cmds, func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextList} })
			break
		}
		if m.Tag == q.tag() {
			q.posts = nil
		}
		if ctx != nil && ctx.Store != nil {
			if fs := feedstore.Get(ctx.Store); fs != nil {
				fs.ObservePosts(m.Value.Items...)
			}
		}
		q.posts = append(q.posts, m.Value.Items...)
		q.cursor = m.Value.NextCursor
		if len(m.Value.Items) == 0 {
			q.cursor = nil
		}
		q.refilter(ctx)
	case ui.ResultMsg[*toggleOutcome], ui.ResultMsg[*deleteOutcome], ui.EventMsg:
		q.refilter(ctx)
	default:
		if blockSignal(msg) {
			q.refilter(ctx)
		}
	}
	return tea.Batch(cmds...)
}

func (q *quotes) Resize(_ *ui.Context, size ui.Size) { q.size = size }

func (q *quotes) Bounds(body layout.Rect) layout.Rect {
	return centeredIn(body, min(quotesW, body.W-4), min(userListH, body.H-2))
}

func (q *quotes) Modal() bool { return true }

func (q *quotes) Title(*ui.Context) (string, ui.Badge) { return textMenuQuotes, ui.Badge{} }

func (q *quotes) Help(*ui.Context) []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "글 보기"))}
}

func (q *quotes) maybeMore(ctx *ui.Context) tea.Cmd {
	if q.cursor == nil || q.loading || q.loadingMore {
		return nil
	}
	sel, ok := q.list.Selected()
	if !ok {
		return nil
	}
	for i, p := range q.shown {
		if p.ID == sel.ID && i >= len(q.shown)-moreThreshold {
			q.loadingMore = true
			return tea.Batch(q.spin.Start(ctx), q.fetch(ctx, q.tagMore(), *q.cursor))
		}
	}
	return nil
}

func (q *quotes) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		p, ok := q.list.Selected()
		if !ok {
			return true, nil
		}
		return true, tea.Batch(closeOverlay, ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: p.ID}))
	}
	handled, cmd := q.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, q.maybeMore(ctx))
}

func (q *quotes) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	return tea.Batch(q.list.HandleMouse(ctx, msg, local), q.maybeMore(ctx))
}

func (q *quotes) renderRow(ctx *ui.Context, p api.Post, selected bool, width int) []string {
	cp := cachedPost(ctx, p)
	return renderCard(ctx, api.TimelineItem{ID: p.ID, Type: "post", Post: *cp, At: cp.CreatedAt}, cp,
		cardOpts{Width: width, Density: ui.DensityLine, Selected: selected, PollSel: -1}).Lines
}

func (q *quotes) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := q.size.W, q.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	switch {
	case len(q.shown) == 0 && (q.loading || q.err != nil):
		return blankRows(t, []string{statusLine(t, q.spin, q.loading, q.err, w)}, w, h)
	case len(q.shown) == 0:
		return widgets.Empty(t, q.size, "", textNoQuotes, "")
	}
	listH := h
	if q.loadingMore {
		listH = max(h-1, 0)
	}
	out := strings.Split(q.list.View(ctx, ui.Size{W: w, H: listH}), "\n")
	if q.loadingMore {
		out = append(out, statusLine(t, q.spin, true, nil, w))
	}
	return blankRows(t, out, w, h)
}
