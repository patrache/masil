package uitest

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
)

// Box is a drawn rectangle. Z: panes 0, overlays 10+i, toast 100.
type Box struct {
	Rect     layout.Rect
	Bordered bool
	Z        int
}

// Z levels of BoxesOf.
const (
	zPane    = 0
	zOverlay = 10
	zToast   = 100
)

// maxIntegrityReports caps the per-call violation reports; one summary line follows.
const maxIntegrityReports = 20

// BoxesOf returns panes (Layout) + Overlays + Toast.
func BoxesOf(f app.FrameInfo) []Box {
	var out []Box
	for _, p := range f.Layout.Panes {
		out = append(out, Box{Rect: p.Rect, Bordered: p.Bordered, Z: zPane})
	}
	for i, o := range f.Overlays {
		out = append(out, Box{Rect: o.Rect, Bordered: o.Bordered, Z: zOverlay + i})
	}
	if f.Toast != nil {
		out = append(out, Box{Rect: f.Toast.Rect, Bordered: f.Toast.Bordered, Z: zToast})
	}
	return out
}

// AssertIntegrity checks line count/width, borders, forbidden runes, status line and bounds (CA 10.2).
//
// Every violation is reported with tb.Errorf (never Fatal), so one frame shows all its
// problems; after maxIntegrityReports reports a single summary line is added.
//
//  1. exactly h lines; each line has ansi.StringWidth == w and ansi.StringWidthWc == w.
//  2. every Bordered box has its four corners and all four edges drawn with border's
//     runes, except the title segment of the top edge and cells covered by a box of
//     higher Z.
//  3. no U+FFFD, control characters (C0, DEL, C1) or invalid UTF-8 (unpaired surrogates).
//  4. outside TooSmall (w < 20 or h < 6), when there is at least one layout pane, the
//     last row is not part of any Bordered box (it is the status line).
//  5. every box is on screen (0 <= X, X+W <= w, 0 <= Y, Y+H <= h), and outside TooSmall
//     no overlay or toast covers the status line.
//  6. the raw line holds no escape sequence but SGR (CSI ... m) and OSC 8 hyperlinks
//     whose target is printable ASCII (pending-fixes 0-b): ansi.Strip would hide an
//     injected OSC 52 clipboard write, OSC 0/2 title, screen clear or cursor move
//     from checks 1 and 3.
//
// The screen is ansi.Strip'ed and cut into cells by grapheme width: a wide grapheme
// occupies its first cell, the following cell(s) hold a continuation marker.
func AssertIntegrity(tb testing.TB, content string, w, h int, boxes []Box, border lipgloss.Border) {
	tb.Helper()
	r := &reporter{tb: tb}
	defer r.finish()

	lines := strings.Split(content, "\n")
	if len(lines) != h {
		r.errorf("frame has %d lines, want %d", len(lines), h)
	}
	grid := make([][]string, len(lines))
	for y, line := range lines {
		if sw := ansi.StringWidth(line); sw != w {
			r.errorf("line %d: width %d, want %d: %q", y, sw, w, ansi.Strip(line))
		}
		if wc := ansi.StringWidthWc(line); wc != w {
			r.errorf("line %d: StringWidthWc %d, want %d (grapheme/wcwidth renderers disagree, CA 8.2): %q",
				y, wc, w, ansi.Strip(line))
		}
		checkEscapes(r, y, line)
		plain := ansi.Strip(line)
		checkRunes(r, y, plain)
		grid[y] = toCells(plain)
	}

	s := screen{grid: grid, w: w, h: h}
	for _, b := range boxes {
		if b.Rect.X < 0 || b.Rect.Y < 0 || b.Rect.W < 0 || b.Rect.H < 0 ||
			b.Rect.X+b.Rect.W > w || b.Rect.Y+b.Rect.H > h {
			r.errorf("box %+v (Z=%d) out of bounds of the %dx%d screen", b.Rect, b.Z, w, h)
		}
	}
	for _, b := range boxes {
		if b.Bordered {
			checkBorder(r, s, b, boxes, border)
		}
	}

	bp := layout.DefaultBreakpoints()
	tooSmall := w < bp.MinCols || h < bp.MinRows
	hasPane := false
	for _, b := range boxes {
		if b.Z == zPane {
			hasPane = true
		}
	}
	if tooSmall || h <= 0 {
		return
	}
	last := h - 1
	for _, b := range boxes {
		if !onRow(b.Rect, last) {
			continue
		}
		if b.Z > zPane {
			r.errorf("box %+v (Z=%d) covers the status line (row %d)", b.Rect, b.Z, last)
		} else if hasPane && b.Bordered {
			r.errorf("status line: last row %d is part of bordered box %+v", last, b.Rect)
		}
	}
}

