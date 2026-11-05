package widgets

import (
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

const (
	// composerCharLimit is the message body limit in code points (api.md: 1..4000).
	// It is enforced by the Composer itself because textarea.CharLimit counts cells.
	composerCharLimit = 4000
	// maxCompletionRows is the completion popup height cap.
	maxCompletionRows = 5
	// maxPendingBytes caps the raw key text kept for cross-key renormalization.
	maxPendingBytes = 64
	// maxPasteBytes bounds the raw text normalized for one paste (far above the limit).
	maxPasteBytes = 1 << 20
	// selectedPrefix marks the selected completion row in labels (plan 04 convention).
	selectedPrefix = "› "
)

// Composer wraps a textarea: virtual cursor OFF, no line numbers, no prompt,
// CharLimit from limits; KeyMap removes ctrl+k (DeleteAfterCursor), ctrl+f
// (CharacterForward), ctrl+n (LineNext) — they are hard globals; InsertNewline =
// shift+enter, alt+enter, ctrl+j (enter is send). INPUT NORMALIZATION (CA 8.4):
// KeyPressMsg.Text, PasteMsg.Content and SetValue go through textwidth.SanitizeInput
// BEFORE reaching the textarea.
//
// The textarea is the edit buffer; the Composer draws it itself (grapheme-wise soft
// wrap, every cell themed) so the real cursor column always equals the width of the
// text before it. Further decisions:
//   - Text keys are normalized together with the grapheme before the cursor and any
//     earlier key text that had no visible effect (a lone ZWJ, VS16, skin tone), so
//     "👨", "‍", "👩" typed separately end as SanitizeInput("👨‍👩"). After every
//     edit the whole value is re-normalized if needed, keeping the cursor at the
//     normalized length of the text before it.
//   - left/right/backspace/delete work on whole grapheme clusters; up/down move by
//     drawn rows. ctrl+w (window prefix) and ctrl+v (clipboard paste, which would
//     bypass normalization) are unbound too; selection and page keys are unbound
//     because the Composer does not draw them.
//   - ctrl+c with text clears it (Reset, popup closed, nil Cmd); on an empty value it
//     does nothing. Embedding components report handled = (Value() != "" before
//     Update) for ctrl+c, so the second press reaches the app's quit arming.
//   - While a completion popup is shown: up/down move its selection, tab/enter
//     replace the word before the cursor with the item's Insert and close it, esc
//     closes it. A Label starting with "› " marks the selected row.
//   - enter, tab and esc are otherwise ignored (the embedding component owns them).
type Composer struct {
	ta          textarea.Model
	placeholder string
	items       []Completion
	sel         int
	pending     string // raw key text absorbed without visible effect
	top         int    // first drawn row
	lastW       int    // inner width of the last View (0 = never drawn)
	lastMax     int    // maxRows of the last View
}

// Completion is one @mention / :emoji / /command popup row.
type Completion struct{ Label, Insert, Detail string }

// NewComposer creates a composer.
func NewComposer(placeholder string) *Composer {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // code point limit enforced by the Composer (textarea counts cells)
	ta.MaxHeight = 0
	ta.MaxWidth = 0
	ta.SetVirtualCursor(false)
	ta.KeyMap = composerKeyMap()
	ta.SetWidth(80)
	return &Composer{ta: ta, placeholder: textwidth.Sanitize(placeholder), lastMax: 1}
}

func composerKeyMap() textarea.KeyMap {
	km := textarea.DefaultKeyMap()
	km.DeleteAfterCursor = withoutKeys(km.DeleteAfterCursor, "ctrl+k")
	km.CharacterForward = withoutKeys(km.CharacterForward, "ctrl+f")
	km.LineNext = withoutKeys(km.LineNext, "ctrl+n")
	km.DeleteWordBackward = withoutKeys(km.DeleteWordBackward, "ctrl+w")
	km.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"), key.WithHelp("shift+enter", "줄바꿈"))
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.PageUp, km.PageDown = none, none
	km.SelectCharacterForward, km.SelectCharacterBackward = none, none
	km.SelectWordForward, km.SelectWordBackward = none, none
	km.SelectLineUp, km.SelectLineDown = none, none
	km.SelectAll, km.CopySelection = none, none
	return km
}

func withoutKeys(b key.Binding, drop ...string) key.Binding {
	keys := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(drop, k) })
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(b.Help().Key, b.Help().Desc))
}

// Value is already normalized; this exact string is what gets sent.
func (c *Composer) Value() string {
	return c.ta.Value()
}

// SetValue normalizes (edit-last loads the server body through this); the cursor
// goes to the end.
func (c *Composer) SetValue(s string) {
	c.pending = ""
	c.apply(s, "")
}

