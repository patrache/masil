package feed

import (
	"context"
	"encoding/json"
	"path"
	"reflect"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteProfile, newProfile)
}

// profileTab is a profile sub-tab.
type profileTab int

const (
	tabPosts profileTab = iota
	tabReplies
	tabMedia
	tabGuestbook
)

// profileTabs is the tab order: label, ListUserPosts filter ("" = not a post
// list) and the Route.Query["tab"] value that opens it.
var profileTabs = []struct{ label, filter, query string }{
	{"글", "posts", "posts"},
	{"답글", "replies", "replies"},
	{"미디어", "media", "media"},
	{"방명록", "", "guestbook"},
}

// postTabs is the number of post-list tabs (글 · 답글 · 미디어).
const postTabs = 3

// Profile layout.
const (
	avatarCols  = 8 // avatar box (image or text chip) in the Full header
	avatarRows  = 4
	bioMaxLines = 4
	fullMinW    = 40 // narrower panes get the compact header even at DensityFull
)

// Profile copy.
const (
	textProfileTitle      = "프로필"
	textProfileLoadFailed = "프로필을 불러오지 못했어요"
	textUserNotFound      = "사용자를 찾을 수 없어요"
	hintGoBack            = "esc 로 돌아가기"
	textPrivateTitle      = "비공개 계정이에요"
	hintPrivate           = "팔로우 요청이 승인되면 글을 볼 수 있어요"
	textBlockedTitle      = "차단한 사용자예요"
	hintBlocked           = "차단 해제는 ctrl+k → 차단 목록"
	textSuspendedTitle    = "정지된 계정이에요"
	textCannotView        = "글을 볼 수 없어요"
	textNoProfilePosts    = "아직 글이 없어요"
	textNoProfileReplies  = "아직 답글이 없어요"
	textNoProfileMedia    = "아직 사진이 없어요"
	chipFollow            = "[팔로우]"
	chipFollowing         = "[팔로잉 ✓]"
	chipRequested         = "[요청됨]"
	chipEditProfile       = "[프로필 편집]"
	textFriend            = "친구"
	textFollowsYou        = "나를 팔로우해요"
	textRequestsCount     = "팔로우 요청 "
	textUnfollowAsk       = "팔로우를 끊을까요?"
	textUnfollowYes       = "끊기"
	textFollowRequested   = "팔로우를 요청했어요"
	textMenuStories       = "스토리 보기"
	textMenuBlockUser     = "차단"
	textMenuReportUser    = "사용자 신고"
	textMenuEditProfile   = "프로필 편집"
	textMenuFollowReqs    = "팔로우 요청"
	errContextFollow      = "팔로우"
	errContextProfile     = "프로필"
)

// presenceLabels is the profile header's presence wording.
var presenceLabels = map[api.PresenceStatus]string{
	api.PresenceOnline:  "온라인",
	api.PresenceAway:    "자리 비움",
	api.PresenceDND:     "방해 금지",
	api.PresenceOffline: "오프라인",
}

// Profile result tags (ui.Do).
const (
	tagProfile          = "feed.profile:"            // + route id
	tagProfilePosts     = "feed.profile_posts:"      // + route id + " " + filter: first page
	tagProfilePostsMore = "feed.profile_posts_more:" // + route id + " " + filter: next page
	tagProfileRequests  = "feed.profile_requests:"   // + route id: my pending follow requests (count)
	tagFollow           = "feed.follow:"             // + user id
	tagUnfollow         = "feed.unfollow:"           // + user id
)

// Chip actions (a click on the header chip line).
const (
	chipActFollow   = "follow"
	chipActEdit     = "edit"
	chipActRequests = "requests"
)

// userRef is the API reference of a RouteProfile ID: "@me" -> api.MeRef,
// "@x" -> api.Handle("x"), anything else a user ID.
func userRef(id string) api.UserRef {
	switch {
	case id == string(api.MeRef):
		return api.MeRef
	case strings.HasPrefix(id, "@"):
		return api.Handle(id)
	}
	return api.UserID(id)
}

// tabFromQuery is the tab Route.Query["tab"] names.
func tabFromQuery(q map[string]string) (profileTab, bool) {
	v, ok := q["tab"]
	if !ok {
		return tabPosts, false
	}
	for i, t := range profileTabs {
		if t.query == v {
			return profileTab(i), true
		}
	}
	return tabPosts, false
}

// followOutcome is a PUT / DELETE /v1/users/{user}/follow request.
type followOutcome struct {
	claim
	userID   string
	unfollow bool
	state    string // PUT answer: active | pending
}

// profileList is one post-list tab: the server items (raw, in server order)
// and the shown ones (blocked authors / reposters dropped).
type profileList struct {
	filter      string
	raw         []api.TimelineItem
	items       []api.TimelineItem
	list        *widgets.List[api.TimelineItem]
	views       map[viewKey]cardView
	cursor      *string
	loaded      bool
	loading     bool
	loadingMore bool
	err         error
}

// redraw drops the cached renders without re-reading the items.
func (l *profileList) redraw() {
	l.views = nil
	l.list.SetItems(l.items)
}

// chipSpan is a clickable chip on the header's chip line.
type chipSpan struct {
	x0, x1 int
	act    string
}

// profile is RouteProfile: the header (avatar, name, presence, status, bio,
// counts, relationship chips) above the 글 · 답글 · 미디어 · 방명록 tabs.
type profile struct {
	id  string // route ID: user id, "@handle" or "@me"
	ref api.UserRef

	prof       *api.Profile
	loading    bool
	loadFailed bool
	notFound   bool
	following  bool // a follow / unfollow request of this screen is in flight

	tab   profileTab
	tabs  *widgets.Tabs
	lists [postTabs]*profileList
	gb    *guestbookTab // the 방명록 tab (guestbook_tab.go)

	reqCount       int // my pending follow requests (first page), when reqKnown
	reqKnown       bool
	reqLoading     bool
	storiesLoading bool

	imgs imageSlots
	spin *widgets.Spinner
	size ui.Size

	// geometry of the last View (pane-local rows; -1 = not drawn)
	tabY, chipY, listY, listH int
	chips                     []chipSpan
	visible                   []string
}

var (
	_ ui.Component    = (*profile)(nil)
	_ ui.KeyHandler   = (*profile)(nil)
	_ ui.MouseHandler = (*profile)(nil)
	_ ui.Titled       = (*profile)(nil)
	_ ui.Helper       = (*profile)(nil)
	_ ui.Watcher      = (*profile)(nil)
)

