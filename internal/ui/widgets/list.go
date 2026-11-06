package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// wheelLines is how far one mouse wheel notch scrolls a List.
const wheelLines = 3

// List is a selectable, scrollable list: selection by item ID, anchored scroll.
//
// Items render to a variable number of lines (render returns them for a width).
// The scroll position lives only in a ScrollAnchor (top item ID + line offset into
// it, or pinned to the bottom), so it survives re-wrapping on width/mode changes.
// Rendered lines are cached per (item ID, width, selected) and dropped on the next
// SetItems, a theme change or a ctx.Mode change (so density changes re-render even at
// the same width). "g g" (top) is a two-key sequence tracked without any timer.
type List[T any] struct {
	idOf   func(T) string
	render func(ctx *ui.Context, item T, selected bool, width int) []string

	items []T
	index map[string]int
	sel   int // -1 = none

	anchor    ScrollAnchor
	anchorIdx int  // fallback when the anchor item disappears
	follow    bool // stick to the bottom on new items
	reveal    bool // bring the selection into view on the next layout
	pendingG  bool // first "g" of "g g" seen

	cache      map[listCacheKey][]string
	cacheTheme *theme.Theme
	cacheMode  layout.Mode

	size ui.Size // last View size
	rows []int   // last View: item index per visible row (-1 = blank)
}

type listCacheKey struct {
	id       string
	width    int
	selected bool
}

// ScrollAnchor makes scroll position width-independent: survives re-wrap on mode/width change.
type ScrollAnchor struct {
	ItemID     string
	LineOffset int
	AtBottom   bool
}

// NewList creates a list.
//
// render's output is cached by item ID (plus width and selection), and the cache is
// also dropped when ctx.Theme or ctx.Mode changes. Any other input render reads
// (Store state such as names or presence, ctx.Now for relative times, ...) is not
// tracked: when such inputs change, call SetItems again to re-render.
func NewList[T any](idOf func(T) string, render func(ctx *ui.Context, item T, selected bool, width int) []string) *List[T] {
	return &List[T]{idOf: idOf, render: render, sel: -1, index: map[string]int{}}
}

// SetItems replaces items, keeping selection & top anchor by ID.
//
// It always drops the render cache, so callers also use it to re-render after a
// non-item input of render changes (Store, time), even with the same items.
//
// A removed selected item hands the selection to the item now at the same index
// (or the last one); with no prior selection the first item is selected. A removed
// anchor item falls back to the item at the anchor's last index. Without Follow the
// view is never pinned to the bottom by new items.
func (l *List[T]) SetItems(items []T) {
	prevSelID := l.selectedID()
	l.items = items
	l.index = make(map[string]int, len(items))
	for i, it := range items {
		if _, dup := l.index[l.idOf(it)]; !dup {
			l.index[l.idOf(it)] = i
		}
	}
	l.cache = nil

	n := len(items)
	switch i, ok := l.index[prevSelID]; {
	case n == 0:
		l.sel = -1
	case prevSelID != "" && ok:
		l.sel = i
	case l.sel >= 0:
		l.sel = min(l.sel, n-1)
	default:
		l.sel = 0
	}

	if l.anchor.ItemID != "" {
		if _, ok := l.index[l.anchor.ItemID]; !ok {
			l.anchor.LineOffset = 0
			l.anchor.ItemID = ""
			if n > 0 {
				l.anchor.ItemID = l.idOf(items[min(max(l.anchorIdx, 0), n-1)])
			}
		}
	}
	if !l.follow {
		l.anchor.AtBottom = false
	}
}

// Selected returns the selected item.
func (l *List[T]) Selected() (T, bool) {
	if l.sel < 0 || l.sel >= len(l.items) {
		var zero T
		return zero, false
	}
	return l.items[l.sel], true
}

// At is the item drawn at row y of the last View (the item a click on that
// row selects), and false for a blank row or one outside the view.
func (l *List[T]) At(y int) (T, bool) {
	if y >= 0 && y < len(l.rows) {
		if i := l.rows[y]; i >= 0 && i < len(l.items) {
			return l.items[i], true
		}
	}
	var zero T
	return zero, false
}

// Select selects by ID (unknown IDs are ignored) and scrolls it into view.
func (l *List[T]) Select(id string) {
	if i, ok := l.index[id]; ok {
		l.sel = i
		l.reveal = true
	}
}

