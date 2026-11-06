package feed

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Profile overlay ids (args "").
const (
	overlayFollowRequests = "feed.follow_requests"
	overlayProfileEdit    = "feed.profile_edit"
)

// Profile overlay tags (ui.Do).
const (
	tagFollowReqs     = "feed.follow_requests:me"
	tagFollowReqsMore = "feed.follow_requests_more:me"
	tagAcceptFollow   = "feed.accept_follow:"  // + user id
	tagDeclineFollow  = "feed.decline_follow:" // + user id
	tagProfileSave    = "feed.profile_save:"   // + my user id
)

// Profile overlay copy, limits and sizes.
const (
	textFollowReqsTitle = "팔로우 요청"
	textNoFollowReqs    = "대기 중인 팔로우 요청이 없어요"
	textAccepted        = "팔로우 요청을 수락했어요"
	textDeclined        = "팔로우 요청을 거절했어요"
	errContextFollowReq = "팔로우 요청"
	textEditTitle       = "프로필 편집"
	textEditName        = "이름"
	textEditBio         = "소개"
	textEditAvatar      = "아바타"
	textBioPlaceholder  = "나를 한 줄로 소개해 보세요"
	textAvatarHint      = "이미지 경로 · 비우면 그대로"
	textAvatarClear     = "저장하면 아바타를 지워요 · ctrl+x 취소"
	textEditKeys        = "tab 다음 칸 · ctrl+x 아바타 지우기 · enter 저장"
	textNameRequired    = "이름을 입력해 주세요"
	textProfileSaved    = "프로필을 저장했어요"
	errContextSave      = "프로필 저장"
	purposeAvatar       = "avatar"
	nameLimit           = 32
	bioLimit            = 160
	bioRows             = 3
	editW               = 56
	editH               = 13
	editLabelW          = 7 // "아바타 "
)

func init() {
	ui.RegisterOverlay(overlayFollowRequests, func(*ui.Context, string) ui.Overlay { return newFollowRequests() })
	ui.RegisterOverlay(overlayProfileEdit, func(ctx *ui.Context, _ string) ui.Overlay {
		return overlayOrNil(newProfileEdit(ctx))
	})
}

// ---- feed.follow_requests ----

// followRequestOutcome is an accept / decline of userID's follow request.
type followRequestOutcome struct {
	claim
	userID string
	accept bool
}

// followReqs is feed.follow_requests: my pending follow requests; a accepts,
// d declines (the row goes once the server agrees), enter opens the profile.
type followReqs struct {
	entries     []api.FollowRequestEntry
	shown       []api.FollowRequestEntry
	list        *widgets.List[api.FollowRequestEntry]
	busy        map[string]bool
	cursor      *string
	loading     bool
	loadingMore bool
	err         error
	spin        *widgets.Spinner
	size        ui.Size
}

var (
	_ ui.Overlay      = (*followReqs)(nil)
	_ ui.KeyHandler   = (*followReqs)(nil)
	_ ui.MouseHandler = (*followReqs)(nil)
	_ ui.Titled       = (*followReqs)(nil)
	_ ui.Helper       = (*followReqs)(nil)
)

func newFollowRequests() *followReqs {
	f := &followReqs{busy: map[string]bool{}, spin: widgets.NewSpinner()}
	f.list = widgets.NewList(func(e api.FollowRequestEntry) string { return e.User.ID }, f.renderRow)
	return f
}

func (f *followReqs) fetch(ctx *ui.Context, tag, cursor string) tea.Cmd {
	return ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.FollowRequestEntry], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListFollowRequests(c, api.PageOpts{Cursor: cursor, Limit: listPageLimit})
	})
}

func (f *followReqs) Init(ctx *ui.Context) tea.Cmd {
	f.loading = true
	return tea.Batch(f.spin.Start(ctx), f.fetch(ctx, tagFollowReqs, ""))
}

// refilter re-reads the entries into the List without blocked users.
func (f *followReqs) refilter(ctx *ui.Context) {
	f.shown = f.shown[:0:0]
	for _, e := range f.entries {
		if ctx == nil || ctx.Store == nil || !ctx.Store.IsBlocked(e.User.ID) {
			f.shown = append(f.shown, e)
		}
	}
	f.list.SetItems(f.shown)
}

