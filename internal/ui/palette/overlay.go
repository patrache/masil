package palette

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// The palette overlay (CA 5.6, plan 03 Task 29): first row "› " + a textinput (real
// cursor, ctrl+k removed from DeleteAfterCursor, SanitizeInput), second row a rule,
// then the ranked items under 이동/명령/설정 headers. ↑/↓ move the selection, enter
// opens it (a route: close + Navigate; a command: ui.RunCommand(id, "") through the
// app's dispatch, which closes the palette first). esc and palette.open are left to
// the app (close). People search: 2+ runes => ui.After(300ms) debounce, latest
// generation only, ui.Do "core.palette.people:<q>".

// Overlay geometry caps.
const (
	maxW = 72
	maxH = 20
)

// peopleDebounce is the people-search debounce (CA 5.3.2 timers).
const peopleDebounce = 300 * time.Millisecond

// peopleLimit is the SearchUsers page size.
const peopleLimit = 8

// peopleTagPrefix is the ui.Do tag prefix of the people search.
const peopleTagPrefix = "core.palette.people:"

// promptText is drawn before the query.
const promptText = "› "

// Texts.
const (
	paletteTitle    = "명령 팔레트"
	placeholderText = "채널·사람·명령 찾기"
	noResultsText   = "찾는 항목이 없어요"
	emptyHintText   = "검색어를 입력해 이동하거나 명령을 실행하세요"
)

// peopleSearchMsg is the debounced people-search trigger of generation gen.
type peopleSearchMsg struct {
	gen int
	q   string
}

// row is one drawn list row: a group header or an item (index into o.items).
type row struct {
	header string
	item   int
}

// overlay is the palette ui.Overlay.
type overlay struct {
	input textinput.Model
	query string // last query the items were computed for

	items []Item // display order (grouped)
	rows  []row
	sel   int // index into items
	top   int // first visible row

	gen     int               // people-search generation
	people  []api.UserSummary // last people-search results
	peopleQ string            // the query they were found for

	size ui.Size
}

var (
	_ ui.Overlay      = (*overlay)(nil)
	_ ui.KeyHandler   = (*overlay)(nil)
	_ ui.MouseHandler = (*overlay)(nil)
	_ ui.Cursorer     = (*overlay)(nil)
	_ ui.Titled       = (*overlay)(nil)
	_ ui.Helper       = (*overlay)(nil)
)

// Open builds the palette overlay (ctrl+k) with initial as the starting query.
func Open(ctx *ui.Context, initial string) ui.Overlay {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	km.DeleteAfterCursor = withoutKeys(km.DeleteAfterCursor, "ctrl+k")
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	o := &overlay{input: ti}
	o.input.SetValue(oneLine(initial))
	o.input.CursorEnd()
	o.query = "\x00" // force the first refresh
	return o
}

// withoutKeys returns b without the keys in drop.
func withoutKeys(b key.Binding, drop ...string) key.Binding {
	keys := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(drop, k) })
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(b.Help().Key, b.Help().Desc))
}

// oneLine normalizes input text (textwidth.SanitizeInput, CA 8.4) to one line.
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

func (o *overlay) Init(ctx *ui.Context) tea.Cmd {
	cmd := o.input.Focus()
	return tea.Batch(cmd, o.changed(ctx))
}

func (o *overlay) Resize(_ *ui.Context, size ui.Size) {
	o.size = size
	o.scroll()
}

func (o *overlay) Modal() bool { return true }

func (o *overlay) Title(*ui.Context) (string, ui.Badge) { return paletteTitle, ui.Badge{} }

func (o *overlay) Help(*ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "선택")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "열기")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
	}
}

// Bounds: W = min(72, body.W-4), H = min(20, body.H-2), centered horizontally, a
// quarter of the spare height above.
func (o *overlay) Bounds(body layout.Rect) layout.Rect {
	w, h := max(0, min(maxW, body.W-4)), max(0, min(maxH, body.H-2))
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/4), W: w, H: h}
}

