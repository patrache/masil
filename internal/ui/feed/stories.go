package feed

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteStories, newStories)
}

// Story viewer tuning.
const (
	storyAdvance  = 5 * time.Second // auto-advance period
	storyCaptionN = 3               // most caption rows under a photo
)

// textStoryPaused marks a paused story (copy: the feed catalog area, story_*).
const textStoryPaused = "⏸"

// Story viewer result tags (ui.Do).
const (
	tagUserStories = "feed.user_stories:" // + user id: GET /v1/users/{user}/stories
	tagStoryView   = "feed.story_view:"   // + story id: POST /v1/stories/{id}/view
	tagDeleteStory = "feed.delete_story:" // + story id: DELETE /v1/stories/{id}
)

// storyGens numbers every armed story timer, so the ticks of two viewer
// instances never mix and a stale tick is recognized.
var storyGens atomic.Int64

// storyTickMsg is the auto-advance timer of the viewer whose current gen is Gen.
type storyTickMsg struct{ Gen int }

// storyDeleteOutcome is a DELETE /v1/stories/{id} request.
type storyDeleteOutcome struct {
	claim
	storyID string
}

// stories is RouteStories: one user's stories full screen, the progress bar on
// top, auto-advancing every 5 s, then the next unseen group of the ring.
//
// The group comes from FeedStore.Stories(); a user the ring does not have (a
// public account I do not follow, opened from the profile) is loaded with
// ListUserStories into a group only this screen holds.
type stories struct {
	key    string
	userID string // the route's user: the group the viewer starts with

	cur     string          // user id of the group shown
	temp    *api.StoryGroup // the screen-only group (ListUserStories), nil when cur is a ring group
	storyID string          // the story shown ("" before the first one / after the end)
	order   []string        // story ids of the group at the last show / settle (to step past a deleted one)
	viewed  map[string]bool // POST view sent

	gen     int  // current timer gen (0 = none)
	active  bool // the app's focus is on this route: the timer runs, Back may be sent
	paused  bool // the menu was opened: the timer waits for the next key
	left    bool // the viewer ended (or was left with esc): the next activation starts over
	loading bool // ListUserStories in flight

	size ui.Size
	imgs imageSlots
	spin *widgets.Spinner
}

var (
	_ ui.Component    = (*stories)(nil)
	_ ui.KeyHandler   = (*stories)(nil)
	_ ui.MouseHandler = (*stories)(nil)
	_ ui.Titled       = (*stories)(nil)
	_ ui.Helper       = (*stories)(nil)
)

// newStories builds RouteStories (ID = the user id of the group to start with).
func newStories(_ *ui.Context, r ui.Route) ui.Component {
	return &stories{key: r.Key(), userID: r.ID, active: true, viewed: map[string]bool{}, spin: widgets.NewSpinner()}
}

func (s *stories) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

// Init marks the story ring open for resync and shows the route's group.
func (s *stories) Init(ctx *ui.Context) tea.Cmd {
	fs := s.fs(ctx)
	if fs == nil {
		return nil
	}
	fs.SetOpen("stories", true)
	return s.start(ctx)
}

// start (re)starts the viewer at the route's group: the ring group when the
// ring has it, else the screen-only group loaded with ListUserStories.
func (s *stories) start(ctx *ui.Context) tea.Cmd {
	s.left, s.paused = false, false
	s.cur, s.temp, s.storyID, s.order = s.userID, nil, "", nil
	s.cancel()
	fs := s.fs(ctx)
	fs.PruneStories(ctx.Now())
	if ctx.Store.IsBlocked(s.userID) {
		return s.finish()
	}
	if g := s.ringGroup(fs, s.userID); g != nil {
		return s.showGroup(ctx, g)
	}
	s.loading = true
	id := s.userID
	return tea.Batch(s.spin.Start(ctx), ui.Do(ctx, tagUserStories+id, func(c context.Context, a *api.Client) ([]api.Story, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListUserStories(c, api.UserID(id))
	}))
}

