package feed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteCompose, newCompose)
}

// Composer modes: Arg "story" is a story; otherwise Query["mode"] picks a reply
// or a quote of Route.ID, and anything else is a new post.
const (
	modePost  = "post"
	modeReply = "reply"
	modeQuote = "quote"
	modeStory = "story"
)

// Composer limits (code points, api.md 5.14–5.16).
const (
	postLimit      = 500
	storyLimit     = 200
	maxPostMedia   = 4
	composeMaxRows = 8 // input rows the composer grows to
	storyPreviewH  = 5 // rows of the story background preview
	chipNameW      = 20
)

// storyBGs is the "b" cycle (api.md 5.16 bg), starting at the default.
var storyBGs = []string{"accent", "accent2", "accent3", "accent4", "plain"}

// Composer copy.
const (
	textComposeNew    = "새 글"
	textComposeReply  = "답글"
	textComposeQuote  = "인용"
	textComposeStory  = "새 스토리"
	placeholderPost   = "무슨 생각을 하고 있나요?"
	placeholderStory  = "스토리에 짧게 남겨 보세요 · 24시간 뒤 사라져요"
	textEmptyPost     = "내용을 입력해 주세요"
	textMaxPhotos     = "사진은 4장까지예요"
	textReplyNoPoll   = "답글에는 투표를 붙일 수 없어요"
	textPosted        = "올렸어요"
	textStoryPosted   = "스토리를 올렸어요"
	textStillUpload   = "사진을 올리는 중이에요"
	textStillSending  = "올리는 중이에요 · 잠시만 기다려 주세요"
	textUploadFailed  = "✕ 업로드 실패"
	textDiscardAsk    = "작성 중인 글을 버릴까요?"
	textDiscardBody   = "쓴 내용과 첨부한 사진이 사라져요."
	textDiscardYes    = "버리기"
	textDiscardNo     = "계속 쓰기"
	textStoryBG       = "배경 "
	textPhotoCount    = "사진 "
	errContextCompose = "글 올리기"
	errContextStory   = "스토리 올리기"
)

// Composer result tags (ui.Do): "feed.<action>:<target>".
const (
	tagComposeSend   = "feed.compose:"        // + compose route key
	tagComposeTarget = "feed.compose_target:" // + compose route key
	tagDeleteUpload  = "feed.delete_upload:"  // + upload id
)

// tooLongText is the toast for a body over limit code points.
func tooLongText(limit int) string { return fmt.Sprintf("%d자를 넘었어요", limit) }

// attachment is one picked photo: uploading while Upload and Err are both nil.
type attachment struct {
	Name   string
	Size   int64
	Upload *api.Upload
	Err    error
	seq    int // matches the attachedMsg of its upload
}

func (a attachment) uploading() bool { return a.Upload == nil && a.Err == nil }

// attachedMsg is the outcome of one upload, delivered back to the compose
// instance whose route key is Route.
type attachedMsg struct {
	Route string
	A     attachment
}

// discardMsg clears the compose instance Route (the "버리기" of the confirm).
type discardMsg struct{ Route string }

// composeState is the Persistent state of a composer.
type composeState struct {
	Mode        string
	Body        string
	Attachments []attachment
	BG          string
	Nonce       string
}

// composeResult is a sent post or story.
type composeResult struct {
	Post   *api.Post
	Story  *api.Story
	Groups []api.StoryGroup // the story feed after a story (FeedOK)
	FeedOK bool
	Mode   string
	Target string
}

// compose is RouteCompose: a post (new / reply / quote) or a story.
type compose struct {
	key   string
	route ui.Route
	mode  string
	input *widgets.Composer
	atts  []attachment
	seq   int
	bg    string
	nonce string // client_nonce of the post being sent, reused until it succeeds
	spin  *widgets.Spinner
	size  ui.Size

	sending       bool
	targetLoading bool
	targetFailed  bool
	paneFocused   bool // the app's focus is on this instance (FocusChangedMsg)
	inputY        int  // first row of the input box in the last layout
	inputH        int  // input box height in the last layout (0 = not drawn)
}

var (
	_ ui.Component     = (*compose)(nil)
	_ ui.KeyHandler    = (*compose)(nil)
	_ ui.MouseHandler  = (*compose)(nil)
	_ ui.InputCapturer = (*compose)(nil)
	_ ui.Cursorer      = (*compose)(nil)
	_ ui.Persistent    = (*compose)(nil)
	_ ui.Titled        = (*compose)(nil)
	_ ui.Helper        = (*compose)(nil)
	_ ui.Watcher       = (*compose)(nil)
)

