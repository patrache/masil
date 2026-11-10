package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/textwidth"
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

	open   bool
	forced bool // ctrl+n / ctrl+space: open from the first char
	items  []string
	sel    int
	top    int
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

// Completion is the open menu: its items and the selected one (ok=false
// when it is closed).
func (e *Editor) Completion() (items []string, sel int, ok bool) {
	if !e.as.open {
		return nil, 0, false
	}
	return append([]string(nil), e.as.items...), e.as.sel, true
}

// CompletionOpen reports whether the menu is open (tab and enter accept).
func (e *Editor) CompletionOpen() bool { return e.as.open }

// CompletionKeys reports whether ctrl+n / ctrl+p are the editor's now
// (insert mode, completion on, editable).
func (e *Editor) CompletionKeys() bool {
	return e.as.complete && !e.readOnly && e.core.IsInsertMode()
}

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

func (e *Editor) closeMenu() {
	e.as.open, e.as.forced, e.as.items, e.as.sel, e.as.top = false, false, nil, 0, 0
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

// refreshMenu opens, refills or closes the menu for the word being typed.
func (e *Editor) refreshMenu() {
	if !e.as.complete || e.readOnly || !e.core.IsInsertMode() {
		e.closeMenu()
		return
	}
	_, _, _, prefix, word := e.cursorWord()
	if !e.as.forced && len([]rune(prefix)) < codeassist.MinPrefix {
		e.closeMenu()
		return
	}
	sources := []map[string]int{e.Words()}
	if e.as.others != nil {
		sources = append(sources, e.as.others()...)
	}
	items := codeassist.Complete(e.as.lang, prefix, word, sources...)
	if len(items) == 0 {
		e.closeMenu()
		return
	}
	e.as.open, e.as.items, e.as.sel, e.as.top = true, items, 0, 0
}

// acceptCompletion replaces the word under the cursor with the selected
// item: one edit, reported by TakeOps.
func (e *Editor) acceptCompletion() {
	if !e.as.open || len(e.as.items) == 0 {
		e.closeMenu()
		return
	}
	item := e.as.items[e.as.sel]
	e.closeMenu()
	row, from, to, _, _ := e.cursorWord()
	b := e.core.GetBuffer()
	if to > from {
		_ = b.DeleteRunesAt(row, from, to-from)
	}
	if err := b.InsertRunesAt(row, from, []rune(item)); err != nil {
		e.load(e.text, b.GetCursor().Position) // put the text back
		return
	}
	col := from + len([]rune(item))
	b.SetCursor(core.Cursor{Position: core.Position{Row: row, Col: col}, Preferred: col})
	e.afterKey()
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
				return
			}
		}
		e.refreshMenu()
	case code == tea.KeyBackspace || code == tea.KeyDelete:
		if e.as.open {
			e.refreshMenu()
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
	if !e.as.open || len(e.as.items) == 0 || rows <= 0 {
		return
	}
	st := e.core.GetState()
	b := e.core.GetBuffer()
	row, from, _, _, _ := e.cursorWord()
	y := row - st.TopLine
	if y < 0 || y >= rows {
		return
	}
	n := min(len(e.as.items)-e.as.top, codeassist.MaxVisible)
	below, above := rows-y-1, y
	y0 := y + 1
	switch {
	case below >= n:
	case above >= n:
		y0 = y - n
	case below >= above:
		n = below
	default:
		n, y0 = above, 0
	}
	if n <= 0 {
		return
	}
	labels := make([]string, n)
	w := 0
	for i := range n {
		labels[i] = textwidth.Sanitize(e.as.items[e.as.top+i])
		w = max(w, textwidth.Width(labels[i]))
	}
	w = min(w+2, e.w)
	x := e.gutterWidth() + cellX(layoutLine(b.GetLineRunes(row)), from) - e.left
	x = max(0, min(x, e.w-w))
	item := lipgloss.NewStyle().Background(e.th.P.Chip).Foreground(e.th.P.FG)
	sel := lipgloss.NewStyle().Background(e.th.P.Acc).Foreground(e.th.P.BG).Bold(true)
	for i := range n {
		style := item
		if e.as.top+i == e.as.sel {
			style = sel
		}
		cell := style.Render(textwidth.PadRight(" "+textwidth.Truncate(labels[i], w-2, "…"), w))
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
