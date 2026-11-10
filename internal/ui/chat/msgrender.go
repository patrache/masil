package chat

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// rowKind is the kind of one row of a message list.
type rowKind int

const (
	rowMessage rowKind = iota // a server message
	rowPending                // an optimistic send (chatstore.PendingMessage)
	rowDay                    // date divider (full density only)
	rowUnread                 // "── 새 메시지 ──"
	rowLoading                // older page loading
	rowNotice                 // a centered notice (Text)
	rowReplies                // a thread's "── 답글 N개 ───" divider under its root (Text)
)

// row is one item of a message list. ID: the message id, "pending:"+nonce,
// "day:"+YYYYMMDD, "unread" or "loading".
type row struct {
	Kind    rowKind
	ID      string
	Msg     *api.Message
	Pending *chatstore.PendingMessage
	Text    string
}

// rowOpts controls buildRows: LastRead is my read pointer (the unread divider goes
// before the first newer message), Loading prepends a loading row, InThread drops
// the unread divider (the read pointer is per channel).
type rowOpts struct {
	LastRead string
	Loading  bool
	InThread bool
}

// msgStyle controls renderRow.
type msgStyle struct {
	Density      ui.Density
	Width        int
	Selected     bool // every line painted with S.Cursorline
	Continuation bool // same author within 5 minutes: no header line
	InThread     bool // no reply count
	Fade         *lipgloss.Style
}

// Rendering constants.
const (
	continuationGap = 5 * time.Minute
	fullIndent      = 2
	lineNameMax     = 16 // DensityLine name cap (cells)
	minimalNameMax  = 6  // DensityMinimal name cap (cells)
	bubbleRatio     = 80 // bubble width, percent of the row
	pendingBodyMin  = 10 // one-line densities keep this much of a pending body before shortening the status
)

// buildRows turns a window of messages plus the list's pending sends into rows:
// a loading row first (o.Loading), date dividers before each day group (full
// density only), the unread divider before the first message newer than
// o.LastRead that someone else wrote (only when a message at or before it is in
// the window, never in a thread), then the pending sends in SentAt order.
func buildRows(ctx *ui.Context, msgs []api.Message, pending []chatstore.PendingMessage, o rowOpts) []row {
	loc := locOf(ctx)
	full := ctx != nil && ctx.Density() == ui.DensityFull
	rows := make([]row, 0, len(msgs)+len(pending)+4)
	if o.Loading {
		rows = append(rows, row{Kind: rowLoading, ID: "loading"})
	}
	unreadDone := o.LastRead == "" || o.InThread
	seenRead := false
	lastDay := ""
	for i := range msgs {
		m := &msgs[i]
		if !unreadDone {
			switch {
			case m.ID <= o.LastRead:
				seenRead = true
			case seenRead && ctx != nil && ctx.Store != nil && ctx.Store.IsMe(m.Author.ID):
				// my own message is never new: the divider waits for someone else's
			case seenRead:
				rows = append(rows, row{Kind: rowUnread, ID: "unread"})
				unreadDone = true
			default: // the read pointer is older than the window: nothing to mark
				unreadDone = true
			}
		}
		if full {
			if day := m.CreatedAt.In(loc).Format("20060102"); day != lastDay {
				lastDay = day
				rows = append(rows, row{Kind: rowDay, ID: "day:" + day, Text: hz(ctx).DayDivider(m.CreatedAt, loc)})
			}
		}
		rows = append(rows, row{Kind: rowMessage, ID: m.ID, Msg: m})
	}
	ps := slices.Clone(pending)
	slices.SortStableFunc(ps, func(a, b chatstore.PendingMessage) int { return a.SentAt.Compare(b.SentAt) })
	for i := range ps {
		rows = append(rows, row{Kind: rowPending, ID: "pending:" + ps[i].Nonce, Pending: &ps[i]})
	}
	return rows
}

// isContinuation reports whether cur continues prev's group (no header line):
// both plain messages of the same author, cur less than 5 minutes after prev, no
// divider in between (prev is the row right above cur).
func isContinuation(prev, cur row) bool {
	if prev.Kind != rowMessage || cur.Kind != rowMessage || prev.Msg == nil || cur.Msg == nil {
		return false
	}
	p, c := prev.Msg, cur.Msg
	if p.Author.ID != c.Author.ID || headerless(p) || headerless(c) {
		return false
	}
	d := c.CreatedAt.Sub(p.CreatedAt)
	return d >= 0 && d < continuationGap
}