// ringGroup is userID's group in the FeedStore ring, or nil.
func (s *stories) ringGroup(fs *feedstore.FeedStore, userID string) *api.StoryGroup {
	if fs == nil {
		return nil
	}
	gs := fs.Stories()
	for i := range gs {
		if gs[i].User.ID == userID {
			return &gs[i]
		}
	}
	return nil
}

// group is the group shown: the screen-only one, else cur's ring group.
func (s *stories) group(ctx *ui.Context) *api.StoryGroup {
	if s.temp != nil {
		if len(s.temp.Stories) == 0 {
			return nil
		}
		return s.temp
	}
	return s.ringGroup(s.fs(ctx), s.cur)
}

// prune drops expired stories from the ring and the screen-only group.
func (s *stories) prune(ctx *ui.Context) {
	now := ctx.Now()
	if fs := s.fs(ctx); fs != nil {
		fs.PruneStories(now)
	}
	if s.temp != nil {
		s.dropTemp(func(st api.Story) bool { return st.ExpiresAt.After(now) })
	}
}

// dropTemp keeps the screen-only stories for which keep is true.
func (s *stories) dropTemp(keep func(api.Story) bool) {
	kept := s.temp.Stories[:0:0]
	for _, st := range s.temp.Stories {
		if keep(st) {
			kept = append(kept, st)
		}
	}
	s.temp.Stories = kept
	s.temp.HasUnseen = anyStoryUnseen(kept)
}

func anyStoryUnseen(ss []api.Story) bool {
	for _, st := range ss {
		if !st.Seen {
			return true
		}
	}
	return false
}

func storyIDs(g *api.StoryGroup) []string {
	out := make([]string, len(g.Stories))
	for i, st := range g.Stories {
		out[i] = st.ID
	}
	return out
}

// resolve is the story to draw: the current one, or (when it was deleted or
// expired) the first one after it in the order it had, still present. ok is
// false when the group is gone, blocked or has nothing left after the current.
func (s *stories) resolve(ctx *ui.Context) (g *api.StoryGroup, idx int, ok bool) {
	g = s.group(ctx)
	if g == nil || s.storyID == "" || ctx.Store.IsBlocked(g.User.ID) {
		return nil, 0, false
	}
	at := map[string]int{}
	for i, st := range g.Stories {
		at[st.ID] = i
	}
	if i, ok := at[s.storyID]; ok {
		return g, i, true
	}
	past := false
	for _, id := range s.order {
		if id == s.storyID {
			past = true
			continue
		}
		if i, ok := at[id]; ok && past {
			return g, i, true
		}
	}
	return g, 0, false
}

// cancel stops the pending timer (its tick no longer matches).
func (s *stories) cancel() { s.gen = 0 }

// arm starts the auto-advance timer for the story shown (only while active and
// not paused).
func (s *stories) arm(ctx *ui.Context) tea.Cmd {
	s.cancel()
	if !s.active || s.paused || s.storyID == "" {
		return nil
	}
	s.gen = int(storyGens.Add(1))
	return ui.After(ctx, storyAdvance, storyTickMsg{Gen: s.gen})
}

// showGroup shows g from its first unseen story (else its first).
func (s *stories) showGroup(ctx *ui.Context, g *api.StoryGroup) tea.Cmd {
	idx := 0
	for i, st := range g.Stories {
		if !st.Seen {
			idx = i
			break
		}
	}
	return s.show(ctx, g, idx)
}

