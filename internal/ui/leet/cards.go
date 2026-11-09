package leet

import (
	"encoding/json"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// The #daily chat cards (A10.4): the day's root card (leet.daily) and the
// solve cards in its thread (leet.solve). Data that does not decode, a date
// or slug that fails the checks, or the tool's flag off draws the message's
// body like a text message, and the keys are left to chat. A solve card's
// code is a spoiler: folded until asked for, with a confirm when I have not
// solved that day; what is unfolded lives in this run's memory only.

func init() {
	ui.RegisterCard(ui.CardLeetDaily, ui.CardRenderer{Lines: dailyLines, Key: dailyKey})
	ui.RegisterCard(ui.CardLeetSolve, ui.CardRenderer{Lines: solveLines, Key: solveKey})
}

// dailyCard decodes m's leet.daily data (ok=false: draw the body).
func dailyCard(m *api.Message) (api.LeetDailyCard, bool) {
	var d api.LeetDailyCard
	if !features.On(toolID) || m == nil || m.Card == nil || json.Unmarshal(m.Card.Data, &d) != nil {
		return d, false
	}
	if !validDate(d.Date) || !leetcode.ValidSlug(d.Slug) {
		return d, false
	}
	return d, true
}

// solveCard decodes m's leet.solve data (ok=false: draw the body).
func solveCard(m *api.Message) (api.LeetSolveCard, bool) {
	var d api.LeetSolveCard
	if !features.On(toolID) || m == nil || m.Card == nil || json.Unmarshal(m.Card.Data, &d) != nil {
		return d, false
	}
	if !validDate(d.Date) || !leetcode.ValidSlug(d.Slug) {
		return d, false
	}
	return d, true
}

func validDate(s string) bool {
	if len(s) != len(time.DateOnly) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// oneLine: widget and tiny modes draw every card on one line.
func oneLine(ctx *ui.Context) bool {
	return ctx.Mode == layout.ModeWidget || ctx.Mode == layout.ModeTiny || ctx.Mode == layout.ModeTooSmall
}

// bodyLines is the fallback: the message body drawn as chat draws text.
func bodyLines(ctx *ui.Context, m *api.Message, width int) []string {
	th := themeOf(ctx)
	if m == nil || width <= 0 {
		return nil
	}
	o := markup.RenderOptions{Width: width, Hyperlinks: true}
	if oneLine(ctx) {
		o.Compact, o.MaxLines = true, 1
	}
	return markup.Render(markup.Parse(m.Body, markup.ParseOptions{}), th, o)
}

// authorName is m's author, sanitized (display name, else handle).
func authorName(u api.UserSummary) string {
	if n := textwidth.Sanitize(u.DisplayName); !textwidth.Blank(n) {
		return n
	}
	return textwidth.Sanitize(u.Handle)
}

func cut(st func(...string) string, s string, w int) string {
	return st(textwidth.Truncate(s, w, "…"))
}

// replies is the number of solves in the root's thread.
func replies(m *api.Message) int {
	if m.Thread == nil {
		return 0
	}
	return m.Thread.ReplyCount
}

// dailyLines draws a leet.daily root.
func dailyLines(ctx *ui.Context, m *api.Message, width int) []string {
	d, ok := dailyCard(m)
	if !ok {
		return bodyLines(ctx, m, width)
	}
	th, t := themeOf(ctx), ctx.T.Leetcode
	id, diff := textwidth.Sanitize(d.FrontendID), textwidth.Sanitize(d.Difficulty)
	if oneLine(ctx) {
		return []string{cut(th.S.Base.Render, t.CardDailyLine(id, diff, replies(m)), width)}
	}
	out := []string{
		cut(th.S.Bold.Render, t.CardDailyHead(d.Date, authorName(m.Author)), width),
		cut(th.S.Base.Render, t.CardDailyProblem(id, textwidth.Sanitize(d.Title), diff), width),
	}
	if mine := mySlugOn(ctx, d.Date); mine != "" && mine != d.Slug {
		out = append(out, cut(th.S.Warning.Render, t.CardMismatch(), width))
	}
	return append(out, cut(th.S.Dim.Render, t.CardDailyKeys(replies(m)), width))
}

// mySlugOn is the slug this device got as date's problem ("" unknown): this
// run's memory, else the local cache (kept in memory once read).
func mySlugOn(ctx *ui.Context, date string) string {
	if s := withMemo(ctx, func(m *memo) string { return m.days[date].Slug }); s != "" {
		return s
	}
	if ctx.Paths.Cache == "" {
		return ""
	}
	q, hit := leetcode.OpenCache(ctx.Paths.Cache).Daily(date)
	if !hit || q == nil {
		return ""
	}
	rememberDay(ctx, date, q, false)
	return q.Slug
}

// dailyKey: enter opens the day's problem, b the space's ranking.
func dailyKey(ctx *ui.Context, m *api.Message, k tea.KeyPressMsg) (bool, tea.Cmd) {
	d, ok := dailyCard(m)
	if !ok {
		return false, nil
	}
	switch {
	case ctx.Keys.Matches(k, keymap.ListOpen):
		slug := d.Slug
		if mine := mySlugOn(ctx, d.Date); mine != "" && mine != d.Slug && leetcode.ValidSlug(mine) {
			// A10.3: the card differs from what I got — open mine
			if d.Date == leetcode.UTCDate(nowFunc(ctx)()) {
				return true, ui.OpenTool(toolID, todayInstance, nil)
			}
			slug = mine
		}
		target := d.Date + "/" + slug
		return true, ui.OpenTool(toolID, instancePrefix+target, map[string]string{queryTarget: target})
	case k.String() == "b":
		return boardKey(ctx, m)
	}
	return false, nil
}

// boardKey opens the ranking of m's space (none known: chat keeps b).
func boardKey(ctx *ui.Context, m *api.Message) (bool, tea.Cmd) {
	space := spaceOfChannel(ctx, m.ChannelID)
	if space == "" {
		return false, nil
	}
	return true, ui.OpenRegisteredOverlay(OverlayBoard, space)
}

// spaceOfChannel is a channel's space ("" = a DM or unknown).
func spaceOfChannel(ctx *ui.Context, channelID string) string {
	if ctx.Store == nil {
		return ""
	}
	if c := ctx.Store.Channels[channelID]; c != nil && c.SpaceID != nil {
		return *c.SpaceID
	}
	return ""
}

// ---- solve cards ----

// spoilers are the solve cards whose code is shown (or whose confirm is up),
// by message id: this run, this screen; never saved.
var (
	spoilerMu sync.Mutex
	spoilers  = map[string]spoilerState{}
)

type spoilerState int

const (
	spoilerFolded spoilerState = iota
	spoilerAsking              // "아직 안 풀었어요. 그래도 볼까요?" under the card
	spoilerShown
)

func spoilerOf(id string) spoilerState {
	spoilerMu.Lock()
	defer spoilerMu.Unlock()
	return spoilers[id]
}

func setSpoiler(id string, st spoilerState) {
	spoilerMu.Lock()
	defer spoilerMu.Unlock()
	if st == spoilerFolded {
		delete(spoilers, id)
	} else {
		spoilers[id] = st
	}
}

// langName is a LeetCode language slug's name (unknown: the slug, sanitized).
func langName(slug string) string {
	if l, ok := leetcode.LangBySlug(slug); ok {
		return l.Name
	}
	return textwidth.Sanitize(slug)
}

// hasCode: the solver shared code.
func hasCode(d api.LeetSolveCard) bool { return d.Code != nil && d.Code.Text != "" }

// solveLines draws a leet.solve reply.
func solveLines(ctx *ui.Context, m *api.Message, width int) []string {
	d, ok := solveCard(m)
	if !ok {
		return bodyLines(ctx, m, width)
	}
	th, t := themeOf(ctx), ctx.T.Leetcode
	head := t.CardSolveHead(authorName(m.Author), langName(d.Lang))
	if d.RuntimePercentile != nil {
		head = t.CardSolveHeadPct(authorName(m.Author), langName(d.Lang), topPercent(*d.RuntimePercentile))
	}
	out := []string{cut(th.S.Base.Render, head, width)}
	if oneLine(ctx) || !hasCode(d) {
		return out
	}
	switch spoilerOf(m.ID) {
	case spoilerAsking:
		return append(out,
			cut(th.S.Warning.Render, "  "+t.SpoilerConfirm(), width),
			cut(th.S.Dim.Render, "  "+t.SpoilerKeys(), width))
	case spoilerShown:
		out = append(out, cut(th.S.Dim.Render, "  "+t.CardCodeHide(), width))
		return append(out, codeLines(th, d.Code, width)...)
	}
	return append(out, cut(th.S.Dim.Render, "  "+t.CardCodeShow(), width))
}

// codeLines is the shared code as a code block (sanitized by markup).
func codeLines(th *theme.Theme, c *api.LeetSolveCode, width int) []string {
	blk := markup.Block{Kind: markup.Code, Lang: textwidth.Sanitize(c.Lang), Code: c.Text}
	return markup.Render(markup.Doc{Blocks: []markup.Block{blk}}, th, markup.RenderOptions{Width: width})
}

// solveKey: enter/space folds and unfolds the code — when I have not solved
// that day, the card asks first (enter/y shows, esc/n keeps it folded); b
// opens the ranking.
func solveKey(ctx *ui.Context, m *api.Message, k tea.KeyPressMsg) (bool, tea.Cmd) {
	d, ok := solveCard(m)
	if !ok {
		return false, nil
	}
	if k.String() == "b" {
		return boardKey(ctx, m)
	}
	if !hasCode(d) || oneLine(ctx) {
		return false, nil
	}
	yes := k.String() == "space" || ctx.Keys.Matches(k, keymap.ListOpen)
	id := m.ID
	switch spoilerOf(id) {
	case spoilerAsking:
		switch {
		case yes || k.String() == "y":
			setSpoiler(id, spoilerShown)
		case k.String() == "esc" || k.String() == "n":
			setSpoiler(id, spoilerFolded)
		default:
			return false, nil
		}
	case spoilerShown:
		if !yes {
			return false, nil
		}
		setSpoiler(id, spoilerFolded)
	default:
		if !yes {
			return false, nil
		}
		if solvedOn(knownMe(ctx), d.Date) {
			setSpoiler(id, spoilerShown)
		} else {
			setSpoiler(id, spoilerAsking)
		}
	}
	return true, nil
}