// reporter caps the number of violations reported through tb.
type reporter struct {
	tb testing.TB
	n  int
}

func (r *reporter) errorf(format string, args ...any) {
	r.tb.Helper()
	r.n++
	if r.n <= maxIntegrityReports {
		r.tb.Errorf("integrity: "+format, args...)
	}
}

func (r *reporter) finish() {
	r.tb.Helper()
	if r.n > maxIntegrityReports {
		r.tb.Errorf("integrity: ... and %d more violations (%d total)", r.n-maxIntegrityReports, r.n)
	}
}

// checkRunes reports forbidden runes in one stripped line (check 3).
func checkRunes(r *reporter, y int, plain string) {
	for i := 0; i < len(plain); {
		c, size := utf8.DecodeRuneInString(plain[i:])
		col := ansi.StringWidth(plain[:i])
		switch {
		case c == utf8.RuneError && size == 1:
			r.errorf("line %d col %d: invalid UTF-8 byte %#x (unpaired surrogate or broken sequence)", y, col, plain[i])
		case c == utf8.RuneError:
			r.errorf("line %d col %d: U+FFFD replacement character", y, col)
		case c < 0x20 || c == 0x7f || (c >= 0x80 && c <= 0x9f):
			r.errorf("line %d col %d: control character %U", y, col, c)
		}
		i += size
	}
}

// checkEscapes reports every escape or control sequence of a raw line other
// than SGR and a clean OSC 8 hyperlink (check 6).
func checkEscapes(r *reporter, y int, line string) {
	var state byte
	for rest := line; rest != ""; {
		seq, width, n, ns := ansi.DecodeSequence(rest, state, nil)
		state = ns
		if n <= 0 { // defensive: never loop forever
			n = 1
			seq = rest[:1]
		}
		rest = rest[n:]
		if width > 0 && !escapeLike(seq) {
			continue // a printable grapheme
		}
		if !allowedEscape(seq) {
			r.errorf("line %d: escape sequence %q (only SGR and OSC 8 with a printable-ASCII target may reach the terminal)", y, seq)
		}
	}
}

// escapeLike reports a sequence that starts with ESC or a raw C1 byte, or holds
// a C0/DEL/C1 rune.
func escapeLike(seq string) bool {
	if seq == "" {
		return false
	}
	if c := seq[0]; c == 0x1b || c >= 0x80 && c <= 0x9f {
		return true
	}
	for _, c := range seq {
		if c < 0x20 || c >= 0x7f && c <= 0x9f {
			return true
		}
	}
	return false
}

