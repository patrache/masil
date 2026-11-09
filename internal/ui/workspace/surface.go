package workspace

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/hooks"
	"github.com/patrache/masil/internal/workspace/journal"
	"github.com/patrache/masil/internal/workspace/share"
)

// Surface is one workspace tool instance: the folder picker (no folder yet)
// or an open folder with its tree, code and terminal cells (spec B6).
type Surface struct {
	id    string
	route ui.Route

	picker *form // the folder picker (an instance without a folder)
	dlg    *form // a dialog over the whole surface

	dir, name string // the folder (absolute) and its base name
	openErr   error
	root      *fsroot.Root
	jr        *journal.Journal
	docs      *share.DocManager

	tree *treeCell
	code *codeCell
	term *termCell
	own  *ownerShare // the running share (nil = solo)

	focus    int
	zoomed   bool // the tool surface's wide view
	cellZoom bool // ctrl+\ z: the focused cell over the whole body
	appFocus bool
	size     ui.Size
	mode     layout.Mode
	plan     Plan
	prefix   *termpane.Prefix
	pumping  bool
	hooks    *hooks.Receiver
	closed   bool
}

var (
	_ ui.Component     = (*Surface)(nil)
	_ ui.KeyHandler    = (*Surface)(nil)
	_ ui.MouseHandler  = (*Surface)(nil)
	_ ui.Titled        = (*Surface)(nil)
	_ ui.Helper        = (*Surface)(nil)
	_ ui.InputCapturer = (*Surface)(nil)
	_ ui.Cursorer      = (*Surface)(nil)
	_ ui.KeyGrabber    = (*Surface)(nil)
	_ ui.StatusTexter  = (*Surface)(nil)
	_ ui.Closer        = (*Surface)(nil)
	_ ui.ToolStatuser  = (*Surface)(nil)
)

// newSurface builds the instance of route r: Query "open" is a folder,
// none is the folder picker.
func newSurface(ctx *ui.Context, r ui.Route) *Surface {
	s := &Surface{
		id: r.ID, route: r, focus: PaneTree, zoomed: true, appFocus: true,
		prefix: termpane.NewPrefix(prefixKeys),
		tree:   &treeCell{}, code: &codeCell{}, term: &termCell{},
	}
	if dir := r.Query["open"]; dir != "" {
		s.openFolder(ctx, dir)
	} else {
		s.picker = newPicker(ctx, s)
	}
	addLive(s)
	return s
}

// openFolder opens dir: the root, the recovery journal, the documents, the
// tree's first level; it remembers the folder and offers recovery.
func (s *Surface) openFolder(ctx *ui.Context, dir string) {
	root, err := fsroot.Open(dir)
	if err != nil {
		s.openErr = err
		return
	}
	s.root, s.dir = root, root.Dir()
	s.name = baseName(s.dir)
	if ctx.Paths.Cache != "" {
		if j, err := journal.Open(ctx.Paths.Cache, s.dir); err == nil {
			s.jr = j
		}
	}
	s.docs = share.NewDocManager(root, s.jr, nowClock(ctx))
	s.tree.load(root)
	rememberFolder(ctx, s.dir)
	if entries, err := s.docs.Recoverable(); err == nil && len(entries) > 0 {
		s.dlg = newRecoverDialog(s, entries)
	}
}

// baseName is the last element of an absolute folder path.
func baseName(dir string) string {
	d := strings.TrimRight(strings.ReplaceAll(dir, "\\", "/"), "/")
	if i := strings.LastIndexByte(d, '/'); i >= 0 && i+1 < len(d) {
		return d[i+1:]
	}
	return d
}

// nowClock is the engine's clock from the UI's (the FakeClock in tests).
type uiClock struct{ now func() time.Time }

func (c uiClock) Now() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

func nowClock(ctx *ui.Context) share.Clock { return uiClock{now: ctx.Now} }

// sharing reports whether this folder is shared right now (Task 21).
func (s *Surface) sharing() bool { return s.ownerSharing() }

// peopleCount is how many participants are in (Task 21).
func (s *Surface) peopleCount() int { return s.ownerPeople() }

// Init implements ui.Component.
func (s *Surface) Init(ctx *ui.Context) tea.Cmd { return s.armPump(ctx) }

