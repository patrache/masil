package feed

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
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
	ui.RegisterRoute(ui.RouteTrends, newTrends)
}

// Trends tuning.
const (
	trendsRefresh  = 5 * time.Minute             // Top 10 refresh period
	trendsVisible  = trendsRefresh + time.Second // a refresh runs only if View ran this recently
	searchDebounce = 300 * time.Millisecond      // like the palette people search
	searchLimit    = 20                          // hashtag / post search page size
	searchMaxRunes = 100                         // api.md 0.3 search query
	tagMaxRunes    = 50                          // GET /v1/hashtags/search q
	trendTagMaxW   = 16                          // tag column cap (cells, "#" included)
	searchPrefix   = "  " + iconSearch + " "     // gutter + icon before the input
	searchPrefixW  = 2 + 2 + 1                   // its width in cells
	openRowPrefix  = "open:"                     // row key of the "#<q> 태그 피드 열기" row
)

// Trends copy.
const (
	textTrendsTitle  = "트렌드"
	iconTrendsEmpty  = "📈"
	textTrendsEmpty  = "아직 트렌드가 없어요"
	hintTrendsEmpty  = "글에 #태그를 달아 보세요"
	iconSearch       = "🔍"
	textSearchHint   = "#태그 또는 글 검색"
	textSearchEmpty  = "검색 결과가 없어요"
	hintSearchEmpty  = "#태그 로 시작하면 태그를 찾아요"
	textTagTooLong   = "태그는 50자까지예요"
	textQueryTooLong = "검색어는 100자까지예요"
	textOpenTagFeed  = " 태그 피드 열기"
	textComputedAt   = " 기준"
	errContextSearch = "검색"
	errContextTrends = "트렌드"
)

// Trends result tags (ui.Do).
const (
	tagTrends          = "feed.trends"
	tagSearchTags      = "feed.search_tags:"       // + tag query (no '#')
	tagSearchPosts     = "feed.search_posts:"      // + query: first page
	tagSearchPostsMore = "feed.search_posts_more:" // + query: next page
)

// trendsTickMsg is the Top 10 refresh timer, current while Gen matches.
type trendsTickMsg struct{ Gen int }

// searchDebounceMsg runs the search for Q, current while Gen matches.
type searchDebounceMsg struct {
	Gen int
	Q   string
}

// trendsGens numbers timer generations across instances, so a released
// instance's timers never match a new one.
var trendsGens atomic.Int64

func nextTrendsGen() int { return int(trendsGens.Add(1)) }

// searchKind is what the search line's query asks for.
type searchKind int

const (
	kindTop   searchKind = iota // no query (or a lone "#"): the Top 10
	kindTags                    // "#..." : hashtag search
	kindPosts                   // anything else: post search
)

// classify splits a normalized query into its kind and argument (the tag
// without '#', or the query itself).
func classify(q string) (searchKind, string) {
	switch {
	case q == "" || q == "#":
		return kindTop, ""
	case strings.HasPrefix(q, "#"):
		return kindTags, q[1:]
	}
	return kindPosts, q
}

// normQuery is the input value as a query: SanitizeInput (one line), trimmed.
func normQuery(s string) string { return strings.TrimSpace(oneLine(s)) }

// trendRow is one row of the Top 10 or of the hashtag results.
type trendRow struct {
	key  string // tag, or "open:<tag>"
	tag  string
	rank int // Top 10 rank (0 = a search row)
	meta string
	open bool // the "#<q> 태그 피드 열기" row
}

// trendsState is the Persistent state: the search line, its query and the
// selected row / result.
type trendsState struct {
	Open       bool
	Query      string
	InputFocus bool
	Selected   string
}