// allowedEscape: not escape-like, an SGR sequence, or an OSC 8 hyperlink
// (BEL or ST terminated) whose parameters and target are printable ASCII.
func allowedEscape(seq string) bool {
	if !escapeLike(seq) {
		return true
	}
	if body, ok := strings.CutPrefix(seq, "\x1b["); ok {
		params, ok := strings.CutSuffix(body, "m")
		return ok && strings.Trim(params, "0123456789;:") == ""
	}
	body, ok := strings.CutPrefix(seq, "\x1b]8;")
	if !ok {
		return false
	}
	payload, ok := strings.CutSuffix(body, "\x07")
	if !ok {
		if payload, ok = strings.CutSuffix(body, "\x1b\\"); !ok {
			return false
		}
	}
	for i := 0; i < len(payload); i++ {
		if c := payload[i]; c <= 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// contCell marks the cells covered by the right part of a wide grapheme.
const contCell = "\x00cont"

// toCells splits a stripped line into screen cells.
func toCells(plain string) []string {
	var cells []string
	for rest := plain; rest != ""; {
		g, gw := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if g == "" { // defensive: never loop forever
			_, n := utf8.DecodeRuneInString(rest)
			g, gw = rest[:n], 1
		}
		rest = rest[len(g):]
		if gw <= 0 {
			if n := len(cells); n > 0 {
				cells[n-1] += g
			}
			continue
		}
		cells = append(cells, g)
		for range gw - 1 {
			cells = append(cells, contCell)
		}
	}
	return cells
}

// screen is the frame as cell rows, and the expected size.
type screen struct {
	grid [][]string
	w, h int
}

// onScreen reports whether (x, y) is inside the expected w x h screen.
func (s screen) onScreen(x, y int) bool { return x >= 0 && y >= 0 && x < s.w && y < s.h }

// at returns the cell at (x, y); ok is false outside the drawn frame.
func (s screen) at(x, y int) (string, bool) {
	if y < 0 || y >= len(s.grid) || x < 0 || x >= len(s.grid[y]) {
		return "", false
	}
	return s.grid[y][x], true
}

func showCell(c string, ok bool) string {
	switch {
	case !ok:
		return "<missing>"
	case c == contCell:
		return "<wide-char continuation>"
	}
	return fmt.Sprintf("%q", c)
}

// covered reports whether (x, y) lies under a box drawn above b.
func covered(b Box, boxes []Box, x, y int) bool {
	for _, o := range boxes {
		if o.Z > b.Z && o.Rect.Contains(x, y) {
			return true
		}
	}
	return false
}

func onRow(r layout.Rect, y int) bool { return r.W > 0 && r.H > 0 && y >= r.Y && y < r.Y+r.H }

// checkBorder verifies one bordered box's corners and edges (check 2).
func checkBorder(r *reporter, s screen, b Box, boxes []Box, border lipgloss.Border) {
	rc := b.Rect
	if rc.W < 2 || rc.H < 2 {
		r.errorf("bordered box %+v (Z=%d) is too small for a border", rc, b.Z)
		return
	}
	x0, y0, x1, y1 := rc.X, rc.Y, rc.X+rc.W-1, rc.Y+rc.H-1
	expect := func(kind string, x, y int, want string) {
		if covered(b, boxes, x, y) {
			return
		}
		if !s.onScreen(x, y) {
			return // reported by the bounds check
		}
		got, ok := s.at(x, y)
		if !ok || got != want {
			r.errorf("box %+v (Z=%d): %s at (%d,%d) = %s, want %q", rc, b.Z, kind, x, y, showCell(got, ok), want)
		}
	}
	expect("top-left corner", x0, y0, border.TopLeft)
	expect("top-right corner", x1, y0, border.TopRight)
	expect("bottom-left corner", x0, y1, border.BottomLeft)
	expect("bottom-right corner", x1, y1, border.BottomRight)

	// Title segment: scanning from the cell after the top-left corner, the first run
	// of non-Top cells (title, badges) plus the one cell right after it if blank.
	title := map[int]bool{}
	x := x0 + 1
	for ; x < x1; x++ {
		if c, _ := s.at(x, y0); c != border.Top {
			break
		}
	}
	for ; x < x1; x++ {
		c, ok := s.at(x, y0)
		if !ok || c == border.Top {
			break
		}
		title[x] = true
	}
	if c, _ := s.at(x, y0); x < x1 && c == " " {
		title[x] = true
	}

	for x := x0 + 1; x < x1; x++ {
		if !title[x] {
			expect("top edge", x, y0, border.Top)
		}
		expect("bottom edge", x, y1, border.Bottom)
	}
	for y := y0 + 1; y < y1; y++ {
		expect("left edge", x0, y, border.Left)
		expect("right edge", x1, y, border.Right)
	}
}