// Reset clears the input and closes the completion popup.
func (c *Composer) Reset() {
	c.ta.Reset()
	c.items, c.sel = nil, 0
	c.pending = ""
	c.top = 0
}

// Focus focuses the input.
func (c *Composer) Focus() tea.Cmd {
	return c.ta.Focus()
}

// Blur blurs the input.
func (c *Composer) Blur() {
	c.ta.Blur()
	c.pending = ""
}

// Focused reports focus.
func (c *Composer) Focused() bool {
	return c.ta.Focused()
}

// Update feeds a message to the textarea (after normalization). Input is ignored
// while blurred.
func (c *Composer) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if !c.Focused() {
		return nil
	}
	switch m := msg.(type) {
	case tea.PasteMsg:
		c.insert(m.Content)
	case tea.KeyPressMsg:
		return c.handleKey(m)
	}
	return nil
}

func (c *Composer) handleKey(m tea.KeyPressMsg) tea.Cmd {
	s := m.String()
	if s == "ctrl+c" {
		if c.Value() != "" {
			c.Reset()
		}
		return nil
	}
	if len(c.items) > 0 {
		switch s {
		case "up":
			c.sel = max(0, c.sel-1)
			return nil
		case "down":
			c.sel = min(len(c.items)-1, c.sel+1)
			return nil
		case "tab", "enter":
			c.accept()
			return nil
		case "esc":
			c.items, c.sel = nil, 0
			return nil
		}
	}
	km := c.ta.KeyMap
	switch {
	case s == "enter" || s == "tab" || s == "esc":
		return nil
	case key.Matches(m, km.InsertNewline):
		c.insert("\n")
	case isTextKey(m):
		c.insert(m.Text)
	case key.Matches(m, km.CharacterBackward):
		c.moveGrapheme(-1)
	case key.Matches(m, km.CharacterForward):
		c.moveGrapheme(+1)
	case key.Matches(m, km.LinePrevious):
		c.moveRow(-1)
	case key.Matches(m, km.LineNext):
		c.moveRow(+1)
	case key.Matches(m, km.DeleteCharacterBackward):
		c.deleteGrapheme(-1)
	case key.Matches(m, km.DeleteCharacterForward):
		c.deleteGrapheme(+1)
	default:
		c.pending = ""
		var cmd tea.Cmd
		c.ta, cmd = c.ta.Update(m)
		c.apply(c.split())
		return cmd
	}
	return nil
}

// isTextKey reports a key that types text (no ctrl/alt/meta/super/hyper modifier).
func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// View auto-grows 1..maxRows, bordered with acc when focused. The completion popup
// (at most 5 rows) is drawn above the box; every line is exactly width cells.
func (c *Composer) View(ctx *ui.Context, width, maxRows int) string {
	if width <= 0 {
		return ""
	}
	t := themeOf(ctx)
	maxRows = max(1, maxRows)
	iw := max(1, width-2)
	c.lastW, c.lastMax = iw, maxRows
	v := c.Value()
	rows, cr, _ := layoutRows(v, len(c.beforeCursor()), iw)
	n := min(len(rows), maxRows)
	c.top = clampTop(c.top, cr, n, len(rows))

	out := c.popupLines(t, width)
	var content []string
	if v == "" && c.placeholder != "" {
		content = append(content, t.S.Placeholder.Render(textwidth.Truncate(c.placeholder, iw, "…")))
	} else {
		for _, r := range rows[c.top : c.top+n] {
			content = append(content, t.S.Base.Render(v[r.start:r.end]))
		}
	}
	style := t.S.Input
	if c.Focused() {
		style = t.S.InputFocused
	}
	out = append(out, Box(t, style, ui.Size{W: width, H: n + 2}, strings.Join(content, "\n")))
	return strings.Join(out, "\n")
}

// Height returns the rendered height: popup rows + drawn rows (1..maxRows) + border.
func (c *Composer) Height(width, maxRows int) int {
	if width <= 0 {
		return 0
	}
	rows, _, _ := layoutRows(c.Value(), len(c.beforeCursor()), max(1, width-2))
	return c.popupRows() + min(len(rows), max(1, maxRows)) + 2
}

// Cursor is relative to the Composer.View origin (nil while blurred).
func (c *Composer) Cursor() *tea.Cursor {
	if !c.Focused() {
		return nil
	}
	rows, cr, cx := layoutRows(c.Value(), len(c.beforeCursor()), c.lastW)
	n := min(len(rows), c.lastMax)
	top := clampTop(c.top, cr, n, len(rows))
	return tea.NewCursor(1+cx, c.popupRows()+1+cr-top)
}