// headerless kinds render without a name line.
func headerless(m *api.Message) bool { return m.Kind == "me" || m.Kind == "system" }

// renderRow renders one row; every line is at most st.Width cells, every cell
// styled. A selected row is painted with S.Cursorline, a fading one with st.Fade's
// background, both padded to st.Width.
func renderRow(ctx *ui.Context, r row, st msgStyle) []string {
	if st.Width <= 0 {
		return nil
	}
	t := themeOf(ctx)
	var lines []string
	switch r.Kind {
	case rowMessage:
		if r.Msg != nil {
			lines = renderMessage(ctx, t, r.Msg, st)
		}
	case rowPending:
		if r.Pending != nil {
			lines = renderPending(ctx, t, r.Pending, st)
		}
	case rowDay:
		if st.Density == ui.DensityFull {
			lines = []string{rule(t, t.S.Dim, "── "+textwidth.Sanitize(r.Text)+" ", st.Width, false)}
		}
	case rowUnread:
		lines = []string{rule(t, t.S.Error, " "+cat(ctx).Chat.UnreadRule()+" ", st.Width, true)}
	case rowLoading:
		lines = []string{centerLine(t, t.S.Dim, cat(ctx).Chat.Loading(), st.Width)}
	case rowNotice:
		for _, l := range textwidth.Wrap(textwidth.Sanitize(r.Text), st.Width) {
			lines = append(lines, centerLine(t, t.S.Dim, l, st.Width))
		}
	}
	for i, l := range lines {
		if textwidth.Width(l) > st.Width {
			lines[i] = textwidth.Truncate(l, st.Width, "")
		}
	}
	switch {
	case st.Selected:
		lines = paint(t, lines, t.S.Cursorline, st.Width)
	case st.Fade != nil:
		lines = paint(t, lines, *st.Fade, st.Width)
	}
	return lines
}

// ---- messages ----

func renderMessage(ctx *ui.Context, t *theme.Theme, m *api.Message, st msgStyle) []string {
	s := ctx.Store
	mine := s != nil && s.IsMe(m.Author.ID)
	switch {
	case !mine && s != nil && s.IsBlocked(m.Author.ID):
		return []string{fit(t.S.Dim.Render(cat(ctx).Chat.BlockedMsg()), st.Width)}
	case m.Kind == "system" && !m.Deleted:
		txt := ""
		if m.System != nil {
			txt = systemText(ctx, *m.System)
		}
		if txt == "" {
			txt = markup.Plain(m.Body, st.Width, cat(ctx).Common.CodeSummary)
		}
		return []string{centerLine(t, t.S.System, "── "+txt+" ──", st.Width)}
	case m.Kind == "me" && !m.Deleted:
		return renderMe(ctx, t, m, st)
	}
	switch st.Density {
	case ui.DensityLine:
		return []string{lineMessage(ctx, t, m, st)}
	case ui.DensityMinimal:
		return []string{minimalMessage(ctx, t, m, st)}
	case ui.DensityBubble:
		return bubbleMessage(ctx, t, m, mine, st)
	default:
		return fullMessage(ctx, t, m, st)
	}
}

// fullMessage: "<name> <time>[ 📌]" (unless a continuation), the body indented 2,
// then the reaction chips and "💬 답글 N개" (joined by " · " when they fit).
func fullMessage(ctx *ui.Context, t *theme.Theme, m *api.Message, st msgStyle) []string {
	var out []string
	if !st.Continuation {
		out = append(out, header(ctx, t, m.Author, m.CreatedAt, m.Pinned && !m.Deleted, st.Width))
	}
	ind := indentOf(st.Width)
	bw := st.Width - ind
	pad := blank(t, ind)
	for _, l := range bodyLines(ctx, t, m, bw) {
		out = append(out, pad+l)
	}
	if !m.Deleted {
		for _, l := range metaLines(cat(ctx), t, m, bw, st.InThread) {
			out = append(out, pad+l)
		}
	}
	return out
}