// show makes g.Stories[idx] the current story: marks it seen (POST view once
// per id, not for my own or an already seen story), loads its photo and
// restarts the timer. An instance without the focus (the user is elsewhere)
// only moves; onFocus shows the story for real when it is back.
func (s *stories) show(ctx *ui.Context, g *api.StoryGroup, idx int) tea.Cmd {
	if g == nil || idx < 0 || idx >= len(g.Stories) {
		return nil
	}
	st := g.Stories[idx]
	s.cur, s.storyID, s.order = g.User.ID, st.ID, storyIDs(g)
	if !s.active {
		s.cancel()
		return nil
	}
	var cmds []tea.Cmd
	if !st.Seen && !s.viewed[st.ID] && !ctx.Store.IsMe(st.Author.ID) {
		s.viewed[st.ID] = true
		id := st.ID
		cmds = append(cmds, ui.Do(ctx, tagStoryView+id, func(c context.Context, a *api.Client) (struct{}, error) {
			if a == nil {
				return struct{}{}, errNoClient
			}
			return struct{}{}, a.ViewStory(c, id)
		}))
	}
	if s.temp != nil {
		s.temp.Stories[idx].Seen = true
		s.temp.HasUnseen = anyStoryUnseen(s.temp.Stories)
	} else if fs := s.fs(ctx); fs != nil {
		fs.MarkStorySeen(st.ID)
	}
	if st.Media != nil {
		cmds = append(cmds, s.imgs.request(ctx, *st.Media))
	}
	return tea.Batch(append(cmds, s.arm(ctx))...)
}

// finish ends the viewer: Back while this route has the focus (never from an
// instance the user already left), and the next activation starts over.
func (s *stories) finish() tea.Cmd {
	s.left = true
	s.storyID, s.order = "", nil
	s.cancel()
	if !s.active {
		return nil
	}
	return ui.Back()
}

// next is space / → / n / the timer: the next story, the next unseen group at
// the end of a ring group, else the end.
func (s *stories) next(ctx *ui.Context) tea.Cmd {
	g, idx, ok := s.resolve(ctx)
	if ok && idx+1 < len(g.Stories) {
		return s.show(ctx, g, idx+1)
	}
	return s.nextGroup(ctx)
}

// prev is ← / p: the previous story of the group (the first one restarts).
func (s *stories) prev(ctx *ui.Context) tea.Cmd {
	g, idx, ok := s.resolve(ctx)
	if !ok {
		return s.nextGroup(ctx)
	}
	return s.show(ctx, g, max(idx-1, 0))
}

// nextGroup moves to the next ring group with unseen stories after the current
// one (wrapping; blocked users skipped). A screen-only group never continues
// into the ring.
func (s *stories) nextGroup(ctx *ui.Context) tea.Cmd {
	if s.temp != nil {
		return s.finish()
	}
	fs := s.fs(ctx)
	if fs == nil {
		return s.finish()
	}
	gs := fs.Stories()
	start := 0
	for i := range gs {
		if gs[i].User.ID == s.cur {
			start = i + 1
			break
		}
	}
	for k := range gs {
		g := &gs[(start+k)%len(gs)]
		if g.User.ID == s.cur || !g.HasUnseen || len(g.Stories) == 0 || ctx.Store.IsBlocked(g.User.ID) {
			continue
		}
		return s.showGroup(ctx, g)
	}
	return s.finish()
}

// settle follows the ring after a change (deletion, expiry, block, resync): a
// current story that is gone hands over to the next one.
func (s *stories) settle(ctx *ui.Context) tea.Cmd {
	if s.loading || s.left || s.storyID == "" {
		return nil
	}
	g, idx, ok := s.resolve(ctx)
	if !ok {
		if !s.active {
			return s.finish() // nothing to resume: the next visit starts over
		}
		return s.nextGroup(ctx)
	}
	if g.Stories[idx].ID != s.storyID {
		return s.show(ctx, g, idx)
	}
	s.order = storyIDs(g)
	return nil
}

// ---- Update ----

