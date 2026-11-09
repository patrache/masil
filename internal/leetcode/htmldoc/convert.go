// Package htmldoc turns a LeetCode problem's HTML body into sanitized document
// blocks (v0.1.3 spec A10.2, JA12): a small allowlist converter built on the
// standard library only. Tags outside the allowlist keep their text; every
// string that leaves the package went through textwidth.Sanitize.
//
// Inline spans use their own type with markup's kind values (the UI converts
// with markup.InlineKind(k)): importing markup would pull internal/api into
// the leetcode packages, which the import rule forbids.
package htmldoc

import (
	"html"
	"strconv"
	"strings"
	"unicode"

	"github.com/patrache/masil/internal/textwidth"
)

// Kind is a block kind.
type Kind int

// Block kinds.
const (
	Para Kind = iota
	Pre
	Bullet
	Numbered
	Image
)

// InlineKind is an inline span kind; the values equal markup.Text, Bold,
// Italic and InlineCode.
type InlineKind int

// Inline kinds.
const (
	Text InlineKind = iota
	Bold
	Italic
	InlineCode
)

// Inline is one styled run of text (sanitized).
type Inline struct {
	Kind InlineKind
	Text string
}

// Block is one document block.
type Block struct {
	Kind   Kind
	Inline []Inline // Para, Bullet, Numbered
	Text   string   // Pre: lines joined by "\n", each sanitized
	N      int      // Numbered: the item number
	Depth  int      // list nesting, 0-based, at most MaxDepth
	URL    string   // Image: only https://assets.leetcode.com/… ("" otherwise)
	Alt    string   // Image
}

const (
	// MaxInput is the largest input converted; longer input is cut.
	MaxInput = 1 << 20
	// MaxDepth is the deepest list Depth reported.
	MaxDepth = 3
	// maxNesting is the deepest element stack; deeper tags keep their text only.
	maxNesting = 64
	// imageHost is the only image URL prefix kept.
	imageHost = "https://assets.leetcode.com/"
	maxURL    = 2048
)

// Convert converts problem HTML to blocks. It never panics and never recurses.
func Convert(src string) []Block {
	if len(src) > MaxInput {
		src = src[:MaxInput]
	}
	c := &conv{}
	tokenize(src, c)
	c.finish()
	return c.out
}

type listCtx struct {
	ordered     bool
	n           int
	itemOpen    bool
	itemEmitted bool
}

type conv struct {
	out []Block

	stack   []string       // open allowlisted/transparent/other elements
	skipped map[string]int // open tags dropped past maxNesting, by name

	bold, italic, code int
	lists              []listCtx
	pre                int
	preBuf             strings.Builder
	sup, sub           int
	scriptBuf          strings.Builder // text inside <sup>/<sub>
	cells              []int           // per open <tr>: cells seen

	cur []Inline // the block being built
}

// voids never take a closing tag.
var voids = map[string]bool{"br": true, "img": true, "hr": true, "input": true, "meta": true, "link": true, "wbr": true, "col": true, "source": true, "area": true, "base": true, "embed": true, "param": true, "track": true}

func (c *conv) style() InlineKind {
	switch {
	case c.code > 0:
		return InlineCode
	case c.bold > 0:
		return Bold
	case c.italic > 0:
		return Italic
	}
	return Text
}

// text handles a run of character data (entities not yet decoded).
func (c *conv) text(raw string) {
	s := html.UnescapeString(raw)
	if c.sup > 0 || c.sub > 0 {
		c.scriptBuf.WriteString(s)
		return
	}
	if c.pre > 0 {
		c.preBuf.WriteString(s)
		return
	}
	c.add(s, c.style())
}

// finish emits what unclosed elements left at the end of the input.
func (c *conv) finish() {
	if c.scriptBuf.Len() > 0 {
		c.script(func(s string) string { return s })
	}
	if c.pre > 0 {
		c.pre = 0
		c.flushPre()
	}
	c.flush()
}

