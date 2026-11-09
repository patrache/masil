package archsim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/patrache/masil/internal/archdoc"
)

func run(t *testing.T, d *archdoc.Design, opt TimedOptions) *Timeline {
	t.Helper()
	tl, err := RunTimed(context.Background(), d, opt)
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func frameAt(t *testing.T, tl *Timeline, t10 int) Frame {
	t.Helper()
	for _, f := range tl.Frames {
		if f.T10 == t10 {
			return f
		}
	}
	t.Fatalf("no frame at %d", t10)
	return Frame{}
}

func hasEvent(tl *Timeline, e Event) bool { return slices.Contains(tl.Events, e) }

func box(id string, kind archdoc.BoxKind, parent string) archdoc.Box {
	b := archdoc.Box{ID: id, Kind: kind, Label: id, Parent: parent, Rect: [4]int{0, 0, 40, 20}}
	if kind == archdoc.BoxRegion {
		b.RegionCode = "ap-northeast-2"
	}
	return b
}

func in(parent string) func(*archdoc.Node) { return func(n *archdoc.Node) { n.Parent = parent } }

// failoverDesign: api in az-b reads from a replica in az-c and writes to a
// primary in az-a; az-a goes off at 30s.
func failoverDesign(failoverTo string) *archdoc.Design {
	d := design([]archdoc.Node{
		cl("c", 200, 800),
		nd("api", archdoc.KindAPI, in("az-b")),
		nd("primary", archdoc.KindDBPrimary, in("az-a"), func(n *archdoc.Node) { n.Params.FailoverTo, n.Params.FailoverS = failoverTo, 30 }),
		nd("replica", archdoc.KindDBReplica, in("az-c")),
	}, []archdoc.Edge{
		ed("e1", "c", "api"), ed("e2", "api", "primary", shares(0, 1000)), ed("e3", "api", "replica", shares(1000, 0)),
	}, archdoc.Fault{ID: "f1", AtS: 30, Kind: archdoc.FaultAZOff, Target: "az-a"})
	d.Boxes = []archdoc.Box{box("r", archdoc.BoxRegion, ""), box("v", archdoc.BoxVPC, "r"),
		box("az-a", archdoc.BoxAZ, "v"), box("az-b", archdoc.BoxAZ, "v"), box("az-c", archdoc.BoxAZ, "v")}
	return d
}

func TestTimedAZOffFailover(t *testing.T) {
	tl := run(t, failoverDesign("replica"), TimedOptions{})
	for _, e := range []Event{
		{T10: 300, Kind: EventFaultStart, Target: "az-a"},
		{T10: 300, Kind: EventFailoverStart, Target: "primary", Value: 30},
		{T10: 600, Kind: EventFailoverDone, Target: "replica"},
	} {
		if !hasEvent(tl, e) {
			t.Errorf("missing %+v in %+v", e, tl.Events)
		}
	}
	if len(tl.Frames) != 120 || tl.EngineVersion != EngineVersion || tl.Summary == nil {
		t.Fatalf("frames %d version %d summary %v", len(tl.Frames), tl.EngineVersion, tl.Summary != nil)
	}
	if f := frameAt(t, tl, 300); f.ErrPermille != 0 {
		t.Fatalf("errors before the fault: %+v", f)
	}
	for s := 31; s <= 59; s++ {
		if f := frameAt(t, tl, s*10); f.ErrPermille < 200 || !f.Nodes["primary"].Down {
			t.Fatalf("frame %ds: %+v", s, f)
		}
	}
	if f := frameAt(t, tl, 700); f.ErrPermille != 0 || f.RPS != 200 {
		t.Fatalf("frame 70s after failover: %+v", f)
	}
	if tl.Summary.Nodes["primary"].Down != true || tl.Summary.ErrorPermille <= 0 {
		t.Fatalf("summary = %+v", tl.Summary.Nodes["primary"])
	}
}

func TestTimedNoReplicaNoRecovery(t *testing.T) {
	d := failoverDesign("")
	if !slices.ContainsFunc(archdoc.Check(d), func(f archdoc.Finding) bool {
		return f.Code == archdoc.CheckSPOFSingleInstance && slices.Contains(f.Targets, "primary")
	}) {
		t.Fatalf("no SPOF finding: %+v", archdoc.Check(d))
	}
	tl := run(t, d, TimedOptions{})
	if !hasEvent(tl, Event{T10: 300, Kind: EventWritesFailing, Target: "primary"}) {
		t.Fatalf("events = %+v", tl.Events)
	}
	for _, e := range tl.Events {
		if e.Kind == EventFailoverDone {
			t.Fatalf("recovered: %+v", e)
		}
	}
	for _, f := range tl.Frames[30:] {
		if f.ErrPermille < 200 {
			t.Fatalf("frame %d: writes recovered: %+v", f.T10, f)
		}
	}
}

func TestTimedAutoscaleWarmup(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 1500, 800, func(n *archdoc.Node) { n.Traffic.Curve = archdoc.CurveRamp }),
		nd("api", archdoc.KindAPI, func(n *archdoc.Node) {
			n.Autoscale = archdoc.Autoscale{Enabled: true, Min: 2, Max: 8, TargetCPUPermille: 600, WarmupS: 90, CooldownS: 300}
		}),
	}, []archdoc.Edge{ed("e1", "c", "api")})
	tl := run(t, d, TimedOptions{})
	first := -1
	for _, e := range tl.Events {
		if e.Kind == EventScaleOut && e.Target == "api" {
			first = e.T10
			break
		}
	}
	if first < 0 {
		t.Fatalf("no scale_out: %+v", tl.Events)
	}
	rose := -1
	for _, f := range tl.Frames {
		if f.Nodes["api"].Instances > 2 {
			rose = f.T10
			break
		}
	}
	if rose < 0 || rose < first+900 {
		t.Fatalf("first order %d, instances rose at %d", first, rose)
	}
	if last := tl.Frames[len(tl.Frames)-1].Nodes["api"].Instances; last <= 2 || last > 8 {
		t.Fatalf("instances at the end = %d", last)
	}
	if tl.Summary.Nodes["api"].AvgInstancesMilli <= 2000 {
		t.Fatalf("avg instances = %d", tl.Summary.Nodes["api"].AvgInstancesMilli)
	}
}