// modeOf is the composer mode of r.
func modeOf(r ui.Route) string {
	if r.Arg == modeStory {
		return modeStory
	}
	if r.ID != "" {
		switch r.Query["mode"] {
		case modeReply:
			return modeReply
		case modeQuote:
			return modeQuote
		}
	}
	return modePost
}

// newCompose builds RouteCompose (Arg "post"|"story", ID = reply / quote target,
// Query["mode"] = "reply"|"quote").
func newCompose(_ *ui.Context, r ui.Route) ui.Component {
	c := &compose{key: r.Key(), route: r, mode: modeOf(r), bg: storyBGs[0], spin: widgets.NewSpinner()}
	ph := placeholderPost
	if c.mode == modeStory {
		ph = placeholderStory
	}
	c.input = widgets.NewComposer(ph)
	return c
}

func (c *compose) fs(ctx *ui.Context) *feedstore.FeedStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return feedstore.Get(ctx.Store)
}

func (c *compose) story() bool { return c.mode == modeStory }

func (c *compose) limit() int {
	if c.story() {
		return storyLimit
	}
	return postLimit
}

func (c *compose) purpose() string {
	if c.story() {
		return purposeStory
	}
	return purposePost
}

// target is the reply / quote target post id ("" for a new post or a story).
func (c *compose) target() string {
	if c.mode == modeReply || c.mode == modeQuote {
		return c.route.ID
	}
	return ""
}

func (c *compose) targetPost(ctx *ui.Context) *api.Post {
	if id := c.target(); id != "" {
		return fsPost(c.fs(ctx), id)
	}
	return nil
}

// WatchTopics watches the reply / quote target the composer shows: the
// post.stats of what this composer sends goes only to topic:post:<target>
// (events.md 4.8), so without it the cached counts would stay stale.
func (c *compose) WatchTopics() []string {
	if id := c.target(); id != "" {
		return []string{"post:" + id}
	}
	return nil
}

// Init focuses the input and loads an uncached reply / quote target.
func (c *compose) Init(ctx *ui.Context) tea.Cmd {
	return tea.Batch(c.input.Focus(), c.loadTarget(ctx))
}

func (c *compose) loadTarget(ctx *ui.Context) tea.Cmd {
	id := c.target()
	if id == "" || c.targetLoading || c.targetPost(ctx) != nil {
		return nil
	}
	c.targetLoading, c.targetFailed = true, false
	return ui.Do(ctx, tagComposeTarget+c.key, func(cx context.Context, a *api.Client) (*api.Post, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.GetPost(cx, id)
	})
}

// ---- Update ----

func (c *compose) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	switch m := msg.(type) {
	case tea.PasteMsg:
		if !c.sending { // the draft being sent is read-only
			cmds = append(cmds, c.input.Update(ctx, m))
		}
	case ui.RouteUpdatedMsg:
		cmds = append(cmds, c.onRouteUpdated(ctx, m.Route))
	case ui.FocusChangedMsg:
		c.paneFocused = m.RouteKey == c.key
	case attachPickMsg:
		if m.Route == c.key {
			if c.sending {
				cmds = append(cmds, ui.Toast(textStillSending, ui.ToastInfo))
			} else {
				cmds = append(cmds, c.onPick(ctx, m))
			}
		}
	case attachedMsg:
		if m.Route == c.key {
			cmds = append(cmds, c.onAttached(ctx, m.A))
		}
	case discardMsg:
		// never while sending: the uploads belong to the in-flight request and
		// the post would be published anyway (leave refuses to ask then)
		if m.Route == c.key && !c.sending {
			cmds = append(cmds, c.discard(ctx))
		}
	case ui.ResultMsg[*api.Post]:
		if m.Tag == tagComposeTarget+c.key {
			cmds = append(cmds, c.onTarget(ctx, m))
		}
	case ui.ResultMsg[*composeResult]:
		if m.Tag == tagComposeSend+c.key {
			cmds = append(cmds, c.onSent(ctx, m))
		}
	}
	cmds = append(cmds, c.spin.Update(ctx, msg))
	return tea.Batch(cmds...)
}