// trends is RouteTrends: the feed tabs, the Top 10 hashtags (refreshed every
// 5 minutes while shown) and, behind "/", a search line for hashtags ("#…")
// and posts.
type trends struct {
	spin *widgets.Spinner
	imgs imageSlots
	size ui.Size

	loading    bool // GET /v1/trends in flight
	fetchedAt  time.Time
	tickGen    int
	ticking    bool
	lastViewAt time.Time

	top     *widgets.List[trendRow]
	topRows []trendRow
	topTagW int

	searchOpen bool
	inputFocus bool
	input      textinput.Model
	query      string // normalized input value
	gen        int    // search debounce generation
	note       string // replaces the results (query too long)

	tagQ    string // the tag query tagRes answers ("" = none yet)
	tagRes  []api.HashtagSuggestion
	tagList *widgets.List[trendRow]
	tagRows []trendRow
	tagTagW int

	postQ           string // the query posts answers ("" = none yet)
	posts           *cardList
	postCursor      *string
	postLoadingMore bool
	postErr         error

	pending *trendsState

	// geometry of the last View (pane-local rows; -1 = not drawn)
	tabY, searchY, listY, listH int
	inputCol                    int
	tabHit                      func(x int) (ui.Route, bool)
}

var (
	_ ui.Component     = (*trends)(nil)
	_ ui.KeyHandler    = (*trends)(nil)
	_ ui.MouseHandler  = (*trends)(nil)
	_ ui.Titled        = (*trends)(nil)
	_ ui.Helper        = (*trends)(nil)
	_ ui.Watcher       = (*trends)(nil)
	_ ui.Persistent    = (*trends)(nil)
	_ ui.InputCapturer = (*trends)(nil)
	_ ui.Cursorer      = (*trends)(nil)
	_ ui.Completer     = (*trends)(nil)
)

// newTrends builds RouteTrends.
func newTrends(_ *ui.Context, _ ui.Route) ui.Component {
	t := &trends{
		spin:  widgets.NewSpinner(),
		input: newPathInput(),
		tabY:  -1, searchY: -1, listY: -1,
	}
	t.input.Blur()
	t.top = widgets.NewList(func(r trendRow) string { return r.key }, t.renderRow(&t.topTagW))
	t.tagList = widgets.NewList(func(r trendRow) string { return r.key }, t.renderRow(&t.tagTagW))
	t.posts = newCardList(&t.imgs)
	return t
}

func (t *trends) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init fetches the Top 10, arms the refresh timer and restores a saved search.
func (t *trends) Init(ctx *ui.Context) tea.Cmd {
	cmds := []tea.Cmd{t.loadTrends(ctx), t.armTick(ctx)}
	if p := t.pending; p != nil && p.Open {
		t.searchOpen = true
		t.input.SetValue(oneLine(p.Query))
		t.input.CursorEnd()
		t.setFocus(p.InputFocus)
		t.query = normQuery(t.input.Value())
		t.gen = nextTrendsGen()
		cmds = append(cmds, t.search(ctx))
	}
	t.syncLists(ctx, true)
	return tea.Batch(cmds...)
}

func (t *trends) loadTrends(ctx *ui.Context) tea.Cmd {
	if t.loading {
		return nil
	}
	t.loading = true
	return tea.Batch(t.spin.Start(ctx), ui.Do(ctx, tagTrends, func(c context.Context, a *api.Client) (*api.Trends, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.Trends(c)
	}))
}

// ---- refresh timer (the timeline's lastViewAt rule) ----

func (t *trends) armTick(ctx *ui.Context) tea.Cmd {
	t.tickGen = nextTrendsGen()
	t.ticking = true
	return ui.After(ctx, trendsRefresh, trendsTickMsg{Gen: t.tickGen})
}

func (t *trends) shownRecently(ctx *ui.Context) bool {
	return !t.lastViewAt.IsZero() && ctx.Now().Sub(t.lastViewAt) <= trendsVisible
}

func (t *trends) onTick(ctx *ui.Context, m trendsTickMsg) tea.Cmd {
	if m.Gen != t.tickGen {
		return nil
	}
	t.ticking = false
	if !t.shownRecently(ctx) {
		return nil // stopped; rearm restarts it when the screen is shown again
	}
	return tea.Batch(t.loadTrends(ctx), t.armTick(ctx))
}

