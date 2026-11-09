package palette

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/sahilm/fuzzy"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// ItemKind classifies palette items.
type ItemKind int

// Item kinds.
const (
	ItemChannel ItemKind = iota
	ItemDM
	ItemSpace
	ItemPerson
	ItemCommand
	ItemRoute
)

// Item is one palette row.
type Item struct {
	Kind     ItemKind
	Title    string
	Subtitle string
	Badge    ui.Badge
	Route    *ui.Route
	Command  *Command
	Score    int
}

// Source contributes palette items. Items is called synchronously on the UI goroutine
// for a non-empty query (no network); a source filters by query itself and sets Score
// (higher ranks first).
type Source interface {
	Items(ctx *ui.Context, query string) []Item
}

// RegisterSource adds a source (init()-time).
func RegisterSource(s Source) {
	mu.Lock()
	defer mu.Unlock()
	sources = append(sources, s)
}

// The core default sources (CA 5.6): channels, DMs, spaces, friends, commands,
// plus 홈 (widget, tiny and compact have no rail or sidebar that leads back to it).
func init() {
	RegisterSource(homeSource{})
	RegisterSource(channelSource{})
	RegisterSource(dmSource{})
	RegisterSource(spaceSource{})
	RegisterSource(friendSource{})
	RegisterSource(commandSource{})
}

// Group ids (headers: GroupTitle).
const (
	groupGo      = GroupNav
	groupCommand = GroupCommand
	groupSetting = GroupSettings
)

// maxRecent is how many chosen routes the empty query lists.
const maxRecent = 8

// noMatchScore ranks a server-found person whose fields do not fuzzy-match the query.
const noMatchScore = -1 << 20

// kindRank orders equal scores: Channel, DM, Space, Person, Route, Command.
var kindRank = map[ItemKind]int{
	ItemChannel: 0, ItemDM: 1, ItemSpace: 2, ItemPerson: 3, ItemRoute: 4, ItemCommand: 5,
}

// match is the best sahilm/fuzzy score of query over the candidates (empty ones
// skipped); ok is false when none matches.
func match(query string, cands ...string) (score int, ok bool) {
	cs := slices.DeleteFunc(slices.Clone(cands), func(s string) bool { return s == "" })
	ms := fuzzy.Find(query, cs)
	if len(ms) == 0 {
		return 0, false
	}
	best := ms[0].Score
	for _, m := range ms[1:] {
		best = max(best, m.Score)
	}
	return best, true
}

// matchTitle matches Title, Chosung(Title) and the keywords.
func matchTitle(query, title string, keywords ...string) (int, bool) {
	return match(query, append([]string{title, Chosung(title)}, keywords...)...)
}

// minWordPrefix is the shortest query that matches English keywords and command
// ids: a single Latin letter starts a word of most commands.
const minWordPrefix = 2

func isIDSep(r rune) bool { return r == '.' || r == '_' || r == '-' }

// idWords splits a command id into its words ("theme.set.retro_amber" => theme,
// set, retro, amber) followed by its tails from each word boundary, separators
// kept (set.retro_amber, retro_amber, amber), so "pin.command" finds
// layout.pin.command as fuzzy over the whole id did.
func idWords(id string) []string {
	out := strings.FieldsFunc(id, isIDSep)
	for i, r := range id {
		if isIDSep(r) && i+1 < len(id) && !isIDSep(rune(id[i+1])) {
			out = append(out, id[i+1:])
		}
	}
	return out
}

func hasHangul(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) })
}

// matchCommand matches a command (or a fixed route such as 홈): Title, Chosung(Title)
// and Hangul keywords fuzzily (sahilm/fuzzy); English keywords and the id only when
// the lowercased query (2+ runes) prefixes the whole keyword/id or one of its words
// (keywords split on spaces, ids on '.', '_', '-', plus the id's tails from a word
// boundary — idWords), scored by fuzzy over those words.
// Subsequence matching over English words made "a" list nearly every command.
func matchCommand(query, title, id string, keywords ...string) (int, bool) {
	cands := []string{title, Chosung(title)}
	lq := strings.ToLower(query)
	prefix := utf8.RuneCountInString(lq) >= minWordPrefix
	words := func(whole string, parts []string) {
		if !prefix {
			return
		}
		for _, w := range append([]string{whole}, parts...) {
			if strings.HasPrefix(strings.ToLower(w), lq) {
				cands = append(cands, w)
			}
		}
	}
	if id != "" {
		words(id, idWords(id))
	}
	for _, k := range keywords {
		if hasHangul(k) {
			cands = append(cands, k)
			continue
		}
		words(k, strings.Fields(k))
	}
	return match(query, cands...)
}

