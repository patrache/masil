package archsim

import (
	"math"
	"testing"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
)

func TestMonthlyCostByHand(t *testing.T) {
	api := nd("api", archdoc.KindAPI, func(n *archdoc.Node) {
		n.InstanceType, n.Region, n.Instances, n.Disk.SizeGB = "c7g.xlarge", "ap-northeast-2", 2, 0
	})
	d := design([]archdoc.Node{api}, nil)
	it, ok := pricing.Lookup(pricing.AWS, "ap-northeast-2", "c7g.xlarge")
	if !ok {
		t.Fatal("c7g.xlarge missing")
	}
	want := int(math.Round(float64(2*it.HourlyMicroUSD*730) / 1e6))
	c := MonthlyCost(d, nil)
	if c.TotalUSDMonth != want || c.ByNode["api"] != want || c.AsOf != pricing.AsOf() {
		t.Fatalf("cost = %+v, want %d", c, want)
	}
	if len(c.Top3) != 1 || c.Top3[0] != "api" {
		t.Fatalf("top3 = %v", c.Top3)
	}
	if got := MonthlyCost(d, map[string]int{"api": 1000}).TotalUSDMonth; got != int(math.Round(float64(it.HourlyMicroUSD*730)/1e6)) {
		t.Fatalf("one average instance = %d", got)
	}
	if r := Analyze(d, Options{}); r.Cost.TotalUSDMonth != want || r.Nodes["api"].CostUSDMonth != want {
		t.Fatalf("analyze cost = %+v", r.Cost)
	}
}

func TestMonthlyCostRegionFromBox(t *testing.T) {
	api := nd("api", archdoc.KindAPI, func(n *archdoc.Node) { n.Parent, n.Disk.SizeGB = "seoul", 0 })
	d := design([]archdoc.Node{api}, nil)
	d.Boxes = []archdoc.Box{{ID: "seoul", Kind: archdoc.BoxRegion, RegionCode: "ap-northeast-2", Rect: [4]int{0, 0, 40, 20}}}
	it, _ := pricing.Lookup(pricing.AWS, "ap-northeast-2", "c7g.large")
	if got, want := MonthlyCost(d, nil).TotalUSDMonth, int(math.Round(float64(2*it.HourlyMicroUSD*730)/1e6)); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
}

func TestMonthlyCostDiskEgressCustom(t *testing.T) {
	c := cl("c", 100, 1000, func(n *archdoc.Node) { n.Traffic.RespKB = 1024 })
	api := nd("api", archdoc.KindAPI, func(n *archdoc.Node) { n.Region = "us-east-1"; n.Instances = 1 })
	own := nd("own", archdoc.KindAPI, func(n *archdoc.Node) {
		n.Provider, n.CustomHourlyMicroUSD, n.Instances, n.Disk.SizeGB = "custom", 1_000_000, 3, 0
	})
	d := design([]archdoc.Node{c, api, own}, []archdoc.Edge{ed("e1", "c", "api")})
	r, _ := pricing.RegionOf(pricing.AWS, "us-east-1")
	it := r.Instances["c7g.large"]
	// 100 rps × 1 MiB × 2,628,000 s = 256,640.625 GiB of egress.
	egress := float64(100*1024*2_628_000) / (1024 * 1024) * float64(r.EgressGBMicroUSD)
	disk := float64(100 * r.DiskGBMonthMicroUSD[archdoc.DiskSSD])
	apiWant := int(math.Round((float64(it.HourlyMicroUSD*730) + disk + egress) / 1e6))
	got := MonthlyCost(d, nil)
	if got.ByNode["api"] != apiWant {
		t.Fatalf("api = %d, want %d", got.ByNode["api"], apiWant)
	}
	if got.ByNode["own"] != 2190 || got.ByNode["c"] != 0 {
		t.Fatalf("own = %d client = %d", got.ByNode["own"], got.ByNode["c"])
	}
	if got.Top3[0] != "api" || got.Top3[1] != "own" || len(got.Top3) != 2 {
		t.Fatalf("top3 = %v", got.Top3)
	}
}
