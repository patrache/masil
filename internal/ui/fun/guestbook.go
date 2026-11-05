// Package fun is the sns-fun "fun" UI (CA 6.2): status messages, presence
// mode, focus mode and its summary, and the guestbook write dialog.
package fun

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// overlayGuestbookWrite is the guestbook write dialog; args = the owner's user id.
const overlayGuestbookWrite = "fun.guestbook_write"

// Guestbook dialog result tags (ui.Do). A written entry is broadcast as
// ui.ResultMsg[*api.GuestbookEntry]{Tag: tagGuestbookWrite + ownerID}; the
// profile's guestbook tab (ui/feed) puts it on top.
const (
	tagGuestbookWrite = "fun.guestbook_write:" // + owner id
	tagGuestbookOwner = "fun.guestbook_owner:" // + owner id: the owner's handle for the title
)

// Guestbook dialog copy, limits and sizes.
const (
	textGuestbookTitle   = "방명록 남기기"
	textGuestbookHint    = "주인에게 한마디 남겨 보세요"
	textGuestbookSecret  = "비밀글로 남기기"
	textGuestbookKeys    = "enter 남기기 · tab 이동 · space 비밀글"
	textGuestbookSending = "보내는 중…"
	textGuestbookEmpty   = "내용을 입력해 주세요"
	textGuestbookWritten = "방명록을 남겼어요"
	guestbookLimit       = 300 // api.md 0.3: 1..300 code points
	guestbookW           = 56
	guestbookH           = 12
	guestbookMaxRows     = 5
)

func init() {
	ui.RegisterOverlay(overlayGuestbookWrite, func(ctx *ui.Context, args string) ui.Overlay {
		if g := newGuestbookWrite(ctx, strings.TrimSpace(args)); g != nil {
			return g
		}
		return nil
	})
}

// tooLongText is the over-limit toast.
func tooLongText(limit int) string { return strconv.Itoa(limit) + "자를 넘었어요" }

// guestbookWrite is fun.guestbook_write: a Composer (1..300, N/300) above the
// [ ] 비밀글로 남기기 toggle. tab moves between them, space flips the toggle,
// enter sends. A refusal (403 private_account / action_not_allowed, …) is a
// toast and the dialog stays with its draft.
type guestbookWrite struct {
	ownerID   string
	owner     string // "@handle" ("" until known)
	input     *widgets.Composer
	secret    bool
	onToggle  bool
	busy      bool
	askedName bool
	size      ui.Size
	toggleY   int         // toggle row of the last View (-1 = not drawn)
	inputY    [2]int      // composer rows [from, to) of the last View
	cursor    *tea.Cursor // of the last View, inner-rect relative
}

var (
	_ ui.Overlay       = (*guestbookWrite)(nil)
	_ ui.KeyHandler    = (*guestbookWrite)(nil)
	_ ui.MouseHandler  = (*guestbookWrite)(nil)
	_ ui.InputCapturer = (*guestbookWrite)(nil)
	_ ui.Cursorer      = (*guestbookWrite)(nil)
	_ ui.Titled        = (*guestbookWrite)(nil)
	_ ui.Helper        = (*guestbookWrite)(nil)
)

// newGuestbookWrite is the dialog for ownerID's guestbook (nil without an id).
func newGuestbookWrite(ctx *ui.Context, ownerID string) *guestbookWrite {
	if ownerID == "" {
		return nil
	}
	g := &guestbookWrite{ownerID: ownerID, input: widgets.NewComposer(textGuestbookHint), toggleY: -1}
	g.input.Focus()
	g.owner = knownHandle(ctx, ownerID)
	return g
}

// knownHandle is "@handle" of a user the Store knows (me included), else "".
func knownHandle(ctx *ui.Context, id string) string {
	if ctx == nil || ctx.Store == nil {
		return ""
	}
	if me := ctx.Me(); me != nil && me.ID == id {
		return "@" + textwidth.Sanitize(me.Handle)
	}
	if u := ctx.Store.Users[id]; u != nil {
		return "@" + textwidth.Sanitize(u.Handle)
	}
	return ""
}

