package codeassist

import "strings"

// MaxMenuItems caps the menu's items (the language server's and the words).
const MaxMenuItems = 200

// Item is one completion menu entry: a word from the buffers or a language
// server's item (v0.1.3-1).
type Item struct {
	Label  string // what the menu shows
	Detail string // shown after the label: a server item's type or kind ("" for a word)
	Text   string // what accepting inserts
	Filter string // what typing on is matched against ("" = Label)
	// Edit: accepting replaces from rune column From of the cursor's line
	// up to After runes past the cursor (a server's textEdit, which may
	// start before the typed word, e.g. at a "."); otherwise the word
	// under the cursor is replaced.
	Edit        bool
	From, After int
	Server      bool // from the language server
}

// Merge is the menu: the server's items first, in its order, then the words
// it does not already offer (by text or label), at most MaxMenuItems.
func Merge(server []Item, words []string) []Item {
	out := make([]Item, 0, min(len(server)+len(words), MaxMenuItems))
	seen := map[string]bool{}
	for _, it := range server {
		if len(out) == MaxMenuItems {
			return out
		}
		out = append(out, it)
		seen[it.Text], seen[it.Label] = true, true
	}
	for _, w := range words {
		if len(out) == MaxMenuItems {
			break
		}
		if !seen[w] {
			out = append(out, Item{Label: w, Text: w})
			seen[w] = true
		}
	}
	return out
}

// FilterPrefix keeps the items whose Filter (Label when empty) starts with
// prefix, ignoring case (server items asked for a shorter prefix).
func FilterPrefix(items []Item, prefix string) []Item {
	if prefix == "" {
		return items
	}
	lp := strings.ToLower(prefix)
	var out []Item
	for _, it := range items {
		f := it.Filter
		if f == "" {
			f = it.Label
		}
		if strings.HasPrefix(strings.ToLower(f), lp) {
			out = append(out, it)
		}
	}
	return out
}

// Span is the rune range [from, to) of line that accepting it with the
// cursor at col replaces with Text.
func (it Item) Span(line []rune, col int) (from, to int) {
	col = max(0, min(col, len(line)))
	if it.Edit {
		return max(0, min(it.From, col)), max(col, min(col+it.After, len(line)))
	}
	prefix := Prefix(line, col)
	from, to = col-len([]rune(prefix)), col
	if prefix == "" { // a digit-led run is not replaced, only appended to
		return col, col
	}
	for to < len(line) && IsWordRune(line[to]) {
		to++
	}
	return from, to
}
