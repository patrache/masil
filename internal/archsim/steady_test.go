package archsim

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/patrache/masil/internal/archdoc"
)

// nd builds a node of kind with that kind's defaults.
func nd(id string, kind archdoc.NodeKind, mods ...func(*archdoc.Node)) archdoc.Node {
	n := archdoc.Defaults(kind)
	n.ID, n.Label = id, id
	for _, f := range mods {
		f(&n)
	}
	return n
}

// cl builds a client sending rps requests per second, read‰ of them reads.
func cl(id string, rps, read int, mods ...func(*archdoc.Node)) archdoc.Node {
	return nd(id, archdoc.KindClient, append([]func(*archdoc.Node){func(n *archdoc.Node) {
		n.Traffic.RPS, n.Traffic.ReadPermille = rps, read
	}}, mods...)...)
}

func ed(id, from, to string, mods ...func(*archdoc.Edge)) archdoc.Edge {
	e := archdoc.DefaultEdge()
	e.ID, e.From, e.To = id, from, to
	for _, f := range mods {
		f(&e)
	}
	return e
}

func shares(read, write int) func(*archdoc.Edge) {
	return func(e *archdoc.Edge) { e.ReadPermille, e.WritePermille = read, write }
}

func design(nodes []archdoc.Node, edges []archdoc.Edge, faults ...archdoc.Fault) *archdoc.Design {
	return &archdoc.Design{SchemaVersion: 1, Title: "t", Seed: 1, DurationS: 120, Nodes: nodes, Edges: edges, Faults: faults}
}

// big is a fast API node that never limits anything in these tests.
func big(n *archdoc.Node) { n.Instances, n.CPUCores, n.CPUMicrosPerReq = 10, 64, 100 }

func TestAnalyzeSingleNodeUtil(t *testing.T) {
	d := design([]archdoc.Node{cl("c", 400, 800), nd("api", archdoc.KindAPI)}, []archdoc.Edge{ed("e1", "c", "api")})
	r := Analyze(d, Options{})
	api := r.Nodes["api"]
	if api.UtilPermille != 800 || api.Limit != LimitCPU || api.InRPS != 400 || api.CapacityRPS != 500 {
		t.Fatalf("api = %+v", api)
	}
	if r.RPS != 400 || r.EngineVersion != EngineVersion || r.Overloaded {
		t.Fatalf("result = %+v", r)
	}
	if r.P99Ms < r.P50Ms || r.P50Ms <= 0 {
		t.Fatalf("p50 %d p99 %d", r.P50Ms, r.P99Ms)
	}
	if r.Bottleneck == nil || r.Bottleneck.Target != "api" || r.Bottleneck.Limit != LimitCPU || r.Bottleneck.UtilPermille != 800 {
		t.Fatalf("bottleneck = %+v", r.Bottleneck)
	}
}

func TestAnalyzeCDNHitsOnlyReads(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 1000, 800),
		nd("cdn", archdoc.KindCDN, func(n *archdoc.Node) { n.Params.HitPermille = 600 }),
		nd("api", archdoc.KindAPI, big),
	}, []archdoc.Edge{ed("e1", "c", "cdn"), ed("e2", "cdn", "api")})
	r := Analyze(d, Options{})
	if got := r.Nodes["api"].InRPS; got != 520 {
		t.Fatalf("origin InRPS = %d, want 520", got)
	}
	if got := r.Nodes["cdn"].InRPS; got != 1000 {
		t.Fatalf("cdn InRPS = %d", got)
	}
	if got := r.Edges["e2"].RPS; got != 520 {
		t.Fatalf("edge RPS = %d", got)
	}
}

func TestAnalyzeWritesToPrimaryOnly(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 1000, 800),
		nd("api", archdoc.KindAPI, big),
		nd("primary", archdoc.KindDBPrimary, func(n *archdoc.Node) { n.CPUCores = 32 }),
		nd("replica", archdoc.KindDBReplica, func(n *archdoc.Node) { n.CPUCores = 32 }),
	}, []archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "primary"), ed("e3", "api", "replica")})
	r := Analyze(d, Options{})
	// reads split 1000:1000 → half each; both edges carry writes 1000:1000 but
	// the replica's write share goes to the primary.
	if got := r.Nodes["replica"].InRPS; got != 400 {
		t.Fatalf("replica InRPS = %d, want 400 (reads only)", got)
	}
	if got := r.Nodes["primary"].InRPS; got != 600 {
		t.Fatalf("primary InRPS = %d, want 600", got)
	}
	if r.ErrorPermille != 0 {
		t.Fatalf("errors = %d", r.ErrorPermille)
	}
}

