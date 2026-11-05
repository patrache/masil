package feed

import (
	"context"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// overlayPollVote is the feed poll vote overlay; args "<postID>[ <option idx>]"
// (the idx, from a click on a multi-choice option, starts pre-checked).
const overlayPollVote = "feed.poll_vote"

// Poll result tags (ui.Do): "feed.<action>:<poll id>".
const (
	tagVote        = "feed.vote:"
	tagRetractVote = "feed.retract_vote:"
	tagClosePoll   = "feed.close_poll:"
	tagGetPoll     = "feed.poll:"
)

// Poll copy.
const (
	textPollTitle    = "투표"
	textPollMenu     = "투표하기"
	textPollClosed   = "마감된 투표예요"
	textPollCloseAsk = "투표를 마감할까요?"
	textPollCloseYes = "마감"
	errContextPoll   = "투표"
)

// Vote overlay outer size: width (clamped to the body) and minimum height.
const (
	pollVoteW    = 56
	pollVoteMinH = 6
)

func init() {
	ui.RegisterOverlay(overlayPollVote, func(ctx *ui.Context, args string) ui.Overlay {
		return pollVoteOverlay(ctx, args)
	})
}

// pollOutcome is a vote / retract / close / refresh request on a post poll; poll
// is the server's answer (authoritative, my_votes included).
type pollOutcome struct {
	claim
	pollID string
	postID string
	poll   *api.Poll
}

func isPollTag(tag string) bool {
	for _, p := range []string{tagVote, tagRetractVote, tagClosePoll, tagGetPoll} {
		if strings.HasPrefix(tag, p) {
			return true
		}
	}
	return false
}

func nowOf(ctx *ui.Context) time.Time {
	if ctx != nil && ctx.Now != nil {
		return ctx.Now()
	}
	return time.Now()
}

// pollClosed reports a poll closed by its creator or past closes_at.
func pollClosed(ctx *ui.Context, pl *api.Poll) bool {
	if pl == nil {
		return true
	}
	return pl.Closed || (pl.ClosesAt != nil && !nowOf(ctx).Before(*pl.ClosesAt))
}

// pollCall runs fn as a poll request of p tagged tag + poll id. A deleted post
// or a closed poll answers with a toast and sends nothing (unless open).
func pollCall(ctx *ui.Context, p *api.Post, tag string, open bool,
	fn func(c context.Context, a *api.Client, pollID string) (*api.Poll, error)) tea.Cmd {
	if p == nil {
		return nil
	}
	if p.Deleted {
		return ui.Toast(textDeleted, ui.ToastInfo)
	}
	if p.Poll == nil {
		return nil
	}
	if !open && pollClosed(ctx, p.Poll) {
		return ui.Toast(textPollClosed, ui.ToastInfo)
	}
	o := &pollOutcome{pollID: p.Poll.ID, postID: p.ID}
	return ui.Do(ctx, tag+o.pollID, func(c context.Context, a *api.Client) (*pollOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		pl, err := fn(c, a, o.pollID)
		o.poll = pl
		return o, err
	})
}

// votePoll sends PUT /v1/polls/{id}/vote {"option_idxs": idxs} for p's poll
// (tag "feed.vote:<pollID>"); the answer replaces the cached poll.
func votePoll(ctx *ui.Context, p *api.Post, idxs []int) tea.Cmd {
	idxs = append([]int{}, idxs...)
	return pollCall(ctx, p, tagVote, false, func(c context.Context, a *api.Client, id string) (*api.Poll, error) {
		return a.Vote(c, id, idxs)
	})
}

// retractVote is DELETE /v1/polls/{id}/vote (tag "feed.retract_vote:<pollID>").
func retractVote(ctx *ui.Context, p *api.Post) tea.Cmd {
	return pollCall(ctx, p, tagRetractVote, false, func(c context.Context, a *api.Client, id string) (*api.Poll, error) {
		return a.RetractVote(c, id)
	})
}

// closePoll is POST /v1/polls/{id}/close (creator; tag "feed.close_poll:<pollID>").
func closePoll(ctx *ui.Context, p *api.Post) tea.Cmd {
	return pollCall(ctx, p, tagClosePoll, false, func(c context.Context, a *api.Client, id string) (*api.Poll, error) {
		return a.ClosePoll(c, id)
	})
}

// refreshPoll is GET /v1/polls/{id} (tag "feed.poll:<pollID>").
func refreshPoll(ctx *ui.Context, p *api.Post) tea.Cmd {
	return pollCall(ctx, p, tagGetPoll, true, func(c context.Context, a *api.Client, id string) (*api.Poll, error) {
		return a.GetPoll(c, id)
	})
}

// applyPoll is applyResults for poll requests: the answer goes to
// FeedStore.SetPoll (my_votes authoritative). 409 poll_closed marks the cached
// poll closed, toasts "마감된 투표예요" and refreshes it with GET /v1/polls/{id};
// a failed refresh keeps the cache quietly; other errors report ui.ErrorMsg.
func applyPoll(ctx *ui.Context, m ui.ResultMsg[*pollOutcome]) tea.Cmd {
	o := m.Value
	if !isPollTag(m.Tag) || !o.take() {
		return nil
	}
	var fs *feedstore.FeedStore
	if ctx != nil && ctx.Store != nil {
		fs = feedstore.Get(ctx.Store)
	}
	if m.Err != nil {
		switch {
		case api.IsCode(m.Err, "poll_closed"):
			var p *api.Post
			if fs != nil {
				p = fs.Post(o.postID)
			}
			if p != nil && p.Poll != nil && p.Poll.ID == o.pollID {
				pl := *p.Poll
				pl.Closed = true
				fs.SetPoll(pl)
			}
			if p == nil {
				p = &api.Post{ID: o.postID, Poll: &api.Poll{ID: o.pollID}}
			}
			return tea.Batch(ui.Toast(textPollClosed, ui.ToastInfo), refreshPoll(ctx, p))
		case strings.HasPrefix(m.Tag, tagGetPoll):
			return nil
		}
		return func() tea.Msg { return ui.ErrorMsg{Err: m.Err, Context: errContextPoll} }
	}
	if o.poll != nil && fs != nil {
		fs.SetPoll(*o.poll)
	}
	return nil
}

// pollHit is a click on a card's poll option idx: a single-choice poll votes
// for it at once, a multi-choice one opens the overlay with it pre-checked.
func pollHit(ctx *ui.Context, p *api.Post, value string) tea.Cmd {
	if p == nil {
		return nil
	}
	idx, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	if !p.Deleted && p.Poll != nil && p.Poll.Multi && !pollClosed(ctx, p.Poll) {
		return ui.OpenRegisteredOverlay(overlayPollVote, p.ID+" "+value)
	}
	return votePoll(ctx, p, []int{idx})
}

// ---- feed.poll_vote ----

// pollVote is feed.poll_vote: the options as ( )/(•) (single) or [ ]/[x]
// (multi) beside widgets.PollView bars. j/k move, space picks, enter votes,
// x retracts my vote, c closes the poll (its creator, after a Confirm). A
// closed poll is read-only: "마감된 투표예요" and the bars.
type pollVote struct {
	postID string
	pollID string
	picks  map[int]bool // option idx -> picked
	sel    int          // option slice position
	top    int
	rows   int // content rows wanted (Bounds has no context: kept by Update / View)
	size   ui.Size
}

var (
	_ ui.Overlay      = (*pollVote)(nil)
	_ ui.KeyHandler   = (*pollVote)(nil)
	_ ui.MouseHandler = (*pollVote)(nil)
	_ ui.Titled       = (*pollVote)(nil)
	_ ui.Helper       = (*pollVote)(nil)
)

// pollVoteOverlay is newPollVote as a ui.Overlay (a nil interface when it fails).
func pollVoteOverlay(ctx *ui.Context, args string) ui.Overlay {
	if v := newPollVote(ctx, args); v != nil {
		return v
	}
	return nil
}

// newPollVote parses "<postID>[ <option idx>]"; nil when the post is not cached,
// deleted or has no poll.
func newPollVote(ctx *ui.Context, args string) *pollVote {
	f := strings.Fields(args)
	if len(f) == 0 || ctx == nil || ctx.Store == nil {
		return nil
	}
	fs := feedstore.Get(ctx.Store)
	if fs == nil {
		return nil
	}
	p := fs.Post(f[0])
	if p == nil || p.Deleted || p.Poll == nil {
		return nil
	}
	v := &pollVote{postID: p.ID, pollID: p.Poll.ID}
	v.resetPicks(p.Poll)
	v.rows = v.wantRows(ctx)
	pre := -1
	if len(f) > 1 {
		if n, err := strconv.Atoi(f[1]); err == nil {
			pre = n
		}
	}
	at := -1
	for i, o := range p.Poll.Options {
		if o.Idx == pre {
			at = i
		}
	}
	if at >= 0 {
		if !p.Poll.Multi {
			clear(v.picks)
		}
		v.picks[pre] = true
		v.sel = at
		return v
	}
	for i, o := range p.Poll.Options {
		if v.picks[o.Idx] {
			v.sel = i
			break
		}
	}
	return v
}

// resetPicks picks exactly my votes.
func (v *pollVote) resetPicks(pl *api.Poll) {
	v.picks = map[int]bool{}
	if pl == nil {
		return
	}
	for _, i := range pl.MyVotes {
		v.picks[i] = true
	}
}

// post is the cached post when it still carries this poll.
func (v *pollVote) post(ctx *ui.Context) *api.Post {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	fs := feedstore.Get(ctx.Store)
	if fs == nil {
		return nil
	}
	p := fs.Post(v.postID)
	if p == nil || p.Deleted || p.Poll == nil || p.Poll.ID != v.pollID {
		return nil
	}
	return p
}

func (v *pollVote) Init(*ui.Context) tea.Cmd { return nil }

func (v *pollVote) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	cmd := applyResults(ctx, msg)
	if m, ok := msg.(ui.ResultMsg[*pollOutcome]); ok && m.Err == nil && m.Value != nil && m.Value.pollID == v.pollID {
		if p := v.post(ctx); p != nil {
			v.resetPicks(p.Poll)
		}
	}
	v.rows = v.wantRows(ctx)
	return cmd
}

