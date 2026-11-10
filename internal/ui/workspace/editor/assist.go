package editor

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/workspace/ot"
)

// Syntax colors and the completion menu (v0.1.3-1). Both are mine alone:
// the menu never changes the text until an item is accepted, and accepting
// is an ordinary edit (one op through TakeOps, like typed text).

// assist is the editor's highlighting and completion state.
type assist struct {
	lang      string
	hl        *codeassist.Highlighter
	highlight bool
	complete  bool
	styles    map[chroma.TokenType]lipgloss.Style

	others func() []map[string]int // other open buffers' words
	idx    codeassist.Index
	idxVer int // ver the index counted
	ver    int // bumped by every text change

	open      bool
	forced    bool // ctrl+n / ctrl+space: open from the first char
	triggered bool // a server's trigger character (".") opened it
	items     []codeassist.Item
	sel       int
	top       int

	// The language server (v0.1.3-1), when the owner's surface has one for
	// this document: requests go out through TakeCompletionRequest and the
	// answers come back through SetServerItems.
	server  bool
	trigger []string
	seq     int                // the latest request; older answers are dropped
	want    *CompletionRequest // asked, not yet taken
	asked   anchor             // where the latest request was made
	srv     []codeassist.Item  // the last answer still in force
	srvAt   anchor             // where it was asked
	srvOK   bool
	srvMore bool // the server said isIncomplete: ask again as the word grows
}

// anchor is where a request was made: the cursor's line, where the word
// being typed starts, the line's text before it and the word so far.
type anchor struct {
	row, from    int
	head, prefix string
}

// CompletionRequest is a completion request for the language server.
type CompletionRequest struct {
	Seq      int
	Line     int    // 0-based
	Col      int    // the cursor's rune column
	LineText []rune // the line as asked
}

// SetLanguage sets the language (codeassist ids: codeassist.FromFile);
// "" or one chroma does not know draws plain text.
func (e *Editor) SetLanguage(lang string) {
	if lang == e.as.lang && e.as.hl != nil {
		return
	}
	e.as.lang = lang
	e.as.hl = codeassist.NewHighlighter(lang)
}

// Language is the editor's language.
func (e *Editor) Language() string { return e.as.lang }

// SetTheme switches the theme (a runtime theme change): the text, token
// and menu colors follow it.
func (e *Editor) SetTheme(th *theme.Theme) {
	if th == nil || th == e.th {
		return
	}
	e.th, e.as.styles = th, nil
}

// SetAssist turns syntax colors and the completion menu on or off.
func (e *Editor) SetAssist(highlight, complete bool) {
	e.as.highlight, e.as.complete = highlight, complete
	if !complete {
		e.closeMenu()
	}
}

// SetWordSource gives the completion the other open buffers' words.
func (e *Editor) SetWordSource(f func() []map[string]int) { e.as.others = f }

// Words counts the document's words (cached until the text changes).
func (e *Editor) Words() map[string]int {
	if e.as.idxVer != e.as.ver || e.as.idx.Counts() == nil {
		e.as.idx.Update(strings.Split(string(e.text), "\n"))
		e.as.idxVer = e.as.ver
	}
	return e.as.idx.Counts()
}

// Completion is the open menu: its labels and the selected one (ok=false
// when it is closed).
func (e *Editor) Completion() (labels []string, sel int, ok bool) {
	if !e.as.open {
		return nil, 0, false
	}
	for _, it := range e.as.items {
		labels = append(labels, it.Label)
	}
	return labels, e.as.sel, true
}

// CompletionItems is the open menu's items (nil when it is closed).
func (e *Editor) CompletionItems() []codeassist.Item {
	if !e.as.open {
		return nil
	}
	return slices.Clone(e.as.items)
}

// SetServer says whether a language server serves this document now and
// its trigger characters (the owner's surface, v0.1.3-1). Off drops its
// items and any request not yet taken.
func (e *Editor) SetServer(on bool, trigger []string) {
	e.as.trigger = trigger
	if on == e.as.server {
		return
	}
	e.as.server = on
	if !on {
		e.as.want, e.as.srv, e.as.srvOK = nil, nil, false
		e.as.seq++
		if e.as.open {
			e.fillMenu(true)
		}
	}
}

// TakeCompletionRequest is the request the last key made for the language
// server, once (ok=false: none).
func (e *Editor) TakeCompletionRequest() (CompletionRequest, bool) {
	w := e.as.want
	e.as.want = nil
	if w == nil {
		return CompletionRequest{}, false
	}
	return *w, true
}

