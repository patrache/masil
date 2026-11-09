package palette_test

import (
	"reflect"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

func TestRegisterDuplicatePanics(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	palette.ResetForTest()
	palette.Register(palette.Command{ID: "t.dup", Title: "dup"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate id")
		}
	}()
	palette.Register(palette.Command{ID: "t.dup", Title: "dup again"})
}

func TestLookupReturnsCopy(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	palette.ResetForTest()
	palette.Register(palette.Command{ID: "t.x", Title: "x", Keywords: []string{"a", "b"}, Group: palette.GroupCommand})
	got, ok := palette.Lookup("t.x")
	if !ok || got.ID != "t.x" || got.Title != "x" || got.Group != palette.GroupCommand || !reflect.DeepEqual(got.Keywords, []string{"a", "b"}) {
		t.Fatalf("Lookup(t.x) = %+v, %v", got, ok)
	}
	got.Keywords[0] = "mutated"
	got.Title = "mutated"
	again, ok := palette.Lookup("t.x")
	if !ok || again.Title != "x" || !reflect.DeepEqual(again.Keywords, []string{"a", "b"}) {
		t.Fatalf("Lookup must return a copy; got %+v", again)
	}
	none, ok := palette.Lookup("nope")
	if ok || !reflect.DeepEqual(none, palette.Command{}) {
		t.Fatalf("Lookup(nope) = %+v, %v; want zero, false", none, ok)
	}
}

type fakeSource struct{}

func (fakeSource) Items(*ui.Context, string) []palette.Item { return nil }

func TestSnapshotRestores(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	palette.ResetForTest()
	palette.Register(palette.Command{ID: "t.keep"})
	restore := palette.SnapshotForTest()
	palette.ResetForTest()
	palette.Register(palette.Command{ID: "t.temp"})
	palette.RegisterSource(fakeSource{})
	restore()
	if _, ok := palette.Lookup("t.keep"); !ok {
		t.Fatal("t.keep lost after restore")
	}
	if _, ok := palette.Lookup("t.temp"); ok {
		t.Fatal("t.temp survived restore")
	}
}
