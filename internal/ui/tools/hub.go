package tools

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// The hub (A8.2): one row per tool — " {n} {icon} {name}  {status}", n = its
// Order (the digit that opens it) — then "── 열려 있음 ──" and the open tool
// instances (title, status), and the key line at the bottom. The first line
// carries the close key on its right, and the title "도구" on its left in tiny
// (bordered modes have it in the pane border). The selection walks both lists:
// enter opens a tool (ModeAuto) or shows an open instance, m picks the way to
// open a tool this once (openmenu.go), x closes an open instance, a digit opens
// that tool at once.

// Hub layout defaults (the one place for its widths).
var hubLayout = struct {
	gap       string // between a row's name and its status
	indent    string // before an open instance's title
	maxNameW  int    // the name column's widest, before the status
	minStatus int    // cells kept for the status before the name column shrinks
}{gap: "  ", indent: "   ", maxNameW: 24, minStatus: 8}

// hubRowKind is what a hub row is.
type hubRowKind int

const (
	rowTool hubRowKind = iota // a registered tool
	rowOpen                   // an open tool instance
)

// hubRow is one selectable hub row.
type hubRow struct {
	kind hubRowKind
	spec ui.ToolSpec     // rowTool
	open ui.OpenToolInfo // rowOpen
}

type hub struct {
	size ui.Size
	sel  int       // index into rows(); clamped at use
	menu *openMenu // the open-mode menu, nil when closed
}

func newHub(*ui.Context) *hub { return &hub{} }

func (h *hub) Init(*ui.Context) tea.Cmd           { return nil }
func (h *hub) Resize(_ *ui.Context, size ui.Size) { h.size = size }

// Update: when ctrl+t brings the hub forward from the chat (Toggled), the
// cursor goes to the open instance that was last active (OpenToolInfo.Last),
// so enter shows it again; with none marked the cursor stays where it was.
// A zoom or tools.next/prev landing on the hub leaves the cursor alone.
func (h *hub) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ui.ToolSwitchedMsg); ok && m.Toggled && m.Route.Kind == ui.RouteTools && h.menu == nil {
		for i, r := range h.rows(ctx) {
			if r.kind == rowOpen && r.open.Last {
				h.sel = i
				break
			}
		}
	}
	return nil
}

// rows are the hub's selectable rows now: the flagged-on tools by Order, then
// the open instances.
func (h *hub) rows(ctx *ui.Context) []hubRow {
	var out []hubRow
	for _, s := range ui.Tools() {
		out = append(out, hubRow{kind: rowTool, spec: s})
	}
	if ctx.OpenTools != nil {
		for _, o := range ctx.OpenTools() {
			out = append(out, hubRow{kind: rowOpen, open: o})
		}
	}
	return out
}

// selected is the selected row (ok=false: none), with h.sel clamped to rows.
func (h *hub) selected(rows []hubRow) (hubRow, bool) {
	if len(rows) == 0 {
		h.sel = 0
		return hubRow{}, false
	}
	h.sel = min(max(h.sel, 0), len(rows)-1)
	return rows[h.sel], true
}

// openTool runs s.Open with mode.
func openTool(ctx *ui.Context, s ui.ToolSpec, mode termhost.Mode) tea.Cmd {
	if s.Open == nil {
		return nil
	}
	return s.Open(ctx, mode)
}