func (o *overlay) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		o.edit(tea.PasteMsg{Content: oneLine(m.Content)})
		return o.changed(ctx)
	case peopleSearchMsg:
		if m.gen != o.gen || m.q != strings.TrimSpace(o.input.Value()) {
			return nil
		}
		q := m.q
		return ui.Do(ctx, peopleTagPrefix+q, func(c context.Context, a *api.Client) ([]api.UserSummary, error) {
			return a.SearchUsers(c, q, peopleLimit)
		})
	case ui.ResultMsg[[]api.UserSummary]:
		q, ok := strings.CutPrefix(m.Tag, peopleTagPrefix)
		if !ok || m.Err != nil || q != strings.TrimSpace(o.input.Value()) {
			return nil
		}
		o.people, o.peopleQ = m.Value, q
		o.refresh(ctx)
	case ui.EventMsg, ui.ResyncDoneMsg:
		o.refresh(ctx)
	}
	return nil
}

// edit feeds an input msg to the textinput and re-normalizes the value.
func (o *overlay) edit(msg tea.Msg) {
	o.input, _ = o.input.Update(msg)
	v := o.input.Value()
	if n := oneLine(v); n != v {
		rs := []rune(v)
		before := oneLine(string(rs[:min(o.input.Position(), len(rs))]))
		o.input.SetValue(n)
		o.input.SetCursor(len([]rune(before)))
	}
}

// changed recomputes the items when the query changed and (re)arms the people search.
func (o *overlay) changed(ctx *ui.Context) tea.Cmd {
	q := strings.TrimSpace(o.input.Value())
	if q == o.query {
		return nil
	}
	o.query = q
	o.sel, o.top = 0, 0
	o.refresh(ctx)
	o.gen++
	if len([]rune(q)) < 2 {
		return nil
	}
	return ui.After(ctx, peopleDebounce, peopleSearchMsg{gen: o.gen, q: q})
}

// refresh recomputes the grouped items, keeping the selected item when it remains.
func (o *overlay) refresh(ctx *ui.Context) {
	var keep string
	if o.sel >= 0 && o.sel < len(o.items) {
		keep = itemKey(o.items[o.sel])
	}
	ranked := collect(ctx, o.input.Value(), o.people, o.peopleQ)
	var order []string
	groups := map[string][]Item{}
	for _, it := range ranked {
		g := groupOf(it)
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], it)
	}
	o.items, o.rows = nil, nil
	for _, g := range order {
		o.rows = append(o.rows, row{header: g, item: -1})
		for _, it := range groups[g] {
			o.rows = append(o.rows, row{item: len(o.items)})
			o.items = append(o.items, it)
		}
	}
	o.sel = 0
	for i, it := range o.items {
		if keep != "" && itemKey(it) == keep {
			o.sel = i
		}
	}
	o.scroll()
}

// listH is the number of item rows drawn (below the input and the rule).
func (o *overlay) listH() int {
	return max(0, o.size.H-2)
}

// selRow is the row index of the selected item (-1 = none).
func (o *overlay) selRow() int {
	for i, r := range o.rows {
		if r.header == "" && r.item == o.sel {
			return i
		}
	}
	return -1
}

// scroll keeps the selected row (and its header right above) visible.
func (o *overlay) scroll() {
	h := o.listH()
	sr := o.selRow()
	if h <= 0 || sr < 0 {
		o.top = 0
		return
	}
	want := sr
	if sr > 0 && o.rows[sr-1].header != "" {
		want = sr - 1 // show the group header too
	}
	if want < o.top {
		o.top = want
	}
	if sr >= o.top+h {
		o.top = sr - h + 1
	}
	o.top = max(0, min(o.top, max(0, len(o.rows)-h)))
}

func (o *overlay) move(d int) {
	if len(o.items) == 0 {
		return
	}
	o.sel = min(max(o.sel+d, 0), len(o.items)-1)
	o.scroll()
}