// lineMessage: "HH:MM 이름 본문 💬N" on one line.
func lineMessage(ctx *ui.Context, t *theme.Theme, m *api.Message, st msgStyle) string {
	head := t.S.Timestamp.Render(clockOf(ctx, m.CreatedAt)) + blank(t, 1) +
		textwidth.Truncate(widgets.UserName(t, m.Author, isMe(ctx, m.Author.ID)), lineNameMax, "…") + blank(t, 1)
	suffix := ""
	if !m.Deleted && !st.InThread && m.Thread != nil && m.Thread.ReplyCount > 0 {
		suffix = t.S.Dim.Render(" 💬" + hz(ctx).Count(m.Thread.ReplyCount))
	}
	room := st.Width - textwidth.Width(head) - textwidth.Width(suffix)
	body := ""
	if room > 0 {
		body = compactBody(ctx, t, m, room)
	}
	return fit(head+body+suffix, st.Width)
}

// minimalMessage: "이름(6칸) 본문(Plain)" on one line (a registered card: its
// first line).
func minimalMessage(ctx *ui.Context, t *theme.Theme, m *api.Message, st msgStyle) string {
	name := textwidth.Truncate(widgets.UserName(t, m.Author, false), minimalNameMax, "…") + blank(t, 1)
	room := st.Width - textwidth.Width(name)
	body := ""
	if room > 0 {
		var card []string
		switch {
		case m.Deleted:
			body = t.S.Deleted.Render(textwidth.Truncate(cat(ctx).Chat.MsgDeleted(), room, "…"))
		case cardLines(ctx, m, room, &card):
			if len(card) > 0 {
				body = card[0]
			}
		default:
			body = t.S.Base.Render(markup.Plain(plainBody(m), room, cat(ctx).Common.CodeSummary))
		}
	}
	return fit(name+body, st.Width)
}

// bubbleMessage: others "이름 HH:MM" + a chip-colored bubble (<= 80% wide) on the
// left; mine "HH:MM 나" + the bubble right-aligned. Replies and chips go under it
// ("💬3 👍 2").
func bubbleMessage(ctx *ui.Context, t *theme.Theme, m *api.Message, mine bool, st msgStyle) []string {
	var out []string
	if !st.Continuation {
		clock := t.S.Timestamp.Render(clockOf(ctx, m.CreatedAt))
		if mine {
			me := widgets.UserName(t, api.UserSummary{ID: m.Author.ID, DisplayName: cat(ctx).Chat.Me()}, true)
			out = append(out, clock+blank(t, 1)+me)
		} else {
			name := widgets.UserName(t, m.Author, false)
			name = textwidth.Truncate(name, max(1, st.Width-textwidth.Width(clock)-1), "…")
			out = append(out, name+blank(t, 1)+clock)
		}
	}
	pad, bw := bubbleDims(st.Width)
	out = append(out, bubble(t, bodyLines(ctx, t, m, bw), pad)...)
	if !m.Deleted {
		out = append(out, bubbleMeta(cat(ctx), t, m, max(1, st.Width*bubbleRatio/100), st.InThread)...)
	}
	if mine {
		out = alignRight(t, out, st.Width)
	}
	return out
}

// renderMe: "* 이름 본문" (no header) — wrapped in full/bubble, one line otherwise.
func renderMe(ctx *ui.Context, t *theme.Theme, m *api.Message, st msgStyle) []string {
	name := textwidth.Sanitize(displayName(m.Author))
	text := "* " + name + " " + markup.Plain(m.Body, 4*len(m.Body)+8, cat(ctx).Common.CodeSummary)
	edited := m.EditedAt != nil
	switch st.Density {
	case ui.DensityLine, ui.DensityMinimal:
		prefix := ""
		if st.Density == ui.DensityLine {
			prefix = t.S.Timestamp.Render(clockOf(ctx, m.CreatedAt)) + blank(t, 1)
		}
		room := st.Width - textwidth.Width(prefix)
		if room <= 0 {
			return []string{fit(prefix, st.Width)}
		}
		return []string{fit(prefix+meLine(t, m, name, textwidth.Truncate(text, room, "…")), st.Width)}
	}
	var out []string
	for i, l := range textwidth.Wrap(text, st.Width) {
		if i == 0 {
			out = append(out, meLine(t, m, name, l))
			continue
		}
		out = append(out, t.S.Base.Italic(true).Render(l))
	}
	if edited {
		out = appendSuffix(out, " "+cat(ctx).Chat.Edited(), t.S.Dim, st.Width)
	}
	return out
}

