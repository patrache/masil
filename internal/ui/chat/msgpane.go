package chat

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	loading      bool   // the first page is not in yet (and no first request failed)
	failed       string // notice of a failed first page ("" = none); cleared once a page lands or the owner retries
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

	// head returns rows drawn above the list's own rows (a thread's root and
	// its replies divider); dens overrides ctx.Density() (a narrow thread
	// column); rootExpanded lifts the root's line cap (nil / false = none).
	head         func(ctx *ui.Context) []row
	dens         func(ctx *ui.Context) ui.Density
	rootExpanded bool

	// sel is the mouse drag selection (nil = none): highlighted while shown,
	// copied on the release. pressOpen is the row whose open (a click on the
	// selected row) waits for a release without motion.
	sel       *textSel
	pressOpen string
}

// lineRef is one drawn line: a row and the line inside it. It survives
// scrolling and new rows (ordered by the row's index, then the line).
type lineRef struct {
	id   string
	line int
}

// textSel is a line-granular selection from anchor (the press) to head.
type textSel struct {
	anchor, head lineRef
	x0, y0       int  // press cell (a click that wobbles one column is no drag)
	dragging     bool // the button is still held
	moved        bool // the pointer moved: the release copies instead of clicking
}

// newMsgPane creates the list of a channel (rootID "") or of a thread's replies.
func newMsgPane(channelID, rootID string) *msgPane {
	p := &msgPane{channelID: channelID, rootID: rootID, index: map[string]int{}, fading: map[string]struct{}{}, spinner: widgets.NewSpinner()}
	p.list = widgets.NewList(func(r row) string { return r.ID }, p.render)
	p.list.Follow(true)
	return p
}