// onRouteUpdated switches between reply and quote of the same post (one
// instance): the header follows, the draft and photos stay, the nonce of the
// other mode's request is dropped. The same mode changes nothing.
func (c *compose) onRouteUpdated(ctx *ui.Context, r ui.Route) tea.Cmd {
	c.route = r
	nm := modeOf(r)
	if c.story() || nm == c.mode {
		return nil
	}
	c.mode = nm
	c.nonce = ""
	return tea.Batch(c.loadTarget(ctx), c.input.Focus())
}

func (c *compose) onTarget(ctx *ui.Context, m ui.ResultMsg[*api.Post]) tea.Cmd {
	c.targetLoading = false
	if m.Err != nil || m.Value == nil {
		c.targetFailed = true
		if api.IsCode(m.Err, "not_found") {
			if fs := c.fs(ctx); fs != nil {
				fs.MarkDeleted(c.route.ID)
			}
			return nil
		}
		return postErr(m.Err)
	}
	if fs := c.fs(ctx); fs != nil {
		fs.ObservePosts(*m.Value)
	}
	return nil
}

// ---- attachments ----

// photos counts the attachments that are (or will be) sent.
func (c *compose) photos() int {
	n := 0
	for _, a := range c.atts {
		if a.Err == nil {
			n++
		}
	}
	return n
}

func (c *compose) uploadingAny() bool {
	for _, a := range c.atts {
		if a.uploading() {
			return true
		}
	}
	return false
}

// openAttach opens feed.attach for this instance (posts: at most 4 photos).
func (c *compose) openAttach() tea.Cmd {
	if !c.story() && c.photos() >= maxPostMedia {
		return ui.Toast(textMaxPhotos, ui.ToastInfo)
	}
	return ui.OpenRegisteredOverlay(overlayAttach, c.purpose()+" "+c.key)
}

// onPick starts uploading a picked file. A story keeps one photo: the new one
// replaces (and deletes) the old.
func (c *compose) onPick(ctx *ui.Context, m attachPickMsg) tea.Cmd {
	var cmds []tea.Cmd
	if c.story() {
		for _, a := range c.atts {
			cmds = append(cmds, deleteUpload(ctx, a.Upload))
		}
		c.atts = nil
	} else if c.photos() >= maxPostMedia {
		return ui.Toast(textMaxPhotos, ui.ToastInfo)
	}
	c.seq++
	a := attachment{Name: filepath.Base(m.Path), seq: c.seq}
	c.atts = append(c.atts, a)
	return tea.Batch(append(cmds, c.spin.Start(ctx), uploadFile(ctx, c.key, c.purpose(), m.Path, a))...)
}

// uploadFile prepares path (imgrender.PrepareUpload: metadata stripped) and
// uploads it off the UI goroutine; the outcome comes back as attachedMsg.
func uploadFile(ctx *ui.Context, route, purpose, path string, a attachment) tea.Cmd {
	var client *api.Client
	if ctx != nil {
		client = ctx.API
	}
	return func() tea.Msg {
		data, ct, err := imgrender.PrepareUpload(path)
		if err != nil {
			a.Err = err
			return attachedMsg{Route: route, A: a}
		}
		a.Size = int64(len(data))
		if client == nil {
			a.Err = errNoClient
			return attachedMsg{Route: route, A: a}
		}
		a.Upload, a.Err = client.Upload(context.Background(), purpose, ct, bytes.NewReader(data), a.Size)
		if a.Err != nil {
			a.Upload = nil
		} else if a.Upload == nil {
			a.Err = errNoClient
		}
		return attachedMsg{Route: route, A: a}
	}
}

// onAttached stores an upload outcome; one whose chip is already gone (removed
// with x, or replaced) is deleted on the server.
func (c *compose) onAttached(ctx *ui.Context, a attachment) tea.Cmd {
	defer func() {
		if !c.uploadingAny() {
			c.spin.Stop()
		}
	}()
	for i := range c.atts {
		if c.atts[i].seq == a.seq && c.atts[i].uploading() {
			c.atts[i] = a
			return attachErr(a.Err)
		}
	}
	return deleteUpload(ctx, a.Upload)
}

// attachErr reports a failed upload: a missing file, an image imgrender cannot
// use (as the server's 415 / 413 wording) or the API error.
func attachErr(err error) tea.Cmd {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return ui.Toast(textFileNotFound, ui.ToastError)
	case errors.Is(err, imgrender.ErrUnsupported):
		err = &api.Error{Code: "unsupported_media_type"}
	case errors.Is(err, imgrender.ErrTooLarge):
		err = &api.Error{Code: "payload_too_large"}
	}
	return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: textAttachTitle} }
}