func (s *stories) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	fs := s.fs(ctx)
	if fs == nil {
		return nil
	}
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	switch m := msg.(type) {
	case storyTickMsg:
		if m.Gen != 0 && m.Gen == s.gen {
			s.gen = 0
			if s.active && !s.paused {
				cmds = append(cmds, s.next(ctx))
			}
		}
		return tea.Batch(cmds...)
	case ui.FocusChangedMsg:
		cmds = append(cmds, s.onFocus(ctx, m.RouteKey == s.key))
	case ui.ResultMsg[[]api.Story]:
		if m.Tag == tagUserStories+s.userID && s.loading {
			cmds = append(cmds, s.onUserStories(ctx, m))
		}
	case ui.ResultMsg[*storyDeleteOutcome]:
		cmds = append(cmds, s.onDeleted(ctx, m))
	case ui.EventMsg:
		if m.Event.Type == "story.deleted" && s.temp != nil {
			var d struct {
				StoryID string `json:"story_id"`
			}
			if json.Unmarshal(m.Event.Data, &d) == nil && d.StoryID != "" {
				s.dropTemp(func(st api.Story) bool { return st.ID != d.StoryID })
			}
		}
	case ui.ModeChangedMsg, ui.ThemeChangedMsg, ui.LocaleChangedMsg:
		cmds = append(cmds, s.renderCurrent(ctx))
	default:
		if rt := reflect.TypeOf(msg); rt != nil && rt.PkgPath() == widgetsPkg {
			return tea.Batch(append(cmds, s.spin.Update(ctx, msg))...)
		}
	}
	if id, _, ok := s.imgs.loaded(msg); ok {
		if st := s.currentStory(ctx); st != nil && st.Media != nil && st.Media.ID == id {
			cmds = append(cmds, s.renderCurrent(ctx))
		}
	}
	cmds = append(cmds, s.spin.Update(ctx, msg))
	s.prune(ctx)
	cmds = append(cmds, s.settle(ctx))
	if s.imgs.hasWants() {
		cmds = append(cmds, s.imgs.flush(ctx))
	}
	return tea.Batch(cmds...)
}

// onFocus follows the app's focus: leaving stops the timer; coming back resumes
// it, or starts over when the viewer had ended.
func (s *stories) onFocus(ctx *ui.Context, here bool) tea.Cmd {
	if here == s.active {
		return nil
	}
	s.active = here
	if !here {
		s.cancel()
		return nil
	}
	if s.left || (s.storyID == "" && !s.loading) {
		return s.start(ctx)
	}
	if g, idx, ok := s.resolve(ctx); ok {
		return s.show(ctx, g, idx) // seen + POST view if it moved meanwhile; restarts the timer
	}
	return s.settle(ctx)
}

// onUserStories turns the ListUserStories result into the screen-only group.
func (s *stories) onUserStories(ctx *ui.Context, m ui.ResultMsg[[]api.Story]) tea.Cmd {
	s.loading = false
	s.spin.Stop()
	if m.Err != nil {
		err, where := m.Err, cat(ctx).Feed.ErrCtxStories()
		return tea.Batch(func() tea.Msg { return ui.ErrorMsg{Err: err, Context: where} }, s.finish())
	}
	now := ctx.Now()
	var kept []api.Story
	for _, st := range m.Value {
		if st.ExpiresAt.After(now) && !ctx.Store.IsBlocked(st.Author.ID) {
			kept = append(kept, st)
		}
	}
	if len(kept) == 0 {
		return tea.Batch(ui.Toast(cat(ctx).Feed.StoriesNone(), ui.ToastInfo), s.finish())
	}
	last := kept[len(kept)-1]
	s.temp = &api.StoryGroup{User: kept[0].Author, Stories: kept, HasUnseen: anyStoryUnseen(kept), LatestAt: last.CreatedAt}
	return s.showGroup(ctx, s.temp)
}