// HandleKey: the menu's keys while it is open; otherwise the selection, enter,
// m, x and the digits. esc is left to the shell (back to the chat).
func (h *hub) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if h.menu != nil {
		return true, h.menuKey(ctx, k)
	}
	rows := h.rows(ctx)
	row, ok := h.selected(rows)
	switch k {
	case "up", "k":
		h.sel--
		return true, nil
	case "down", "j":
		h.sel++
		return true, nil
	case "home", "g":
		h.sel = 0
		return true, nil
	case "end", "G":
		h.sel = len(rows) - 1
		return true, nil
	case "enter":
		switch {
		case !ok:
			return true, nil
		case row.kind == rowTool:
			return true, openTool(ctx, row.spec, termhost.ModeAuto)
		}
		return true, ui.Navigate(row.open.Route)
	case "m":
		if ok && row.kind == rowTool {
			h.menu = newOpenMenu(ctx, row.spec.ID)
		}
		return true, nil
	case "x":
		if ok && row.kind == rowOpen {
			return true, ui.CloseTool(row.open.Route.ID)
		}
		return true, nil
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		n := int(k[0] - '0')
		for _, r := range rows {
			if r.kind == rowTool && r.spec.Order == n {
				return true, openTool(ctx, r.spec, termhost.ModeAuto)
			}
		}
		return true, nil
	}
	return false, nil
}

// menuKey handles a key while the open-mode menu is up: up/down pick, enter
// opens the tool that way, esc closes the menu; every other key is swallowed.
func (h *hub) menuKey(ctx *ui.Context, k string) tea.Cmd {
	switch k {
	case "up", "k":
		h.menu.move(-1)
	case "down", "j":
		h.menu.move(+1)
	case "esc":
		h.menu = nil
	case "enter":
		menu := h.menu
		h.menu = nil
		mode, ok := menu.choose(ctx)
		s, found := ui.ToolByID(menu.tool)
		if !ok || !found {
			return nil
		}
		return openTool(ctx, s, mode)
	}
	return nil
}

// line is one drawn hub line: left text, right (status) text, selected or not.
type line struct {
	left, right string
	sel         bool
	header      bool // a section rule (── … ──), drawn dim
	flush       bool // right text flush with the right edge (the first line)
}

// View draws the hub in exactly size.W x size.H.
func (h *hub) View(ctx *ui.Context, focused bool) string {
	w, ht := h.size.W, h.size.H
	if w <= 0 || ht <= 0 {
		return ""
	}
	t := ctx.T
	rows := h.rows(ctx)
	h.selected(rows)

	var tools, opens []hubRow
	for _, r := range rows {
		if r.kind == rowTool {
			tools = append(tools, r)
		} else {
			opens = append(opens, r)
		}
	}

	closeKey := ctx.Keys.Binding(keymap.ToolsToggle).Help().Key
	first := line{right: t.Tools.HubClose(textwidth.Sanitize(closeKey)) + " ", flush: true}
	if ctx.Mode == layout.ModeTiny { // no pane border carries the title there (A8.2 " 도구 … ctrl+t 닫기")
		first.left = " " + t.Tools.TabLabel()
	}
	content := []line{first}
	selLine := 0

	nameW := 0
	for _, r := range tools {
		nameW = max(nameW, textwidth.Width(textwidth.Sanitize(toolLabel(t, r.spec))))
	}
	nameW = min(nameW, hubLayout.maxNameW, max(0, w-4-hubLayout.minStatus))
	if len(tools) == 0 {
		content = append(content, line{left: " " + t.Tools.HubEmpty()})
	}
	for i, r := range tools {
		label := textwidth.PadRight(textwidth.Truncate(textwidth.Sanitize(toolLabel(t, r.spec)), nameW, "…"), nameW)
		status := ""
		if r.spec.Status != nil {
			status = textwidth.Sanitize(r.spec.Status(ctx))
		}
		n := " "
		if r.spec.Order >= 1 && r.spec.Order <= 9 {
			n = fmt.Sprint(r.spec.Order)
		}
		if i == h.sel {
			selLine = len(content)
		}
		content = append(content, line{left: " " + n + " " + label, right: status, sel: i == h.sel && h.menu == nil})
	}

	switch {
	case h.menu != nil:
		title := h.menu.tool
		if s, ok := ui.ToolByID(h.menu.tool); ok {
			title = textwidth.Sanitize(toolLabel(t, s))
		}
		content = append(content, line{left: " ── " + t.Tools.MenuTitle(title) + " ──", header: true})
		for i, m := range h.menu.modes {
			mark := "   "
			if i == h.menu.sel {
				mark = " ▸ "
				selLine = len(content)
			}
			content = append(content, line{left: mark + modeName(t, m), sel: i == h.menu.sel})
		}
	case len(opens) > 0:
		content = append(content, line{left: " ── " + t.Tools.HubOpenHeader() + " ──", header: true})
		titleW := 0
		for _, r := range opens {
			titleW = max(titleW, textwidth.Width(r.open.Title))
		}
		titleW = min(titleW, hubLayout.maxNameW, max(0, w-len(hubLayout.indent)-hubLayout.minStatus))
		for j, r := range opens {
			i := len(tools) + j
			if i == h.sel {
				selLine = len(content)
			}
			title := textwidth.PadRight(textwidth.Truncate(r.open.Title, titleW, "…"), titleW)
			content = append(content, line{left: hubLayout.indent + title, right: r.open.Status, sel: i == h.sel})
		}
	}

	var footer []line
	switch {
	case h.menu != nil:
		footer = []line{{left: " " + t.Tools.MenuFooter()}}
	case len(tools) > 0:
		footer = []line{{left: " " + t.Tools.HubFooter(digitRange(tools))}}
	}
	if ht < 3 { // too short for the key line: the rows only
		footer = nil
	}
	avail := ht - len(footer)
	off := 0
	if selLine >= avail {
		off = selLine - avail + 1
	}
	shown := content[min(off, len(content)):]
	if len(shown) > avail {
		shown = shown[:avail]
	}

	th := ctx.Theme
	out := make([]string, 0, ht)
	for _, l := range shown {
		out = append(out, drawLine(th.S.Base, th.S.Dim, th.S.Selected, l, focused, w))
	}
	for len(out) < avail {
		out = append(out, th.S.Base.Render(strings.Repeat(" ", w)))
	}
	for _, l := range footer {
		out = append(out, drawLine(th.S.Dim, th.S.Dim, th.S.Selected, l, focused, w))
	}
	return strings.Join(out, "\n")
}