func (v *pollVote) Resize(_ *ui.Context, size ui.Size) { v.size = size }

// wantRows is the question header and one row per option (+ the closed notice).
func (v *pollVote) wantRows(ctx *ui.Context) int {
	p := v.post(ctx)
	if p == nil {
		return 1
	}
	n := 1 + len(p.Poll.Options)
	if pollClosed(ctx, p.Poll) {
		n++
	}
	return n
}

func (v *pollVote) Bounds(body layout.Rect) layout.Rect {
	// At least 6 rows outer: a shorter box would lose its border (CA 5.3.7).
	return centeredIn(body, min(pollVoteW, body.W-4), max(v.rows+2, pollVoteMinH))
}

func (v *pollVote) Modal() bool { return true }

func (v *pollVote) Title(*ui.Context) (string, ui.Badge) { return textPollTitle, ui.Badge{} }

func (v *pollVote) Help(ctx *ui.Context) []key.Binding {
	p := v.post(ctx)
	esc := key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기"))
	if p == nil || pollClosed(ctx, p.Poll) {
		return []key.Binding{esc}
	}
	out := []key.Binding{
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "고르기")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "투표")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "투표 취소")),
	}
	if ctx.Store.IsMe(p.Poll.CreatorID) {
		out = append(out, key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "마감")))
	}
	return append(out, esc)
}