// rearm restarts a stopped refresh once the screen is shown again (fetching at
// once when the Top 10 is older than the period).
func (t *trends) rearm(ctx *ui.Context) tea.Cmd {
	if t.ticking || !t.shownRecently(ctx) {
		return nil
	}
	var load tea.Cmd
	if !t.fetchedAt.IsZero() && ctx.Now().Sub(t.fetchedAt) >= trendsRefresh {
		load = t.loadTrends(ctx)
	}
	return tea.Batch(load, t.armTick(ctx))
}

// ---- search ----

// setFocus moves the focus to the input (true) or the list below (false).
func (t *trends) setFocus(input bool) {
	t.inputFocus = input
	if input {
		t.input.Focus()
	} else {
		t.input.Blur()
	}
	t.posts.setNoSel(t.searchOpen && input)
	t.redrawRows()
}

func (t *trends) openSearch() {
	t.searchOpen = true
	t.setFocus(true)
}

// closeSearch closes the search line and drops its results (back to the Top 10).
func (t *trends) closeSearch() {
	t.searchOpen = false
	t.input.Reset()
	t.query, t.note = "", ""
	t.gen = nextTrendsGen()
	t.tagQ, t.tagRes, t.tagRows = "", nil, nil
	t.tagList.SetItems(nil)
	t.postQ, t.postCursor, t.postErr, t.postLoadingMore = "", nil, nil, false
	t.posts.clear()
	t.setFocus(false)
}

// onInputChanged re-reads the query; a changed one bumps the generation and
// arms the debounce (nothing for an empty or too long query).
func (t *trends) onInputChanged(ctx *ui.Context) tea.Cmd {
	q := normQuery(t.input.Value())
	if q == t.query {
		return nil
	}
	t.query, t.note = q, ""
	t.gen = nextTrendsGen()
	kind, arg := classify(q)
	switch {
	case kind == kindTop:
		return nil
	case kind == kindTags && utf8.RuneCountInString(arg) > tagMaxRunes:
		t.note = textTagTooLong
		return nil
	case kind == kindPosts && utf8.RuneCountInString(q) > searchMaxRunes:
		t.note = textQueryTooLong
		return nil
	}
	return ui.After(ctx, searchDebounce, searchDebounceMsg{Gen: t.gen, Q: q})
}

// search requests the results of the current query.
func (t *trends) search(ctx *ui.Context) tea.Cmd {
	q := t.query
	kind, arg := classify(q)
	switch kind {
	case kindTags:
		if utf8.RuneCountInString(arg) > tagMaxRunes {
			return nil
		}
		return ui.Do(ctx, tagSearchTags+arg, func(c context.Context, a *api.Client) ([]api.HashtagSuggestion, error) {
			if a == nil {
				return nil, errNoClient
			}
			return a.SearchHashtags(c, arg, searchLimit)
		})
	case kindPosts:
		if utf8.RuneCountInString(q) > searchMaxRunes {
			return nil
		}
		t.postLoadingMore = false
		return t.searchPosts(ctx, q, "")
	}
	return nil
}

func (t *trends) searchPosts(ctx *ui.Context, q, cursor string) tea.Cmd {
	tag := tagSearchPosts + q
	if cursor != "" {
		tag = tagSearchPostsMore + q
		t.postLoadingMore = true
	}
	return tea.Batch(t.spin.Start(ctx), ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.PostSearchHit], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.SearchPosts(c, api.PostSearchQuery{Q: q, PageOpts: api.PageOpts{Cursor: cursor, Limit: searchLimit}})
	}))
}

func (t *trends) maybeMorePosts(ctx *ui.Context) tea.Cmd {
	kind, _ := classify(t.query)
	if kind != kindPosts || t.postQ != t.query || t.postCursor == nil || t.postLoadingMore || !t.posts.nearEnd() {
		return nil
	}
	return t.searchPosts(ctx, t.query, *t.postCursor)
}