// deleteUpload drops a pending upload (DELETE /v1/uploads/{id}); errors are
// ignored (the server removes unattached uploads after 24h anyway).
func deleteUpload(ctx *ui.Context, up *api.Upload) tea.Cmd {
	if up == nil || up.ID == "" {
		return nil
	}
	id := up.ID
	return ui.Do(ctx, tagDeleteUpload+id, func(cx context.Context, a *api.Client) (struct{}, error) {
		if a == nil {
			return struct{}{}, errNoClient
		}
		return struct{}{}, a.DeleteUpload(cx, id)
	})
}

// removeLast is x: the last chip goes (its upload is deleted).
func (c *compose) removeLast(ctx *ui.Context) tea.Cmd {
	n := len(c.atts)
	if n == 0 {
		return nil
	}
	a := c.atts[n-1]
	c.atts = c.atts[:n-1]
	return deleteUpload(ctx, a.Upload)
}

func (c *compose) mediaIDs() []string {
	var out []string
	for _, a := range c.atts {
		if a.Upload != nil {
			out = append(out, a.Upload.ID)
		}
	}
	return out
}

// ---- drafts ----

func (c *compose) hasDraft() bool { return c.input.Value() != "" || len(c.atts) > 0 }

func (c *compose) reset() {
	c.input.Reset()
	c.atts = nil
	c.nonce = ""
	c.bg = storyBGs[0]
	c.sending = false
	c.spin.Stop()
}

// discard is "버리기": the pending uploads are deleted and the draft cleared.
func (c *compose) discard(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	for _, a := range c.atts {
		cmds = append(cmds, deleteUpload(ctx, a.Upload))
	}
	c.reset()
	return tea.Batch(append(cmds, c.input.Focus())...)
}

// leave is esc outside the input: a draft asks first, else back.
func (c *compose) leave() tea.Cmd {
	if !c.hasDraft() {
		return ui.Back()
	}
	route := c.key
	reset := func() tea.Msg { return discardMsg{Route: route} }
	// "계속 쓰기" (or esc) closes the dialog straight back into the input.
	return tea.Batch(c.input.Focus(), ui.OpenOverlay(widgets.Confirm(textDiscardAsk, textDiscardBody, textDiscardYes, textDiscardNo, tea.Batch(reset, ui.Back()))))
}

func (c *compose) cycleBG() {
	for i, b := range storyBGs {
		if b == c.bg {
			c.bg = storyBGs[(i+1)%len(storyBGs)]
			return
		}
	}
	c.bg = storyBGs[0]
}

// ---- sending ----

// isPollInput reports a body that is a /poll command.
func isPollInput(body string) bool {
	rest, ok := strings.CutPrefix(body, "/"+string(slash.Poll))
	return ok && (rest == "" || strings.TrimLeft(rest, " \t\n") != rest)
}

// send posts the draft in the current mode (or the story). Over-long and empty
// drafts, a reply poll and a bad /poll are refused without a request.
func (c *compose) send(ctx *ui.Context) tea.Cmd {
	if c.sending {
		return nil
	}
	if c.uploadingAny() {
		return ui.Toast(textStillUpload, ui.ToastInfo)
	}
	body := c.input.Value()
	text := strings.TrimSpace(body)
	media := c.mediaIDs()
	if c.story() {
		if utf8.RuneCountInString(body) > storyLimit {
			return ui.Toast(tooLongText(storyLimit), ui.ToastInfo)
		}
		if text == "" && len(media) == 0 {
			return ui.Toast(textEmptyPost, ui.ToastInfo)
		}
		req := api.CreateStoryRequest{Body: text, BG: c.bg}
		if len(media) > 0 {
			req.MediaUploadID = media[0]
		}
		return c.do(ctx, func(cx context.Context, a *api.Client) (*composeResult, error) {
			st, err := a.CreateStory(cx, req)
			if err != nil {
				return nil, err
			}
			r := &composeResult{Story: st, Mode: modeStory}
			if gs, err := a.StoryFeed(cx); err == nil {
				r.Groups, r.FeedOK = gs, true
			}
			return r, nil
		})
	}
	var poll *api.PollInput
	if isPollInput(body) {
		if c.mode == modeReply {
			return ui.Toast(textReplyNoPoll, ui.ToastInfo)
		}
		cmd, ok, err := slash.Parse(body)
		if err != nil {
			return ui.Toast(slash.Message(ctx.T, err), ui.ToastInfo)
		}
		if ok && cmd.Poll != nil {
			poll, text = cmd.Poll, ""
		}
	}
	if poll == nil && utf8.RuneCountInString(body) > postLimit {
		return ui.Toast(tooLongText(postLimit), ui.ToastInfo)
	}
	if poll == nil && text == "" && len(media) == 0 && c.mode != modeQuote {
		return ui.Toast(textEmptyPost, ui.ToastInfo)
	}
	if c.nonce == "" {
		c.nonce = ids.New()
	}
	req := api.CreatePostRequest{ClientNonce: c.nonce, Body: text, MediaUploadIDs: media, Poll: poll}
	mode, target := c.mode, c.target()
	switch mode {
	case modeReply:
		req.ReplyToID = target
	case modeQuote:
		req.QuoteOfID = target
	}
	return c.do(ctx, func(cx context.Context, a *api.Client) (*composeResult, error) {
		p, _, err := a.CreatePost(cx, req)
		if err != nil {
			return nil, err
		}
		return &composeResult{Post: p, Mode: mode, Target: target}, nil
	})
}

