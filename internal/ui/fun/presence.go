package fun

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

// overlayPresence is the presence mode picker (no args).
const overlayPresence = "fun.presence"

// tagPresenceSet is the picker's ui.Do tag (+ my user id).
const tagPresenceSet = "fun.presence_set:"

// presenceDialogW is the picker width (copy: the fun catalog area).
const presenceDialogW = 36

// presenceModes are the manual modes in picker order; dot is the glyph's
// status. label is the name in the screen's language; aliases are other words
// "/presence <word>" accepts (ASCII ones in any language, plus aliasesIn(t)).
var presenceModes = []struct {
	mode      string
	label     func(t *i18n.T) string
	dot       api.PresenceStatus
	aliases   []string
	aliasesIn func(t *i18n.T) []string
}{
	{"auto", func(t *i18n.T) string { return t.Fun.PresenceAuto() }, api.PresenceOnline, []string{"online"},
		func(t *i18n.T) []string { return []string{t.Fun.PresenceAliasOnline()} }},
	{"away", func(t *i18n.T) string { return t.Fun.PresenceAway() }, api.PresenceAway, nil, nil},
	{"dnd", func(t *i18n.T) string { return t.Fun.PresenceDnd() }, api.PresenceDND, nil, nil},
}

func init() {
	palette.Register(palette.Command{
		ID: "presence.set", Label: func(t *i18n.T) string { return t.Fun.CmdPresenceSet() }, Group: palette.GroupCommand,
		Keywords:   []string{"away", "dnd", "presence"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Fun.KwPresenceSet()) },
		Run:        runPresenceSet,
	})
	ui.RegisterOverlay(overlayPresence, func(ctx *ui.Context, _ string) ui.Overlay { return newPresenceDialog(ctx) })
}

// presenceMode is the mode named by args ("auto|away|dnd", or its label or an
// alias in t's language).
func presenceMode(t *i18n.T, args string) (string, bool) {
	a := strings.ToLower(strings.TrimSpace(args))
	for _, m := range presenceModes {
		words := append([]string{m.mode, strings.ToLower(m.label(t))}, m.aliases...)
		if m.aliasesIn != nil {
			for _, w := range m.aliasesIn(t) {
				words = append(words, strings.ToLower(w))
			}
		}
		for _, w := range words {
			if a == w {
				return m.mode, true
			}
		}
	}
	return "", false
}

// runPresenceSet is presence.set: no args => the picker; a mode => PUT at once.
func runPresenceSet(ctx *ui.Context, args string) tea.Cmd {
	if strings.TrimSpace(args) == "" {
		return ui.OpenRegisteredOverlay(overlayPresence, "")
	}
	c := cat(ctx)
	mode, ok := presenceMode(c, args)
	if !ok {
		return ui.Toast(c.Fun.PresenceInvalid(), ui.ToastError)
	}
	return doApply(ctx, setPresence(mode), applyPresence, c.Fun.PresenceChanged())
}

func setPresence(mode string) func(context.Context, *api.Client) (*api.PresenceState, error) {
	return func(c context.Context, a *api.Client) (*api.PresenceState, error) {
		if a == nil {
			return nil, &api.Error{Code: "network"}
		}
		return a.SetPresenceMode(c, mode)
	}
}

// applyPresence stores my presence (UI goroutine only).
func applyPresence(ctx *ui.Context, p *api.PresenceState) {
	if ctx == nil || ctx.Store == nil || p == nil {
		return
	}
	ctx.Store.Self = *p
}

// presenceDialog is fun.presence: ● auto / ◐ away / ⊘ dnd with the
// current mode marked; j/k move, enter (or a click) sets the mode.
type presenceDialog struct {
	sel  int
	busy bool
	size ui.Size
	rowY [3]int // row of each mode in the last View (-1 = not drawn)
}

var (
	_ ui.Overlay      = (*presenceDialog)(nil)
	_ ui.KeyHandler   = (*presenceDialog)(nil)
	_ ui.MouseHandler = (*presenceDialog)(nil)
	_ ui.Titled       = (*presenceDialog)(nil)
	_ ui.Helper       = (*presenceDialog)(nil)
)

func newPresenceDialog(ctx *ui.Context) *presenceDialog {
	d := &presenceDialog{rowY: [3]int{-1, -1, -1}}
	d.sel = currentMode(ctx)
	return d
}

