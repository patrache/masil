package markup

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
)

// RenderOptions controls Render.
type RenderOptions struct {
	Width       int
	MyHandle    string           // highlight mentions of me
	Mentions    []api.MentionRef // only these @handles are styled as mentions (resolved by server)
	MentionHere bool
	Compact     bool // single-line summary mode: code blocks => "‹코드 N줄›", newlines => " "
	MaxLines    int  // 0 = unlimited; overflow adds "… 더 보기"
	Hyperlinks  bool // OSC 8 via lipgloss Style.Hyperlink
}

const moreTail = "… 더 보기"

// styleKind names the theme style of a cell; cells with equal styleKey render as one run.
type styleKind int

const (
	kBase styleKind = iota
	kBold
	kItalic
	kInlineCode
	kLink
	kMention
	kMentionMe
	kHashtag
	kDim
	kCodeBar
	kCodeBlock
	kToken // chroma token, see styleKey.tt
)

type styleKey struct {
	kind styleKind
	tt   chroma.TokenType
	url  string // kLink with hyperlinks
}

// cell is one grapheme with its style.
type cell struct {
	g   string
	w   int
	key styleKey
}

type line []cell

// seg is a run of raw (unsanitized) text with one style; it may contain "\n".
type seg struct {
	text string
	key  styleKey
}

func codeSummary(code string) string {
	n := 0
	if code != "" {
		n = strings.Count(code, "\n") + 1
	}
	return "‹코드 " + strconv.Itoa(n) + "줄›"
}

// Render returns lines, each Width <= o.Width, sanitized.
func Render(d Doc, t *theme.Theme, o RenderOptions) []string {
	if o.Width <= 0 {
		return nil
	}
	var lines []line
	if o.Compact {
		lines = []line{compactLine(d, o)}
	} else {
		for _, b := range d.Blocks {
			if b.Kind == Code {
				lines = append(lines, highlightCells(b.Lang, b.Code, o.Width)...)
				continue
			}
			for _, logical := range splitLines(inlineSegs(b.Inline, o)) {
				lines = append(lines, wrapCells(logical, o.Width)...)
			}
		}
	}
	if o.MaxLines > 0 && len(lines) > o.MaxLines {
		lines = lines[:o.MaxLines]
		last := trimSpaceCells(lines[len(lines)-1])
		lines[len(lines)-1] = truncCells(last, o.Width, moreTail, styleKey{kind: kDim}, true)
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = renderLine(l, t)
	}
	return out
}

// compactLine joins every block into one line: newlines => " ", code => summary.
func compactLine(d Doc, o RenderOptions) line {
	var segs []seg
	for i, b := range d.Blocks {
		if i > 0 {
			segs = append(segs, seg{" ", styleKey{}})
		}
		if b.Kind == Code {
			segs = append(segs, seg{codeSummary(b.Code), styleKey{kind: kDim}})
			continue
		}
		for _, s := range inlineSegs(b.Inline, o) {
			s.text = strings.ReplaceAll(s.text, "\n", " ")
			segs = append(segs, s)
		}
	}
	return truncCells(cellsOf(segs), o.Width, "…", styleKey{}, false)
}

func mentionSet(o RenderOptions) map[string]bool {
	set := make(map[string]bool, len(o.Mentions))
	for _, m := range o.Mentions {
		set[strings.ToLower(m.Handle)] = true
	}
	return set
}

// inlineSegs maps inline spans to styled segments.
func inlineSegs(ins []Inline, o RenderOptions) []seg {
	mentioned := mentionSet(o)
	me := strings.ToLower(o.MyHandle)
	segs := make([]seg, 0, len(ins))
	for _, in := range ins {
		k := styleKey{}
		switch in.Kind {
		case Bold:
			k.kind = kBold
		case Italic:
			k.kind = kItalic
		case InlineCode:
			k.kind = kInlineCode
		case Link:
			k.kind = kLink
			if o.Hyperlinks && SafeURL(in.URL) {
				k.url = in.URL
			}
		case Mention:
			switch {
			case !mentioned[in.Handle]:
			case me != "" && in.Handle == me:
				k.kind = kMentionMe
			default:
				k.kind = kMention
			}
		case Here:
			if o.MentionHere {
				k.kind = kMentionMe
			}
		case Hashtag:
			k.kind = kHashtag
		}
		segs = append(segs, seg{in.Text, k})
	}
	return segs
}