// newProfile builds RouteProfile (ID = user id, "@handle" or "@me";
// Query["tab"] = posts | replies | media | guestbook picks the first tab).
func newProfile(_ *ui.Context, r ui.Route) ui.Component {
	p := &profile{
		id:    r.ID,
		ref:   userRef(r.ID),
		tabs:  widgets.NewTabs(),
		gb:    newGuestbookTab(),
		spin:  widgets.NewSpinner(),
		tabY:  -1,
		chipY: -1,
		listY: -1,
	}
	p.tab, _ = tabFromQuery(r.Query)
	for i := range p.lists {
		l := &profileList{filter: profileTabs[i].filter}
		l.list = widgets.NewList(func(it api.TimelineItem) string { return it.ID }, p.renderer(l))
		p.lists[i] = l
	}
	return p
}

func (p *profile) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init loads the profile (and the story feed for the ring mark when the
// FeedStore has none yet); the tab list follows the profile.
func (p *profile) Init(ctx *ui.Context) tea.Cmd {
	cmds := []tea.Cmd{p.loadProfile(ctx)}
	if fs := p.fs(ctx); fs != nil && fs.Stories() == nil {
		p.storiesLoading = true
		cmds = append(cmds, ui.Do(ctx, tagStories, func(c context.Context, a *api.Client) ([]api.StoryGroup, error) {
			if a == nil {
				return nil, errNoClient
			}
			return a.StoryFeed(c)
		}))
	}
	return tea.Batch(cmds...)
}

func (p *profile) loadProfile(ctx *ui.Context) tea.Cmd {
	if p.loading {
		return nil
	}
	p.loading = true
	ref := p.ref
	return tea.Batch(p.spin.Start(ctx), ui.Do(ctx, tagProfile+p.id, func(c context.Context, a *api.Client) (*api.Profile, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.GetUser(c, ref)
	}))
}

func (p *profile) listTag(l *profileList, more bool) string {
	if more {
		return tagProfilePostsMore + p.id + " " + l.filter
	}
	return tagProfilePosts + p.id + " " + l.filter
}

// loadList requests a page of l (cursor "" = the first page, replacing it).
func (p *profile) loadList(ctx *ui.Context, l *profileList, cursor string) tea.Cmd {
	if p.prof == nil || l.loading || l.loadingMore {
		return nil
	}
	more := cursor != ""
	if more {
		l.loadingMore = true
	} else {
		l.loading = true
	}
	ref, filter := api.UserID(p.prof.ID), l.filter
	return tea.Batch(p.spin.Start(ctx), ui.Do(ctx, p.listTag(l, more), func(c context.Context, a *api.Client) (*api.Page[api.TimelineItem], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListUserPosts(c, ref, filter, api.PageOpts{Cursor: cursor})
	}))
}

// ensureTab loads the active tab (posts or guestbook) the first time it can
// be shown.
func (p *profile) ensureTab(ctx *ui.Context) tea.Cmd {
	if p.tab == tabGuestbook {
		return p.ensureGuestbook(ctx)
	}
	if int(p.tab) >= postTabs || !p.canView(ctx) {
		return nil
	}
	if l := p.lists[p.tab]; !l.loaded && !l.loading {
		return p.loadList(ctx, l, "")
	}
	return nil
}

// loadRequests counts my pending follow requests (first page) for the header
// of my private profile.
func (p *profile) loadRequests(ctx *ui.Context) tea.Cmd {
	if p.prof == nil || !p.isMe(ctx) || !p.prof.IsPrivate || p.reqLoading {
		return nil
	}
	p.reqLoading = true
	return ui.Do(ctx, tagProfileRequests+p.id, func(c context.Context, a *api.Client) (*api.Page[api.FollowRequestEntry], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListFollowRequests(c, api.PageOpts{})
	})
}

// ---- state ----

func (p *profile) rel() *api.Relationship {
	if p.prof == nil {
		return nil
	}
	return p.prof.Relationship
}

func (p *profile) isMe(ctx *ui.Context) bool {
	if p.prof == nil {
		return false
	}
	if ctx != nil && ctx.Store != nil && ctx.Store.IsMe(p.prof.ID) {
		return true
	}
	return p.id == string(api.MeRef) || p.prof.Relationship == nil
}

// blocked: I blocked this user (the profile's flag or the Store's block list).
func (p *profile) blocked(ctx *ui.Context) bool {
	if p.prof == nil || p.isMe(ctx) {
		return false
	}
	if r := p.rel(); r != nil && r.Blocked {
		return true
	}
	return ctx != nil && ctx.Store != nil && ctx.Store.IsBlocked(p.prof.ID)
}

// canView reports whether the tabs (posts, guestbook) can be shown.
func (p *profile) canView(ctx *ui.Context) bool {
	if p.prof == nil || p.blocked(ctx) || p.prof.Suspended {
		return false
	}
	r := p.rel()
	return r == nil || r.CanViewPosts
}

// canStories is canView: the stories of a blocked, suspended or locked
// private user are never requested.
func (p *profile) canStories(ctx *ui.Context) bool { return p.canView(ctx) }

func (p *profile) storiesRoute() ui.Route {
	return ui.Route{Kind: ui.RouteStories, ID: p.prof.ID}
}

// ---- Update ----

func (p *profile) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg), p.guestbookUpdate(ctx, msg)}
	force := true
	imagesChanged := false
	switch m := msg.(type) {
	case ui.ResultMsg[*api.Profile]:
		cmds = append(cmds, p.onProfile(ctx, m))
	case ui.ResultMsg[*api.Page[api.TimelineItem]]:
		cmds = append(cmds, p.onPage(ctx, m))
	case ui.ResultMsg[*api.Page[api.FollowRequestEntry]]:
		if m.Tag == tagProfileRequests+p.id && p.reqLoading {
			p.reqLoading = false
			if m.Err == nil && m.Value != nil {
				p.reqCount, p.reqKnown = len(m.Value.Items), true
			}
		}
	case ui.ResultMsg[*followOutcome]:
		cmds = append(cmds, p.onFollow(ctx, m))
	case ui.ResultMsg[*followRequestOutcome]:
		p.onRequestHandled(ctx, m)
	case ui.ResultMsg[*api.Me]:
		p.onSaved(ctx, m)
	case ui.ResultMsg[*blockOutcome]:
		if m.Err == nil && m.Value != nil && p.prof != nil && m.Value.userID == p.prof.ID {
			if r := p.rel(); r != nil {
				r.Blocked = true
			}
		}
	case ui.ResultMsg[[]api.StoryGroup]:
		if m.Tag == tagStories && p.storiesLoading {
			p.storiesLoading = false
			if fs := p.fs(ctx); fs != nil && m.Err == nil {
				fs.SetStories(m.Value)
			}
		}
	case ui.EventMsg:
		cmds = append(cmds, p.onEvent(ctx, m))
	case ui.RouteUpdatedMsg:
		if t, ok := tabFromQuery(m.Route.Query); ok {
			cmds = append(cmds, p.setTab(ctx, t))
		}
	case ui.ModeChangedMsg, ui.ThemeChangedMsg:
		imagesChanged = true
	default:
		if rt := reflect.TypeOf(msg); rt != nil && rt.PkgPath() == widgetsPkg {
			force = false // spinner ticks
		}
	}
	if id, _, ok := p.imgs.loaded(msg); ok {
		cmds = append(cmds, p.renderMedia(ctx, id))
	}
	cmds = append(cmds, p.spin.Update(ctx, msg))
	if p.imgs.hasWants() {
		cmds = append(cmds, p.imgs.flush(ctx))
		force = true
	}
	cmds = append(cmds, p.sync(ctx, force))
	if imagesChanged {
		cmds = append(cmds, p.rerenderImages(ctx))
	}
	return tea.Batch(cmds...)
}