func (c *compose) do(ctx *ui.Context, fn func(context.Context, *api.Client) (*composeResult, error)) tea.Cmd {
	c.sending = true
	return ui.Do(ctx, tagComposeSend+c.key, func(cx context.Context, a *api.Client) (*composeResult, error) {
		if a == nil {
			return nil, errNoClient
		}
		return fn(cx, a)
	})
}

// onSent caches what was made, clears the draft and goes back (when this
// composer still has the focus).
func (c *compose) onSent(ctx *ui.Context, m ui.ResultMsg[*composeResult]) tea.Cmd {
	if !c.sending {
		return nil
	}
	c.sending = false
	if m.Err != nil || m.Value == nil {
		errCtx := errContextCompose
		if c.story() {
			errCtx = errContextStory
		}
		err := m.Err
		if err == nil {
			err = errNoClient
		}
		return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: errCtx} }
	}
	r := m.Value
	done := textPosted
	if fs := c.fs(ctx); fs != nil {
		if r.Post != nil {
			fs.ObservePosts(*r.Post)
			if r.Mode == modeReply && r.Target != "" {
				if fs.HasReplies(r.Target) {
					fs.Replies(r.Target).Upsert(*r.Post)
				} else {
					// a window holding only this reply: "답글 더 보기" loads the rest
					fs.Replies(r.Target).Replace([]api.Post{*r.Post}, false, true)
				}
			}
		}
		if r.Story != nil && r.FeedOK {
			fs.SetStories(r.Groups)
		}
	}
	if r.Story != nil {
		done = textStoryPosted
	}
	c.reset()
	cmds := []tea.Cmd{ui.Toast(done, ui.ToastSuccess), c.input.Focus()}
	if c.paneFocused {
		cmds = append(cmds, ui.Back())
	}
	return tea.Batch(cmds...)
}

// ---- layout & rendering ----

// composeLayout is what fits in the pane, top to bottom: header, target card,
// story preview, input, poll preview, chips, blank rows, footer (bottom row).
type composeLayout struct {
	header  bool
	context []string
	preview int
	maxRows int
	inputH  int
	poll    []string
	chips   []string
	footer  bool
}

// layout fills the pane by priority: input (one row), footer, header (tiny only:
// elsewhere the heading is the pane border title), chips,
// poll preview, target card, story preview, then more input rows.
func (c *compose) layout(ctx *ui.Context) composeLayout {
	w, h := c.size.W, c.size.H
	var l composeLayout
	if w < 3 || h < 3 {
		return l
	}
	l.maxRows = 1
	l.inputH = c.input.Height(w, 1)
	budget := h - l.inputH
	if budget >= 1 {
		l.footer = true
		budget--
	}
	if budget >= 1 && ctx != nil && ctx.Mode == layout.ModeTiny {
		l.header = true
		budget--
	}
	take := func(lines []string) []string {
		n := min(len(lines), budget)
		budget -= n
		return lines[:n]
	}
	l.chips = take(c.chipLines(ctx, w))
	l.poll = take(c.pollLines(ctx, w))
	if cl := c.contextLines(ctx, w); len(cl) <= budget {
		l.context = cl
		budget -= len(cl)
	}
	if c.story() && budget >= 3 {
		l.preview = min(storyPreviewH, budget)
		budget -= l.preview
	}
	if budget > 0 {
		l.maxRows = 1 + min(budget, composeMaxRows-1)
		l.inputH = c.input.Height(w, l.maxRows)
	}
	return l
}