func searchErr(err error) tea.Cmd {
	return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: errContextSearch} }
}

// onTags applies a hashtag search answer of the current query.
func (t *trends) onTags(m ui.ResultMsg[[]api.HashtagSuggestion]) tea.Cmd {
	kind, arg := classify(t.query)
	if kind != kindTags || m.Tag != tagSearchTags+arg {
		return nil // stale or not ours
	}
	t.tagQ = arg
	if m.Err != nil {
		t.tagRes = nil
		return searchErr(m.Err)
	}
	t.tagRes = m.Value
	return nil
}

// onPosts applies a post search page of the current query.
func (t *trends) onPosts(ctx *ui.Context, m ui.ResultMsg[*api.Page[api.PostSearchHit]]) tea.Cmd {
	more := false
	switch m.Tag {
	case tagSearchPosts + t.query:
	case tagSearchPostsMore + t.query:
		if !t.postLoadingMore {
			return nil
		}
		more = true
		t.postLoadingMore = false
	default:
		return nil
	}
	if kind, _ := classify(t.query); kind != kindPosts {
		return nil
	}
	t.spin.Stop()
	if m.Err != nil || m.Value == nil {
		if !more {
			t.postQ, t.postErr, t.postCursor = t.query, m.Err, nil
			t.posts.clear()
		}
		return searchErr(m.Err)
	}
	ps := make([]api.Post, 0, len(m.Value.Items))
	for _, h := range m.Value.Items {
		ps = append(ps, h.Post)
	}
	t.postQ, t.postErr = t.query, nil
	t.posts.setPosts(ctx, ps, more)
	t.postCursor = m.Value.NextCursor
	if len(ps) == 0 {
		t.postCursor = nil // an empty page never loops
	}
	return nil
}

// ---- Update ----

func (t *trends) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	force := true
	imagesChanged := false
	switch m := msg.(type) {
	case trendsTickMsg:
		force = false
		cmds = append(cmds, t.onTick(ctx, m))
	case searchDebounceMsg:
		force = false
		if m.Gen == t.gen && m.Q == t.query {
			cmds = append(cmds, t.search(ctx))
		}
	case ui.ResultMsg[*api.Trends]:
		if m.Tag == tagTrends {
			t.loading = false
			if !t.postLoadingMore {
				t.spin.Stop()
			}
			fs := t.fs(ctx)
			switch {
			case m.Err == nil && m.Value != nil && fs != nil:
				fs.SetTrends(m.Value)
				t.fetchedAt = ctx.Now()
			case m.Err != nil && (fs == nil || fs.Trends() == nil):
				cmds = append(cmds, func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextTrends} })
			}
		}
	case ui.ResultMsg[[]api.HashtagSuggestion]:
		cmds = append(cmds, t.onTags(m))
	case ui.ResultMsg[*api.Page[api.PostSearchHit]]:
		cmds = append(cmds, t.onPosts(ctx, m))
	case ui.ModeChangedMsg, ui.ThemeChangedMsg:
		imagesChanged = true
	default:
		if rt := reflect.TypeOf(msg); rt != nil && rt.PkgPath() == widgetsPkg {
			force = false // spinner ticks
		}
	}
	if id, _, ok := t.imgs.loaded(msg); ok {
		cmds = append(cmds, t.posts.renderMedia(ctx, id, t.size.W))
	}
	cmds = append(cmds, t.spin.Update(ctx, msg), t.rearm(ctx))
	if t.imgs.hasWants() {
		cmds = append(cmds, t.imgs.flush(ctx))
		force = true
	}
	t.syncLists(ctx, force)
	cmds = append(cmds, t.posts.requestImages(ctx))
	if imagesChanged {
		cmds = append(cmds, t.posts.rerenderImages(ctx, t.size.W))
	}
	return tea.Batch(cmds...)
}

