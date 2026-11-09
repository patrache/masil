// Package pricing holds the cloud price tables built into the app (spec C2.10,
// JC10). Prices are on-demand list prices entered by hand from each provider's
// public pages (tables/*.json: as_of month and source URLs). There is no network
// lookup: the same build always gives the same numbers. All money is integer
// micro-USD.
package pricing

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
)

// Provider is a cloud provider. Custom has no table: the node's own hourly price.
type Provider string

const (
	AWS    Provider = "aws"
	GCP    Provider = "gcp"
	Azure  Provider = "azure"
	Custom Provider = "custom"
)

// HoursPerMonth is the month length used for every monthly estimate.
const HoursPerMonth = 730

// InstanceType is one priced machine (or the representative unit of a managed
// service; see the table notes).
type InstanceType struct {
	Name           string
	VCPU           int
	MemoryMiB      int
	NICMbps        int
	HourlyMicroUSD int64
}

// Region is one region's prices.
type Region struct {
	Code                  string
	Instances             map[string]InstanceType
	DiskGBMonthMicroUSD   map[archdoc.DiskType]int64
	IOPSMonthMicroUSD     int64 // per provisioned IOPS above the disk's included amount
	EgressGBMicroUSD      int64 // internet egress
	LBHourlyMicroUSD      int64
	LBGBMicroUSD          int64 // processed data, approximating capacity units
	ObjectGBMonthMicroUSD int64
	Managed               map[archdoc.NodeKind]string // representative managed instance type per kind
}

// Table is one provider's price table.
type Table struct {
	Provider Provider
	AsOf     string // "2026-11"
	Sources  []string
	Regions  map[string]Region
	defaults map[archdoc.NodeKind]string
}

//go:embed tables/*.json
var tableFS embed.FS

type fileInstance struct {
	Name           string `json:"name"`
	VCPU           int    `json:"vcpu"`
	MemoryMiB      int    `json:"memory_mib"`
	NICMbps        int    `json:"nic_mbps"`
	HourlyMicroUSD int64  `json:"hourly_micro_usd"`
}

type fileRegion struct {
	Instances             []fileInstance              `json:"instances"`
	DiskGBMonthMicroUSD   map[archdoc.DiskType]int64  `json:"disk_gb_month_micro_usd"`
	IOPSMonthMicroUSD     int64                       `json:"iops_month_micro_usd"`
	EgressGBMicroUSD      int64                       `json:"egress_gb_micro_usd"`
	LBHourlyMicroUSD      int64                       `json:"lb_hourly_micro_usd"`
	LBGBMicroUSD          int64                       `json:"lb_gb_micro_usd"`
	ObjectGBMonthMicroUSD int64                       `json:"object_gb_month_micro_usd"`
	Managed               map[archdoc.NodeKind]string `json:"managed"`
}

type fileTable struct {
	Provider Provider                    `json:"provider"`
	AsOf     string                      `json:"as_of"`
	Sources  []string                    `json:"sources"`
	Defaults map[archdoc.NodeKind]string `json:"defaults"`
	Regions  map[string]fileRegion       `json:"regions"`
}

var tables = mustLoad()

func mustLoad() []Table {
	var out []Table
	for _, p := range []Provider{AWS, GCP, Azure} {
		data, err := tableFS.ReadFile("tables/" + string(p) + ".json")
		if err != nil {
			panic(fmt.Sprintf("pricing: %v", err))
		}
		var ft fileTable
		if err := json.Unmarshal(data, &ft); err != nil || ft.Provider != p {
			panic(fmt.Sprintf("pricing: tables/%s.json: %v", p, err))
		}
		t := Table{Provider: p, AsOf: ft.AsOf, Sources: ft.Sources, Regions: map[string]Region{}, defaults: ft.Defaults}
		for code, fr := range ft.Regions {
			r := Region{
				Code: code, Instances: map[string]InstanceType{}, DiskGBMonthMicroUSD: fr.DiskGBMonthMicroUSD,
				IOPSMonthMicroUSD: fr.IOPSMonthMicroUSD, EgressGBMicroUSD: fr.EgressGBMicroUSD,
				LBHourlyMicroUSD: fr.LBHourlyMicroUSD, LBGBMicroUSD: fr.LBGBMicroUSD,
				ObjectGBMonthMicroUSD: fr.ObjectGBMonthMicroUSD, Managed: fr.Managed,
			}
			for _, fi := range fr.Instances {
				r.Instances[fi.Name] = InstanceType(fi)
			}
			t.Regions[code] = r
		}
		out = append(out, t)
	}
	return out
}