// Resize implements ui.Component.
func (s *Surface) Resize(ctx *ui.Context, size ui.Size) {
	s.size = size
	s.mode = ctx.Mode
	s.relayout(ctx)
}

// relayout places the cells and gives the editor and the terminal their sizes.
func (s *Surface) relayout(ctx *ui.Context) {
	p := planLayout(s.mode, s.size, s.zoomed, s.sharing(), false, s.focus)
	if s.cellZoom {
		p = zoomCell(p, s.focus)
	}
	s.plan = p
	if r := s.termRect(); r.W > 0 && r.H > 1 {
		if t := s.term.current(); t != nil && t.view != nil {
			t.view.Resize(ctx, ui.Size{W: r.W, H: r.H - 1})
		}
	}
	s.syncPaneFocus()
}

// termRect is the terminal cell less its left separator when it stands
// beside the code cell.
func (s *Surface) termRect() layout.Rect {
	r := s.plan.Term
	if r.W > 1 && s.plan.Code.W > 0 && r.Y == s.plan.Code.Y && r.X > s.plan.Code.X {
		r.X++
		r.W--
	}
	return r
}

// setFocus moves the surface's own focus to a cell.
func (s *Surface) setFocus(ctx *ui.Context, pane int) {
	if pane == PanePeople && !s.sharing() {
		return
	}
	s.focus = pane
	s.relayout(ctx)
}

// syncPaneFocus tells the terminals whether the surface's focus is on them.
func (s *Surface) syncPaneFocus() {
	for i, t := range s.term.terms {
		if t.view != nil {
			t.view.SetCellFocus(s.focus == PaneTerm && i == s.term.cur)
		}
	}
}

// Update implements ui.Component.
func (s *Surface) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	switch m := msg.(type) {
	case pumpMsg:
		if m.id != s.id {
			return nil
		}
		return s.onPump(ctx)
	case ui.ToolSwitchedMsg:
		if m.Route.Key() == s.route.Key() && m.Zoomed != s.zoomed {
			s.zoomed = m.Zoomed
			s.relayout(ctx)
		}
		return nil
	case ui.FocusChangedMsg:
		s.appFocus = m.RouteKey == s.route.Key()
		for _, t := range s.term.terms {
			if t.view == nil {
				continue
			}
			fm := m
			if s.appFocus {
				fm.RouteKey = t.routeKey()
			}
			t.view.Update(ctx, fm)
		}
		return nil
	case tea.PasteMsg:
		switch {
		case s.picker != nil:
			s.picker.paste(ctx, m.Content)
		case s.dlg != nil:
			s.dlg.paste(ctx, m.Content)
		case s.focus == PaneTerm:
			if t := s.term.current(); t != nil && t.view != nil {
				return t.view.Update(ctx, msg)
			}
		case s.focus == PaneCode:
			s.code.paste(ctx, s, m.Content)
		}
		return nil
	case termpane.ExitedMsg:
		return s.termExited(ctx, m)
	case hookMsg:
		if m.surface != s.id {
			return nil
		}
		return s.onHook(ctx, m.ev)
	case termpane.NotifyMsg:
		return s.onNotify(ctx, m)
	}
	s.prefix.Update(msg)
	for _, t := range s.term.terms {
		if t.view != nil {
			cmds = append(cmds, t.view.Update(ctx, msg))
		}
	}
	cmds = append(cmds, s.ownerUpdate(ctx, msg))
	return tea.Batch(cmds...)
}

// GrabsKeys implements ui.KeyGrabber: only while the focus is on a running
// terminal that has the keyboard.
func (s *Surface) GrabsKeys() bool {
	if s.picker != nil || s.dlg != nil || s.focus != PaneTerm || s.plan.Tiny {
		return false
	}
	t := s.term.current()
	return t != nil && t.view != nil && t.view.GrabsKeys()
}

// CapturesInput implements ui.InputCapturer.
func (s *Surface) CapturesInput() bool {
	switch {
	case s.picker != nil:
		return true // the path field and the list keys (j/k) are the picker's
	case s.dlg != nil:
		return true
	case s.focus == PaneCode:
		if d := s.code.current(); d != nil {
			return d.ed.CapturesInput()
		}
	}
	return false
}