// HandleKey handles list navigation keys: list.up/down, list.top (and "g g"),
// list.bottom, list.pageup/pagedown (half the last view height). Any other key
// (enter included) is not handled.
func (l *List[T]) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	var km *keymap.Keymap
	if ctx != nil {
		km = ctx.Keys
	}
	if msg.String() == "g" {
		if l.pendingG {
			l.pendingG = false
			l.moveTo(0)
		} else {
			l.pendingG = true
		}
		return true, nil
	}
	l.pendingG = false
	switch {
	case km.Matches(msg, keymap.ListUp):
		l.moveTo(l.sel - 1)
	case km.Matches(msg, keymap.ListDown):
		l.moveTo(l.sel + 1)
	case km.Matches(msg, keymap.ListTop):
		l.moveTo(0)
	case km.Matches(msg, keymap.ListBottom):
		l.moveTo(len(l.items) - 1)
	case km.Matches(msg, keymap.ListPageUp):
		l.moveTo(l.pageFrom(ctx, -1))
	case km.Matches(msg, keymap.ListPageDown):
		l.moveTo(l.pageFrom(ctx, +1))
	default:
		return false, nil
	}
	return true, nil
}

// HandleMouse handles clicks and wheel: the wheel scrolls 3 lines (selection kept),
// a left click selects the item drawn on that row of the last View.
func (l *List[T]) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	m := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelDown:
			l.scrollBy(ctx, wheelLines)
		case tea.MouseWheelUp:
			l.scrollBy(ctx, -wheelLines)
		}
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft || local.Y < 0 || local.Y >= len(l.rows) {
			return nil
		}
		if i := l.rows[local.Y]; i >= 0 && i < len(l.items) {
			l.sel = i
		}
	}
	return nil
}

// View renders exactly size.H lines of exactly size.W cells.
func (l *List[T]) View(ctx *ui.Context, size ui.Size) string {
	l.size = size
	l.rows = l.rows[:0]
	if size.W <= 0 || size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	idx, off := l.place(ctx, size.W, size.H)
	out := make([]string, 0, size.H)
	for i := idx; i < len(l.items) && len(out) < size.H; i++ {
		lines := l.linesOf(ctx, i, size.W)
		for _, ln := range lines[off:] {
			if len(out) == size.H {
				break
			}
			out = append(out, fitLine(t, ln, size.W))
			l.rows = append(l.rows, i)
		}
		off = 0
	}
	for len(out) < size.H {
		out = append(out, pad(t, size.W))
		l.rows = append(l.rows, -1)
	}
	return strings.Join(out, "\n")
}

// Anchor returns the current scroll anchor.
func (l *List[T]) Anchor() ScrollAnchor {
	return l.anchor
}

// Restore applies a scroll anchor. An anchor pinned to the bottom always applies;
// otherwise an unknown item ID is ignored.
func (l *List[T]) Restore(a ScrollAnchor) {
	if a.AtBottom {
		l.anchor = ScrollAnchor{ItemID: a.ItemID, AtBottom: true}
		l.reveal = false
		return
	}
	if i, ok := l.index[a.ItemID]; ok {
		l.anchor = ScrollAnchor{ItemID: a.ItemID, LineOffset: max(0, a.LineOffset)}
		l.anchorIdx = i
		l.reveal = false
	}
}

// AtBottom reports whether the view is at the bottom (as of the last layout).
func (l *List[T]) AtBottom() bool {
	return l.anchor.AtBottom
}

// Follow sticks to bottom on new items (chat). A list that has not been scrolled
// yet starts at the bottom.
func (l *List[T]) Follow(bottom bool) {
	l.follow = bottom
	if bottom && l.anchor.ItemID == "" {
		l.anchor.AtBottom = true
	}
}

func (l *List[T]) selectedID() string {
	if l.sel < 0 || l.sel >= len(l.items) {
		return ""
	}
	return l.idOf(l.items[l.sel])
}

func (l *List[T]) moveTo(i int) {
	if len(l.items) == 0 {
		return
	}
	l.sel = min(max(i, 0), len(l.items)-1)
	l.reveal = true
}

// pageFrom is the item index half a view height away from the selection.
func (l *List[T]) pageFrom(ctx *ui.Context, dir int) int {
	half := max(1, l.size.H/2)
	i := max(l.sel, 0)
	for acc := 0; acc < half; {
		if dir > 0 {
			if i >= len(l.items)-1 {
				break
			}
			acc += l.height(ctx, i, l.size.W)
			i++
		} else {
			if i <= 0 {
				break
			}
			i--
			acc += l.height(ctx, i, l.size.W)
		}
	}
	return i
}

