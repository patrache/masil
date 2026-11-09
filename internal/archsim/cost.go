package archsim

import (
	"cmp"
	"math"
	"slices"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
)

const (
	secondsPerMonth = pricing.HoursPerMonth * 3600 // 2,628,000
	kbPerGB         = 1024 * 1024
	includedSSDIOPS = 3000 // general-purpose SSD baseline included in the GB price
)

// baseRegion is the region priced when a node has none (or one the table
// lacks): each table's reference region.
var baseRegion = map[pricing.Provider]string{pricing.AWS: "us-east-1", pricing.GCP: "us-central1", pricing.Azure: "eastus"}

func (m *model) priceRegion(p pricing.Provider, region string) (pricing.Region, bool) {
	if r, ok := pricing.RegionOf(p, region); ok {
		return r, true
	}
	return pricing.RegionOf(p, baseRegion[p])
}

// MonthlyCost is the monthly estimate (C2.10) with each node at the given
// average instance count (milli-instances; a missing node uses its own
// instance count). Usage-priced parts (egress, LB data) come from a
// steady-state pass of the client traffic.
func MonthlyCost(d *archdoc.Design, avgInstancesMilli map[string]int) Cost {
	if d == nil {
		d = &archdoc.Design{}
	}
	m := newModel(d)
	in := &passInput{rps: m.clientRPS()}
	fs, _ := m.steady(in)
	inst := make([]float64, len(m.nodes))
	for u := range m.nodes {
		if v, ok := avgInstancesMilli[m.nodes[u].n.ID]; ok {
			inst[u] = float64(max(v, 0)) / 1000
		} else {
			inst[u] = baseInstances(m.nodes[u].n)
		}
	}
	return m.cost(inst, fs, in.rps)
}

// cost prices every node: hourly × 730 × instances, disk GB and provisioned
// IOPS, object storage GB, LB processed data, and internet egress (response
// size × client requests × a month) charged to the client's first hops. The
// LB hour is its instance price, counted once. Custom nodes use their own
// hourly price; client and external nodes cost nothing.
func (m *model) cost(inst []float64, fs *flowState, rps []float64) Cost {
	micro := make([]float64, len(m.nodes))
	for u := range m.nodes {
		micro[u] = m.nodeMicro(u, inst[u], fs)
	}
	for _, c := range m.clients {
		if !(rps[c] > 0) {
			continue
		}
		read, _ := trafficOf(m.nodes[c].n)
		resp := float64(max(m.nodes[c].n.Traffic.RespKB, 0))
		for cls, share := range [2]float64{read, 1 - read} {
			for _, rt := range m.nodes[c].routes[cls] {
				v := m.edges[rt.e].to
				region, ok := m.nodeRegion(v)
				if !ok {
					continue
				}
				gb := mul(mul(mul(mul(rps[c], share), rt.frac), resp), secondsPerMonth) / kbPerGB
				micro[v] += mul(gb, float64(region.EgressGBMicroUSD))
			}
		}
	}
	out := Cost{ByNode: map[string]int{}, AsOf: pricing.AsOf()}
	total := 0.0
	var ranked []int
	for u := range m.nodes {
		if !(micro[u] > 0) {
			micro[u] = 0
		}
		micro[u] = math.Min(micro[u], maxInt)
		total += micro[u]
		out.ByNode[m.nodes[u].n.ID] = toInt(micro[u] / 1e6)
		if micro[u] > 0 {
			ranked = append(ranked, u)
		}
	}
	slices.SortStableFunc(ranked, func(a, b int) int { return cmp.Compare(micro[b], micro[a]) })
	for _, u := range ranked[:min(3, len(ranked))] {
		out.Top3 = append(out.Top3, m.nodes[u].n.ID)
	}
	out.TotalUSDMonth = toInt(total / 1e6)
	return out
}

// nodeRegion is the price region of a priced (aws, gcp, azure) node.
func (m *model) nodeRegion(u int) (pricing.Region, bool) {
	n := m.nodes[u].n
	switch p := pricing.Provider(n.Provider); p {
	case pricing.AWS, pricing.GCP, pricing.Azure:
		return m.priceRegion(p, m.nodes[u].region)
	}
	return pricing.Region{}, false
}

func (m *model) nodeMicro(u int, inst float64, fs *flowState) float64 {
	n := m.nodes[u].n
	if n.Kind == archdoc.KindClient || n.Kind == archdoc.KindExternal || !(inst > 0) {
		return 0
	}
	hours := mul(pricing.HoursPerMonth, inst)
	if pricing.Provider(n.Provider) == pricing.Custom {
		return mul(float64(max(n.CustomHourlyMicroUSD, 0)), hours)
	}
	region, ok := m.nodeRegion(u)
	if !ok {
		return 0
	}
	total := 0.0
	if it, ok := region.Instances[n.InstanceType]; ok {
		total += mul(float64(it.HourlyMicroUSD), hours)
	}
	size := float64(max(n.Disk.SizeGB, 0))
	switch n.Kind {
	case archdoc.KindObjectStorage:
		total += mul(size, float64(region.ObjectGBMonthMicroUSD))
	case archdoc.KindLB:
		kbps := fs.k[u][0] + fs.k[u][1]
		total += mul(mul(kbps, secondsPerMonth)/kbPerGB, float64(region.LBGBMicroUSD))
	case archdoc.KindCDN:
	default:
		if size > 0 {
			disk := mul(size, float64(region.DiskGBMonthMicroUSD[n.Disk.Type]))
			iops := float64(max(n.Disk.IOPS, 0))
			switch n.Disk.Type {
			case archdoc.DiskSSD:
				iops = math.Max(iops-includedSSDIOPS, 0)
			case archdoc.DiskHDD:
				iops = 0
			}
			disk += mul(iops, float64(region.IOPSMonthMicroUSD))
			total += mul(disk, inst)
		}
	}
	return total
}
