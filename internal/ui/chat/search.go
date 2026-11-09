package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteSearch, newSearch)
	palette.Register(palette.Command{
		ID: "search.messages", Title: "메시지 검색", Keywords: []string{"search", "messages", "검색", "메시지 찾기"}, Group: palette.GroupNav, KeyHint: "ctrl+f",
		Run: func(_ *ui.Context, args string) tea.Cmd {
			return ui.Navigate(ui.Route{Kind: ui.RouteSearch, Arg: strings.TrimSpace(args)})
		},
	})
}

// Search texts and limits.
const (
	searchTitle       = "검색"
	searchQueryMax    = 100 // api.md 0.3: 검색어 1..100
	searchPageLimit   = 20
	searchPlaceholder = "메시지 검색 — 검색어를 입력하고 enter"
	searchIdleText    = "메시지를 검색해 보세요"
	searchIdleHint    = "검색어를 입력하고 enter"
	searchLoadingText = "검색 중…"
	searchEmptyText   = "검색 결과가 없어요"
	searchEmptyHint   = "다른 낱말로 찾아 보세요"
	searchScopeText   = "검색 범위가 너무 넓어요 — 방이나 채널을 골라 주세요"
	searchMoreFailed  = "검색 결과를 더 불러오지 못했어요"
	searchTagPrefix   = "chat.search:"
	searchChipSep     = " · "
)

// searchScope is where a search looks: the route's channel, a space, or everything readable.
type searchScope int

// Search scopes (chip order).
const (
	scopeChannel searchScope = iota
	scopeSpace
	scopeAll
)

func (s searchScope) label() string {
	switch s {
	case scopeChannel:
		return "이 채널"
	case scopeSpace:
		return "이 방"
	}
	return "전체"
}

// searchRow is one result row (ID = the message id).
type searchRow struct {
	ID  string
	hit api.MessageSearchHit
}

// searchPage is one search result (owner/gen guard against stale and foreign results).
type searchPage struct {
	owner *searchView
	gen   int
	more  bool
	page  *api.Page[api.MessageSearchHit]
}

// searchSaved is the Persistent state of a search.
type searchSaved struct {
	Query string
	Scope int
}

// chipSpan is the cell range [x0, x1) of a scope chip on the chips row (last View).
type chipSpan struct {
	x0, x1 int
	scope  searchScope
}

// searchView is RouteSearch: the query input (focused first), the scope chips
// 이 채널 · 이 방 · 전체 and the result list (newest first, paged at the bottom).
// Route.ID is the channel or space scope ("" = everything), Route.Arg a query to
// search at once.
type searchView struct {
	scope     searchScope
	scopes    []searchScope // the offered scopes, in chip order
	channelID string
	spaceID   string

	input   textinput.Model
	inputOn bool
	curX    int // the input cursor column (last View)

	query       string // the query of the shown results
	hits        []api.MessageSearchHit
	list        *widgets.List[searchRow]
	next        *string
	gen         int
	loading     bool
	loadingMore bool
	searched    bool
	errText     string

	size    ui.Size
	focused bool
	rowH    int // lines per result row (last View)
	chips   []chipSpan
}

var (
	_ ui.Component     = (*searchView)(nil)
	_ ui.KeyHandler    = (*searchView)(nil)
	_ ui.MouseHandler  = (*searchView)(nil)
	_ ui.InputCapturer = (*searchView)(nil)
	_ ui.Cursorer      = (*searchView)(nil)
	_ ui.Titled        = (*searchView)(nil)
	_ ui.Helper        = (*searchView)(nil)
	_ ui.Persistent    = (*searchView)(nil)
)

// newSearch is the RouteSearch factory.
func newSearch(ctx *ui.Context, r ui.Route) ui.Component {
	v := &searchView{input: newTextInput(), rowH: 2}
	v.input.CharLimit = searchQueryMax
	v.list = v.newList()
	v.setScopes(ctx, r.ID)
	if q := strings.TrimSpace(oneLine(r.Arg)); q != "" {
		v.input.SetValue(q)
		v.input.CursorEnd()
	}
	v.setInput(v.input.Value() == "")
	return v
}