// SetServerItems takes the language server's answer to request seq: an
// answer to an older request, or for a cursor that has moved on since, is
// dropped. err (a timeout, no server): the words alone. It reports whether
// the answer was used.
func (e *Editor) SetServerItems(seq int, items []codeassist.Item, incomplete bool, err error) bool {
	if seq != e.as.seq || !e.as.server || !e.menuLive() || !e.at(e.as.asked) {
		return false
	}
	if err != nil {
		items, incomplete = nil, false
	}
	e.as.srv, e.as.srvAt, e.as.srvOK, e.as.srvMore = items, e.as.asked, err == nil, incomplete
	e.fillMenu(e.as.sel > 0) // a choice already made stays; else the server's first item
	return true
}

// here is the cursor's anchor now.
func (e *Editor) here() anchor {
	row, from, _, prefix, _ := e.cursorWord()
	line := e.core.GetBuffer().GetLineRunes(row)
	return anchor{row: row, from: from, head: string(line[:min(from, len(line))]), prefix: prefix}
}

// at reports whether the cursor is still in the word a was asked for.
func (e *Editor) at(a anchor) bool {
	h := e.here()
	return h.row == a.row && h.from == a.from && h.head == a.head
}

// serverItems is the server's answer for the word being typed (filtered
// when the word grew since it was asked), nil when it no longer applies.
func (e *Editor) serverItems(prefix string) []codeassist.Item {
	if !e.as.server || !e.as.srvOK || !e.at(e.as.srvAt) {
		return nil
	}
	if prefix == e.as.srvAt.prefix {
		return e.as.srv
	}
	return codeassist.FilterPrefix(e.as.srv, prefix)
}

// ask makes a request for the word being typed (when a server is on and
// the menu may open); an answer still in force and complete is filtered
// instead of asked again.
func (e *Editor) ask() {
	if !e.as.server || !e.menuLive() {
		return
	}
	h := e.here()
	if !e.as.forced && !e.as.triggered && len([]rune(h.prefix)) < codeassist.MinPrefix {
		return
	}
	if e.as.srvOK && !e.as.srvMore && e.at(e.as.srvAt) && strings.HasPrefix(h.prefix, e.as.srvAt.prefix) {
		return
	}
	pos := e.core.GetBuffer().GetCursor().Position
	e.as.seq++
	e.as.asked = h
	e.as.want = &CompletionRequest{Seq: e.as.seq, Line: pos.Row, Col: pos.Col,
		LineText: slices.Clone(e.core.GetBuffer().GetLineRunes(pos.Row))}
}

// CompletionOpen reports whether the menu is open (tab and enter accept).
func (e *Editor) CompletionOpen() bool { return e.as.open }

// CompletionKeys reports whether ctrl+n / ctrl+p are the editor's now
// (insert mode, completion on, editable).
func (e *Editor) CompletionKeys() bool { return e.menuLive() }