// Init focuses the input and, for an owner the Store does not know, fetches
// the profile for the title.
func (g *guestbookWrite) Init(ctx *ui.Context) tea.Cmd {
	cmds := []tea.Cmd{g.input.Focus()}
	if g.owner == "" && !g.askedName {
		g.askedName = true
		id := g.ownerID
		cmds = append(cmds, ui.Do(ctx, tagGuestbookOwner+id, func(c context.Context, a *api.Client) (*api.Profile, error) {
			if a == nil {
				return nil, &api.Error{Code: "network"}
			}
			return a.GetUser(c, api.UserID(id))
		}))
	}
	return tea.Batch(cmds...)
}

func (g *guestbookWrite) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if !g.onToggle && !g.busy {
			return g.input.Update(ctx, m)
		}
	case ui.ResultMsg[*api.Profile]:
		if m.Tag == tagGuestbookOwner+g.ownerID && m.Err == nil && m.Value != nil && g.owner == "" {
			g.owner = "@" + textwidth.Sanitize(m.Value.Handle)
		}
	case ui.ResultMsg[*api.GuestbookEntry]:
		if m.Tag != tagGuestbookWrite+g.ownerID || !g.busy {
			return nil
		}
		g.busy = false
		if m.Err != nil || m.Value == nil {
			err := m.Err
			if err == nil {
				err = &api.Error{Code: "network"}
			}
			return ui.Toast(api.Describe(err), ui.ToastError)
		}
		return tea.Batch(closeOverlay, ui.Toast(textGuestbookWritten, ui.ToastSuccess))
	}
	return nil
}

func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

// submit validates the body (1..300 code points after trimming) and sends it.
func (g *guestbookWrite) submit(ctx *ui.Context) tea.Cmd {
	if g.busy {
		return nil
	}
	body := strings.TrimSpace(g.input.Value())
	switch n := utf8.RuneCountInString(body); {
	case n == 0:
		return ui.Toast(textGuestbookEmpty, ui.ToastError)
	case n > guestbookLimit:
		return ui.Toast(tooLongText(guestbookLimit), ui.ToastError)
	}
	g.busy = true
	owner, secret := g.ownerID, g.secret
	return ui.Do(ctx, tagGuestbookWrite+owner, func(c context.Context, a *api.Client) (*api.GuestbookEntry, error) {
		if a == nil {
			return nil, &api.Error{Code: "network"}
		}
		return a.WriteGuestbook(c, api.UserID(owner), body, secret)
	})
}

// setToggle moves the focus to the toggle (true) or the input.
func (g *guestbookWrite) setToggle(on bool) tea.Cmd {
	g.onToggle = on
	if on {
		g.input.Blur()
		return nil
	}
	return g.input.Focus()
}

func (g *guestbookWrite) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s := msg.String()
	if s == "esc" || s == "ctrl+k" {
		return false, nil // the app closes the dialog (and opens the palette for ctrl+k)
	}
	if g.busy {
		return true, nil
	}
	switch s {
	case "tab", "shift+tab":
		return true, g.setToggle(!g.onToggle)
	case "enter":
		return true, g.submit(ctx)
	}
	if g.onToggle {
		if s == "space" || s == "x" {
			g.secret = !g.secret
		}
		return true, nil
	}
	if s == "ctrl+c" {
		had := g.input.Value() != ""
		return had, g.input.Update(ctx, msg)
	}
	return true, g.input.Update(ctx, msg)
}

// HandleMouse: a click on the toggle flips it, a click in the input focuses it.
func (g *guestbookWrite) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft || g.busy {
		return nil
	}
	switch {
	case g.toggleY >= 0 && local.Y == g.toggleY:
		g.secret = !g.secret
		return g.setToggle(true)
	case local.Y >= g.inputY[0] && local.Y < g.inputY[1]:
		return g.setToggle(false)
	}
	return nil
}

func (g *guestbookWrite) CapturesInput() bool { return true }