func (v *searchView) newList() *widgets.List[searchRow] {
	return widgets.NewList(func(r searchRow) string { return r.ID }, v.render)
}

// setScopes derives the offered scopes from the route id: a space = 이 방 · 전체, a
// channel = 이 채널 (· 이 방 when it has a space) · 전체, none = (이 방 of the
// current space ·) 전체. The first offered scope is the starting one, except that
// a global search starts at 전체.
func (v *searchView) setScopes(ctx *ui.Context, id string) {
	var spaces map[string]bool
	if ctx != nil && ctx.Store != nil {
		spaces = map[string]bool{}
		for sid := range ctx.Store.Spaces {
			spaces[sid] = true
		}
	}
	switch {
	case id != "" && spaces[id]:
		v.spaceID = id
		v.scopes, v.scope = []searchScope{scopeSpace, scopeAll}, scopeSpace
	case id != "":
		v.channelID = id
		v.scopes, v.scope = []searchScope{scopeChannel}, scopeChannel
		if ctx != nil && ctx.Store != nil {
			if ch := ctx.Store.Channels[id]; ch != nil && ch.SpaceID != nil && *ch.SpaceID != "" {
				v.spaceID = *ch.SpaceID
				v.scopes = append(v.scopes, scopeSpace)
			}
		}
		v.scopes = append(v.scopes, scopeAll)
	default:
		v.spaceID = currentSpaceID(ctx)
		v.scopes, v.scope = []searchScope{scopeAll}, scopeAll
		if v.spaceID != "" {
			v.scopes = []searchScope{scopeSpace, scopeAll}
		}
	}
}

// setInput focuses (on) or leaves (off) the query input.
func (v *searchView) setInput(on bool) {
	v.inputOn = on
	if on {
		v.input.Focus()
		v.input.CursorEnd()
	} else {
		v.input.Blur()
	}
}

// ---- requests ----

func (v *searchView) Init(ctx *ui.Context) tea.Cmd { return v.search(ctx) }

// request is the search query of the current scope.
func (v *searchView) request(q string) api.MessageSearchQuery {
	r := api.MessageSearchQuery{Q: q, PageOpts: api.PageOpts{Limit: searchPageLimit}}
	switch v.scope {
	case scopeChannel:
		r.ChannelID = v.channelID
	case scopeSpace:
		r.SpaceID = v.spaceID
	}
	return r
}

// search starts a new search of the input's query in the current scope (nothing
// for a blank query) and moves the focus to the results.
func (v *searchView) search(ctx *ui.Context) tea.Cmd {
	q := strings.TrimSpace(v.input.Value())
	if q == "" {
		return nil
	}
	v.gen++
	v.query, v.loading, v.loadingMore = q, true, false
	v.setInput(false)
	gen, req := v.gen, v.request(q)
	return ui.Do(ctx, fmt.Sprintf("%s%d", searchTagPrefix, gen), func(c context.Context, cl *api.Client) (searchPage, error) {
		p, err := cl.SearchMessages(c, req)
		return searchPage{owner: v, gen: gen, page: p}, err
	})
}

// more fetches the next page of the shown results.
func (v *searchView) more(ctx *ui.Context) tea.Cmd {
	if v.next == nil || v.loading || v.loadingMore || v.query == "" {
		return nil
	}
	v.loadingMore = true
	gen, req := v.gen, v.request(v.query)
	req.Cursor = *v.next
	return ui.Do(ctx, fmt.Sprintf("%s%d", searchTagPrefix, gen), func(c context.Context, cl *api.Client) (searchPage, error) {
		p, err := cl.SearchMessages(c, req)
		return searchPage{owner: v, gen: gen, more: true, page: p}, err
	})
}