// onDeleted applies my own story deletion right after the 204 (the
// story.deleted event that follows changes nothing), once however many
// viewers see the result.
func (s *stories) onDeleted(ctx *ui.Context, m ui.ResultMsg[*storyDeleteOutcome]) tea.Cmd {
	o := m.Value
	if !strings.HasPrefix(m.Tag, tagDeleteStory) || o == nil {
		return nil
	}
	if s.temp != nil && m.Err == nil {
		s.dropTemp(func(st api.Story) bool { return st.ID != o.storyID })
	}
	if !o.take() {
		return nil
	}
	if m.Err != nil {
		err, where := m.Err, cat(ctx).Feed.ErrCtxStoryDelete()
		return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: where} }
	}
	if fs := s.fs(ctx); fs != nil {
		fs.RemoveStory(o.storyID)
	}
	return ui.Toast(cat(ctx).Feed.StoryDeleted(), ui.ToastSuccess)
}

// deleteStory is the Cmd behind the menu's delete: DELETE /v1/stories/{id} (tag "feed.delete_story:<id>").
func deleteStory(ctx *ui.Context, storyID string) tea.Cmd {
	o := &storyDeleteOutcome{storyID: storyID}
	return ui.Do(ctx, tagDeleteStory+storyID, func(c context.Context, a *api.Client) (*storyDeleteOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		return o, a.DeleteStory(c, storyID)
	})
}

// currentStory is the story drawn now, or nil.
func (s *stories) currentStory(ctx *ui.Context) *api.Story {
	g, idx, ok := s.resolve(ctx)
	if !ok {
		return nil
	}
	return &g.Stories[idx]
}

// renderCurrent renders the current story's loaded photo for the current box
// (Update only), so a kitty Setup leaves with this Update.
func (s *stories) renderCurrent(ctx *ui.Context) tea.Cmd {
	st := s.currentStory(ctx)
	if st == nil || st.Media == nil {
		return nil
	}
	lay := storyLayoutOf(s.size, st)
	return s.imgs.render(ctx, *st.Media, lay.imgCols, lay.imgRows)
}

// ---- layout & rendering ----

// storyLayout is where a story's parts go in a w x h pane.
type storyLayout struct {
	bar, header      bool // row 0, row 1
	gap              bool // a blank row under the header
	contentY         int  // first content row
	contentH         int
	imgCols, imgRows int // photo box (0 without a photo)
	captionH         int // caption rows under the photo
}

func storyLayoutOf(size ui.Size, st *api.Story) storyLayout {
	var l storyLayout
	h := size.H
	if h >= 1 {
		l.bar = true
		l.contentY++
	}
	if h >= 2 {
		l.header = true
		l.contentY++
	}
	if h >= 8 {
		l.gap = true
		l.contentY++
	}
	l.contentH = max(h-l.contentY, 0)
	if st == nil || st.Media == nil || l.contentH <= 0 || size.W <= 0 {
		return l
	}
	if st.Body != "" && l.contentH >= 4 {
		l.captionH = min(storyCaptionN, max(l.contentH/4, 1))
	}
	l.imgCols, l.imgRows = storyImageBox(*st.Media, size.W, l.contentH-l.captionH)
	return l
}

// storyImageBox is the largest box of the photo's aspect ratio (4:3 when
// unknown; a cell is twice as tall as wide) inside w x h cells.
func storyImageBox(m api.Media, w, h int) (cols, rows int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	iw, ih := int64(4), int64(3)
	if m.Width != nil && m.Height != nil && *m.Width > 0 && *m.Height > 0 {
		iw, ih = int64(*m.Width), int64(*m.Height)
	}
	cols = w
	r := int64(cols) * ih / (iw * 2)
	if r > int64(h) {
		r = int64(h)
		cols = int(min(int64(w), max(r*iw*2/ih, 1)))
	}
	return cols, int(max(r, 1))
}

