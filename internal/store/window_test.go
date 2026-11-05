package store

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

type item struct {
	ID string
	At time.Time
	V  string
}

func newItemWindow() *Window[item] {
	return NewWindow(func(i item) string { return i.ID }, func(i item) time.Time { return i.At })
}

func ids(w *Window[item]) []string {
	var out []string
	for _, it := range w.Items() {
		out = append(out, it.ID)
	}
	return out
}

func TestWindow(t *testing.T) {
	w := newItemWindow()
	if w.Len() != 0 || len(w.Items()) != 0 || w.Covers("A") || w.OverlapAnchor(5*time.Second) != "" {
		t.Fatal("empty window")
	}

	w.Upsert(item{ID: "03"}, item{ID: "01"}, item{ID: "05"})
	w.Upsert(item{ID: "02"})
	if got := ids(w); !reflect.DeepEqual(got, []string{"01", "02", "03", "05"}) {
		t.Fatalf("Upsert order = %v", got)
	}
	w.Upsert(item{ID: "03", V: "new"})
	if v, ok := w.Get("03"); !ok || v.V != "new" || w.Len() != 4 {
		t.Errorf("Upsert replace: %+v %v len %d", v, ok, w.Len())
	}
	if _, ok := w.Get("04"); ok {
		t.Error("Get missing")
	}

	if !w.Update("02", func(i *item) { i.V = "upd" }) {
		t.Error("Update existing returned false")
	}
	if v, _ := w.Get("02"); v.V != "upd" {
		t.Errorf("Update = %+v", v)
	}
	if w.Update("99", func(*item) {}) {
		t.Error("Update missing returned true")
	}
	if !w.Remove("01") || w.Remove("01") {
		t.Error("Remove")
	}
	if got := ids(w); !reflect.DeepEqual(got, []string{"02", "03", "05"}) {
		t.Errorf("after Remove = %v", got)
	}

	// Items is a copy.
	its := w.Items()
	its[0].V = "mutated"
	if v, _ := w.Get("02"); v.V == "mutated" {
		t.Error("Items must return a copy")
	}

	w.Replace([]item{{ID: "20"}, {ID: "10"}, {ID: "20", V: "dup"}}, true, false)
	if got := ids(w); !reflect.DeepEqual(got, []string{"10", "20"}) {
		t.Errorf("Replace = %v", got)
	}
	if !w.HasMoreBefore() || w.HasMoreAfter() {
		t.Errorf("Replace flags %v %v", w.HasMoreBefore(), w.HasMoreAfter())
	}

	w.PrependOlder([]item{{ID: "05"}, {ID: "10", V: "again"}}, false)
	if got := ids(w); !reflect.DeepEqual(got, []string{"05", "10", "20"}) {
		t.Errorf("PrependOlder = %v", got)
	}
	if w.HasMoreBefore() {
		t.Error("PrependOlder flag")
	}
	w.AppendNewer([]item{{ID: "20"}, {ID: "30"}}, true)
	if got := ids(w); !reflect.DeepEqual(got, []string{"05", "10", "20", "30"}) {
		t.Errorf("AppendNewer = %v", got)
	}
	if !w.HasMoreAfter() {
		t.Error("AppendNewer flag")
	}

	// Covers: first <= id <= last; beyond last only when !hasMoreAfter.
	for id, want := range map[string]bool{"04": false, "05": true, "15": true, "30": true, "31": false} {
		if got := w.Covers(id); got != want {
			t.Errorf("Covers(%s) hasMoreAfter=true = %v, want %v", id, got, want)
		}
	}
	w.AppendNewer(nil, false)
	if !w.Covers("31") || w.Covers("04") {
		t.Error("Covers with hasMoreAfter=false")
	}

	w.Clear()
	if w.Len() != 0 || w.HasMoreBefore() || w.HasMoreAfter() || w.Covers("10") {
		t.Error("Clear")
	}

	// OverlapAnchor.
	base := time.Date(2026, 11, 5, 11, 0, 0, 0, time.UTC)
	w.Replace([]item{
		{ID: "A", At: base},
		{ID: "B", At: base.Add(1 * time.Second)},
		{ID: "C", At: base.Add(2 * time.Second)},
		{ID: "D", At: base.Add(8 * time.Second)},
	}, false, false)
	if got := w.OverlapAnchor(5 * time.Second); got != "C" {
		t.Errorf("OverlapAnchor = %q, want C", got)
	}
	w.Replace([]item{{ID: "A", At: base}}, false, false)
	if got := w.OverlapAnchor(5 * time.Second); got != "" {
		t.Errorf("OverlapAnchor single = %q", got)
	}
}

func TestRing(t *testing.T) {
	r := NewRing[int](50)
	if len(r.Items()) != 0 {
		t.Fatal("empty ring")
	}
	for i := 1; i <= 60; i++ {
		r.Push(i)
	}
	got := r.Items()
	if len(got) != 50 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0] != 60 || got[49] != 11 {
		t.Errorf("order: first %d last %d", got[0], got[49])
	}
	got[0] = -1
	if r.Items()[0] != 60 {
		t.Error("Items must return a copy")
	}

	small := NewRing[string](3)
	for i := 0; i < 2; i++ {
		small.Push(strconv.Itoa(i))
	}
	if !reflect.DeepEqual(small.Items(), []string{"1", "0"}) {
		t.Errorf("partial ring = %v", small.Items())
	}
}
