package archsim

import (
	"encoding/json"

	"github.com/patrache/masil/internal/archdoc"
)

// EngineVersion changes whenever the same design and seed can give different
// numbers. Results and cards carry it so a reader with another engine can say
// the numbers may differ.
const EngineVersion = 1

// Options tunes Analyze.
type Options struct {
	Samples int // end-to-end latency samples; 0 = 20,000
}

const defaultSamples = 20_000

// LimitKind names what fills up first at a node or edge.
type LimitKind string

const (
	LimitCPU   LimitKind = "cpu"
	LimitIOPS  LimitKind = "iops"
	LimitNIC   LimitKind = "nic"
	LimitPool  LimitKind = "pool"
	LimitExt   LimitKind = "ext_limit"
	LimitQueue LimitKind = "queue"
	LimitLink  LimitKind = "link"
)

// Result is one analysis. Every number is a rounded integer (requests/s, ms,
// permille, dollars) so the same design and seed give the same bytes everywhere.
type Result struct {
	EngineVersion int                   `json:"engine_version"`
	Nodes         map[string]NodeResult `json:"nodes"`
	Edges         map[string]EdgeResult `json:"edges"`
	RPS           int                   `json:"rps"` // client requests/s
	P50Ms         int                   `json:"p50_ms"`
	P99Ms         int                   `json:"p99_ms"`
	ErrorPermille int                   `json:"error_permille"`
	Overloaded    bool                  `json:"overloaded"`
	Bottleneck    *Bottleneck           `json:"bottleneck"`
	Cost          Cost                  `json:"cost"`
	Findings      []archdoc.Finding     `json:"findings"`
}

// NodeResult is one node. InRPS counts attempts (retries included).
// CapacityRPS is 0 when nothing limits the node.
type NodeResult struct {
	InRPS             int       `json:"in_rps"`
	CapacityRPS       int       `json:"capacity_rps"`
	UtilPermille      int       `json:"util_permille"`
	Limit             LimitKind `json:"limit"`
	P50Ms             int       `json:"p50_ms"`
	P99Ms             int       `json:"p99_ms"`
	ErrPermille       int       `json:"err_permille"` // failed share of attempts
	Overloaded        bool      `json:"overloaded"`
	Down              bool      `json:"down"`
	AvgInstancesMilli int       `json:"avg_instances_milli"`
	CostUSDMonth      int       `json:"cost_usd_month"`
}

// EdgeResult is one edge. UtilPermille is against the edge's own bandwidth and
// is 0 when the edge has none (the endpoints' NICs limit instead).
type EdgeResult struct {
	RPS           int `json:"rps"`
	UtilPermille  int `json:"util_permille"`
	LatencyMicros int `json:"latency_micros"`
}

// Bottleneck is the node or edge that fills first, with the numbers behind it:
// Demand per second × PerUnitMilli/1000 of the limited resource per request
// (cpu and pool: CPU µs per request, iops: milli-IOPS of the dominant class,
// nic and link: KB per request ×1000, ext_limit and queue: 1000).
type Bottleneck struct {
	Target       string    `json:"target"`
	Limit        LimitKind `json:"limit"`
	UtilPermille int       `json:"util_permille"`
	Demand       int       `json:"demand"`
	PerUnitMilli int       `json:"per_unit_milli"`
}

// Cost is the monthly estimate in whole dollars at the price tables of AsOf.
type Cost struct {
	TotalUSDMonth int            `json:"total_usd_month"`
	ByNode        map[string]int `json:"by_node"`
	Top3          []string       `json:"top3"` // most expensive node IDs, cost > 0
	AsOf          string         `json:"as_of"`
}

// Canonical is the result as JSON with sorted map keys — the bytes goldens and
// card replays compare.
func (r *Result) Canonical() []byte {
	b, err := json.Marshal(r)
	if err != nil { // only ints, strings and bools: cannot fail
		panic(err)
	}
	return b
}