// searchErrText is the on-screen text of a failed search: limit_exceeded with
// details.what == "search_scope" asks for a narrower scope, else api.Describe.
func searchErrText(err error) string {
	var e *api.Error
	if api.IsCode(err, "limit_exceeded") && errors.As(err, &e) {
		var d struct {
			What string `json:"what"`
		}
		if json.Unmarshal(e.Details, &d) == nil && d.What == "search_scope" {
			return searchScopeText
		}
	}
	return api.Describe(err)
}

func (v *searchView) onPage(m ui.ResultMsg[searchPage]) tea.Cmd {
	if m.Value.more {
		v.loadingMore = false
		if m.Err != nil {
			return errorCmd(m.Err, searchMoreFailed)
		}
		if m.Value.page != nil {
			for _, h := range m.Value.page.Items {
				if !slices.ContainsFunc(v.hits, func(x api.MessageSearchHit) bool { return x.Message.ID == h.Message.ID }) {
					v.hits = append(v.hits, h)
				}
			}
			v.next = m.Value.page.NextCursor
		} else {
			v.next = nil
		}
		v.sync()
		return nil
	}
	v.loading, v.searched = false, true
	v.list = v.newList()
	v.hits, v.next, v.errText = nil, nil, ""
	if m.Err != nil {
		v.errText = searchErrText(m.Err)
	} else if m.Value.page != nil {
		for _, h := range m.Value.page.Items {
			if !slices.ContainsFunc(v.hits, func(x api.MessageSearchHit) bool { return x.Message.ID == h.Message.ID }) {
				v.hits = append(v.hits, h)
			}
		}
		v.next = m.Value.page.NextCursor
	}
	v.sync()
	return nil
}

// sync rebuilds the list rows (selection kept by id).
func (v *searchView) sync() {
	rows := make([]searchRow, 0, len(v.hits))
	for _, h := range v.hits {
		rows = append(rows, searchRow{ID: h.Message.ID, hit: h})
	}
	v.list.SetItems(rows)
}

func (v *searchView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[searchPage]:
		if m.Value.owner == v && m.Value.gen == v.gen {
			return v.onPage(m)
		}
	case tea.PasteMsg:
		if v.inputOn {
			_, cmd := inputUpdate(&v.input, m)
			return cmd
		}
	}
	return nil
}