func TestAnalyzeReplicaWritesWithoutPrimaryFail(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 1000, 800),
		nd("api", archdoc.KindAPI, big),
		nd("replica", archdoc.KindDBReplica, func(n *archdoc.Node) { n.CPUCores = 32 }),
	}, []archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "replica")})
	r := Analyze(d, Options{})
	if r.Nodes["replica"].InRPS != 800 || r.ErrorPermille != 200 {
		t.Fatalf("replica %+v errors %d", r.Nodes["replica"], r.ErrorPermille)
	}
}

func TestAnalyzeIOPSBottleneck(t *testing.T) {
	d := design([]archdoc.Node{
		cl("c", 2400, 0),
		nd("api", archdoc.KindAPI, big),
		nd("db", archdoc.KindDBPrimary, func(n *archdoc.Node) { n.CPUCores, n.Disk.IOPS = 16, 3060 }),
	}, []archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "db")})
	r := Analyze(d, Options{})
	want := Bottleneck{Target: "db", Limit: LimitIOPS, UtilPermille: 980, Demand: 2400, PerUnitMilli: 1250}
	if r.Bottleneck == nil || *r.Bottleneck != want {
		t.Fatalf("bottleneck = %+v, want %+v", r.Bottleneck, want)
	}
	if db := r.Nodes["db"]; db.Limit != LimitIOPS || db.UtilPermille != 980 || db.Overloaded {
		t.Fatalf("db = %+v", db)
	}
}

func TestAnalyzeMemoryShrinksHitRate(t *testing.T) {
	build := func(ws int) *Result {
		d := design([]archdoc.Node{
			cl("c", 1000, 800),
			nd("api", archdoc.KindAPI, big),
			nd("cache", archdoc.KindCache, func(n *archdoc.Node) {
				n.Instances, n.MemoryMiB, n.Params.HitPermille, n.Params.WorkingSetMiB = 2, 4096, 900, ws
			}),
			nd("db", archdoc.KindDBPrimary, func(n *archdoc.Node) { n.CPUCores = 32 }),
		}, []archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "cache"), ed("e3", "cache", "db")})
		return Analyze(d, Options{})
	}
	fits, spills := build(8192), build(16384)
	// reads 800: hit 900‰ → 80 misses; working set 2× memory → hit 450‰ → 440 misses. Writes 200 pass.
	if got := fits.Nodes["db"].InRPS; got != 280 {
		t.Fatalf("db InRPS (fits) = %d, want 280", got)
	}
	if got := spills.Nodes["db"].InRPS; got != 640 {
		t.Fatalf("db InRPS (spills) = %d, want 640", got)
	}
}

func retryDesign(retries int) *archdoc.Design {
	return design([]archdoc.Node{
		cl("c", 1000, 800),
		nd("api", archdoc.KindAPI, big, func(n *archdoc.Node) { n.Retries = retries }),
		nd("db", archdoc.KindDBPrimary, func(n *archdoc.Node) { n.CPUCores = 1 }),
	}, []archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "db")})
}

func TestAnalyzeRetryStorm(t *testing.T) {
	r0, r3 := Analyze(retryDesign(0), Options{}), Analyze(retryDesign(3), Options{})
	if !r0.Nodes["db"].Overloaded || !r3.Nodes["db"].Overloaded {
		t.Fatalf("db not overloaded: %+v / %+v", r0.Nodes["db"], r3.Nodes["db"])
	}
	if r0.Nodes["db"].InRPS != 1000 {
		t.Fatalf("db InRPS (no retries) = %d", r0.Nodes["db"].InRPS)
	}
	if r3.Nodes["db"].InRPS <= r0.Nodes["db"].InRPS {
		t.Fatalf("retries did not raise db load: %d <= %d", r3.Nodes["db"].InRPS, r0.Nodes["db"].InRPS)
	}
	if r3.Edges["e2"].RPS != r3.Nodes["db"].InRPS {
		t.Fatalf("edge attempts %d != db arrivals %d", r3.Edges["e2"].RPS, r3.Nodes["db"].InRPS)
	}
}

