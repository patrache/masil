package palette

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"github.com/sahilm/fuzzy"

	"github.com/patrache/masil/internal/api"
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

// The core default sources (CA 5.6): channels, DMs, spaces, friends, commands.
func init() {
	RegisterSource(channelSource{})
	RegisterSource(dmSource{})
	RegisterSource(spaceSource{})
	RegisterSource(friendSource{})
	RegisterSource(commandSource{})
}

// Group headers.
const (
	groupGo      = "이동"
	groupCommand = "명령"
	groupSetting = "설정"
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
// channels then the recently chosen routes; otherwise every source's items plus the
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

// emptyItems: unread channels (spaces in order, then DMs), then the recent routes.
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
		if seen[itemKey(it)] {
			continue
		}
		seen[itemKey(it)] = true
		out = append(out, refresh(ctx, it))
	}
	return out
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

// dmItems lists the DMs and group DMs (DMOrder, then any other DM channel by id).
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
		if ch == nil || (ch.Kind != api.ChannelDM && ch.Kind != api.ChannelGroupDM) {
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
	if name == "" {
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
	mu.Lock()
	cs := cloneCommands(commands)
	mu.Unlock()
	var out []Item
	for i := range cs {
		c := cs[i]
		if c.Available != nil && !c.Available(ctx) {
			continue
		}
		s, ok := matchTitle(q, c.Title, append([]string{c.ID}, c.Keywords...)...)
		if !ok {
			continue
		}
		out = append(out, Item{Kind: ItemCommand, Title: c.Title, Command: &c, Score: s})
	}
	return out
}