// HandleKey implements ui.KeyHandler.
func (s *Surface) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.picker != nil {
		cmd := s.picker.handleKey(ctx, msg)
		if s.picker.closed {
			return true, tea.Batch(cmd, ui.CloseTool(s.id))
		}
		return true, cmd
	}
	if s.dlg != nil {
		s.noteOwnerKey(ctx)
		d := s.dlg
		cmd := d.handleKey(ctx, msg)
		if d.closed && s.dlg == d {
			s.dlg = nil
			s.relayout(ctx)
		}
		return true, cmd
	}
	if s.root == nil {
		return false, nil
	}
	s.noteOwnerKey(ctx)
	if s.GrabsKeys() {
		t := s.term.current()
		ok, cmd := t.view.HandleKey(ctx, msg)
		if t.attention && !ctx.Keys.MatchesEscape(msg) { // the owner answers it
			cmd = tea.Batch(cmd, s.setAttention(ctx, t, false))
		}
		return ok, cmd
	}
	r := s.prefix.Key(ctx, msg)
	switch r.Action {
	case termpane.PrefixNone:
	case termpane.PrefixArm:
		return true, r.Cmd
	case termpane.PrefixExtra:
		return true, s.prefixKey(ctx, r.Key)
	case termpane.PrefixZoom:
		return true, s.prefixKey(ctx, "z")
	case termpane.PrefixFocus:
		return true, s.moveFocus(ctx, r.Dir)
	case termpane.PrefixPalette:
		return true, func() tea.Msg { return ui.ShellActionMsg{Action: ui.ShellPalette} }
	case termpane.PrefixSurface:
		return true, func() tea.Msg { return ui.ToolSurfaceMsg{Action: ui.ToolSurfaceToggle} }
	case termpane.PrefixKill:
		if ui.KillSwitchActive() {
			return true, ui.KillAll(ctx)
		}
		return true, nil
	default: // leave, scroll, help, literal, cancel: nothing outside a terminal
		return true, nil
	}
	if s.plan.Tiny {
		return s.tinyKey(ctx, msg)
	}
	switch s.focus {
	case PaneTree:
		return s.treeKey(ctx, msg)
	case PaneCode:
		return s.codeKey(ctx, msg)
	case PaneTerm:
		return s.termKey(ctx, msg)
	case PanePeople:
		return s.peopleKey(ctx, msg)
	}
	return false, nil
}

// prefixKey runs one of this tool's keys after ctrl+\ (from any cell, the
// terminal's through termpane.Options.OnPrefix).
func (s *Surface) prefixKey(ctx *ui.Context, k string) tea.Cmd {
	switch k {
	case "1":
		s.setFocus(ctx, PaneTree)
	case "2":
		s.setFocus(ctx, PaneCode)
	case "3":
		s.setFocus(ctx, PaneTerm)
	case "4":
		s.setFocus(ctx, PanePeople)
	case "n":
		s.openNewTerm(ctx, s.termMode())
	case "e":
		s.openNewTerm(ctx, termhostExec)
	case "S":
		return s.shareKey(ctx)
	case "enter": // a waiting join request first, else the proposals
		if s.nextJoinDialog(ctx) {
			return nil
		}
		return s.openProposals(ctx)
	case "z":
		s.cellZoom = !s.cellZoom
		s.relayout(ctx)
	}
	return nil
}

// moveFocus is ctrl+\ h/j/k/l: the neighbour cell in that direction, else the
// shell's pane to that side.
func (s *Surface) moveFocus(ctx *ui.Context, dir string) tea.Cmd {
	cur := s.plan.rectOf(s.focus)
	best, bestD := 0, 1<<30
	for _, c := range []int{PaneTree, PaneCode, PaneTerm, PanePeople} {
		r := s.plan.rectOf(c)
		if c == s.focus || r.W == 0 || r.H == 0 {
			continue
		}
		var d int
		switch dir {
		case "h":
			d = cur.X - (r.X + r.W)
		case "l":
			d = r.X - (cur.X + cur.W)
		case "k":
			d = cur.Y - (r.Y + r.H)
		case "j":
			d = r.Y - (cur.Y + cur.H)
		}
		if d >= 0 && d < bestD {
			best, bestD = c, d
		}
	}
	if best != 0 {
		s.setFocus(ctx, best)
		return nil
	}
	a := map[string]ui.ShellAction{"h": ui.ShellFocusLeft, "j": ui.ShellFocusDown, "k": ui.ShellFocusUp, "l": ui.ShellFocusRight}[dir]
	return func() tea.Msg { return ui.ShellActionMsg{Action: a} }
}