func (f *followReqs) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	cmds := []tea.Cmd{applyResults(ctx, msg), f.spin.Update(ctx, msg)}
	switch m := msg.(type) {
	case ui.ResultMsg[*api.Page[api.FollowRequestEntry]]:
		switch m.Tag {
		case tagFollowReqs:
			f.loading = false
		case tagFollowReqsMore:
			f.loadingMore = false
		default:
			return tea.Batch(cmds...)
		}
		if !f.loading && !f.loadingMore {
			f.spin.Stop()
		}
		if m.Err != nil || m.Value == nil {
			f.err = m.Err
			cmds = append(cmds, profileErr(m.Err, errContextList))
			break
		}
		if m.Tag == tagFollowReqs {
			f.entries = nil
		}
		seen := map[string]bool{}
		for _, e := range f.entries {
			seen[e.User.ID] = true
		}
		for _, e := range m.Value.Items {
			if !seen[e.User.ID] {
				seen[e.User.ID] = true
				f.entries = append(f.entries, e)
			}
		}
		f.cursor = m.Value.NextCursor
		if len(m.Value.Items) == 0 {
			f.cursor = nil
		}
		f.refilter(ctx)
	case ui.ResultMsg[*followRequestOutcome]:
		o := m.Value
		if o == nil || (!strings.HasPrefix(m.Tag, tagAcceptFollow) && !strings.HasPrefix(m.Tag, tagDeclineFollow)) {
			break
		}
		delete(f.busy, o.userID)
		first := o.take()
		if m.Err != nil {
			if first {
				cmds = append(cmds, profileErr(m.Err, errContextFollowReq))
			}
			break
		}
		out := f.entries[:0:0]
		for _, e := range f.entries {
			if e.User.ID != o.userID {
				out = append(out, e)
			}
		}
		f.entries = out
		f.refilter(ctx)
		if first {
			text := textDeclined
			if o.accept {
				text = textAccepted
			}
			cmds = append(cmds, ui.Toast(text, ui.ToastSuccess))
		}
	default:
		if blockSignal(msg) {
			f.refilter(ctx)
		}
	}
	return tea.Batch(cmds...)
}

func (f *followReqs) Resize(_ *ui.Context, size ui.Size) { f.size = size }

func (f *followReqs) Bounds(body layout.Rect) layout.Rect {
	return centeredIn(body, min(userListW, body.W-4), min(userListH, body.H-2))
}

func (f *followReqs) Modal() bool { return true }

func (f *followReqs) Title(*ui.Context) (string, ui.Badge) { return textFollowReqsTitle, ui.Badge{} }

func (f *followReqs) Help(*ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	return []key.Binding{b("a", "수락"), b("d", "거절"), b("enter", "프로필")}
}

// answer accepts or declines the selected request (one request per user at a time).
func (f *followReqs) answer(ctx *ui.Context, accept bool) tea.Cmd {
	e, ok := f.list.Selected()
	if !ok || f.busy[e.User.ID] {
		return nil
	}
	id := e.User.ID
	f.busy[id] = true
	o := &followRequestOutcome{userID: id, accept: accept}
	tag := tagDeclineFollow + id
	if accept {
		tag = tagAcceptFollow + id
	}
	return ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*followRequestOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		if accept {
			return o, a.AcceptFollowRequest(c, api.UserID(id))
		}
		return o, a.DeclineFollowRequest(c, api.UserID(id))
	})
}

func (f *followReqs) maybeMore(ctx *ui.Context) tea.Cmd {
	if f.cursor == nil || f.loading || f.loadingMore {
		return nil
	}
	sel, ok := f.list.Selected()
	if !ok {
		return nil
	}
	for i, e := range f.shown {
		if e.User.ID == sel.User.ID && i >= len(f.shown)-moreThreshold {
			f.loadingMore = true
			return tea.Batch(f.spin.Start(ctx), f.fetch(ctx, tagFollowReqsMore, *f.cursor))
		}
	}
	return nil
}