func TestAnalyzeOverloadTimeout(t *testing.T) {
	d := design([]archdoc.Node{cl("c", 750, 800), nd("api", archdoc.KindAPI)}, []archdoc.Edge{ed("e1", "c", "api")})
	r := Analyze(d, Options{})
	api := r.Nodes["api"]
	if !api.Overloaded || api.UtilPermille != 1500 || api.P99Ms != 2000 || api.P50Ms != 2000 {
		t.Fatalf("api = %+v", api)
	}
	if api.ErrPermille < 332 || api.ErrPermille > 334 {
		t.Fatalf("ErrPermille = %d, want ≈333", api.ErrPermille)
	}
	if r.ErrorPermille < 332 || r.ErrorPermille > 334 || !r.Overloaded {
		t.Fatalf("result errors %d overloaded %v", r.ErrorPermille, r.Overloaded)
	}
}

func TestAnalyzeCycleDropped(t *testing.T) {
	d := design([]archdoc.Node{cl("c", 100, 800), nd("a", archdoc.KindAPI), nd("b", archdoc.KindAPI)},
		[]archdoc.Edge{ed("e1", "c", "a"), ed("e2", "a", "b"), ed("e3", "b", "a")})
	r := Analyze(d, Options{})
	if r.Nodes["a"].InRPS != 100 || r.Nodes["b"].InRPS != 100 || r.Edges["e3"].RPS != 0 {
		t.Fatalf("a %+v b %+v e3 %+v", r.Nodes["a"], r.Nodes["b"], r.Edges["e3"])
	}
	if !slices.ContainsFunc(r.Findings, func(f archdoc.Finding) bool { return f.Code == archdoc.CheckCycle }) {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

func TestAnalyzeAsyncSkipsClientLatency(t *testing.T) {
	slow := func(n *archdoc.Node) { n.CPUMicrosPerReq = 500_000; n.Instances, n.CPUCores = 10, 64 }
	mk := func(p archdoc.Protocol) *Result {
		return Analyze(design([]archdoc.Node{cl("c", 10, 0), nd("api", archdoc.KindAPI), nd("w", archdoc.KindWorker, slow)},
			[]archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "w", func(e *archdoc.Edge) { e.Protocol = p })}), Options{})
	}
	sync, async := mk(archdoc.ProtoHTTP1), mk(archdoc.ProtoAsync)
	if async.Nodes["w"].InRPS != 10 || sync.Nodes["w"].InRPS != 10 {
		t.Fatalf("worker load sync %d async %d", sync.Nodes["w"].InRPS, async.Nodes["w"].InRPS)
	}
	if async.P50Ms >= sync.P50Ms || async.P50Ms > 100 {
		t.Fatalf("p50 sync %d async %d", sync.P50Ms, async.P50Ms)
	}
}