func TestTimedQueueBuildsAndDrains(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 250, 800, func(n *archdoc.Node) {
			n.Traffic.Curve, n.Traffic.SpikeMult, n.Traffic.SpikeAtS, n.Traffic.SpikeDurS = archdoc.CurveSpike, 4, 20, 20
		}),
		nd("api", archdoc.KindAPI),
	}, []archdoc.Edge{ed("e1", "c", "api")})
	tl := run(t, d, TimedOptions{})
	peak := 0
	for _, f := range tl.Frames {
		peak = max(peak, f.Nodes["api"].QueueLen)
	}
	if peak < 500 {
		t.Fatalf("queue peak = %d", peak)
	}
	if q := frameAt(t, tl, 200).Nodes["api"].QueueLen; q != 0 {
		t.Fatalf("queue before the spike = %d", q)
	}
	if q := frameAt(t, tl, 800).Nodes["api"].QueueLen; q != 0 {
		t.Fatalf("queue did not drain: %d", q)
	}
	if !slices.ContainsFunc(tl.Events, func(e Event) bool { return e.Kind == EventQueueDrop && e.Target == "api" }) {
		t.Fatalf("no queue_drop: %+v", tl.Events)
	}
	if f := frameAt(t, tl, 300); f.ErrPermille == 0 || f.RPS != 1000 {
		t.Fatalf("frame in the spike: %+v", f)
	}
}