func (f *followReqs) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case msg.String() == "a":
		return true, f.answer(ctx, true)
	case msg.String() == "d":
		return true, f.answer(ctx, false)
	case ctx.Keys.Matches(msg, keymap.ListOpen):
		e, ok := f.list.Selected()
		if !ok {
			return true, nil
		}
		return true, tea.Batch(closeOverlay, ui.Navigate(ui.Route{Kind: ui.RouteProfile, ID: e.User.ID}))
	}
	handled, cmd := f.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, f.maybeMore(ctx))
}

func (f *followReqs) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	return tea.Batch(f.list.HandleMouse(ctx, msg, local), f.maybeMore(ctx))
}

// renderRow is "@minji 민지 · 30분".
func (f *followReqs) renderRow(ctx *ui.Context, e api.FollowRequestEntry, selected bool, width int) []string {
	t := themeOf(ctx)
	text := t.S.Bold.Render(handleOf(e.User))
	if n := displayName(e.User); n != "" {
		named := e.User
		named.DisplayName = n
		text += t.S.Base.Render(" ") + widgets.UserName(t, named, false)
	}
	if ctx != nil && ctx.Now != nil {
		text += t.S.Timestamp.Render(" · " + humanize.Relative(ctx.Now(), e.CreatedAt))
	}
	return []string{selectedLine(t, text, selected, width)}
}

func (f *followReqs) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := f.size.W, f.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	switch {
	case len(f.shown) == 0 && (f.loading || f.err != nil):
		return blankRows(t, []string{statusLine(t, f.spin, f.loading, f.err, w)}, w, h)
	case len(f.shown) == 0:
		return widgets.Empty(t, f.size, "", textNoFollowReqs, "")
	}
	listH := h
	if f.loadingMore {
		listH = max(h-1, 0)
	}
	out := strings.Split(f.list.View(ctx, ui.Size{W: w, H: listH}), "\n")
	if f.loadingMore {
		out = append(out, statusLine(t, f.spin, true, nil, w))
	}
	return blankRows(t, out, w, h)
}

// ---- feed.profile_edit ----

// Edit fields in tab order.
const (
	fieldName = iota
	fieldBio
	fieldAvatar
	editFields
)

// profileEdit is feed.profile_edit: name (1..32), bio (0..160, N/160) and an
// avatar file path (empty = keep). ctrl+x marks the avatar for removal; enter
// saves only the fields that changed (an avatar path is uploaded first).
type profileEdit struct {
	meID        string
	origName    string
	origBio     string
	name        textinput.Model
	bio         *widgets.Composer
	avatar      textinput.Model
	focus       int
	clearAvatar bool
	saving      bool
	size        ui.Size
	cursor      *tea.Cursor // of the last View, inner-rect relative
}

var (
	_ ui.Overlay    = (*profileEdit)(nil)
	_ ui.KeyHandler = (*profileEdit)(nil)
	_ ui.Cursorer   = (*profileEdit)(nil)
	_ ui.Titled     = (*profileEdit)(nil)
	_ ui.Helper     = (*profileEdit)(nil)
)

// newProfileEdit is the dialog prefilled from Store.Me (nil before login).
func newProfileEdit(ctx *ui.Context) *profileEdit {
	me := ctx.Me()
	if me == nil {
		return nil
	}
	e := &profileEdit{meID: me.ID, name: newPathInput(), bio: widgets.NewComposer(textBioPlaceholder), avatar: newPathInput()}
	e.name.CharLimit = nameLimit
	e.name.SetValue(oneLine(me.DisplayName))
	e.name.CursorEnd()
	e.bio.SetValue(me.Bio)
	e.avatar.Blur()
	e.origName = strings.TrimSpace(e.name.Value())
	e.origBio = strings.TrimSpace(e.bio.Value())
	return e
}

func (e *profileEdit) Init(*ui.Context) tea.Cmd { return nil }

// setFocus moves the focus to field i (wrapping).
func (e *profileEdit) setFocus(i int) tea.Cmd {
	e.focus = (i%editFields + editFields) % editFields
	e.name.Blur()
	e.bio.Blur()
	e.avatar.Blur()
	switch e.focus {
	case fieldName:
		return e.name.Focus()
	case fieldBio:
		return e.bio.Focus()
	}
	return e.avatar.Focus()
}

