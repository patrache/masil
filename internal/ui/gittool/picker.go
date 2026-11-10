package gittool

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The repository picker (A11.2 step 3): the recent repositories first (most
// often the way in: a masil started by the daemon or a shortcut starts in the
// home folder), newest on top, each as its folder name and a dim tail of its
// path; then "경로 직접 입력", a one-line path field with folder-name
// completion on tab. A chosen path that is not a git repository says so and
// opens nothing.

// pickedMsg is the answer to choosing path: its repository top level, or err.
type pickedMsg struct {
	id   string // the picking component's instance id
	path string
	repo string
	err  error
}

type picker struct {
	id     string                                     // instance id (its msgs)
	onPick func(ctx *ui.Context, repo string) tea.Cmd // a repository was chosen

	size    ui.Size
	sel     int // index into rows (the recents, then the path row)
	typing  bool
	input   textinput.Model
	notRepo bool // the last choice was not a repository
	busy    bool // a choice is being checked

	// Where the last View drew the path field (Cursor reads it).
	lastInputRow int
	lastLabel    string

	// next opens the chosen repository once this picker has closed
	// (CloseTool): the close is handled first, so the picker's place on a
	// full tool surface is free (batched Cmds land in any order).
	next tea.Cmd
}

var (
	_ ui.Component     = (*picker)(nil)
	_ ui.KeyHandler    = (*picker)(nil)
	_ ui.InputCapturer = (*picker)(nil)
	_ ui.Completer     = (*picker)(nil)
	_ ui.Cursorer      = (*picker)(nil)
	_ ui.Titled        = (*picker)(nil)
	_ ui.ToolStatuser  = (*picker)(nil)
	_ ui.Closer        = (*picker)(nil)
)

// newPicker is the standalone picker instance (pickID): a chosen repository
// opens its own instance and the picker closes — the close first, the open
// from CloseTool, so a full tool surface has the picker's place for it.
func newPicker(_ *ui.Context, r ui.Route) ui.Component {
	query := r.Query
	var p *picker
	p = newPickerFor(r.ID, func(_ *ui.Context, repo string) tea.Cmd {
		p.next = ui.OpenTool(toolID, instancePrefix+repo, query)
		return ui.CloseTool(r.ID)
	})
	return p
}

// CloseTool hands over the open of the chosen repository (next), if any.
func (p *picker) CloseTool(*ui.Context) tea.Cmd {
	next := p.next
	p.next = nil
	return next
}

func newPickerFor(id string, onPick func(*ui.Context, string) tea.Cmd) *picker {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	km.DeleteAfterCursor = withoutKeys(km.DeleteAfterCursor, "ctrl+k") // ctrl+k is the palette
	none := key.NewBinding(key.WithDisabled())
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	ti.CharLimit = 4096
	return &picker{id: id, onPick: onPick, input: ti}
}

func withoutKeys(b key.Binding, drop ...string) key.Binding {
	keys := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(drop, k) })
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(b.Help().Key, b.Help().Desc))
}

func (p *picker) Init(*ui.Context) tea.Cmd           { return nil }
func (p *picker) Resize(_ *ui.Context, size ui.Size) { p.size = size }
func (p *picker) CapturesInput() bool                { return p.typing }

// Completing: while the path field is open, tab (a hard global) comes here
// first — it completes the folder name.
func (p *picker) Completing() bool { return p.typing }

func (p *picker) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Lazygit.Name() + " · " + ctx.T.Lazygit.PickTitle(), ui.Badge{}
}

func (p *picker) ToolStatus(ctx *ui.Context) string { return ctx.T.Lazygit.PickTitle() }

// recents are the remembered repositories, newest first.
func (p *picker) recents(ctx *ui.Context) []string {
	return loadState(ctx).Recent
}

func (p *picker) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(pickedMsg)
	if !ok || m.id != p.id {
		return nil
	}
	p.busy = false
	if m.err != nil {
		p.notRepo = true
		return nil
	}
	if p.onPick == nil {
		return nil
	}
	return p.onPick(ctx, m.repo)
}

