package chat

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// ---- shared overlay plumbing (menus and forms) ----

// Overlay outer widths (CA 5.3.7: the app clamps to the body and degrades small ones).
const (
	menuMaxW = 40
	formMaxW = 56
)

// themeOf is ctx's theme (the default theme without one).
func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}

// centered is a w x h rect centered in body (the app clamps it to body).
func centered(body layout.Rect, w, h int) layout.Rect {
	w, h = min(w, body.W), min(h, body.H)
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

// closeOverlay is the Cmd closing the top overlay.
func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

// overlayPad is the left/right content margin inside an overlay (none when very narrow).
func overlayPad(w int) int {
	if w >= 12 {
		return 1
	}
	return 0
}

// blank is n Base styled spaces ("" for n <= 0).
func blank(t *theme.Theme, n int) string {
	if n <= 0 {
		return ""
	}
	return t.S.Base.Render(strings.Repeat(" ", n))
}

// cell renders s in st truncated (with "…") to w cells and padded with st blanks to exactly w.
func cell(st func(...string) string, s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = textwidth.Truncate(s, w, "…")
	return st(s + strings.Repeat(" ", max(0, w-textwidth.Width(s))))
}

// renderOverlayRows fits rows (already styled, each at most size.W-2*pad cells) into
// exactly size.H lines of size.W cells, every cell styled.
func renderOverlayRows(t *theme.Theme, rows []string, size ui.Size) string {
	p := overlayPad(size.W)
	cw := size.W - 2*p
	out := make([]string, 0, size.H)
	for i := range size.H {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		l = textwidth.Truncate(l, cw, "")
		out = append(out, blank(t, p)+l+blank(t, cw-textwidth.Width(l))+blank(t, p))
	}
	return strings.Join(textwidth.Fit(out, size.W, size.H), "\n")
}

// morph holds the overlay that replaced its owner in place (menu item Run, form
// OnDone): once set, the owner delegates Bounds/View/HandleKey/Update/Cursor/... to
// it, so closing-then-opening never races (tea.Sequence is banned).
type morph struct {
	next ui.Overlay
	size ui.Size // last size given to the owner
}

// become installs o (Init + Resize to the current size) and returns its Init Cmd.
func (m *morph) become(ctx *ui.Context, o ui.Overlay) tea.Cmd {
	m.next = o
	cmd := o.Init(ctx)
	if m.size.W > 0 || m.size.H > 0 {
		o.Resize(ctx, m.size)
	}
	return cmd
}

// finish is the end of an action: morph into o when it is non-nil, else close and run cmd.
func (m *morph) finish(ctx *ui.Context, cmd tea.Cmd, o ui.Overlay) tea.Cmd {
	if o != nil {
		return tea.Batch(cmd, m.become(ctx, o))
	}
	return tea.Batch(closeOverlay, cmd)
}

func (m *morph) key(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if kh, ok := m.next.(ui.KeyHandler); ok {
		return kh.HandleKey(ctx, msg)
	}
	return false, nil
}

func (m *morph) mouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if mh, ok := m.next.(ui.MouseHandler); ok {
		return mh.HandleMouse(ctx, msg, local)
	}
	return nil
}

func (m *morph) cursor() *tea.Cursor {
	if c, ok := m.next.(ui.Cursorer); ok {
		return c.Cursor()
	}
	return nil
}

func (m *morph) title(ctx *ui.Context) (string, ui.Badge) {
	if t, ok := m.next.(ui.Titled); ok {
		return t.Title(ctx)
	}
	return "", ui.Badge{}
}

func (m *morph) help(ctx *ui.Context) []key.Binding {
	if h, ok := m.next.(ui.Helper); ok {
		return h.Help(ctx)
	}
	return nil
}

func (m *morph) captures() bool {
	if c, ok := m.next.(ui.InputCapturer); ok {
		return c.CapturesInput()
	}
	return false
}

// ---- menu ----

// menuItem is one menu row. Run returns a Cmd and optionally an overlay that
// replaces the menu in place (nil overlay = the menu closes). A nil Run just closes.
type menuItem struct {
	Label, Hint      string
	Danger, Disabled bool
	Run              func(ctx *ui.Context) (tea.Cmd, ui.Overlay)
}

// menu is a centered, modal action list (newMenu).
type menu struct {
	morph
	title string
	items []menuItem
	cur   int // cursor item (-1 = no enabled item)

	// last layout (mouse hit testing)
	headRows, top int
}

var (
	_ ui.Overlay       = (*menu)(nil)
	_ ui.KeyHandler    = (*menu)(nil)
	_ ui.MouseHandler  = (*menu)(nil)
	_ ui.Titled        = (*menu)(nil)
	_ ui.Helper        = (*menu)(nil)
	_ ui.Cursorer      = (*menu)(nil)
	_ ui.InputCapturer = (*menu)(nil)
)

// newMenu is a menu overlay: j/k/↑/↓ move (disabled rows are skipped), enter or a
// click runs, a key equal to an item's Hint runs that item, esc closes. Disabled rows
// are S.Dim, dangerous ones S.Error, hints right-aligned S.Dim.
func newMenu(title string, items []menuItem) ui.Overlay {
	m := &menu{title: title, items: items, cur: -1}
	m.cur = m.nextEnabled(-1, 1)
	return m
}

// nextEnabled is the first enabled index after from in direction dir, or from when none.
func (m *menu) nextEnabled(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(m.items); i += dir {
		if !m.items[i].Disabled {
			return i
		}
	}
	return from
}

