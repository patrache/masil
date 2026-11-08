// Package markup parses and renders the body markdown subset (api.md 2.1, CA 4.8).
package markup

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/patrache/masil/internal/textwidth"
)

// Doc is a parsed body.
type Doc struct{ Blocks []Block }

// BlockKind is Paragraph | Code.
type BlockKind int

// Block kinds.
const (
	Paragraph BlockKind = iota
	Code
)

// Block is a paragraph or a fenced code block.
type Block struct {
	Kind   BlockKind // Paragraph | Code
	Inline []Inline  // Paragraph
	Lang   string    // Code
	Code   string    // Code
}

// InlineKind is Text | Bold | Italic | InlineCode | Link | Mention | Here | Hashtag.
//
// CA 4.8 names the inline code-span kind `Code`, which collides with the block
// kind `Code` in the same package; the inline one is named InlineCode.
type InlineKind int

// Inline kinds.
const (
	Text InlineKind = iota
	Bold
	Italic
	InlineCode
	Link
	Mention
	Here
	Hashtag
)

// Inline is one inline span.
//
// Text is what the span shows (the original "@MinJi" / "#GoLang" for mentions
// and hashtags, the content without markers for Bold/Italic/InlineCode, the
// label for Link). Handle is the normalized key: the lowercase handle for
// Mention and the lowercase tag (no "#") for Hashtag.
type Inline struct {
	Kind   InlineKind // Text | Bold | Italic | InlineCode | Link | Mention | Here | Hashtag
	Text   string
	URL    string // Link (also bare URLs)
	Handle string // Mention (lowercase, no @); Hashtag (lowercase, no #)
}

// ParseOptions: Hashtags is true for posts/stories, false for messages.
type ParseOptions struct{ Hashtags bool }

const fence = "```"

// Parse parses the api.md 2.1 grammar; no other markdown.
func Parse(body string, opts ParseOptions) Doc {
	var d Doc
	var para []string
	flush := func() {
		text := strings.Trim(strings.Join(para, "\n"), "\n")
		para = para[:0]
		if text == "" {
			return
		}
		d.Blocks = append(d.Blocks, Block{Kind: Paragraph, Inline: parseInline(text, opts)})
	}
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		lang, ok := openFence(lines[i])
		if !ok {
			para = append(para, lines[i])
			continue
		}
		flush()
		var code []string
		for i++; i < len(lines); i++ {
			if isCloseFence(lines[i]) {
				break
			}
			code = append(code, lines[i])
		}
		d.Blocks = append(d.Blocks, Block{Kind: Code, Lang: lang, Code: strings.Join(code, "\n")})
	}
	flush()
	return d
}

// openFence reports whether line opens a fenced code block ("```lang"); the
// info string may not contain backticks (so "```x```" stays inline text).
func openFence(line string) (lang string, ok bool) {
	t := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(t, fence) {
		return "", false
	}
	info := t[len(fence):]
	if strings.Contains(info, "`") {
		return "", false
	}
	if f := strings.Fields(info); len(f) > 0 {
		lang = f[0]
	}
	return lang, true
}

func isCloseFence(line string) bool {
	return strings.TrimSpace(line) == fence
}

// inlineParser scans one paragraph.
type inlineParser struct {
	s    string
	opts ParseOptions
	out  []Inline
	text strings.Builder
}

