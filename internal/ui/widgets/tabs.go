package widgets

import (
	"strings"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Tabs is a horizontal tab bar with badges and overflow arrows.
//
// Each tab is " label " (TabActive for the active one, else Tab) followed by its Badge,
// tabs are separated by one Base blank. When they do not fit, a window around the
// active tab is shown with "‹ " / " ›" marking hidden tabs. View records the drawn
// spans so Hit maps a column back to a tab (an arrow maps to the nearest hidden tab).
type Tabs struct {
	items  []Tab
	active int
	spans  []tabSpan // from the last View
}

// tabSpan is the column range [x0, x1) that selects index.
type tabSpan struct{ x0, x1, index int }

// Tab is one tab.
type Tab struct {
	Label string
	Badge ui.Badge
	// Short is the narrowest label WalkBar falls back to (an icon, an
	// initial); "" means the label's first two cells. Tabs ignores it.
	Short string
}

const (
	arrowLeft  = "‹ "
	arrowRight = " ›"
	arrowW     = 2
)

// NewTabs creates an empty tab bar.
func NewTabs() *Tabs {
	return &Tabs{}
}

// Set replaces the tabs (active is clamped into range).
func (t *Tabs) Set(items []Tab, active int) {
	t.items = append([]Tab(nil), items...)
	t.active = min(max(0, active), max(0, len(t.items)-1))
	t.spans = nil
}

// View renders the bar at exactly width cells.
func (t *Tabs) View(th *theme.Theme, width int) string {
	t.spans = nil
	if width <= 0 {
		return ""
	}
	n := len(t.items)
	if n == 0 {
		return pad(th, width)
	}
	labels := make([]string, n)
	ws := make([]int, n)
	for i, it := range t.items {
		labels[i] = textwidth.Sanitize(it.Label)
		ws[i] = textwidth.Width(labels[i]) + 2 + textwidth.Width(Badge(th, it.Badge))
	}
	span := func(s, e int) int { // cells of tabs s..e plus needed arrows
		w := e - s
		for i := s; i <= e; i++ {
			w += ws[i]
		}
		if s > 0 {
			w += arrowW
		}
		if e < n-1 {
			w += arrowW
		}
		return w
	}

	s, e := 0, 0
	for e+1 < n && span(s, e+1) <= width {
		e++
	}
	if e < t.active || span(s, e) > width {
		s, e = t.active, t.active
		for s > 0 && span(s-1, e) <= width {
			s--
		}
		for e+1 < n && span(s, e+1) <= width {
			e++
		}
	}

	var b strings.Builder
	x := 0
	if s > 0 {
		b.WriteString(th.S.Dim.Render(arrowLeft))
		t.spans = append(t.spans, tabSpan{0, arrowW, s - 1})
		x = arrowW
	}
	room := width - x
	if e < n-1 {
		room -= arrowW
	}
	for i := s; i <= e; i++ {
		if i > s {
			if room < 1 {
				break
			}
			b.WriteString(pad(th, 1))
			x++
			room--
		}
		seg, sw := t.tabSegment(th, i, labels[i], room)
		if sw == 0 {
			break
		}
		b.WriteString(seg)
		t.spans = append(t.spans, tabSpan{x, x + sw, i})
		x += sw
		room -= sw
	}
	if e < n-1 && x+arrowW <= width {
		b.WriteString(pad(th, room))
		x += room
		b.WriteString(th.S.Dim.Render(arrowRight))
		t.spans = append(t.spans, tabSpan{x, x + arrowW, e + 1})
		x += arrowW
	}
	out := textwidth.Truncate(b.String(), width, "")
	t.clipSpans(width)
	return out + pad(th, width-textwidth.Width(out))
}

// clipSpans drops/clips recorded spans that fall outside the drawn width.
func (t *Tabs) clipSpans(width int) {
	kept := t.spans[:0]
	for _, sp := range t.spans {
		if sp.x0 >= width {
			continue
		}
		sp.x1 = min(sp.x1, width)
		kept = append(kept, sp)
	}
	t.spans = kept
}

// tabSegment renders tab i within room cells; the label is truncated (badge dropped)
// when the whole tab does not fit. Width 0 means nothing fits.
func (t *Tabs) tabSegment(th *theme.Theme, i int, label string, room int) (string, int) {
	st := th.S.Tab
	if i == t.active {
		st = th.S.TabActive
	}
	badge := Badge(th, t.items[i].Badge)
	full := textwidth.Width(label) + 2 + textwidth.Width(badge)
	if full <= room {
		return st.Render(" "+label+" ") + badge, full
	}
	if room < 3 {
		return "", 0
	}
	l := textwidth.Truncate(label, room-2, "…")
	w := textwidth.Width(l) + 2
	return st.Render(" "+l+" ") + pad(th, room-w), room
}

// Hit maps a column to a tab index (from the last View).
func (t *Tabs) Hit(x int) (index int, ok bool) {
	for _, sp := range t.spans {
		if x >= sp.x0 && x < sp.x1 {
			return sp.index, true
		}
	}
	return 0, false
}