func (g *guestbookWrite) Resize(ctx *ui.Context, size ui.Size) {
	g.size = size
	_ = g.View(ctx, true) // cursor geometry for Cursor (which has no Context)
}

func (g *guestbookWrite) Bounds(body layout.Rect) layout.Rect {
	w, h := max(min(guestbookW, body.W-4, body.W), 0), max(min(guestbookH, body.H-2, body.H), 0)
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-h)/2, W: w, H: h}
}

func (g *guestbookWrite) Modal() bool { return true }

// Title is "방명록 남기기 · @owner".
func (g *guestbookWrite) Title(*ui.Context) (string, ui.Badge) {
	if g.owner == "" {
		return textGuestbookTitle, ui.Badge{}
	}
	return textGuestbookTitle + " · " + g.owner, ui.Badge{}
}

func (g *guestbookWrite) Help(*ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	return []key.Binding{b("enter", "남기기"), b("tab", "이동"), b("space", "비밀글"), b("esc", "취소")}
}

func (g *guestbookWrite) Cursor() *tea.Cursor {
	if g.cursor == nil || g.onToggle || g.busy {
		return nil
	}
	c := *g.cursor
	return &c
}

func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
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

// View is the N/300 counter, the input box, the toggle and the key hint, cut
// to the inner size so the focused row stays visible.
func (g *guestbookWrite) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := g.size.W, g.size.H
	g.cursor, g.toggleY, g.inputY = nil, -1, [2]int{}
	if w <= 0 || h <= 0 {
		return ""
	}
	pad := 0
	if w >= 12 {
		pad = 1
	}
	cw := max(w-2*pad, 1)
	var rows []string
	n := utf8.RuneCountInString(g.input.Value())
	count := strconv.Itoa(n) + "/" + strconv.Itoa(guestbookLimit)
	cst := t.S.Dim
	if n > guestbookLimit {
		cst = t.S.Error
	}
	rows = append(rows, t.S.Base.Render(strings.Repeat(" ", max(cw-textwidth.Width(count), 0)))+cst.Render(count))

	maxRows := min(max(h-5, 1), guestbookMaxRows)
	inTop := len(rows)
	rows = append(rows, strings.Split(g.input.View(ctx, cw, maxRows), "\n")...)
	inEnd := len(rows)
	focusRow := inTop
	curCol := -1
	if c := g.input.Cursor(); c != nil && !g.onToggle {
		focusRow, curCol = inTop+c.Position.Y, pad+c.Position.X
	}

	mark, st, prefix := "[ ] ", t.S.Base, t.S.Base.Render("  ")
	if g.secret {
		mark = "[x] "
	}
	if g.onToggle {
		st, prefix = t.S.Base.Foreground(t.P.Acc).Bold(true), t.S.Base.Foreground(t.P.Acc).Render("› ")
	}
	toggleRow := len(rows)
	rows = append(rows, prefix+st.Render(textwidth.Truncate(mark+textGuestbookSecret, max(cw-2, 0), "…")))
	if g.onToggle {
		focusRow = toggleRow
	}
	hint := textGuestbookKeys
	if g.busy {
		hint = textGuestbookSending
	}
	rows = append(rows, t.S.Dim.Render(textwidth.Truncate(hint, cw, "…")))

	top := 0
	if focusRow >= h {
		top = focusRow - h + 1
	}
	if toggleRow-top >= 0 && toggleRow-top < h {
		g.toggleY = toggleRow - top
	}
	g.inputY = [2]int{max(inTop-top, 0), max(min(inEnd-top, h), 0)}
	if curCol >= 0 && focusRow-top >= 0 && focusRow-top < h {
		g.cursor = tea.NewCursor(min(curCol, w-1), focusRow-top)
	}
	side := t.S.Base.Render(strings.Repeat(" ", pad))
	out := make([]string, 0, h)
	for i := top; i < top+h; i++ {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		out = append(out, side+fit(t, l, cw)+side)
	}
	for i := range out {
		out[i] = fit(t, out[i], w)
	}
	return strings.Join(out, "\n")
}