// choose checks path off the UI goroutine (git rev-parse).
func (p *picker) choose(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	p.notRepo = false
	if path == "" {
		return nil
	}
	p.busy = true
	env, id := curEnv(), p.id
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), resolveTimeout)
		defer cancel()
		repo, err := lazygit.RepoRoot(c, env, path)
		return pickedMsg{id: id, path: path, repo: repo, err: err}
	}
}

func (p *picker) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.typing {
		return p.inputKey(ctx, msg)
	}
	rec := p.recents(ctx)
	n := len(rec) + 1 // + the path row
	switch msg.String() {
	case "up", "k":
		p.sel = max(0, p.sel-1)
	case "down", "j":
		p.sel = min(n-1, p.sel+1)
	case "home", "g":
		p.sel = 0
	case "end", "G":
		p.sel = n - 1
	case "enter":
		p.sel = min(max(p.sel, 0), n-1)
		if p.sel < len(rec) {
			return true, p.choose(rec[p.sel])
		}
		p.typing, p.notRepo = true, false
		return true, p.input.Focus()
	default:
		return false, nil
	}
	return true, nil
}

func (p *picker) inputKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.typing = false
		p.input.Blur()
		return true, nil
	case "enter":
		return true, p.choose(expandHome(p.input.Value()))
	case "tab":
		if v, ok := complete(p.input.Value()); ok {
			p.input.SetValue(v)
			p.input.CursorEnd()
		}
		return true, nil
	case "ctrl+c":
		return false, nil
	}
	p.notRepo = false
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if v := oneLine(p.input.Value()); v != p.input.Value() {
		p.input.SetValue(v)
		p.input.CursorEnd()
	}
	return true, cmd
}

// oneLine normalizes typed text (CA 8.4) to one line.
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

// expandHome turns a leading "~" into the home folder.
func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

// complete extends the typed path's last element to the folder names that
// start with it (directories only): the one match plus a separator, else
// their longest common prefix. ok is false when nothing changes.
func complete(typed string) (string, bool) {
	full := expandHome(typed)
	if full == "" {
		return typed, false
	}
	dir, base := filepath.Split(full)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return typed, false
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".")) {
			continue
		}
		if e.IsDir() {
			names = append(names, name)
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && fi.IsDir() {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return typed, false
	}
	add := names[0]
	if len(names) == 1 {
		add += string(filepath.Separator)
	} else {
		for _, n := range names[1:] {
			add = commonPrefix(add, n)
		}
	}
	if len(add) <= len(base) {
		return typed, false
	}
	// Keep what the user typed ("~/…") and add only the new part.
	return typed + add[len(base):], true
}

func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	// Do not cut a multi-byte rune in half.
	for i > 0 && i < len(a) && !utf8Start(a[i]) {
		i--
	}
	return a[:i]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// pickLine is one drawn picker row.
type pickLine struct {
	text, tail string
	style      lipgloss.Style
	tailStyle  lipgloss.Style
	sel        bool
}

