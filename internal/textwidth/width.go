package textwidth

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Width is ansi.StringWidth(s) — call on sanitized text.
func Width(s string) int {
	return ansi.StringWidth(s)
}

// Truncate is grapheme-safe, ANSI-aware and never splits a wide char; result Width <= w.
// When a wide char does not fit and one cell is left over, a space fills it (CA 8.3).
func Truncate(s string, w int, tail string) string {
	if w <= 0 {
		return ""
	}
	if Width(s) <= w {
		return s
	}
	out := ansi.Truncate(s, w, tail)
	if Width(out) > w { // defensive; ansi.Truncate already guarantees this
		out = ansi.Truncate(s, w, "")
	}
	if Width(out) == w-1 {
		out += " "
	}
	return out
}

// TruncateLeft truncates from the left, prefixing head; result Width <= w.
// When a wide char does not fit and one cell is left over, a leading space fills it.
func TruncateLeft(s string, w int, head string) string {
	if w <= 0 {
		return ""
	}
	sw := Width(s)
	if sw <= w {
		return s
	}
	hw := Width(head)
	if hw >= w {
		return Truncate(head, w, "")
	}
	// ansi.TruncateLeft removes n cells from the left but keeps a wide char
	// that straddles the cut, so grow n until the result fits.
	out := head
	for n := sw - (w - hw); n < sw; n++ {
		if r := ansi.TruncateLeft(s, n, head); Width(r) <= w {
			out = r
			break
		}
	}
	if Width(out) == w-1 {
		out = " " + out
	}
	return out
}

// PadRight pads with spaces to exactly w (truncates if wider).
func PadRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = Truncate(s, w, "")
	return s + spaces(w-Width(s))
}

// PadLeft pads on the left to exactly w (truncates if wider).
func PadLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = Truncate(s, w, "")
	return spaces(w-Width(s)) + s
}

// Center centers s in exactly w cells (extra cell goes right; truncates if wider).
func Center(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = Truncate(s, w, "")
	gap := w - Width(s)
	left := gap / 2
	return spaces(left) + s + spaces(gap-left)
}

// Fit returns exactly h lines, each exactly w cells (pad/truncate) — used by every pane.
func Fit(lines []string, w, h int) []string {
	if h <= 0 {
		return []string{}
	}
	out := make([]string, h)
	for i := range out {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = PadRight(l, w)
	}
	return out
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// Wrap word-wraps at spaces, hard-wraps long words, CJK breakable between syllables; each line Width <= w.
// "\n" starts a new line; ANSI escapes are kept and not counted. w <= 0 yields []string{""}.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return []string{""}
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		lines = append(lines, wrapParagraph(para, w)...)
	}
	return lines
}

// token is one printable grapheme g with the zero-width escapes/marks before it (pre).
type token struct {
	pre, g string
	w      int
	space  bool
}

// unit is an unbreakable run of tokens: a word, a single CJK/wide grapheme
// (with attached punctuation), or a run of spaces.
type unit struct {
	toks  []token
	w     int
	space bool
}

func (u unit) text() string {
	var b strings.Builder
	for _, t := range u.toks {
		b.WriteString(t.pre)
		b.WriteString(t.g)
	}
	return b.String()
}

// escapes returns only the zero-width prefixes of u (kept when its spaces are dropped).
func (u unit) escapes() string {
	var b strings.Builder
	for _, t := range u.toks {
		b.WriteString(t.pre)
	}
	return b.String()
}

func tokenize(s string) (toks []token, trailing string) {
	var state byte
	var pending strings.Builder
	for s != "" {
		seq, _, n, newState := ansi.DecodeSequence(s, state, nil)
		state = newState
		if n <= 0 || n > len(s) {
			n = 1
			seq = s[:1]
		}
		s = s[n:]
		gw := ansi.StringWidth(seq)
		if gw == 0 {
			pending.WriteString(seq)
			continue
		}
		toks = append(toks, token{pre: pending.String(), g: seq, w: gw, space: strings.Trim(seq, " ") == ""})
		pending.Reset()
	}
	return toks, pending.String()
}

const (
	noBreakBefore = ".,!?:;)]}'\"%…、。，．！？：；）」』】〉》〕’”~-"
	noBreakAfter  = "([{'\"“‘「『【〈《〔"
)

// breakable reports whether a line may break between graphemes a and b
// (neither is a space): only around wide (CJK/Hangul/emoji) graphemes, and
// never before closing or after opening punctuation.
func breakable(a, b token) bool {
	if a.w < 2 && b.w < 2 {
		return false
	}
	return !strings.Contains(noBreakBefore, b.g) && !strings.Contains(noBreakAfter, a.g)
}

func toUnits(toks []token) []unit {
	var units []unit
	for i, t := range toks {
		if i > 0 {
			last := &units[len(units)-1]
			if last.space == t.space && (t.space || !breakable(toks[i-1], t)) {
				last.toks = append(last.toks, t)
				last.w += t.w
				continue
			}
		}
		units = append(units, unit{toks: []token{t}, w: t.w, space: t.space})
	}
	return units
}

func wrapParagraph(s string, w int) []string {
	toks, trailing := tokenize(s)
	var (
		lines      []string
		line       strings.Builder
		lineW      int
		hasContent bool
		pend       *unit  // spaces waiting for the next word on this line
		carry      string // escapes of dropped spaces, written before the next text
	)
	dropPending := func() {
		if pend != nil {
			carry += pend.escapes()
			pend = nil
		}
	}
	flush := func() {
		dropPending()
		lines = append(lines, line.String())
		line.Reset()
		lineW, hasContent = 0, false
	}
	put := func(text string, tw int) {
		line.WriteString(carry)
		carry = ""
		line.WriteString(text)
		lineW += tw
		hasContent = true
	}
	pendW := func() int {
		if pend == nil {
			return 0
		}
		return pend.w
	}
	for i, u := range toUnits(toks) {
		if u.space && i > 0 { // leading spaces of a paragraph are kept as indentation
			if hasContent {
				u := u
				pend = &u
			} else {
				carry += u.escapes() // spaces at a wrapped line start are dropped
			}
			continue
		}
		switch {
		case lineW+pendW()+u.w <= w:
			if pend != nil {
				put(pend.text(), pend.w)
				pend = nil
			}
			put(u.text(), u.w)
		case u.w <= w:
			flush()
			put(u.text(), u.w)
		default: // longer than a line: hard-wrap by grapheme
			if hasContent {
				flush()
			}
			for _, t := range u.toks {
				g, tw := t.g, t.w
				if tw > w { // cannot fit even alone
					g, tw = "…", 1
				}
				if lineW+tw > w {
					flush()
				}
				put(t.pre+g, tw)
			}
		}
	}
	dropPending()
	line.WriteString(carry)
	line.WriteString(trailing)
	lines = append(lines, line.String())
	return lines
}