// itemKey identifies an item for de-duplication and selection keeping.
func itemKey(it Item) string {
	switch {
	case it.Command != nil:
		return "cmd:" + it.Command.ID
	case it.Route != nil:
		return "route:" + it.Route.Key()
	}
	return "title:" + it.Title
}

// groupOf is the header an item is listed under.
func groupOf(it Item) string {
	if it.Kind == ItemCommand {
		if it.Command != nil && it.Command.Group != "" {
			return it.Command.Group
		}
		return groupCommand
	}
	return groupGo
}

// collect returns the ranked items for query: an empty query lists the unread
// channels, the recently chosen routes, 홈, then every available command; otherwise every source's items plus the
// people-search results (people found for peopleQ), de-duplicated (best score
// kept) and sorted by score, kind, title.
func collect(ctx *ui.Context, query string, people []api.UserSummary, peopleQ string) []Item {
	q := strings.TrimSpace(query)
	if q == "" {
		return emptyItems(ctx)
	}
	mu.Lock()
	srcs := slices.Clone(sources)
	mu.Unlock()

	var all []Item
	for _, s := range srcs {
		all = append(all, s.Items(ctx, q)...)
	}
	all = append(all, peopleItems(ctx, q, people, peopleQ)...)

	best := map[string]int{} // key -> index in out
	var out []Item
	for _, it := range all {
		k := itemKey(it)
		if i, ok := best[k]; ok {
			if it.Score > out[i].Score {
				out[i] = it
			}
			continue
		}
		best[k] = len(out)
		out = append(out, it)
	}
	slices.SortStableFunc(out, func(a, b Item) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		if c := cmp.Compare(kindRank[a.Kind], kindRank[b.Kind]); c != 0 {
			return c
		}
		return cmp.Compare(a.Title, b.Title)
	})
	return out
}

// emptyItems: unread channels (spaces in order, then DMs), then the recent routes,
// then 홈 (unless it is a recent route), then every available command (commandItems)
// so the commands can be found without knowing their names.
func emptyItems(ctx *ui.Context) []Item {
	var out []Item
	seen := map[string]bool{}
	for _, it := range append(channelItems(ctx), dmItems(ctx)...) {
		if it.Badge.Count > 0 || it.Badge.Mention {
			seen[itemKey(it)] = true
			out = append(out, it)
		}
	}
	recentMu.Lock()
	rs := slices.Clone(recent)
	recentMu.Unlock()
	n := 0
	for _, it := range rs {
		if n == maxRecent {
			break
		}
		n++
		if seen[itemKey(it)] || hiddenDMRoute(ctx, it) {
			continue
		}
		seen[itemKey(it)] = true
		out = append(out, refresh(ctx, it))
	}
	if home := homeItem(); !seen[itemKey(home)] {
		out = append(out, home)
	}
	return append(out, commandItems(ctx)...)
}

// hiddenDMRoute: it routes to a 1:1 DM with a blocked peer (Store.HiddenDM), e.g. a
// recent route chosen before the block.
func hiddenDMRoute(ctx *ui.Context, it Item) bool {
	if ctx == nil || ctx.Store == nil || it.Route == nil || it.Route.Kind != ui.RouteChannel {
		return false
	}
	return ctx.Store.HiddenDM(ctx.Store.Channels[it.Route.ID])
}

// groupRank orders the command groups of the empty query (the overlay lists the
// groups in first-item order): 이동 right after 홈, then 명령, 설정, any other.
var groupRank = map[string]int{groupGo: 0, groupCommand: 1, groupSetting: 2}

// commandItems are the available commands by group (groupRank, then the group name)
// then title (the non-empty query's tie order) — stable and deterministic.
func commandItems(ctx *ui.Context) []Item {
	var out []Item
	for _, c := range availableCommands(ctx) {
		out = append(out, commandItem(ctx, c, 0))
	}
	rank := func(it Item) int {
		if r, ok := groupRank[groupOf(it)]; ok {
			return r
		}
		return len(groupRank)
	}
	slices.SortStableFunc(out, func(a, b Item) int {
		if c := cmp.Compare(rank(a), rank(b)); c != 0 {
			return c
		}
		if c := cmp.Compare(groupOf(a), groupOf(b)); c != 0 {
			return c
		}
		return cmp.Compare(a.Title, b.Title)
	})
	return out
}

// availableCommands copies the registered commands whose Available(ctx) holds.
func availableCommands(ctx *ui.Context) []Command {
	mu.Lock()
	cs := cloneCommands(commands)
	mu.Unlock()
	return slices.DeleteFunc(cs, func(c Command) bool { return c.Available != nil && !c.Available(ctx) })
}

