// Package apitool is the screen of the API test tool (v0.1.3 C3): the tool
// surface, its panes and widgets over the engine internal/apitool.
package apitool

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Table layout (C3.16 [builder's call], decided here).
const (
	kvNarrow    = 60              // below this width the table is a "key = value" list
	kvRevealFor = 5 * time.Second // V shows a secret value this long
	kvMinKey    = 8
)

// KVCols are the column titles of a table; Secretable tables mask the
// values of rows whose key SetSecretNames marks secret.
type KVCols struct {
	Key, Value string
	Secretable bool
}

type kvDeleted struct {
	at int
	kv apitool.KV
}

// kvRevealEnd hides a revealed secret (gen = the reveal it belongs to).
type kvRevealEnd struct {
	t   *KVTable
	gen int
}

// KVTable edits key/value rows (headers, query, form fields, checks,
// extracts, environments). Keys: j/k ↑↓ row, tab/shift+tab column, enter
// edit/confirm, esc cancel, space on/off, a add below, d delete, u undo the
// last delete, V show a secret value for 5 seconds.
type KVTable struct {
	cols    KVCols
	rows    []apitool.KV
	cur     int
	col     int // 0 key, 1 value
	top     int
	undo    []kvDeleted
	secret  func(string) bool
	lastW   int
	lastH   int
	focused bool

	editing bool
	added   bool // the edited row was just added by a
	buf     []rune
	pos     int

	revealed  int // row index shown in clear, -1 none
	revealGen int
}

// NewKVTable is an empty table with column titles c.
func NewKVTable(c KVCols) *KVTable {
	return &KVTable{cols: c, revealed: -1, lastW: 80, lastH: 10}
}

// SetRows replaces the rows (ids kept as given).
func (t *KVTable) SetRows(rows []apitool.KV) {
	t.rows = slices.Clone(rows)
	t.editing = false
	t.revealed = -1
	t.undo = nil
	t.cur = min(t.cur, max(len(t.rows)-1, 0))
}

// Rows is a copy of the rows; a cell being edited is not included until
// confirmed.
func (t *KVTable) Rows() []apitool.KV {
	if len(t.rows) == 0 {
		return nil
	}
	return slices.Clone(t.rows)
}

// SetSecretNames marks the rows whose key is secret (Secretable tables).
func (t *KVTable) SetSecretNames(f func(name string) bool) { t.secret = f }

// CapturesInput reports whether a cell is being edited (every key is text).
func (t *KVTable) CapturesInput() bool { return t.editing }

func (t *KVTable) isSecret(i int) bool {
	return t.cols.Secretable && t.secret != nil && i >= 0 && i < len(t.rows) && t.secret(t.rows[i].Key)
}

func (t *KVTable) hide() { t.revealed = -1 }

// HandleKey handles one key press: handled = the table used it, changed =
// the rows changed; cmd is the reveal timer.
func (t *KVTable) HandleKey(ctx *ui.Context, m tea.KeyPressMsg) (handled, changed bool, cmd tea.Cmd) {
	k := m.String()
	if t.revealed >= 0 && k != "V" {
		t.hide()
	}
	if t.editing {
		return true, t.editKey(m, k), nil
	}
	switch k {
	case "j", "down":
		t.cur = min(t.cur+1, max(len(t.rows)-1, 0))
	case "k", "up":
		t.cur = max(t.cur-1, 0)
	case "tab", "right":
		t.col = 1
	case "shift+tab", "left":
		t.col = 0
	case "enter":
		if len(t.rows) == 0 {
			t.add()
			return true, true, nil
		}
		t.startEdit(false)
	case "space":
		if len(t.rows) == 0 {
			return true, false, nil
		}
		t.rows[t.cur].Off = !t.rows[t.cur].Off
		return true, true, nil
	case "a":
		t.add()
		return true, true, nil
	case "d":
		if len(t.rows) == 0 {
			return true, false, nil
		}
		t.undo = append(t.undo, kvDeleted{t.cur, t.rows[t.cur]})
		t.rows = slices.Delete(t.rows, t.cur, t.cur+1)
		t.cur = min(t.cur, max(len(t.rows)-1, 0))
		return true, true, nil
	case "u":
		if len(t.undo) == 0 {
			return true, false, nil
		}
		d := t.undo[len(t.undo)-1]
		t.undo = t.undo[:len(t.undo)-1]
		at := min(d.at, len(t.rows))
		t.rows = slices.Insert(t.rows, at, d.kv)
		t.cur = at
		return true, true, nil
	case "V":
		if !t.isSecret(t.cur) {
			return false, false, nil
		}
		t.revealed = t.cur
		t.revealGen++
		return true, false, ui.After(ctx, kvRevealFor, kvRevealEnd{t: t, gen: t.revealGen})
	default:
		return false, false, nil
	}
	return true, false, nil
}

// HandleMsg takes the table's own messages (reveal timer, paste while
// editing); it reports whether msg was one.
func (t *KVTable) HandleMsg(msg tea.Msg) bool {
	switch m := msg.(type) {
	case kvRevealEnd:
		if m.t != t || m.gen != t.revealGen || t.revealed < 0 {
			return false
		}
		t.hide()
		return true
	case tea.PasteMsg:
		if !t.editing {
			return false
		}
		s := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(m.Content)
		t.insert(textwidth.SanitizeInput(s))
		return true
	}
	return false
}

func (t *KVTable) add() {
	at := 0
	if len(t.rows) > 0 {
		at = t.cur + 1
	}
	t.rows = slices.Insert(t.rows, at, apitool.KV{})
	t.cur, t.col = at, 0
	t.startEdit(true)
}

func (t *KVTable) cell() *string {
	if t.col == 0 {
		return &t.rows[t.cur].Key
	}
	return &t.rows[t.cur].Value
}

func (t *KVTable) startEdit(added bool) {
	t.editing, t.added = true, added
	t.buf = []rune(*t.cell())
	t.pos = len(t.buf)
}

// commit writes the buffer into the cell; reports a change.
func (t *KVTable) commit() bool {
	c := t.cell()
	v := string(t.buf)
	t.editing = false
	if *c == v {
		return false
	}
	*c = v
	return true
}

func (t *KVTable) editKey(m tea.KeyPressMsg, k string) (changed bool) {
	switch k {
	case "enter":
		return t.commit()
	case "esc":
		t.editing = false
		if t.added && t.rows[t.cur] == (apitool.KV{}) {
			t.rows = slices.Delete(t.rows, t.cur, t.cur+1)
			t.cur = min(t.cur, max(len(t.rows)-1, 0))
			return true
		}
	case "tab", "shift+tab":
		changed = t.commit()
		t.col = 1
		if k == "shift+tab" {
			t.col = 0
		}
		t.startEdit(t.added)
	case "backspace":
		if t.pos > 0 {
			t.buf = slices.Delete(t.buf, t.pos-1, t.pos)
			t.pos--
		}
	case "delete":
		if t.pos < len(t.buf) {
			t.buf = slices.Delete(t.buf, t.pos, t.pos+1)
		}
	case "left":
		t.pos = max(t.pos-1, 0)
	case "right":
		t.pos = min(t.pos+1, len(t.buf))
	case "home", "ctrl+a":
		t.pos = 0
	case "end", "ctrl+e":
		t.pos = len(t.buf)
	case "ctrl+u":
		t.buf, t.pos = t.buf[t.pos:], 0
	default:
		if m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0 {
			t.insert(textwidth.SanitizeInput(m.Text))
		}
	}
	return changed
}

func (t *KVTable) insert(s string) {
	r := []rune(s)
	t.buf = slices.Insert(t.buf, t.pos, r...)
	t.pos += len(r)
}

// maskSecret shows only the last 4 characters of a long secret value.
func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if utf8.RuneCountInString(v) < 8 {
		return "••••"
	}
	r := []rune(v)
	return "••••" + string(r[len(r)-4:])
}

// layout is the geometry shared by View and Cursor.
type kvLayout struct {
	narrow    bool
	header    int // rows above the data rows
	keyW      int
	keyX      int // column of the key text
	valX      int // column of the value text (wide)
	visible   int
	w, h, top int
}

func (t *KVTable) layout(w, h int) kvLayout {
	l := kvLayout{w: w, h: h, narrow: w < kvNarrow, keyX: 3}
	if !l.narrow {
		l.header = 1
		kw := max(textwidth.Width(t.cols.Key), kvMinKey)
		for _, r := range t.rows {
			kw = max(kw, textwidth.Width(textwidth.Sanitize(r.Key)))
		}
		l.keyW = min(kw, max(w/3, kvMinKey))
		l.valX = l.keyX + l.keyW + 2
	}
	l.visible = max(h-l.header, 0)
	top := t.top
	if t.cur < top {
		top = t.cur
	}
	if l.visible > 0 && t.cur >= top+l.visible {
		top = t.cur - l.visible + 1
	}
	l.top = max(top, 0)
	return l
}

func (t *KVTable) display(i, col int) string {
	if t.editing && i == t.cur && col == t.col {
		return string(t.buf)
	}
	r := t.rows[i]
	if col == 0 {
		return textwidth.Sanitize(r.Key)
	}
	v := textwidth.Sanitize(r.Value)
	if t.isSecret(i) && t.revealed != i && !(t.editing && i == t.cur) {
		return maskSecret(v)
	}
	return v
}

// View draws exactly h lines of w cells.
func (t *KVTable) View(ctx *ui.Context, w, h int, focused bool) []string {
	t.lastW, t.lastH, t.focused = w, h, focused
	l := t.layout(w, h)
	t.top = l.top
	var st *styles
	if ctx != nil && ctx.Theme != nil {
		st = &styles{sel: ctx.Theme.S.Selected, dim: ctx.Theme.S.Dim}
	}
	var lines []string
	if l.header > 0 {
		hdr := "   " + textwidth.PadRight(textwidth.Truncate(t.cols.Key, l.keyW, "…"), l.keyW) + "  " + t.cols.Value
		lines = append(lines, st.dimmed(textwidth.Truncate(hdr, w, "…")))
	}
	if len(t.rows) == 0 {
		lines = append(lines, st.dimmed(textwidth.Truncate("   "+ctx.T.Apitest.KvEmpty(), w, "…")))
		return textwidth.Fit(lines, w, h)
	}
	for i := l.top; i < len(t.rows) && i < l.top+l.visible; i++ {
		lines = append(lines, t.rowLine(i, l, st))
	}
	return textwidth.Fit(lines, w, h)
}

type styles struct{ sel, dim lipgloss.Style }

func (s *styles) dimmed(x string) string {
	if s == nil || x == "" {
		return x
	}
	return s.dim.Render(x)
}

func (s *styles) selected(x string) string {
	if s == nil || x == "" {
		return x
	}
	return s.sel.Render(x)
}

func (t *KVTable) rowLine(i int, l kvLayout, st *styles) string {
	r := t.rows[i]
	cursor, mark := " ", "✓"
	if i == t.cur && t.focused {
		cursor = "›"
	}
	if r.Off {
		mark = "·"
	}
	active := func(col int, s string) string {
		if i == t.cur && t.focused && col == t.col && !t.editing {
			return st.selected(s)
		}
		if r.Off {
			return st.dimmed(s)
		}
		return s
	}
	prefix := cursor + mark + " "
	key, val := t.display(i, 0), t.display(i, 1)
	if l.narrow {
		kw := textwidth.Width(key)
		rest := max(l.w-3-kw-3, 0)
		return textwidth.Truncate(prefix+active(0, key)+" = "+active(1, textwidth.Truncate(val, rest, "…")), l.w, "…")
	}
	keyCell := textwidth.PadRight(textwidth.Truncate(key, l.keyW, "…"), l.keyW)
	valCell := textwidth.Truncate(val, max(l.w-l.valX, 0), "…")
	return prefix + active(0, keyCell) + "  " + active(1, valCell)
}

// Cursor is the text cursor while editing, relative to the View origin
// (nil when not editing or off screen).
func (t *KVTable) Cursor() *tea.Cursor {
	if !t.editing {
		return nil
	}
	l := t.layout(t.lastW, t.lastH)
	y := l.header + t.cur - l.top
	if y < l.header || y >= l.h {
		return nil
	}
	before := textwidth.Width(string(t.buf[:t.pos]))
	x := l.keyX + before
	switch {
	case l.narrow && t.col == 1:
		x = l.keyX + textwidth.Width(t.display(t.cur, 0)) + 3 + before
	case !l.narrow && t.col == 1:
		x = l.valX + before
	}
	if x >= l.w {
		x = l.w - 1
	}
	return tea.NewCursor(x, y)
}