// syncLists re-reads the Top 10, the hashtag rows and the post results into
// their Lists, then applies a restored selection once its list has items.
func (t *trends) syncLists(ctx *ui.Context, force bool) {
	var rows []trendRow
	if fs := t.fs(ctx); fs != nil && fs.Trends() != nil {
		for _, tr := range fs.Trends().Items {
			rows = append(rows, trendRow{
				key: tr.Tag, tag: tr.Tag, rank: tr.Rank,
				meta: humanize.Count(tr.PostCount) + "개 글 · " + humanize.Count(tr.AuthorCount) + "명",
			})
		}
	}
	if force || !sameRowKeys(rows, t.topRows) {
		t.topRows, t.topTagW = rows, tagColumn(rows)
		t.top.SetItems(rows)
	}

	rows = nil
	if t.tagQ != "" {
		exact := false
		for _, s := range t.tagRes {
			if strings.EqualFold(s.Tag, t.tagQ) {
				exact = true
			}
			rows = append(rows, trendRow{key: s.Tag, tag: s.Tag, meta: "24시간 글 " + humanize.Count(s.PostCount24h) + "개"})
		}
		if !exact {
			rows = append(rows, trendRow{key: openRowPrefix + t.tagQ, tag: t.tagQ, open: true})
		}
	}
	if force || !sameRowKeys(rows, t.tagRows) {
		t.tagRows, t.tagTagW = rows, tagColumn(rows)
		t.tagList.SetItems(rows)
	}

	t.posts.sync(ctx, force)
	t.applyPending()
}

// applyPending selects the restored row / result once the shown list has it.
func (t *trends) applyPending() {
	p := t.pending
	if p == nil {
		return
	}
	if !p.Open || p.Selected == "" {
		t.pending = nil
		return
	}
	switch kind, _ := t.shownKind(); kind {
	case kindTop:
		if len(t.topRows) > 0 {
			t.top.Select(p.Selected)
			t.pending = nil
		}
	case kindTags:
		if len(t.tagRows) > 0 {
			t.tagList.Select(p.Selected)
			t.pending = nil
		}
	case kindPosts:
		if t.postQ == t.query && t.postQ != "" {
			t.posts.list.Select(p.Selected)
			t.pending = nil
		}
	}
}

func sameRowKeys(a, b []trendRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].key != b[i].key || a[i].meta != b[i].meta {
			return false
		}
	}
	return true
}

// tagColumn is the tag column width of rows ("#tag", capped).
func tagColumn(rows []trendRow) int {
	w := 0
	for _, r := range rows {
		if !r.open {
			w = max(w, textwidth.Width("#"+textwidth.Sanitize(r.tag)))
		}
	}
	return min(w, trendTagMaxW)
}

// redrawRows drops the row Lists' render caches (selection drawing follows the focus).
func (t *trends) redrawRows() {
	t.top.SetItems(t.topRows)
	t.tagList.SetItems(t.tagRows)
}

// ---- rendering ----

// shownKind is what the list area shows: the Top 10 unless the search line
// holds a query.
func (t *trends) shownKind() (searchKind, string) {
	if !t.searchOpen {
		return kindTop, ""
	}
	return classify(t.query)
}

// rowsActive reports whether the row lists draw their selection.
func (t *trends) rowsActive() bool { return !t.searchOpen || !t.inputFocus }

// renderRow is a row List's render callback; tagW points at that list's tag column.
func (t *trends) renderRow(tagW *int) func(*ui.Context, trendRow, bool, int) []string {
	return func(ctx *ui.Context, r trendRow, selected bool, width int) []string {
		th := themeOf(ctx)
		return []string{selectedLine(th, rowText(th, r, *tagW), selected && t.rowsActive(), width)}
	}
}

