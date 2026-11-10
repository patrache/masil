package ot

// UndoLimit caps each of the undo and redo stacks.
const UndoLimit = 1000

// Undo is an own-edits-only undo manager (spec B17, ot.js UndoManager):
// it stores inverses of this editor's edits and transforms them over other
// people's edits, so undo never touches someone else's text.
//
// Feed every own edit to Record (with the document before it) and every
// remote edit to Remote; apply what Undo/Redo return as a local edit
// without recording it. The zero value is ready to use.
type Undo struct {
	undo, redo []Op
	doc        []rune // current document as Undo has seen it
	known      bool   // doc is valid
}

// Record notes an own edit made on before.
func (u *Undo) Record(op Op, before []rune) {
	after, err := op.Apply(before)
	if err != nil {
		return
	}
	u.undo = pushLimited(u.undo, op.Invert(before))
	u.redo = nil
	u.doc, u.known = after, true
}

// Remote transforms the stacks over someone else's edit.
func (u *Undo) Remote(op Op) {
	if u.known {
		if d, err := op.Apply(u.doc); err == nil {
			u.doc = d
		} else {
			u.known = false
		}
	}
	u.undo = transformStack(u.undo, op)
	u.redo = transformStack(u.redo, op)
}

// Undo returns the operation that reverts the latest own edit still visible.
func (u *Undo) Undo() (Op, bool) {
	var op Op
	var ok bool
	u.undo, op, ok = u.pop(u.undo)
	if ok {
		u.redo = pushLimited(u.redo, u.advance(op))
	}
	return op, ok
}

// Redo re-applies the latest undone edit.
func (u *Undo) Redo() (Op, bool) {
	var op Op
	var ok bool
	u.redo, op, ok = u.pop(u.redo)
	if ok {
		u.undo = pushLimited(u.undo, u.advance(op))
	}
	return op, ok
}

// pop takes the top operation that still changes something.
func (u *Undo) pop(stack []Op) ([]Op, Op, bool) {
	for len(stack) > 0 {
		op := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !op.IsNoop() {
			return stack, op, true
		}
	}
	return stack, nil, false
}

// advance applies op to the tracked document and returns its inverse.
func (u *Undo) advance(op Op) Op {
	if !u.known {
		return nil
	}
	inv := op.Invert(u.doc)
	d, err := op.Apply(u.doc)
	if err != nil {
		u.known = false
		return nil
	}
	u.doc = d
	return inv
}

func pushLimited(stack []Op, op Op) []Op {
	if op == nil {
		return stack
	}
	stack = append(stack, op)
	if len(stack) > UndoLimit {
		stack = append([]Op(nil), stack[len(stack)-UndoLimit:]...)
	}
	return stack
}

// transformStack rewrites a stack (top = last, relative to the current
// document) over op, top down.
func transformStack(stack []Op, op Op) []Op {
	out := make([]Op, len(stack))
	for i := len(stack) - 1; i >= 0; i-- {
		s2, op2, err := Transform(stack[i], op)
		if err != nil {
			return nil // out of step: forget the history rather than corrupt text
		}
		out[i], op = s2, op2
	}
	return out
}
