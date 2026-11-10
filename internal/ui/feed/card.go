// Package feed is the sns-fun feed UI (CA 6.2): timelines, post detail, the
// composer, profiles, stories, hashtags/trends and the notification inbox.
package feed

import (
	"github.com/patrache/masil/internal/i18n"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// hitKind is what a click on a card region does.
type hitKind int

const (
	hitHashtag    hitKind = iota // Value: tag (lowercase, no '#')
	hitPollOption                // Value: option idx
	hitImage                     // Value: media idx
	hitAuthor                    // Value: user id
	hitQuote                     // Value: quoted post id
)

// cardHit is a clickable region. Line indexes cardView.Lines; X0..X1 (half-open)
// are cells from the card's left edge, including the 2-cell gutter.
type cardHit struct {
	Line, X0, X1 int
	Kind         hitKind
	Value        string
}

// cardOpts controls renderCard.
type cardOpts struct {
	Width    int
	Density  ui.Density
	Selected bool
	PollSel  int                                        // poll option slice position with the Selected background; -1 = none
	Image    func(m api.Media, cols, rows int) []string // nil or nil result => dim placeholder box (feed.card_image_loading)
	// ExactTime shows the header time as humanize MessageTime ("11:05",
	// yesterday's "<yesterday> 11:05", ...) instead of the relative one (the post
	// detail's main post).
	ExactTime bool
}

// cardView is a rendered card: every line exactly Width cells.
type cardView struct {
	Lines []string
	Hits  []cardHit
}

// Card layout constants.
const (
	gutterW        = 2
	fullBodyLines  = 8
	bubbleBodyLine = 6
	fullImageCols  = 48
	bubbleImageCol = 30
	imageMinRows   = 3
	imageMaxRows   = 12
)

// pollPreviewIcon leads a poll's one-line preview (card copy: the feed catalog area).
const pollPreviewIcon = "📊 "

// hiddenByBlock reports whether a timeline item must be skipped because its
// author or its reposter is blocked. renderCard does not filter; callers do.
func hiddenByBlock(ctx *ui.Context, it api.TimelineItem) bool {
	if ctx == nil || ctx.Store == nil {
		return false
	}
	if ctx.Store.IsBlocked(it.Post.Author.ID) {
		return true
	}
	return it.RepostedBy != nil && ctx.Store.IsBlocked(it.RepostedBy.ID)
}

// imageBox is the cell size of a card's first image: Full is up to 48 columns,
// Bubble up to 30; rows keep the aspect ratio (a cell is twice as tall as wide,
// rounded to the nearest row) clamped to 3..12, 4:3 when the size is unknown.
// At the 12-row cap the width shrinks with it (C6: the box keeps the picture's
// shape on every screen, not 48x12 here and 30x11 there); only a very wide
// picture at the 3-row floor gets a taller box. Line and Minimal show no image.
func imageBox(m api.Media, contentW int, d ui.Density) (cols, rows int) {
	switch d {
	case ui.DensityFull:
		cols = min(contentW, fullImageCols)
	case ui.DensityBubble:
		cols = min(contentW, bubbleImageCol)
	default:
		return 0, 0
	}
	if cols <= 0 {
		return 0, 0
	}
	w, h := 4, 3
	if m.Width != nil && m.Height != nil && *m.Width > 0 && *m.Height > 0 {
		w, h = *m.Width, *m.Height
	}
	w64, h64 := int64(w), int64(h)
	for w64 > 1<<24 || h64 > 1<<24 { // hostile sizes: keep the products far from overflow
		w64, h64 = max(1, w64>>1), max(1, h64>>1)
	}
	r := (int64(cols)*h64 + w64) / (2 * w64) // round(cols*h / 2w)
	if r > imageMaxRows {
		r = imageMaxRows
		cols = int(max(1, min(int64(cols), (r*2*w64+h64/2)/h64))) // round(rows*2w / h)
	}
	return cols, int(max(r, imageMinRows))
}

// renderCard draws one post card. p is the post to draw (the FeedStore copy, so
// in-place updates show); it supplies the repost header.
func renderCard(ctx *ui.Context, it api.TimelineItem, p *api.Post, o cardOpts) cardView {
	if p == nil {
		p = &it.Post
	}
	b := newCardBuilder(ctx, o)
	if o.Width <= 0 {
		return cardView{}
	}
	switch o.Density {
	case ui.DensityLine:
		b.lineCard(it, p)
	case ui.DensityMinimal:
		b.minimalCard(p)
	default:
		b.fullCard(it, p, o)
	}
	return cardView{Lines: b.lines, Hits: b.hits}
}

// cardBuilder accumulates card lines and hit regions.
type cardBuilder struct {
	ctx    *ui.Context
	t      *theme.Theme
	c      *i18n.T // the screen's catalog
	w, cw  int     // card width, content width (w - gutter)
	gutter string
	lines  []string
	hits   []cardHit
}

func newCardBuilder(ctx *ui.Context, o cardOpts) *cardBuilder {
	t := theme.Get(theme.Default)
	if ctx != nil && ctx.Theme != nil {
		t = ctx.Theme
	}
	g := t.S.Base.Render("  ")
	if o.Selected {
		g = t.S.Base.Foreground(t.P.Acc).Render("▎") + t.S.Base.Render(" ")
	}
	return &cardBuilder{ctx: ctx, t: t, c: cat(ctx), w: o.Width, cw: o.Width - gutterW, gutter: g}
}

// fit truncates s to exactly w cells, padding with Base-styled spaces.
func fit(t *theme.Theme, s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = textwidth.Truncate(s, w, "")
	if pad := w - textwidth.Width(s); pad > 0 {
		s += t.S.Base.Render(strings.Repeat(" ", pad))
	}
	return s
}

// add appends a gutter + content line (content cut to the content width) and returns its index.
func (b *cardBuilder) add(content string) int {
	line := b.gutter
	if b.cw > 0 {
		line += fit(b.t, content, b.cw)
	}
	b.lines = append(b.lines, fit(b.t, line, b.w))
	return len(b.lines) - 1
}

// addRaw appends a full-width line without the gutter.
func (b *cardBuilder) addRaw(s string) {
	b.lines = append(b.lines, fit(b.t, s, b.w))
}

// hit records a region on line, clamped to the card width; empty regions are dropped.
func (b *cardBuilder) hit(line, x0, x1 int, k hitKind, v string) {
	x0, x1 = max(x0, 0), min(x1, b.w)
	if x0 >= x1 {
		return
	}
	b.hits = append(b.hits, cardHit{Line: line, X0: x0, X1: x1, Kind: k, Value: v})
}

func (b *cardBuilder) now() time.Time {
	if b.ctx == nil || b.ctx.Now == nil {
		return time.Time{} // never in practice: every Context has a clock (CA 5.3)
	}
	n := b.ctx.Now()
	if b.ctx.Loc != nil {
		n = n.In(b.ctx.Loc)
	}
	return n
}

func (b *cardBuilder) isMe(id string) bool {
	return b.ctx != nil && b.ctx.Store != nil && b.ctx.Store.IsMe(id)
}

func (b *cardBuilder) myHandle() string {
	if me := b.ctx.Me(); me != nil {
		return me.Handle
	}
	return ""
}

// displayName is the sanitized display name, falling back to the handle when
// the screen would show nothing of it (textwidth.Blank, pending-fixes 1).
func displayName(u api.UserSummary) string {
	if n := textwidth.Sanitize(u.DisplayName); !textwidth.Blank(n) {
		return n
	}
	return textwidth.Sanitize(u.Handle)
}

func handleOf(u api.UserSummary) string { return "@" + textwidth.Sanitize(u.Handle) }

// fullCard is DensityFull / DensityBubble.
func (b *cardBuilder) fullCard(it api.TimelineItem, p *api.Post, o cardOpts) {
	t := b.t
	if it.Type == "repost" && it.RepostedBy != nil {
		idx := b.add(t.S.Dim.Render("↻ " + b.c.Feed.CardRepostedBy(displayName(*it.RepostedBy))))
		b.hit(idx, gutterW, gutterW+min(b.cw, textwidth.Width("↻ "+displayName(*it.RepostedBy))), hitAuthor, it.RepostedBy.ID)
	}
	b.header(p, o.ExactTime)

	if p.Deleted {
		b.add(t.S.Deleted.Render(b.c.Feed.CardDeleted()))
		b.addRaw(t.S.Base.Foreground(t.P.Line).Render(strings.Repeat("┄", max(b.w, 0))))
		return
	}

	maxLines := fullBodyLines
	if o.Density == ui.DensityBubble {
		maxLines = bubbleBodyLine
	}
	if p.Body != "" && b.cw > 0 {
		doc := markup.Parse(p.Body, markup.ParseOptions{Hashtags: true})
		for _, l := range markup.Render(doc, t, markup.RenderOptions{
			Width: b.cw, MyHandle: b.myHandle(), Mentions: p.Mentions, MaxLines: maxLines, Hyperlinks: true,
		}) {
			b.hashtagHits(b.add(l), p.Hashtags, gutterW)
		}
	}
	b.images(p, o)
	b.quote(p)
	if p.Poll != nil && b.cw > 0 {
		for i, l := range widgets.PollView(t, *p.Poll, b.cw, o.PollSel) {
			idx := b.add(l)
			if i > 0 && i-1 < len(p.Poll.Options) {
				b.hit(idx, gutterW, b.w, hitPollOption, strconv.Itoa(p.Poll.Options[i-1].Idx))
			}
		}
	}
	b.add(b.stats(p, true))
	b.addRaw(t.S.Base.Foreground(t.P.Line).Render(strings.Repeat("┄", max(b.w, 0))))
}

// header is "@handle 이름 · 3분" (exact: "· 11:05"): the name is cut first so the time stays.
func (b *cardBuilder) header(p *api.Post, exact bool) {
	ts := " · " + hz(b.ctx).Relative(b.now(), p.CreatedAt)
	if exact {
		var loc *time.Location
		if b.ctx != nil {
			loc = b.ctx.Loc
		}
		ts = " · " + hz(b.ctx).MessageTime(b.now(), p.CreatedAt, loc)
	}
	idx, whoW := 0, 0
	if room := b.cw - textwidth.Width(ts); room >= 2 {
		var who string
		who, whoW = b.authorLabel(p.Author, room)
		idx = b.add(who + b.t.S.Timestamp.Render(ts))
	} else {
		var who string
		who, whoW = b.authorLabel(p.Author, b.cw)
		idx = b.add(who)
	}
	b.hit(idx, gutterW, gutterW+whoW, hitAuthor, p.Author.ID)
}

// authorLabel is "@handle 이름" in at most room cells: the name is cut (or dropped)
// before the handle. Text is cut before styling so every cell, "…" included, is styled.
func (b *cardBuilder) authorLabel(u api.UserSummary, room int) (string, int) {
	t := b.t
	if room <= 0 {
		return "", 0
	}
	h := handleOf(u)
	if textwidth.Width(h) >= room {
		h = textwidth.Truncate(h, room, "…")
		return t.S.Bold.Render(h), textwidth.Width(h)
	}
	out, w := t.S.Bold.Render(h), textwidth.Width(h)
	if nameRoom := room - w - 1; nameRoom >= 2 {
		n := displayName(u)
		if textwidth.Width(n) > nameRoom {
			n = textwidth.Truncate(n, nameRoom, "…")
		}
		named := u
		named.DisplayName = n
		out += t.S.Base.Render(" ") + widgets.UserName(t, named, b.isMe(u.ID))
		w += 1 + textwidth.Width(n)
	}
	return out, w
}

// shortHandle is the plain handle (with or without "@") cut to max(cw/3, 6) cells
// so one-line cards keep room for the body and stats.
func (b *cardBuilder) shortHandle(h string) string {
	if limit := max(b.cw/3, 6); textwidth.Width(h) > limit {
		return textwidth.Truncate(h, limit, "…")
	}
	return h
}

// images draws the first image (or its placeholder) and a "🖼 +N" chip for the rest.
func (b *cardBuilder) images(p *api.Post, o cardOpts) {
	if len(p.Media) == 0 {
		return
	}
	cols, rows := imageBox(p.Media[0], b.cw, o.Density)
	if cols <= 0 || rows <= 0 {
		return
	}
	var img []string
	if o.Image != nil {
		img = o.Image(p.Media[0], cols, rows)
	}
	if len(img) == 0 {
		img = placeholderBox(b.t, b.c.Feed.CardImageLoading(), cols, rows)
	}
	for r := 0; r < rows; r++ {
		var l string
		if r < len(img) {
			l = img[r]
		}
		idx := b.add(fit(b.t, l, cols))
		b.hit(idx, gutterW, gutterW+cols, hitImage, "0")
	}
	if n := len(p.Media) - 1; n > 0 {
		chip := " 🖼 +" + strconv.Itoa(n) + " "
		idx := b.add(b.t.S.Reaction.Render(chip))
		b.hit(idx, gutterW, gutterW+textwidth.Width(chip), hitImage, "1")
	}
}

// placeholderBox is a dim cols x rows frame with label (feed.card_image_loading)
// in the middle.
func placeholderBox(t *theme.Theme, label string, cols, rows int) []string {
	dim := t.S.Dim
	out := make([]string, rows)
	if cols < 2 || rows < 2 {
		for i := range out {
			out[i] = dim.Render(strings.Repeat("░", max(cols, 0)))
		}
		return out
	}
	inner := cols - 2
	mid := rows / 2
	for i := range out {
		switch {
		case i == 0:
			out[i] = dim.Render("┌" + strings.Repeat("─", inner) + "┐")
		case i == rows-1:
			out[i] = dim.Render("└" + strings.Repeat("─", inner) + "┘")
		case i == mid:
			out[i] = dim.Render("│" + textwidth.Center(textwidth.Truncate(label, inner, "…"), inner) + "│")
		default:
			out[i] = dim.Render("│" + strings.Repeat(" ", inner) + "│")
		}
	}
	return out
}

// quote draws the quoted post box: "┃ " + header and one body line, or one notice line.
func (b *cardBuilder) quote(p *api.Post) {
	q := p.Quote
	if q == nil {
		return
	}
	t := b.t
	bar := t.S.Base.Foreground(t.P.Line).Render("┃") + t.S.Base.Render(" ")
	qw := b.cw - 2
	switch {
	case q.Available && q.Post != nil && !q.Post.Deleted:
		qp := q.Post
		ts := " · " + hz(b.ctx).Relative(b.now(), qp.CreatedAt)
		who, _ := b.authorLabel(qp.Author, max(qw-textwidth.Width(ts), 2))
		head := who + t.S.Timestamp.Render(ts)
		body := markup.Plain(qp.Body, qw)
		if body == "" {
			body = previewOf(qp, qw)
		}
		for _, l := range []string{head, t.S.Base.Render(body)} {
			idx := b.add(bar + l)
			b.hit(idx, gutterW, b.w, hitQuote, qp.ID)
		}
	case q.Available && q.Post != nil, !q.Available && q.Reason == "deleted":
		b.add(bar + t.S.Deleted.Render(b.c.Feed.CardDeleted()))
	default:
		b.add(bar + t.S.Deleted.Render(b.c.Feed.CardNotVisible()))
	}
}

// previewOf is a one-line stand-in for a post without body text.
func previewOf(p *api.Post, w int) string {
	switch {
	case p.Poll != nil:
		return textwidth.Truncate(pollPreviewIcon+textwidth.Sanitize(p.Poll.Question), max(w, 0), "…")
	case len(p.Media) > 0:
		return textwidth.Truncate("🖼 "+strconv.Itoa(len(p.Media)), max(w, 0), "…")
	}
	return ""
}

// stats is "♥ 12  ↻ 3  💬 4[  ❝ N]" (spaced) or "♥12 ↻3 💬4[ ❝N]" (compact). ↻ is
// omitted at zero unless I reposted; my like is acc2 bold, my repost acc3.
func (b *cardBuilder) stats(p *api.Post, spaced bool) string {
	t := b.t
	dim := t.S.Dim
	sep, gap := "  ", " "
	if !spaced {
		sep, gap = " ", ""
	}
	icon := func(s string, mine bool, st lipgloss.Style) string {
		if mine {
			return st.Render(s)
		}
		return dim.Render(s)
	}
	out := icon("♥", p.Viewer.Liked, t.S.Base.Foreground(t.P.Acc2).Bold(true)) + dim.Render(gap+hz(b.ctx).Count(p.LikeCount))
	if p.RepostCount > 0 || p.Viewer.Reposted {
		out += dim.Render(sep) + icon("↻", p.Viewer.Reposted, t.S.Base.Foreground(t.P.Acc3)) + dim.Render(gap+hz(b.ctx).Count(p.RepostCount))
	}
	out += dim.Render(sep + "💬" + gap + hz(b.ctx).Count(p.ReplyCount))
	if p.QuoteCount > 0 {
		out += dim.Render(sep + "❝" + gap + hz(b.ctx).Count(p.QuoteCount))
	}
	return out
}

// lineCard is DensityLine: "3분 @hana 오늘 첫 눈 ❄ 사무실 창밖 🖼1 ♥12 ↻3 💬4".
func (b *cardBuilder) lineCard(it api.TimelineItem, p *api.Post) {
	t := b.t
	prefix := t.S.Timestamp.Render(hz(b.ctx).Relative(b.now(), p.CreatedAt)) + t.S.Base.Render(" ")
	if it.Type == "repost" && it.RepostedBy != nil {
		prefix += t.S.Dim.Render("↻ "+b.shortHandle(displayName(*it.RepostedBy))) + t.S.Base.Render(" ")
	}
	who := t.S.Base.Foreground(t.UserColor(p.Author.ID, b.isMe(p.Author.ID))).Bold(true).Render(b.shortHandle(handleOf(p.Author)))
	whoX := gutterW + textwidth.Width(prefix)
	whoW := textwidth.Width(who)

	var media, suffix string
	if !p.Deleted {
		if len(p.Media) > 0 {
			media = t.S.Dim.Render(" 🖼" + strconv.Itoa(len(p.Media)))
		}
		suffix = t.S.Dim.Render(" ") + b.stats(p, false)
	}
	bodyW := b.cw - textwidth.Width(prefix) - whoW - 1 - textwidth.Width(media) - textwidth.Width(suffix)
	var body string
	switch {
	case bodyW <= 0:
	case p.Deleted:
		body = t.S.Deleted.Render(textwidth.Truncate(b.c.Feed.CardDeleted(), bodyW, "…"))
	case p.Body != "":
		doc := markup.Parse(p.Body, markup.ParseOptions{Hashtags: true})
		if ls := markup.Render(doc, t, markup.RenderOptions{
			Width: bodyW, MyHandle: b.myHandle(), Mentions: p.Mentions, Compact: true, Hyperlinks: true,
		}); len(ls) > 0 {
			body = ls[0]
		}
	default:
		body = t.S.Base.Render(previewOf(p, bodyW))
	}
	line := prefix + who + t.S.Base.Render(" ") + body + media + suffix
	idx := b.add(line)
	b.hit(idx, whoX, whoX+whoW, hitAuthor, p.Author.ID)
	bodyX := whoX + whoW + 1
	b.hashtagHits(idx, p.Hashtags, bodyX)
	if media != "" {
		mx := bodyX + textwidth.Width(body)
		b.hit(idx, mx+1, mx+textwidth.Width(media), hitImage, "0")
	}
}

// minimalCard is DensityMinimal: "hana: 오늘 첫 눈… ♥12".
func (b *cardBuilder) minimalCard(p *api.Post) {
	t := b.t
	who := t.S.Base.Foreground(t.UserColor(p.Author.ID, b.isMe(p.Author.ID))).Render(b.shortHandle(textwidth.Sanitize(p.Author.Handle)))
	whoW := textwidth.Width(who)
	colon := t.S.Base.Render(": ")
	var suffix string
	if !p.Deleted {
		heart := t.S.Dim.Render("♥")
		if p.Viewer.Liked {
			heart = t.S.Base.Foreground(t.P.Acc2).Bold(true).Render("♥")
		}
		suffix = t.S.Dim.Render(" ") + heart + t.S.Dim.Render(hz(b.ctx).Count(p.LikeCount))
	}
	bodyW := b.cw - whoW - textwidth.Width(colon) - textwidth.Width(suffix)
	var body string
	switch {
	case bodyW <= 0:
	case p.Deleted:
		body = t.S.Deleted.Render(textwidth.Truncate(b.c.Feed.CardDeleted(), bodyW, "…"))
	case p.Body != "":
		body = t.S.Base.Render(markup.Plain(p.Body, bodyW))
	default:
		body = t.S.Base.Render(previewOf(p, bodyW))
	}
	idx := b.add(who + colon + body + suffix)
	b.hit(idx, gutterW, gutterW+whoW, hitAuthor, p.Author.ID)
	b.hashtagHits(idx, p.Hashtags, gutterW+whoW+textwidth.Width(colon))
}

// hashtagHits finds each "#"+tag (case-insensitive, whole tag) on line idx at or
// right of cell from and records a hitHashtag over it.
func (b *cardBuilder) hashtagHits(idx int, tags []string, from int) {
	if len(tags) == 0 {
		return
	}
	s := ansi.Strip(b.lines[idx])
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		for _, m := range findTag(s, tag) {
			x0 := ansi.StringWidth(s[:m[0]])
			if x0 < from {
				continue
			}
			b.hit(idx, x0, x0+ansi.StringWidth(s[m[0]:m[1]]), hitHashtag, tag)
		}
	}
}

// findTag returns the byte ranges of "#"+tag in s, matched case-insensitively and
// not followed by another tag character.
func findTag(s, tag string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); i++ {
		if s[i] != '#' {
			continue
		}
		n, ok := foldPrefix(s[i+1:], tag)
		if !ok {
			continue
		}
		end := i + 1 + n
		if end < len(s) {
			if r, _ := utf8.DecodeRuneInString(s[end:]); isTagRune(r) {
				continue
			}
		}
		out = append(out, [2]int{i, end})
	}
	return out
}

// foldPrefix reports whether s starts with p under simple case folding and how many bytes of s matched.
func foldPrefix(s, p string) (int, bool) {
	n := 0
	for _, pr := range p {
		if n >= len(s) {
			return 0, false
		}
		sr, size := utf8.DecodeRuneInString(s[n:])
		if sr != pr && !strings.EqualFold(string(sr), string(pr)) {
			return 0, false
		}
		n += size
	}
	return n, true
}

func isTagRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