// View implements ui.Component: exactly size.H lines of size.W cells.
func (s *Surface) View(ctx *ui.Context, focused bool) string {
	w, h := s.size.W, s.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	switch {
	case s.picker != nil:
		return strings.Join(s.picker.view(ctx, w, h), "\n")
	case s.dlg != nil:
		return strings.Join(s.dlg.view(ctx, w, h), "\n")
	case s.root == nil:
		lines := []string{""}
		for _, l := range textwidth.Wrap(ctx.T.Workspace.OpenFailed(), max(w-2, 1)) {
			lines = append(lines, " "+ctx.Theme.S.Warning.Render(l))
		}
		return strings.Join(textwidth.Fit(lines, w, h), "\n")
	}
	p := s.plan
	if p.Tiny {
		return strings.Join(textwidth.Fit(s.tinyView(ctx, w, h), w, h), "\n")
	}
	type block struct {
		r     layout.Rect
		lines []string
	}
	var blocks []block
	add := func(r layout.Rect, lines []string) {
		if r.W > 0 && r.H > 0 {
			blocks = append(blocks, block{r, textwidth.Fit(lines, r.W, r.H)})
		}
	}
	add(p.Band, []string{s.bandLine(ctx, p.Band.W)})
	add(p.SubTabs, []string{s.subTabsLine(ctx, p.SubTabs.W)})
	add(p.Tree, s.tree.view(ctx, s, p.Tree.W, p.Tree.H, focused && s.focus == PaneTree))
	add(p.Code, s.code.view(ctx, s, p.Code.W, p.Code.H, focused && s.focus == PaneCode))
	if tr := s.termRect(); tr != p.Term { // side by side: a separator column on its left
		sep := lipgloss.NewStyle().Foreground(ctx.Theme.P.Line).Render("│")
		col := make([]string, p.Term.H)
		for i := range col {
			col[i] = sep
		}
		add(layout.Rect{X: p.Term.X, Y: p.Term.Y, W: 1, H: p.Term.H}, col)
	}
	add(s.termRect(), s.term.view(ctx, s, s.termRect().W, s.termRect().H, focused && s.focus == PaneTerm))
	add(p.People, s.peopleView(ctx, p.People.W, p.People.H, focused && s.focus == PanePeople))
	out := make([]string, h)
	for y := range h {
		var row []block
		for _, b := range blocks {
			if y >= b.r.Y && y < b.r.Y+b.r.H {
				row = append(row, b)
			}
		}
		slices.SortFunc(row, func(a, b block) int { return cmp.Compare(a.r.X, b.r.X) })
		var sb strings.Builder
		x := 0
		for _, b := range row {
			if b.r.X > x {
				sb.WriteString(strings.Repeat(" ", b.r.X-x))
			}
			sb.WriteString(b.lines[y-b.r.Y])
			x = b.r.X + b.r.W
		}
		out[y] = sb.String()
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// Cursor implements ui.Cursorer.
func (s *Surface) Cursor() *tea.Cursor {
	switch {
	case s.picker != nil:
		return s.picker.cursor
	case s.dlg != nil:
		return s.dlg.cursor
	case s.plan.Tiny:
		return nil
	}
	var c *tea.Cursor
	var r layout.Rect
	switch s.focus {
	case PaneCode:
		d := s.code.current()
		if d == nil {
			return nil
		}
		c, r = d.ed.Cursor(), s.plan.Code
		if c != nil {
			c.Y += s.code.top
		}
	case PaneTerm:
		t := s.term.current()
		if t == nil || t.view == nil {
			return nil
		}
		c, r = t.view.Cursor(), s.termRect()
		if c != nil {
			c.Y++
		}
	}
	if c == nil {
		return nil
	}
	c.X += r.X
	c.Y += r.Y
	return c
}

// Title implements ui.Titled: the folder's name (sanitized).
func (s *Surface) Title(ctx *ui.Context) (string, ui.Badge) {
	if s.picker != nil || s.root == nil {
		return ctx.T.Workspace.PickFolderTitle(), ui.Badge{}
	}
	return textwidth.Sanitize(s.name), s.ownerBadge()
}

// ToolStatus implements ui.ToolStatuser (the hub's open list).
func (s *Surface) ToolStatus(ctx *ui.Context) string {
	if s.root == nil {
		return ""
	}
	t := ctx.T.Workspace
	if s.sharing() {
		return t.HubStatusSharing(textwidth.Sanitize(s.name), s.peopleCount())
	}
	return t.HubStatusPrivate(textwidth.Sanitize(s.name), s.term.count())
}

// StatusText implements ui.StatusTexter: the terminal's while it has the keyboard.
func (s *Surface) StatusText(ctx *ui.Context) (string, bool) {
	if s.focus == PaneTerm && s.dlg == nil && s.picker == nil {
		if t := s.term.current(); t != nil && t.view != nil {
			return t.view.StatusText(ctx)
		}
	}
	return "", false
}

// Help implements ui.Helper: the cell keys after ctrl+\.
func (s *Surface) Help(ctx *ui.Context) []key.Binding {
	t := ctx.T.Workspace
	if f := s.picker; f != nil || s.dlg != nil {
		if f == nil {
			f = s.dlg
		}
		hint := t.DlgHint()
		if f.hint != nil {
			hint = f.hint(ctx)
		}
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp(hint, ""))}
	}
	k := ctx.Keys.Binding(keymap.TermhostEscape).Help().Key
	b := func(keyName, help string) key.Binding {
		return key.NewBinding(key.WithKeys(keyName), key.WithHelp(k+" "+keyName, help))
	}
	if s.own != nil {
		return []key.Binding{key.NewBinding(key.WithKeys("x"), key.WithHelp(t.KillHint(k), "")), b("4", t.PrefixHelpPeople()), b("1", t.PrefixHelpTree()), b("3", t.PrefixHelpTerm())}
	}
	return []key.Binding{b("1", t.PrefixHelpTree()), b("2", t.PrefixHelpCode()), b("3", t.PrefixHelpTerm()), b("n", t.PrefixHelpNewTerm())}
}