// rowText is "1  #첫눈   42개 글 · 30명" (top 3 ranks acc bold), a hashtag
// result "#첫눈   24시간 글 42개" or the open row "#첫 태그 피드 열기".
func rowText(th *theme.Theme, r trendRow, tagW int) string {
	tag := "#" + textwidth.Sanitize(r.tag)
	if r.open {
		return th.S.Base.Foreground(th.P.Acc).Render(tag) + th.S.Dim.Render(textOpenTagFeed)
	}
	var b strings.Builder
	if r.rank > 0 {
		st := th.S.Dim
		if r.rank <= 3 {
			st = th.S.Base.Foreground(th.P.Acc).Bold(true)
		}
		b.WriteString(st.Render(textwidth.PadRight(strconv.Itoa(r.rank), 2)) + th.S.Base.Render(" "))
	}
	if textwidth.Width(tag) > tagW {
		tag = textwidth.Truncate(tag, tagW, "…")
	}
	b.WriteString(th.S.Bold.Render(tag) + th.S.Base.Render(strings.Repeat(" ", max(tagW-textwidth.Width(tag), 0)+3)))
	b.WriteString(th.S.Dim.Render(textwidth.Sanitize(r.meta)))
	return b.String()
}

func (t *trends) statusLine(ctx *ui.Context, w int) string {
	th := themeOf(ctx)
	return fit(th, th.S.Base.Render("  ")+t.spin.View(th)+th.S.Dim.Render(" "+textLoading), w)
}

func noteLine(th *theme.Theme, text string, w int) string {
	return fit(th, th.S.Base.Render("  ")+th.S.Dim.Render(text), w)
}

// searchLine is "  🔍 " + the input (hint while empty); it records the cursor column.
func (t *trends) searchLine(ctx *ui.Context, w int) string {
	th := themeOf(ctx)
	if w <= searchPrefixW {
		t.inputCol = min(searchPrefixW, max(w-1, 0))
		return fit(th, th.S.Base.Render(searchPrefix), w)
	}
	line, col := pathLine(th, t.input, textSearchHint, w-searchPrefixW)
	t.inputCol = searchPrefixW + col
	return fit(th, th.S.Base.Render(searchPrefix)+line, w)
}

func (t *trends) View(ctx *ui.Context, _ bool) string {
	t.lastViewAt = ctx.Now()
	th := themeOf(ctx)
	w, h := t.size.W, t.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t.tabY, t.searchY, t.listY, t.listH, t.tabHit = -1, -1, -1, 0, nil
	t.posts.visible = nil
	var out []string
	if d := ctx.Density(); d != ui.DensityLine && d != ui.DensityMinimal {
		line, hit := feedTabs(ctx, tabTrends, w)
		t.tabY, t.tabHit = len(out), hit
		out = append(out, line)
	}
	if t.searchOpen && len(out) < h {
		t.searchY = len(out)
		out = append(out, t.searchLine(ctx, w))
	}
	rest := h - len(out)
	if rest <= 0 {
		return blankRows(th, out, w, h)
	}
	empty := func(icon, title, hint string) {
		out = append(out, strings.Split(widgets.Empty(th, ui.Size{W: w, H: rest}, icon, title, hint), "\n")...)
	}
	list := func(l *widgets.List[trendRow], n int) {
		t.listY, t.listH = len(out), n
		out = append(out, strings.Split(l.View(ctx, ui.Size{W: w, H: n}), "\n")...)
	}
	kind, arg := t.shownKind()
	switch {
	case t.note != "":
		out = append(out, noteLine(th, t.note, w))
	case kind == kindTop:
		fs := t.fs(ctx)
		switch {
		case len(t.topRows) == 0 && (fs == nil || fs.Trends() == nil) && t.loading:
			out = append(out, t.statusLine(ctx, w))
		case len(t.topRows) == 0:
			empty(iconTrendsEmpty, textTrendsEmpty, hintTrendsEmpty)
		default:
			var at *time.Time
			if fs != nil && fs.Trends() != nil {
				at = fs.Trends().ComputedAt
			}
			if at != nil && rest >= len(t.topRows)+2 {
				list(t.top, len(t.topRows))
				out = append(out, fit(th, "", w),
					noteLine(th, humanize.Clock(*at, ctx.Loc, false)+textComputedAt, w))
			} else {
				list(t.top, rest)
			}
		}
	case kind == kindTags:
		if t.tagQ == "" || (t.tagQ != arg && len(t.tagRows) == 0) {
			out = append(out, t.statusLine(ctx, w))
		} else {
			list(t.tagList, rest)
		}
	default: // kindPosts
		switch {
		case t.postQ != t.query && len(t.posts.items) == 0:
			out = append(out, t.statusLine(ctx, w))
		case len(t.posts.items) == 0 && t.postErr != nil:
			empty("⚠", textwidth.Sanitize(api.Describe(t.postErr)), "")
		case len(t.posts.items) == 0:
			empty(iconSearch, textSearchEmpty, hintSearchEmpty)
		default:
			footer := 0
			if t.postLoadingMore {
				footer = 1
			}
			t.listY, t.listH = len(out), max(rest-footer, 0)
			out = append(out, t.posts.View(ctx, w, t.listH)...)
			if footer > 0 {
				out = append(out, t.statusLine(ctx, w))
			}
		}
	}
	return blankRows(th, out, w, h)
}

