package workspace

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// The workspace's dialogs are forms drawn over the whole tool surface (spec
// B6.7: in the widget and tiny sizes the overlay covers everything and long
// warnings wrap to the end). Every sentence wraps; nothing is cut — the body
// scrolls instead.

type rowKind int

const (
	rowText   rowKind = iota // a paragraph (wrapped, never cut)
	rowHead                  // a section heading
	rowOption                // a choice: enter (or space) runs act
	rowInput                 // a text field
	rowBig                   // a large centered text (the check number)
)

type row struct {
	kind     rowKind
	text     string // already sanitized where it carries someone else's text
	dim      bool
	warn     bool
	disabled bool
	checked  *bool // a toggle or radio mark ([x] / [ ])
	act      func(ctx *ui.Context) tea.Cmd
	input    *field
}

// field is a one-line text field.
type field struct {
	label string
	r     []rune
	mask  bool
}

func (f *field) value() string { return string(f.r) }

func (f *field) key(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "backspace":
		if len(f.r) > 0 {
			f.r = f.r[:len(f.r)-1]
		}
		return true
	case "ctrl+u":
		f.r = nil
		return true
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 { // printable (shift allowed)
		f.r = append(f.r, []rune(textwidth.SanitizeInput(msg.Text))...)
		return true
	}
	return false
}

func (f *field) paste(s string) {
	s = strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
	f.r = append(f.r, []rune(s)...)
}

// form is one dialog.
type form struct {
	title  func(ctx *ui.Context) string
	build  func(ctx *ui.Context) []row
	cancel func(ctx *ui.Context) tea.Cmd // esc (nil: just close)
	keys   func(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd)
	hint   func(ctx *ui.Context) string
	sel    int // index into the selectable rows
	scroll int
	closed bool
	// drawn layout (set by view): selectable row -> first body line
	selLine []int
	cursor  *tea.Cursor
	lines   int
}

func (f *form) selectable(rows []row) []int {
	var out []int
	for i, r := range rows {
		if (r.kind == rowOption || r.kind == rowInput) && !r.disabled {
			out = append(out, i)
		}
	}
	return out
}

// current is the selected row index (-1 when nothing can be selected).
func (f *form) current(rows []row) int {
	sel := f.selectable(rows)
	if len(sel) == 0 {
		return -1
	}
	f.sel = max(0, min(f.sel, len(sel)-1))
	return sel[f.sel]
}

// capturesInput: a text field is selected.
func (f *form) capturesInput(ctx *ui.Context) bool {
	rows := f.build(ctx)
	i := f.current(rows)
	return i >= 0 && rows[i].kind == rowInput
}

func (f *form) handleKey(ctx *ui.Context, msg tea.KeyPressMsg) tea.Cmd {
	if f.keys != nil {
		if ok, cmd := f.keys(ctx, msg); ok {
			return cmd
		}
	}
	rows := f.build(ctx)
	cur := f.current(rows)
	n := len(f.selectable(rows))
	k := msg.String()
	if cur >= 0 && rows[cur].kind == rowInput && k != "enter" && k != "esc" && k != "up" && k != "down" && k != "tab" && k != "shift+tab" {
		rows[cur].input.key(msg)
		return nil
	}
	switch k {
	case "esc":
		f.closed = true
		if f.cancel != nil {
			return f.cancel(ctx)
		}
	case "up", "k", "shift+tab":
		if f.sel > 0 {
			f.sel--
		} else {
			f.scroll = max(0, f.scroll-1)
		}
	case "down", "j", "tab":
		if f.sel < n-1 {
			f.sel++
		} else {
			f.scroll++
		}
	case "pgdown", "ctrl+d":
		f.scroll += 5
	case "pgup", "ctrl+u":
		f.scroll = max(0, f.scroll-5)
	case "enter", "space":
		if cur >= 0 && rows[cur].act != nil {
			return rows[cur].act(ctx)
		}
	}
	return nil
}

func (f *form) paste(ctx *ui.Context, s string) {
	rows := f.build(ctx)
	if cur := f.current(rows); cur >= 0 && rows[cur].kind == rowInput {
		rows[cur].input.paste(s)
	}
}