// meLine styles the first line of a /me message: the name in the author's color.
func meLine(t *theme.Theme, m *api.Message, name, l string) string {
	it := t.S.Base.Italic(true)
	if rest, ok := strings.CutPrefix(l, "* "+name); ok {
		return it.Render("* ") + it.Foreground(t.UserColor(m.Author.ID, false)).Bold(true).Render(name) + it.Render(rest)
	}
	return it.Render(l)
}

// header is the full-density name line: "<name> <time>[ 📌]".
func header(ctx *ui.Context, t *theme.Theme, u api.UserSummary, at time.Time, pinned bool, width int) string {
	tail := blank(t, 1) + t.S.Timestamp.Render(clockOf(ctx, at))
	if pinned {
		tail += t.S.Base.Render(" 📌")
	}
	name := widgets.UserName(t, u, isMe(ctx, u.ID))
	name = textwidth.Truncate(name, max(1, width-textwidth.Width(tail)), "…")
	return fit(name+tail, width)
}

// bodyLines renders the content of a message at width (text, poll, roll, deleted)
// with the "(수정됨)" suffix.
func bodyLines(ctx *ui.Context, t *theme.Theme, m *api.Message, width int) []string {
	if width <= 0 {
		return nil
	}
	if m.Deleted {
		return []string{fit(t.S.Deleted.Render(cat(ctx).Chat.MsgDeleted()), width)}
	}
	var out []string
	switch {
	case m.Kind == "poll" && m.Poll != nil:
		out = pollLines(cat(ctx), t, *m.Poll, width)
	case m.Kind == "roll" && m.Roll != nil:
		for _, l := range textwidth.Wrap(rollText(*m.Roll), width) {
			out = append(out, t.S.Base.Render(l))
		}
	case m.Kind == api.MessageKindCard && cardLines(ctx, m, width, &out):
	default:
		out = markup.Render(markup.Parse(m.Body, markup.ParseOptions{Hashtags: false}), t, renderOpts(ctx, m, width, false))
	}
	if m.EditedAt != nil {
		out = appendSuffix(out, " "+cat(ctx).Chat.Edited(), t.S.Dim, width)
	}
	return out
}

// compactBody is the one-line body for DensityLine at width.
func compactBody(ctx *ui.Context, t *theme.Theme, m *api.Message, width int) string {
	switch {
	case m.Deleted:
		return t.S.Deleted.Render(textwidth.Truncate(cat(ctx).Chat.MsgDeleted(), width, "…"))
	case m.Kind == "poll" && m.Poll != nil:
		return t.S.Base.Render(textwidth.Truncate("📊 "+textwidth.Sanitize(m.Poll.Question), width, "…"))
	case m.Kind == "roll" && m.Roll != nil:
		return t.S.Base.Render(textwidth.Truncate(rollText(*m.Roll), width, "…"))
	}
	var card []string
	if cardLines(ctx, m, width, &card) {
		if len(card) > 0 {
			return card[0]
		}
		return ""
	}
	o := renderOpts(ctx, m, width, true)
	o.MaxLines = 1
	if ls := markup.Render(markup.Parse(m.Body, markup.ParseOptions{Hashtags: false}), t, o); len(ls) > 0 {
		return ls[0]
	}
	return ""
}

// cardLines draws a card message of a registered type into *out, each line cut
// to width (a renderer that overflows cannot break the pane), and reports
// whether it did. Unknown types and non-card messages => false: the body is
// drawn as markup like any text message (api.md 5.10 fallback).
func cardLines(ctx *ui.Context, m *api.Message, width int, out *[]string) bool {
	cr, ok := ui.MessageCard(m)
	if !ok {
		return false
	}
	ls := cr.Lines(ctx, m, width)
	lines := make([]string, 0, len(ls))
	for _, l := range ls {
		lines = append(lines, fit(l, width))
	}
	*out = lines
	return true
}

// plainBody is the text Plain previews (poll question, roll result, body).
func plainBody(m *api.Message) string {
	switch {
	case m.Kind == "poll" && m.Poll != nil:
		return "📊 " + m.Poll.Question
	case m.Kind == "roll" && m.Roll != nil:
		return rollText(*m.Roll)
	}
	return m.Body
}