// splitLines splits segments at "\n" into logical lines.
func splitLines(segs []seg) [][]seg {
	out := [][]seg{nil}
	for _, s := range segs {
		parts := strings.Split(s.text, "\n")
		for i, part := range parts {
			if i > 0 {
				out = append(out, nil)
			}
			if part != "" {
				out[len(out)-1] = append(out[len(out)-1], seg{part, s.key})
			}
		}
	}
	return out
}

// cellsOf sanitizes each segment and splits the joined text into graphemes.
// Separately sanitized pieces can merge into a new grapheme when joined, so a
// grapheme whose two widths differ is sanitized again until the line is stable.
func cellsOf(segs []seg) line {
	var cells line
	for _, s := range segs {
		if t := textwidth.Sanitize(s.text); t != "" {
			cells = append(cells, cell{g: t, key: s.key})
		}
	}
	for pass := 0; pass < 4; pass++ {
		var changed bool
		cells, changed = regraph(cells)
		if !changed {
			break
		}
	}
	return cells
}

// regraph re-splits the joined text of cells into graphemes, each keeping the
// style of the cell it starts in; changed reports a grapheme had to be fixed.
func regraph(in line) (out line, changed bool) {
	var plain strings.Builder
	starts := make([]int, len(in))
	for i, c := range in {
		starts[i] = plain.Len()
		plain.WriteString(c.g)
	}
	s := plain.String()
	ci := 0
	for off := 0; off < len(s); {
		g, _ := ansi.FirstGraphemeCluster(s[off:], ansi.GraphemeWidth)
		if g == "" {
			g = s[off : off+1]
		}
		for ci+1 < len(in) && starts[ci+1] <= off {
			ci++
		}
		off += len(g)
		if ansi.StringWidth(g) != ansi.StringWidthWc(g) {
			g = textwidth.Sanitize(g)
			changed = true
		}
		if g != "" {
			out = append(out, cell{g, ansi.StringWidth(g), in[ci].key})
		}
	}
	return out, changed
}

func (l line) width() int {
	w := 0
	for _, c := range l {
		w += c.w
	}
	return w
}

func (l line) plain() string {
	var b strings.Builder
	for _, c := range l {
		b.WriteString(c.g)
	}
	return b.String()
}

func isSpaceCell(c cell) bool { return strings.TrimSpace(c.g) == "" }

// wrapCells word-wraps one logical line with textwidth.Wrap on its plain text,
// then maps the wrapped graphemes back onto the styled cells (Wrap only drops
// spaces at breaks and replaces a grapheme wider than the line with "…").
func wrapCells(segs []seg, w int) []line {
	cells := cellsOf(segs)
	if len(cells) == 0 {
		return []line{nil}
	}
	wrapped := textwidth.Wrap(cells.plain(), w)
	out := make([]line, 0, len(wrapped))
	p := 0
	for _, wl := range wrapped {
		var l line
		for off := 0; off < len(wl); {
			g, _ := ansi.FirstGraphemeCluster(wl[off:], ansi.GraphemeWidth)
			if g == "" {
				g = wl[off : off+1]
			}
			off += len(g)
			for p < len(cells) && cells[p].g != g && isSpaceCell(cells[p]) {
				p++ // a space dropped at a line break
			}
			switch {
			case p < len(cells) && cells[p].g == g:
				l = append(l, cells[p])
				p++
			case p < len(cells) && strings.HasPrefix(g, cells[p].g):
				key := cells[p].key
				for acc := ""; p < len(cells) && acc != g && strings.HasPrefix(g, acc+cells[p].g); p++ {
					acc += cells[p].g
				}
				l = append(l, cell{g, ansi.StringWidth(g), key})
			case p < len(cells) && g == "…":
				l = append(l, cell{g, 1, cells[p].key})
				p++
			default: // defensive: keep what Wrap produced
				l = append(l, cell{g, ansi.StringWidth(g), styleKey{}})
			}
		}
		out = append(out, l)
	}
	return out
}