// add appends s (whitespace collapsed) to the current block in kind k.
func (c *conv) add(s string, k InlineKind) {
	s = collapse(s)
	if s == "" {
		return
	}
	if strings.HasPrefix(s, " ") && (len(c.cur) == 0 || strings.HasSuffix(c.cur[len(c.cur)-1].Text, " ")) {
		s = s[1:]
		if s == "" {
			return
		}
	}
	if n := len(c.cur); n > 0 && c.cur[n-1].Kind == k {
		c.cur[n-1].Text += s
		return
	}
	c.cur = append(c.cur, Inline{Kind: k, Text: s})
}

// collapse turns every whitespace run (incl. NBSP) into one space.
func collapse(s string) string {
	var sb strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == ' ' {
			if !space {
				sb.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		sb.WriteRune(r)
	}
	return sb.String()
}

// flush emits the current inline block, if it has visible text.
func (c *conv) flush() {
	in := c.cur
	c.cur = nil
	var spans []Inline
	for _, s := range in {
		t := textwidth.Sanitize(s.Text)
		if t == "" {
			continue
		}
		if n := len(spans); n > 0 && spans[n-1].Kind == s.Kind {
			spans[n-1].Text += t
			continue
		}
		spans = append(spans, Inline{Kind: s.Kind, Text: t})
	}
	// Trim the block's outer spaces.
	for len(spans) > 0 {
		spans[0].Text = strings.TrimLeft(spans[0].Text, " ")
		if spans[0].Text != "" {
			break
		}
		spans = spans[1:]
	}
	for len(spans) > 0 {
		n := len(spans) - 1
		spans[n].Text = strings.TrimRight(spans[n].Text, " ")
		if spans[n].Text != "" {
			break
		}
		spans = spans[:n]
	}
	if len(spans) == 0 {
		return
	}
	b := Block{Kind: Para, Inline: spans, Depth: c.depth()}
	if n := len(c.lists); n > 0 && c.lists[n-1].itemOpen && !c.lists[n-1].itemEmitted {
		l := &c.lists[n-1]
		l.itemEmitted = true
		if l.ordered {
			b.Kind, b.N = Numbered, l.n
		} else {
			b.Kind = Bullet
		}
	}
	c.out = append(c.out, b)
}

func (c *conv) depth() int {
	d := len(c.lists) - 1
	if d < 0 {
		return 0
	}
	return min(d, MaxDepth)
}

func (c *conv) flushPre() {
	raw := c.preBuf.String()
	c.preBuf.Reset()
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimPrefix(raw, "\n")
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(textwidth.Sanitize(strings.ReplaceAll(l, "\t", "    ")), " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return
	}
	c.out = append(c.out, Block{Kind: Pre, Text: strings.Join(lines, "\n"), Depth: c.depth()})
}

// open handles a start tag (name lowercased).
func (c *conv) open(name string, attrs map[string]string, selfClose bool) {
	void := voids[name] || selfClose
	if !void && len(c.stack) >= maxNesting {
		if c.skipped == nil {
			c.skipped = map[string]int{}
		}
		c.skipped[name]++
		return
	}
	switch name {
	case "br":
		if c.pre > 0 {
			c.preBuf.WriteByte('\n')
		} else if c.sup == 0 && c.sub == 0 {
			c.flush()
		}
	case "img":
		c.image(attrs)
	}
	if void {
		return
	}
	// Implicit closes: <p> in <p>, <li> after an open <li> of the same list.
	switch name {
	case "p":
		if c.top() == "p" {
			c.close("p")
		}
	case "li":
		for i := len(c.stack) - 1; i >= 0; i-- {
			if c.stack[i] == "ul" || c.stack[i] == "ol" {
				break
			}
			if c.stack[i] == "li" {
				c.close("li")
				break
			}
		}
	}
	c.stack = append(c.stack, name)
	switch name {
	case "p", "tr":
		if c.pre == 0 {
			c.flush()
		}
		if name == "tr" {
			c.cells = append(c.cells, 0)
		}
	case "td", "th":
		if n := len(c.cells); n > 0 && c.pre == 0 {
			if c.cells[n-1] > 0 {
				c.add(" | ", Text)
			}
			c.cells[n-1]++
		}
	case "pre":
		if c.pre == 0 {
			c.flush()
		}
		c.pre++
	case "ul", "ol":
		c.flush()
		l := listCtx{ordered: name == "ol"}
		if l.ordered {
			if v, err := strconv.Atoi(strings.TrimSpace(attrs["start"])); err == nil && v >= 1 && v <= 1_000_000 {
				l.n = v - 1
			}
		}
		c.lists = append(c.lists, l)
	case "li":
		c.flush()
		if n := len(c.lists); n > 0 {
			l := &c.lists[n-1]
			l.n++
			l.itemOpen, l.itemEmitted = true, false
		}
	case "strong", "b":
		c.bold++
	case "em", "i":
		c.italic++
	case "code":
		c.code++
	case "sup":
		c.sup++
	case "sub":
		c.sub++
	}
}

func (c *conv) top() string {
	if len(c.stack) == 0 {
		return ""
	}
	return c.stack[len(c.stack)-1]
}

// close handles an end tag: it pops to the matching open element, closing
// the ones above it; an unmatched end tag is ignored.
func (c *conv) close(name string) {
	if c.skipped[name] > 0 {
		c.skipped[name]--
		return
	}
	at := -1
	for i := len(c.stack) - 1; i >= 0; i-- {
		if c.stack[i] == name {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	for len(c.stack) > at {
		top := c.stack[len(c.stack)-1]
		c.stack = c.stack[:len(c.stack)-1]
		c.end(top)
	}
}

// end undoes what open did for one element.
func (c *conv) end(name string) {
	switch name {
	case "p":
		if c.pre == 0 {
			c.flush()
		}
	case "tr":
		if c.pre == 0 {
			c.flush()
		}
		if n := len(c.cells); n > 0 {
			c.cells = c.cells[:n-1]
		}
	case "table":
		if c.pre == 0 {
			c.flush()
		}
	case "pre":
		c.pre--
		if c.pre == 0 {
			c.flushPre()
		}
	case "li":
		c.flush()
		if n := len(c.lists); n > 0 {
			c.lists[n-1].itemOpen = false
		}
	case "ul", "ol":
		c.flush()
		if n := len(c.lists); n > 0 {
			c.lists = c.lists[:n-1]
		}
	case "strong", "b":
		c.bold--
	case "em", "i":
		c.italic--
	case "code":
		c.code--
	case "sup":
		c.sup--
		if c.sup == 0 && c.sub == 0 {
			c.script(superscript)
		}
	case "sub":
		c.sub--
		if c.sup == 0 && c.sub == 0 {
			c.script(func(s string) string { return "_" + s })
		}
	}
}

// script emits the buffered <sup>/<sub> text through conv.
func (c *conv) script(conv func(string) string) {
	s := collapse(c.scriptBuf.String())
	c.scriptBuf.Reset()
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if c.pre > 0 {
		c.preBuf.WriteString(conv(s))
		return
	}
	c.add(conv(s), c.style())
}

var supDigits = map[rune]rune{'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹', '+': '⁺', '-': '⁻'}

// superscript maps digits and +- to Unicode superscripts, or falls back to
// "^" + the text when any other character is present.
func superscript(s string) string {
	var sb strings.Builder
	for _, r := range s {
		u, ok := supDigits[r]
		if !ok {
			return "^" + s
		}
		sb.WriteRune(u)
	}
	return sb.String()
}

func (c *conv) image(attrs map[string]string) {
	if c.pre > 0 {
		return
	}
	c.flush()
	b := Block{Kind: Image, Depth: c.depth(), Alt: strings.TrimSpace(textwidth.Sanitize(collapse(attrs["alt"])))}
	if u := strings.TrimSpace(attrs["src"]); strings.HasPrefix(u, imageHost) && len(u) <= maxURL && safeURL(u) {
		b.URL = u
	}
	c.out = append(c.out, b)
}

// safeURL: printable ASCII only, no quotes, spaces or angle brackets.
func safeURL(u string) bool {
	for i := 0; i < len(u); i++ {
		ch := u[i]
		if ch <= ' ' || ch >= 0x7f || ch == '"' || ch == '\'' || ch == '<' || ch == '>' || ch == '\\' {
			return false
		}
	}
	return true
}

// handler receives tokens.
type handler interface {
	text(raw string)
	open(name string, attrs map[string]string, selfClose bool)
	close(name string)
}

// rawText elements' content is skipped entirely (never shown as text).
var rawText = map[string]bool{"script": true, "style": true}

// tokenize splits src into text and tags, without recursion.
func tokenize(src string, h handler) {
	i, textStart := 0, 0
	emitText := func(end int) {
		if end > textStart {
			h.text(src[textStart:end])
		}
	}
	for i < len(src) {
		if src[i] != '<' {
			i++
			continue
		}
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			emitText(i)
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				i = len(src)
			} else {
				i += 4 + end + 3
			}
			textStart = i
		case len(rest) > 1 && (rest[1] == '!' || rest[1] == '?'):
			emitText(i)
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				i = len(src)
			} else {
				i += end + 1
			}
			textStart = i
		case len(rest) > 2 && rest[1] == '/' && isLetter(rest[2]):
			emitText(i)
			name, _ := readName(rest[2:])
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				i = len(src)
			} else {
				i += end + 1
			}
			textStart = i
			h.close(name)
		case len(rest) > 1 && isLetter(rest[1]):
			emitText(i)
			name, n := readName(rest[1:])
			attrs, selfClose, used, ok := readAttrs(rest[1+n:])
			if !ok {
				// An unterminated tag at the end: dropped.
				i = len(src)
				textStart = i
				continue
			}
			i += 1 + n + used
			textStart = i
			if rawText[name] && !selfClose {
				end := indexFold(src[i:], "</"+name)
				if end < 0 {
					i = len(src)
				} else {
					i += end
					if gt := strings.IndexByte(src[i:], '>'); gt >= 0 {
						i += gt + 1
					} else {
						i = len(src)
					}
				}
				textStart = i
				continue
			}
			h.open(name, attrs, selfClose)
		default:
			i++ // a literal '<'
		}
	}
	emitText(len(src))
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// readName reads a tag name and returns it lowercased with its byte length.
func readName(s string) (string, int) {
	n := 0
	for n < len(s) {
		b := s[n]
		if isLetter(b) || b >= '0' && b <= '9' || b == '-' || b == ':' {
			n++
			continue
		}
		break
	}
	return strings.ToLower(s[:n]), n
}