func TestTimedLBReroute(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 400, 800), nd("lb", archdoc.KindLB), nd("api1", archdoc.KindAPI), nd("api2", archdoc.KindAPI),
	}, []archdoc.Edge{ed("e1", "c", "lb"), ed("e2", "lb", "api1"), ed("e3", "lb", "api2")},
		archdoc.Fault{ID: "f1", AtS: 20, Kind: archdoc.FaultNodeOff, Target: "api1"})
	tl := run(t, d, TimedOptions{})
	if !hasEvent(tl, Event{T10: 200, Kind: EventFaultStart, Target: "api1"}) ||
		!hasEvent(tl, Event{T10: 300, Kind: EventLBReroute, Target: "lb", Value: 1}) {
		t.Fatalf("events = %+v", tl.Events)
	}
	if f := frameAt(t, tl, 250); f.ErrPermille < 450 || f.ErrPermille > 550 {
		t.Fatalf("before reroute: %+v", f)
	}
	if f := frameAt(t, tl, 400); f.ErrPermille != 0 {
		t.Fatalf("after reroute: %+v", f)
	}
}

func TestTimedFaultEndsAndLatency(t *testing.T) {
	d := design([]archdoc.Node{cl("c", 100, 800), nd("api", archdoc.KindAPI)}, []archdoc.Edge{ed("e1", "c", "api")},
		archdoc.Fault{ID: "f1", AtS: 10, Kind: archdoc.FaultLatency, Target: "api", AddMs: 500, DurS: 20},
		archdoc.Fault{ID: "f2", AtS: 50, Kind: archdoc.FaultLatency, Target: "e1", AddMs: 300, DurS: 10},
		archdoc.Fault{ID: "f3", AtS: 70, Kind: archdoc.FaultInstancesOff, Target: "api", Count: 2, DurS: 10})
	tl := run(t, d, TimedOptions{})
	if !hasEvent(tl, Event{T10: 300, Kind: EventFaultEnd, Target: "api"}) {
		t.Fatalf("events = %+v", tl.Events)
	}
	base, slow, link := frameAt(t, tl, 50).P50Ms, frameAt(t, tl, 200).P50Ms, frameAt(t, tl, 550).P50Ms
	if slow < base+480 || link < base+280 || frameAt(t, tl, 400).P50Ms > base+50 {
		t.Fatalf("p50 base %d node %d edge %d", base, slow, link)
	}
	if f := frameAt(t, tl, 750); f.ErrPermille != 1000 || !f.Nodes["api"].Down || f.Nodes["api"].Instances != 0 {
		t.Fatalf("all instances off: %+v", f)
	}
	if f := frameAt(t, tl, 900); f.ErrPermille != 0 || f.Nodes["api"].Instances != 2 {
		t.Fatalf("instances back: %+v", f)
	}
}

func TestTimedCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tl, err := RunTimed(ctx, failoverDesign("replica"), TimedOptions{})
	if tl != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, %v", tl, err)
	}
}

func TestTimedDeterministic(t *testing.T) {
	for name, d := range testdataDesigns(t) {
		a, _ := json.Marshal(run(t, d, TimedOptions{DurationS: 60, SamplesPerFrame: 200}))
		b, _ := json.Marshal(run(t, d, TimedOptions{DurationS: 60, SamplesPerFrame: 200}))
		if !bytes.Equal(a, b) {
			t.Fatalf("%s: two runs differ", name)
		}
	}
}

func TestTimedDegenerateInputs(t *testing.T) {
	for name, d := range degenerateDesigns() {
		t.Run(name, func(t *testing.T) {
			tl := run(t, d, TimedOptions{DurationS: 60, SamplesPerFrame: 50})
			nonNegative(t, name, tl)
			if len(tl.Frames) != 60 {
				t.Fatalf("frames = %d", len(tl.Frames))
			}
			for _, f := range tl.Frames {
				if f.ErrPermille > 1000 {
					t.Fatalf("frame %+v", f)
				}
			}
		})
	}
	tl := run(t, &archdoc.Design{}, TimedOptions{})
	if len(tl.Frames) != 300 {
		t.Fatalf("default duration frames = %d", len(tl.Frames))
	}
}
