package apitool

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// maxSummaryBytes caps the text kept for one scalar (a 10MB string on one
// line is cut; the row only ever shows its start).
const maxSummaryBytes = 4096

// FoldState is the folded containers by JSON path ("$.items[0]"); paths
// survive a new response of the same shape (C3.7: the fold state is kept
// when the same request is sent again).
type FoldState map[string]bool

// Row is one visible line of the tree. Key is the shown key ("\"name\"",
// "[0]", "" for a scalar root); Summary the value ("\"Tom\"", "7", "{",
// "{…}" when folded, "{}" when empty); Count the children of a container;
// Indent = Depth × the indent Window was given.
type Row struct {
	Depth            int
	Key, Summary     string
	Path             string
	Foldable, Folded bool
	Count            int
	Indent           int
}

type tnode struct {
	parent int32
	end    int32 // one past the last descendant
	depth  int32 // root -1 when it is a container
	count  int32
	idx    int32 // array index, -1 for an object member
	kind   byte  // '{', '[' or 0 for a scalar
	rawKey string
	key    string // shown
	text   string // scalar summary
}

// Tree is a parsed JSON body; build it off the UI goroutine (BuildTree),
// then read it from one goroutine.
type Tree struct {
	nodes []tnode
	first int32 // first shown node (1 under a container root)

	once  sync.Once
	paths map[string]int32 // container path -> node
}

// BuildTree parses body into a tree with one token pass. It fails on
// anything that is not exactly one JSON value (the screen then shows text).
func BuildTree(body []byte) (*Tree, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	t := &Tree{}
	var stack []tframe
	pendingKey := ""
	done := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if done {
			return nil, errors.New("apitool: data after the JSON value")
		}
		if len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.obj && top.wantKey {
				if d, ok := tok.(json.Delim); ok && d == '}' {
					// closes below
				} else {
					k, _ := tok.(string)
					pendingKey = k
					top.wantKey = false
					continue
				}
			}
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n := &t.nodes[f.at]
			n.end = int32(len(t.nodes))
			n.count = f.n
			afterValue(stack)
			if len(stack) == 0 {
				done = true
			}
			continue
		}
		n := tnode{parent: -1, idx: -1, depth: int32(len(stack)) - 1}
		if len(stack) > 0 {
			p := &stack[len(stack)-1]
			n.parent = p.at
			if p.obj {
				n.rawKey = pendingKey
				n.key = `"` + textwidth.Sanitize(pendingKey) + `"`
			} else {
				n.idx = p.n
				n.key = "[" + strconv.Itoa(int(p.n)) + "]"
			}
			p.n++
		}
		if d, ok := tok.(json.Delim); ok {
			n.kind = byte(d)
			stack = append(stack, tframe{at: int32(len(t.nodes)), obj: d == '{', wantKey: d == '{'})
			t.nodes = append(t.nodes, n)
			continue
		}
		n.text = scalarSummary(tok)
		n.end = int32(len(t.nodes)) + 1
		t.nodes = append(t.nodes, n)
		afterValue(stack)
		if len(stack) == 0 {
			done = true
		}
	}
	if len(t.nodes) == 0 || len(stack) > 0 {
		return nil, errors.New("apitool: not a JSON value")
	}
	if t.nodes[0].kind != 0 {
		t.first = 1
	} else {
		t.nodes[0].depth = 0
	}
	return t, nil
}

// tframe is an open container while building.
type tframe struct {
	at      int32
	obj     bool
	n       int32
	wantKey bool
}

// afterValue: the next token in an object is a key again.
func afterValue(stack []tframe) {
	if len(stack) > 0 && stack[len(stack)-1].obj {
		stack[len(stack)-1].wantKey = true
	}
}

func scalarSummary(tok json.Token) string {
	switch v := tok.(type) {
	case string:
		s := v
		if len(s) > maxSummaryBytes {
			cut := maxSummaryBytes
			for cut > 0 && !utf8.RuneStart(s[cut]) {
				cut--
			}
			s = s[:cut] + "…"
		}
		return `"` + textwidth.Sanitize(s) + `"`
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return "null"
	}
	return ""
}

var identKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// path is the JSON path of node i (apitool.EvalPath syntax).
func (t *Tree) path(i int32) string {
	var parts []string
	for j := i; j >= 0 && t.nodes[j].parent >= 0; j = t.nodes[j].parent {
		n := &t.nodes[j]
		switch {
		case n.idx >= 0:
			parts = append(parts, "["+strconv.Itoa(int(n.idx))+"]")
		case identKey.MatchString(n.rawKey):
			parts = append(parts, "."+n.rawKey)
		case !strings.Contains(n.rawKey, "'"):
			parts = append(parts, "['"+n.rawKey+"']")
		default:
			parts = append(parts, `["`+n.rawKey+`"]`)
		}
	}
	slices.Reverse(parts)
	return "$" + strings.Join(parts, "")
}

// folded resolves f to the sorted node indexes of folded, non-empty
// containers present in this tree.
func (t *Tree) folded(f FoldState) []int32 {
	if len(f) == 0 {
		return nil
	}
	t.once.Do(func() {
		t.paths = map[string]int32{}
		for i := range t.nodes {
			if t.nodes[i].kind != 0 {
				t.paths[t.path(int32(i))] = int32(i)
			}
		}
	})
	var out []int32
	for p, on := range f {
		if i, ok := t.paths[p]; on && ok && t.nodes[i].count > 0 && i >= t.first {
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return out
}

// foldedWithin reports whether a folded index lies in (i, end).
func foldedWithin(folded []int32, i, end int32) bool {
	k, _ := slices.BinarySearch(folded, i+1)
	return k < len(folded) && folded[k] < end
}

// Count is the number of visible rows under f.
func (t *Tree) Count(f FoldState) int {
	n := len(t.nodes) - int(t.first)
	end := int32(-1)
	for _, i := range t.folded(f) {
		if i < end {
			continue // inside an already folded subtree
		}
		n -= int(t.nodes[i].end - i - 1)
		end = t.nodes[i].end
	}
	return n
}

// Window is the visible rows top..top+h under f. It walks only what it
// needs: unfolded subtrees above top are skipped whole.
func (t *Tree) Window(f FoldState, top, h, indent int) []Row {
	folded := t.folded(f)
	isFolded := func(i int32) bool {
		_, ok := slices.BinarySearch(folded, i)
		return ok
	}
	var out []Row
	row := 0
	i := t.first
	n := int32(len(t.nodes))
	for i < n && len(out) < h {
		nd := &t.nodes[i]
		fold := nd.kind != 0 && isFolded(i)
		if row < top && nd.kind != 0 && !fold && !foldedWithin(folded, i, nd.end) {
			if size := int(nd.end - i); row+size <= top {
				row += size
				i = nd.end
				continue
			}
		}
		if row >= top {
			out = append(out, t.row(i, fold, indent))
		}
		row++
		if fold {
			i = nd.end
		} else {
			i++
		}
	}
	return out
}

func (t *Tree) row(i int32, fold bool, indent int) Row {
	nd := &t.nodes[i]
	r := Row{Depth: int(nd.depth), Key: nd.key, Path: t.path(i), Count: int(nd.count), Indent: int(nd.depth) * indent}
	switch nd.kind {
	case 0:
		r.Summary = nd.text
	default:
		open, closer := string(nd.kind), "}"
		if nd.kind == '[' {
			closer = "]"
		}
		switch {
		case nd.count == 0:
			r.Summary = open + closer
		case fold:
			r.Summary = open + "…" + closer
			r.Foldable, r.Folded = true, true
		default:
			r.Summary = open
			r.Foldable = true
		}
	}
	return r
}

// RowText is a row as one line (not cut to a width): indent, fold mark,
// key and value, and the child count when folded.
func RowText(t *i18n.T, r Row) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", r.Indent))
	switch {
	case r.Folded:
		b.WriteString("▸ ")
	case r.Foldable:
		b.WriteString("▾ ")
	default:
		b.WriteString("  ")
	}
	if r.Key != "" {
		b.WriteString(r.Key + ": ")
	}
	b.WriteString(r.Summary)
	if r.Folded {
		b.WriteString(" " + t.Apitest.TreeCount(r.Count))
	}
	return b.String()
}
