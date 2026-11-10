// Package ot is rune-based text operational transformation for workspace
// co-editing (spec B7.2, docs/contract/workspace.md 7): operations,
// transform/compose/invert, the owner-side Sequencer, the client state
// machine, own-edit undo, diff and the three-way disk merge.
//
// Lengths and positions count runes, never bytes or graphemes.
package ot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	// ErrBaseLen: the operation does not fit the document (or the other operation).
	ErrBaseLen = errors.New("ot: operation base length mismatch")
	// ErrInvalid: a component is malformed (negative, empty or more than one field).
	ErrInvalid = errors.New("ot: invalid operation component")
)

// MaxLen bounds every count and both length sums of an operation (runes).
// Far above any openable document (2 MiB); it keeps participant-supplied
// counts from wrapping int arithmetic.
const MaxLen = 1 << 30

// Component is exactly one of: keep Retain runes, remove Delete runes, or
// add Insert.
type Component struct {
	Retain, Delete int
	Insert         string
}

func (c Component) isRetain() bool { return c.Retain > 0 }
func (c Component) isDelete() bool { return c.Delete > 0 }
func (c Component) isInsert() bool { return c.Insert != "" }

func (c Component) valid() bool {
	n := 0
	if c.Retain != 0 {
		if c.Retain < 0 {
			return false
		}
		n++
	}
	if c.Delete != 0 {
		if c.Delete < 0 {
			return false
		}
		n++
	}
	if c.Insert != "" {
		n++
	}
	return n == 1
}

// Op is a sequence of components spanning the whole document. Its JSON form
// is the ot.js array: positive int = retain, negative = delete, string = insert.
type Op []Component

func (o Op) validate() error {
	base, target := 0, 0
	for _, c := range o {
		if !c.valid() || c.Retain > MaxLen || c.Delete > MaxLen {
			return ErrInvalid
		}
		base += c.Retain + c.Delete
		target += c.Retain + utf8.RuneCountInString(c.Insert)
		if base > MaxLen || target > MaxLen {
			return ErrInvalid
		}
	}
	return nil
}

// BaseLen is the document length (runes) the operation applies to.
func (o Op) BaseLen() int {
	n := 0
	for _, c := range o {
		n += c.Retain + c.Delete
	}
	return n
}

// TargetLen is the document length after applying the operation.
func (o Op) TargetLen() int {
	n := 0
	for _, c := range o {
		n += c.Retain + utf8.RuneCountInString(c.Insert)
	}
	return n
}

// IsNoop reports whether the operation changes nothing.
func (o Op) IsNoop() bool {
	for _, c := range o {
		if c.Delete != 0 || c.Insert != "" {
			return false
		}
	}
	return true
}

// Apply returns a new document; doc is not modified.
func (o Op) Apply(doc []rune) ([]rune, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	if o.BaseLen() != len(doc) {
		return nil, ErrBaseLen
	}
	out := make([]rune, 0, o.TargetLen())
	pos := 0
	for _, c := range o {
		switch {
		case c.isRetain():
			if c.Retain > len(doc)-pos {
				return nil, ErrBaseLen
			}
			out = append(out, doc[pos:pos+c.Retain]...)
			pos += c.Retain
		case c.isDelete():
			if c.Delete > len(doc)-pos {
				return nil, ErrBaseLen
			}
			pos += c.Delete
		default:
			out = append(out, []rune(c.Insert)...)
		}
	}
	return out, nil
}

// Invert returns the operation that undoes o on the document o produced
// from doc. doc must be o's base document.
func (o Op) Invert(doc []rune) Op {
	var b builder
	pos := 0
	for _, c := range o {
		switch {
		case c.isRetain():
			b.retain(c.Retain)
			pos += c.Retain
		case c.isDelete():
			end := min(pos+c.Delete, len(doc))
			b.insert(string(doc[min(pos, end):end]))
			pos += c.Delete
		case c.isInsert():
			b.delete(utf8.RuneCountInString(c.Insert))
		}
	}
	return b.result()
}

// Insert builds "insert s at rune index at" over a baseLen-rune document.
func Insert(at int, s string, baseLen int) Op {
	var b builder
	b.retain(at)
	b.insert(s)
	b.retain(baseLen - at)
	return b.result()
}