func (s *stories) View(ctx *ui.Context, _ bool) string {
	th := themeOf(ctx)
	w, h := s.size.W, s.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	s.prune(ctx)
	if s.loading {
		line := fit(th, th.S.Base.Render("  ")+s.spin.View(th)+th.S.Dim.Render(" "+cat(ctx).Feed.Loading()), w)
		return blankRows(th, []string{line}, w, h)
	}
	g, idx, ok := s.resolve(ctx)
	if !ok {
		return strings.Join(textwidth.Fit(strings.Split(widgets.Empty(th, s.size, "", cat(ctx).Feed.StoriesNone(), ""), "\n"), w, h), "\n")
	}
	st := &g.Stories[idx]
	lay := storyLayoutOf(s.size, st)
	var out []string
	if lay.bar {
		out = append(out, storyBar(th, len(g.Stories), idx, w))
	}
	if lay.header {
		out = append(out, s.headerLine(ctx, th, g, idx, w))
	}
	if lay.gap {
		out = append(out, fit(th, "", w))
	}
	if lay.contentH > 0 {
		if st.Media != nil {
			out = append(out, s.photoLines(ctx, th, st, lay, w)...)
		} else {
			out = append(out, storyBox(ctx, th, st, w, lay.contentH)...)
		}
	}
	return blankRows(th, out, w, h)
}

// storyBar is the progress bar: n equal segments, the shown one and those
// before it "━" (acc), the rest "─" (line), one blank between segments when
// they are wide enough.
func storyBar(th *theme.Theme, n, cur, w int) string {
	if n <= 0 {
		return fit(th, "", w)
	}
	done := th.S.Base.Foreground(th.P.Acc)
	rest := th.S.Base.Foreground(th.P.Line)
	gap := w/n >= 3
	var b strings.Builder
	for i := 0; i < n; i++ {
		x0, x1 := i*w/n, (i+1)*w/n
		segW := x1 - x0
		if segW <= 0 {
			continue
		}
		pad := 0
		if gap && i < n-1 {
			pad = 1
		}
		st, ch := rest, "─"
		if i <= cur {
			st, ch = done, "━"
		}
		b.WriteString(st.Render(strings.Repeat(ch, segW-pad)))
		if pad > 0 {
			b.WriteString(th.S.Base.Render(" "))
		}
	}
	return fit(th, b.String(), w)
}

// headerLine is "@hana 하나 · 3분" with "1/2" (and ⏸ while paused) at the right.
func (s *stories) headerLine(ctx *ui.Context, th *theme.Theme, g *api.StoryGroup, idx, w int) string {
	st := g.Stories[idx]
	now := ctx.Now()
	created := st.CreatedAt
	if ctx.Loc != nil {
		now, created = now.In(ctx.Loc), created.In(ctx.Loc)
	}
	self := ctx.Store.IsMe(st.Author.ID)
	left := th.S.Base.Foreground(th.UserColor(st.Author.ID, self)).Bold(true).Render(handleOf(st.Author)) +
		th.S.Base.Render(" "+displayName(st.Author)) +
		th.S.Dim.Render(" · "+hz(ctx).Relative(now, created))
	right := strconv.Itoa(idx+1) + "/" + strconv.Itoa(len(g.Stories))
	if s.paused {
		right = textStoryPaused + " " + right
	}
	rw := textwidth.Width(right) + 1
	if w <= rw+4 {
		return fit(th, left, w)
	}
	return fit(th, left, w-rw) + th.S.Dim.Render(textwidth.PadLeft(right, rw))
}

// boxStyle is the story background box style of bg (the text: the theme
// background on an accent, the foreground on "plain" / chip) and its hashtag
// variant.
func boxStyle(th *theme.Theme, bg string) (base, tag func(string) string) {
	fg := th.P.BG
	switch bg {
	case "accent", "accent2", "accent3", "accent4":
	default:
		fg = th.P.FG
	}
	st := th.S.Base.Background(th.StoryBG(bg)).Foreground(fg).Bold(true)
	ul := st.Underline(true)
	return func(x string) string { return st.Render(x) }, func(x string) string { return ul.Render(x) }
}