func (v *searchView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

// ---- scope ----

// scopeIndex is the chip index of the current scope.
func (v *searchView) scopeIndex() int {
	return max(0, slices.Index(v.scopes, v.scope))
}

// setScope switches to scope (an offered one) and searches again when it changed.
func (v *searchView) setScope(ctx *ui.Context, s searchScope) tea.Cmd {
	if s == v.scope || !slices.Contains(v.scopes, s) {
		return nil
	}
	v.scope = s
	if strings.TrimSpace(v.input.Value()) == "" {
		v.input.SetValue(v.query)
	}
	return v.search(ctx)
}

// ---- drawing ----

// headerRows is the rows above the results: the input, plus the chips row when
// more than one scope is offered and there is room.
func (v *searchView) headerRows() int {
	if len(v.scopes) > 1 && v.size.H >= 3 {
		return 2
	}
	return 1
}

// oneLineRows reports the one-line result rows of the line/minimal densities.
func oneLineRows(ctx *ui.Context) bool {
	if ctx == nil {
		return false
	}
	d := ctx.Density()
	return d == ui.DensityLine || d == ui.DensityMinimal
}

// refLabel is the sanitized channel label of a result: "#개발" / "🔒개발", a DM's
// label from the Store, else its name, else "DM".
func refLabel(ctx *ui.Context, ref api.ChannelRef) string {
	name := ""
	if ref.Name != nil {
		name = *ref.Name
	}
	var held *api.Channel
	if ctx != nil && ctx.Store != nil {
		held = ctx.Store.Channels[ref.ID]
	}
	if ref.Kind == api.ChannelText {
		if held != nil && held.IsPrivate {
			return "🔒" + textwidth.Sanitize(name)
		}
		return "#" + textwidth.Sanitize(name)
	}
	if held != nil {
		if l := ctx.Store.ChannelLabel(ref.ID); l != "" {
			return textwidth.Sanitize(l)
		}
	}
	if name != "" {
		return textwidth.Sanitize(name)
	}
	return "DM"
}

// hitMeta is "#개발 · 노바 개발방 · 민지 · 11:02" (short: "#개발 민지 11:02").
func hitMeta(ctx *ui.Context, h api.MessageSearchHit, short bool) string {
	now := time.Now()
	if ctx != nil && ctx.Now != nil {
		now = ctx.Now()
	}
	at := humanize.MessageTime(now, h.Message.CreatedAt, locOf(ctx))
	who := textwidth.Sanitize(displayName(h.Message.Author))
	if short {
		return refLabel(ctx, h.Channel) + " " + who + " " + at
	}
	parts := []string{refLabel(ctx, h.Channel)}
	if h.Channel.SpaceName != nil && *h.Channel.SpaceName != "" {
		parts = append(parts, textwidth.Sanitize(*h.Channel.SpaceName))
	}
	return strings.Join(append(parts, who, at), " · ")
}

// render draws a result: column 0 is the focus cursor "›"; two lines (meta, then
// the highlighted snippet) or one line in the line/minimal densities.
func (v *searchView) render(ctx *ui.Context, r searchRow, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && v.focused && !v.inputOn {
		cur = t.S.Title.Render("›")
	}
	cw := width - 1
	if cw <= 0 {
		return []string{cur}
	}
	h := r.hit
	if oneLineRows(ctx) {
		meta := hitMeta(ctx, h, true)
		room := cw - textwidth.Width(meta) - 1
		if room < 6 {
			return []string{cur + cell(t.S.Dim.Render, meta, cw)}
		}
		snip := highlightSnippet(t, h.Message.Body, h.Highlights, room)
		return []string{cur + t.S.Dim.Render(meta) + blank(t, 1) + snip + blank(t, room-textwidth.Width(snip))}
	}
	ind := 0
	if cw >= 10 {
		ind = 2
	}
	snip := highlightSnippet(t, h.Message.Body, h.Highlights, cw-ind)
	return []string{
		cur + cell(t.S.Dim.Render, hitMeta(ctx, h, false), cw),
		blank(t, 1+ind) + snip + blank(t, cw-ind-textwidth.Width(snip)),
	}
}

// chipsLine draws the scope chips in exactly w cells and records their spans
// (" · " separators, a single space when that does not fit).
func (v *searchView) chipsLine(t *theme.Theme, w int) string {
	v.chips = v.chips[:0]
	for _, sep := range []string{searchChipSep, " "} {
		total := 0
		for i, s := range v.scopes {
			if i > 0 {
				total += textwidth.Width(sep)
			}
			total += textwidth.Width(s.label())
		}
		if total <= w || sep == " " {
			var b strings.Builder
			x := 0
			for i, s := range v.scopes {
				if i > 0 {
					b.WriteString(t.S.Dim.Render(sep))
					x += textwidth.Width(sep)
				}
				st := t.S.Dim
				if s == v.scope {
					st = t.S.Selected
				}
				l := s.label()
				b.WriteString(st.Render(l))
				if x+textwidth.Width(l) <= w {
					v.chips = append(v.chips, chipSpan{x0: x, x1: x + textwidth.Width(l), scope: s})
				}
				x += textwidth.Width(l)
			}
			line := b.String()
			if x > w {
				return textwidth.Truncate(line, w, "")
			}
			return line + blank(t, w-x)
		}
	}
	return blank(t, w)
}

// bodyView is the area under the header: an error, the idle / loading / empty
// states, or the results.
func (v *searchView) bodyView(ctx *ui.Context, size ui.Size) string {
	t := themeOf(ctx)
	switch {
	case v.errText != "":
		return widgets.Empty(t, size, "⚠", v.errText, "")
	case len(v.hits) > 0:
		return v.list.View(ctx, size)
	case v.loading:
		return widgets.Empty(t, size, "", searchLoadingText, "")
	case v.searched:
		return widgets.Empty(t, size, "🔍", searchEmptyText, searchEmptyHint)
	}
	return widgets.Empty(t, size, "🔍", searchIdleText, searchIdleHint)
}

func (v *searchView) View(ctx *ui.Context, focused bool) string {
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	v.focused = focused
	if oneLineRows(ctx) {
		v.rowH = 1
	} else {
		v.rowH = 2
	}
	t := themeOf(ctx)
	prefix, pw := "", 0
	if w >= 12 {
		prefix, pw = t.S.Title.Render("🔍")+blank(t, 1), 3
	}
	in, col := inputLine(t, v.input, searchPlaceholder, w-pw)
	v.curX = pw + col
	lines := []string{prefix + in}
	hdr := v.headerRows()
	if hdr == 2 {
		lines = append(lines, v.chipsLine(t, w))
	}
	if h > hdr {
		lines = append(lines, strings.Split(v.bodyView(ctx, ui.Size{W: w, H: h - hdr}), "\n")...)
	}
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// Cursor is the real cursor in the input while it is focused.
func (v *searchView) Cursor() *tea.Cursor {
	if !v.inputOn || v.size.H < 1 || v.curX >= v.size.W {
		return nil
	}
	return tea.NewCursor(v.curX, 0)
}

func (v *searchView) CapturesInput() bool { return v.inputOn }

// Title is "검색 · <scope>": the channel label, the space name or 전체.
func (v *searchView) Title(ctx *ui.Context) (string, ui.Badge) {
	scope := scopeAll.label()
	switch v.scope {
	case scopeChannel:
		scope = scopeChannel.label()
		if ctx != nil && ctx.Store != nil {
			if ch := ctx.Store.Channels[v.channelID]; ch != nil {
				ref := api.ChannelRef{ID: ch.ID, Kind: ch.Kind, Name: ch.Name}
				scope = refLabel(ctx, ref)
			}
		}
	case scopeSpace:
		scope = scopeSpace.label()
		if ctx != nil && ctx.Store != nil {
			if sd := ctx.Store.Spaces[v.spaceID]; sd != nil && sd.Name != "" {
				scope = textwidth.Sanitize(sd.Name)
			}
		}
	}
	return searchTitle + " · " + scope, ui.Badge{}
}

func (v *searchView) Help(ctx *ui.Context) []key.Binding {
	if v.inputOn {
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "검색"))}
	}
	hs := ctx.Keys.Help(keymap.ListOpen, keymap.ChannelSearch)
	return append(hs, key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "범위")))
}

