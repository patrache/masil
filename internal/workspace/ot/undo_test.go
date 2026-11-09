package ot

import "testing"

type undoDoc struct {
	t    *testing.T
	text []rune
	u    Undo
}

func (d *undoDoc) local(op Op) {
	d.u.Record(op, d.text)
	d.text = mustApply(d.t, d.text, op)
}

func (d *undoDoc) remote(op Op) {
	d.u.Remote(op)
	d.text = mustApply(d.t, d.text, op)
}

func (d *undoDoc) undo() bool {
	op, ok := d.u.Undo()
	if ok {
		d.text = mustApply(d.t, d.text, op)
	}
	return ok
}

func (d *undoDoc) redo() bool {
	op, ok := d.u.Redo()
	if ok {
		d.text = mustApply(d.t, d.text, op)
	}
	return ok
}

func TestUndoOnlyOwn(t *testing.T) {
	d := &undoDoc{t: t}
	d.local(Insert(0, "abc", 0))
	d.remote(Insert(0, "X", 3))
	if string(d.text) != "Xabc" {
		t.Fatal(string(d.text))
	}
	if !d.undo() || string(d.text) != "X" {
		t.Fatalf("undo: %q", string(d.text))
	}
	if !d.redo() || string(d.text) != "Xabc" {
		t.Fatalf("redo: %q", string(d.text))
	}
	if d.redo() {
		t.Fatal("nothing left to redo")
	}

	e := &undoDoc{t: t}
	e.local(Insert(0, "abc", 0))
	e.remote(Insert(0, "X", 3))
	e.remote(Delete(2, 1, 4)) // someone deletes the "b"
	if string(e.text) != "Xac" {
		t.Fatal(string(e.text))
	}
	if !e.undo() || string(e.text) != "X" {
		t.Fatalf("undo after remote delete: %q", string(e.text))
	}
	if e.undo() {
		t.Fatal("undid someone else's edit")
	}
}

func TestUndoSkipsFullyErased(t *testing.T) {
	d := &undoDoc{t: t}
	d.local(Insert(0, "a", 0))
	d.local(Insert(1, "b", 1))
	d.remote(Delete(1, 1, 2)) // our "b" is gone
	if !d.undo() || string(d.text) != "" {
		t.Fatalf("undo: %q", string(d.text))
	}
	if d.undo() {
		t.Fatal("stack should be empty")
	}
}

func TestUndoNewEditClearsRedo(t *testing.T) {
	d := &undoDoc{t: t}
	d.local(Insert(0, "a", 0))
	d.undo()
	d.local(Insert(0, "z", 0))
	if d.redo() {
		t.Fatal("redo after a new edit")
	}
	d.remote(Insert(1, "Q", 1))
	if !d.undo() || string(d.text) != "Q" {
		t.Fatalf("%q", string(d.text))
	}
}