func renderOpts(ctx *ui.Context, m *api.Message, width int, compact bool) markup.RenderOptions {
	c := cat(ctx)
	o := markup.RenderOptions{Width: width, Mentions: m.Mentions, MentionHere: m.MentionHere, Compact: compact, Hyperlinks: true,
		MoreTail: c.Common.MoreTail(), CodeSummary: c.Common.CodeSummary}
	if me := ctx.Me(); me != nil {
		o.MyHandle = me.Handle
	}
	return o
}

// pollLines: "📊 투표: 질문[ (마감)]", the widgets.PollView option lines, then
// "N명 참여[ · 복수 선택]".
func pollLines(c *i18n.T, t *theme.Theme, p api.Poll, width int) []string {
	head := c.Chat.PollHead(textwidth.Sanitize(p.Question))
	if p.Closed {
		head += " " + c.Chat.PollClosedMark()
	}
	out := []string{t.S.Bold.Render(textwidth.Truncate(head, width, "…"))}
	if pv := widgets.PollView(t, p, width, -1); len(pv) > 1 {
		out = append(out, pv[1:]...)
	}
	meta := c.Chat.PollVoters(p.VoterCount)
	if p.Multi {
		meta += " · " + c.Chat.PollMulti()
	}
	return append(out, t.S.Dim.Render(textwidth.Truncate(meta, width, "…")))
}

// metaLines: reaction chips and "💬 답글 N개" (one line when they fit).
func metaLines(c *i18n.T, t *theme.Theme, m *api.Message, width int, inThread bool) []string {
	if width <= 0 {
		return nil
	}
	lines := widgets.ReactionChips(t, m.Reactions, width)
	if inThread || m.Thread == nil || m.Thread.ReplyCount <= 0 {
		return lines
	}
	replies := t.S.Dim.Render(textwidth.Truncate(c.Chat.MetaReplies(humanize.For(c.Locale()).Count(m.Thread.ReplyCount)), width, "…"))
	sep := t.S.Dim.Render(" · ")
	if n := len(lines); n > 0 && textwidth.Width(lines[n-1])+textwidth.Width(sep)+textwidth.Width(replies) <= width {
		lines[n-1] += sep + replies
		return lines
	}
	return append(lines, replies)
}

// bubbleMeta: "💬N" then the chips under a bubble.
func bubbleMeta(c *i18n.T, t *theme.Theme, m *api.Message, width int, inThread bool) []string {
	lead := ""
	if !inThread && m.Thread != nil && m.Thread.ReplyCount > 0 {
		lead = t.S.Dim.Render(textwidth.Truncate("💬"+humanize.For(c.Locale()).Count(m.Thread.ReplyCount), width, "…"))
	}
	room := width
	if lead != "" {
		room -= textwidth.Width(lead) + 1
	}
	var chips []string
	if room > 0 {
		chips = widgets.ReactionChips(t, m.Reactions, room)
	}
	switch {
	case lead == "":
		return chips
	case len(chips) == 0:
		return []string{lead}
	}
	out := []string{lead + blank(t, 1) + chips[0]}
	return append(out, chips[1:]...)
}

// bubbleDims is the inner padding and content width of a bubble in a row of width w.
func bubbleDims(w int) (pad, inner int) {
	if w >= 6 {
		pad = 1
	}
	return pad, max(1, w*bubbleRatio/100-2*pad)
}

// bubble boxes lines in the chip background, every line as wide as the widest.
func bubble(t *theme.Theme, lines []string, pad int) []string {
	inner := 0
	for _, l := range lines {
		inner = max(inner, textwidth.Width(l))
	}
	st := t.S.Reaction
	out := make([]string, len(lines))
	for i, l := range lines {
		fill := inner - textwidth.Width(l) + pad
		out[i] = spaces(st, pad) + recolorBG(l, t.P.BG, st.GetBackground()) + spaces(st, fill)
	}
	return out
}

// alignRight pads every line on the left so it ends at width.
func alignRight(t *theme.Theme, lines []string, width int) []string {
	for i, l := range lines {
		if w := textwidth.Width(l); w < width {
			lines[i] = blank(t, width-w) + l
		}
	}
	return lines
}

// ---- pending sends ----