// toggle picks option slice position i (radio for single choice).
func (v *pollVote) toggle(pl *api.Poll, i int) {
	if i < 0 || i >= len(pl.Options) {
		return
	}
	idx := pl.Options[i].Idx
	if pl.Multi {
		v.picks[idx] = !v.picks[idx]
		return
	}
	clear(v.picks)
	v.picks[idx] = true
}

// picked is the picked option idxs in option order; the cursor's when none.
func (v *pollVote) picked(pl *api.Poll) []int {
	var out []int
	for _, o := range pl.Options {
		if v.picks[o.Idx] {
			out = append(out, o.Idx)
		}
	}
	if len(out) == 0 && v.sel >= 0 && v.sel < len(pl.Options) {
		out = []int{pl.Options[v.sel].Idx}
	}
	return out
}

func (v *pollVote) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	p := v.post(ctx)
	km := ctx.Keys
	nav := km.Matches(msg, keymap.ListUp) || km.Matches(msg, keymap.ListDown) ||
		km.Matches(msg, keymap.ListTop) || km.Matches(msg, keymap.ListBottom)
	s := msg.String()
	act := nav || km.Matches(msg, keymap.ListOpen) || s == "space" || s == "x" || s == "c"
	if p == nil || pollClosed(ctx, p.Poll) {
		return act, nil // read-only: swallow the vote keys
	}
	pl := p.Poll
	last := len(pl.Options) - 1
	switch {
	case km.Matches(msg, keymap.ListUp):
		v.sel = max(v.sel-1, 0)
	case km.Matches(msg, keymap.ListDown):
		v.sel = min(v.sel+1, last)
	case km.Matches(msg, keymap.ListTop):
		v.sel = 0
	case km.Matches(msg, keymap.ListBottom):
		v.sel = last
	case s == "space":
		v.toggle(pl, v.sel)
	case km.Matches(msg, keymap.ListOpen):
		return true, tea.Batch(closeOverlay, votePoll(ctx, p, v.picked(pl)))
	case s == "x":
		if len(pl.MyVotes) == 0 {
			return true, nil
		}
		return true, retractVote(ctx, p)
	case s == "c":
		if !ctx.Store.IsMe(pl.CreatorID) {
			return true, nil
		}
		return true, ui.OpenOverlay(widgets.Confirm(textPollCloseAsk, "", textPollCloseYes, textCancel, closePoll(ctx, p)))
	default:
		return false, nil
	}
	return true, nil
}