func (t *trends) Resize(ctx *ui.Context, size ui.Size) {
	t.size = size
	t.lastViewAt = ctx.Now() // the app resizes only placed (shown) instances
}

// ---- input ----

// rowList is the shown row List (nil when the post results are shown).
func (t *trends) rowList() (*widgets.List[trendRow], []trendRow) {
	switch kind, _ := t.shownKind(); kind {
	case kindTop:
		return t.top, t.topRows
	case kindTags:
		return t.tagList, t.tagRows
	}
	return nil, nil
}

// hasResults reports whether the list below the input has rows to move to.
func (t *trends) hasResults() bool {
	if t.note != "" {
		return false
	}
	if l, rows := t.rowList(); l != nil {
		return len(rows) > 0
	}
	return len(t.posts.items) > 0
}

// toResults moves the focus from the input to the first result.
func (t *trends) toResults() {
	if !t.hasResults() {
		return
	}
	t.setFocus(false)
	if l, rows := t.rowList(); l != nil {
		l.Select(rows[0].key)
		return
	}
	t.posts.list.Select(t.posts.items[0].ID)
}

// openRow opens a Top 10 / hashtag row: its tag feed.
func openRow(r trendRow) tea.Cmd {
	return ui.Navigate(ui.Route{Kind: ui.RouteHashtag, ID: r.tag})
}

// escape clears a non-empty query (focus back on the input), else closes the search line.
func (t *trends) escape(ctx *ui.Context) tea.Cmd {
	if t.input.Value() != "" {
		t.input.Reset()
		cmd := t.onInputChanged(ctx)
		t.setFocus(true)
		return cmd
	}
	t.closeSearch()
	return nil
}

func (t *trends) inputKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return true, t.escape(ctx)
	case "down", "tab", "enter":
		t.toResults()
		return true, nil
	case "up", "shift+tab":
		return true, nil
	}
	handled, cmd := inputUpdate(&t.input, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, t.onInputChanged(ctx))
}

func (t *trends) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if t.searchOpen && t.inputFocus {
		return t.inputKey(ctx, msg)
	}
	km := ctx.Keys
	switch s := msg.String(); s {
	case "/":
		t.openSearch()
		return true, nil
	case "esc":
		if t.searchOpen {
			return true, t.escape(ctx)
		}
		return false, nil
	case "1":
		return true, ui.Navigate(ui.Route{Kind: ui.RouteFeed, Arg: tabDiscover})
	case "2":
		return true, ui.Navigate(ui.Route{Kind: ui.RouteFeed, Arg: tabFollowing})
	case "3":
		return true, nil
	}
	if km.Matches(msg, keymap.PostCompose) {
		return true, ui.Navigate(ui.Route{Kind: ui.RouteCompose, Arg: "post"})
	}
	if l, rows := t.rowList(); l != nil {
		if len(rows) == 0 {
			return false, nil
		}
		if t.searchOpen && km.Matches(msg, keymap.ListUp) {
			if r, ok := l.Selected(); ok && r.key == rows[0].key {
				t.setFocus(true)
				return true, nil
			}
		}
		if km.Matches(msg, keymap.ListOpen) {
			if r, ok := l.Selected(); ok {
				return true, openRow(r)
			}
			return true, nil
		}
		return l.HandleKey(ctx, msg)
	}
	if t.note != "" || len(t.posts.items) == 0 {
		return false, nil
	}
	if t.searchOpen && km.Matches(msg, keymap.ListUp) && t.posts.selectedIndex() <= 0 {
		t.setFocus(true)
		return true, nil
	}
	handled, cmd := t.posts.handleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, t.maybeMorePosts(ctx))
}