func parseInline(s string, opts ParseOptions) []Inline {
	p := &inlineParser{s: s, opts: opts}
	for i := 0; i < len(s); {
		if n := p.token(i); n > 0 {
			i += n
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		p.text.WriteString(s[i : i+size])
		i += size
	}
	p.flushText()
	return p.out
}

func (p *inlineParser) flushText() {
	if p.text.Len() > 0 {
		p.out = append(p.out, Inline{Kind: Text, Text: p.text.String()})
		p.text.Reset()
	}
}

func (p *inlineParser) emit(in Inline) {
	p.flushText()
	p.out = append(p.out, in)
}

// literal writes s[i:i+n] as text and returns n (consumes a marker that did not match).
func (p *inlineParser) literal(i, n int) int {
	p.text.WriteString(p.s[i : i+n])
	return n
}

// token tries every inline rule at byte i and returns the bytes consumed (0 = none).
func (p *inlineParser) token(i int) int {
	s := p.s
	if c := s[i]; (c == '*' || c == '_') && escaped(s, i) {
		return p.literal(i, runLen(s, i, c)) // `¯\_(ツ)_/¯` stays itself
	}
	switch c := s[i]; {
	case c == '`':
		return p.codeSpan(i)
	case strings.HasPrefix(s[i:], "**"):
		return p.bold(i)
	case c == '*':
		return p.star(i)
	case c == '_':
		return p.underscore(i)
	case c == '[':
		return p.link(i)
	case c == 'h' && (strings.HasPrefix(s[i:], "http://") || strings.HasPrefix(s[i:], "https://")):
		return p.bareURL(i)
	case c == '@':
		return p.mention(i)
	case c == '#' && p.opts.Hashtags:
		return p.hashtag(i)
	}
	return 0
}

// codeSpan: a run of N backticks closed by a run of exactly N, same line, non-empty.
func (p *inlineParser) codeSpan(i int) int {
	s := p.s
	n := runLen(s, i, '`')
	for j := i + n; j < len(s); {
		k := strings.IndexByte(s[j:], '`')
		if k < 0 {
			break
		}
		j += k
		m := runLen(s, j, '`')
		if m == n {
			content := s[i+n : j]
			if content == "" || strings.Contains(content, "\n") {
				break
			}
			p.emit(Inline{Kind: InlineCode, Text: content})
			return j + m - i
		}
		j += m
	}
	return p.literal(i, n)
}

// escaped reports a backslash right before byte i. The subset has no escapes
// (the backslash itself stays visible), but a "*" or "_" run after one neither
// opens nor closes an emphasis, so the /shrug face (api.md 5.10) is not italic.
func escaped(s string, i int) bool { return i > 0 && s[i-1] == '\\' }

// nextMarker is the index of the first unescaped marker at or after from, or -1.
func nextMarker(s string, from int, marker string) int {
	for j := from; j < len(s); {
		k := strings.Index(s[j:], marker)
		if k < 0 {
			return -1
		}
		if !escaped(s, j+k) {
			return j + k
		}
		j += k + len(marker)
	}
	return -1
}

func runLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func validEmphasis(content string) bool {
	return content != "" && !strings.Contains(content, "\n") &&
		!startsWithSpace(content) && !endsWithSpace(content)
}

func startsWithSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func endsWithSpace(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.IsSpace(r)
}

// bold: "**text**" with no nesting.
func (p *inlineParser) bold(i int) int {
	s := p.s
	if k := nextMarker(s, i+2, "**") - (i + 2); k >= 0 {
		if content := s[i+2 : i+2+k]; validEmphasis(content) {
			p.emit(Inline{Kind: Bold, Text: content})
			return k + 4
		}
	}
	return p.literal(i, 2)
}

// star: "*text*" italic.
func (p *inlineParser) star(i int) int {
	s := p.s
	if k := nextMarker(s, i+1, "*") - (i + 1); k >= 0 {
		if content := s[i+1 : i+1+k]; validEmphasis(content) {
			p.emit(Inline{Kind: Italic, Text: content})
			return k + 2
		}
	}
	return p.literal(i, 1)
}

// boundary reports whether r (utf8.RuneError for start/end of text) is a word boundary.
func boundary(r rune) bool {
	return r == utf8.RuneError || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// underscore: "_text_" only at word boundaries, so snake_case stays text.
func (p *inlineParser) underscore(i int) int {
	s := p.s
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	next, _ := utf8.DecodeRuneInString(s[i+1:])
	if i > 0 && !boundary(prev) || next == utf8.RuneError || unicode.IsSpace(next) || next == '_' {
		return p.literal(i, 1)
	}
	for j := i + 1; j < len(s); j++ {
		if s[j] == '\n' {
			break
		}
		if s[j] != '_' || escaped(s, j) {
			continue
		}
		before, _ := utf8.DecodeLastRuneInString(s[:j])
		after, _ := utf8.DecodeRuneInString(s[j+1:])
		if unicode.IsSpace(before) || !boundary(after) || after == '_' {
			continue
		}
		p.emit(Inline{Kind: Italic, Text: s[i+1 : j]})
		return j + 1 - i
	}
	return p.literal(i, 1)
}

func isWebURL(u string) bool {
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(u, scheme) && len(u) > len(scheme) {
			return true
		}
	}
	return false
}

// link: "[text](http(s)://...)".
func (p *inlineParser) link(i int) int {
	s := p.s
	j := strings.IndexByte(s[i+1:], ']')
	if j < 0 {
		return p.literal(i, 1)
	}
	j += i + 1
	text := s[i+1 : j]
	if text == "" || strings.ContainsAny(text, "[\n") || !strings.HasPrefix(s[j+1:], "(") {
		return p.literal(i, 1)
	}
	k := strings.IndexByte(s[j+2:], ')')
	if k < 0 {
		return p.literal(i, 1)
	}
	url := s[j+2 : j+2+k]
	if !isWebURL(url) || strings.IndexFunc(url, unicode.IsSpace) >= 0 || !SafeURL(url) {
		return p.literal(i, 1)
	}
	p.emit(Inline{Kind: Link, Text: text, URL: url})
	return j + 2 + k + 1 - i
}

// unsafeURLRune reports a control rune (C0, DEL, C1) or an invalid UTF-8
// byte: either could end or forge a terminal escape sequence (OSC 8 link
// target, CA 10.2 hostile bodies), so URLs never contain one.
func unsafeURLRune(r rune, size int) bool {
	return unicode.IsControl(r) || r == utf8.RuneError && size <= 1
}

// SafeURL reports whether u is valid UTF-8 without control runes, i.e. safe
// to embed in an OSC 8 hyperlink or hand to a link opener.
func SafeURL(u string) bool {
	for i := 0; i < len(u); {
		r, size := utf8.DecodeRuneInString(u[i:])
		if unsafeURLRune(r, size) {
			return false
		}
		i += size
	}
	return true
}

const urlTrailing = ".,)!?:;"

// bareURL: "http(s)://..." up to visible whitespace, minus trailing
// punctuation. A bare URL is its own link text, so a run that hits a rune the
// screen does not show as a break (a control other than tab/newline, invalid
// UTF-8, U+2028/U+2029) is plain text as a whole (pending-fixes 0-b ①):
// linking the clean prefix would make "https://a.com\u0085.b.com" read as one
// address while it opens https://a.com. A run holding a hidden rune (zero-width,
// bidi, CGJ, variation selector...) is plain text too, since the screen would
// read an address the link does not open (pending-fixes 1); VS15/VS16 only pick
// the presentation of the emoji before them, so emoji URLs stay links.
func (p *inlineParser) bareURL(i int) int {
	s := p.s
	if isASCIIWord(prevVisible(s[:i])) {
		return 0
	}
	end := i
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' && strings.HasPrefix(s[end+1:], "\n") {
			break
		}
		if unsafeURLRune(r, size) || r == '\u2028' || r == '\u2029' || hidden(r) && r != 0xfe0e && r != 0xfe0f {
			return p.literal(i, end-i)
		}
		if unicode.IsSpace(r) || strings.ContainsRune("<>\"`", r) {
			break
		}
		end += size
	}
	url := strings.TrimRight(s[i:end], urlTrailing)
	if !isWebURL(url) {
		return 0
	}
	p.emit(Inline{Kind: Link, Text: url, URL: url})
	return len(url)
}

