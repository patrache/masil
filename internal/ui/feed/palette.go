package feed

import (
	"fmt"
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

// Palette copy.
const (
	textHashtagOpen  = "태그 피드 열기"
	textTrendRankFmt = "트렌드 %d위"
)

// Palette groups (palette.Command.Group).
const (
	groupGo      = "이동"
	groupCommand = "명령"
	groupSetting = "설정"
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
		ID: "post.new", Title: "새 글 쓰기", Group: groupCommand, KeyHint: "c",
		Keywords: []string{"post", "new", "write", "compose", "글쓰기", "작성"},
		Run:      nav(ui.Route{Kind: ui.RouteCompose, Arg: modePost}),
	})
	palette.Register(palette.Command{
		ID: "story.new", Title: "새 스토리 올리기", Group: groupCommand,
		Keywords: []string{"story", "new", "스토리"},
		Run:      nav(ui.Route{Kind: ui.RouteCompose, Arg: modeStory}),
	})
	palette.Register(palette.Command{
		ID: "feed.following", Title: "피드: 팔로잉", Group: groupGo, KeyHint: "f",
		Keywords: []string{"feed", "following", "timeline", "타임라인"},
		Run:      nav(ui.Route{Kind: ui.RouteFeed, Arg: "following"}),
	})
	palette.Register(palette.Command{
		ID: "feed.discover", Title: "피드: 전체(발견)", Group: groupGo,
		Keywords: []string{"feed", "discover", "explore", "all", "발견"},
		Run:      nav(ui.Route{Kind: ui.RouteFeed, Arg: "discover"}),
	})
	palette.Register(palette.Command{
		ID: "trends", Title: "트렌드 Top 10", Group: groupGo,
		Keywords: []string{"trends", "hashtag", "search", "해시태그", "검색"},
		Run:      nav(ui.Route{Kind: ui.RouteTrends}),
	})
	palette.Register(palette.Command{
		ID: "notifications", Title: "알림함", Group: groupGo, KeyHint: "ctrl+n",
		Keywords: []string{"notifications", "inbox", "알림"},
		Run:      nav(ui.Route{Kind: ui.RouteNotifications}),
		Badge:    inboxBadge,
	})
	palette.Register(palette.Command{
		ID: "profile.me", Title: "내 프로필", Group: groupGo,
		Keywords:  []string{"profile", "me", "프로필"},
		Available: hasMe,
		Run: func(ctx *ui.Context, _ string) tea.Cmd {
			if !hasMe(ctx) {
				return nil
			}
			return ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: ctx.Me().ID})
		},
	})
	palette.Register(palette.Command{
		ID: "profile.edit", Title: "프로필 편집", Group: groupSetting,
		Keywords:  []string{"profile", "edit", "bio", "avatar", "이름", "소개"},
		Available: hasMe,
		Run:       open(overlayProfileEdit),
	})
	palette.Register(palette.Command{
		ID: "follow_requests", Title: "팔로우 요청", Group: groupCommand,
		Keywords: []string{"follow", "requests", "private", "팔로우"},
		Run:      open(overlayFollowRequests),
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
				out = append(out, tagItem(tr.Tag, trendSubtitle(tr.Rank), scoreTrendBase-tr.Rank))
			}
		}
	}
	if !exact {
		out = append(out, tagItem(rest, textHashtagOpen, scoreOpenTag))
	}
	return out
}

func trendSubtitle(rank int) string { return fmt.Sprintf(textTrendRankFmt, rank) }

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