// uploadErr is a PrepareUpload failure in the server's 415 / 413 wording.
func uploadErr(err error) error {
	switch {
	case errors.Is(err, imgrender.ErrUnsupported):
		return &api.Error{Code: "unsupported_media_type"}
	case errors.Is(err, imgrender.ErrTooLarge):
		return &api.Error{Code: "payload_too_large"}
	}
	return err
}

// save validates and sends the changed fields (PATCH /v1/me/profile, tag
// "feed.profile_save:<my id>"), uploading an avatar path first.
func (e *profileEdit) save(ctx *ui.Context) tea.Cmd {
	if e.saving {
		return nil
	}
	name := strings.TrimSpace(e.name.Value())
	if name == "" {
		return ui.Toast(textNameRequired, ui.ToastError)
	}
	if utf8.RuneCountInString(name) > nameLimit {
		return ui.Toast(tooLongText(nameLimit), ui.ToastError)
	}
	bio := strings.TrimSpace(e.bio.Value())
	if utf8.RuneCountInString(bio) > bioLimit {
		return ui.Toast(tooLongText(bioLimit), ui.ToastError)
	}
	file := strings.TrimSpace(e.avatar.Value())
	if file != "" {
		file = expandHome(file)
		if !fileExists(file) {
			return ui.Toast(textFileNotFound, ui.ToastError)
		}
	}
	var req api.UpdateProfileRequest
	if name != e.origName {
		req.DisplayName = api.Some(name)
	}
	if bio != e.origBio {
		req.Bio = api.Some(bio)
	}
	if e.clearAvatar && file == "" {
		req.AvatarUploadID = api.Null[string]()
	}
	if !req.DisplayName.Set && !req.Bio.Set && !req.AvatarUploadID.Set && file == "" {
		return closeOverlay
	}
	e.saving = true
	return ui.Do(ctx, tagProfileSave+e.meID, func(c context.Context, a *api.Client) (*api.Me, error) {
		if a == nil {
			return nil, errNoClient
		}
		if file != "" {
			data, ct, err := imgrender.PrepareUpload(file)
			if err != nil {
				return nil, uploadErr(err)
			}
			up, err := a.Upload(c, purposeAvatar, ct, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return nil, err
			}
			if up == nil {
				return nil, errNoClient
			}
			req.AvatarUploadID = api.Some(up.ID)
		}
		return a.UpdateProfile(c, req)
	})
}

// applySaved copies the saved name, bio and avatar into Store.Me (and my
// cached user).
func applySaved(ctx *ui.Context, r *api.Me) {
	me := ctx.Me()
	if me == nil || r == nil {
		return
	}
	me.DisplayName, me.Bio, me.AvatarURL = r.DisplayName, r.Bio, r.AvatarURL
	if u := ctx.Store.Users[me.ID]; u != nil {
		u.DisplayName, u.AvatarURL = r.DisplayName, r.AvatarURL
	}
}

func (e *profileEdit) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	switch m := msg.(type) {
	case tea.PasteMsg:
		cmds = append(cmds, e.input(ctx, m))
	case ui.ResultMsg[*api.Me]:
		if m.Tag != tagProfileSave+e.meID || !e.saving {
			break
		}
		e.saving = false
		if m.Err != nil || m.Value == nil {
			cmds = append(cmds, profileErr(m.Err, errContextSave))
			break
		}
		applySaved(ctx, m.Value)
		cmds = append(cmds, closeOverlay, ui.Toast(textProfileSaved, ui.ToastSuccess))
	}
	return tea.Batch(cmds...)
}

// input feeds a key or paste to the focused field (handled = it was used).
func (e *profileEdit) input(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	_, cmd := e.feed(ctx, msg)
	return cmd
}

func (e *profileEdit) feed(ctx *ui.Context, msg tea.Msg) (bool, tea.Cmd) {
	switch e.focus {
	case fieldBio:
		if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "ctrl+c" {
			had := e.bio.Value() != ""
			return had, e.bio.Update(ctx, msg)
		}
		return true, e.bio.Update(ctx, msg)
	case fieldAvatar:
		handled, cmd := inputUpdate(&e.avatar, msg)
		if strings.TrimSpace(e.avatar.Value()) != "" {
			e.clearAvatar = false
		}
		return handled, cmd
	}
	return inputUpdate(&e.name, msg)
}