func (m *menu) Init(*ui.Context) tea.Cmd { return nil }

func (m *menu) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if m.next != nil {
		return m.next.Update(ctx, msg)
	}
	return nil
}

func (m *menu) Resize(ctx *ui.Context, size ui.Size) {
	m.size = size
	if m.next != nil {
		m.next.Resize(ctx, size)
	}
}

func (m *menu) Bounds(body layout.Rect) layout.Rect {
	if m.next != nil {
		return m.next.Bounds(body)
	}
	return centered(body, min(body.W, menuMaxW), len(m.items)+2)
}

func (m *menu) Modal() bool {
	if m.next != nil {
		return m.next.Modal()
	}
	return true
}

func (m *menu) Title(ctx *ui.Context) (string, ui.Badge) {
	if m.next != nil {
		return m.morph.title(ctx)
	}
	return m.title, ui.Badge{}
}

func (m *menu) Help(ctx *ui.Context) []key.Binding {
	if m.next != nil {
		return m.morph.help(ctx)
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", cat(ctx).Chat.HelpRun())),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", cat(ctx).Common.HelpMove())),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", cat(ctx).Common.HelpClose())),
	}
}

func (m *menu) Cursor() *tea.Cursor {
	if m.next != nil {
		return m.morph.cursor()
	}
	return nil
}

func (m *menu) CapturesInput() bool { return m.next != nil && m.captures() }

// run runs item i (enabled only).
func (m *menu) run(ctx *ui.Context, i int) tea.Cmd {
	if i < 0 || i >= len(m.items) || m.items[i].Disabled {
		return nil
	}
	m.cur = i
	it := m.items[i]
	if it.Run == nil {
		return closeOverlay
	}
	cmd, o := it.Run(ctx)
	return m.finish(ctx, cmd, o)
}

func (m *menu) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.next != nil {
		return m.morph.key(ctx, msg)
	}
	switch {
	case ctx.Keys.Matches(msg, keymap.NavBack):
		return false, nil // the app closes the menu
	case ctx.Keys.Matches(msg, keymap.ListUp):
		m.cur = m.nextEnabled(m.cur, -1)
		return true, nil
	case ctx.Keys.Matches(msg, keymap.ListDown):
		m.cur = m.nextEnabled(m.cur, 1)
		return true, nil
	case ctx.Keys.Matches(msg, keymap.ListOpen):
		return true, m.run(ctx, m.cur)
	}
	s := msg.String()
	for i, it := range m.items {
		if it.Hint != "" && it.Hint == s && !it.Disabled {
			return true, m.run(ctx, i)
		}
	}
	return false, nil
}

func (m *menu) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if m.next != nil {
		return m.morph.mouse(ctx, msg, local)
	}
	switch ev := msg.(type) {
	case tea.MouseClickMsg:
		if ev.Button != tea.MouseLeft {
			return nil
		}
		if i := m.top + local.Y - m.headRows; local.Y >= m.headRows && i < len(m.items) {
			return m.run(ctx, i)
		}
	case tea.MouseWheelMsg:
		switch ev.Button {
		case tea.MouseWheelUp:
			m.cur = m.nextEnabled(m.cur, -1)
		case tea.MouseWheelDown:
			m.cur = m.nextEnabled(m.cur, 1)
		}
	}
	return nil
}

func (m *menu) View(ctx *ui.Context, focused bool) string {
	if m.next != nil {
		return m.next.View(ctx, focused)
	}
	w, h := m.size.W, m.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t := themeOf(ctx)
	cw := w - 2*overlayPad(w)
	// The title is on the border; when the app drew none (degraded to the whole
	// body) there are spare rows, and the title goes inside.
	var head []string
	if h > len(m.items) {
		head = append(head, cell(t.S.Title.Render, textwidth.Sanitize(m.title), cw))
		if h > len(m.items)+1 {
			head = append(head, "")
		}
	}
	avail := max(0, h-len(head))
	top := 0
	if m.cur >= avail {
		top = m.cur - avail + 1
	}
	top = max(0, min(top, len(m.items)-avail))
	m.headRows, m.top = len(head), top
	rows := head
	for i := top; i < len(m.items) && i < top+avail; i++ {
		rows = append(rows, m.row(t, i, cw))
	}
	return renderOverlayRows(t, rows, m.size)
}

// row draws item i in exactly cw cells: marker, label, right-aligned hint.
func (m *menu) row(t *theme.Theme, i, cw int) string {
	it := m.items[i]
	label, hint := t.S.Base, t.S.Dim
	switch {
	case i == m.cur:
		label, hint = t.S.Selected, t.S.Selected
	case it.Disabled:
		label = t.S.Dim
	case it.Danger:
		label = t.S.Error
	}
	marker := "  "
	if i == m.cur {
		marker = "› "
	}
	mw := textwidth.Width(marker)
	if cw <= mw {
		return cell(label.Render, marker, cw)
	}
	hintText := textwidth.Sanitize(it.Hint)
	hw := textwidth.Width(hintText)
	lw := cw - mw
	if hw > 0 && lw-hw-1 >= 4 {
		lw -= hw + 1
	} else {
		hintText, hw = "", 0
	}
	out := label.Render(marker) + cell(label.Render, textwidth.Sanitize(it.Label), lw)
	if hw > 0 {
		out += label.Render(" ") + hint.Render(hintText)
	}
	return out
}