func (o *overlay) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if ctx.Keys.Matches(msg, keymap.PaletteOpen) || ctx.Keys.Matches(msg, keymap.NavBack) {
		return false, nil // the app closes the palette
	}
	switch msg.String() {
	case "up":
		o.move(-1)
		return true, nil
	case "down":
		o.move(+1)
		return true, nil
	case "pgup":
		o.move(-max(1, o.listH()-1))
		return true, nil
	case "pgdown":
		o.move(max(1, o.listH()-1))
		return true, nil
	case "enter":
		return true, o.choose()
	case "tab", "shift+tab", "ctrl+p", "ctrl+n", "ctrl+c":
		return true, nil
	}
	if isTextKey(msg) {
		o.edit(tea.PasteMsg{Content: oneLine(msg.Text)})
	} else {
		o.edit(msg)
	}
	return true, o.changed(ctx)
}

// choose opens the selected item.
func (o *overlay) choose() tea.Cmd {
	if o.sel < 0 || o.sel >= len(o.items) {
		return nil
	}
	it := o.items[o.sel]
	switch {
	case it.Command != nil:
		return ui.RunCommand(it.Command.ID, "")
	case it.Route != nil:
		remember(it)
		r := *it.Route
		return tea.Batch(
			func() tea.Msg { return ui.CloseOverlayMsg{} },
			ui.Navigate(r),
		)
	}
	return nil
}

// HandleMouse: a click selects an item row (a click on the selected one opens it);
// the wheel moves the selection.
func (o *overlay) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		if m.Button == tea.MouseWheelUp {
			o.move(-1)
		} else if m.Button == tea.MouseWheelDown {
			o.move(+1)
		}
	case tea.MouseClickMsg:
		ri := o.top + local.Y - 2
		if local.Y < 2 || ri < 0 || ri >= len(o.rows) || o.rows[ri].header != "" {
			return nil
		}
		if o.rows[ri].item == o.sel {
			return o.choose()
		}
		o.sel = o.rows[ri].item
		o.scroll()
	}
	return nil
}

// inputLine draws the query in w cells (placeholder when empty) and returns the
// cursor column. Long queries scroll so the cursor stays visible.
func (o *overlay) inputLine(t *theme.Theme, w int) (string, int) {
	if w <= 0 {
		return "", 0
	}
	rs := []rune(o.input.Value())
	if len(rs) == 0 {
		ph := textwidth.Truncate(placeholderText, w, "…")
		return t.S.Placeholder.Render(ph) + pad(t, w-textwidth.Width(ph)), 0
	}
	pos := min(o.input.Position(), len(rs))
	before, after := string(rs[:pos]), string(rs[pos:])
	if textwidth.Width(before) >= w {
		before = textwidth.TruncateLeft(before, w-1, "")
		after = ""
	}
	col := textwidth.Width(before)
	text := before + textwidth.Truncate(after, w-col, "")
	return t.S.Base.Render(text) + pad(t, w-textwidth.Width(text)), col
}

// Cursor is the real cursor in the query (relative to the inner rect).
func (o *overlay) Cursor() *tea.Cursor {
	pw := textwidth.Width(promptText)
	if o.size.W <= pw || o.size.H <= 0 {
		return nil
	}
	_, col := o.inputLine(theme.Get(theme.Default), o.size.W-pw)
	return tea.NewCursor(min(pw+col, o.size.W-1), 0)
}

func pad(t *theme.Theme, n int) string {
	if n <= 0 {
		return ""
	}
	return t.S.Base.Render(strings.Repeat(" ", n))
}

// fit truncates a styled line to w cells and pads it with Base blanks.
func fit(t *theme.Theme, l string, w int) string {
	l = textwidth.Truncate(l, w, "")
	return l + pad(t, w-textwidth.Width(l))
}