// heading is the header text: 새 글 / 답글 · @hana / 인용 / 새 스토리.
func (c *compose) heading(ctx *ui.Context) string {
	switch c.mode {
	case modeStory:
		return textComposeStory
	case modeQuote:
		return textComposeQuote
	case modeReply:
		if p := c.targetPost(ctx); p != nil && !p.Deleted && p.Author.Handle != "" && !c.blocked(ctx, p) {
			return textComposeReply + " · @" + textwidth.Sanitize(p.Author.Handle)
		}
		return textComposeReply
	}
	return textComposeNew
}

func (c *compose) blocked(ctx *ui.Context, p *api.Post) bool {
	return p != nil && ctx != nil && ctx.Store != nil && ctx.Store.IsBlocked(p.Author.ID)
}

// contextLines is the target card: a reply's parent as one DensityLine row, a
// quote's target as the first two lines of its card behind a "│ " bar.
func (c *compose) contextLines(ctx *ui.Context, w int) []string {
	if c.target() == "" {
		return nil
	}
	th := themeOf(ctx)
	p := c.targetPost(ctx)
	switch {
	case p == nil && c.targetFailed:
		return []string{fit(th, th.S.Base.Render("  ")+th.S.Deleted.Render(textNotVisible), w)}
	case p == nil:
		return []string{fit(th, th.S.Base.Render("  ")+c.spin.View(th)+th.S.Dim.Render(" "+textLoading), w)}
	case c.blocked(ctx, p):
		return []string{fit(th, th.S.Base.Render("  ")+th.S.Deleted.Render(textNotVisible), w)}
	}
	it := api.TimelineItem{ID: p.ID, Type: "post", Post: *p}
	if c.mode == modeReply {
		v := renderCard(ctx, it, p, cardOpts{Width: w, Density: ui.DensityLine, PollSel: -1})
		if len(v.Lines) == 0 {
			return nil
		}
		return []string{fit(th, v.Lines[0], w)}
	}
	bar := th.S.Dim.Render("│ ")
	if w-2 <= gutterW {
		return []string{fit(th, bar, w)}
	}
	v := renderCard(ctx, it, p, cardOpts{Width: w - 2, Density: ui.DensityFull, PollSel: -1})
	var out []string
	for _, l := range v.Lines {
		if len(out) == 2 || isRule(l) {
			break
		}
		out = append(out, fit(th, bar+l, w))
	}
	return out
}

// pollLines is the /poll preview (0 votes), its usage error, and the reply
// warning. Stories have none.
func (c *compose) pollLines(ctx *ui.Context, w int) []string {
	body := c.input.Value()
	if c.story() || !isPollInput(body) {
		return nil
	}
	th := themeOf(ctx)
	warn := func(s string) []string {
		var out []string
		for _, l := range textwidth.Wrap(s, w) {
			out = append(out, fit(th, th.S.Warning.Render(l), w))
		}
		return out
	}
	cmd, _, err := slash.Parse(body)
	if err != nil {
		return warn(slash.Message(tOf(ctx), err))
	}
	if cmd == nil || cmd.Poll == nil {
		return nil
	}
	pl := api.Poll{Question: cmd.Poll.Question, Multi: cmd.Poll.Multi, MyVotes: []int{}}
	for i, o := range cmd.Poll.Options {
		pl.Options = append(pl.Options, api.PollOption{Idx: i, Text: o})
	}
	var out []string
	for _, l := range widgets.PollView(th, pl, w, -1) {
		out = append(out, fit(th, l, w))
	}
	if c.mode == modeReply {
		out = append(out, warn(textReplyNoPoll)...)
	}
	return out
}

// humanBytes is a short size: 812B, 3.4KB, 1.2MB.
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 10*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024:
		return fmt.Sprintf("%dKB", n/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}

// chip is "🖼 photo.jpg 1.2MB ✓", "🖼 photo.jpg ⠋" or "🖼 photo.jpg ✕ 업로드 실패".
func (c *compose) chip(th *theme.Theme, a attachment) string {
	s := th.S.Base.Render("🖼 " + textwidth.Truncate(textwidth.Sanitize(a.Name), chipNameW, "…") + " ")
	switch {
	case a.uploading():
		return s + c.spin.View(th)
	case a.Err != nil:
		return s + th.S.Error.Render(textUploadFailed)
	}
	size := a.Size
	if size == 0 && a.Upload != nil {
		size = int64(a.Upload.Size)
	}
	return s + th.S.Dim.Render(humanBytes(size)) + th.S.Base.Render(" ") + th.S.Success.Render("✓")
}