// HandleMouse implements ui.MouseHandler: a click focuses the cell under it.
func (s *Surface) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if s.picker != nil || s.dlg != nil || s.root == nil || s.plan.Tiny {
		return nil
	}
	in := func(r layout.Rect) bool {
		return r.W > 0 && local.X >= r.X && local.X < r.X+r.W && local.Y >= r.Y && local.Y < r.Y+r.H
	}
	for _, c := range []int{PaneTree, PaneCode, PaneTerm, PanePeople} {
		r := s.plan.rectOf(c)
		if c == PaneTerm {
			r = s.termRect()
		}
		if !in(r) {
			continue
		}
		if _, click := msg.(tea.MouseClickMsg); click && s.focus != c {
			s.setFocus(ctx, c)
		}
		switch c {
		case PaneTree:
			return s.tree.mouse(ctx, s, msg, local.Y-r.Y)
		case PaneTerm:
			if t := s.term.current(); t != nil && t.view != nil && local.Y > r.Y {
				return t.view.HandleMouse(ctx, msg, ui.Point{X: local.X - r.X, Y: local.Y - r.Y - 1})
			}
		}
		return nil
	}
	return nil
}

// CloseTool implements ui.Closer: the terminals end, the hook endpoint and
// the folder close; unsaved text stays in the recovery journal.
func (s *Surface) CloseTool(ctx *ui.Context) tea.Cmd {
	if s.closed {
		return nil
	}
	s.closed = true
	dropLive(s)
	cmd := s.ownerClose(ctx)
	for _, t := range s.term.terms {
		t.close()
	}
	for _, t := range s.term.execs {
		t.cleanup()
	}
	s.term.terms, s.term.execs = nil, nil
	if s.hooks != nil {
		_ = s.hooks.Close()
		s.hooks = nil
	}
	if s.docs != nil {
		// write the journal for what is still unsaved (its 1 s batching)
		s.docs.Tick(nowClock(ctx).Now().Add(time.Minute))
	}
	if s.root != nil {
		_ = s.root.Close()
	}
	return cmd
}