// icon is the kind glyph drawn before titles that do not carry their own.
func icon(it Item) string {
	switch it.Kind {
	case ItemChannel:
		if strings.HasPrefix(it.Title, "#") {
			return ""
		}
		return "# "
	case ItemSpace:
		if strings.HasPrefix(it.Title, "◆") {
			return ""
		}
		return "◆ "
	case ItemDM, ItemPerson:
		return "@ "
	}
	return "› "
}

// presence is the DM partner's presence dot ("" for group DMs / unknown).
func presence(ctx *ui.Context, it Item) string {
	if it.Kind != ItemDM || it.Route == nil || ctx.Store == nil {
		return ""
	}
	ch := ctx.Store.Channels[it.Route.ID]
	if ch == nil || ch.Kind != api.ChannelDM {
		return ""
	}
	for _, m := range ch.Members {
		if !ctx.Store.IsMe(m.ID) {
			return widgets.PresenceDot(ctx.Theme, ctx.Store.Presence[m.ID].Status)
		}
	}
	return ""
}

// itemLine draws one item row in exactly w cells.
func (o *overlay) itemLine(ctx *ui.Context, it Item, selected bool, w int) string {
	t := ctx.Theme
	base, dim := t.S.Base, t.S.Dim
	if selected {
		base = t.S.Selected
		dim = t.S.Selected.Foreground(t.P.Dim)
	}
	var right []string
	if it.Command != nil && it.Command.KeyHint != "" {
		right = append(right, dim.Render(textwidth.Sanitize(it.Command.KeyHint)))
	}
	if p := presence(ctx, it); p != "" {
		right = append(right, p)
	}
	if b := widgets.Badge(t, it.Badge); b != "" {
		right = append(right, b)
	}
	r := strings.Join(right, base.Render(" "))
	rw := textwidth.Width(r)
	if rw > 0 {
		r += base.Render(" ")
		rw++
	}
	lw := w - 1 - rw // one leading blank
	if lw < 4 {
		r, rw, lw = "", 0, w-1
	}
	title := textwidth.Truncate(icon(it)+textwidth.Sanitize(it.Title), max(0, lw), "…")
	left := base.Render(title)
	used := textwidth.Width(title)
	if sub := textwidth.Sanitize(it.Subtitle); sub != "" && lw-used > 3 {
		s := textwidth.Truncate("  "+sub, lw-used, "…")
		left += dim.Render(s)
		used += textwidth.Width(s)
	}
	gap := max(0, w-1-used-rw)
	return fit(t, base.Render(" ")+left+base.Render(strings.Repeat(" ", gap))+r, w)
}

func (o *overlay) View(ctx *ui.Context, _ bool) string {
	w, h := o.size.W, o.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t := ctx.Theme
	pw := textwidth.Width(promptText)
	lines := make([]string, 0, h)
	if w > pw {
		in, _ := o.inputLine(t, w-pw)
		lines = append(lines, t.S.Title.Render(promptText)+in)
	} else {
		lines = append(lines, pad(t, w))
	}
	if h > 1 {
		rule := lipgloss.NewStyle().Inherit(t.S.Base).Foreground(t.P.Line)
		lines = append(lines, rule.Render(strings.Repeat("─", w)))
	}
	lh := o.listH()
	switch {
	case lh <= 0:
	case len(o.rows) == 0:
		msg := noResultsText
		if strings.TrimSpace(o.input.Value()) == "" {
			msg = emptyHintText
		}
		lines = append(lines, fit(t, t.S.Dim.Render(" "+textwidth.Truncate(msg, max(0, w-1), "…")), w))
	default:
		for i := o.top; i < len(o.rows) && i < o.top+lh; i++ {
			r := o.rows[i]
			if r.header != "" {
				lines = append(lines, fit(t, t.S.Subtitle.Render(" "+r.header), w))
				continue
			}
			lines = append(lines, o.itemLine(ctx, o.items[r.item], r.item == o.sel, w))
		}
	}
	for len(lines) < h {
		lines = append(lines, pad(t, w))
	}
	return strings.Join(textwidth.Fit(lines[:h], w, h), "\n")
}