// nonNegative fails when any JSON number in v is negative or not an integer.
func nonNegative(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	if bytes.Contains(b, []byte("NaN")) || bytes.Contains(b, []byte("Inf")) {
		t.Fatalf("%s: %s", name, b)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var walk func(path string, x any)
	walk = func(path string, x any) {
		switch x := x.(type) {
		case map[string]any:
			for k, v := range x {
				walk(path+"."+k, v)
			}
		case []any:
			for i, v := range x {
				walk(path+"["+strconv.Itoa(i)+"]", v)
			}
		case json.Number:
			n, err := x.Int64()
			if err != nil || n < 0 {
				t.Errorf("%s: %s = %s", name, path, x)
			}
		}
	}
	walk("", tree)
}

func degenerateDesigns() map[string]*archdoc.Design {
	chain := func(mods ...func(*archdoc.Node)) *archdoc.Design {
		return design([]archdoc.Node{cl("c", 500, 800), nd("api", archdoc.KindAPI, mods...), nd("db", archdoc.KindDBPrimary)},
			[]archdoc.Edge{ed("e1", "c", "api"), ed("e2", "api", "db")})
	}
	ratio := design([]archdoc.Node{cl("c", 500, 800), nd("a", archdoc.KindAPI), nd("b", archdoc.KindAPI), nd("x", archdoc.KindCache)},
		[]archdoc.Edge{ed("e1", "c", "a"), ed("e2", "c", "b"), ed("e3", "c", "x"), ed("e4", "a", "b")})
	return map[string]*archdoc.Design{
		"instances_0":    chain(func(n *archdoc.Node) { n.Instances = 0 }),
		"cpu_micros_0":   chain(func(n *archdoc.Node) { n.CPUMicrosPerReq = 0 }),
		"nic_0":          chain(func(n *archdoc.Node) { n.NICMbps = 0 }),
		"ratio_sum_3000": ratio,
		"rps_0":          design([]archdoc.Node{cl("c", 0, 800), nd("api", archdoc.KindAPI)}, []archdoc.Edge{ed("e1", "c", "api")}),
		"rps_2e9":        design([]archdoc.Node{cl("c", 2_000_000_000, 800), nd("api", archdoc.KindAPI)}, []archdoc.Edge{ed("e1", "c", "api")}),
		"autoscale_minmax": chain(func(n *archdoc.Node) {
			n.Autoscale = archdoc.Autoscale{Enabled: true, Min: 10, Max: 2, TargetCPUPermille: 600}
		}),
		"empty": {},
		"negative_huge": design([]archdoc.Node{
			cl("c", 1000, 5000, func(n *archdoc.Node) { n.Traffic.ReqKB, n.Traffic.RespKB = -5, 1<<40 }),
			nd("api", archdoc.KindAPI, func(n *archdoc.Node) {
				n.CPUCores, n.TimeoutMs, n.Retries, n.QueueCap, n.Instances = -1, 0, 1000, -3, -2
			}),
			nd("cache", archdoc.KindCache, func(n *archdoc.Node) { n.Params.HitPermille, n.MemoryMiB, n.Params.WorkingSetMiB = 5000, 0, 100 }),
			nd("db", archdoc.KindDBPrimary, func(n *archdoc.Node) { n.Disk.IOPS, n.Params.PoolMax, n.Params.FailoverTo = 0, -1, "db" }),
			nd("ext", archdoc.KindExternal, func(n *archdoc.Node) { n.Params.ErrorPermille, n.Params.MaxRPS = 9999, -1 }),
		}, []archdoc.Edge{
			ed("e1", "c", "cache", shares(-100, 4000)), ed("e2", "cache", "db"), ed("e3", "c", "ext", func(e *archdoc.Edge) { e.Mbps, e.LatencyMicros = -1, -50 }),
			ed("e4", "c", "api"), ed("e5", "nobody", "db"), ed("e6", "db", "db"),
		}),
		"huge_cores": chain(func(n *archdoc.Node) { n.CPUCores, n.Instances, n.CPUMicrosPerReq = 448, 1000, 1 }),
	}
}

func TestAnalyzeDegenerateInputs(t *testing.T) {
	for name, d := range degenerateDesigns() {
		t.Run(name, func(t *testing.T) {
			r := Analyze(d, Options{Samples: 500})
			nonNegative(t, name, r)
			if r.ErrorPermille > 1000 {
				t.Fatalf("ErrorPermille = %d", r.ErrorPermille)
			}
			for id, n := range r.Nodes {
				if n.ErrPermille > 1000 {
					t.Fatalf("%s ErrPermille = %d", id, n.ErrPermille)
				}
			}
		})
	}
	if r := Analyze(degenerateDesigns()["instances_0"], Options{}); !r.Nodes["api"].Down || r.ErrorPermille != 1000 {
		t.Fatalf("instances 0: %+v errors %d", r.Nodes["api"], r.ErrorPermille)
	}
	if r := Analyze(degenerateDesigns()["nic_0"], Options{}); !r.Nodes["api"].Overloaded || r.Nodes["api"].Limit != LimitNIC {
		t.Fatalf("nic 0: %+v", r.Nodes["api"])
	}
	if r := Analyze(degenerateDesigns()["autoscale_minmax"], Options{}); r.Nodes["api"].AvgInstancesMilli != 10_000 {
		t.Fatalf("autoscale min>max: %+v", r.Nodes["api"])
	}
}

func testdataDesigns(t testing.TB) map[string]*archdoc.Design {
	t.Helper()
	files, err := filepath.Glob("../archdoc/testdata/designs/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no designs: %v", err)
	}
	out := map[string]*archdoc.Design{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := archdoc.Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[filepath.Base(f)] = d
	}
	return out
}

