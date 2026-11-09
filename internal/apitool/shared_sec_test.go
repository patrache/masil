package apitool

import (
	"encoding/json"
	"testing"

	"github.com/patrache/masil/internal/collab"
)

// A peer's unknown body kind (the server refuses one; an old or forged
// document may still hold it) reads as no body; the method is kept as
// written (Prepare refuses a method that is not a token, the screen
// sanitizes it).
func TestCollectionFromDocUnknownBodyKind(t *testing.T) {
	objs := []collab.Object{
		{ID: "col", Type: ObjCol, Fields: map[string]json.RawMessage{"name": mustRaw("c")}},
		{ID: "req:a", Type: ObjRequest, Fields: map[string]json.RawMessage{"name": mustRaw("a"), "method": mustRaw("GET\x1b]0;x\a"), "body_kind": mustRaw("kind\x1b]52;c;eA==\a")}},
		{ID: "req:b", Type: ObjRequest, Fields: map[string]json.RawMessage{"name": mustRaw("b"), "method": mustRaw("POST"), "body_kind": mustRaw("json")}},
	}
	c, err := CollectionFromDoc(collab.Snapshot{Objects: objs})
	if err != nil {
		t.Fatal(err)
	}
	if k := c.Requests["a"].Body.Kind; k != BodyNone {
		t.Fatalf("unknown body kind read as %q, want none", k)
	}
	if k := c.Requests["b"].Body.Kind; k != BodyJSON {
		t.Fatalf("json body kind read as %q", k)
	}
	if _, err := Prepare(c.Requests["a"], "", &Scope{}, Dyn{}); err == nil {
		t.Fatal("a method with controls was prepared")
	}
}