func (p *profile) stopSpinner() {
	if p.loading || p.gb.loading || p.gb.loadingMore {
		return
	}
	for _, l := range p.lists {
		if l.loading || l.loadingMore {
			return
		}
	}
	p.spin.Stop()
}

func profileErr(err error, context string) tea.Cmd {
	if err == nil {
		return nil
	}
	return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: context} }
}

// onProfile stores the loaded profile and starts what depends on it.
func (p *profile) onProfile(ctx *ui.Context, m ui.ResultMsg[*api.Profile]) tea.Cmd {
	if m.Tag != tagProfile+p.id || !p.loading {
		return nil
	}
	p.loading = false
	p.stopSpinner()
	if m.Err != nil || m.Value == nil {
		if api.IsCode(m.Err, "not_found") {
			p.notFound = true
			return nil
		}
		if p.prof == nil {
			p.loadFailed = true
		}
		return profileErr(m.Err, errContextProfile)
	}
	p.prof = m.Value
	p.loadFailed, p.notFound = false, false
	return tea.Batch(p.ensureTab(ctx), p.loadRequests(ctx))
}

// onPage stores a post-list page this instance requested.
func (p *profile) onPage(ctx *ui.Context, m ui.ResultMsg[*api.Page[api.TimelineItem]]) tea.Cmd {
	for _, l := range p.lists {
		more := false
		switch m.Tag {
		case p.listTag(l, false):
			if !l.loading {
				return nil
			}
			l.loading = false
		case p.listTag(l, true):
			if !l.loadingMore {
				return nil
			}
			l.loadingMore, more = false, true
		default:
			continue
		}
		p.stopSpinner()
		if m.Err != nil || m.Value == nil {
			if !more {
				l.err = m.Err
				l.loaded = true
			}
			return profileErr(m.Err, errContextFeed)
		}
		l.err = nil
		l.loaded = true
		if fs := p.fs(ctx); fs != nil {
			for _, it := range m.Value.Items {
				fs.ObservePosts(it.Post)
			}
		}
		if more {
			seen := make(map[string]bool, len(l.raw))
			for _, it := range l.raw {
				seen[it.ID] = true
			}
			for _, it := range m.Value.Items {
				if !seen[it.ID] {
					l.raw = append(l.raw, it)
				}
			}
		} else {
			l.raw = append([]api.TimelineItem(nil), m.Value.Items...)
		}
		l.cursor = m.Value.NextCursor
		if len(m.Value.Items) == 0 {
			l.cursor = nil // an empty page never loops
		}
		return nil
	}
	return nil
}

// onFollow applies a follow / unfollow answer for this profile's user.
func (p *profile) onFollow(ctx *ui.Context, m ui.ResultMsg[*followOutcome]) tea.Cmd {
	o := m.Value
	if o == nil || p.prof == nil || o.userID != p.prof.ID {
		return nil
	}
	p.following = false
	first := o.take()
	if m.Err != nil {
		if first {
			return profileErr(m.Err, errContextFollow)
		}
		return nil
	}
	r := p.rel()
	if r == nil {
		return nil
	}
	prev := r.Following
	if o.unfollow {
		r.Following = "none"
		if prev == "active" {
			p.prof.FollowerCount = max(p.prof.FollowerCount-1, 0)
			if p.prof.IsPrivate {
				r.CanViewPosts = false
			}
		}
		return nil
	}
	r.Following = o.state
	switch o.state {
	case "active":
		if prev != "active" {
			p.prof.FollowerCount++
		}
		r.CanViewPosts = true
		return p.ensureTab(ctx)
	case "pending":
		if first && prev != "pending" {
			return ui.Toast(textFollowRequested, ui.ToastSuccess)
		}
	}
	return nil
}

// onRequestHandled keeps my header in step with feed.follow_requests.
func (p *profile) onRequestHandled(ctx *ui.Context, m ui.ResultMsg[*followRequestOutcome]) {
	if m.Err != nil || m.Value == nil || !p.isMe(ctx) {
		return
	}
	if p.reqKnown {
		p.reqCount = max(p.reqCount-1, 0)
	}
	if m.Value.accept {
		p.prof.FollowerCount++
	}
}

// onSaved shows my saved profile (feed.profile_edit's PATCH answer).
func (p *profile) onSaved(ctx *ui.Context, m ui.ResultMsg[*api.Me]) {
	if !strings.HasPrefix(m.Tag, tagProfileSave) || m.Err != nil || m.Value == nil || !p.isMe(ctx) || m.Value.ID != p.prof.ID {
		return
	}
	p.prof.DisplayName = m.Value.DisplayName
	p.prof.Bio = m.Value.Bio
	p.prof.AvatarURL = m.Value.AvatarURL
}

