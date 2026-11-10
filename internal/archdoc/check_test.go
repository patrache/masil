package archdoc

import (
	"slices"
	"testing"
)

type builder struct{ d *Design }

func newBuilder() *builder { return &builder{d: New("check")} }

func (b *builder) node(id string, k NodeKind, parent string, instances int) *builder {
	n := Defaults(k)
	n.ID, n.Parent, n.Instances = id, parent, instances
	b.d.Nodes = append(b.d.Nodes, n)
	return b
}

func (b *builder) box(id string, k BoxKind, parent string) *builder {
	b.d.Boxes = append(b.d.Boxes, Box{ID: id, Kind: k, Parent: parent, Rect: [4]int{0, 0, 10, 10}})
	return b
}

func (b *builder) edge(from, to string) *builder {
	e := DefaultEdge()
	e.ID, e.From, e.To = "e_"+from+"_"+to, from, to
	b.d.Edges = append(b.d.Edges, e)
	return b
}

func has(fs []Finding, code CheckCode, targets ...string) bool {
	for _, f := range fs {
		if f.Code == code && (targets == nil || slices.Equal(f.Targets, targets)) {
			return true
		}
	}
	return false
}

func TestCheckFindings(t *testing.T) {
	cases := []struct {
		name    string
		b       *builder
		code    CheckCode
		targets []string
		want    bool
	}{
		{"single api no lb", newBuilder().node("c", KindClient, "", 1).node("api", KindAPI, "", 1).edge("c", "api"),
			CheckSPOFSingleInstance, []string{"api"}, true},
		{"single api behind lb", newBuilder().node("c", KindClient, "", 1).node("lb", KindLB, "", 2).node("api", KindAPI, "", 1).
			edge("c", "lb").edge("lb", "api"), CheckSPOFSingleInstance, nil, false},
		{"two apis", newBuilder().node("c", KindClient, "", 1).node("api", KindAPI, "", 2).edge("c", "api"),
			CheckSPOFSingleInstance, nil, false},
		{"one lb", newBuilder().node("c", KindClient, "", 1).node("lb", KindLB, "", 1).node("api", KindAPI, "", 2).
			edge("c", "lb").edge("lb", "api"), CheckSPOFSingleLB, []string{"lb"}, true},
		{"primary replica same az", newBuilder().box("r", BoxRegion, "").box("v", BoxVPC, "r").box("az-a", BoxAZ, "v").
			node("p", KindDBPrimary, "az-a", 1).node("q", KindDBReplica, "az-a", 1).edge("p", "q"),
			CheckPrimaryReplicaSameAZ, []string{"p", "q"}, true},
		{"primary replica two azs", newBuilder().box("r", BoxRegion, "").box("v", BoxVPC, "r").box("az-a", BoxAZ, "v").box("az-b", BoxAZ, "v").
			node("p", KindDBPrimary, "az-a", 1).node("q", KindDBReplica, "az-b", 1).edge("p", "q"),
			CheckPrimaryReplicaSameAZ, nil, false},
		{"api only in one az", newBuilder().box("r", BoxRegion, "").box("v", BoxVPC, "r").box("az-a", BoxAZ, "v").
			node("c", KindClient, "", 1).node("a1", KindAPI, "az-a", 2).node("a2", KindAPI, "az-a", 2).edge("c", "a1").edge("c", "a2"),
			CheckSingleAZService, []string{"a1", "a2"}, true},
		{"api in two azs", newBuilder().box("r", BoxRegion, "").box("v", BoxVPC, "r").box("az-a", BoxAZ, "v").box("az-b", BoxAZ, "v").
			node("c", KindClient, "", 1).node("a1", KindAPI, "az-a", 2).node("a2", KindAPI, "az-b", 2).edge("c", "a1").edge("c", "a2"),
			CheckSingleAZService, nil, false},
		{"isolated", newBuilder().node("c", KindClient, "", 1).node("api", KindAPI, "", 2).node("lonely", KindCache, "", 2).edge("c", "api"),
			CheckUnconnected, []string{"lonely"}, true},
		{"cycle", newBuilder().node("a", KindAPI, "", 2).node("b", KindAPI, "", 2).node("c", KindAPI, "", 2).edge("a", "b").edge("b", "a").edge("b", "c"),
			CheckCycle, []string{"a", "b"}, true},
		{"self loop", newBuilder().node("a", KindAPI, "", 2).edge("a", "a"), CheckCycle, []string{"a"}, true},
		{"no cycle", newBuilder().node("a", KindAPI, "", 2).node("b", KindAPI, "", 2).edge("a", "b"), CheckCycle, nil, false},
		{"az at top", newBuilder().box("az", BoxAZ, ""), CheckAZOutsideVPC, []string{"az"}, true},
		{"vpc at top", newBuilder().box("v", BoxVPC, ""), CheckVPCOutsideRegion, []string{"v"}, true},
		{"vpc in region", newBuilder().box("r", BoxRegion, "").box("v", BoxVPC, "r"), CheckVPCOutsideRegion, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Check(tc.b.d)
			if has(got, tc.code, tc.targets...) != tc.want {
				t.Fatalf("Check = %+v; want %s %v present=%v", got, tc.code, tc.targets, tc.want)
			}
		})
	}
	// A client (or CDN, external) at the top level is normal.
	b := newBuilder().box("r", BoxRegion, "").box("v", BoxVPC, "r").box("az-a", BoxAZ, "v").box("az-b", BoxAZ, "v").
		node("c", KindClient, "", 1).node("lb", KindLB, "v", 2).node("a1", KindAPI, "az-a", 2).node("a2", KindAPI, "az-b", 2).
		edge("c", "lb").edge("lb", "a1").edge("lb", "a2")
	if got := Check(b.d); len(got) != 0 {
		t.Fatalf("healthy design findings = %+v", got)
	}
}

func TestCheckSortedAndTolerant(t *testing.T) {
	b := newBuilder().node("z", KindAPI, "", 1).node("y", KindAPI, "", 1).box("loop1", BoxAZ, "loop2").box("loop2", BoxVPC, "loop1")
	b.d.Nodes[0].Parent = "missing"
	e := DefaultEdge()
	e.ID, e.From, e.To = "e_bad", "z", "nobody"
	b.d.Edges = append(b.d.Edges, e)
	got := Check(b.d) // unvalidated input: must not panic
	if !slices.IsSortedFunc(got, compareFinding) {
		t.Fatalf("findings not sorted: %+v", got)
	}
	for _, f := range got {
		if !slices.IsSorted(f.Targets) {
			t.Fatalf("targets not sorted: %+v", f)
		}
	}
	if !has(got, CheckUnconnected, "y") || !has(got, CheckUnconnected, "z") {
		t.Fatalf("findings = %+v", got)
	}
}