// currentMode is the index of my manual mode (auto when unknown).
func currentMode(ctx *ui.Context) int {
	if ctx == nil || ctx.Store == nil {
		return 0
	}
	for i, m := range presenceModes {
		if m.mode == ctx.Store.Self.Mode {
			return i
		}
	}
	return 0
}

func (d *presenceDialog) Init(*ui.Context) tea.Cmd { return nil }

func (d *presenceDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(ui.ResultMsg[*api.PresenceState])
	if !ok || m.Tag != meTag(ctx, tagPresenceSet) || !d.busy {
		return nil
	}
	d.busy = false
	if m.Err != nil || m.Value == nil {
		err := m.Err
		if err == nil {
			err = &api.Error{Code: "network"}
		}
		return func() tea.Msg { return ui.ErrorMsg{Err: err} }
	}
	applyPresence(ctx, m.Value)
	return tea.Batch(closeOverlay, ui.Toast(cat(ctx).Fun.PresenceChanged(), ui.ToastSuccess))
}

func (d *presenceDialog) submit(ctx *ui.Context) tea.Cmd {
	if d.busy {
		return nil
	}
	d.busy = true
	return ui.Do(ctx, meTag(ctx, tagPresenceSet), setPresence(presenceModes[d.sel].mode))
}

func (d *presenceDialog) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s := msg.String()
	if s == "esc" || s == "ctrl+k" {
		return false, nil
	}
	if d.busy {
		return true, nil
	}
	switch s {
	case "j", "down", "tab":
		d.sel = min(d.sel+1, len(presenceModes)-1)
	case "k", "up", "shift+tab":
		d.sel = max(d.sel-1, 0)
	case "1", "2", "3":
		d.sel = int(s[0] - '1')
		return true, d.submit(ctx)
	case "enter", "space":
		return true, d.submit(ctx)
	}
	return true, nil
}

func (d *presenceDialog) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft || d.busy {
		return nil
	}
	for i, y := range d.rowY {
		if y >= 0 && y == local.Y {
			d.sel = i
			return d.submit(ctx)
		}
	}
	return nil
}

func (d *presenceDialog) Resize(_ *ui.Context, size ui.Size) { d.size = size }

func (d *presenceDialog) Bounds(body layout.Rect) layout.Rect {
	return centered(body, presenceDialogW, len(presenceModes)+3)
}

func (d *presenceDialog) Modal() bool { return true }

func (d *presenceDialog) Title(ctx *ui.Context) (string, ui.Badge) {
	return cat(ctx).Fun.PresenceTitle(), ui.Badge{}
}

func (d *presenceDialog) Help(ctx *ui.Context) []key.Binding {
	t := cat(ctx)
	return []key.Binding{helpKey("enter", t.Fun.HelpChange()), helpKey("j/k", t.Common.HelpMove()), helpKey("esc", t.Common.HelpCancel())}
}

// View is one row per mode ("› ● <label>  <now>") and the key hint, cut to the
// inner size so the selected row stays visible.
func (d *presenceDialog) View(ctx *ui.Context, _ bool) string {
	t, c := themeOf(ctx), cat(ctx)
	w, h := d.size.W, d.size.H
	d.rowY = [3]int{-1, -1, -1}
	if w <= 0 || h <= 0 {
		return ""
	}
	pad := 0
	if w >= 12 {
		pad = 1
	}
	cw := max(w-2*pad, 1)
	cur := currentMode(ctx)
	var rows []string
	for i, m := range presenceModes {
		prefix, st := rowStyle(t, d.sel == i, false)
		line := prefix + widgets.PresenceDot(t, m.dot) + t.S.Base.Render(" ") + st(m.label(c))
		if i == cur {
			line += t.S.Dim.Render("  " + c.Fun.NowLabel())
		}
		rows = append(rows, line)
	}
	hint := c.Fun.PresenceKeys()
	if d.busy {
		hint = c.Common.Sending()
	}
	rows = append(rows, t.S.Dim.Render(textwidth.Truncate(hint, cw, "…")))
	top := 0
	if d.sel >= h {
		top = d.sel - h + 1
	}
	for i := range presenceModes {
		if i-top >= 0 && i-top < h {
			d.rowY[i] = i - top
		}
	}
	side := t.S.Base.Render(strings.Repeat(" ", pad))
	out := make([]string, 0, h)
	for i := top; i < top+h; i++ {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		out = append(out, fit(t, side+fit(t, l, cw)+side, w))
	}
	return strings.Join(out, "\n")
}