// HandleMouse: a click on an option row moves there and picks it.
func (v *pollVote) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft {
		return nil
	}
	p := v.post(ctx)
	if p == nil || pollClosed(ctx, p.Poll) {
		return nil
	}
	if i := v.top + local.Y - 1; local.Y >= 1 && i < len(p.Poll.Options) {
		v.sel = i
		v.toggle(p.Poll, i)
	}
	return nil
}

func (v *pollVote) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := v.size.W, v.size.H
	v.rows = v.wantRows(ctx)
	if w <= 0 || h <= 0 {
		return ""
	}
	p := v.post(ctx)
	if p == nil {
		return blankRows(t, []string{t.S.Deleted.Render(textwidth.Truncate(textDeleted, w, "…"))}, w, h)
	}
	pl := *p.Poll
	if pollClosed(ctx, &pl) {
		out := []string{t.S.Dim.Render(textwidth.Truncate(textPollClosed, w, "…"))}
		out = append(out, widgets.PollView(t, pl, w, -1)...)
		return blankRows(t, out, w, h)
	}

	// Rows below the question header keep the cursor in view.
	room := max(h-1, 1)
	if v.sel < v.top {
		v.top = v.sel
	}
	if v.sel >= v.top+room {
		v.top = v.sel - room + 1
	}
	head := widgets.PollView(t, pl, w, -1)
	out := []string{}
	if len(head) > 0 {
		out = append(out, head[0])
	}
	const markW = 4 // "(•) " / "[x] "
	bars := widgets.PollView(t, pl, max(w-markW, 0), v.sel)
	for i := v.top; i < len(pl.Options) && len(out) < h; i++ {
		o := pl.Options[i]
		mark := "( )"
		switch {
		case pl.Multi && v.picks[o.Idx]:
			mark = "[x]"
		case pl.Multi:
			mark = "[ ]"
		case v.picks[o.Idx]:
			mark = "(•)"
		}
		st := t.S.Base
		if i == v.sel {
			st = t.S.Selected
		}
		markSt := st
		if v.picks[o.Idx] {
			markSt = st.Foreground(t.P.Acc)
		}
		line := markSt.Render(mark) + st.Render(" ")
		if i+1 < len(bars) {
			line += bars[i+1]
		}
		out = append(out, line)
	}
	return blankRows(t, out, w, h)
}