func (t *trends) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	inList := t.listY >= 0 && local.Y >= t.listY && local.Y < t.listY+t.listH
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		if !inList {
			return nil
		}
		lp := ui.Point{X: local.X, Y: local.Y - t.listY}
		if l, _ := t.rowList(); l != nil {
			l.HandleMouse(ctx, msg, lp)
			return nil
		}
		t.posts.handleMouse(ctx, msg, lp)
		return t.maybeMorePosts(ctx)
	}
	if m, ok := msg.(tea.MouseClickMsg); !ok || m.Button != tea.MouseLeft {
		return nil
	}
	switch y := local.Y; {
	case y == t.tabY && t.tabHit != nil:
		if r, ok := t.tabHit(local.X); ok && r.Kind != ui.RouteTrends {
			return ui.Navigate(r)
		}
	case y == t.searchY:
		if !t.inputFocus {
			t.setFocus(true)
		}
	case inList:
		ly := y - t.listY
		if l, rows := t.rowList(); l != nil {
			start := 0
			for i, r := range rows {
				if r.key == l.Anchor().ItemID {
					start = i
					break
				}
			}
			if i := start + ly; i >= 0 && i < len(rows) {
				if t.searchOpen && t.inputFocus {
					t.setFocus(false)
				}
				l.Select(rows[i].key)
				return openRow(rows[i])
			}
			return nil
		}
		if t.searchOpen && t.inputFocus {
			t.setFocus(false)
		}
		cmd := t.posts.handleMouse(ctx, msg, ui.Point{X: local.X, Y: ly})
		return tea.Batch(cmd, t.maybeMorePosts(ctx))
	}
	return nil
}

// ---- optional capabilities ----

func (t *trends) Title(*ui.Context) (string, ui.Badge) { return textTrendsTitle, ui.Badge{} }

func (t *trends) Help(*ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	if t.searchOpen {
		return []key.Binding{b("#태그", "태그 검색"), b("esc", "닫기")}
	}
	return []key.Binding{b("enter", "태그 피드"), b("/", "검색")}
}

func (t *trends) WatchTopics() []string { return append([]string(nil), t.posts.visible...) }

// CapturesInput is true while the search input has the focus.
func (t *trends) CapturesInput() bool { return t.searchOpen && t.inputFocus }

// Completing lets tab reach the input (move to the results) before the
// hard-global focus cycle while there are results below it.
func (t *trends) Completing() bool { return t.CapturesInput() && t.hasResults() }

func (t *trends) Cursor() *tea.Cursor {
	if !t.CapturesInput() || t.searchY < 0 || t.size.W <= 0 {
		return nil
	}
	return tea.NewCursor(min(t.inputCol, t.size.W-1), t.searchY)
}

// selectedID is the selected row key of the shown list, or the selected post
// result's item ID.
func (t *trends) selectedID() string {
	if l, _ := t.rowList(); l != nil {
		if r, ok := l.Selected(); ok {
			return r.key
		}
		return ""
	}
	return t.posts.selectedID()
}

func (t *trends) SaveState() any {
	return trendsState{Open: t.searchOpen, Query: t.input.Value(), InputFocus: t.inputFocus, Selected: t.selectedID()}
}

func (t *trends) LoadState(v any) {
	if s, ok := v.(trendsState); ok {
		t.pending = &s
	}
}
