package archdoc

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
)

func set(obj, field, v string) Change {
	return Change{Kind: ChangeSet, Obj: obj, Field: field, Value: json.RawMessage(v)}
}

func TestUndoSkipsOthersEdits(t *testing.T) {
	d := small()
	d.Nodes[2].CPUCores, d.Nodes[2].MemoryMiB = 2, 4096 // n_api
	before := d.ToObjects()
	mine := []Change{set("n:n_api", "cpu_cores", "4"), set("n:n_api", "memory_mib", "8192")}
	var u UndoLog
	u.Record(before, mine)
	current := before.Apply(mine).Apply([]Change{set("n:n_api", "memory_mib", "16384")}) // remote edit
	got := u.Undo(current)
	want := []Change{set("n:n_api", "cpu_cores", "2")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Undo = %v, want %v", got, want)
	}
	current = current.Apply(got)
	if got := u.Redo(current); !reflect.DeepEqual(got, []Change{set("n:n_api", "cpu_cores", "4")}) {
		t.Fatalf("Redo = %v", got)
	}
}

func TestUndoRedoAddDelete(t *testing.T) {
	d := small()
	before := d.ToObjects()
	n := Defaults(KindCache)
	n.ID = "n_new"
	fields := (&Design{Nodes: []Node{n}}).ToObjects()["n:n_new"]
	add := []Change{{Kind: ChangePut, Obj: "n:n_new", Value: marshalFields(fields)}}
	var u UndoLog
	if u.CanUndo() || u.CanRedo() {
		t.Fatal("empty log can undo")
	}
	u.Record(before, add)
	current := before.Apply(add)
	undo := u.Undo(current)
	if !reflect.DeepEqual(undo, []Change{{Kind: ChangeDel, Obj: "n:n_new"}}) {
		t.Fatalf("Undo = %v", undo)
	}
	current = current.Apply(undo)
	redo := u.Redo(current)
	if len(redo) != 1 || redo[0].Kind != ChangeRestore || redo[0].Obj != "n:n_new" {
		t.Fatalf("Redo = %v", redo)
	}
	if got := current.Apply(redo); !reflect.DeepEqual(got, before.Apply(add)) {
		t.Fatal("Redo did not restore the fields")
	}
	if u.Redo(current) != nil || u.CanRedo() {
		t.Fatal("redo past the end")
	}

	// Deleting a node: undo restores it with its fields.
	var del UndoLog
	rm := []Change{{Kind: ChangeDel, Obj: "n:n_db"}, {Kind: ChangeDel, Obj: "e:e_3"}}
	del.Record(before, rm)
	cur := before.Apply(rm)
	if got := cur.Apply(del.Undo(cur)); !reflect.DeepEqual(got, before) {
		t.Fatal("undo of delete did not bring the objects back")
	}

	// A new record after an undo drops the redo branch.
	u.Undo(current.Apply(redo))
	u.Record(before, []Change{set("n:n_api", "retries", "3")})
	if u.CanRedo() {
		t.Fatal("redo survived a new record")
	}

	// Cap 200: the oldest entry is dropped.
	var c UndoLog
	o := before
	for i := 0; i <= 200; i++ {
		ch := []Change{set("n:n_api", "timeout_ms", strconv.Itoa(1000+i))}
		c.Record(o, ch)
		o = o.Apply(ch)
	}
	steps := 0
	for c.CanUndo() {
		c.Undo(o)
		steps++
	}
	if steps != 200 {
		t.Fatalf("undo steps = %d, want 200", steps)
	}
	if c.Undo(o) != nil {
		t.Fatal("undo past the start")
	}
	// No-op records are not kept.
	var n0 UndoLog
	n0.Record(before, []Change{set("n:n_api", "retries", "0")})
	if n0.CanUndo() {
		t.Fatal("no-op change recorded")
	}
}
