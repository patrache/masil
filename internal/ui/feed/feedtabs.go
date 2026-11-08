package feed

import (
	"strings"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Feed surface tabs (mockup ① copy): 피드 = discover, 팔로잉 = following, #트렌드 = RouteTrends.
const (
	tabDiscover  = "discover"
	tabFollowing = "following"
	tabTrends    = "trends"
)

// feedTabRoutes is the tab order and where each tab goes.
var feedTabRoutes = []struct {
	key, label string
	route      ui.Route
}{
	{tabDiscover, "피드", ui.Route{Kind: ui.RouteFeed, Arg: tabDiscover}},
	{tabFollowing, "팔로잉", ui.Route{Kind: ui.RouteFeed, Arg: tabFollowing}},
	{tabTrends, "#트렌드", ui.Route{Kind: ui.RouteTrends}},
}

func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}

// feedTabs is the feed surface's top tab line ("피드 / 팔로잉 / #트렌드") with
// active ("discover" | "following" | "trends") highlighted, exactly width cells,
// and a hit test mapping a column to the tab's route.
func feedTabs(ctx *ui.Context, active string, width int) (line string, hit func(x int) (ui.Route, bool)) {
	tabs := widgets.NewTabs()
	items := make([]widgets.Tab, len(feedTabRoutes))
	idx := 0
	for i, r := range feedTabRoutes {
		items[i] = widgets.Tab{Label: r.label}
		if r.key == active {
			idx = i
		}
	}
	tabs.Set(items, idx)
	line = tabs.View(themeOf(ctx), width)
	return line, func(x int) (ui.Route, bool) {
		i, ok := tabs.Hit(x)
		if !ok || i < 0 || i >= len(feedTabRoutes) {
			return ui.Route{}, false
		}
		return feedTabRoutes[i].route, true
	}
}

// Story ring copy and limits.
const (
	ringMine     = "내 스토리"
	ringAdd      = "+ " + ringMine
	ringUnseen   = "◉"
	ringSeen     = "○"
	ringNameMax  = 8 // cells per name
	ringGap      = 2 // blanks between entries
	ringLeftEdge = gutterW
)

// storyRing is the one-line story ring: my group first ("◉/○ 내 스토리", or
// "+ 내 스토리" when I have none), then the other groups in server order, unseen
// "◉" (acc) and seen "○" (dim). Entries that do not fit are dropped whole. hit
// maps a column to the group's user id; the "+ 내 스토리" entry yields ("", true),
// meaning "write a story".
func storyRing(ctx *ui.Context, groups []api.StoryGroup, width int) (lines []string, hit func(x int) (userID string, ok bool)) {
	t := themeOf(ctx)
	type span struct {
		x0, x1 int
		id     string
	}
	var spans []span
	hit = func(x int) (string, bool) {
		for _, s := range spans {
			if x >= s.x0 && x < s.x1 {
				return s.id, true
			}
		}
		return "", false
	}
	if width <= 0 {
		return nil, hit
	}

	type entry struct {
		id, text, mark string
		unseen         bool
	}
	var mine *api.StoryGroup
	var others []api.StoryGroup
	for i := range groups {
		if ctx != nil && ctx.Store != nil && ctx.Store.IsMe(groups[i].User.ID) && mine == nil {
			mine = &groups[i]
			continue
		}
		others = append(others, groups[i])
	}
	var entries []entry
	if mine != nil {
		entries = append(entries, entry{id: mine.User.ID, text: ringMine, mark: markOf(mine.HasUnseen), unseen: mine.HasUnseen})
	} else {
		entries = append(entries, entry{id: "", text: ringAdd})
	}
	for _, g := range others {
		name := displayName(g.User)
		if textwidth.Width(name) > ringNameMax {
			name = textwidth.Truncate(name, ringNameMax, "…")
		}
		entries = append(entries, entry{id: g.User.ID, text: name, mark: markOf(g.HasUnseen), unseen: g.HasUnseen})
	}

	var b strings.Builder
	x := min(ringLeftEdge, width)
	b.WriteString(t.S.Base.Render(strings.Repeat(" ", x)))
	for i, e := range entries {
		w := textwidth.Width(e.text)
		if e.mark != "" {
			w += textwidth.Width(e.mark) + 1
		}
		gap := 0
		if i > 0 {
			gap = ringGap
		}
		if x+gap+w > width {
			break
		}
		b.WriteString(t.S.Base.Render(strings.Repeat(" ", gap)))
		x += gap
		if e.mark != "" {
			st := t.S.Dim
			if e.unseen {
				st = t.S.Base.Foreground(t.P.Acc).Bold(true)
			}
			b.WriteString(st.Render(e.mark) + t.S.Base.Render(" "))
		}
		label := t.S.Base.Render(e.text)
		if e.id == "" {
			label = t.S.Dim.Render(e.text)
		}
		b.WriteString(label)
		spans = append(spans, span{x0: x, x1: x + w, id: e.id})
		x += w
	}
	return []string{fit(t, b.String(), width)}, hit
}

func markOf(unseen bool) string {
	if unseen {
		return ringUnseen
	}
	return ringSeen
}
