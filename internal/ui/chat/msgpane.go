package chat

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

// msgPane is a message list (the channel's top-level messages, or one thread's
// replies when rootID is set): rows from chatstore plus the list's pending sends,
// selection on message rows only, bottom following, the "새 메시지 N개 ↓" banner
// and the new-message highlight. Its owner loads pages (older / newer) and
// decides what a selected row opens.
type msgPane struct {
	channelID, rootID string

	list  *widgets.List[row]
	rows  []row
	index map[string]int // row id -> index in rows

	fade    widgets.Fade
	fading  map[string]struct{} // ids being highlighted (pruned once their fade ends)
	spinner *widgets.Spinner

	lastRead     string // unread divider pointer, frozen when the pane is created
	loading      bool   // the first page is not in yet
	loadingOlder bool   // an older page is in flight
	newBelow     int    // new messages that arrived below while not following
	noFollow     bool   // the next refresh appends a newer page: never follow nor count it

	started  bool          // the first loaded content got its initial selection
	restore  *channelSaved // state to apply once content is loaded
	lastRow  string        // last selectable row id of the previous refresh
	focused  bool          // of the View being drawn (render reads it)
	built    ui.Density    // density the rows were built for (date dividers are full-only)
	size     ui.Size       // last view size (whole pane)
	listSize ui.Size       // last list size (pane minus the banner)

	// older / newer load the next page at the top / bottom edge; open runs a
	// click on the already selected row (nil = none).
	older, newer func(ctx *ui.Context) tea.Cmd
	open         func(r row) tea.Cmd
}

// newMsgPane creates the list of a channel (rootID "") or of a thread's replies.
func newMsgPane(channelID, rootID string) *msgPane {
	p := &msgPane{channelID: channelID, rootID: rootID, index: map[string]int{}, fading: map[string]struct{}{}, spinner: widgets.NewSpinner()}
	p.list = widgets.NewList(func(r row) string { return r.ID }, p.render)
	p.list.Follow(true)
	return p
}

// selectable rows are messages and pending sends (never dividers or notices).
func (r row) selectable() bool { return r.Kind == rowMessage || r.Kind == rowPending }

// window is the store window the pane lists (nil when none is held).
func (p *msgPane) window(ctx *ui.Context) *store.Window[api.Message] {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	if p.rootID != "" {
		_, w := c.Thread(p.rootID)
		return w
	}
	if !c.HasWindow(p.channelID) {
		return nil
	}
	return c.Window(p.channelID)
}

// loaded reports whether a REST page of the list has landed.
func (p *msgPane) loaded(ctx *ui.Context) bool {
	c := chatOf(ctx)
	if c == nil {
		return false
	}
	if p.rootID != "" {
		root, _ := c.Thread(p.rootID)
		return root != nil
	}
	return c.Loaded(p.channelID)
}

// atBottom: the view shows the end of the list and the window is the latest page.
func (p *msgPane) atBottom(ctx *ui.Context) bool {
	if !p.list.AtBottom() {
		return false
	}
	w := p.window(ctx)
	return w == nil || !w.HasMoreAfter()
}

// refresh rebuilds the rows from chatstore (selection and scroll kept by id) and
// applies the following rule: at the bottom with the last row selected, new rows
// below take the selection; otherwise they only count into the banner.
func (p *msgPane) refresh(ctx *ui.Context) {
	var msgs []api.Message
	if w := p.window(ctx); w != nil {
		msgs = w.Items()
	}
	loaded := p.loaded(ctx)
	p.loading = !loaded
	p.built = ctx.Density()
	rows := buildRows(ctx, msgs, pendingFor(ctx, p.channelID, p.rootID), rowOpts{
		LastRead: p.lastRead,
		Loading:  p.loading || p.loadingOlder,
		InThread: p.rootID != "",
	})

	prev, hadSel := p.selected()
	wasLast := !hadSel || prev.ID == p.lastRow
	bottom := p.list.AtBottom()
	oldLast := p.lastRow
	p.setRows(rows)
	newLast := p.lastSelectable()
	p.lastRow = newLast

	switch {
	case !p.started:
		if loaded {
			p.started = true
			if newLast != "" {
				p.list.Select(newLast)
			}
			p.applyRestore()
		}
	case p.noFollow:
		p.noFollow = false
	case newLast == oldLast || newLast == "":
	case oldLast == "" || !p.has(oldLast):
		if wasLast {
			p.list.Select(newLast)
		}
	default:
		if wasLast && bottom {
			p.list.Select(newLast)
			break
		}
		for _, r := range rows[p.index[oldLast]+1:] {
			if r.Kind == rowMessage && r.Msg != nil && !isMe(ctx, r.Msg.Author.ID) && !ctx.Store.IsBlocked(r.Msg.Author.ID) { // my own sends and folded messages never count
				p.newBelow++
			}
		}
	}
	if sel, ok := p.selected(); ok && sel.ID == p.lastRow {
		p.newBelow = 0
	}
}