// readAttrs reads attributes up to and including '>'. ok is false when the
// input ends first.
func readAttrs(s string) (attrs map[string]string, selfClose bool, used int, ok bool) {
	i := 0
	for i < len(s) {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '>':
			return attrs, selfClose, i + 1, true
		case '/':
			selfClose = true
			i++
			continue
		}
		selfClose = false
		start := i
		for i < len(s) && !isSpace(s[i]) && s[i] != '=' && s[i] != '>' && s[i] != '/' {
			i++
		}
		key := strings.ToLower(s[start:i])
		if i == start { // a stray '=' etc.
			i++
			continue
		}
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && isSpace(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				end := strings.IndexByte(s[i+1:], q)
				if end < 0 {
					return nil, false, len(s), false
				}
				val = s[i+1 : i+1+end]
				i += end + 2
			} else {
				vs := i
				for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
					i++
				}
				val = s[vs:i]
			}
		}
		if attrs == nil {
			attrs = map[string]string{}
		}
		if _, dup := attrs[key]; !dup && len(attrs) < 32 {
			attrs[key] = html.UnescapeString(val)
		}
	}
	return nil, false, len(s), false
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

// indexFold is a case-insensitive strings.Index for an ASCII needle.
func indexFold(s, sub string) int {
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if strings.EqualFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}