// renderPending: my optimistic send, dim body + " · 보내는 중…", or the failed
// state in S.Error with the reason (errtext.Describe) — its own line(s) in the
// multi-line densities, inside the status of the one-line ones while it fits.
func renderPending(ctx *ui.Context, t *theme.Theme, p *chatstore.PendingMessage, st msgStyle) []string {
	var me api.UserSummary
	if m := ctx.Me(); m != nil {
		me = m.UserSummary
	}
	body := textwidth.Sanitize(p.Body)
	switch p.Kind {
	case "me":
		body = "* " + textwidth.Sanitize(displayName(me)) + " " + body
	case "poll": // Body = the question (plainBody's form)
		body = "📊 " + body
	case "roll": // Body = the dice expression
		body = "🎲 " + body
	}
	stStyle := t.S.Dim
	if p.Failed != nil {
		stStyle = t.S.Error
	}
	status := stStyle.Render(statusPlain(cat(ctx), p))
	switch st.Density {
	case ui.DensityLine, ui.DensityMinimal:
		head := textwidth.Truncate(widgets.UserName(t, me, true), minimalNameMax, "…") + blank(t, 1)
		if st.Density == ui.DensityLine {
			head = t.S.Timestamp.Render(clockOf(ctx, p.SentAt)) + blank(t, 1) +
				textwidth.Truncate(widgets.UserName(t, me, true), lineNameMax, "…") + blank(t, 1)
		}
		// one line: keep some of the body visible, shortening the failed status
		// (with reason + hint → with reason → bare) until it does.
		room := st.Width - textwidth.Width(head) - textwidth.Width(status)
		if p.Failed != nil {
			reason := failedReason(ctx, p)
			short, hint := " · "+cat(ctx).Chat.SendFailed(), " — "+cat(ctx).Chat.SendFailedHint()
			for _, alt := range []string{short + ": " + reason + hint, short + ": " + reason, short} {
				status = stStyle.Render(alt)
				room = st.Width - textwidth.Width(head) - textwidth.Width(status)
				if room >= min(textwidth.Width(body), pendingBodyMin) {
					break
				}
			}
		}
		if room <= 0 {
			status = textwidth.Truncate(status, max(0, st.Width-textwidth.Width(head)), "…")
		}
		b := ""
		if room > 0 {
			b = t.S.Dim.Render(markup.Plain(body, room, cat(ctx).Common.CodeSummary))
		}
		return []string{fit(head+b+status, st.Width)}
	case ui.DensityBubble:
		var out []string
		if !st.Continuation {
			out = append(out, t.S.Timestamp.Render(clockOf(ctx, p.SentAt))+blank(t, 1)+
				widgets.UserName(t, api.UserSummary{ID: me.ID, DisplayName: cat(ctx).Chat.Me()}, true))
		}
		pad, bw := bubbleDims(st.Width)
		out = append(out, bubble(t, dimWrap(t, body, bw), pad)...)
		for _, l := range textwidth.Wrap(strings.TrimPrefix(statusPlain(cat(ctx), p), " · "), st.Width) {
			out = append(out, stStyle.Render(l))
		}
		if p.Failed != nil {
			for _, l := range textwidth.Wrap(failedReason(ctx, p), st.Width) {
				out = append(out, stStyle.Render(l))
			}
		}
		return alignRight(t, out, st.Width)
	}
	var out []string
	if !st.Continuation {
		out = append(out, header(ctx, t, me, p.SentAt, false, st.Width))
	}
	ind := indentOf(st.Width)
	bw := st.Width - ind
	lines := appendSuffix(dimWrap(t, body, bw), statusPlain(cat(ctx), p), stStyle, bw)
	if p.Failed != nil {
		for _, l := range textwidth.Wrap(failedReason(ctx, p), max(1, bw)) {
			lines = append(lines, stStyle.Render(l))
		}
	}
	for _, l := range lines {
		out = append(out, blank(t, ind)+l)
	}
	return out
}

// failedReason is the user-facing reason of a failed send (sanitized for display).
func failedReason(ctx *ui.Context, p *chatstore.PendingMessage) string {
	return errtext.Describe(tOf(ctx), p.Failed)
}