// Paste types text in insert mode with the menu out of the way (a pasted
// newline is a newline, never an accept).
func (e *Editor) Paste(text string) {
	e.closeMenu()
	complete := e.as.complete
	e.as.complete = false
	for _, r := range text {
		if r == '\n' {
			e.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		} else {
			e.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	e.as.complete = complete
}

// isTrigger reports whether text, one typed key, is a trigger character.
func isTrigger(trigger []string, text string) bool {
	return text != "" && slices.Contains(trigger, text)
}

// closeMenu closes the menu for good: a request in flight is dropped
// (its answer must not open it again).
func (e *Editor) closeMenu() {
	e.hideMenu()
	e.as.forced, e.as.triggered = false, false
	if e.as.want != nil || e.as.server {
		e.as.seq++
		e.as.want = nil
	}
}

// hideMenu hides it while there is nothing to show (an answer may still
// fill it).
func (e *Editor) hideMenu() {
	e.as.open, e.as.items, e.as.sel, e.as.top = false, nil, 0, 0
}

// menuKey runs a key while the menu is open (handled=false: not a menu key).
func (e *Editor) menuKey(ks string) bool {
	switch ks {
	case "up", "ctrl+p":
		e.moveMenu(-1)
	case "down", "ctrl+n":
		e.moveMenu(1)
	case "tab", "enter":
		e.acceptCompletion()
	case "esc":
		e.closeMenu()
	default:
		return false
	}
	return true
}

func (e *Editor) moveMenu(d int) {
	n := len(e.as.items)
	if n == 0 {
		return
	}
	e.as.sel = ((e.as.sel+d)%n + n) % n
	if e.as.sel < e.as.top {
		e.as.top = e.as.sel
	}
	if e.as.sel >= e.as.top+codeassist.MaxVisible {
		e.as.top = e.as.sel - codeassist.MaxVisible + 1
	}
}

// cursorWord is the cursor's line, its prefix start and the end of the
// word under the cursor.
func (e *Editor) cursorWord() (row, from, to int, prefix, word string) {
	pos := e.core.GetBuffer().GetCursor().Position
	line := e.core.GetBuffer().GetLineRunes(pos.Row)
	prefix = codeassist.Prefix(line, pos.Col)
	word = codeassist.WordAt(line, pos.Col)
	from = pos.Col - len([]rune(prefix))
	to = pos.Col
	for to < len(line) && codeassist.IsWordRune(line[to]) {
		to++
	}
	if prefix == "" { // a digit-led run is not replaced, only appended to
		word = ""
		to = pos.Col
	}
	return pos.Row, from, to, prefix, word
}

// menuLive reports whether the menu may be open now (insert mode on an
// editable document, completion on).
func (e *Editor) menuLive() bool {
	return e.as.complete && !e.readOnly && e.core.IsInsertMode()
}

// refreshMenu opens, refills or closes the menu for the word being typed;
// the first item is selected.
func (e *Editor) refreshMenu() { e.fillMenu(false) }

// fillMenu is refreshMenu; keep holds on to the selected word when it is
// still offered (a refill for someone else's edit must not move it).
func (e *Editor) fillMenu(keep bool) {
	if !e.menuLive() {
		e.closeMenu()
		return
	}
	_, _, _, prefix, word := e.cursorWord()
	if !e.as.forced && !e.as.triggered && len([]rune(prefix)) < codeassist.MinPrefix {
		e.closeMenu()
		return
	}
	var words []string
	if e.as.forced || len([]rune(prefix)) >= codeassist.MinPrefix {
		sources := []map[string]int{e.Words()}
		if e.as.others != nil {
			sources = append(sources, e.as.others()...)
		}
		words = codeassist.Complete(e.as.lang, prefix, word, sources...)
	}
	items := codeassist.Merge(e.serverItems(prefix), words)
	if len(items) == 0 {
		if e.as.server {
			e.hideMenu() // its answer may still come
		} else {
			e.closeMenu()
		}
		return
	}
	sel, top := 0, 0
	if keep && e.as.open && e.as.sel < len(e.as.items) {
		cur := e.as.items[e.as.sel].Label
		if i := slices.IndexFunc(items, func(it codeassist.Item) bool { return it.Label == cur }); i >= 0 {
			sel, top = i, min(e.as.top, i) // drawMenu scrolls it into view
		}
	}
	e.as.open, e.as.items, e.as.sel, e.as.top = true, items, sel, top
}

// acceptCompletion replaces the word under the cursor — or a server
// item's own range — with the selected item: one edit, reported by TakeOps.
func (e *Editor) acceptCompletion() {
	if !e.as.open || len(e.as.items) == 0 {
		e.closeMenu()
		return
	}
	item := e.as.items[e.as.sel]
	e.closeMenu()
	pos := e.core.GetBuffer().GetCursor().Position
	from, to := item.Span(e.core.GetBuffer().GetLineRunes(pos.Row), pos.Col)
	before := e.text
	start := indexOf(before, core.Position{Row: pos.Row, Col: from})
	end := indexOf(before, core.Position{Row: pos.Row, Col: to})
	ins := []rune(item.Text)
	next := slices.Concat(before[:start], ins, before[end:])
	e.load(next, positionOf(next, start+len(ins)))
	e.ops = append(e.ops, ot.Diff(before, next))
	e.core.ScrollViewport()
	e.scrollColumn()
}

// afterAssistKey updates the menu after a key the editor ran: typing a word
// rune refills it (or opens it), anything else but a deletion closes it.
func (e *Editor) afterAssistKey(code rune, text string) {
	if !e.core.IsInsertMode() {
		e.closeMenu()
		return
	}
	switch {
	case text != "":
		for _, r := range text {
			if !codeassist.IsWordRune(r) {
				e.closeMenu()
				if e.as.server && isTrigger(e.as.trigger, text) {
					e.as.triggered = true // "fmt.": the server's members, words only once typed
					e.ask()
				}
				return
			}
		}
		e.refreshMenu()
		e.ask()
	case code == tea.KeyBackspace || code == tea.KeyDelete:
		if e.as.open || e.as.want != nil || e.as.triggered {
			e.refreshMenu()
			e.ask()
		}
	default:
		e.closeMenu()
	}
}

// tokenStyle is tt's color in the theme (cached).
func (e *Editor) tokenStyle(tt chroma.TokenType) lipgloss.Style {
	if e.as.styles == nil {
		e.as.styles = map[chroma.TokenType]lipgloss.Style{}
	}
	st, ok := e.as.styles[tt]
	if !ok {
		st = e.th.EditorTokenStyle(tt)
		e.as.styles[tt] = st
	}
	return st
}

// spans is the token spans of the shown rows [top, top+rows) (nil when
// highlighting is off).
func (e *Editor) spans(top, rows int) [][]codeassist.Span {
	if !e.as.highlight || e.as.hl == nil {
		return nil
	}
	b := e.core.GetBuffer()
	n := b.LineCount()
	return e.as.hl.Window(b.GetLineRunes, n, top, min(top+rows, n))
}

// spanAt is the token type at rune column col of a row's spans; i is the
// span to start looking from (the cells of a row come in order).
func spanAt(spans []codeassist.Span, i *int, col int) (chroma.TokenType, bool) {
	for *i < len(spans) && spans[*i].To <= col {
		*i++
	}
	if *i < len(spans) && spans[*i].From <= col {
		return spans[*i].Type, true
	}
	return 0, false
}

// drawMenu lays the open menu over the drawn rows: under the cursor's line
// (above it when there is no room), from the start of the typed word.
func (e *Editor) drawMenu(out []string, rows int) {
	if !e.as.open || len(e.as.items) == 0 || rows <= 0 || !e.menuLive() {
		return
	}
	st := e.core.GetState()
	b := e.core.GetBuffer()
	row, from, _, _, _ := e.cursorWord()
	y := row - st.TopLine
	if y < 0 || y >= rows {
		return
	}
	total := len(e.as.items)
	want := min(total, codeassist.MaxVisible)
	n := want
	below, above := rows-y-1, y
	y0 := y + 1
	switch {
	case below >= want:
	case above >= want:
		y0 = y - want
	case below >= above:
		n = below
	default:
		n, y0 = above, 0
	}
	if n <= 0 {
		return
	}
	// The scroll window follows the rows drawn (fewer than MaxVisible in a
	// short pane), so the selected item is always one of them.
	top := min(e.as.top, e.as.sel)
	if e.as.sel >= top+n {
		top = e.as.sel - n + 1
	}
	top = max(0, min(top, total-n))
	e.as.top = top
	labels, details := make([]string, n), make([]string, n)
	lw, dw := 0, 0
	for i := range n {
		it := e.as.items[top+i]
		labels[i] = textwidth.Sanitize(it.Label)
		details[i] = textwidth.Sanitize(it.Detail)
		lw = max(lw, textwidth.Width(labels[i]))
		dw = max(dw, textwidth.Width(details[i]))
	}
	w := lw + 2
	if dw > 0 {
		w += dw + 2 // "  detail", dim
	}
	w = min(w, e.w)
	x := e.gutterWidth() + cellX(layoutLine(b.GetLineRunes(row)), from) - e.left
	x = max(0, min(x, e.w-w))
	item := lipgloss.NewStyle().Background(e.th.P.Chip).Foreground(e.th.P.FG)
	sel := lipgloss.NewStyle().Background(e.th.P.Acc).Foreground(e.th.P.BG).Bold(true)
	for i := range n {
		style := item
		if top+i == e.as.sel {
			style = sel
		}
		var cell string
		if dw == 0 || lw+2 >= w {
			cell = style.Render(textwidth.PadRight(" "+textwidth.Truncate(labels[i], w-2, "…"), w))
		} else {
			left := style.Render(textwidth.PadRight(" "+labels[i], lw+1))
			dim := style.Bold(false).Faint(true)
			if top+i != e.as.sel {
				dim = dim.Foreground(e.th.P.Dim)
			}
			right := dim.Render(textwidth.PadRight("  "+textwidth.Truncate(details[i], w-lw-3, "…"), w-lw-1))
			cell = left + right
		}
		r := y0 + i
		out[r] = splice(out[r], x, w, cell)
	}
}

// splice puts box (w cells) over line at cell column x.
func splice(line string, x, w int, box string) string {
	left := ansi.Truncate(line, x, "")
	if lw := textwidth.Width(left); lw < x {
		left += strings.Repeat(" ", x-lw)
	}
	right := ansi.TruncateLeft(line, x+w, "")
	return left + "\x1b[m" + box + right
}