// sync rebuilds the rows when the density changed since they were built (a mode
// switch adds or drops the date dividers).
func (p *msgPane) sync(ctx *ui.Context) {
	if ctx != nil && ctx.Density() != p.built {
		p.refresh(ctx)
	}
}

// setRows stores rows and hands them to the list (which drops its render cache).
func (p *msgPane) setRows(rows []row) {
	p.rows = rows
	p.index = make(map[string]int, len(rows))
	for i, r := range rows {
		if _, dup := p.index[r.ID]; !dup {
			p.index[r.ID] = i
		}
	}
	p.list.SetItems(rows)
}

// repaint re-renders every row (render inputs outside the rows changed).
func (p *msgPane) repaint() { p.list.SetItems(p.rows) }

func (p *msgPane) has(id string) bool {
	_, ok := p.index[id]
	return ok
}

// lastSelectable is the id of the last message / pending row ("" = none).
func (p *msgPane) lastSelectable() string {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if p.rows[i].selectable() {
			return p.rows[i].ID
		}
	}
	return ""
}

// firstSelectable is the id of the first message / pending row ("" = none).
func (p *msgPane) firstSelectable() string {
	for _, r := range p.rows {
		if r.selectable() {
			return r.ID
		}
	}
	return ""
}

// selected returns the selected row.
func (p *msgPane) selected() (row, bool) {
	return p.list.Selected()
}

// selectID selects a row by id (unknown ids are ignored) and scrolls it into view.
func (p *msgPane) selectID(id string) bool {
	if !p.has(id) {
		return false
	}
	p.list.Select(id)
	if id == p.lastRow {
		p.newBelow = 0
	}
	return true
}

// applyRestore applies saved selection and scroll (Persistent) once.
func (p *msgPane) applyRestore() {
	st := p.restore
	if st == nil {
		return
	}
	p.restore = nil
	if st.Selected != "" {
		p.list.Select(st.Selected)
	}
	p.list.Restore(st.Anchor)
}

// snap moves a selection that landed on a divider to the nearest selectable row,
// first in direction dir.
func (p *msgPane) snap(dir int) {
	cur, ok := p.selected()
	if !ok || cur.selectable() {
		return
	}
	i := p.index[cur.ID]
	for _, d := range []int{dir, -dir} {
		for j := i + d; j >= 0 && j < len(p.rows); j += d {
			if p.rows[j].selectable() {
				p.list.Select(p.rows[j].ID)
				return
			}
		}
	}
}

// handleKey handles list navigation; at the first (last) message an up (down)
// move asks the owner for the older (newer) page.
func (p *msgPane) handleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	prev, _ := p.selected()
	atFirst := prev.ID != "" && prev.ID == p.firstSelectable()
	atLast := prev.ID != "" && prev.ID == p.lastSelectable()
	up := km.Matches(msg, keymap.ListUp) || km.Matches(msg, keymap.ListPageUp) || km.Matches(msg, keymap.ListTop)
	down := km.Matches(msg, keymap.ListDown) || km.Matches(msg, keymap.ListPageDown) || km.Matches(msg, keymap.ListBottom)
	handled, cmd := p.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	dir := 1
	if up || msg.String() == "g" {
		dir = -1
	}
	p.snap(dir)
	if km.Matches(msg, keymap.ListBottom) {
		p.newBelow = 0
	}
	if sel, ok := p.selected(); ok && sel.ID == p.lastRow {
		p.newBelow = 0
	}
	var more tea.Cmd
	switch {
	case up && atFirst && p.older != nil:
		more = p.older(ctx)
	case down && atLast && p.newer != nil:
		more = p.newer(ctx)
	}
	return true, tea.Batch(cmd, more)
}

// handleMouse: the wheel scrolls 3 lines (paging at either edge); a left click on a
// message row selects it, a click on the row that was already selected runs open.
func (p *msgPane) handleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		cmd := p.list.HandleMouse(ctx, msg, local)
		var more tea.Cmd
		switch m.Button {
		case tea.MouseWheelUp:
			if a := p.list.Anchor(); len(p.rows) > 0 && a.ItemID == p.rows[0].ID && a.LineOffset == 0 && p.older != nil {
				more = p.older(ctx)
			}
		case tea.MouseWheelDown:
			if p.list.AtBottom() && p.newer != nil {
				more = p.newer(ctx)
			}
		}
		return tea.Batch(cmd, more)
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		id, _, ok := p.lineAt(ctx, local.Y)
		if !ok {
			return nil
		}
		r := p.rows[p.index[id]]
		if !r.selectable() {
			return nil
		}
		if cur, ok := p.selected(); ok && cur.ID == id && p.open != nil {
			return p.open(r)
		}
		p.selectID(id)
	}
	return nil
}

