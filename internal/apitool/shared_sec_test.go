package apitool

import (
	"encoding/json"
	"errors"
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

// folderStats counts the folders of f and the deepest nesting.
func folderStats(f *Folder, depth int) (n, deepest int) {
	deepest = depth
	for _, it := range f.Items {
		if it.Folder != nil {
			cn, cd := folderStats(it.Folder, depth+1)
			n += cn + 1
			deepest = max(deepest, cd)
		}
	}
	return n, deepest
}

// A peer's document with too many or too deeply nested folders reads
// within the caps: folders past MaxSharedFolders (by id) are dropped and
// their items go to the root; a folder past MaxSharedFolderDepth starts a
// new chain at the root. Every request stays reachable.
func TestCollectionFromDocFolderCaps(t *testing.T) {
	const n = 5_000
	objs := []collab.Object{{ID: "col", Type: ObjCol, Fields: map[string]json.RawMessage{"name": mustRaw("deep")}}}
	for i := range n {
		p := ""
		if i > 0 {
			p = "f" + itoa(i-1)
		}
		objs = append(objs, collab.Object{ID: "folder:f" + itoa(i), Type: ObjFolder, Fields: map[string]json.RawMessage{"name": mustRaw("x"), "parent": mustRaw(p)}})
	}
	for _, f := range []string{"f0", "f40", "f4999"} {
		objs = append(objs, collab.Object{ID: "req:in-" + f, Type: ObjRequest, Fields: map[string]json.RawMessage{"name": mustRaw(f), "folder": mustRaw(f)}})
	}
	c, err := CollectionFromDoc(collab.Snapshot{Objects: objs})
	if err != nil {
		t.Fatal(err)
	}
	folders, deepest := folderStats(&c.Root, 0)
	if folders > MaxSharedFolders || deepest > MaxSharedFolderDepth {
		t.Fatalf("read %d folders, %d deep (caps %d, %d)", folders, deepest, MaxSharedFolders, MaxSharedFolderDepth)
	}
	if ids := c.Root.RequestIDs(); len(ids) != 3 {
		t.Fatalf("reachable requests = %v", ids)
	}
}

// My own edits and uploads stay within the folder caps.
func TestSharedLimitsFolders(t *testing.T) {
	wide := &Collection{Requests: map[string]*Request{}}
	for i := range MaxSharedFolders + 1 {
		wide.Root.Items = append(wide.Root.Items, Node{Folder: &Folder{ID: "w" + itoa(i), Name: "w"}})
	}
	if err := CheckSharedLimits(wide); !errors.Is(err, ErrSharedTooManyFolders) {
		t.Fatalf("too many folders: %v", err)
	}
	deep := &Collection{Requests: map[string]*Request{}}
	f := &deep.Root
	for i := range MaxSharedFolderDepth + 1 {
		sub := &Folder{ID: "d" + itoa(i), Name: "d"}
		f.Items = append(f.Items, Node{Folder: sub})
		f = sub
	}
	if err := CheckSharedLimits(deep); !errors.Is(err, ErrSharedFoldersTooDeep) {
		t.Fatalf("too deep: %v", err)
	}
	ok := &Collection{Requests: map[string]*Request{}}
	f = &ok.Root
	for i := range MaxSharedFolderDepth {
		sub := &Folder{ID: "o" + itoa(i), Name: "o"}
		f.Items = append(f.Items, Node{Folder: sub})
		f = sub
	}
	if err := CheckSharedLimits(ok); err != nil {
		t.Fatalf("at the depth cap: %v", err)
	}
}