func (p *picker) View(ctx *ui.Context, focused bool) string {
	w, h := p.size.W, p.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	rec := p.recents(ctx)
	p.sel = min(max(p.sel, 0), len(rec))

	var head []pickLine
	if ctx.Mode == layout.ModeTiny { // no border carries the title there
		head = append(head, pickLine{text: " " + hubIcon + " " + t.Lazygit.Name() + " · " + t.Lazygit.PickTitle(), style: th.S.Title})
	}
	var rows []pickLine
	selRow := 0
	if len(rec) == 0 {
		rows = append(rows, pickLine{text: "   " + t.Lazygit.PickEmpty(), style: th.S.Dim})
	}
	nameW := 0
	for _, r := range rec {
		nameW = max(nameW, textwidth.Width(folderName(r)))
	}
	nameW = min(nameW, max(8, w/2))
	for i, r := range rec {
		mark := "   "
		if i == p.sel && !p.typing {
			mark = " › "
			selRow = len(rows)
		}
		name := textwidth.PadRight(textwidth.Truncate(folderName(r), nameW, "…"), nameW)
		rows = append(rows, pickLine{text: mark + name, tail: textwidth.Sanitize(r), style: th.S.Base, tailStyle: th.S.Dim, sel: i == p.sel && !p.typing && focused})
	}
	pathSel := p.sel == len(rec)
	if pathSel {
		selRow = len(rows)
	}
	switch {
	case p.typing:
		rows = append(rows, pickLine{text: " " + t.Lazygit.PickInputLabel() + " ", style: th.S.Bold})
	case pathSel:
		rows = append(rows, pickLine{text: " › " + t.Lazygit.PickTypePath(), style: th.S.Base, sel: focused})
	default:
		rows = append(rows, pickLine{text: "   " + t.Lazygit.PickTypePath(), style: th.S.Base})
	}
	if p.notRepo {
		rows = append(rows, pickLine{text: "   " + t.Lazygit.NotRepo(), style: th.S.Error})
	}

	footer := t.Lazygit.PickFooter()
	if p.typing {
		footer = t.Lazygit.PickInputFooter()
	}
	foot := []pickLine{{text: " " + footer, style: th.S.Dim}}
	if h < 3 {
		foot = nil
	}
	if len(head)+len(foot) >= h {
		head = nil
	}
	avail := max(0, h-len(head)-len(foot))
	off := 0
	if selRow >= avail {
		off = selRow - avail + 1
	}
	out := make([]string, 0, h)
	for _, l := range head {
		out = append(out, drawPickLine(th, l, w))
	}
	for i := off; i < len(rows) && len(out) < len(head)+avail; i++ {
		l := rows[i]
		if p.typing && i == len(rec) {
			p.lastInputRow, p.lastLabel = len(out), l.text
			out = append(out, p.inputLine(th, l, w))
			continue
		}
		out = append(out, drawPickLine(th, l, w))
	}
	for len(out) < len(head)+avail {
		out = append(out, th.S.Base.Render(strings.Repeat(" ", w)))
	}
	for _, l := range foot {
		out = append(out, drawPickLine(th, l, w))
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// drawPickLine draws l in exactly w cells: its text, then its dim tail
// (cut from the left so the end of the path stays).
func drawPickLine(th *theme.Theme, l pickLine, w int) string {
	st, tailSt := l.style, l.tailStyle
	if l.sel {
		st = th.S.Selected
		tailSt = th.S.Selected.Foreground(th.S.Dim.GetForeground())
	}
	text := textwidth.Truncate(l.text, w, "…")
	used := textwidth.Width(text)
	s := st.Render(text)
	if l.tail != "" && w-used > 4 {
		tail := "  " + textwidth.TruncateLeft(l.tail, w-used-2, "…")
		s += tailSt.Render(tail)
		used += textwidth.Width(tail)
	}
	if used < w {
		s += st.Render(strings.Repeat(" ", w-used))
	}
	return s
}

// inputLine draws the path field after its label; col (for Cursor) is where
// the cursor is.
func (p *picker) inputLine(th *theme.Theme, label pickLine, w int) string {
	s, _ := p.inputParts(label.text, w)
	lab := textwidth.Truncate(label.text, w, "")
	rest := w - textwidth.Width(lab)
	return label.style.Render(lab) + th.S.InputFocused.Render(textwidth.PadRight(s, max(0, rest)))
}

// inputParts is the shown part of the value in the room after label, and the
// cursor's column inside that room.
func (p *picker) inputParts(label string, w int) (string, int) {
	room := w - textwidth.Width(label)
	if room <= 1 {
		return "", 0
	}
	v := []rune(p.input.Value())
	pos := min(max(p.input.Position(), 0), len(v))
	before, after := string(v[:pos]), string(v[pos:])
	if textwidth.Width(before) > room-1 {
		before = textwidth.TruncateLeft(before, room-1, "…")
	}
	col := textwidth.Width(before)
	return before + textwidth.Truncate(after, room-col, ""), col
}

// Cursor is the real cursor in the path field (relative to the inner rect).
func (p *picker) Cursor() *tea.Cursor {
	if !p.typing || p.size.W <= 0 || p.size.H <= 0 {
		return nil
	}
	_, col := p.inputParts(p.lastLabel, p.size.W)
	x := min(textwidth.Width(p.lastLabel)+col, p.size.W-1)
	y := min(max(p.lastInputRow, 0), p.size.H-1)
	return tea.NewCursor(x, y)
}

func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}
