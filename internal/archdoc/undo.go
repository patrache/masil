package archdoc

import (
	"encoding/json"
	"maps"
	"slices"
)

// UndoCap is how many of my own edit steps the undo log keeps.
const UndoCap = 200

// UndoLog records my own edits so they can be undone and redone without
// touching anyone else's work: a field whose current value is no longer the one
// I wrote is skipped. The zero value is ready to use.
type UndoLog struct {
	steps [][]undoObj
	next  int // steps[:next] can be undone, steps[next:] redone
}

type undoObj struct {
	key           string
	before, after map[string]json.RawMessage // nil = object absent
}

// Record notes one step: applied, as applied on top of before. A step that
// changes nothing is not kept. Recording drops anything that could be redone.
func (u *UndoLog) Record(before Objects, applied []Change) {
	after := before.Apply(applied)
	var keys []string
	for _, c := range applied {
		keys = append(keys, c.Obj)
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	var step []undoObj
	for _, k := range keys {
		b, a := before[k], after[k]
		if (b == nil) == (a == nil) && fieldsEqual(b, a) {
			continue
		}
		step = append(step, undoObj{key: k, before: maps.Clone(b), after: maps.Clone(a)})
	}
	if len(step) == 0 {
		return
	}
	u.steps = append(u.steps[:u.next], step)
	if len(u.steps) > UndoCap {
		u.steps = slices.Delete(u.steps, 0, len(u.steps)-UndoCap)
	}
	u.next = len(u.steps)
}

// CanUndo reports whether Undo has a step left.
func (u *UndoLog) CanUndo() bool { return u.next > 0 }

// CanRedo reports whether Redo has a step left.
func (u *UndoLog) CanRedo() bool { return u.next < len(u.steps) }

// Undo returns the changes that take back my last step, given the current
// state. It moves back one step even when every change is skipped; nil when
// there is nothing to undo.
func (u *UndoLog) Undo(current Objects) []Change {
	if !u.CanUndo() {
		return nil
	}
	u.next--
	return revert(u.steps[u.next], current, false)
}

// Redo re-applies the step Undo took back, with the same skipping rule.
func (u *UndoLog) Redo(current Objects) []Change {
	if !u.CanRedo() {
		return nil
	}
	u.next++
	return revert(u.steps[u.next-1], current, true)
}

func revert(step []undoObj, current Objects, redo bool) []Change {
	var out []Change
	for _, o := range step {
		mine, target := o.after, o.before // undo: I wrote after, go back to before
		if redo {
			mine, target = o.before, o.after
		}
		cur, live := current[o.key]
		switch {
		case mine != nil && target == nil:
			if live {
				out = append(out, Change{Kind: ChangeDel, Obj: o.key})
			}
		case mine == nil && target != nil:
			if !live {
				out = append(out, Change{Kind: ChangeRestore, Obj: o.key, Value: marshalFields(target)})
			}
		default:
			if !live {
				continue
			}
			for _, f := range union(mine, target) {
				if !rawEqual(mine[f], target[f]) && rawEqual(cur[f], mine[f]) {
					out = append(out, Change{Kind: ChangeSet, Obj: o.key, Field: f, Value: target[f]})
				}
			}
		}
	}
	return out
}

func fieldsEqual(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !rawEqual(v, w) {
			return false
		}
	}
	return true
}