func isASCIIWord(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// mention: (?<![A-Za-z0-9_])@([a-z0-9_]{3,20})(?![a-z0-9_]), case-insensitive.
// Go regexp has no lookbehind, so the previous rune is checked by hand. Both
// look-arounds skip hidden runes, as the screen does (pending-fixes 1).
func (p *inlineParser) mention(i int) int {
	s := p.s
	if isASCIIWord(prevVisible(s[:i])) {
		return 0
	}
	n := 0
	for i+1+n < len(s) && isASCIIWord(rune(s[i+1+n])) {
		n++
	}
	if end, cut := hiddenCut(s, i+1+n, isASCIIWord); cut {
		return p.literal(i, end-i)
	}
	if n < 3 || n > 20 {
		return 0
	}
	text := s[i : i+1+n]
	handle := strings.ToLower(text[1:])
	if handle == "here" {
		p.emit(Inline{Kind: Here, Text: text})
	} else {
		p.emit(Inline{Kind: Mention, Text: text, Handle: handle})
	}
	return 1 + n
}

func isTagRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// hashtag: (?<![\p{L}\p{N}_])#([\p{L}\p{N}_]{1,50}), lowercased. Both
// look-arounds skip hidden runes, as the screen does (pending-fixes 1).
func (p *inlineParser) hashtag(i int) int {
	s := p.s
	if isTagRune(prevVisible(s[:i])) {
		return 0
	}
	end, count := i+1, 0
	for end < len(s) && count < 50 {
		r, size := utf8.DecodeRuneInString(s[end:])
		if !isTagRune(r) {
			break
		}
		end += size
		count++
	}
	if end, cut := hiddenCut(s, end, isTagRune); cut {
		return p.literal(i, end-i)
	}
	if count == 0 {
		return 0
	}
	text := s[i:end]
	p.emit(Inline{Kind: Hashtag, Text: text, Handle: strings.ToLower(text[1:])})
	return end - i
}

// hidden reports a rune the screen never draws (textwidth.Invisible: format
// characters including ZWJ, line/paragraph separators, variation selectors,
// other default ignorables). Sanitize removes it after parsing, so a token
// boundary it makes would be invisible: "#공지\u200b사항" reads "#공지사항" and
// "@sam\u200b_admin" reads "@sam_admin" (pending-fixes 1 review ④).
func hidden(r rune) bool { return textwidth.Invisible(r) }

// prevVisible returns the last rune of s that is not hidden, or
// utf8.RuneError when there is none (start of text).
func prevVisible(s string) rune {
	for s != "" {
		r, n := utf8.DecodeLastRuneInString(s)
		if !hidden(r) {
			return r
		}
		s = s[:len(s)-n]
	}
	return utf8.RuneError
}

// hiddenCut reports whether a token scanned up to s[:end] was cut short only
// by hidden runes: s[end:] starts with a hidden run and the first visible rune
// after it would have continued the token (word). Then the screen shows one
// longer word, so the whole word (word and hidden runes from end on) is plain
// text; the returned index is where that word ends.
func hiddenCut(s string, end int, word func(rune) bool) (int, bool) {
	j := end
	for j < len(s) {
		r, n := utf8.DecodeRuneInString(s[j:])
		if !hidden(r) {
			break
		}
		j += n
	}
	if j == end || j == len(s) {
		return end, false
	}
	if r, _ := utf8.DecodeRuneInString(s[j:]); !word(r) {
		return end, false
	}
	for j < len(s) {
		r, n := utf8.DecodeRuneInString(s[j:])
		if !hidden(r) && !word(r) {
			break
		}
		j += n
	}
	return j, true
}