// statusPlain is the pending message's suffix: " · 보내는 중…" or
// " · 전송 실패 — enter 다시 보내기 · d 버리기".
func statusPlain(c *i18n.T, p *chatstore.PendingMessage) string {
	if p.Failed != nil {
		return " · " + c.Chat.SendFailed() + " — " + c.Chat.SendFailedHint()
	}
	return " · " + c.Common.Sending()
}

// dimWrap wraps sanitized text to width in S.Dim.
func dimWrap(t *theme.Theme, s string, width int) []string {
	var out []string
	for _, l := range textwidth.Wrap(s, max(1, width)) {
		out = append(out, t.S.Dim.Render(l))
	}
	return out
}

// ---- texts ----

// systemText is the sentence of a system event in the screen language (sanitized). X = the
// target user's display name, else the actor's.
func systemText(ctx *ui.Context, e api.SystemEvent) string {
	who := e.ActorID
	if e.TargetUserID != nil && *e.TargetUserID != "" {
		who = *e.TargetUserID
	}
	x := nameOf(ctx, who)
	c := cat(ctx)
	var s string
	switch e.Event {
	case "member_joined", "group_member_added":
		s = c.Chat.SysMemberJoined(x)
	case "member_left":
		s = c.Chat.SysMemberLeft(x)
	case "member_kicked":
		s = c.Chat.SysMemberKicked(x)
	case "group_member_removed":
		s = c.Chat.SysGroupMemberRemoved(x)
	case "channel_created":
		s = c.Chat.SysChannelCreated(e.Data.Name)
	case "channel_renamed":
		s = c.Chat.SysChannelRenamed(e.Data.Name)
	case "channel_topic_changed":
		s = c.Chat.SysTopicChanged(e.Data.Topic)
	case "message_pinned":
		s = c.Chat.SysMessagePinned()
	case "group_renamed":
		s = c.Chat.SysGroupRenamed(e.Data.Name)
	}
	return textwidth.Sanitize(s)
}

// rollText is "🎲 2d6 → 3 + 5 + 2 = 10" ("🎲 1d20 → 17" for a single term).
func rollText(r api.RollResult) string {
	terms := make([]string, 0, len(r.Rolls)+1)
	for _, n := range r.Rolls {
		terms = append(terms, strconv.Itoa(n))
	}
	expr := strings.Join(terms, " + ")
	switch {
	case r.Modifier > 0:
		expr += " + " + strconv.Itoa(r.Modifier)
	case r.Modifier < 0:
		expr += " - " + strconv.Itoa(-r.Modifier)
	}
	out := "🎲 " + textwidth.Sanitize(r.Expr) + " → "
	if len(r.Rolls) == 0 || (len(r.Rolls) == 1 && r.Modifier == 0) {
		return out + strconv.Itoa(r.Total)
	}
	return out + expr + " = " + strconv.Itoa(r.Total)
}

// nameOf is a user's display name from the store (unsanitized), or chat.unknown_user.
func nameOf(ctx *ui.Context, id string) string {
	if ctx != nil && ctx.Store != nil {
		if u := ctx.Store.User(id); u != nil {
			return displayName(*u)
		}
	}
	return cat(ctx).Chat.UnknownUser()
}

// displayName is the display name, falling back to the handle (unsanitized)
// when the screen would show nothing of it (textwidth.Blank): a name made only
// of runes Sanitize drops (Hangul fillers, zero-width, bidi, noncharacters) or
// blank-cell runes (U+2800) must not make a sender look nameless
// (pending-fixes 1).
func displayName(u api.UserSummary) string {
	if !textwidth.Blank(u.DisplayName) {
		return u.DisplayName
	}
	return u.Handle
}

func isMe(ctx *ui.Context, id string) bool {
	return ctx != nil && ctx.Store != nil && ctx.Store.IsMe(id)
}

func locOf(ctx *ui.Context) *time.Location {
	if ctx != nil && ctx.Loc != nil {
		return ctx.Loc
	}
	return time.Local
}

// clockOf is "11:05" (or "오전 11:05" with ui.clock = "12h").
func clockOf(ctx *ui.Context, at time.Time) string {
	h12 := ctx != nil && ctx.Config != nil && ctx.Config.UI.Clock == "12h"
	return hz(ctx).Clock(at, locOf(ctx), h12)
}

// ---- line helpers ----

