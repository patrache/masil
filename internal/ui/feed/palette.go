package feed

import (
	"github.com/patrache/masil/internal/i18n"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// Palette commands of ui/feed (CA 5.6 sns-fun ui/feed row) and the palette
// "#tag" source. Every command runs through the app's ui.RunCommandMsg
// dispatch (CR-B); Run only navigates or opens a registered overlay.

// Palette groups (palette.Command.Group).
const (
	groupGo      = palette.GroupNav
	groupCommand = palette.GroupCommand
	groupSetting = palette.GroupSettings
)

// Hashtag source scores: a Top 10 trend outranks the "open this tag" item
// (rank 1 highest); the open item keeps score 0 (cross-track note 11).
const (
	scoreTrendBase = 20 // trend rank r scores scoreTrendBase - r
	scoreOpenTag   = 0
)

func init() {
	nav := func(r ui.Route) func(*ui.Context, string) tea.Cmd {
		return func(*ui.Context, string) tea.Cmd { return ui.Navigate(r) }
	}
	open := func(id string) func(*ui.Context, string) tea.Cmd {
		return func(*ui.Context, string) tea.Cmd { return ui.OpenRegisteredOverlay(id, "") }
	}
	hasMe := func(ctx *ui.Context) bool { return ctx != nil && ctx.Me() != nil }

	palette.Register(palette.Command{
		ID: "post.new", Label: func(t *i18n.T) string { return t.Feed.CmdPostNew() }, Group: groupCommand, KeyHint: "c",
		Keywords:   []string{"post", "new", "write", "compose"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwPostNew()) },
		Run:        nav(ui.Route{Kind: ui.RouteCompose, Arg: modePost}),
	})
	palette.Register(palette.Command{
		ID: "story.new", Label: func(t *i18n.T) string { return t.Feed.CmdStoryNew() }, Group: groupCommand,
		Keywords:   []string{"story", "new"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwStoryNew()) },
		Run:        nav(ui.Route{Kind: ui.RouteCompose, Arg: modeStory}),
	})
	palette.Register(palette.Command{
		ID: "feed.following", Label: func(t *i18n.T) string { return t.Feed.CmdFeedFollowing() }, Group: groupGo, KeyHint: "f",
		Keywords:   []string{"feed", "following", "timeline"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwFeedFollowing()) },
		Run:        nav(ui.Route{Kind: ui.RouteFeed, Arg: "following"}),
	})
	palette.Register(palette.Command{
		ID: "feed.discover", Label: func(t *i18n.T) string { return t.Feed.CmdFeedDiscover() }, Group: groupGo,
		Keywords:   []string{"feed", "discover", "explore", "all"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwFeedDiscover()) },
		Run:        nav(ui.Route{Kind: ui.RouteFeed, Arg: "discover"}),
	})
	palette.Register(palette.Command{
		ID: "trends", Label: func(t *i18n.T) string { return t.Feed.CmdTrends() }, Group: groupGo,
		Keywords:   []string{"trends", "hashtag", "search"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwTrends()) },
		Run:        nav(ui.Route{Kind: ui.RouteTrends}),
	})
	palette.Register(palette.Command{
		ID: "notifications", Label: func(t *i18n.T) string { return t.Feed.CmdNotifications() }, Group: groupGo, KeyHint: "ctrl+n",
		Keywords:   []string{"notifications", "inbox"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwNotifications()) },
		Run:        nav(ui.Route{Kind: ui.RouteNotifications}),
		Badge:      inboxBadge,
	})
	palette.Register(palette.Command{
		ID: "profile.me", Label: func(t *i18n.T) string { return t.Feed.CmdProfileMe() }, Group: groupGo,
		Keywords:   []string{"profile", "me"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwProfileMe()) },
		Available:  hasMe,
		Run: func(ctx *ui.Context, _ string) tea.Cmd {
			if !hasMe(ctx) {
				return nil
			}
			return ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: ctx.Me().ID})
		},
	})
	palette.Register(palette.Command{
		ID: "profile.edit", Label: func(t *i18n.T) string { return t.Feed.CmdProfileEdit() }, Group: groupSetting,
		Keywords:   []string{"profile", "edit", "bio", "avatar"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwProfileEdit()) },
		Available:  hasMe,
		Run:        open(overlayProfileEdit),
	})
	palette.Register(palette.Command{
		ID: "follow_requests", Label: func(t *i18n.T) string { return t.Feed.CmdFollowRequests() }, Group: groupCommand,
		Keywords:   []string{"follow", "requests", "private"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Feed.KwFollowRequests()) },
		Run:        open(overlayFollowRequests),
	})
	palette.RegisterSource(hashtagSource{})
}

// hashtagSource is the palette "#tag" source: for "#<rest>" (rest 1..50 code
// points) the cached Top 10 trends whose tag starts with rest (no network:
// Items is synchronous), then one item opening the rest's tag feed unless a
// trend is exactly that tag. Other queries yield nothing. Server prefix
// suggestions (SearchHashtags) belong to the trends screen's "/" search.
type hashtagSource struct{}

var _ palette.Source = hashtagSource{}

// Items implements palette.Source.
func (hashtagSource) Items(ctx *ui.Context, query string) []palette.Item {
	rest, ok := strings.CutPrefix(strings.TrimSpace(query), "#")
	if !ok {
		return nil
	}
	if n := utf8.RuneCountInString(rest); n < 1 || n > tagMaxRunes {
		return nil
	}
	var out []palette.Item
	exact := false
	if ctx != nil && ctx.Store != nil {
		if fs := feedstore.Get(ctx.Store); fs != nil && fs.Trends() != nil {
			lower := strings.ToLower(rest)
			for _, tr := range fs.Trends().Items {
				if !strings.HasPrefix(strings.ToLower(tr.Tag), lower) {
					continue
				}
				exact = exact || strings.EqualFold(tr.Tag, rest)
				out = append(out, tagItem(tr.Tag, cat(ctx).Feed.PaletteTrendRank(tr.Rank), scoreTrendBase-tr.Rank))
			}
		}
	}
	if !exact {
		out = append(out, tagItem(rest, cat(ctx).Feed.PaletteHashtagOpen(), scoreOpenTag))
	}
	return out
}

// tagItem is a route item opening RouteHashtag{ID: tag}.
func tagItem(tag, subtitle string, score int) palette.Item {
	return palette.Item{
		Kind:     palette.ItemRoute,
		Title:    "#" + tag,
		Subtitle: subtitle,
		Route:    &ui.Route{Kind: ui.RouteHashtag, ID: tag},
		Score:    score,
	}
}
