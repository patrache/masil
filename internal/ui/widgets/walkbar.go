package widgets

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
)

// WalkBar is the shell's top tab bar: the places the tab keys ([ / ]) walk
// through, in their order, the current one emphasised (TabActive), "[" before
// the entry [ goes to and "]" after the one ] goes to (wrapping; with two
// entries both marks are on the other one, with one there are none).
//
// Unlike Tabs it never scrolls entries out of sight: when the bar is too wide
// the labels are shortened step by step — every label cut to a common width
// with "…", then the Short labels (icons, initials), then without the padding
// blanks, then without badges — so every entry keeps a visible, clickable cell
// on one line. Hit maps a column back to an entry.
type WalkBar struct {
	items  []Tab
	active int
	spans  []tabSpan // from the last View
}

// NewWalkBar creates an empty walk bar.
func NewWalkBar() *WalkBar { return &WalkBar{} }

// Set replaces the entries (active is clamped into range).
func (b *WalkBar) Set(items []Tab, active int) {
	b.items = append([]Tab(nil), items...)
	b.active = min(max(0, active), max(0, len(b.items)-1))
	b.spans = nil
}

// walkFit is one shortening step: labels cut to capW (0 = full), Short
// labels, padding blanks, badges.
type walkFit struct {
	capW   int
	short  bool
	padded bool
	badges bool
}

// marks returns the entries [ and ] go to (-1 when there is none).
func (b *WalkBar) marks() (prev, next int) {
	n := len(b.items)
	if n < 2 {
		return -1, -1
	}
	return (b.active - 1 + n) % n, (b.active + 1) % n
}

// label is entry i's label under f.
func (b *WalkBar) label(i int, f walkFit) string {
	l := textwidth.Sanitize(b.items[i].Label)
	if f.short {
		if s := textwidth.Sanitize(b.items[i].Short); s != "" {
			return s
		}
		return shortLabel(l)
	}
	if f.capW > 0 {
		return textwidth.Truncate(l, f.capW, "…")
	}
	return l
}

// shortLabel is the Short step's fallback for an entry without a Short: its
// leading graphemes up to 2 cells, plus one more grapheme when that leaves only
// an ASCII sigil ("#개발" -> "#개", "@민수" -> "@민", "#general" -> "#g"), so
// double-width names cut at the Short step stay apart.
func shortLabel(l string) string {
	g := uniseg.NewGraphemes(strings.TrimLeft(l, " "))
	out, w := "", 0
	for g.Next() {
		c := g.Str()
		cw := textwidth.Width(c)
		if strings.TrimSpace(c) == "" || (w+cw > 2 && !sigil(out)) {
			break
		}
		out, w = out+c, w+cw
		if w >= 2 && !sigil(out) {
			break
		}
	}
	return out
}

// sigil reports whether s is one ASCII punctuation or symbol mark (#, @, ...).
func sigil(s string) bool {
	return len(s) == 1 && (unicode.IsPunct(rune(s[0])) || unicode.IsSymbol(rune(s[0])))
}

// segment renders entry i under f and returns its width.
func (b *WalkBar) segment(th *theme.Theme, i int, f walkFit) (string, int) {
	prev, next := b.marks()
	lead, trail := "", ""
	if f.padded {
		lead, trail = " ", " "
	}
	if i == prev {
		lead = "["
	}
	if i == next {
		trail = "]"
	}
	text := lead + b.label(i, f) + trail
	st := th.S.Tab
	if i == b.active {
		st = th.S.TabActive
	}
	out, w := st.Render(text), textwidth.Width(text)
	if f.badges {
		badge := Badge(th, b.items[i].Badge)
		out += badge
		w += textwidth.Width(badge)
	}
	return out, w
}

// width is the whole bar's width under f (one blank between entries).
func (b *WalkBar) width(th *theme.Theme, f walkFit) int {
	w := len(b.items) - 1
	for i := range b.items {
		_, sw := b.segment(th, i, f)
		w += sw
	}
	return w
}

// fit picks the first shortening step whose bar fits in width cells (the last
// step when none does: View then clips it).
func (b *WalkBar) fit(th *theme.Theme, width int) walkFit {
	full := walkFit{padded: true, badges: true}
	if b.width(th, full) <= width {
		return full
	}
	maxW := 0
	for i := range b.items {
		maxW = max(maxW, textwidth.Width(b.label(i, full)))
	}
	for c := maxW - 1; c >= 3; c-- {
		if f := (walkFit{capW: c, padded: true, badges: true}); b.width(th, f) <= width {
			return f
		}
	}
	for _, f := range []walkFit{
		{short: true, padded: true, badges: true},
		{short: true, badges: true},
	} {
		if b.width(th, f) <= width {
			return f
		}
	}
	return walkFit{short: true}
}

// View renders the bar at exactly width cells.
func (b *WalkBar) View(th *theme.Theme, width int) string {
	b.spans = nil
	if width <= 0 {
		return ""
	}
	if len(b.items) == 0 {
		return pad(th, width)
	}
	f := b.fit(th, width)
	var sb strings.Builder
	x := 0
	for i := range b.items {
		if i > 0 {
			sb.WriteString(pad(th, 1))
			x++
		}
		seg, sw := b.segment(th, i, f)
		sb.WriteString(seg)
		b.spans = append(b.spans, tabSpan{x, x + sw, i})
		x += sw
	}
	out := textwidth.Truncate(sb.String(), width, "")
	kept := b.spans[:0]
	for _, sp := range b.spans {
		if sp.x0 < width {
			sp.x1 = min(sp.x1, width)
			kept = append(kept, sp)
		}
	}
	b.spans = kept
	return out + pad(th, width-textwidth.Width(out))
}

// Hit maps a column to an entry index (from the last View).
func (b *WalkBar) Hit(x int) (index int, ok bool) {
	for _, sp := range b.spans {
		if x >= sp.x0 && x < sp.x1 {
			return sp.index, true
		}
	}
	return 0, false
}
