package archdoc

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestDiffApplyReachesTarget(t *testing.T) {
	var all []Objects
	for _, b := range fixtures(t) {
		all = append(all, mustDecode(t, b).ToObjects())
	}
	all = append(all, small().ToObjects(), Objects{})
	for i, a := range all {
		for j, b := range all {
			ch := DiffObjects(a, b)
			if got := a.Apply(ch); !reflect.DeepEqual(got, b) {
				t.Fatalf("pair %d→%d: Apply(Diff) != target", i, j)
			}
			if !slices.IsSortedFunc(ch, func(x, y Change) int {
				if x.Obj != y.Obj {
					return compareStr(x.Obj, y.Obj)
				}
				return compareStr(x.Field, y.Field)
			}) {
				t.Fatalf("pair %d→%d: changes not sorted", i, j)
			}
			if i == j && len(ch) != 0 {
				t.Fatalf("diff of %d with itself = %v", i, ch)
			}
		}
	}
	// Apply works on a copy.
	a := small().ToObjects()
	before := a["n:n_api"]["cpu_cores"]
	_ = a.Apply([]Change{{Kind: ChangeSet, Obj: "n:n_api", Field: "cpu_cores", Value: raw("8")}})
	if string(a["n:n_api"]["cpu_cores"]) != string(before) {
		t.Fatal("Apply mutated its receiver")
	}
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func TestApplyDropsSetOnDeleted(t *testing.T) {
	o := small().ToObjects()
	got := o.Apply([]Change{
		{Kind: ChangeDel, Obj: "n:n_api"},
		{Kind: ChangeSet, Obj: "n:n_api", Field: "cpu_cores", Value: raw("8")},
		{Kind: ChangeSet, Obj: "n:nobody", Field: "cpu_cores", Value: raw("8")},
	})
	if _, ok := got["n:n_api"]; ok {
		t.Fatalf("deleted object came back: %v", got["n:n_api"])
	}
	if _, ok := got["n:nobody"]; ok {
		t.Fatal("Set created an object")
	}
	// Restore brings the fields back; Put on a live object merges.
	fields, _ := json.Marshal(o["n:n_api"])
	got = got.Apply([]Change{
		{Kind: ChangeRestore, Obj: "n:n_api", Value: fields},
		{Kind: ChangePut, Obj: "n:n_api", Value: raw(`{"cpu_cores":16}`)},
	})
	if string(got["n:n_api"]["cpu_cores"]) != "16" || string(got["n:n_api"]["kind"]) != `"api"` {
		t.Fatalf("restored = %v", got["n:n_api"])
	}
}

func TestSummarize(t *testing.T) {
	a := small()
	a.Nodes[2].CPUCores = 2
	b := small()
	b.Nodes[2].CPUCores = 8
	added := Defaults(KindCache)
	added.ID = "n_cache"
	b.Nodes = append(b.Nodes, added)
	b.Edges = slices.DeleteFunc(b.Edges, func(e Edge) bool { return e.ID == "e_3" })
	b.Faults = nil // f_1 targets n_api; drop it too
	got := Summarize(a, b)
	want := DesignDiff{
		Added:   []string{"n:n_cache"},
		Removed: []string{"e:e_3", "f:f_1"},
		Changed: []FieldChange{{Obj: "n:n_api", Field: "cpu_cores", From: raw("2"), To: raw("8")}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Summarize =\n%+v\nwant\n%+v", got, want)
	}
	if d := Summarize(a, a); len(d.Added)+len(d.Removed)+len(d.Changed) != 0 {
		t.Fatalf("self diff = %+v", d)
	}
}