// digitRange is the footer's digit range over tools ("1–5"; one tool: "4").
func digitRange(tools []hubRow) string {
	lo, hi := 0, 0
	for _, r := range tools {
		if o := r.spec.Order; o >= 1 && o <= 9 {
			if lo == 0 || o < lo {
				lo = o
			}
			hi = max(hi, o)
		}
	}
	switch {
	case lo == 0:
		return ""
	case lo == hi:
		return fmt.Sprint(lo)
	}
	return fmt.Sprintf("%d–%d", lo, hi)
}

// drawLine draws l in exactly w cells: left in base (dim for a header), the
// right text after the gap in dim (a line with no left: right-aligned); the
// selected row of a focused hub in sel.
func drawLine(base, dim, sel lipgloss.Style, l line, focused bool, w int) string {
	if l.sel && focused {
		base, dim = sel, sel.Foreground(dim.GetForeground())
	}
	if l.header {
		base = dim
	}
	left := textwidth.Truncate(l.left, w, "…")
	used := textwidth.Width(left)
	s := base.Render(left)
	if l.right != "" {
		var right string
		switch {
		case l.left == "":
			right = textwidth.PadLeft(textwidth.Truncate(l.right, w, "…"), w)
		case l.flush && w-used > len(hubLayout.gap):
			right = textwidth.PadLeft(textwidth.Truncate(l.right, w-used-len(hubLayout.gap), "…"), w-used)
		case w-used > len(hubLayout.gap):
			right = hubLayout.gap + textwidth.Truncate(l.right, w-used-len(hubLayout.gap), "…")
		}
		s += dim.Render(right)
		used += textwidth.Width(right)
	}
	if used < w {
		s += base.Render(strings.Repeat(" ", w-used))
	}
	return s
}
