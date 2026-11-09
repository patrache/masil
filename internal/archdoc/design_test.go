package archdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func fixtures(t testing.TB) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "designs", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 3 {
		t.Fatalf("want at least 3 fixtures, got %d", len(paths))
	}
	out := map[string][]byte{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(p)] = b
	}
	return out
}

func mustDecode(t testing.TB, b []byte) *Design {
	t.Helper()
	d, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return d
}

func mustEncode(t testing.TB, d *Design) []byte {
	t.Helper()
	b, err := d.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return b
}

func issuesOf(t testing.TB, err error) []Issue {
	t.Helper()
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("err %T is not *InvalidError", err)
	}
	return ie.Issues
}

func hasIssue(is []Issue, path string, code IssueCode) bool {
	for _, i := range is {
		if i.Code == code && (path == "" || i.Path == path) {
			return true
		}
	}
	return false
}

// small builds a valid design: client → lb → api → db primary/replica inside region > vpc > az.
func small() *Design {
	d := New("small")
	d.Seed = 7
	region := Box{ID: "b_region", Kind: BoxRegion, Label: "seoul", RegionCode: "ap-northeast-2", Rect: [4]int{0, 0, 100, 40}}
	vpc := Box{ID: "b_vpc", Kind: BoxVPC, Label: "vpc", Rect: [4]int{2, 2, 90, 34}, Parent: "b_region"}
	az := Box{ID: "b_az", Kind: BoxAZ, Label: "az-a", Rect: [4]int{4, 4, 80, 30}, Parent: "b_vpc"}
	d.Boxes = []Box{vpc, region, az}
	mk := func(id string, k NodeKind, parent string) Node {
		n := Defaults(k)
		n.ID, n.Label, n.Parent = id, id, parent
		return n
	}
	d.Nodes = []Node{
		mk("n_db", KindDBPrimary, "b_az"),
		mk("n_client", KindClient, ""),
		mk("n_api", KindAPI, "b_az"),
		mk("n_lb", KindLB, "b_vpc"),
	}
	edge := func(id, from, to string) Edge {
		e := DefaultEdge()
		e.ID, e.From, e.To = id, from, to
		return e
	}
	d.Edges = []Edge{edge("e_2", "n_lb", "n_api"), edge("e_1", "n_client", "n_lb"), edge("e_3", "n_api", "n_db")}
	d.Faults = []Fault{{ID: "f_1", AtS: 30, Kind: FaultNodeOff, Target: "n_api", DurS: 10}}
	return d
}

func TestEncodeCanonical(t *testing.T) {
	for name, b := range fixtures(t) {
		d := mustDecode(t, b)
		e1, e2 := mustEncode(t, d), mustEncode(t, d)
		if !bytes.Equal(e1, e2) {
			t.Fatalf("%s: two encodes differ", name)
		}
		if !bytes.HasSuffix(e1, []byte("}\n")) {
			t.Fatalf("%s: encoding does not end with a newline", name)
		}
		d2 := mustDecode(t, e1)
		for i := 1; i < len(d2.Nodes); i++ {
			if d2.Nodes[i-1].ID >= d2.Nodes[i].ID {
				t.Fatalf("%s: nodes not sorted by ID: %q then %q", name, d2.Nodes[i-1].ID, d2.Nodes[i].ID)
			}
		}
	}
	// Input order never changes the bytes.
	a := small()
	b := small()
	slices.Reverse(b.Nodes)
	slices.Reverse(b.Edges)
	slices.Reverse(b.Boxes)
	if !bytes.Equal(mustEncode(t, a), mustEncode(t, b)) {
		t.Fatal("reordered slices encode differently")
	}
	// Encode does not reorder the caller's slices.
	if a.Nodes[0].ID != "n_db" {
		t.Fatalf("Encode mutated the design: first node %q", a.Nodes[0].ID)
	}
	if !strings.Contains(string(mustEncode(t, New("x"))), `"nodes": []`) {
		t.Fatal("empty node list must encode as []")
	}
}

func TestRoundTrip(t *testing.T) {
	for name, b := range fixtures(t) {
		d1 := mustDecode(t, b)
		d2 := mustDecode(t, mustEncode(t, d1))
		if !reflect.DeepEqual(d1, d2) {
			t.Fatalf("%s: round trip differs\n%+v\n%+v", name, d1, d2)
		}
	}
	d := small()
	if is := d.Validate(); len(is) != 0 {
		t.Fatalf("small() invalid: %v", is)
	}
	got := mustDecode(t, mustEncode(t, d))
	if len(got.Nodes) != 4 || got.Nodes[0].ID != "n_api" || got.Nodes[1].Traffic == nil || got.Nodes[0].Traffic != nil {
		t.Fatalf("decoded nodes = %+v", got.Nodes)
	}
}