// chipLines packs the chips into lines of w cells (two blanks apart).
func (c *compose) chipLines(ctx *ui.Context, w int) []string {
	if len(c.atts) == 0 {
		return nil
	}
	th := themeOf(ctx)
	sep := th.S.Base.Render("  ")
	var out []string
	cur, curW := "", 0
	for _, a := range c.atts {
		ch := c.chip(th, a)
		cw := textwidth.Width(ch)
		if cw > w {
			ch, cw = textwidth.Truncate(ch, w, "…"), w
		}
		switch {
		case curW == 0:
			cur, curW = ch, cw
		case curW+2+cw <= w:
			cur, curW = cur+sep+ch, curW+2+cw
		default:
			out = append(out, fit(th, cur, w))
			cur, curW = ch, cw
		}
	}
	return append(out, fit(th, cur, w))
}

// previewLines is the story background box with the text centered.
func (c *compose) previewLines(ctx *ui.Context, w, n int) []string {
	if n <= 0 {
		return nil
	}
	th := themeOf(ctx)
	fg := th.P.BG
	if c.bg == "plain" {
		fg = th.P.FG
	}
	st := th.S.Base.Background(th.StoryBG(c.bg)).Foreground(fg).Bold(true)
	var text []string
	if v := textwidth.Sanitize(c.input.Value()); v != "" {
		text = textwidth.Wrap(v, max(w-4, 1))
	}
	if len(text) > n {
		text = text[:n]
	}
	top := (n - len(text)) / 2
	out := make([]string, n)
	for i := range out {
		line := ""
		if j := i - top; j >= 0 && j < len(text) {
			line = text[j]
		}
		out[i] = st.Render(textwidth.Center(line, w))
	}
	return out
}

// footer is the bottom row: the story background (or the photo count) on the
// left, the "N/500" counter (S.Error over the limit) at the right. Key hints are
// the status bar's (Help).
func (c *compose) footer(ctx *ui.Context, w int) string {
	th := themeOf(ctx)
	n := utf8.RuneCountInString(c.input.Value())
	count := fmt.Sprintf("%d/%d", n, c.limit())
	cst := th.S.Dim
	if n > c.limit() {
		cst = th.S.Error
	}
	cw := textwidth.Width(count)
	if cw >= w {
		return fit(th, cst.Render(textwidth.Truncate(count, w, "")), w)
	}
	info := ""
	switch {
	case c.story():
		info = textStoryBG + c.bg
	case len(c.atts) > 0:
		info = fmt.Sprintf("%s%d/%d", textPhotoCount, c.photos(), maxPostMedia)
	}
	left := fit(th, th.S.Dim.Render(textwidth.Truncate(info, max(w-cw-1, 0), "…")), w-cw)
	return left + cst.Render(count)
}

func (c *compose) View(ctx *ui.Context, _ bool) string {
	th := themeOf(ctx)
	w, h := c.size.W, c.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	l := c.layout(ctx)
	var out []string
	if l.header {
		out = append(out, fit(th, th.S.Title.Render(c.heading(ctx)), w))
	}
	// (the heading is the pane title; only tiny, which draws no borders, repeats it inside)
	out = append(out, l.context...)
	out = append(out, c.previewLines(ctx, w, l.preview)...)
	c.inputY, c.inputH = len(out), 0
	if l.inputH > 0 {
		in := strings.Split(c.input.View(ctx, w, l.maxRows), "\n")
		c.inputH = len(in)
		out = append(out, in...)
	}
	out = append(out, l.poll...)
	out = append(out, l.chips...)
	if l.footer {
		for len(out) < h-1 {
			out = append(out, fit(th, "", w))
		}
		out = append(out[:min(len(out), h-1)], c.footer(ctx, w))
	}
	return blankRows(th, out, w, h)
}

// Resize keeps the input geometry current for Cursor (which has no Context).
func (c *compose) Resize(ctx *ui.Context, size ui.Size) {
	c.size = size
	_ = c.View(ctx, false)
}

// ---- input ----

func (c *compose) focusInput() tea.Cmd { return c.input.Focus() }