// refresh updates a remembered channel/DM item's badge from the Store.
func refresh(ctx *ui.Context, it Item) Item {
	if it.Route == nil || it.Route.Kind != ui.RouteChannel || ctx == nil || ctx.Store == nil {
		return it
	}
	if ch := ctx.Store.Channels[it.Route.ID]; ch != nil && (it.Kind == ItemChannel || it.Kind == ItemDM) {
		it.Badge = channelBadge(ch)
	}
	return it
}

// ---- recently chosen routes (package memory, newest first) ----

var (
	recentMu sync.Mutex
	recent   []Item
)

// remember records a route item chosen in the palette (newest first, no duplicates).
func remember(it Item) {
	if it.Route == nil {
		return
	}
	it.Score = 0
	r := *it.Route
	it.Route = &r
	k := itemKey(it)
	recentMu.Lock()
	defer recentMu.Unlock()
	recent = slices.DeleteFunc(recent, func(o Item) bool { return itemKey(o) == k })
	recent = append([]Item{it}, recent...)
	if len(recent) > maxRecent {
		recent = recent[:maxRecent]
	}
}

// ---- built-in sources ----

// Home item copy.
const (
	homeTitle    = "홈"
	homeSubtitle = "방·DM·친구·피드"
)

// homeItem is the 홈 route (spec 4 레일/탭의 홈; now also the joined spaces).
func homeItem() Item {
	return Item{Kind: ItemRoute, Title: homeTitle, Subtitle: homeSubtitle, Route: &ui.Route{Kind: ui.RouteHome}}
}

type homeSource struct{}

func (homeSource) Items(_ *ui.Context, q string) []Item {
	it := homeItem()
	s, ok := matchCommand(q, it.Title, "", "home")
	if !ok {
		return nil
	}
	it.Score = s
	return []Item{it}
}

func channelBadge(ch *api.Channel) ui.Badge {
	return ui.Badge{Count: ch.My.UnreadCount, Mention: ch.My.MentionCount > 0}
}

// channelItems lists the text channels of every joined space, in space order.
func channelItems(ctx *ui.Context) []Item {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	st := ctx.Store
	var out []Item
	for _, sid := range st.SpaceOrder {
		sp := st.Spaces[sid]
		if sp == nil {
			continue
		}
		for _, ch := range st.SpaceChannels(sid) {
			if ch.Kind != api.ChannelText {
				continue
			}
			out = append(out, Item{
				Kind:     ItemChannel,
				Title:    st.ChannelLabel(ch.ID),
				Subtitle: sp.Name,
				Badge:    channelBadge(ch),
				Route:    &ui.Route{Kind: ui.RouteChannel, ID: ch.ID},
			})
		}
	}
	return out
}

// dmItems lists the DMs and group DMs (DMOrder, then any other DM channel by id),
// leaving out a 1:1 DM with a blocked peer (Store.HiddenDM), as the DM list does.
func dmItems(ctx *ui.Context) []Item {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	st := ctx.Store
	ids := slices.Clone(st.DMOrder)
	var rest []string
	for id, ch := range st.Channels {
		if (ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM) && !slices.Contains(ids, id) {
			rest = append(rest, id)
		}
	}
	slices.Sort(rest)
	var out []Item
	for _, id := range append(ids, rest...) {
		ch := st.Channels[id]
		if ch == nil || (ch.Kind != api.ChannelDM && ch.Kind != api.ChannelGroupDM) || st.HiddenDM(ch) {
			continue
		}
		sub := "DM"
		if ch.Kind == api.ChannelGroupDM {
			sub = "그룹 DM"
		}
		out = append(out, Item{
			Kind:     ItemDM,
			Title:    st.ChannelLabel(id),
			Subtitle: sub,
			Badge:    channelBadge(ch),
			Route:    &ui.Route{Kind: ui.RouteChannel, ID: id},
		})
	}
	return out
}

// dmKeywords are the handles of a DM's other members ("minji", "@minji").
func dmKeywords(ctx *ui.Context, id string) []string {
	ch := ctx.Store.Channels[id]
	if ch == nil {
		return nil
	}
	var out []string
	for _, m := range ch.Members {
		if !ctx.Store.IsMe(m.ID) && m.Handle != "" {
			out = append(out, m.Handle, "@"+m.Handle)
		}
	}
	return out
}

type channelSource struct{}