// scrollBy moves the view by delta lines using the last View size.
func (l *List[T]) scrollBy(ctx *ui.Context, delta int) {
	if len(l.items) == 0 || l.size.W <= 0 || l.size.H <= 0 {
		return
	}
	l.reveal = false
	idx, off := l.place(ctx, l.size.W, l.size.H)
	off += delta
	for off < 0 && idx > 0 {
		idx--
		off += l.height(ctx, idx, l.size.W)
	}
	for idx < len(l.items)-1 && off >= l.height(ctx, idx, l.size.W) {
		off -= l.height(ctx, idx, l.size.W)
		idx++
	}
	l.anchor = ScrollAnchor{ItemID: l.idOf(l.items[idx]), LineOffset: max(0, off)}
	l.place(ctx, l.size.W, l.size.H) // clamp to the bottom and refresh AtBottom
}

// place resolves the anchor into the top (item index, line offset) for a w x h
// view: bottom pinning, revealing the selection, never leaving blank rows below
// when there is content above. It stores the normalized anchor.
func (l *List[T]) place(ctx *ui.Context, w, h int) (idx, off int) {
	n := len(l.items)
	if n == 0 {
		l.reveal = false
		return 0, 0
	}
	if l.anchor.AtBottom {
		idx, off = l.bottomTop(ctx, w, h)
	} else if i, ok := l.index[l.anchor.ItemID]; ok {
		idx = i
		off = min(max(l.anchor.LineOffset, 0), l.height(ctx, i, w)-1)
	}
	if l.reveal && l.sel >= 0 {
		idx, off = l.revealSel(ctx, w, h, idx, off)
	}
	l.reveal = false
	if l.remaining(ctx, w, idx, off, h) < h {
		idx, off = l.bottomTop(ctx, w, h)
	}
	l.anchor = ScrollAnchor{ItemID: l.idOf(l.items[idx]), LineOffset: off, AtBottom: l.remaining(ctx, w, idx, off, h) <= h}
	l.anchorIdx = idx
	return idx, off
}

// bottomTop is the top position that shows the last h lines.
func (l *List[T]) bottomTop(ctx *ui.Context, w, h int) (int, int) {
	acc := 0
	for i := len(l.items) - 1; i >= 0; i-- {
		hg := l.height(ctx, i, w)
		if acc+hg >= h {
			return i, acc + hg - h
		}
		acc += hg
	}
	return 0, 0
}

// remaining counts lines from (idx, off) to the end, stopping once past limit.
func (l *List[T]) remaining(ctx *ui.Context, w, idx, off, limit int) int {
	total := -off
	for i := idx; i < len(l.items) && total <= limit; i++ {
		total += l.height(ctx, i, w)
	}
	return total
}

// revealSel scrolls minimally so the selected item is fully visible (its top when
// it is taller than the view).
func (l *List[T]) revealSel(ctx *ui.Context, w, h, idx, off int) (int, int) {
	s := l.sel
	if s < idx || (s == idx && off > 0) {
		return s, 0
	}
	dist := -off
	for i := idx; i <= s; i++ {
		dist += l.height(ctx, i, w)
	}
	if dist <= h {
		return idx, off
	}
	acc := l.height(ctx, s, w)
	if acc >= h {
		return s, 0
	}
	i := s
	for acc < h && i > 0 {
		i--
		acc += l.height(ctx, i, w)
	}
	return i, max(0, acc-h)
}

func (l *List[T]) height(ctx *ui.Context, i, w int) int {
	if w <= 0 {
		return 1
	}
	return len(l.linesOf(ctx, i, w))
}

// linesOf renders item i at width w (at least one line), through the cache.
func (l *List[T]) linesOf(ctx *ui.Context, i, w int) []string {
	t := themeOf(ctx)
	var mode layout.Mode
	if ctx != nil {
		mode = ctx.Mode
	}
	if l.cache == nil || l.cacheTheme != t || l.cacheMode != mode {
		l.cache = map[listCacheKey][]string{}
		l.cacheTheme = t
		l.cacheMode = mode
	}
	it := l.items[i]
	k := listCacheKey{id: l.idOf(it), width: w, selected: i == l.sel}
	if lines, ok := l.cache[k]; ok {
		return lines
	}
	lines := l.render(ctx, it, k.selected, w)
	if len(lines) == 0 {
		lines = []string{""}
	}
	l.cache[k] = lines
	return lines
}