// density is the density the pane draws with (ctx.Density() unless overridden).
func (p *msgPane) density(ctx *ui.Context) ui.Density {
	if p.dens != nil {
		return p.dens(ctx)
	}
	return ctx.Density()
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
	if loaded {
		p.failed = ""
	}
	p.loading = !loaded && p.failed == ""
	p.built = p.density(ctx)
	rows := buildRows(ctx, msgs, pendingFor(ctx, p.channelID, p.rootID), rowOpts{
		LastRead: p.lastRead,
		Loading:  p.loading || p.loadingOlder,
		InThread: p.rootID != "",
	})
	if p.built != ui.DensityFull && ctx.Density() == ui.DensityFull { // date dividers are full-only
		rows = slices.DeleteFunc(rows, func(r row) bool { return r.Kind == rowDay })
	}
	if p.failed != "" {
		rows = append([]row{{Kind: rowNotice, ID: "load-failed", Text: p.failed}}, rows...)
	}
	if p.head != nil {
		head := p.head(ctx)
		// the first date divider repeats the day the root (drawn above it) was sent
		if len(head) > 0 && head[0].Msg != nil {
			day := "day:" + head[0].Msg.CreatedAt.In(locOf(ctx)).Format("20060102")
			if i := slices.IndexFunc(rows, func(r row) bool { return r.Kind == rowDay }); i >= 0 && rows[i].ID == day {
				rows = slices.Delete(rows, i, i+1)
			}
		}
		rows = append(head, rows...)
	}

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
	if ctx != nil && p.density(ctx) != p.built {
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

// firstUnreadAfter is the first message row after anchor that someone else
// wrote (as the unread divider counts: my own messages are never new), "" = none.
func (p *msgPane) firstUnreadAfter(ctx *ui.Context, anchor string) string {
	for _, r := range p.rows {
		if r.Kind != rowMessage || r.Msg == nil || r.Msg.ID <= anchor {
			continue
		}
		if ctx != nil && ctx.Store != nil && ctx.Store.IsMe(r.Msg.Author.ID) {
			continue
		}
		return r.ID
	}
	return ""
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

// handleMouse: the wheel scrolls 3 lines (paging at either edge); a click on a
// reaction chip or a poll bar runs it (hitAction), else a left click on a message
// row selects it, a click on the row that was already selected runs open.
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
		if m.Button == tea.MouseLeft {
			p.clearDrag() // a new press drops the last selection
		}
		if m.Button != tea.MouseLeft && m.Button != tea.MouseRight {
			return nil
		}
		id, line, ok := p.lineAt(ctx, local.Y)
		if !ok {
			return nil
		}
		r := p.rows[p.index[id]]
		if !r.selectable() {
			if m.Button == tea.MouseLeft {
				p.startDrag(id, line, local)
			}
			return nil
		}
		// a reaction chip (left = toggle, right = who reacted) or a poll bar (left = vote)
		if cmd, hit := p.hitAction(ctx, r, line, local.X, m.Button); hit {
			p.selectID(id)
			return cmd
		}
		if m.Button != tea.MouseLeft {
			return nil
		}
		p.startDrag(id, line, local)
		if cur, ok := p.selected(); ok && cur.ID == id {
			if p.open != nil {
				p.pressOpen = id // opened by the release unless the pointer moves
			}
			return nil
		}
		p.selectID(id)
	}
	return nil
}

// lineAt maps a row y of the last list view to the row drawn there and the line
// inside it.
func (p *msgPane) lineAt(_ *ui.Context, y int) (rowID string, line int, ok bool) {
	r, line, ok := p.list.RowAt(y)
	if !ok {
		return "", 0, false
	}
	return r.ID, line, true
}

// ---- drag selection ----

// startDrag starts a selection at line of row id (the press).
func (p *msgPane) startDrag(id string, line int, at ui.Point) {
	ref := lineRef{id: id, line: line}
	p.sel = &textSel{anchor: ref, head: ref, x0: at.X, y0: at.Y, dragging: true}
}

// clearDrag drops the selection and a pending click (esc, a press elsewhere).
func (p *msgPane) clearDrag() {
	p.sel, p.pressOpen = nil, ""
}

// escDrag: esc with a selection shown only clears it.
func (p *msgPane) escDrag(ctx *ui.Context, msg tea.KeyPressMsg) bool {
	if p.sel == nil || !ctx.Keys.Matches(msg, keymap.NavBack) {
		return false
	}
	p.clearDrag()
	return true
}

// handleDrag follows the held button (local is unclipped: above or below the
// list scrolls it by a line and holds the edge line; at the first loaded line
// it loads the older page). The release copies the selected lines (ui.Copy,
// "복사했어요") once the pointer moved; without motion it is a click and runs
// the pending open.
func (p *msgPane) handleDrag(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	s := p.sel
	if s == nil || !s.dragging {
		return nil
	}
	_, release := msg.(tea.MouseReleaseMsg)
	// moved is pointer motion only: the rows under a still pointer shift
	// (a new message while following the bottom, the typing line, the banner),
	// and a click must stay a click
	if !release && (local.Y != s.y0 || abs(local.X-s.x0) > 1) {
		s.moved = true
	}
	var more tea.Cmd
	if s.moved {
		at, ok, top := p.dragRef(ctx, local.Y, !release)
		if ok {
			s.head = at
		}
		if top && p.older != nil {
			more = p.older(ctx) // past the first loaded line: the older page, as the wheel
		}
	}
	if !release {
		return more
	}
	s.dragging = false
	open := p.pressOpen
	p.pressOpen = ""
	if !s.moved {
		p.sel = nil
		if cur, ok := p.selected(); ok && open != "" && cur.ID == open && p.open != nil {
			return p.open(cur)
		}
		return nil
	}
	text := p.selText(ctx)
	if text == "" {
		return nil
	}
	return ui.Copy(ctx, text, msgCopied)
}

// dragRef is the line under list row y, clamped into the list (scrolling one
// line first when scroll and y is past an edge); below the last row it is the
// last line drawn. top reports a scroll up that found the first loaded line
// already at the top (the caller loads the older page).
func (p *msgPane) dragRef(ctx *ui.Context, y int, scroll bool) (ref lineRef, ok, top bool) {
	h := p.listSize.H
	if h <= 0 {
		return lineRef{}, false, false
	}
	if y < 0 || y >= h {
		if scroll {
			if y < 0 {
				a := p.list.Anchor()
				top = len(p.rows) > 0 && a.ItemID == p.rows[0].ID && a.LineOffset == 0
			}
			p.list.ScrollBy(ctx, min(max(y, -1), 1))
			p.list.View(ctx, p.listSize) // refresh RowAt for the new top
		}
		y = min(max(y, 0), h-1)
	}
	for ; y >= 0; y-- {
		if id, line, ok := p.lineAt(ctx, y); ok {
			return lineRef{id: id, line: line}, true, top
		}
	}
	return lineRef{}, false, top
}

// selRange is the selection in row order (ai <= bi); false once a row of it
// is gone.
func (p *msgPane) selRange() (a, b lineRef, ai, bi int, ok bool) {
	if p.sel == nil {
		return lineRef{}, lineRef{}, 0, 0, false
	}
	a, b = p.sel.anchor, p.sel.head
	ai, okA := p.index[a.id]
	bi, okB := p.index[b.id]
	if !okA || !okB {
		return lineRef{}, lineRef{}, 0, 0, false
	}
	if bi < ai || (bi == ai && b.line < a.line) {
		a, b, ai, bi = b, a, bi, ai
	}
	return a, b, ai, bi, true
}

// copyable: line of r is message text (not a divider, a notice or the thread
// root's "더 보기").
func (p *msgPane) copyable(r row, line int) bool {
	if !r.selectable() {
		return false
	}
	return p.rootID == "" || r.ID != p.rootID || p.rootExpanded || line != rootMaxLines
}

// inSel reports whether line of row id is inside the selection.
func (p *msgPane) inSel(id string, line int) bool {
	a, b, ai, bi, ok := p.selRange()
	i, has := p.index[id]
	if !ok || !has || i < ai || i > bi {
		return false
	}
	return (i != ai || line >= a.line) && (i != bi || line <= b.line)
}

// selText is the selection as plain text: each selected line as drawn (no
// escapes, trailing blanks dropped), a message's own indent (full indent,
// bubble padding, right alignment) removed, one line per screen line.
// Folded and deleted messages copy only what they show.
func (p *msgPane) selText(ctx *ui.Context) string {
	a, b, ai, bi, ok := p.selRange()
	if !ok || p.listSize.W <= 0 {
		return ""
	}
	var out []string
	for i := ai; i <= bi; i++ {
		r := p.rows[i]
		if !r.selectable() {
			continue
		}
		lines := p.render(ctx, r, false, p.listSize.W)
		from, to := 0, len(lines)-1
		if i == ai {
			from = a.line
		}
		if i == bi {
			to = min(to, b.line)
		}
		var got []string
		for j := from; j <= to; j++ {
			if p.copyable(r, j) {
				got = append(got, strings.TrimRight(ansi.Strip(lines[j]), " "))
			}
		}
		out = append(out, dedent(got, from == 0)...)
	}
	return strings.Join(out, "\n")
}

// dedent drops a message's own indent: the first line entirely when it is the
// row's first (a header or a one-line row), the indent the other lines share
// from the rest.
func dedent(lines []string, head bool) []string {
	if len(lines) == 0 {
		return lines
	}
	rest := lines
	if head {
		lines[0] = strings.TrimLeft(lines[0], " ")
		rest = lines[1:]
	}
	ind := -1
	for _, l := range rest {
		if l == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if ind < 0 || n < ind {
			ind = n
		}
	}
	for i, l := range rest {
		if ind > 0 && len(l) >= ind {
			rest[i] = l[ind:]
		}
	}
	return lines
}

// paintSel repaints the selected list lines in S.TextSelect (plain text, so
// links and colors give way to the selection while it shows).
func (p *msgPane) paintSel(t *theme.Theme, out []string, width int) {
	if _, _, _, _, ok := p.selRange(); !ok {
		p.sel = nil
		return
	}
	for y := range out {
		r, line, ok := p.list.RowAt(y)
		if !ok || !p.copyable(r, line) || !p.inSel(r.ID, line) {
			continue
		}
		plain := ansi.Strip(out[y])
		out[y] = t.S.TextSelect.Render(plain + strings.Repeat(" ", max(0, width-textwidth.Width(plain))))
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
	switch r.Kind {
	case rowLoading:
		return []string{p.loadingLine(ctx, width)}
	case rowReplies:
		if width <= 0 {
			return nil
		}
		t := themeOf(ctx)
		return []string{rule(t, t.S.Dim, "── "+textwidth.Truncate(r.Text, max(1, width-4), "…")+" ", width, false)}
	}
	st := p.style(ctx, r, selected, width)
	lines := renderRow(ctx, r, st)
	if p.rootID != "" && r.ID == p.rootID && !p.rootExpanded && len(lines) > rootMaxLines {
		t := themeOf(ctx)
		more := []string{fit(t.S.Dim.Render(textwidth.Truncate(rootMoreText, width, "…")), width)}
		if st.Selected {
			more = paint(t, more, t.S.Cursorline, width)
		}
		lines = append(lines[:rootMaxLines:rootMaxLines], more...)
	}
	return lines
}

// style is the msgStyle row r is drawn with at width.
func (p *msgPane) style(ctx *ui.Context, r row, selected bool, width int) msgStyle {
	st := msgStyle{
		Density:  p.density(ctx),
		Width:    width,
		Selected: selected && p.focused && r.selectable(),
		InThread: p.rootID != "",
	}
	if i, ok := p.index[r.ID]; ok && i > 0 {
		st.Continuation = isContinuation(p.rows[i-1], r)
	}
	if _, ok := p.fading[r.ID]; ok {
		s := p.fade.Style(themeOf(ctx), r.ID)
		st.Fade = &s
	}
	return st
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
	if p.sel != nil && p.sel.moved { // a press alone (a click) paints nothing
		p.paintSel(t, out, size.W)
	}
	if banner != "" {
		out = append(out, centerLine(t, t.S.Warning, textwidth.Truncate(banner, size.W, "…"), size.W))
	}
	return strings.Join(textwidth.Fit(out, size.W, size.H), "\n")
}