func trimSpaceCells(l line) line {
	for len(l) > 0 && isSpaceCell(l[len(l)-1]) {
		l = l[:len(l)-1]
	}
	return l
}

// truncCells cuts l so that l+tail fits in w cells; with always=false it only
// cuts (and adds tail) when l is wider than w.
func truncCells(l line, w int, tail string, tailKey styleKey, always bool) line {
	if !always && l.width() <= w {
		return l
	}
	tc := cellsOf([]seg{{tail, tailKey}})
	budget := w - tc.width()
	if budget < 0 {
		return takeCells(tc, w)
	}
	cut := takeCells(l, budget)
	if len(cut) < len(l) {
		cut = trimSpaceCells(cut)
	}
	return append(cut, tc...)
}

func takeCells(l line, w int) line {
	out := make(line, 0, len(l))
	used := 0
	for _, c := range l {
		if used+c.w > w {
			break
		}
		out = append(out, c)
		used += c.w
	}
	return out
}

func style(t *theme.Theme, k styleKey) lipgloss.Style {
	switch k.kind {
	case kBold:
		return t.S.Bold
	case kItalic:
		return t.S.Base.Italic(true)
	case kInlineCode:
		return t.S.InlineCode
	case kLink:
		if k.url != "" && SafeURL(k.url) { // second guard: never put control bytes in OSC 8
			return t.S.Link.Hyperlink(HyperlinkTarget(k.url))
		}
		return t.S.Link
	case kMention:
		return t.S.Mention
	case kMentionMe:
		return t.S.MentionMe
	case kHashtag:
		return t.S.Hashtag
	case kDim:
		return t.S.Dim
	case kCodeBar:
		return t.S.CodeBar
	case kCodeBlock:
		return t.S.CodeBlock
	case kToken:
		return t.CodeStyle(k.tt)
	default:
		return t.S.Base
	}
}

// renderLine renders runs of equally styled cells.
func renderLine(l line, t *theme.Theme) string {
	var b strings.Builder
	for i := 0; i < len(l); {
		j := i
		var run strings.Builder
		for j < len(l) && l[j].key == l[i].key {
			run.WriteString(l[j].g)
			j++
		}
		b.WriteString(renderRun(style(t, l[i].key), run.String()))
		i = j
	}
	return b.String()
}

// renderRun renders s in st as one SGR run. lipgloss styles an underlined or
// struck-through string rune by rune (to treat spaces apart), which splits
// multi-rune graphemes such as flags (🇰🇷) into separate SGR runs; each lone
// regional indicator then measures 2 cells and the line overflows its width.
// So those two attributes are dropped from st and set around the whole run
// instead (the run's closing reset clears them).
func renderRun(st lipgloss.Style, s string) string {
	under, strike := st.GetUnderline(), st.GetStrikethrough()
	if !under && !strike {
		return st.Render(s)
	}
	var pre ansi.Style
	if under {
		pre = pre.UnderlineStyle(st.GetUnderlineStyle()) // an underline color stays in st
	}
	if strike {
		pre = pre.Strikethrough(true)
	}
	return pre.String() + st.UnsetUnderline().UnsetStrikethrough().Render(s) + ansi.ResetStyle
}

// HyperlinkTarget percent-encodes u for an OSC 8 target: every byte outside
// the RFC 3986 unreserved and reserved sets (all bytes >= 0x80 included) and a
// "%" that does not start a %XX escape become %XX. Raw UTF-8 must never reach
// the escape: a continuation byte 0x9C (e.g. in 한 = ED 95 9C) is read as
// String Terminator and would end the sequence early. The visible link text is
// not affected.
func HyperlinkTarget(u string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c == '%' && i+2 < len(u) && isHex(u[i+1]) && isHex(u[i+2]):
			b.WriteByte(c)
		case c != '%' && uriByte(c):
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// uriByte reports an RFC 3986 unreserved or reserved character.
func uriByte(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	return strings.IndexByte("-._~:/?#[]@!$&'()*+,;=", c) >= 0
}