// mutate decodes enc into a generic tree, lets fn change it and re-marshals it.
func mutate(t *testing.T, enc []byte, fn func(m map[string]any)) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(enc))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func node0(m map[string]any) map[string]any {
	return m["nodes"].([]any)[0].(map[string]any)
}

func TestDecodeLimits(t *testing.T) {
	t.Run("nodes", func(t *testing.T) {
		d := New("many")
		for i := 0; i <= MaxNodes; i++ {
			n := Defaults(KindAPI)
			n.ID = NewID('n')
			d.Nodes = append(d.Nodes, n)
		}
		_, err := Decode(mustEncode(t, d))
		if !hasIssue(issuesOf(t, err), "nodes", IssueTooManyNodes) {
			t.Fatalf("issues = %v", issuesOf(t, err))
		}
	})
	t.Run("edges", func(t *testing.T) {
		d := small()
		for i := 0; i <= MaxEdges; i++ {
			e := DefaultEdge()
			e.ID, e.From, e.To = NewID('e'), "n_api", "n_db"
			d.Edges = append(d.Edges, e)
		}
		_, err := Decode(mustEncode(t, d))
		if !hasIssue(issuesOf(t, err), "edges", IssueTooManyEdges) {
			t.Fatalf("issues = %v", issuesOf(t, err))
		}
	})
	t.Run("depth", func(t *testing.T) {
		d := small()
		d.Boxes = append(d.Boxes, Box{ID: "b_az2", Kind: BoxAZ, Label: "deep", Rect: [4]int{6, 6, 10, 10}, Parent: "b_az"})
		_, err := Decode(mustEncode(t, d))
		is := issuesOf(t, err)
		if !hasIssue(is, "", IssueDepth) {
			t.Fatalf("issues = %v", is)
		}
	})
	t.Run("too_large", func(t *testing.T) {
		big := append([]byte(`{"schema_version":1,"title":"`), bytes.Repeat([]byte("a"), MaxBytes)...)
		big = append(big, `"}`...)
		_, err := Decode(big)
		if !hasIssue(issuesOf(t, err), "", IssueTooLarge) {
			t.Fatal("want too_large")
		}
	})
	t.Run("schema_version", func(t *testing.T) {
		b := mutate(t, mustEncode(t, small()), func(m map[string]any) { m["schema_version"] = json.Number("2") })
		_, err := Decode(b)
		if !hasIssue(issuesOf(t, err), "schema_version", IssueSchemaVersion) {
			t.Fatal("want schema_version")
		}
	})
	t.Run("label", func(t *testing.T) {
		d := small()
		d.Nodes[0].Label = strings.Repeat("가", MaxLabel+1)
		_, err := Decode(mustEncode(t, d))
		is := issuesOf(t, err)
		if !hasIssue(is, "", IssueLabelTooLong) {
			t.Fatalf("issues = %v", is)
		}
		d.Nodes[0].Label = strings.Repeat("가", MaxLabel)
		if _, err := Decode(mustEncode(t, d)); err != nil {
			t.Fatalf("40 graphemes rejected: %v", err)
		}
	})
}