// SetCompletion sets the @mention / :emoji / /command popup above the input (nil
// or empty closes it). The selection starts at the row whose Label begins with
// "› ", else the first row.
func (c *Composer) SetCompletion(items []Completion) {
	c.sel = 0
	if len(items) == 0 {
		c.items = nil
		return
	}
	c.items = slices.Clone(items)
	for i, it := range items {
		if strings.HasPrefix(it.Label, selectedPrefix) {
			c.sel = i
			break
		}
	}
}

func (c *Composer) popupRows() int {
	return min(len(c.items), maxCompletionRows)
}

// popupLines draws the visible completion rows (a window around the selection).
func (c *Composer) popupLines(t *theme.Theme, width int) []string {
	n := c.popupRows()
	if n == 0 {
		return nil
	}
	start := min(max(c.sel-n+1, 0), len(c.items)-n)
	out := make([]string, 0, n)
	for i := start; i < start+n; i++ {
		it := c.items[i]
		label, detail := textwidth.Sanitize(it.Label), textwidth.Sanitize(it.Detail)
		if i == c.sel {
			text := " " + label
			if detail != "" {
				text += "  " + detail
			}
			out = append(out, t.S.Selected.Render(textwidth.PadRight(text, width)))
			continue
		}
		line := t.S.Base.Render(" " + label)
		if detail != "" {
			line += t.S.Dim.Render("  " + detail)
		}
		out = append(out, fitLine(t, line, width))
	}
	return out
}

// accept replaces the word before the cursor with the selected Insert.
func (c *Composer) accept() {
	if c.sel < 0 || c.sel >= len(c.items) {
		return
	}
	ins := c.items[c.sel].Insert
	c.items, c.sel = nil, 0
	c.pending = ""
	before, after := c.split()
	ls := strings.LastIndexByte(before, '\n') + 1
	ts := ls + strings.LastIndexByte(before[ls:], ' ') + 1
	c.apply(before[:ts]+ins, after)
}

// insert types raw text at the cursor (keys and pastes).
func (c *Composer) insert(raw string) {
	if len(raw) > maxPasteBytes {
		raw = raw[:maxPasteBytes]
	}
	before, after := c.split()
	ps := lastGraphemeStart(before)
	base, prev := before[:ps], before[ps:]
	ctxRaw := prev + c.pending
	ins := textwidth.SanitizeInput(ctxRaw + raw)
	if raw != "" && ins == textwidth.SanitizeInput(ctxRaw) && len(c.pending)+len(raw) <= maxPendingBytes {
		c.pending += raw
	} else {
		c.pending = ""
	}
	if room := composerCharLimit - runes(base) - runes(after); runes(ins) > room {
		ins = truncRunes(ins, room)
		if runes(ins) < runes(prev) { // the grapheme merged with prev does not fit
			ins = prev
			c.pending = ""
		}
	}
	c.apply(base+ins, after)
}

// moveGrapheme moves the cursor one grapheme cluster (or newline) left/right.
func (c *Composer) moveGrapheme(dir int) {
	c.pending = ""
	before, after := c.split()
	if dir < 0 {
		if before == "" {
			return
		}
		ps := lastGraphemeStart(before)
		if ps == len(before) { // at a line start: step over the newline
			ps--
		}
		c.set(before[:ps], before[ps:]+after)
		return
	}
	if after == "" {
		return
	}
	n := nextGraphemeLen(after)
	c.set(before+after[:n], after[n:])
}

// deleteGrapheme deletes one grapheme cluster (or newline) before/after the cursor.
func (c *Composer) deleteGrapheme(dir int) {
	c.pending = ""
	before, after := c.split()
	if dir < 0 {
		if before == "" {
			return
		}
		ps := lastGraphemeStart(before)
		if ps == len(before) {
			ps--
		}
		c.apply(before[:ps], after)
		return
	}
	if after == "" {
		return
	}
	c.apply(before, after[nextGraphemeLen(after):])
}

// moveRow moves the cursor one drawn row up/down, keeping its column when possible.
func (c *Composer) moveRow(dir int) {
	c.pending = ""
	v := c.Value()
	rows, cr, cx := layoutRows(v, len(c.beforeCursor()), c.lastW)
	tr := cr + dir
	if tr < 0 || tr >= len(rows) {
		return
	}
	r := rows[tr]
	off, x := r.start, 0
	for off < r.end {
		n := nextGraphemeLen(v[off:r.end])
		gw := ansi.StringWidth(v[off : off+n])
		if x+gw > cx {
			break
		}
		x += gw
		off += n
	}
	c.set(v[:off], v[off:])
}

// beforeCursor is the value text before the cursor.
func (c *Composer) beforeCursor() string {
	b, _ := c.split()
	return b
}