// storyText is the story body as wrapped lines of (text, isHashtag) words:
// markup with hashtags, markers dropped, sanitized.
func storyText(body string, w int) [][]storyWord {
	if w <= 0 || body == "" {
		return nil
	}
	tags := map[string]bool{}
	var paras []string
	for _, b := range markup.Parse(body, markup.ParseOptions{Hashtags: true}).Blocks {
		if b.Kind == markup.Code {
			paras = append(paras, strings.Split(b.Code, "\n")...)
			continue
		}
		var p strings.Builder
		for _, in := range b.Inline {
			if in.Kind == markup.Hashtag {
				tags[textwidth.Sanitize(in.Text)] = true
			}
			p.WriteString(in.Text)
		}
		paras = append(paras, strings.Split(p.String(), "\n")...)
	}
	var out [][]storyWord
	for _, p := range paras {
		for _, line := range textwidth.Wrap(textwidth.Sanitize(p), w) {
			var words []storyWord
			for i, word := range strings.Split(line, " ") {
				if i > 0 {
					words = append(words, storyWord{text: " "})
				}
				words = append(words, storyWord{text: word, tag: tags[word]})
			}
			out = append(out, words)
		}
	}
	return out
}

type storyWord struct {
	text string
	tag  bool
}

// storyBox is the text story: the background box filling w x n, the body
// centered both ways (rows that do not fit are cut, the last one ends with "…").
func storyBox(ctx *ui.Context, th *theme.Theme, st *api.Story, w, n int) []string {
	base, tag := boxStyle(th, st.BG)
	text := storyText(st.Body, max(w-4, 1))
	if len(text) > n {
		text = text[:n]
		if n > 0 {
			last := text[n-1]
			text[n-1] = append(last[:len(last):len(last)], storyWord{text: "…"})
		}
	}
	top := (n - len(text)) / 2
	out := make([]string, n)
	for i := range out {
		j := i - top
		if j < 0 || j >= len(text) {
			out[i] = base(strings.Repeat(" ", w))
			continue
		}
		out[i] = centerWords(text[j], w, base, tag)
	}
	return out
}

// centerWords draws words centered in exactly w cells on the box background.
func centerWords(words []storyWord, w int, base, tag func(string) string) string {
	var plain strings.Builder
	for _, wd := range words {
		plain.WriteString(wd.text)
	}
	tw := textwidth.Width(plain.String())
	if tw > w {
		return base(textwidth.Truncate(plain.String(), w, "…")) + base(strings.Repeat(" ", max(w-textwidth.Width(textwidth.Truncate(plain.String(), w, "…")), 0)))
	}
	left := (w - tw) / 2
	var b strings.Builder
	b.WriteString(base(strings.Repeat(" ", left)))
	for _, wd := range words {
		if wd.tag {
			b.WriteString(tag(wd.text))
		} else {
			b.WriteString(base(wd.text))
		}
	}
	b.WriteString(base(strings.Repeat(" ", w-left-tw)))
	return b.String()
}

// photoLines is the photo story: the image centered in its box (the background
// box with "사진" until it arrives), then the caption.
func (s *stories) photoLines(ctx *ui.Context, th *theme.Theme, st *api.Story, lay storyLayout, w int) []string {
	imgH := lay.contentH - lay.captionH
	var out []string
	img := s.imgs.cell(ctx, *st.Media, lay.imgCols, lay.imgRows)
	if img == nil {
		ph := *st
		ph.Body = cat(ctx).Feed.StoryPhoto()
		out = append(out, storyBox(ctx, th, &ph, w, imgH)...)
	} else {
		top := (imgH - len(img)) / 2
		left := (w - lay.imgCols) / 2
		pad := th.S.Base.Render(strings.Repeat(" ", max(left, 0)))
		for i := 0; i < imgH; i++ {
			j := i - top
			if j < 0 || j >= len(img) {
				out = append(out, fit(th, "", w))
				continue
			}
			out = append(out, fit(th, pad+img[j], w))
		}
	}
	if lay.captionH > 0 {
		doc := markup.Parse(st.Body, markup.ParseOptions{Hashtags: true})
		lines := markup.Render(doc, th, markup.RenderOptions{Width: max(w-4, 1), MaxLines: lay.captionH, MoreTail: cat(ctx).Common.MoreTail()})
		for i := 0; i < lay.captionH; i++ {
			l := ""
			if i < len(lines) {
				l = lines[i]
			}
			out = append(out, fit(th, th.S.Base.Render("  ")+l, w))
		}
	}
	return out
}