func (e *profileEdit) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "tab":
		return true, e.setFocus(e.focus + 1)
	case "shift+tab":
		return true, e.setFocus(e.focus - 1)
	case "enter":
		return true, e.save(ctx)
	case "ctrl+x":
		e.clearAvatar = !e.clearAvatar
		if e.clearAvatar {
			e.avatar.Reset()
		}
		return true, nil
	case "esc":
		return false, nil
	}
	return e.feed(ctx, msg)
}

func (e *profileEdit) Resize(ctx *ui.Context, size ui.Size) {
	e.size = size
	_ = e.View(ctx, true) // cursor geometry for Cursor (which has no Context)
}

func (e *profileEdit) Bounds(body layout.Rect) layout.Rect {
	return centeredIn(body, min(editW, body.W-4), min(editH, body.H-2))
}

func (e *profileEdit) Modal() bool { return true }

func (e *profileEdit) Title(*ui.Context) (string, ui.Badge) { return textEditTitle, ui.Badge{} }

func (e *profileEdit) Help(*ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	return []key.Binding{b("tab", "다음 칸"), b("enter", "저장"), b("ctrl+x", "아바타 지우기"), b("esc", "취소")}
}

func (e *profileEdit) Cursor() *tea.Cursor {
	if e.cursor == nil {
		return nil
	}
	c := *e.cursor
	return &c
}

// labeled is "<label padded to editLabelW>" + field, exactly w cells.
func labeled(t *theme.Theme, label, field string, focused bool, w int) string {
	st := t.S.Dim
	if focused {
		st = t.S.Base.Foreground(t.P.Acc).Bold(true)
	}
	lw := min(editLabelW, w)
	return fit(t, st.Render(textwidth.PadRight(textwidth.Truncate(label, lw, ""), lw))+field, w)
}

func (e *profileEdit) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := e.size.W, e.size.H
	e.cursor = nil
	if w <= 0 || h <= 0 {
		return ""
	}
	fw := max(w-editLabelW, 0)
	rule := t.S.Base.Foreground(t.P.Line).Render(strings.Repeat("─", w))
	var rows []string
	curRow, curCol := -1, 0

	nameLine, nameCol := pathLine(t, e.name, "", fw)
	if e.focus == fieldName {
		curRow, curCol = len(rows), editLabelW+nameCol
	}
	rows = append(rows, labeled(t, textEditName, nameLine, e.focus == fieldName, w), rule)

	n := utf8.RuneCountInString(e.bio.Value())
	count := strconv.Itoa(n) + "/" + strconv.Itoa(bioLimit)
	cst := t.S.Dim
	if n > bioLimit {
		cst = t.S.Error
	}
	head := labeled(t, textEditBio, "", e.focus == fieldBio, max(w-textwidth.Width(count), 0))
	rows = append(rows, fit(t, head+cst.Render(count), w))
	box := strings.Split(e.bio.View(ctx, w, bioRows), "\n")
	if e.focus == fieldBio {
		if c := e.bio.Cursor(); c != nil {
			curRow, curCol = len(rows)+c.Position.Y, c.Position.X
		}
	}
	rows = append(rows, box...)

	var avatarLine string
	avatarCol := 0
	if e.clearAvatar {
		avatarLine = fit(t, t.S.Warning.Render(textwidth.Truncate(textAvatarClear, fw, "…")), fw)
	} else {
		avatarLine, avatarCol = pathLine(t, e.avatar, textAvatarHint, fw)
	}
	if e.focus == fieldAvatar {
		curRow, curCol = len(rows), editLabelW+avatarCol
	}
	rows = append(rows, labeled(t, textEditAvatar, avatarLine, e.focus == fieldAvatar, w), rule,
		fit(t, t.S.Dim.Render(textwidth.Truncate(textEditKeys, w, "…")), w))

	top := 0
	if curRow >= h {
		top = curRow - h + 1
	}
	if curRow >= 0 && fw > 0 {
		cur := tea.NewCursor(min(curCol, w-1), curRow-top)
		e.cursor = cur
	}
	return blankRows(t, rows[top:], w, h)
}
