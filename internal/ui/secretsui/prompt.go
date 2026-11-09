package secretsui

import (
	"encoding/json"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// PromptMsg is broadcast when the masked prompt closes: Value on enter,
// Canceled (and a zero Value) on esc.
type PromptMsg struct {
	Tool, Name string
	Value      secrets.Secret
	Canceled   bool
}

// promptArgs is OverlayPrompt's args.
type promptArgs struct {
	Tool  string `json:"tool"`
	Name  string `json:"name"`
	Label string `json:"label"`
}

// maskRune is what the field shows for every typed character.
const maskRune = "•"

// promptFieldMax is the widest field.
const promptFieldMax = 40

// prompt is the OverlayPrompt dialog: a label and a field that never shows
// what was typed or pasted.
type prompt struct {
	args   promptArgs
	buf    []rune
	size   ui.Size
	cursor *tea.Cursor
}

var (
	_ ui.Overlay    = (*prompt)(nil)
	_ ui.KeyHandler = (*prompt)(nil)
	_ ui.Cursorer   = (*prompt)(nil)
	_ ui.Titled     = (*prompt)(nil)
)

// newPrompt parses args; broken args or a missing tool/name open nothing.
func newPrompt(_ *ui.Context, args string) ui.Overlay {
	var a promptArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil || a.Tool == "" || a.Name == "" {
		return nil
	}
	return &prompt{args: a}
}

func (d *prompt) Init(*ui.Context) tea.Cmd        { return nil }
func (d *prompt) Resize(_ *ui.Context, s ui.Size) { d.size = s }
func (d *prompt) Modal() bool                     { return true }
func (d *prompt) Bounds(body layout.Rect) layout.Rect {
	return centered(body, promptW, promptH)
}
func (d *prompt) Title(ctx *ui.Context) (string, ui.Badge) {
	return toolTitle(ctx, d.args.Tool), ui.Badge{}
}
func (d *prompt) Cursor() *tea.Cursor { return d.cursor }

// Update takes pastes (a cookie is usually pasted): line breaks and control
// characters are dropped.
func (d *prompt) Update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.PasteMsg); ok {
		d.add(m.Content)
	}
	return nil
}

func (d *prompt) add(s string) {
	for _, r := range s {
		if !unicode.IsControl(r) {
			d.buf = append(d.buf, r)
		}
	}
}

// done closes the dialog and broadcasts the result; the buffer is wiped.
func (d *prompt) done(canceled bool) tea.Cmd {
	m := PromptMsg{Tool: d.args.Tool, Name: d.args.Name, Canceled: canceled}
	if !canceled {
		m.Value = secrets.New(string(d.buf))
	}
	clear(d.buf)
	d.buf = nil
	return tea.Batch(closeOverlay, emit(m))
}

func (d *prompt) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return true, d.done(false)
	case "esc":
		return true, d.done(true)
	case "ctrl+c":
		return false, nil
	case "backspace", "ctrl+h":
		if n := len(d.buf); n > 0 {
			d.buf[n-1] = 0
			d.buf = d.buf[:n-1]
		}
	case "ctrl+u":
		clear(d.buf)
		d.buf = d.buf[:0]
	default:
		if msg.Text != "" {
			d.add(msg.Text)
		}
	}
	return true, nil // modal: every key stops here
}

func (d *prompt) View(ctx *ui.Context, focused bool) string {
	d.cursor = nil
	t, tr := themeOf(ctx), catalog(ctx)
	cw := contentWidth(d.size.W)
	rows := wrapped(d.args.Label, cw, t.S.Title.Render)
	rows = append(rows, line{})
	fieldRow := len(rows)
	fw := min(cw, promptFieldMax)
	n := min(len(d.buf), max(0, fw-1)) // the mask shows the count, scrolled to the end
	mask := strings.Repeat(maskRune, n)
	rows = append(rows, line{text: textwidth.PadRight(mask, fw), style: t.S.Selected.Render, keep: true})
	rows = append(rows, line{}, line{text: tr.Tools.PromptFooter(), style: t.S.Dim.Render})
	s, y := render(t, rows, d.size, fieldRow)
	if focused && y >= 0 && y < d.size.H {
		x := n
		if d.size.W >= 12 {
			x++
		}
		d.cursor = tea.NewCursor(x, y)
	}
	return s
}