func TestAnalyzeSameTwice(t *testing.T) {
	for name, d := range testdataDesigns(t) {
		a, b := Analyze(d, Options{}).Canonical(), Analyze(d, Options{}).Canonical()
		if !bytes.Equal(a, b) {
			t.Fatalf("%s: two runs differ", name)
		}
	}
}

func TestAnalyzeSeedChangesSamplesOnly(t *testing.T) {
	d := design([]archdoc.Node{cl("c", 400, 800), nd("api", archdoc.KindAPI)}, []archdoc.Edge{ed("e1", "c", "api")})
	a := Analyze(d, Options{})
	d.Seed = 2
	b := Analyze(d, Options{})
	if a.Nodes["api"] != b.Nodes["api"] || a.RPS != b.RPS {
		t.Fatalf("seed changed the flow: %+v vs %+v", a.Nodes["api"], b.Nodes["api"])
	}
}

func TestDetExpClose(t *testing.T) {
	r := newRNG(3)
	for i := 0; i < 10_000; i++ {
		x := float64(r.f64()*1400) - 745
		got, want := detExp(x), math.Exp(x)
		if want == 0 || math.IsInf(want, 0) {
			continue
		}
		if math.Abs(got-want) > 2*(math.Nextafter(want, math.Inf(1))-want) {
			t.Fatalf("detExp(%v) = %v, math.Exp = %v", x, got, want)
		}
	}
	if detExp(0) != 1 || detExp(math.Inf(-1)) != 0 || !math.IsInf(detExp(1000), 1) || !math.IsNaN(detExp(math.NaN())) {
		t.Fatal("special cases")
	}
}

// bigDesign is a generated layered design of n nodes and about e edges.
func bigDesign(n, e int) *archdoc.Design {
	s := uint64(12345)
	next := func(k int) int {
		s = s*6364136223846793005 + 1442695040888963407
		return int((s >> 33) % uint64(k))
	}
	kinds := []archdoc.NodeKind{archdoc.KindLB, archdoc.KindAPI, archdoc.KindCache, archdoc.KindWorker, archdoc.KindSearch, archdoc.KindDBReplica, archdoc.KindDBPrimary}
	nodes := []archdoc.Node{cl("n000", 5000, 800)}
	for i := 1; i < n; i++ {
		layer := 1 + (i-1)*len(kinds)/(n-1)
		nodes = append(nodes, nd(id3("n", i), kinds[layer-1], func(x *archdoc.Node) { x.Pos = [2]int{layer * 20, i} }))
	}
	var edges []archdoc.Edge
	for i := 1; i < n && len(edges) < e; i++ { // every node reachable
		edges = append(edges, ed(id3("e", len(edges)), nodes[next(i)].ID, nodes[i].ID, shares(200, 200)))
	}
	for len(edges) < e {
		a, b := next(n-1), 0
		b = a + 1 + next(n-1-a)
		edges = append(edges, ed(id3("e", len(edges)), nodes[a].ID, nodes[b].ID, shares(100, 100)))
	}
	return design(nodes, edges)
}

func id3(p string, i int) string {
	s := strconv.Itoa(i)
	for len(s) < 3 {
		s = "0" + s
	}
	return p + s
}

func BenchmarkAnalyze300(b *testing.B) {
	d := bigDesign(300, 1000)
	b.ResetTimer()
	for b.Loop() {
		Analyze(d, Options{})
	}
}

func TestAnalyzeBudget(t *testing.T) {
	if os.Getenv("MASIL_PERF") != "1" {
		t.Skip("set MASIL_PERF=1")
	}
	d := bigDesign(300, 1000)
	var ds []time.Duration
	for range 20 {
		start := time.Now()
		Analyze(d, Options{})
		ds = append(ds, time.Since(start))
	}
	slices.Sort(ds)
	if med := ds[len(ds)/2]; med > 5*time.Millisecond {
		t.Fatalf("median %v > 5ms", med)
	}
}