func (v *searchView) SaveState() any {
	q := strings.TrimSpace(v.input.Value())
	if q == "" {
		q = v.query
	}
	return searchSaved{Query: q, Scope: int(v.scope)}
}

func (v *searchView) LoadState(s any) {
	var st searchSaved
	switch x := s.(type) {
	case searchSaved:
		st = x
	case *searchSaved:
		if x == nil {
			return
		}
		st = *x
	default:
		return
	}
	if sc := searchScope(st.Scope); slices.Contains(v.scopes, sc) {
		v.scope = sc
	}
	if q := strings.TrimSpace(oneLine(st.Query)); q != "" {
		v.input.SetValue(q)
		v.setInput(false)
	}
}

// ---- input ----

// open jumps to a result: a top-level message opens its channel at it, a thread
// reply its thread with the reply selected.
func (v *searchView) open(r searchRow) tea.Cmd {
	m := r.hit.Message
	ch := r.hit.Channel.ID
	if ch == "" {
		ch = m.ChannelID
	}
	if ch == "" {
		return nil
	}
	if m.ThreadRootID != nil && *m.ThreadRootID != "" {
		return ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: *m.ThreadRootID, Arg: ch, Anchor: m.ID})
	}
	return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: m.ID})
}

// inputKey handles keys while the input is focused: enter searches, esc / ↓ go to
// the results (esc with nothing searched is left to the app: back), the rest edits.
func (v *searchView) inputKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return true, v.search(ctx)
	case "esc":
		if !v.searched && !v.loading {
			return false, nil
		}
		v.setInput(false)
		return true, nil
	case "down":
		if len(v.hits) == 0 {
			return true, nil
		}
		v.setInput(false)
		return true, nil
	}
	return inputUpdate(&v.input, msg)
}