func (channelSource) Items(ctx *ui.Context, q string) []Item {
	var out []Item
	for _, it := range channelItems(ctx) {
		name := strings.TrimPrefix(it.Title, "#")
		if s, ok := matchTitle(q, it.Title, name, Chosung(name)); ok {
			it.Score = s
			out = append(out, it)
		}
	}
	return out
}

type dmSource struct{}

func (dmSource) Items(ctx *ui.Context, q string) []Item {
	var out []Item
	for _, it := range dmItems(ctx) {
		s, ok := matchTitle(q, it.Title)
		if !ok {
			// A handle-only match ranks just below the person (profile) item it ties with.
			if s, ok = match(q, dmKeywords(ctx, it.Route.ID)...); ok {
				s--
			}
		}
		if ok {
			it.Score = s
			out = append(out, it)
		}
	}
	return out
}

type spaceSource struct{}

func (spaceSource) Items(ctx *ui.Context, q string) []Item {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	st := ctx.Store
	var out []Item
	for _, sid := range st.SpaceOrder {
		sp := st.Spaces[sid]
		if sp == nil || sp.DefaultChannelID == "" {
			continue
		}
		title := "◆ " + sp.Name
		s, ok := matchTitle(q, title, sp.Name, Chosung(sp.Name))
		if !ok {
			continue
		}
		out = append(out, Item{
			Kind:     ItemSpace,
			Title:    title,
			Subtitle: "방",
			Route:    &ui.Route{Kind: ui.RouteChannel, ID: sp.DefaultChannelID},
			Score:    s,
		})
	}
	return out
}

// personItem is a person row (-> RouteProfile{ID: "@handle"}).
func personItem(u api.UserSummary) Item {
	name := u.DisplayName
	if textwidth.Blank(name) { // nothing visible (pending-fixes 1)
		name = u.Handle
	}
	return Item{
		Kind:     ItemPerson,
		Title:    name,
		Subtitle: "@" + u.Handle,
		Route:    &ui.Route{Kind: ui.RouteProfile, ID: "@" + u.Handle},
	}
}

func personMatch(q string, u api.UserSummary) (int, bool) {
	return matchTitle(q, personItem(u).Title, u.Handle, "@"+u.Handle)
}

type friendSource struct{}

func (friendSource) Items(ctx *ui.Context, q string) []Item {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	var out []Item
	for _, f := range ctx.Store.Friends {
		if f.User.Handle == "" || ctx.Store.IsBlocked(f.User.ID) {
			continue
		}
		if s, ok := personMatch(q, f.User); ok {
			it := personItem(f.User)
			it.Score = s
			out = append(out, it)
		}
	}
	return out
}

// peopleItems are the people-search results. They are kept across edits but listed
// only while they still fuzzy-match the current query, or when they were found for
// exactly it (the server may match on more than the fuzzy fields).
func peopleItems(ctx *ui.Context, q string, people []api.UserSummary, peopleQ string) []Item {
	var out []Item
	for _, u := range people {
		if u.Handle == "" || (ctx != nil && ctx.Store != nil && (ctx.Store.IsMe(u.ID) || ctx.Store.IsBlocked(u.ID))) {
			continue
		}
		s, ok := personMatch(q, u)
		if !ok {
			if strings.TrimSpace(peopleQ) != q {
				continue
			}
			s = noMatchScore
		}
		it := personItem(u)
		it.Score = s
		out = append(out, it)
	}
	return out
}

type commandSource struct{}

func (commandSource) Items(ctx *ui.Context, q string) []Item {
	var out []Item
	for _, c := range availableCommands(ctx) {
		s, ok := matchCommand(q, c.TitleIn(ctxT(ctx)), c.ID, commandKeywords(c)...)
		if !ok {
			continue
		}
		out = append(out, commandItem(ctx, c, s))
	}
	return out
}

// commandItem is c's palette item with its Badge (the 알림함's unread count) drawn,
// for a query's match and the empty query's command list alike.
func commandItem(ctx *ui.Context, c Command, score int) Item {
	it := Item{Kind: ItemCommand, Title: c.TitleIn(ctxT(ctx)), Command: &c, Score: score}
	if c.Badge != nil && ctx != nil {
		it.Badge = c.Badge(ctx)
	}
	return it
}

// ctxT is the screen's catalog (nil without a Context: ko).
func ctxT(ctx *ui.Context) *i18n.T {
	if ctx == nil {
		return nil
	}
	return ctx.T
}

// commandKeywords are c's keywords plus its English label, so a command is
// found by its English name on any screen (i18n spec 6.4.4).
func commandKeywords(c Command) []string {
	if c.Label == nil {
		return c.Keywords
	}
	return append(slices.Clone(c.Keywords), c.Label(i18n.For(i18n.En)))
}