// indentOf is the full-density body indent (none when too narrow).
func indentOf(width int) int {
	if width > fullIndent+1 {
		return fullIndent
	}
	return 0
}

// fit truncates a styled line to width (no padding).
func fit(s string, width int) string {
	if textwidth.Width(s) > width {
		return textwidth.Truncate(s, width, "")
	}
	return s
}

// spaces is n blanks in st.
func spaces(st lipgloss.Style, n int) string {
	if n <= 0 {
		return ""
	}
	return st.Render(strings.Repeat(" ", n))
}

// appendSuffix appends suffix to the last line when it fits, else on its own line.
// suffix is plain text starting with " " (dropped on its own line).
func appendSuffix(lines []string, suffix string, st lipgloss.Style, width int) []string {
	if n := len(lines); n > 0 && textwidth.Width(lines[n-1])+textwidth.Width(suffix) <= width {
		lines[n-1] += st.Render(suffix)
		return lines
	}
	return append(lines, st.Render(textwidth.Truncate(strings.TrimPrefix(suffix, " "), width, "…")))
}

// centerLine centers text (sanitized by the caller) in width with Base blanks.
func centerLine(t *theme.Theme, st lipgloss.Style, text string, width int) string {
	text = textwidth.Truncate(text, width, "…")
	w := textwidth.Width(text)
	left := (width - w) / 2
	return blank(t, left) + st.Render(text) + blank(t, width-w-left)
}

// rule is a "─"-filled divider of exactly width: label left ("── label ───…") or
// centered ("───── label ─────").
func rule(t *theme.Theme, st lipgloss.Style, label string, width int, centered bool) string {
	label = textwidth.Truncate(label, width, "…")
	rest := width - textwidth.Width(label)
	if !centered {
		return st.Render(label + strings.Repeat("─", rest))
	}
	left := rest / 2
	return st.Render(strings.Repeat("─", left) + label + strings.Repeat("─", rest-left))
}

// paint gives every line st's background (cells in the base background are
// recolored, other backgrounds — code, chips — stay) and pads it to width.
func paint(t *theme.Theme, lines []string, st lipgloss.Style, width int) []string {
	bg := st.GetBackground()
	if _, none := bg.(lipgloss.NoColor); none || bg == nil {
		return lines
	}
	for i, l := range lines {
		l = recolorBG(l, t.P.BG, bg)
		lines[i] = l + spaces(st, width-textwidth.Width(l))
	}
	return lines
}

// bgSGR is the truecolor SGR background parameter list of c ("48;2;r;g;b").
func bgSGR(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return "48;2;" + strconv.Itoa(int(r>>8)) + ";" + strconv.Itoa(int(g>>8)) + ";" + strconv.Itoa(int(b>>8))
}

// recolorBG rewrites every truecolor SGR background equal to from into to; other
// escapes (OSC 8 links, foregrounds, other backgrounds) are kept verbatim.
func recolorBG(s string, from, to color.Color) string {
	if from == nil || to == nil {
		return s
	}
	src := strings.Split(bgSGR(from), ";")
	dst := bgSGR(to)
	if bgSGR(from) == dst || !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = s[i:]
		j := 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j >= len(s) { // unterminated: keep as is
			b.WriteString(s)
			return b.String()
		}
		if s[j] != 'm' {
			b.WriteString(s[:j+1])
			s = s[j+1:]
			continue
		}
		params := strings.Split(s[2:j], ";")
		out := make([]string, 0, len(params))
		for k := 0; k < len(params); {
			p := params[k]
			switch {
			case (p == "38" || p == "48" || p == "58") && k+4 < len(params) && params[k+1] == "2":
				if p == "48" && slices.Equal(params[k:k+5], src) {
					out = append(out, dst)
				} else {
					out = append(out, params[k:k+5]...)
				}
				k += 5
			case (p == "38" || p == "48" || p == "58") && k+2 < len(params) && params[k+1] == "5":
				out = append(out, params[k:k+3]...)
				k += 3
			default:
				out = append(out, p)
				k++
			}
		}
		b.WriteString("\x1b[" + strings.Join(out, ";") + "m")
		s = s[j+1:]
	}
}

// tOf is ctx.T (nil when ctx is nil — errtext then says it in ko).
func tOf(ctx *ui.Context) *i18n.T {
	if ctx == nil {
		return nil
	}
	return ctx.T
}