func (c *compose) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if c.sending {
		return c.handleKeySending(ctx, msg)
	}
	km := ctx.Keys
	s := msg.String()
	if c.input.Focused() {
		switch {
		case s == "ctrl+c":
			had := c.input.Value() != ""
			return had, c.input.Update(ctx, msg)
		case s == "ctrl+o":
			return true, c.openAttach()
		case km.Matches(msg, keymap.ComposeSend):
			return true, c.send(ctx)
		case km.Matches(msg, keymap.ComposeLeave):
			c.input.Blur()
			return true, nil
		}
		return true, c.input.Update(ctx, msg)
	}
	switch {
	case km.Matches(msg, keymap.ComposeFocus), s == "enter":
		return true, c.focusInput()
	case s == "o" || s == "ctrl+o":
		return true, c.openAttach()
	case s == "x":
		return true, c.removeLast(ctx)
	case s == "b" && c.story():
		c.cycleBG()
		return true, nil
	case km.Matches(msg, keymap.ComposeLeave):
		return true, c.leave()
	}
	return false, nil
}

// handleKeySending is HandleKey while the draft is being sent: the draft is
// read-only (onSent clears what was sent; CreatePost refers to the uploads),
// so typing, x, o / ctrl+o and b do nothing, and esc cannot leave (the
// "버리기" dialog would delete uploads of a post that is published anyway).
// esc in the input still ends the input, and ctrl+c goes to the app.
func (c *compose) handleKeySending(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	s := msg.String()
	switch {
	case s == "ctrl+c":
		return false, nil
	case c.input.Focused():
		if km.Matches(msg, keymap.ComposeLeave) {
			c.input.Blur()
		}
		return true, nil
	case km.Matches(msg, keymap.ComposeFocus), s == "enter":
		return true, c.focusInput()
	case s == "o", s == "ctrl+o", s == "x", s == "b" && c.story(), km.Matches(msg, keymap.ComposeLeave):
		return true, ui.Toast(textStillSending, ui.ToastInfo)
	}
	return false, nil
}

// HandleMouse: a click on the input box focuses it.
func (c *compose) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if m, ok := msg.(tea.MouseClickMsg); !ok || m.Button != tea.MouseLeft {
		return nil
	}
	if c.inputH > 0 && local.Y >= c.inputY && local.Y < c.inputY+c.inputH && !c.input.Focused() {
		return c.focusInput()
	}
	return nil
}

func (c *compose) CapturesInput() bool { return c.input.Focused() }

// Cursor is the input cursor relative to the pane (geometry of the last layout).
func (c *compose) Cursor() *tea.Cursor {
	if !c.input.Focused() || c.inputH == 0 {
		return nil
	}
	cur := c.input.Cursor()
	if cur == nil {
		return nil
	}
	cur.Position.Y += c.inputY
	cur.Position.X = min(max(cur.Position.X, 0), c.size.W-1)
	cur.Position.Y = min(max(cur.Position.Y, 0), c.size.H-1)
	return cur
}

// ---- optional capabilities ----

func (c *compose) Title(ctx *ui.Context) (string, ui.Badge) { return c.heading(ctx), ui.Badge{} }

func (c *compose) Help(ctx *ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	if c.input.Focused() {
		return []key.Binding{b("enter", "올리기"), b("ctrl+o", "사진"), b("esc", "입력 끝")}
	}
	out := []key.Binding{b("i", "쓰기"), b("o", "사진"), b("x", "사진 빼기")}
	if c.story() {
		out = append(out, b("b", "배경"))
	}
	return append(out, b("esc", "닫기"))
}

// SaveState is {Mode, Body, Attachments, BG, Nonce}.
func (c *compose) SaveState() any {
	return composeState{
		Mode:        c.mode,
		Body:        c.input.Value(),
		Attachments: append([]attachment(nil), c.atts...),
		BG:          c.bg,
		Nonce:       c.nonce,
	}
}

// LoadState restores a saved draft. Uploads that were in flight are dropped
// (their outcome went to the released instance); a nonce made for another
// mode is not reused.
func (c *compose) LoadState(v any) {
	st, ok := v.(composeState)
	if !ok {
		return
	}
	c.input.SetValue(st.Body)
	c.atts = nil
	for _, a := range st.Attachments {
		if !a.uploading() {
			c.atts = append(c.atts, a)
		}
		c.seq = max(c.seq, a.seq)
	}
	if st.BG != "" {
		c.bg = st.BG
	}
	if st.Mode == c.mode {
		c.nonce = st.Nonce
	}
}
