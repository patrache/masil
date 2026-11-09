package uitest

import (
	"cmp"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// The x-pseudo region check (P5 Task 4, i18n spec 9.3). Under x-pseudo every
// catalog string is drawn wrapped in ⟦⟧; letters outside a wrap are either
// user data (fixtures.UserData), key and command names, or text that never
// went through the catalog.

// PseudoRegions cuts Screen() into region key => text, by the cells each drawn
// box owns in the last Frame(): "pane/<SlotID>/<RouteKind>" for a pane showing
// a route ("pane/<SlotID>" for a route-less pane), "status" for the status
// line, "overlay/<Box.ID>" for each open overlay, "toast" for the toast box and
// "screen" for cells no box owns (the too-small notice). A higher box owns the
// cells it covers (panes < status < overlays bottom -> top < toast). A region's
// text has one line per screen row it owns; cells of a row it owns apart are
// joined by one space.
func PseudoRegions(h *Harness) map[string]string {
	f := h.Model.Frame()
	lines := strings.Split(h.Screen(), "\n")
	rows, cols := len(lines), f.Layout.Cols
	for _, l := range lines {
		cols = max(cols, ansi.StringWidth(l))
	}
	owner := make([][]string, rows)
	for y := range owner {
		owner[y] = make([]string, cols)
	}
	paint := func(r layout.Rect, key string) {
		for y := max(r.Y, 0); y < min(r.Y+r.H, rows); y++ {
			for x := max(r.X, 0); x < min(r.X+r.W, cols); x++ {
				owner[y][x] = key
			}
		}
	}
	var status []layout.Rect
	for _, p := range f.Layout.Panes {
		if p.Slot == layout.SlotStatus {
			status = append(status, p.Rect)
			continue
		}
		key := "pane/" + string(p.Slot)
		if r, ok := f.Routes[p.Slot]; ok {
			key += "/" + string(r.Kind)
		}
		paint(p.Rect, key)
	}
	for _, r := range status {
		paint(r, "status")
	}
	for _, o := range f.Overlays {
		paint(o.Rect, "overlay/"+o.ID)
	}
	if f.Toast != nil {
		paint(f.Toast.Rect, "toast")
	}

	type seg struct {
		row  int
		b    strings.Builder
		last int // column after the last cell written
	}
	segs := map[string]*seg{}
	texts := map[string][]string{}
	flush := func(key string) {
		if s := segs[key]; s != nil {
			texts[key] = append(texts[key], s.b.String())
			delete(segs, key)
		}
	}
	for y, line := range lines {
		x := 0
		for line != "" {
			g, w := ansi.FirstGraphemeCluster(line, ansi.GraphemeWidth)
			if g == "" {
				_, n := utf8.DecodeRuneInString(line)
				g = line[:n]
			}
			line = line[len(g):]
			key := "screen"
			if x < cols && owner[y][x] != "" {
				key = owner[y][x]
			}
			s := segs[key]
			switch {
			case s == nil:
				s = &seg{row: y}
				segs[key] = s
			case s.row != y:
				flush(key)
				s = &seg{row: y}
				segs[key] = s
			case s.last != x:
				s.b.WriteByte(' ')
			}
			s.b.WriteString(g)
			x += max(w, 0)
			s.last = x
		}
		for key, s := range segs {
			if s.row == y {
				flush(key)
			}
		}
	}
	out := make(map[string]string, len(texts))
	for k, ls := range texts {
		out[k] = strings.Join(ls, "\n")
	}
	return out
}

// pseudoKeyNames are the key names help and hints show (never translated).
var pseudoKeyNames = []string{
	"ctrl", "shift", "alt", "enter", "esc", "tab", "space", "backspace", "delete",
	"home", "end", "pgup", "pgdown", "up", "down", "left", "right", "gg",
	"ctrl+k", "ctrl+f", "ctrl+n", "ctrl+w", "ctrl+o", "ctrl+t", "ctrl+c", "ctrl+u", "ctrl+d",
	`ctrl+\`, "shift+tab", "shift+enter", "alt+enter", "gg/home",
}

// PseudoAllow is what may stay unwrapped under x-pseudo: fixtures.UserData,
// key names (enter, ctrl+k …), slash command names ("/poll" and "poll") and
// the product name "masil".
func PseudoAllow(h *Harness) []string {
	out := append(fixtures.UserData(), pseudoKeyNames...)
	for _, s := range slash.Catalog(h.Context().T) {
		out = append(out, string(s.Name), "/"+string(s.Name))
	}
	out = append(out, "masil")
	slices.Sort(out)
	return slices.Compact(out)
}

// AssertPseudoWrapped fails (tb.Errorf, naming region and the words) when text
// shows letters outside ⟦⟧ that allow does not cover. A wrap runs from ⟦ to
// ⟧; one cut short (no ⟧) ends at "…" (truncated), at a border cell (box
// drawing) or at the end of its line — it never covers a later line. The one
// exception is a wrap wrapped onto the next line: when a line ends with a
// wrap still open, the first ⟧ with no ⟦ before it on the very next line
// covers back to that line's last border cell before it.
// Left over, digits, punctuation, symbols, borders, spaces and single letters
// pass; a run of two or more letters (any script) passes only as part of a
// whole allow entry (word-exact: allow "sam" does not cover "sammy"), or as
// one of two or more consecutive words of a multi-word entry (a message body
// cut by wrapping or truncation) — never one word of such an entry alone.
func AssertPseudoWrapped(tb testing.TB, region, text string, allow []string) {
	tb.Helper()
	pairs := map[[2]string]bool{}
	var phrases []string
	for _, a := range allow {
		ws := letterRuns(a)
		for i := 1; i < len(ws); i++ {
			pairs[[2]string{ws[i-1], ws[i]}] = true
		}
		if a != "" {
			phrases = append(phrases, a)
		}
	}
	slices.SortFunc(phrases, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	var bad []string
	open := false
	for _, line := range strings.Split(text, "\n") {
		line, open = maskPseudo(line, open)
		for _, p := range phrases {
			line = removeWord(line, p)
		}
		runs := letterRuns(line)
		for i, w := range runs {
			if utf8.RuneCountInString(w) < 2 {
				continue
			}
			if (i > 0 && pairs[[2]string{runs[i-1], w}]) || (i+1 < len(runs) && pairs[[2]string{w, runs[i+1]}]) {
				continue
			}
			bad = append(bad, w)
		}
	}
	if len(bad) > 0 {
		tb.Errorf("%s: text outside the catalog under x-pseudo: %q", region, bad)
	}
}

func isBorder(r rune) bool { return r >= 0x2500 && r <= 0x257F }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) }

// maskPseudo blanks the wrapped parts of one line (see AssertPseudoWrapped).
// prevOpen: the line above ended with a wrap still open; open: this one does.
func maskPseudo(line string, prevOpen bool) (_ string, open bool) {
	rs := []rune(line)
	in := false
	lastBorder := -1
	for i, r := range rs {
		if isBorder(r) {
			lastBorder = i
			if in { // cut at the border
				in, open = false, true
			}
			continue
		}
		switch {
		case in:
			rs[i] = ' '
			if r == '⟧' || r == '…' { // closed, or truncated
				in = false
			}
		case r == '⟦':
			rs[i], in, open, prevOpen = ' ', true, false, false
		case r == '⟧' && prevOpen: // closes the wrap the line above left open
			for j := lastBorder + 1; j <= i; j++ {
				rs[j] = ' '
			}
			prevOpen = false
		}
	}
	return string(rs), open || in
}

// letterRuns are s's maximal runs of letters (marks included).
func letterRuns(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !isWordRune(r) })
}

// removeWord blanks every occurrence of p in s that is not part of a longer
// letter run (word-exact).
func removeWord(s, p string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, p)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[i+len(p):])
		first, _ := utf8.DecodeRuneInString(p)
		last, _ := utf8.DecodeLastRuneInString(p)
		edgeOK := (i == 0 || !isWordRune(before) || !isWordRune(first)) &&
			(i+len(p) == len(s) || !isWordRune(after) || !isWordRune(last))
		b.WriteString(s[:i])
		if edgeOK {
			b.WriteString(strings.Repeat(" ", utf8.RuneCountInString(p)))
		} else {
			b.WriteString(p)
		}
		s = s[i+len(p):]
	}
}