// lineAt maps a row y of the last list view to the row drawn there and the line
// inside it.
func (p *msgPane) lineAt(ctx *ui.Context, y int) (rowID string, line int, ok bool) {
	w, h := p.listSize.W, p.listSize.H
	if y < 0 || y >= h || w <= 0 || len(p.rows) == 0 {
		return "", 0, false
	}
	a := p.list.Anchor()
	i, found := p.index[a.ItemID]
	if !found {
		return "", 0, false
	}
	sel, _ := p.selected()
	off := a.LineOffset
	for ; i < len(p.rows); i++ {
		n := max(1, len(p.render(ctx, p.rows[i], p.rows[i].ID == sel.ID, w)))
		if y < n-off {
			return p.rows[i].ID, y + off, true
		}
		y -= n - off
		off = 0
	}
	return "", 0, false
}

// startFade highlights a newly arrived message.
func (p *msgPane) startFade(ctx *ui.Context, id string) tea.Cmd {
	cmd := p.fade.Start(ctx, id)
	if cmd != nil {
		p.fading[id] = struct{}{}
		p.repaint()
	}
	return cmd
}

// update drives the highlight and the spinner (their ticks re-render the rows).
func (p *msgPane) update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if fc := p.fade.Update(ctx, msg); fc != nil || len(p.fading) > 0 {
		cmds = append(cmds, fc)
		t := themeOf(ctx)
		for id := range p.fading {
			if !fadingNow(t, &p.fade, id) {
				delete(p.fading, id)
			}
		}
		p.repaint()
	}
	if sc := p.spinner.Update(ctx, msg); sc != nil {
		cmds = append(cmds, sc)
		p.repaint()
	}
	return tea.Batch(cmds...)
}

// fadingNow reports whether f still highlights id (a finished fade is Base again).
func fadingNow(t *theme.Theme, f *widgets.Fade, id string) bool {
	a, b := f.Style(t, id).GetBackground(), t.S.Base.GetBackground()
	if a == nil || b == nil {
		return a != b
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2
}

// startLoading / stopLoading run the spinner of the loading row.
func (p *msgPane) startLoading(ctx *ui.Context) tea.Cmd { return p.spinner.Start(ctx) }
func (p *msgPane) stopLoading() {
	if !p.loading && !p.loadingOlder {
		p.spinner.Stop()
	}
}

// render draws one row (selection painted only while the pane is focused).
func (p *msgPane) render(ctx *ui.Context, r row, selected bool, width int) []string {
	t := themeOf(ctx)
	if r.Kind == rowLoading {
		return []string{p.loadingLine(ctx, width)}
	}
	st := msgStyle{
		Density:  ctx.Density(),
		Width:    width,
		Selected: selected && p.focused && r.selectable(),
		InThread: p.rootID != "",
	}
	if i, ok := p.index[r.ID]; ok && i > 0 {
		st.Continuation = isContinuation(p.rows[i-1], r)
	}
	if _, ok := p.fading[r.ID]; ok {
		s := p.fade.Style(t, r.ID)
		st.Fade = &s
	}
	return renderRow(ctx, r, st)
}

// loadingLine is "<spinner> 불러오는 중…" centered in width cells.
func (p *msgPane) loadingLine(ctx *ui.Context, width int) string {
	t := themeOf(ctx)
	text := loadingText
	if width >= textwidth.Width(text)+2 {
		text = " " + text
		gap := width - 1 - textwidth.Width(text)
		left := gap / 2
		return spaces(t.S.Base, left) + p.spinner.View(t) + t.S.Dim.Render(text) + spaces(t.S.Base, gap-left)
	}
	return centerLine(t, t.S.Dim, textwidth.Truncate(text, width, "…"), width)
}

// bannerText is the "new messages below" banner ("" = none).
func (p *msgPane) bannerText() string {
	if p.newBelow <= 0 {
		return ""
	}
	return "새 메시지 " + strconv.Itoa(p.newBelow) + "개 ↓"
}

// view draws the list and, while messages wait below, the banner line under it:
// exactly size.H lines of size.W cells.
func (p *msgPane) view(ctx *ui.Context, size ui.Size, focused bool) string {
	p.size = size
	if size.W <= 0 || size.H <= 0 {
		p.listSize = ui.Size{}
		return ""
	}
	p.sync(ctx)
	if focused != p.focused {
		p.focused = focused
		p.repaint()
	}
	t := themeOf(ctx)
	banner := p.bannerText()
	lh := size.H
	if banner != "" && size.H > 1 {
		lh--
	} else {
		banner = ""
	}
	p.listSize = ui.Size{W: size.W, H: lh}
	out := strings.Split(p.list.View(ctx, p.listSize), "\n")
	if banner != "" {
		out = append(out, centerLine(t, t.S.Warning, textwidth.Truncate(banner, size.W, "…"), size.W))
	}
	return strings.Join(textwidth.Fit(out, size.W, size.H), "\n")
}