func TestDecodeRejectsBadNumbers(t *testing.T) {
	one := New("one")
	c := Defaults(KindClient)
	c.ID = "n_c"
	one.Nodes = []Node{c}
	enc := mustEncode(t, one)
	cases := []struct {
		name string
		fn   func(n map[string]any)
		path string
	}{
		{"negative", func(n map[string]any) { n["instances"] = json.Number("-1") }, "nodes[0].instances"},
		{"overflow", func(n map[string]any) { n["cpu_cores"] = json.Number("1e309") }, "nodes[0].cpu_cores"},
		{"fraction", func(n map[string]any) { n["memory_mib"] = json.Number("4096.5") }, "nodes[0].memory_mib"},
		{"string", func(n map[string]any) { n["traffic"].(map[string]any)["rps"] = "x" }, "nodes[0].traffic.rps"},
		{"permille", func(n map[string]any) { n["params"].(map[string]any)["hit_permille"] = json.Number("1001") }, "nodes[0].params.hit_permille"},
		{"huge", func(n map[string]any) { n["queue_cap"] = json.Number("99999999999999999999999") }, "nodes[0].queue_cap"},
		{"pos", func(n map[string]any) { n["pos"] = []any{json.Number("1")} }, "nodes[0].pos"},
		{"bool", func(n map[string]any) { n["autoscale"].(map[string]any)["enabled"] = json.Number("1") }, "nodes[0].autoscale.enabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := mutate(t, enc, func(m map[string]any) { tc.fn(node0(m)) })
			_, err := Decode(b)
			is := issuesOf(t, err)
			if !hasIssue(is, tc.path, IssueRange) {
				t.Fatalf("issues = %v, want range at %s", is, tc.path)
			}
		})
	}
	if _, err := Decode([]byte("not json")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("garbage: %v", err)
	}
	if _, err := Decode([]byte(`{"schema_version":1} {}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("trailing data: %v", err)
	}
}

func TestValidateParentKinds(t *testing.T) {
	d := small()
	d.Boxes[2].Parent = "b_region" // az directly under region
	if !hasIssue(d.Validate(), "boxes[2].parent", IssueParentKind) {
		t.Fatalf("az under region: %v", d.Validate())
	}
	d = small()
	if is := d.Validate(); len(is) != 0 { // node under az is fine
		t.Fatalf("issues = %v", is)
	}
	d.Nodes[0].Parent = "b_gone"
	if !hasIssue(d.Validate(), "nodes[0].parent", IssueDanglingRef) {
		t.Fatalf("dangling parent: %v", d.Validate())
	}
	d = small()
	d.Edges[0].To = "n_gone"
	if !hasIssue(d.Validate(), "edges[0].to", IssueDanglingRef) {
		t.Fatalf("dangling edge: %v", d.Validate())
	}
	d = small()
	d.Nodes[1].ID = d.Nodes[0].ID
	if !hasIssue(d.Validate(), "nodes[1].id", IssueDuplicateID) {
		t.Fatalf("duplicate: %v", d.Validate())
	}
	d = small()
	d.Nodes[0].Kind = "mainframe"
	if !hasIssue(d.Validate(), "nodes[0].kind", IssueUnknownKind) {
		t.Fatalf("kind: %v", d.Validate())
	}
	d = small()
	d.Nodes[2].Autoscale = Autoscale{Enabled: true, Min: 5, Max: 2, TargetCPUPermille: 600}
	if !hasIssue(d.Validate(), "nodes[2].autoscale.max", IssueRange) {
		t.Fatalf("autoscale: %v", d.Validate())
	}
	d = small()
	d.Faults[0].Kind = FaultAZOff // target is a node, not an az box
	if !hasIssue(d.Validate(), "faults[0].target", IssueDanglingRef) {
		t.Fatalf("fault target: %v", d.Validate())
	}
	d = small()
	d.Boxes[0].Parent, d.Boxes[1].Parent = "b_region", "b_vpc" // cycle region <-> vpc
	if !hasIssue(d.Validate(), "", IssueDepth) {
		t.Fatalf("cycle: %v", d.Validate())
	}
}

func TestDefaultsAreValid(t *testing.T) {
	d := New("defaults")
	if d.SchemaVersion != SchemaVersion || d.DurationS != 300 {
		t.Fatalf("New = %+v", d)
	}
	if d.Seed >= 1<<53 {
		t.Fatalf("seed %d does not survive a JavaScript number", d.Seed)
	}
	for _, k := range NodeKinds() {
		n := Defaults(k)
		n.ID = NewID('n')
		d.Nodes = append(d.Nodes, n)
		if (k == KindClient) != (n.Traffic != nil) {
			t.Fatalf("%s: traffic = %v", k, n.Traffic)
		}
	}
	if len(NodeKinds()) != 12 {
		t.Fatalf("NodeKinds = %v", NodeKinds())
	}
	if is := d.Validate(); len(is) != 0 {
		t.Fatalf("defaults invalid: %v", is)
	}
	if id := NewID('n'); !strings.HasPrefix(id, "n_") || len(id) != 28 {
		t.Fatalf("NewID = %q", id)
	}
	if Defaults(KindAPI).CPUMicrosPerReq != 8000 {
		t.Fatal("api default cpu time must be 8000µs")
	}
}

// TestFieldTableMatchesTags pins the hand-written field table to the struct JSON tags.
func TestFieldTableMatchesTags(t *testing.T) {
	check := func(name string, typ reflect.Type, got []string, skip ...string) {
		t.Helper()
		var want []string
		var walk func(prefix string, typ reflect.Type)
		walk = func(prefix string, typ reflect.Type) {
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				tag := strings.Split(f.Tag.Get("json"), ",")[0]
				if slices.Contains(skip, tag) && prefix == "" {
					continue
				}
				ft := f.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					walk(prefix+tag+".", ft)
					continue
				}
				want = append(want, prefix+tag)
			}
		}
		walk("", typ)
		slices.Sort(want)
		got = slices.Clone(got)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("%s field table\n got %v\nwant %v", name, got, want)
		}
	}
	check("node", reflect.TypeFor[Node](), fieldNames(nodeFields), "id")
	check("box", reflect.TypeFor[Box](), fieldNames(boxFields), "id")
	check("edge", reflect.TypeFor[Edge](), fieldNames(edgeFields), "id")
	check("fault", reflect.TypeFor[Fault](), fieldNames(faultFields), "id")
	check("meta", reflect.TypeFor[Design](), fieldNames(metaFields), "schema_version", "nodes", "boxes", "edges", "faults")
}
