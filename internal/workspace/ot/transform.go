package ot

import "unicode/utf8"

// iter walks an operation component by component, letting callers consume
// part of the current component.
//
// A partly consumed insert is kept as the runes still to go (ins): turning
// the rest of the string into runes on every step was quadratic when a long
// insert met many short components (v0.1.3 security (a)). While ins is in
// use cur.Insert still holds the whole original text; only isInsert reads it
// then.
type iter struct {
	ops Op
	i   int
	cur Component
	ok  bool
	ins []rune
}

func newIter(o Op) *iter {
	it := &iter{ops: o}
	it.next()
	return it
}

func (it *iter) next() {
	it.ins = nil
	if it.i < len(it.ops) {
		it.cur, it.ok = it.ops[it.i], true
		it.i++
		return
	}
	it.cur, it.ok = Component{}, false
}

// length of the current retain/delete in runes (inserts are consumed whole
// by transform, or split by compose via takeInsert).
func (it *iter) span() int {
	switch {
	case it.cur.isRetain():
		return it.cur.Retain
	case it.cur.isDelete():
		return it.cur.Delete
	default:
		return len(it.insRunes())
	}
}

// insRunes is the current insert's runes not yet consumed.
func (it *iter) insRunes() []rune {
	if it.ins == nil {
		it.ins = []rune(it.cur.Insert)
	}
	return it.ins
}

// consume n units of the current component.
func (it *iter) consume(n int) {
	switch {
	case it.cur.isRetain():
		it.cur.Retain -= n
		if it.cur.Retain == 0 {
			it.next()
		}
	case it.cur.isDelete():
		it.cur.Delete -= n
		if it.cur.Delete == 0 {
			it.next()
		}
	default:
		it.ins = it.insRunes()[n:]
		if len(it.ins) == 0 {
			it.next()
		}
	}
}

// takeInsert returns the first n runes of the current insert and consumes them.
func (it *iter) takeInsert(n int) string {
	s := string(it.insRunes()[:n])
	it.consume(n)
	return s
}

// Transform returns a2, b2 with apply(apply(d,a),b2) == apply(apply(d,b),a2).
// b is the operation that was sequenced first: when both insert at the same
// position, b's text comes first.
func Transform(a, b Op) (a2, b2 Op, err error) {
	if err := a.validate(); err != nil {
		return nil, nil, err
	}
	if err := b.validate(); err != nil {
		return nil, nil, err
	}
	if a.BaseLen() != b.BaseLen() {
		return nil, nil, ErrBaseLen
	}
	var ap, bp builder
	ia, ib := newIter(a), newIter(b)
	for ia.ok || ib.ok {
		if ib.ok && ib.cur.isInsert() {
			n := utf8.RuneCountInString(ib.cur.Insert)
			bp.insert(ib.cur.Insert)
			ap.retain(n)
			ib.next()
			continue
		}
		if ia.ok && ia.cur.isInsert() {
			n := utf8.RuneCountInString(ia.cur.Insert)
			ap.insert(ia.cur.Insert)
			bp.retain(n)
			ia.next()
			continue
		}
		if !ia.ok || !ib.ok {
			return nil, nil, ErrBaseLen
		}
		n := min(ia.span(), ib.span())
		switch {
		case ia.cur.isRetain() && ib.cur.isRetain():
			ap.retain(n)
			bp.retain(n)
		case ia.cur.isDelete() && ib.cur.isDelete():
			// both deleted the same runes
		case ia.cur.isDelete() && ib.cur.isRetain():
			ap.delete(n)
		case ia.cur.isRetain() && ib.cur.isDelete():
			bp.delete(n)
		}
		ia.consume(n)
		ib.consume(n)
	}
	return ap.result(), bp.result(), nil
}
