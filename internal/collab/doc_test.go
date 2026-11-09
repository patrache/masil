package collab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// vector is one entry of docs/contract/collab-vectors.json (collab.md 8).
type vector struct {
	Name    string          `json:"name"`
	Start   json.RawMessage `json:"start"`
	Batches []struct {
		Seq    *int64          `json:"seq"`
		Ops    json.RawMessage `json:"ops"`
		Reject string          `json:"reject"`
	} `json:"batches"`
	Want json.RawMessage `json:"want"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "collab-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vs []vector
	if err := json.Unmarshal(b, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) < 10 {
		t.Fatalf("only %d vectors", len(vs))
	}
	return vs
}

// jsonSame compares two JSON documents by meaning (key order free).
func jsonSame(t *testing.T, got, want []byte) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("got is not JSON: %v: %s", err, got)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatalf("want is not JSON: %v: %s", err, want)
	}
	return reflect.DeepEqual(a, b)
}

// TestDocVectors: the Go reducer lands on the server's canonical snapshot for
// every shared vector; reject batches fail the common shape check and leave
// the state alone.
func TestDocVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			start, err := DecodeSnapshot(v.Start)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			d := NewDoc(start)
			for i, b := range v.Batches {
				before, _ := json.Marshal(d.Snapshot())
				if b.Reject != "" {
					if b.Reject != "invalid" {
						t.Fatalf("batch %d: unknown reject %q", i, b.Reject)
					}
					if ops, err := ParseOps(b.Ops); err == nil {
						t.Fatalf("batch %d: ParseOps accepted %s as %+v", i, b.Ops, ops)
					}
					after, _ := json.Marshal(d.Snapshot())
					if string(before) != string(after) {
						t.Fatalf("batch %d: a rejected batch changed the state", i)
					}
					continue
				}
				if b.Seq == nil {
					t.Fatalf("batch %d: neither seq nor reject", i)
				}
				ops, err := parseOps(b.Ops, true)
				if err != nil {
					t.Fatalf("batch %d: %s: %v", i, b.Ops, err)
				}
				d.Apply(*b.Seq, ops)
			}
			got, _ := json.Marshal(d.Snapshot())
			if !jsonSame(t, got, v.Want) {
				t.Errorf("got  %s\nwant %s", got, v.Want)
			}
			// the canonical form round-trips
			again, err := DecodeSnapshot(got)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			b2, _ := json.Marshal(again)
			if string(b2) != string(got) {
				t.Errorf("canonical round trip:\n %s\n %s", got, b2)
			}
		})
	}
}

func TestDocChangesAndViews(t *testing.T) {
	d := NewDoc(Snapshot{Seq: 1, Objects: []Object{{ID: "n2", Type: "node", Fields: map[string]json.RawMessage{"x": json.RawMessage("1")}}}})
	ch := d.Apply(2, []Op{
		{Op: OpPut, ID: "n1", Type: "node", Fields: map[string]json.RawMessage{"b": json.RawMessage(`"B"`), "a": json.RawMessage(" 1 ")}},
		{Op: OpSet, ID: "zz", Field: "a", Value: json.RawMessage("1")}, // missing: no change
		{Op: OpPut, ID: "e1", Type: "edge", Fields: map[string]json.RawMessage{}},
		{Op: OpDel, ID: "n2"},
	})
	want := []Change{{ID: "n1"}, {ID: "n1", Field: "a"}, {ID: "n1", Field: "b"}, {ID: "e1"}, {ID: "n2", Removed: true}}
	if !reflect.DeepEqual(ch, want) {
		t.Errorf("changes = %+v, want %+v", ch, want)
	}
	if d.Seq() != 2 {
		t.Errorf("seq = %d", d.Seq())
	}
	if got := d.Objects("node"); len(got) != 1 || got[0].ID != "n1" || string(got[0].Fields["a"]) != "1" {
		t.Errorf("Objects(node) = %+v", got)
	}
	if got := d.Objects(""); len(got) != 2 || got[0].ID != "e1" || got[1].ID != "n1" {
		t.Errorf("Objects() = %+v", got)
	}
	if o, ok := d.Object("n2"); !ok || !o.Deleted || string(o.Fields["x"]) != "1" {
		t.Errorf("Object(n2) = %+v %v", o, ok)
	}
	c := d.Clone()
	c.Apply(3, []Op{{Op: OpSet, ID: "n1", Field: "a", Value: json.RawMessage("2")}})
	if string(d.Field("n1", "a")) != "1" || string(c.Field("n1", "a")) != "2" || d.Seq() != 2 || c.Seq() != 3 {
		t.Errorf("Clone is not independent: %s %s", d.Field("n1", "a"), c.Field("n1", "a"))
	}
	o, _ := d.Object("n1")
	o.Fields["a"] = json.RawMessage("9")
	if string(d.Field("n1", "a")) != "1" {
		t.Errorf("Object returned a shared map")
	}
}

func TestOpWire(t *testing.T) {
	for _, c := range []struct {
		op   Op
		want string
	}{
		{Op{Op: OpPut, ID: "n1", Type: "node", Fields: map[string]json.RawMessage{"x": json.RawMessage("1")}}, `{"op":"put","id":"n1","type":"node","fields":{"x":1}}`},
		{Op{Op: OpPut, ID: "n1", Type: "node"}, `{"op":"put","id":"n1","type":"node","fields":{}}`},
		{Op{Op: OpSet, ID: "n1", Field: "x", Value: json.RawMessage(`"a"`)}, `{"op":"set","id":"n1","field":"x","value":"a"}`},
		{Op{Op: OpSet, ID: "n1", Field: "x"}, `{"op":"set","id":"n1","field":"x","value":null}`},
		{Op{Op: OpDel, ID: "n1"}, `{"op":"del","id":"n1"}`},
		{Op{Op: OpUndel, ID: "n1"}, `{"op":"undel","id":"n1"}`},
		{Op{Op: OpReset, Snapshot: &Snapshot{Seq: 3, Objects: []Object{}}}, `{"op":"reset","snapshot":{"seq":3,"objects":[]}}`},
	} {
		b, err := json.Marshal(c.op)
		if err != nil || string(b) != c.want {
			t.Errorf("%+v -> %s (%v), want %s", c.op, b, err, c.want)
		}
		var back Op
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		b2, _ := json.Marshal(back)
		if string(b2) != c.want {
			t.Errorf("round trip %s -> %s", c.want, b2)
		}
	}
	if err := CheckOps([]Op{{Op: OpSet, ID: "n1", Field: "__proto__", Value: json.RawMessage("1")}}); err == nil {
		t.Errorf("CheckOps accepted a reserved field name")
	}
	if err := CheckOps([]Op{{Op: OpReset, Snapshot: &Snapshot{}}}); err == nil {
		t.Errorf("CheckOps accepted a client reset")
	}
	if err := CheckOps(nil); err == nil {
		t.Errorf("CheckOps accepted zero ops")
	}
	if err := CheckOps([]Op{{Op: OpDel, ID: "n1"}}); err != nil {
		t.Errorf("CheckOps(del) = %v", err)
	}
}

func TestDecodeSnapshotChecks(t *testing.T) {
	for _, bad := range []string{
		`[]`, `{"objects":{}}`, `{"seq":-1,"objects":[]}`,
		`{"objects":[{"id":"a","type":"t","fields":{}},{"id":"a","type":"t","fields":{}}]}`,
		`{"objects":[{"id":"__proto__","type":"t","fields":{}}]}`,
		`{"objects":[{"id":"a","type":"t","fields":{"한":1}}]}`,
		`{"objects":[{"id":"a","type":"t"}]}`,
		`{"objects":[{"id":"a","type":"t","fields":{},"deleted":"yes"}]}`,
	} {
		if _, err := DecodeSnapshot(json.RawMessage(bad)); err == nil {
			t.Errorf("DecodeSnapshot(%s) accepted", bad)
		}
	}
	s, err := DecodeSnapshot(json.RawMessage(`{"objects":[{"id":"b","type":"t","fields":{"v":[1, 2]}},{"id":"a","type":"t","fields":{},"deleted":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(s)
	if string(b) != `{"seq":0,"objects":[{"id":"a","type":"t","fields":{},"deleted":true},{"id":"b","type":"t","fields":{"v":[1,2]}}]}` {
		t.Errorf("canonical = %s", b)
	}
}