func (s *stories) Resize(ctx *ui.Context, size ui.Size) { s.size = size }

// ---- input ----

func (s *stories) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	resume := func() tea.Cmd {
		if !s.paused {
			return nil
		}
		s.paused = false
		return s.arm(ctx)
	}
	switch str := msg.String(); {
	case str == "space" || str == "right" || str == "n":
		s.paused = false
		return true, s.next(ctx)
	case str == "left" || str == "p":
		s.paused = false
		return true, s.prev(ctx)
	case km.Matches(msg, keymap.MsgActions):
		return true, s.openMenu(ctx)
	case str == "esc":
		s.left = true // the app goes back; the next visit starts over
		s.cancel()
		return false, nil
	}
	return false, resume()
}

func (s *stories) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft || s.size.W <= 0 {
		return nil
	}
	s.paused = false
	if local.X < s.size.W/3 {
		return s.prev(ctx)
	}
	return s.next(ctx)
}

// openMenu is "m": 본 사람 · 삭제 on my story, 신고 · 작성자 프로필 on someone
// else's. The timer waits while the menu is open (until the next key).
func (s *stories) openMenu(ctx *ui.Context) tea.Cmd {
	st := s.currentStory(ctx)
	if st == nil {
		return nil
	}
	id, author := st.ID, st.Author
	var items []menuItem
	c := cat(ctx)
	if ctx.Store.IsMe(author.ID) {
		label := c.Feed.StoryViewers()
		if st.ViewCount != nil {
			label = c.Feed.StoryViewersN(hz(ctx).Count(*st.ViewCount))
		}
		items = append(items,
			menuItem{label: label, open: func(ctx *ui.Context) ui.Overlay {
				return overlayOrNil(newUserList(ctx, listStoryViewers+" "+id))
			}},
			menuItem{label: c.Feed.StoryDelete(), open: func(ctx *ui.Context) ui.Overlay {
				c := cat(ctx)
				return widgets.Confirm(c.Feed.StoryDeleteAsk(), "", c.Feed.StoryDelete(), c.Feed.Cancel(), deleteStory(ctx, id))
			}},
		)
	} else {
		name := displayName(author)
		items = append(items,
			menuItem{label: c.Feed.StoryReport(), open: func(ctx *ui.Context) ui.Overlay {
				return widgets.ReportDialog("story", id, cat(ctx).Feed.StoryOf(name))
			}},
			menuItem{label: cat(ctx).Feed.MenuAuthor(), run: func(*ui.Context) tea.Cmd {
				return ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: author.ID})
			}},
		)
	}
	s.paused = true
	s.cancel()
	return ui.OpenOverlay(&menu{title: c.Feed.StoriesTitle(), width: menuW, items: items})
}

// ---- optional capabilities ----

func (s *stories) Title(ctx *ui.Context) (string, ui.Badge) {
	return cat(ctx).Feed.StoriesTitle(), ui.Badge{}
}

func (s *stories) Help(ctx *ui.Context) []key.Binding {
	c := cat(ctx)
	b := ctx.Keys.Binding(keymap.MsgActions)
	b.SetHelp(b.Help().Key, c.Feed.HelpMenu())
	return []key.Binding{
		key.NewBinding(key.WithKeys("space", "right", "n"), key.WithHelp("space", c.Feed.HelpNext())),
		key.NewBinding(key.WithKeys("left", "p"), key.WithHelp("←", c.Feed.HelpPrev())),
		b,
	}
}