// Tables returns the AWS, GCP and Azure tables in that order. Callers must not
// change the maps.
func Tables() []Table { return slices.Clone(tables) }

func tableOf(p Provider) (Table, bool) {
	for _, t := range tables {
		if t.Provider == p {
			return t, true
		}
	}
	return Table{}, false
}

// RegionOf returns one region's prices.
func RegionOf(p Provider, region string) (Region, bool) {
	t, ok := tableOf(p)
	if !ok {
		return Region{}, false
	}
	r, ok := t.Regions[region]
	return r, ok
}

// Lookup finds an instance type in a region.
func Lookup(p Provider, region, instance string) (InstanceType, bool) {
	r, ok := RegionOf(p, region)
	if !ok {
		return InstanceType{}, false
	}
	it, ok := r.Instances[instance]
	return it, ok
}

// Instances lists a region's instance types sorted by name (nil if unknown).
func Instances(p Provider, region string) []InstanceType {
	r, ok := RegionOf(p, region)
	if !ok {
		return nil
	}
	out := slices.Collect(maps.Values(r.Instances))
	slices.SortFunc(out, func(a, b InstanceType) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Regions lists a provider's region codes sorted (nil for Custom).
func Regions(p Provider) []string {
	t, ok := tableOf(p)
	if !ok {
		return nil
	}
	return slices.Sorted(maps.Keys(t.Regions))
}

// DefaultType is the instance type a new node of kind starts with ("" for
// client, CDN and external, which are not priced per instance). It exists in
// every region of the provider.
func DefaultType(p Provider, kind archdoc.NodeKind) string {
	t, _ := tableOf(p)
	return t.defaults[kind]
}

// AsOf is the oldest table date — the one to show next to estimates.
func AsOf() string {
	oldest := ""
	for _, t := range tables {
		if oldest == "" || t.AsOf < oldest {
			oldest = t.AsOf
		}
	}
	return oldest
}

// metro groups region codes that sit in the same city across providers.
var metro = map[string]string{
	"ap-northeast-2": "seoul", "asia-northeast3": "seoul", "koreacentral": "seoul",
	"ap-northeast-1": "tokyo",
	"us-east-1":      "virginia", "eastus": "virginia",
	"us-central1":  "iowa",
	"eu-central-1": "frankfurt",
}

// metroRTT is the default round-trip latency between metros in microseconds
// (typical published inter-region figures, rounded). Keys are sorted pairs.
var metroRTT = sortedPairs(map[[2]string]int{
	{"seoul", "tokyo"}:        33_000,
	{"seoul", "virginia"}:     180_000,
	{"seoul", "iowa"}:         160_000,
	{"seoul", "frankfurt"}:    230_000,
	{"tokyo", "virginia"}:     150_000,
	{"tokyo", "iowa"}:         130_000,
	{"tokyo", "frankfurt"}:    225_000,
	{"iowa", "virginia"}:      30_000,
	{"frankfurt", "virginia"}: 90_000,
	{"frankfurt", "iowa"}:     110_000,
})

func sortedPairs(m map[[2]string]int) map[[2]string]int {
	out := make(map[[2]string]int, len(m))
	for k, v := range m {
		if k[0] > k[1] {
			k[0], k[1] = k[1], k[0]
		}
		out[k] = v
	}
	return out
}

const (
	sameMetroMicros    = 2_000
	unknownRegionMicro = 150_000
)

// RegionLatencyMicros is the default latency of a link between two regions:
// 0 within one region, 2ms between providers in one city, the metro table
// otherwise and 150ms when a region is unknown.
func RegionLatencyMicros(a, b string) int {
	if a == b && a != "" {
		return 0
	}
	ma, mb := metro[a], metro[b]
	if ma == "" || mb == "" {
		return unknownRegionMicro
	}
	if ma == mb {
		return sameMetroMicros
	}
	if ma > mb {
		ma, mb = mb, ma
	}
	if v, ok := metroRTT[[2]string{ma, mb}]; ok {
		return v
	}
	return unknownRegionMicro
}