func (v *searchView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	v.focused = true
	if v.inputOn {
		return v.inputKey(ctx, msg)
	}
	switch {
	case ctx.Keys.Matches(msg, keymap.ComposeFocus), ctx.Keys.Matches(msg, keymap.ChannelSearch):
		v.setInput(true)
		return true, nil
	}
	switch msg.String() {
	case "left", "h":
		if i := v.scopeIndex(); i > 0 {
			return true, v.setScope(ctx, v.scopes[i-1])
		}
		return true, nil
	case "right", "l":
		if i := v.scopeIndex(); i < len(v.scopes)-1 {
			return true, v.setScope(ctx, v.scopes[i+1])
		}
		return true, nil
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		r, ok := v.list.Selected()
		if !ok {
			return false, nil
		}
		return true, v.open(r)
	}
	prev, _ := v.list.Selected()
	atLast := len(v.hits) > 0 && prev.ID == v.hits[len(v.hits)-1].Message.ID
	down := ctx.Keys.Matches(msg, keymap.ListDown) || ctx.Keys.Matches(msg, keymap.ListPageDown) || ctx.Keys.Matches(msg, keymap.ListBottom)
	handled, cmd := v.list.HandleKey(ctx, msg)
	if handled && down && atLast {
		cmd = tea.Batch(cmd, v.more(ctx))
	}
	return handled, cmd
}

// rowAt is the result index drawn at body line y (rows are rowH lines), or -1.
func (v *searchView) rowAt(y int) int {
	if y < 0 || y >= v.size.H-v.headerRows() || len(v.hits) == 0 {
		return -1
	}
	a := v.list.Anchor()
	top := slices.IndexFunc(v.hits, func(h api.MessageSearchHit) bool { return h.Message.ID == a.ItemID })
	if top < 0 {
		top = 0
	}
	if i := top + (a.LineOffset+y)/max(1, v.rowH); i < len(v.hits) {
		return i
	}
	return -1
}

// HandleMouse: a click on the input focuses it, on a chip switches the scope; a
// left click selects a result and a click on the selected one opens it; the
// wheel scrolls.
func (v *searchView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	hdr := v.headerRows()
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		if local.Y >= hdr && len(v.hits) > 0 {
			return v.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - hdr})
		}
		return nil
	}
	if click.Button != tea.MouseLeft {
		return nil
	}
	switch {
	case local.Y == 0:
		v.setInput(true)
		return nil
	case local.Y == 1 && hdr == 2:
		for _, c := range v.chips {
			if local.X >= c.x0 && local.X < c.x1 {
				return v.setScope(ctx, c.scope)
			}
		}
		return nil
	}
	i := v.rowAt(local.Y - hdr)
	if i < 0 {
		return nil
	}
	r := searchRow{ID: v.hits[i].Message.ID, hit: v.hits[i]}
	wasInput := v.inputOn
	v.setInput(false)
	if cur, ok := v.list.Selected(); ok && cur.ID == r.ID && !wasInput {
		return v.open(r)
	}
	v.list.Select(r.ID)
	return nil
}

// ---- snippet ----

// snipSpan is a sanitized run of a snippet, highlighted or not.
type snipSpan struct {
	s  string
	hl bool
}

var snipSpaces = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

