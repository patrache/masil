package archdoc

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestObjectsRoundTrip(t *testing.T) {
	for name, b := range fixtures(t) {
		d := mustDecode(t, b)
		got, is := FromObjects(d.ToObjects(), nil)
		if len(is) != 0 {
			t.Fatalf("%s: issues %v", name, is)
		}
		if !reflect.DeepEqual(got, d) {
			t.Fatalf("%s: round trip differs\n got %+v\nwant %+v", name, got, d)
		}
	}
	o := small().ToObjects()
	if _, ok := o["meta"]["title"]; !ok {
		t.Fatal("meta object lacks title")
	}
	if string(o["n:n_api"]["pos"]) != "[0,0]" || string(o["b:b_az"]["rect"]) != "[4,4,80,30]" {
		t.Fatalf("pos/rect = %s %s", o["n:n_api"]["pos"], o["b:b_az"]["rect"])
	}
	if _, ok := o["n:n_api"]["autoscale.min"]; !ok {
		t.Fatal("nested fields must flatten with dots")
	}
	if _, ok := o["n:n_api"]["traffic.rps"]; ok {
		t.Fatal("api node must not carry traffic fields")
	}
	if _, ok := o["n:n_client"]["traffic.rps"]; !ok {
		t.Fatal("client node lacks traffic fields")
	}
	for key, want := range map[string]string{"meta": "meta", "n:x": "node", "b:x": "box", "e:x": "edge", "f:x": "fault", "z:x": ""} {
		if got := ObjType(key); got != want {
			t.Errorf("ObjType(%q) = %q, want %q", key, got, want)
		}
	}
	if ObjKey('n', "a") != "n:a" || ObjKey('e', "x") != "e:x" {
		t.Fatal("ObjKey")
	}
}

func shuffled(o Objects, seed uint64) Objects {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	r := rand.New(rand.NewPCG(seed, seed))
	r.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	out := Objects{}
	for _, k := range keys {
		fields := map[string]json.RawMessage{}
		for f, v := range o[k] {
			fields[f] = v
		}
		out[k] = fields
	}
	return out
}

func node(t *testing.T, kind NodeKind, extra map[string]string) map[string]json.RawMessage {
	t.Helper()
	n := Defaults(kind)
	n.ID = "x"
	d := &Design{Nodes: []Node{n}}
	m := d.ToObjects()["n:x"]
	for k, v := range extra {
		m[k] = raw(v)
	}
	return m
}

func box(t *testing.T, kind BoxKind, parent string) map[string]json.RawMessage {
	t.Helper()
	d := &Design{Boxes: []Box{{ID: "x", Kind: kind, Rect: [4]int{0, 0, 10, 10}, Parent: parent}}}
	return d.ToObjects()["b:x"]
}

func TestFromObjectsDanglingAndCycles(t *testing.T) {
	o := Objects{
		"meta": {"title": raw(`"t"`), "seed": raw(`3`), "duration_s": raw(`120`)},
		"n:a":  node(t, KindAPI, map[string]string{"parent": `"Z"`}),
		"n:b":  node(t, KindDBPrimary, nil),
		"b:X":  box(t, BoxVPC, "Y"),
		"b:Y":  box(t, BoxVPC, "X"),
		"b:Z":  box(t, BoxAZ, "X"),
	}
	e := DefaultEdge()
	e.ID, e.From, e.To = "x", "a", "gone"
	o["e:x"] = (&Design{Edges: []Edge{e}}).ToObjects()["e:x"]
	e.ID, e.From, e.To = "ab", "a", "b"
	o["e:ab"] = (&Design{Edges: []Edge{e}}).ToObjects()["e:ab"]
	f := Fault{ID: "fb", Kind: FaultNodeOff, Target: "b"}
	o["f:fb"] = (&Design{Faults: []Fault{f}}).ToObjects()["f:fb"]

	d1, is1 := FromObjects(o, nil)
	for seed := uint64(1); seed <= 8; seed++ {
		d2, is2 := FromObjects(shuffled(o, seed), nil)
		if !bytes.Equal(mustEncode(t, d1), mustEncode(t, d2)) || !reflect.DeepEqual(is1, is2) {
			t.Fatalf("seed %d: result depends on input order", seed)
		}
	}
	for _, e := range d1.Edges {
		if e.ID == "x" {
			t.Fatal("dangling edge kept")
		}
	}
	if !hasIssue(is1, "e:x", IssueDanglingRef) {
		t.Fatalf("issues = %v", is1)
	}
	roots := 0
	for _, b := range d1.Boxes {
		if (b.ID == "X" || b.ID == "Y") && b.Parent == "" {
			roots++
			if b.ID != "X" {
				t.Fatalf("cycle cut at %s, want the smaller ID X", b.ID)
			}
		}
	}
	if roots != 1 {
		t.Fatalf("boxes = %+v, want exactly one of X/Y at top", d1.Boxes)
	}
	if d1.Title != "t" || d1.Seed != 3 || d1.DurationS != 120 {
		t.Fatalf("meta = %q %d %d", d1.Title, d1.Seed, d1.DurationS)
	}

	// Deleting b removes it and everything that points at it.
	d3, is3 := FromObjects(o, func(obj string) bool { return obj == "n:b" })
	for _, n := range d3.Nodes {
		if n.ID == "b" {
			t.Fatal("deleted node kept")
		}
	}
	if len(d3.Edges) != 0 || len(d3.Faults) != 0 {
		t.Fatalf("edges %v faults %v touch the deleted node", d3.Edges, d3.Faults)
	}
	if !hasIssue(is3, "e:ab", IssueDanglingRef) || !hasIssue(is3, "f:fb", IssueDanglingRef) {
		t.Fatalf("issues = %v", is3)
	}
}