// onEvent handles user.updated (header) and block.updated for this user (a
// block hides the tabs; an unblock reloads the profile and its lists).
func (p *profile) onEvent(ctx *ui.Context, m ui.EventMsg) tea.Cmd {
	if p.prof == nil {
		return nil
	}
	switch m.Event.Type {
	case "user.updated":
		var d struct {
			User api.UserSummary `json:"user"`
		}
		if json.Unmarshal(m.Event.Data, &d) == nil && d.User.ID == p.prof.ID {
			p.prof.UserSummary = d.User
		}
	case "block.updated":
		var d struct {
			UserID  string `json:"user_id"`
			Blocked bool   `json:"blocked"`
		}
		if json.Unmarshal(m.Event.Data, &d) != nil || d.UserID != p.prof.ID {
			return nil
		}
		if r := p.rel(); r != nil {
			r.Blocked = d.Blocked
		}
		if !d.Blocked {
			for _, l := range p.lists {
				l.raw, l.cursor, l.loaded, l.err = nil, nil, false, nil
			}
			p.gb.reset()
			return p.loadProfile(ctx)
		}
	}
	return nil
}

// setTab switches the sub-tab, loading a post tab the first time.
func (p *profile) setTab(ctx *ui.Context, t profileTab) tea.Cmd {
	if t < 0 || int(t) >= len(profileTabs) {
		return nil
	}
	p.tab = t
	return tea.Batch(p.ensureTab(ctx), p.sync(ctx, true))
}

// build is l's items minus blocked authors / reposters (Task 5 block filter).
func (p *profile) build(ctx *ui.Context, l *profileList) []api.TimelineItem {
	out := make([]api.TimelineItem, 0, len(l.raw))
	for _, it := range l.raw {
		if !hiddenByBlock(ctx, it) {
			out = append(out, it)
		}
	}
	return out
}

// sync re-reads every tab list into its List (unless force, only when the IDs
// changed); a list that was empty starts at the top.
func (p *profile) sync(ctx *ui.Context, force bool) tea.Cmd {
	for _, l := range p.lists {
		items := p.build(ctx, l)
		if !force && sameIDs(items, l.items) {
			continue
		}
		wasEmpty := len(l.items) == 0
		l.items = items
		l.views = nil
		l.list.SetItems(items)
		if wasEmpty && len(items) > 0 {
			l.list.Restore(widgets.ScrollAnchor{ItemID: items[0].ID})
		}
	}
	p.syncGuestbook(ctx, force)
	return p.requestImages(ctx)
}

// post is the cached copy of it's post (in-place updates show), else it's own.
func (p *profile) post(ctx *ui.Context, it api.TimelineItem) *api.Post {
	if fs := p.fs(ctx); fs != nil {
		if c := fs.Post(it.Post.ID); c != nil {
			return c
		}
	}
	cp := it.Post
	return &cp
}

// ---- images ----

// avatarMedia is the profile's avatar as a media reference (id = upload id).
func (p *profile) avatarMedia() (api.Media, bool) {
	if p.prof == nil || p.prof.AvatarURL == nil || *p.prof.AvatarURL == "" {
		return api.Media{}, false
	}
	u := *p.prof.AvatarURL
	return api.Media{ID: path.Base(u), URL: u}, true
}

// fullHeader reports whether the header draws the avatar column.
func (p *profile) fullHeader(ctx *ui.Context) bool {
	return ctx.Density() == ui.DensityFull && p.size.W >= fullMinW
}

func (p *profile) activeList() *profileList {
	if int(p.tab) < postTabs {
		return p.lists[p.tab]
	}
	return nil
}

// imageItems are the active tab's cards that draw their first image (only
// cards on or near the screen when near is set).
func (p *profile) imageItems(ctx *ui.Context, near ...[]bool) []api.Media {
	l := p.activeList()
	if l == nil || !p.canView(ctx) {
		return nil
	}
	if d := ctx.Density(); d != ui.DensityFull && d != ui.DensityBubble {
		return nil
	}
	var out []api.Media
	for i, it := range l.items {
		if len(near) > 0 && !near[0][i] {
			continue
		}
		if m, ok := firstMedia(p.post(ctx, it)); ok {
			out = append(out, m)
		}
	}
	return out
}

// requestImages loads the avatar and the first image of each active-tab card
// on or near the screen, and forgets every other image (imageSlots.track).
func (p *profile) requestImages(ctx *ui.Context) tea.Cmd {
	var ms []api.Media
	if l := p.activeList(); l != nil {
		w, h := viewSize(p.size.W, p.listH)
		if p.listH <= 0 && p.size.H > 0 {
			h = p.size.H
		}
		sel := ""
		if it, ok := l.list.Selected(); ok {
			sel = it.ID
		}
		near := nearItems(l.list, l.items, func(it api.TimelineItem) string { return it.ID }, h, func(it api.TimelineItem) int {
			return len(p.view(ctx, l, it, it.ID == sel, w).Lines)
		})
		ms = p.imageItems(ctx, near)
	}
	if m, ok := p.avatarMedia(); ok && p.fullHeader(ctx) {
		ms = append(ms, m)
	}
	return p.imgs.track(ctx, ms...)
}