// Delete builds "delete n runes from at" over a baseLen-rune document.
func Delete(at, n, baseLen int) Op {
	var b builder
	b.retain(at)
	b.delete(n)
	b.retain(baseLen - at - n)
	return b.result()
}

// Noop is the identity operation over an n-rune document.
func Noop(n int) Op {
	var b builder
	b.retain(n)
	return b.result()
}

// TransformIndex moves a cursor index over o. Inserts before the index push
// it right; an insert exactly at the index does not (the other person's text
// lands after the cursor); deletes covering it pull it to the deletion point.
func TransformIndex(o Op, idx int) int {
	pos, out := 0, idx
	for _, c := range o {
		switch {
		case c.isRetain():
			pos += c.Retain
		case c.isInsert():
			if pos < idx {
				out += utf8.RuneCountInString(c.Insert)
			}
		case c.isDelete():
			end := pos + c.Delete
			if idx >= end {
				out -= c.Delete
			} else if idx > pos {
				out -= idx - pos
			}
			pos = end
		}
	}
	return out
}

// builder appends components in canonical form (ot.js TextOperation):
// no empty components, adjacent same kinds merged, insert before delete.
//
// Between two retains the canonical form is one insert (all inserted text,
// in order) followed by one delete (the sum). That trailing run is gathered
// in ins/del and written out once, when a retain arrives or result is read:
// merging with Insert += s would copy the growing text on every insert
// (quadratic in one participant op; v0.1.3 security (a)).
type builder struct {
	ops Op
	ins strings.Builder
	del int
}

// flush writes the pending insert/delete run into ops.
func (b *builder) flush() {
	if b.ins.Len() > 0 {
		b.ops = append(b.ops, Component{Insert: b.ins.String()})
		b.ins = strings.Builder{}
	}
	if b.del > 0 {
		b.ops = append(b.ops, Component{Delete: b.del})
		b.del = 0
	}
}

func (b *builder) retain(n int) {
	if n <= 0 {
		return
	}
	b.flush()
	if l := len(b.ops); l > 0 && b.ops[l-1].isRetain() {
		b.ops[l-1].Retain += n
		return
	}
	b.ops = append(b.ops, Component{Retain: n})
}

func (b *builder) delete(n int) {
	if n > 0 {
		b.del += n
	}
}

func (b *builder) insert(s string) {
	b.ins.WriteString(s)
}

// result is the built operation. The builder may be used further.
func (b *builder) result() Op {
	b.flush()
	return b.ops
}

// MarshalJSON writes the ot.js array form; a nil Op is [].
func (o Op) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, c := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		switch {
		case c.Retain != 0:
			buf.WriteString(strconv.Itoa(c.Retain))
		case c.Delete != 0:
			buf.WriteString(strconv.Itoa(-c.Delete))
		default:
			s, err := json.Marshal(c.Insert)
			if err != nil {
				return nil, err
			}
			buf.Write(s)
		}
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads the ot.js array form and normalizes it (drops 0 and
// "", merges neighbours of the same kind).
func (o *Op) UnmarshalJSON(data []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("ot: %w", err)
	}
	if items == nil && !bytes.Equal(bytes.TrimSpace(data), []byte("[]")) {
		return fmt.Errorf("ot: operation must be an array")
	}
	var b builder
	for _, it := range items {
		it = bytes.TrimSpace(it)
		if len(it) == 0 {
			return ErrInvalid
		}
		if it[0] == '"' {
			var s string
			if err := json.Unmarshal(it, &s); err != nil {
				return fmt.Errorf("ot: %w", err)
			}
			b.insert(s)
			continue
		}
		n, err := strconv.Atoi(string(it))
		if err != nil || n > MaxLen || n < -MaxLen {
			return fmt.Errorf("%w: %s", ErrInvalid, it)
		}
		switch {
		case n > 0:
			b.retain(n)
		case n < 0:
			if b.del > MaxLen {
				return fmt.Errorf("%w: delete run too long", ErrInvalid)
			}
			b.delete(-n)
		}
	}
	*o = b.result()
	return nil
}