func TestFromObjectsDepthAndLimits(t *testing.T) {
	o := Objects{
		"b:a": box(t, BoxRegion, ""),
		"b:b": box(t, BoxVPC, "a"),
		"b:c": box(t, BoxAZ, "b"),
		"b:d": box(t, BoxAZ, "c"),
		"b:e": box(t, BoxAZ, "d"),
		"b:s": box(t, BoxAZ, "s"), // self parent
		"b:q": box(t, "planet", ""),
	}
	d, is := FromObjects(o, nil)
	parent := map[string]string{}
	for _, b := range d.Boxes {
		parent[b.ID] = b.Parent
	}
	if parent["d"] != "b" || parent["e"] != "b" || parent["s"] != "" {
		t.Fatalf("parents = %v", parent)
	}
	if _, ok := parent["q"]; ok || !hasIssue(is, "b:q.kind", IssueUnknownKind) {
		t.Fatalf("unknown box kind kept: %v %v", parent, is)
	}
	boxes := map[string]*Box{}
	for i := range d.Boxes {
		boxes[d.Boxes[i].ID] = &d.Boxes[i]
	}
	for i := range d.Boxes {
		if boxDepth(boxes, &d.Boxes[i]) > MaxDepth {
			t.Fatalf("box %s still too deep", d.Boxes[i].ID)
		}
	}

	o = Objects{}
	for i := 0; i < MaxNodes+5; i++ {
		id := string(rune('A'+i/26/26%26)) + string(rune('A'+i/26%26)) + string(rune('A'+i%26))
		o[ObjKey('n', id)] = node(t, KindAPI, nil)
	}
	d, is = FromObjects(o, nil)
	if len(d.Nodes) != MaxNodes || !hasIssue(is, "nodes", IssueTooManyNodes) {
		t.Fatalf("nodes = %d, issues %v", len(d.Nodes), is)
	}
	if d.Nodes[MaxNodes-1].ID != "ALN" { // index 299 in ID order; the 5 largest are dropped
		t.Fatalf("last kept = %s", d.Nodes[MaxNodes-1].ID)
	}
}

func TestFromObjectsBadFieldValue(t *testing.T) {
	o := Objects{
		"n:a": node(t, KindAPI, map[string]string{
			"instances":     `"-5"`,
			"cpu_cores":     `9999`,
			"label":         `"` + string(bytes.Repeat([]byte("가"), MaxLabel+1)) + `"`,
			"disk.type":     `"floppy"`,
			"unknown.field": `1`,
		}),
		"n:c": node(t, KindClient, map[string]string{"traffic.rps": `1.5`}),
		"n:z": node(t, "mainframe", nil),
	}
	o["n:z"]["kind"] = raw(`"mainframe"`)
	d, is := FromObjects(o, nil)
	def := Defaults(KindAPI)
	if len(d.Nodes) != 2 {
		t.Fatalf("nodes = %+v", d.Nodes)
	}
	a := d.Nodes[0]
	if a.Instances != def.Instances || a.CPUCores != def.CPUCores || a.Label != "" || a.Disk.Type != def.Disk.Type {
		t.Fatalf("node a = %+v", a)
	}
	for _, want := range []Issue{
		{"n:a.instances", IssueRange}, {"n:a.cpu_cores", IssueRange}, {"n:a.label", IssueLabelTooLong},
		{"n:a.disk.type", IssueUnknownKind}, {"n:c.traffic.rps", IssueRange}, {"n:z.kind", IssueUnknownKind},
	} {
		if !hasIssue(is, want.Path, want.Code) {
			t.Errorf("missing %v in %v", want, is)
		}
	}
	if c := d.Nodes[1]; c.Traffic == nil || c.Traffic.RPS != DefaultTraffic().RPS {
		t.Fatalf("client traffic = %+v", c.Traffic)
	}
	if is := d.Validate(); len(is) != 0 {
		t.Fatalf("repaired design invalid: %v", is)
	}
	// Autoscale min > max falls back to the default pair.
	o = Objects{"n:a": node(t, KindAPI, map[string]string{"autoscale.enabled": `true`, "autoscale.min": `9`, "autoscale.max": `3`})}
	d, is = FromObjects(o, nil)
	if d.Nodes[0].Autoscale.Min > d.Nodes[0].Autoscale.Max || !hasIssue(is, "n:a.autoscale.max", IssueRange) {
		t.Fatalf("autoscale = %+v, issues %v", d.Nodes[0].Autoscale, is)
	}
}