// renderMedia renders a newly arrived image wherever it shows, so a kitty Setup
// leaves with this Update.
func (p *profile) renderMedia(ctx *ui.Context, id string) tea.Cmd {
	var cmds []tea.Cmd
	if m, ok := p.avatarMedia(); ok && m.ID == id && p.fullHeader(ctx) {
		cmds = append(cmds, p.imgs.render(ctx, m, avatarCols, avatarRows))
	}
	for _, m := range p.imageItems(ctx) {
		if m.ID == id {
			cols, rows := imageBox(m, p.size.W-gutterW, ctx.Density())
			cmds = append(cmds, p.imgs.render(ctx, m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// rerenderImages renders every loaded image for the current boxes and theme.
func (p *profile) rerenderImages(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	if m, ok := p.avatarMedia(); ok && p.fullHeader(ctx) && p.imgs.image(m.ID) != nil {
		cmds = append(cmds, p.imgs.render(ctx, m, avatarCols, avatarRows))
	}
	for _, m := range p.imageItems(ctx) {
		if p.imgs.image(m.ID) != nil {
			cols, rows := imageBox(m, p.size.W-gutterW, ctx.Density())
			cmds = append(cmds, p.imgs.render(ctx, m, cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

// ---- rendering ----

// renderer is l's List render callback (cached cards).
func (p *profile) renderer(l *profileList) func(*ui.Context, api.TimelineItem, bool, int) []string {
	return func(ctx *ui.Context, it api.TimelineItem, selected bool, width int) []string {
		return p.view(ctx, l, it, selected, width).Lines
	}
}

func (p *profile) view(ctx *ui.Context, l *profileList, it api.TimelineItem, selected bool, width int) cardView {
	k := viewKey{id: it.ID, width: width, density: ctx.Density(), selected: selected, pollSel: -1}
	if v, ok := l.views[k]; ok {
		return v
	}
	v := renderCard(ctx, it, p.post(ctx, it), cardOpts{
		Width: width, Density: ctx.Density(), Selected: selected, PollSel: -1,
		Image: func(m api.Media, cols, rows int) []string { return p.imgs.cell(ctx, m, cols, rows) },
	})
	if l.views == nil {
		l.views = map[viewKey]cardView{}
	}
	l.views[k] = v
	return v
}

func (p *profile) viewsFn(ctx *ui.Context, l *profileList) func(api.TimelineItem) cardView {
	sel := ""
	if it, ok := l.list.Selected(); ok {
		sel = it.ID
	}
	return func(it api.TimelineItem) cardView { return p.view(ctx, l, it, it.ID == sel, p.size.W) }
}

// storyMark is "◉ " (unseen group, acc) / "○ " (seen, dim) / "" (no group).
func (p *profile) storyMark(ctx *ui.Context, t *theme.Theme) (string, int) {
	fs := p.fs(ctx)
	if fs == nil || p.prof == nil {
		return "", 0
	}
	for _, g := range fs.Stories() {
		if g.User.ID != p.prof.ID {
			continue
		}
		if g.HasUnseen {
			return t.S.Base.Foreground(t.P.Acc).Bold(true).Render(ringUnseen) + t.S.Base.Render(" "), 2
		}
		return t.S.Dim.Render(ringSeen) + t.S.Base.Render(" "), 2
	}
	return "", 0
}

// nameLine is "◉ 이름 @handle ● 온라인" in exactly w cells: the presence goes
// first, then the name is cut before the handle.
func (p *profile) nameLine(ctx *ui.Context, w int, presenceLabel bool) string {
	t := themeOf(ctx)
	u := p.prof.UserSummary
	mark, markW := p.storyMark(ctx, t)
	pres, presW := "", 0
	if p.prof.Presence != nil {
		st := api.PresenceStatus(*p.prof.Presence)
		pres, presW = t.S.Base.Render(" ")+widgets.PresenceDot(t, st), 2
		if label := presenceLabels[st]; presenceLabel && label != "" {
			pres += t.S.Base.Render(" " + label)
			presW += 1 + textwidth.Width(label)
		}
	}
	room := w - markW - presW
	if room < 4 {
		pres, presW = "", 0
		room = w - markW
	}
	h := handleOf(u)
	var who string
	if hw := textwidth.Width(h); room-hw-1 >= 2 {
		n := displayName(u)
		if textwidth.Width(n) > room-hw-1 {
			n = textwidth.Truncate(n, room-hw-1, "…")
		}
		named := u
		named.DisplayName = n
		who = widgets.UserName(t, named, p.isMe(ctx)) + t.S.Base.Render(" ") + t.S.Dim.Render(h)
	} else {
		who = t.S.Dim.Render(textwidth.Truncate(h, max(room, 0), "…"))
	}
	return fit(t, mark+who+pres, w)
}

// statusLine is "🎧 듣는 중" (nil when there is no unexpired status).
func (p *profile) statusLine(ctx *ui.Context, w int) (string, bool) {
	s := p.prof.Status
	if s == nil || (s.ExpiresAt != nil && !s.ExpiresAt.After(ctx.Now())) {
		return "", false
	}
	text := textwidth.Sanitize(s.Text)
	if s.Emoji != nil && *s.Emoji != "" {
		text = textwidth.Sanitize(*s.Emoji) + " " + text
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	t := themeOf(ctx)
	return fit(t, t.S.Base.Render(textwidth.Truncate(text, w, "…")), w), true
}

// bioLines is the bio wrapped to w, at most n lines (the last cut with "…").
func (p *profile) bioLines(ctx *ui.Context, w, n int) []string {
	t := themeOf(ctx)
	if w <= 0 || n <= 0 {
		return nil
	}
	var lines []string
	for _, para := range strings.Split(p.prof.Bio, "\n") {
		para = strings.TrimSpace(textwidth.Sanitize(para))
		if para == "" {
			continue
		}
		lines = append(lines, textwidth.Wrap(para, w)...)
	}
	if len(lines) > n {
		lines = lines[:n]
		last := lines[n-1]
		if textwidth.Width(last)+1 > w {
			last = textwidth.Truncate(last, w-1, "")
		}
		lines[n-1] = last + "…"
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fit(t, t.S.Base.Render(l), w)
	}
	return out
}

// countsText is "팔로워 120 · 팔로잉 80 · 글 42" (numbers bold).
func (p *profile) countsText(t *theme.Theme) string {
	part := func(label string, n int) string {
		return t.S.Dim.Render(label+" ") + t.S.Bold.Render(humanize.Count(n))
	}
	sep := t.S.Dim.Render(" · ")
	return part("팔로워", p.prof.FollowerCount) + sep + part("팔로잉", p.prof.FollowingCount) + sep + part("글", p.prof.PostCount)
}

// chipSeg is one chip of the relationship line.
type chipSeg struct {
	text string
	st   func(*theme.Theme) string
	act  string
}

// chipSegs are the relationship chips: [팔로우] / [팔로잉 ✓] / [요청됨], 친구,
// 나를 팔로우해요; on my profile [프로필 편집] and (private) 팔로우 요청 N.
func (p *profile) chipSegs(ctx *ui.Context) []chipSeg {
	acc := func(s string) func(*theme.Theme) string {
		return func(t *theme.Theme) string { return t.S.Base.Foreground(t.P.Acc).Bold(true).Render(s) }
	}
	base := func(s string) func(*theme.Theme) string {
		return func(t *theme.Theme) string { return t.S.Base.Render(s) }
	}
	dim := func(s string) func(*theme.Theme) string {
		return func(t *theme.Theme) string { return t.S.Dim.Render(s) }
	}
	var out []chipSeg
	if p.isMe(ctx) {
		out = append(out, chipSeg{chipEditProfile, acc(chipEditProfile), chipActEdit})
		if p.prof.IsPrivate && p.reqKnown {
			s := textRequestsCount + humanize.Count(p.reqCount)
			out = append(out, chipSeg{s, acc(s), chipActRequests})
		}
		return out
	}
	r := p.rel()
	if r == nil || p.blocked(ctx) || p.prof.Suspended {
		return nil
	}
	switch r.Following {
	case "active":
		out = append(out, chipSeg{chipFollowing, base(chipFollowing), chipActFollow})
	case "pending":
		out = append(out, chipSeg{chipRequested, dim(chipRequested), chipActFollow})
	default:
		out = append(out, chipSeg{chipFollow, acc(chipFollow), chipActFollow})
	}
	if r.IsFriend {
		out = append(out, chipSeg{textFriend, func(t *theme.Theme) string { return t.S.Base.Foreground(t.P.Acc3).Render(textFriend) }, ""})
	}
	if r.FollowedBy == "active" {
		out = append(out, chipSeg{textFollowsYou, dim(textFollowsYou), ""})
	}
	return out
}

// chipLine draws segs (then tail) in exactly w cells, dropping segments that do
// not fit, and returns the drawn clickable spans relative to the line.
func (p *profile) chipLine(ctx *ui.Context, segs []chipSeg, tail string, w int) (string, []chipSpan) {
	t := themeOf(ctx)
	var b strings.Builder
	var spans []chipSpan
	x := 0
	for i, s := range segs {
		sw := textwidth.Width(s.text)
		gap := 0
		if i > 0 {
			gap = 2
		}
		if x+gap+sw > w {
			break
		}
		b.WriteString(t.S.Base.Render(strings.Repeat(" ", gap)))
		x += gap
		b.WriteString(s.st(t))
		if s.act != "" {
			spans = append(spans, chipSpan{x0: x, x1: x + sw, act: s.act})
		}
		x += sw
	}
	if tail != "" {
		gap := 0
		if x > 0 {
			gap = 2
		}
		if x+gap < w {
			b.WriteString(t.S.Base.Render(strings.Repeat(" ", gap)))
			b.WriteString(tail)
		}
	}
	return fit(t, b.String(), w), spans
}

// header draws the profile header for width w; it records the chip line row
// (relative to the header's first row) and its spans.
func (p *profile) header(ctx *ui.Context, w int) (lines []string, chipRow int, spans []chipSpan) {
	t := themeOf(ctx)
	if p.fullHeader(ctx) {
		return p.headerFull(ctx, w)
	}
	cw := max(w-gutterW, 0)
	g := t.S.Base.Render(strings.Repeat(" ", min(gutterW, w)))
	add := func(s string) { lines = append(lines, fit(t, g+s, w)) }
	add(p.nameLine(ctx, cw, false))
	if ctx.Density() != ui.DensityMinimal {
		if s, ok := p.statusLine(ctx, cw); ok {
			add(s)
		} else if bio := p.bioLines(ctx, cw, 1); len(bio) > 0 {
			add(bio[0])
		}
	}
	line, sp := p.chipLine(ctx, p.chipSegs(ctx), p.countsText(t), cw)
	chipRow = len(lines)
	add(line)
	for i := range sp {
		sp[i].x0 += gutterW
		sp[i].x1 += gutterW
	}
	return lines, chipRow, sp
}

// headerFull is the avatar box on the left, the name / status / bio / counts /
// chips column on the right, then a blank line.
func (p *profile) headerFull(ctx *ui.Context, w int) (lines []string, chipRow int, spans []chipSpan) {
	t := themeOf(ctx)
	left := gutterW + avatarCols + 2
	rw := w - left
	var right []string
	right = append(right, p.nameLine(ctx, rw, true))
	if s, ok := p.statusLine(ctx, rw); ok {
		right = append(right, s)
	}
	right = append(right, p.bioLines(ctx, rw, bioMaxLines)...)
	right = append(right, fit(t, p.countsText(t), rw))
	chipRow = len(right)
	line, sp := p.chipLine(ctx, p.chipSegs(ctx), "", rw)
	right = append(right, line)
	for i := range sp {
		sp[i].x0 += left
		sp[i].x1 += left
	}
	av := p.avatarLines(ctx)
	n := max(len(right), avatarRows)
	pad := t.S.Base.Render(strings.Repeat(" ", gutterW))
	gap := t.S.Base.Render("  ")
	for i := range n {
		a := t.S.Base.Render(strings.Repeat(" ", avatarCols))
		if i < len(av) {
			a = fit(t, av[i], avatarCols)
		}
		r := fit(t, "", rw)
		if i < len(right) {
			r = right[i]
		}
		lines = append(lines, fit(t, pad+a+gap+r, w))
	}
	lines = append(lines, fit(t, "", w))
	return lines, chipRow, sp
}

// avatarLines is the loaded avatar image (8x4 cells), else the text avatar
// (the initial on a chip-colored block).
func (p *profile) avatarLines(ctx *ui.Context) []string {
	t := themeOf(ctx)
	if m, ok := p.avatarMedia(); ok {
		if ls := p.imgs.cell(ctx, m, avatarCols, avatarRows); len(ls) > 0 {
			return ls
		}
	}
	chip := t.S.Base.Background(t.P.Chip).Render(strings.Repeat(" ", avatarCols))
	out := make([]string, avatarRows)
	for i := range out {
		out[i] = chip
	}
	out[1] = widgets.Avatar(t, p.prof.UserSummary, avatarCols)
	return out
}

func (p *profile) loadingLine(ctx *ui.Context, w int) string {
	t := themeOf(ctx)
	return fit(t, t.S.Base.Render("  ")+p.spin.View(t)+t.S.Dim.Render(" "+textLoading), w)
}

// lockedState is the empty state drawn instead of the tabs, if any.
func (p *profile) lockedState(ctx *ui.Context) (icon, title, hint string, locked bool) {
	switch {
	case p.blocked(ctx):
		return "⊘", textBlockedTitle, hintBlocked, true
	case p.prof.Suspended:
		return "⊘", textSuspendedTitle, "", true
	case !p.canView(ctx) && p.prof.IsPrivate:
		return "🔒", textPrivateTitle, hintPrivate, true
	case !p.canView(ctx):
		return "⊘", textCannotView, "", true
	}
	return "", "", "", false
}

func (p *profile) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := p.size.W, p.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	p.tabY, p.chipY, p.listY, p.listH = -1, -1, -1, 0
	p.chips, p.visible = nil, nil
	switch {
	case p.prof == nil && p.notFound:
		return widgets.Empty(t, p.size, "⚠", textUserNotFound, hintGoBack)
	case p.prof == nil && p.loadFailed:
		return widgets.Empty(t, p.size, "⚠", textProfileLoadFailed, hintGoBack)
	case p.prof == nil:
		return blankRows(t, []string{p.loadingLine(ctx, w)}, w, h)
	}
	out, chipRow, spans := p.header(ctx, w)
	if chipRow < h {
		p.chipY, p.chips = chipRow, spans
	}
	if icon, title, hint, locked := p.lockedState(ctx); locked {
		if rest := h - len(out); rest > 0 {
			out = append(out, strings.Split(widgets.Empty(t, ui.Size{W: w, H: rest}, icon, title, hint), "\n")...)
		}
		return blankRows(t, out, w, h)
	}
	if len(out) < h {
		items := make([]widgets.Tab, len(profileTabs))
		for i, pt := range profileTabs {
			items[i] = widgets.Tab{Label: pt.label}
		}
		p.tabs.Set(items, int(p.tab))
		p.tabY = len(out)
		out = append(out, p.tabs.View(t, w))
	}
	rest := h - len(out)
	if rest <= 0 {
		return blankRows(t, out, w, h)
	}
	l := p.activeList()
	switch {
	case l == nil:
		out = append(out, p.guestbookView(ctx, w, rest, len(out))...)
	case len(l.items) == 0 && (l.loading || !l.loaded):
		out = append(out, p.loadingLine(ctx, w))
	case len(l.items) == 0 && l.err != nil:
		out = append(out, strings.Split(widgets.Empty(t, ui.Size{W: w, H: rest}, "⚠", textwidth.Sanitize(api.Describe(l.err)), ""), "\n")...)
	case len(l.items) == 0:
		text := textNoProfilePosts
		switch l.filter {
		case "replies":
			text = textNoProfileReplies
		case "media":
			text = textNoProfileMedia
		}
		out = append(out, strings.Split(widgets.Empty(t, ui.Size{W: w, H: rest}, "🌱", text, ""), "\n")...)
	default:
		footer := 0
		if l.loadingMore {
			footer = 1
		}
		p.listY, p.listH = len(out), max(rest-footer, 0)
		if p.listH > 0 {
			out = append(out, strings.Split(l.list.View(ctx, ui.Size{W: w, H: p.listH}), "\n")...)
			p.visible = p.visibleTopics(ctx, l)
		}
		if footer > 0 {
			out = append(out, p.loadingLine(ctx, w))
		}
	}
	return blankRows(t, out, w, h)
}

// visibleTopics is "post:<id>" of every card on screen.
func (p *profile) visibleTopics(ctx *ui.Context, l *profileList) []string {
	a := l.list.Anchor()
	views := p.viewsFn(ctx, l)
	start := 0
	for i, x := range l.items {
		if x.ID == a.ItemID {
			start = i
			break
		}
	}
	seen := map[string]bool{}
	var out []string
	for i, top := start, -a.LineOffset; i < len(l.items) && top < p.listH; i++ {
		if k := "post:" + l.items[i].Post.ID; !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
		top += max(len(views(l.items[i]).Lines), 1)
	}
	return out
}

func (p *profile) Resize(_ *ui.Context, size ui.Size) { p.size = size }

// ---- input ----

// toggleFollow is "+": follow (none), cancel the request (pending) or, after
// the confirm, unfollow (active). Nothing on my profile, a blocked or a
// suspended user, or while a request is in flight.
func (p *profile) toggleFollow(ctx *ui.Context) tea.Cmd {
	r := p.rel()
	if p.prof == nil || r == nil || p.isMe(ctx) || p.blocked(ctx) || p.prof.Suspended || p.following {
		return nil
	}
	id := p.prof.ID
	unfollow := func() tea.Cmd {
		o := &followOutcome{userID: id, unfollow: true}
		return ui.Do(ctx, tagUnfollow+id, func(c context.Context, a *api.Client) (*followOutcome, error) {
			if a == nil {
				return o, errNoClient
			}
			return o, a.Unfollow(c, api.UserID(id))
		})
	}
	switch r.Following {
	case "active":
		return ui.OpenOverlay(widgets.Confirm(textUnfollowAsk, "", textUnfollowYes, textCancel, unfollow()))
	case "pending":
		p.following = true
		return unfollow()
	}
	p.following = true
	o := &followOutcome{userID: id}
	return ui.Do(ctx, tagFollow+id, func(c context.Context, a *api.Client) (*followOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		state, err := a.Follow(c, api.UserID(id))
		o.state = state
		return o, err
	})
}

// openStories is "s" / 스토리 보기 (ignored without a request when the
// stories cannot be viewed).
func (p *profile) openStories(ctx *ui.Context) tea.Cmd {
	if !p.canStories(ctx) {
		return nil
	}
	return ui.Navigate(p.storiesRoute())
}

// openEdit is "e" on my profile.
func (p *profile) openEdit() tea.Cmd { return ui.OpenRegisteredOverlay(overlayProfileEdit, "") }

// openMenu is "m": 스토리 보기, 팔로워, 팔로잉, then 프로필 편집 · 팔로우 요청
// (mine) or 차단 · 사용자 신고 (someone else's).
func (p *profile) openMenu(ctx *ui.Context) tea.Cmd {
	if p.prof == nil {
		return nil
	}
	id, handle, name := p.prof.ID, handleOf(p.prof.UserSummary), displayName(p.prof.UserSummary)
	var items []menuItem
	if p.canStories(ctx) {
		r := p.storiesRoute()
		items = append(items, menuItem{label: textMenuStories, run: func(*ui.Context) tea.Cmd { return ui.Navigate(r) }})
	}
	if p.canView(ctx) {
		items = append(items,
			menuItem{label: textFollowersTitle, open: func(ctx *ui.Context) ui.Overlay { return overlayOrNil(newUserList(ctx, listFollowers+" "+id)) }},
			menuItem{label: textFollowingTitle, open: func(ctx *ui.Context) ui.Overlay { return overlayOrNil(newUserList(ctx, listFollowing+" "+id)) }},
		)
	}
	if p.isMe(ctx) {
		items = append(items,
			menuItem{label: textMenuEditProfile, open: func(ctx *ui.Context) ui.Overlay { return overlayOrNil(newProfileEdit(ctx)) }},
			menuItem{label: textMenuFollowReqs, open: func(*ui.Context) ui.Overlay { return newFollowRequests() }},
		)
	} else {
		if !p.blocked(ctx) {
			items = append(items, menuItem{label: textMenuBlockUser, open: func(ctx *ui.Context) ui.Overlay {
				return widgets.Confirm(name+textBlockAsk, textBlockBody, textBlockYes, textCancel, blockUser(ctx, id))
			}})
		}
		items = append(items, menuItem{label: textMenuReportUser, open: func(*ui.Context) ui.Overlay {
			return widgets.ReportDialog("user", id, handle)
		}})
	}
	return ui.OpenOverlay(&menu{title: handle, width: menuW, items: items})
}

// overlayOrNil turns a nil overlay pointer into a nil ui.Overlay.
func overlayOrNil[T any, P interface {
	*T
	ui.Overlay
}](o P) ui.Overlay {
	if o == nil {
		return nil
	}
	return o
}

func (p *profile) maybeMore(ctx *ui.Context, l *profileList) tea.Cmd {
	if l.cursor == nil || len(l.items) == 0 {
		return nil
	}
	sel, ok := l.list.Selected()
	if !ok {
		return nil
	}
	for i, it := range l.items {
		if it.ID == sel.ID && i >= len(l.items)-moreThreshold {
			return p.loadList(ctx, l, *l.cursor)
		}
	}
	return nil
}

func (p *profile) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.prof == nil {
		return false, nil
	}
	km := ctx.Keys
	if p.onGuestbook(ctx) {
		if handled, cmd := p.guestbookActionKey(ctx, msg); handled {
			return true, cmd
		}
	}
	switch s := msg.String(); {
	case s == "+":
		return true, p.toggleFollow(ctx)
	case km.Matches(msg, keymap.MsgActions):
		return true, p.openMenu(ctx)
	case s == "e" && p.isMe(ctx):
		return true, p.openEdit()
	case s == "s":
		return true, p.openStories(ctx)
	case len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(profileTabs):
		return true, p.setTab(ctx, profileTab(s[0]-'1'))
	}
	if p.onGuestbook(ctx) {
		return p.guestbookListKey(ctx, msg)
	}
	l := p.activeList()
	if l == nil || !p.canView(ctx) {
		return false, nil
	}
	if km.Matches(msg, keymap.ListOpen) {
		if it, ok := l.list.Selected(); ok {
			return true, ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: it.Post.ID})
		}
		return true, nil
	}
	if isPostKey(ctx, msg) {
		if it, ok := l.list.Selected(); ok {
			if handled, cmd := handlePostKey(ctx, msg, p.post(ctx, it)); handled {
				l.redraw() // optimistic like / repost changes show at once
				return true, cmd
			}
		}
	}
	handled, cmd := l.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, p.maybeMore(ctx, l), p.requestImages(ctx))
}

func (p *profile) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if p.prof == nil {
		return nil
	}
	if p.onGuestbook(ctx) {
		if cmd, ok := p.guestbookMouse(ctx, msg, local); ok {
			return cmd
		}
	}
	l := p.activeList()
	inList := l != nil && p.listY >= 0 && local.Y >= p.listY && local.Y < p.listY+p.listH
	switch msg.(type) {
	case tea.MouseWheelMsg:
		if inList {
			l.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - p.listY})
			return tea.Batch(p.maybeMore(ctx, l), p.requestImages(ctx))
		}
		return nil
	case tea.MouseClickMsg:
	default:
		return nil
	}
	if msg.Mouse().Button != tea.MouseLeft {
		return nil
	}
	switch y := local.Y; {
	case y == p.tabY:
		if i, ok := p.tabs.Hit(local.X); ok {
			return p.setTab(ctx, profileTab(i))
		}
	case y == p.chipY:
		for _, c := range p.chips {
			if local.X >= c.x0 && local.X < c.x1 {
				switch c.act {
				case chipActFollow:
					return p.toggleFollow(ctx)
				case chipActEdit:
					return p.openEdit()
				case chipActRequests:
					return ui.OpenRegisteredOverlay(overlayFollowRequests, "")
				}
			}
		}
	case inList:
		ly := y - p.listY
		views := p.viewsFn(ctx, l)
		if it, line, ok := cardAt(l.list, l.items, views, ly); ok {
			for _, hit := range views(it).Hits {
				if hit.Line == line && local.X >= hit.X0 && local.X < hit.X1 {
					return handlePostHit(ctx, hit, p.post(ctx, it))
				}
			}
			// a click on the selected card opens it, like enter (CA 5.5)
			if sel, ok := l.list.Selected(); ok && sel.ID == it.ID {
				return ui.Navigate(ui.Route{Kind: ui.RoutePost, ID: it.Post.ID})
			}
		}
		l.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: ly})
		return tea.Batch(p.maybeMore(ctx, l), p.requestImages(ctx))
	}
	return nil
}

// ---- optional capabilities ----

// Title is "@handle" (the route's handle, or 프로필, until the profile loads).
func (p *profile) Title(*ui.Context) (string, ui.Badge) {
	switch {
	case p.prof != nil:
		return handleOf(p.prof.UserSummary), ui.Badge{}
	case strings.HasPrefix(p.id, "@") && p.id != string(api.MeRef):
		return "@" + textwidth.Sanitize(strings.TrimPrefix(p.id, "@")), ui.Badge{}
	}
	return textProfileTitle, ui.Badge{}
}

func (p *profile) Help(ctx *ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	out := []key.Binding{}
	if p.prof != nil && p.isMe(ctx) {
		out = append(out, b("e", "프로필 편집"))
	} else {
		out = append(out, b("+", "팔로우"))
	}
	if p.tab == tabGuestbook {
		out = append(out, b("w", "방명록 쓰기"))
	}
	return append(out, b("m", "메뉴"), b("1-4", "탭"), b("s", "스토리"))
}

func (p *profile) WatchTopics() []string { return append([]string(nil), p.visible...) }
