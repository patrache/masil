package arch

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

var _ ui.Cursorer = (*canvasModel)(nil)

// helpKey is one row of the key list: the keys as shown and what they do.
type helpKey struct {
	keys string
	desc func(t *i18n.T) string
}

// helpKeys are the canvas keys in the order the help lists them (spec C2.7).
var helpKeys = []helpKey{
	{"hjkl ←↓↑→", func(t *i18n.T) string { return t.Archcanvas.KeySelect() }},
	{"H J K L", func(t *i18n.T) string { return t.Archcanvas.KeyStep() }},
	{"m", func(t *i18n.T) string { return t.Archcanvas.KeyMove() }},
	{"a", func(t *i18n.T) string { return t.Archcanvas.KeyAdd() }},
	{"c", func(t *i18n.T) string { return t.Archcanvas.KeyConnect() }},
	{"e p", func(t *i18n.T) string { return t.Archcanvas.KeyPanel() }},
	{"x", func(t *i18n.T) string { return t.Archcanvas.KeyFault() }},
	{"g", func(t *i18n.T) string { return t.Archcanvas.KeyWrap() }},
	{"v", func(t *i18n.T) string { return t.Archcanvas.KeyMulti() + " · " + t.Archcanvas.KeyView() }},
	{"d", func(t *i18n.T) string { return t.Archcanvas.KeyDelete() }},
	{"u", func(t *i18n.T) string { return t.Archcanvas.KeyUndo() }},
	{"ctrl+r", func(t *i18n.T) string { return t.Archcanvas.KeyRedo() }},
	{"ctrl+←↓↑→", func(t *i18n.T) string { return t.Archcanvas.KeyPan() }},
	{"0", func(t *i18n.T) string { return t.Archcanvas.KeyFit() }},
	{"+ -", func(t *i18n.T) string { return t.Archcanvas.KeyZoom() }},
	{"=", func(t *i18n.T) string { return t.Archcanvas.KeyArrange() }},
	{"/", func(t *i18n.T) string { return t.Archcanvas.KeyFind() }},
	{"space", func(t *i18n.T) string { return t.Archcanvas.KeyRun() }},
	{"< >", func(t *i18n.T) string { return t.Archcanvas.KeySpeed() }},
	{"!", func(t *i18n.T) string { return t.Archcanvas.KeyChecks() }},
	{"?", func(t *i18n.T) string { return t.Archcanvas.KeyHelp() }},
}

func bind(keys, help string, k ...string) key.Binding {
	return key.NewBinding(key.WithKeys(k...), key.WithHelp(keys, help))
}

// Help is the status line's key hints for the current mode (first 4 shown).
func (m *canvasModel) Help(ctx *ui.Context) []key.Binding {
	a := ctx.T.Archcanvas
	confirm := bind("enter", a.KeyConfirm(), "enter")
	cancel := bind("esc", a.KeyCancel(), "esc")
	if m.regs.View == viewSummary {
		return []key.Binding{bind("space", a.KeyRun(), " ")}
	}
	switch m.mode {
	case modeMove:
		return []key.Binding{bind("hjkl", a.KeyMove(), "h", "j", "k", "l"), confirm, cancel}
	case modeConnect:
		return []key.Binding{bind("hjkl", a.KeySelect(), "h", "j", "k", "l"), confirm, cancel}
	case modeMulti:
		return []key.Binding{bind("hjkl", a.KeyMulti(), "h", "j", "k", "l"), bind("space", a.KeyToggle(), " "), bind("g", a.KeyWrap(), "g"), cancel}
	case modePick:
		return []key.Binding{bind("j k", a.KeyPick(), "j", "k"), confirm, cancel}
	case modeFind:
		return []key.Binding{bind("enter", a.KeyFind(), "enter"), cancel}
	case modePanel:
		if m.panel.editing || m.panel.pick != nil {
			return []key.Binding{confirm, cancel}
		}
		return []key.Binding{bind("j k", a.KeySelect(), "j", "k"), bind("enter", a.KeyEdit(), "enter"), cancel}
	case modeHelp:
		return []key.Binding{cancel}
	}
	if ro := m.sink == nil || isReadOnly(m.sink); ro {
		return []key.Binding{bind("hjkl", a.KeySelect(), "h", "j", "k", "l"), bind("/", a.KeyFind(), "/"), bind("?", a.KeyHelp(), "?")}
	}
	return []key.Binding{
		bind("hjkl", a.KeySelect(), "h", "j", "k", "l"),
		bind("a", a.KeyAdd(), "a"),
		bind("c", a.KeyConnect(), "c"),
		bind("m", a.KeyMove(), "m"),
		bind("d", a.KeyDelete(), "d"),
		bind("u", a.KeyUndo(), "u"),
		bind("?", a.KeyHelp(), "?"),
	}
}

// drawHelp draws the tool's help over r: the model note first (it is never
// shortened), then the keys.
func (m *canvasModel) drawHelp(ctx *ui.Context, b *buf, r layout.Rect) {
	if r.W <= 2 || r.H <= 0 {
		return
	}
	b.fill(r, sBase)
	w := r.W - 2
	type line struct {
		s  string
		st sty
	}
	lines := []line{{ctx.T.Archcanvas.HelpTitle(), sAccBold}}
	for _, s := range textwidth.Wrap(ctx.T.Archcanvas.HelpModelIntro(), w) {
		lines = append(lines, line{s, sBase})
	}
	lines = append(lines, line{"", sBase})
	kw := 0
	for _, k := range helpKeys {
		kw = max(kw, textwidth.Width(k.keys))
	}
	for _, k := range helpKeys {
		lines = append(lines, line{textwidth.PadRight(k.keys, kw) + "  " + k.desc(ctx.T), sBase})
	}
	for i, l := range lines {
		if i >= r.H {
			break
		}
		if i == r.H-1 && i < len(lines)-1 {
			b.line(r.X+1, r.Y+i, w, "…", sDim)
			break
		}
		b.line(r.X+1, r.Y+i, w, l.s, l.st)
	}
}

// drawFind draws the find prompt on the canvas's last row.
func (m *canvasModel) drawFind(b *buf, r layout.Rect) {
	if m.mode != modeFind || r.W <= 2 || r.H <= 0 {
		return
	}
	y := r.Y + r.H - 1
	b.fill(layout.Rect{X: r.X, Y: y, W: r.W, H: 1}, sAcc)
	b.line(r.X+1, y, r.W-2, "/ "+m.find, sAcc)
}

// Cursor is the real cursor at the end of the find query (IME input).
func (m *canvasModel) Cursor() *tea.Cursor {
	if m.mode == modePanel && m.panel.editing && m.panel.curY >= 0 {
		return tea.NewCursor(m.panel.curX, m.panel.curY)
	}
	if m.mode != modeFind {
		return nil
	}
	r := m.regs.Canvas
	x := r.X + 1 + textwidth.Width("/ "+m.find)
	if r.W <= 2 || x >= r.X+r.W-1 {
		return nil
	}
	return tea.NewCursor(x, r.Y+r.H-1)
}

func isReadOnly(s docSink) bool {
	ro, _ := s.ReadOnly()
	return ro
}