// highlightSnippet draws body on one line of at most width cells with the search
// highlights (code point offsets into body; out-of-range ones ignored, ends
// clamped, edges widened to whole grapheme clusters) in S.Selected. Newlines become spaces and every run is sanitized. When
// the first highlight would not be visible, the start is cut to "…" + a little
// context before it; an overlong end is cut with "…".
func highlightSnippet(t *theme.Theme, body string, hs []api.SearchHighlight, width int) string {
	if width <= 0 {
		return ""
	}
	rs := []rune(body)
	mark := make([]bool, len(rs))
	for _, h := range hs {
		if h.Length <= 0 || h.Start < 0 || h.Start >= len(rs) {
			continue
		}
		end := len(rs)
		if h.Length < len(rs)-h.Start {
			end = h.Start + h.Length
		}
		for i := h.Start; i < end; i++ {
			mark[i] = true
		}
	}
	// Highlights snap outward to whole grapheme clusters: a cluster is marked when
	// any of its code points is, so no Sanitize call ever sees a partial cluster
	// (a split "👍🏽" or ZWJ sequence or flag would otherwise render differently).
	type cluster struct {
		s  string
		hl bool
	}
	var cs []cluster
	for rest, i := body, 0; rest != ""; {
		g, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if g == "" { // defensive: never loop forever
			_, n := utf8.DecodeRuneInString(rest)
			g = rest[:n]
		}
		rest = rest[len(g):]
		n := utf8.RuneCountInString(g)
		cs = append(cs, cluster{s: g, hl: slices.Contains(mark[i:min(i+n, len(mark))], true)})
		i += n
	}
	var spans []snipSpan
	for i := 0; i < len(cs); {
		var run strings.Builder
		j := i
		for ; j < len(cs) && cs[j].hl == cs[i].hl; j++ {
			run.WriteString(cs[j].s)
		}
		if s := textwidth.Sanitize(snipSpaces.Replace(run.String())); s != "" {
			spans = append(spans, snipSpan{s: s, hl: cs[i].hl})
		}
		i = j
	}
	if spansWidth(spans) > width {
		spans = cutStart(spans, width)
		spans = cutEnd(spans, width)
	}
	var b strings.Builder
	for _, sp := range spans {
		if sp.hl {
			b.WriteString(t.S.Selected.Render(sp.s))
		} else {
			b.WriteString(t.S.Base.Render(sp.s))
		}
	}
	return b.String()
}

func spansWidth(spans []snipSpan) int {
	n := 0
	for _, sp := range spans {
		n += textwidth.Width(sp.s)
	}
	return n
}

// cutStart replaces the text before the first highlight with "…" + up to a third
// of the spare room of context when the highlight would not fit otherwise.
func cutStart(spans []snipSpan, width int) []snipSpan {
	first := slices.IndexFunc(spans, func(sp snipSpan) bool { return sp.hl })
	if first <= 0 {
		return spans
	}
	var pre strings.Builder
	for _, sp := range spans[:first] {
		pre.WriteString(sp.s)
	}
	preW, hlW := textwidth.Width(pre.String()), textwidth.Width(spans[first].s)
	if preW+hlW <= width-1 {
		return spans
	}
	keep := min(preW, max(0, (width-1-hlW)/3))
	lead := "…" + strings.TrimLeft(textwidth.TruncateLeft(pre.String(), keep, ""), " ")
	return append([]snipSpan{{s: lead}}, spans[first:]...)
}

// cutEnd truncates spans to width cells ending in "…" when they are wider.
func cutEnd(spans []snipSpan, width int) []snipSpan {
	if spansWidth(spans) <= width {
		return spans
	}
	room := width - 1
	var out []snipSpan
	for _, sp := range spans {
		w := textwidth.Width(sp.s)
		if w <= room {
			out = append(out, sp)
			room -= w
			continue
		}
		if room > 0 {
			if s := strings.TrimRight(textwidth.Truncate(sp.s, room, ""), " "); s != "" {
				out = append(out, snipSpan{s: s, hl: sp.hl})
			}
		}
		break
	}
	return append(out, snipSpan{s: "…"})
}
