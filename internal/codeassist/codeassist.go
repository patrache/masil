// Package codeassist is the built-in code editors' help (v0.1.3-1): syntax
// highlighting of the shown window with chroma, and completion candidates
// without an LSP — the language's keywords and builtins plus the words
// already in the open buffers, ranked by how the prefix matches and then by
// how often the word occurs. The editors (internal/ui/leet's goeditor and
// internal/ui/workspace/editor) own the menu and the keys.
package codeassist

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/alecthomas/chroma/v2/lexers"
)

// MinPrefix is how many word characters open the menu on their own.
const MinPrefix = 2

// MaxVisible is how many items the menu shows at once.
const MaxVisible = 8

// maxItems caps a candidate list (the menu scrolls through them).
const maxItems = 100

// FromLeet is the language of a LeetCode language slug ("golang" is "go";
// the others are their slug).
func FromLeet(slug string) string {
	if slug == "golang" {
		return "go"
	}
	return slug
}

// byExt maps the file extensions of the LeetCode languages.
var byExt = map[string]string{
	".go": "go", ".py": "python3", ".pyw": "python3", ".c": "c", ".h": "c",
	".cpp": "cpp", ".cc": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp",
	".java": "java", ".cs": "csharp", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".jsx": "javascript", ".ts": "typescript", ".tsx": "typescript", ".mts": "typescript",
	".php": "php", ".swift": "swift", ".kt": "kotlin", ".kts": "kotlin", ".rs": "rust",
	".rb": "ruby", ".scala": "scala", ".sc": "scala",
}

// FromFile is the language of a file name: a LeetCode language by its
// extension, else the name chroma matches it to (highlighting only, no
// keyword table), else "".
func FromFile(name string) string {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	if l, ok := byExt[strings.ToLower(path.Ext(base))]; ok {
		return l
	}
	if l := lexers.Match(base); l != nil {
		return strings.ToLower(l.Config().Name)
	}
	return ""
}

// IsWordRune reports whether r can be part of a word (an identifier).
func IsWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// IsWord reports whether s is a word: word runes, not starting with a digit.
func IsWord(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !IsWordRune(r) || (i == 0 && unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// Prefix is the word typed before column col of line ("" when the runes
// before col do not end in a word, or the word starts with a digit).
func Prefix(line []rune, col int) string {
	col = max(0, min(col, len(line)))
	i := col
	for i > 0 && IsWordRune(line[i-1]) {
		i--
	}
	p := string(line[i:col])
	if !IsWord(p) {
		return ""
	}
	return p
}

// WordAt is the whole word around column col (the prefix and the word
// runes after col).
func WordAt(line []rune, col int) string {
	col = max(0, min(col, len(line)))
	i, j := col, col
	for i > 0 && IsWordRune(line[i-1]) {
		i--
	}
	for j < len(line) && IsWordRune(line[j]) {
		j++
	}
	return string(line[i:j])
}

// CountWords adds the words of text to counts.
func CountWords(text []rune, counts map[string]int) {
	i := 0
	for i < len(text) {
		if !IsWordRune(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && IsWordRune(text[j]) {
			j++
		}
		if !unicode.IsDigit(text[i]) && j-i >= MinPrefix {
			counts[string(text[i:j])]++
		}
		i = j
	}
}

// Index counts a buffer's words line by line, so an edit only recounts the
// lines that changed.
type Index struct {
	lines  []string
	counts map[string]int
}

// Update makes the index count lines (the buffer now): the lines between
// the unchanged head and tail are taken out and the new ones counted.
func (x *Index) Update(lines []string) {
	if x.counts == nil {
		x.counts = map[string]int{}
	}
	old := x.lines
	head := 0
	for head < len(old) && head < len(lines) && old[head] == lines[head] {
		head++
	}
	tail := 0
	for tail < len(old)-head && tail < len(lines)-head && old[len(old)-1-tail] == lines[len(lines)-1-tail] {
		tail++
	}
	sub := map[string]int{}
	for _, l := range old[head : len(old)-tail] {
		CountWords([]rune(l), sub)
	}
	for w, n := range sub {
		if x.counts[w] -= n; x.counts[w] <= 0 {
			delete(x.counts, w)
		}
	}
	for _, l := range lines[head : len(lines)-tail] {
		CountWords([]rune(l), x.counts)
	}
	x.lines = slices.Clone(lines)
}

// Counts is the word counts (shared: do not change).
func (x *Index) Counts() map[string]int {
	if x.counts == nil {
		x.counts = map[string]int{}
	}
	return x.counts
}

// Complete is the candidates for prefix (the word typed before the cursor)
// in lang: keywords, builtins and the words of sources, best first. A word
// matching the prefix case for case comes before one matching it only
// ignoring case; then the more frequent (summed over sources) first, the
// shorter, a–z. current (the whole word under the cursor) is left out.
func Complete(lang, prefix, current string, sources ...map[string]int) []string {
	freq := map[string]int{}
	for _, k := range Keywords(lang) {
		freq[k] += 0
	}
	for _, src := range sources {
		for w, n := range src {
			if n > 0 {
				freq[w] += n
			}
		}
	}
	type cand struct {
		w     string
		exact bool
		n     int
	}
	lp := strings.ToLower(prefix)
	var cs []cand
	for w, n := range freq {
		if w == current || w == prefix {
			continue
		}
		switch {
		case strings.HasPrefix(w, prefix):
			cs = append(cs, cand{w, true, n})
		case strings.HasPrefix(strings.ToLower(w), lp):
			cs = append(cs, cand{w, false, n})
		}
	}
	slices.SortFunc(cs, func(a, b cand) int {
		if a.exact != b.exact {
			if a.exact {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.n, a.n), cmp.Compare(len(a.w), len(b.w)), strings.Compare(a.w, b.w))
	})
	if len(cs) > maxItems {
		cs = cs[:maxItems]
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.w
	}
	return out
}