// split cuts the value at the textarea cursor.
func (c *Composer) split() (before, after string) {
	v := c.ta.Value()
	lines := strings.Split(v, "\n")
	row := min(max(c.ta.Line(), 0), len(lines)-1)
	off := 0
	for _, l := range lines[:row] {
		off += len(l) + 1
	}
	line := []rune(lines[row])
	off += len(string(line[:min(max(c.ta.Column(), 0), len(line))]))
	return v[:off], v[off:]
}

// apply normalizes before+after as a whole (the cursor stays at the normalized
// length of before, snapped to a grapheme boundary), enforces the code point limit
// by trimming the end of before, and loads the result into the textarea.
func (c *Composer) apply(before, after string) {
	for range 4 {
		full := textwidth.SanitizeInput(before + after)
		nb := textwidth.SanitizeInput(before)
		cut := len(nb)
		if !strings.HasPrefix(full, nb) {
			cut = runeOffset(full, runes(nb))
		}
		cut = graphemeBoundary(full, cut)
		before, after = full[:cut], full[cut:]
		if runes(before)+runes(after) <= composerCharLimit {
			break
		}
		after = truncRunes(after, composerCharLimit)
		before = truncRunes(before, composerCharLimit-runes(after))
	}
	c.set(before, after)
}

// set loads before+after into the textarea with the cursor between them.
func (c *Composer) set(before, after string) {
	if b, a := c.split(); b == before && a == after {
		return
	}
	c.ta.Reset()
	c.ta.InsertString(after)
	c.ta.MoveToBegin()
	c.ta.InsertString(before)
}

// row is one drawn row: value[start:end].
type row struct{ start, end int }

// layoutRows soft-wraps v by grapheme clusters into rows of at most w cells (w <= 0
// = no wrap) and locates the cursor at byte offset cur: (row, x). A cursor at the end
// of a full row gets an extra empty row. There is always at least one row.
func layoutRows(v string, cur, w int) (rows []row, cr, cx int) {
	if w <= 0 {
		w = int(^uint(0) >> 1)
	}
	cr, cx = -1, 0
	lineStart := 0
	for {
		lineEnd := len(v)
		if i := strings.IndexByte(v[lineStart:], '\n'); i >= 0 {
			lineEnd = lineStart + i
		}
		rs, x := lineStart, 0
		for off := lineStart; off < lineEnd; {
			n := nextGraphemeLen(v[off:lineEnd])
			gw := ansi.StringWidth(v[off : off+n])
			if x+gw > w && x > 0 {
				rows = append(rows, row{rs, off})
				rs, x = off, 0
			}
			if cr < 0 && off == cur {
				cr, cx = len(rows), x
			}
			x += gw
			off += n
		}
		rows = append(rows, row{rs, lineEnd})
		if cr < 0 && cur >= lineStart && cur <= lineEnd {
			cr, cx = len(rows)-1, x
			if x >= w {
				rows = append(rows, row{lineEnd, lineEnd})
				cr, cx = len(rows)-1, 0
			}
		}
		if lineEnd == len(v) {
			break
		}
		lineStart = lineEnd + 1
	}
	if cr < 0 {
		cr, cx = len(rows)-1, 0
	}
	return rows, cr, cx
}

// clampTop keeps the cursor row cr inside the n visible rows of total.
func clampTop(top, cr, n, total int) int {
	if cr < top {
		top = cr
	}
	if cr >= top+n {
		top = cr - n + 1
	}
	return min(max(top, 0), max(total-n, 0))
}

// lastGraphemeStart is the byte offset of the last grapheme cluster of s's last line
// (len(s) when the line is empty).
func lastGraphemeStart(s string) int {
	off := strings.LastIndexByte(s, '\n') + 1
	last := len(s)
	for off < len(s) {
		last = off
		off += nextGraphemeLen(s[off:])
	}
	return last
}

// nextGraphemeLen is the byte length of s's first grapheme cluster (>= 1 for s != "").
func nextGraphemeLen(s string) int {
	g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
	if g == "" && s != "" {
		_, n := utf8.DecodeRuneInString(s)
		return n
	}
	return len(g)
}

// graphemeBoundary moves byte offset cut forward to the end of the grapheme cluster
// containing it.
func graphemeBoundary(s string, cut int) int {
	off := strings.LastIndexByte(s[:cut], '\n') + 1
	for off < cut {
		off += nextGraphemeLen(s[off:])
	}
	return min(off, len(s))
}

// truncRunes keeps whole grapheme clusters of s while the code point count fits n.
func truncRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if runes(s) <= n {
		return s
	}
	off, count := 0, 0
	for off < len(s) {
		g := nextGraphemeLen(s[off:])
		gr := utf8.RuneCountInString(s[off : off+g])
		if count+gr > n {
			break
		}
		count += gr
		off += g
	}
	return s[:off]
}

// runeOffset is the byte offset of the n-th code point of s (clamped).
func runeOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}

func runes(s string) int { return utf8.RuneCountInString(s) }