// view draws the form in w x h (a frame line on top, the hint at the bottom).
func (f *form) view(ctx *ui.Context, w, h int) []string {
	th := ctx.Theme
	rows := f.build(ctx)
	cur := f.current(rows)
	title := lipgloss.NewStyle().Foreground(th.P.Acc).Bold(true)
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	warn := lipgloss.NewStyle().Foreground(th.P.Acc2)
	selStyle := th.S.Selected
	inner := max(w-2, 1)

	var body []string
	f.selLine = make([]int, len(rows))
	f.cursor = nil
	curLine, curX := -1, 0
	for i, r := range rows {
		f.selLine[i] = len(body)
		switch r.kind {
		case rowText:
			if r.text == "" {
				body = append(body, "")
				continue
			}
			for _, l := range textwidth.Wrap(r.text, inner) {
				st := lipgloss.NewStyle().Foreground(th.P.FG)
				if r.dim {
					st = dim
				}
				if r.warn {
					st = warn
				}
				body = append(body, " "+st.Render(l))
			}
		case rowHead:
			for _, l := range textwidth.Wrap(r.text, inner) {
				body = append(body, " "+dim.Bold(true).Render(l))
			}
		case rowBig:
			body = append(body, textwidth.Center(title.Render(r.text), w))
		case rowOption:
			mark := "  "
			if i == cur {
				mark = "▸ "
			}
			box := ""
			if r.checked != nil {
				box = "[ ] "
				if *r.checked {
					box = "[x] "
				}
			}
			ls := textwidth.Wrap(box+r.text, max(inner-2, 1))
			for j, l := range ls {
				p := "  "
				if j == 0 {
					p = mark
				}
				line := p + l
				switch {
				case r.disabled:
					line = dim.Render(line)
				case i == cur:
					line = selStyle.Render(textwidth.PadRight(line, inner+1))
				case r.warn:
					line = warn.Render(line)
				}
				body = append(body, " "+line)
			}
		case rowInput:
			mark := "  "
			if i == cur {
				mark = "▸ "
			}
			val := r.input.value()
			if r.input.mask {
				val = strings.Repeat("•", len(r.input.r))
			}
			label := r.input.label + ": "
			room := max(inner-2-textwidth.Width(label)-1, 1)
			shown := textwidth.TruncateLeft(val, room, "…")
			body = append(body, " "+mark+label+shown)
			if i == cur {
				curLine = len(body) - 1
				curX = 1 + 2 + textwidth.Width(label) + textwidth.Width(shown)
			}
		}
	}
	f.lines = len(body)
	bodyH := max(h-3, 1)
	// keep the selected row on screen
	if cur >= 0 {
		first := f.selLine[cur]
		last := first
		if cur+1 < len(rows) {
			last = max(first, f.selLine[cur+1]-1)
		} else {
			last = len(body) - 1
		}
		if last >= f.scroll+bodyH {
			f.scroll = last - bodyH + 1
		}
		if first < f.scroll {
			f.scroll = first
		}
	}
	f.scroll = max(0, min(f.scroll, max(0, len(body)-bodyH)))

	out := make([]string, 0, h)
	head := "─ " + f.title(ctx) + " "
	out = append(out, title.Render(textwidth.Truncate(head, w, "…"))+dim.Render(strings.Repeat("─", max(0, w-textwidth.Width(textwidth.Truncate(head, w, "…"))))))
	end := min(len(body), f.scroll+bodyH)
	for _, l := range body[f.scroll:end] {
		out = append(out, l)
	}
	for len(out) < 1+bodyH {
		out = append(out, "")
	}
	more := ""
	if end < len(body) {
		more = "↓ "
	}
	hint := ""
	if f.hint != nil {
		hint = f.hint(ctx)
	} else {
		hint = ctx.T.Workspace.DlgHint()
	}
	out = append(out, dim.Render(strings.Repeat("─", w)))
	out = append(out, dim.Render(textwidth.Truncate(more+hint, w, "…")))
	if curLine >= f.scroll && curLine < end {
		f.cursor = tea.NewCursor(min(curX, w-1), 1+curLine-f.scroll)
	}
	return textwidth.Fit(out, w, h)
}
