// Package archdoc is the architecture design model (spec C2): the design JSON v1
// types, canonical codec and validation, the mapping onto collab objects, design
// checks, auto-arrange, the own-edit undo log and the personal design store.
// It has no UI and no network access. The contract is docs/contract/arch.md.
package archdoc

import (
	"hash/fnv"

	"github.com/patrache/masil/internal/ids"
)

// SchemaVersion is the design JSON version this package reads and writes.
const SchemaVersion = 1

// Limits (JC14).
const (
	MaxNodes  = 300
	MaxEdges  = 1000
	MaxBoxes  = 100
	MaxFaults = 50
	MaxDepth  = 3       // box nesting levels
	MaxBytes  = 1 << 20 // encoded design
	MaxLabel  = 40      // graphemes
	MaxTitle  = 80      // graphemes
)

// NodeW and NodeH are a node's size in world cells (= cells at the detailed zoom).
// World x coordinates sit on an even grid so double-width text never splits.
const NodeW, NodeH = 12, 4

// Design is one architecture design. Every number is an integer so NaN and
// infinities cannot exist in a design.
type Design struct {
	SchemaVersion int     `json:"schema_version"`
	Title         string  `json:"title"`
	Seed          uint64  `json:"seed"`
	DurationS     int     `json:"duration_s"`
	Nodes         []Node  `json:"nodes"`
	Boxes         []Box   `json:"boxes"`
	Edges         []Edge  `json:"edges"`
	Faults        []Fault `json:"faults"`
}

// Node is one component.
type Node struct {
	ID                   string    `json:"id"`
	Kind                 NodeKind  `json:"kind"`
	Label                string    `json:"label"`
	Pos                  [2]int    `json:"pos"`
	Parent               string    `json:"parent"` // box ID, "" = top level
	Provider             string    `json:"provider"`
	InstanceType         string    `json:"instance_type"`
	Region               string    `json:"region"` // "" = from the enclosing region box
	CPUCores             int       `json:"cpu_cores"`
	MemoryMiB            int       `json:"memory_mib"`
	Instances            int       `json:"instances"`
	Autoscale            Autoscale `json:"autoscale"`
	NICMbps              int       `json:"nic_mbps"`
	Disk                 Disk      `json:"disk"`
	CPUMicrosPerReq      int       `json:"cpu_micros_per_req"`
	TimeoutMs            int       `json:"timeout_ms"`
	Retries              int       `json:"retries"`
	QueueCap             int       `json:"queue_cap"`
	CustomHourlyMicroUSD int64     `json:"custom_hourly_micro_usd"`
	Params               Params    `json:"params"`
	Traffic              *Traffic  `json:"traffic,omitempty"` // client nodes only
}

// Autoscale is a node's autoscaling policy.
type Autoscale struct {
	Enabled           bool `json:"enabled"`
	Min               int  `json:"min"`
	Max               int  `json:"max"`
	TargetCPUPermille int  `json:"target_cpu_permille"`
	WarmupS           int  `json:"warmup_s"`
	CooldownS         int  `json:"cooldown_s"`
}

// Disk is a node's attached disk.
type Disk struct {
	Type   DiskType `json:"type"`
	SizeGB int      `json:"size_gb"`
	IOPS   int      `json:"iops"`
}

// Params are the kind-specific settings (C2.8 "부품별 추가 설정").
type Params struct {
	HitPermille        int    `json:"hit_permille"`
	WorkingSetMiB      int    `json:"working_set_mib"`
	TTLS               int    `json:"ttl_s"`
	LBAlgo             LBAlgo `json:"lb_algo"`
	HealthIntervalS    int    `json:"health_interval_s"`
	ReadIOPSMilli      int    `json:"read_iops_milli"`
	WriteIOPSMilli     int    `json:"write_iops_milli"`
	PoolMax            int    `json:"pool_max"`
	FailoverTo         string `json:"failover_to"` // node ID, "" = none
	FailoverS          int    `json:"failover_s"`
	ReplLagMs          int    `json:"repl_lag_ms"`
	MaxRPS             int    `json:"max_rps"`
	RetentionMsgs      int    `json:"retention_msgs"`
	FixedLatencyMs     int    `json:"fixed_latency_ms"`
	ErrorPermille      int    `json:"error_permille"`
	IndexWritePermille int    `json:"index_write_permille"`
}

// Traffic is the client traffic model (C2.9).
type Traffic struct {
	RPS          int   `json:"rps"`
	ReadPermille int   `json:"read_permille"`
	ReqKB        int   `json:"req_kb"`
	RespKB       int   `json:"resp_kb"`
	Curve        Curve `json:"curve"`
	SpikeMult    int   `json:"spike_mult"`
	SpikeAtS     int   `json:"spike_at_s"`
	SpikeDurS    int   `json:"spike_dur_s"`
}

// Box is a region, VPC or AZ.
type Box struct {
	ID         string  `json:"id"`
	Kind       BoxKind `json:"kind"`
	Label      string  `json:"label"`
	RegionCode string  `json:"region_code"`
	Rect       [4]int  `json:"rect"` // x, y, w, h
	Parent     string  `json:"parent"`
}

// Edge is a directed connection.
type Edge struct {
	ID            string   `json:"id"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Protocol      Protocol `json:"protocol"`
	Mbps          int      `json:"mbps"`           // 0 = the endpoints' NICs
	LatencyMicros int      `json:"latency_micros"` // -1 = topology default
	ReadPermille  int      `json:"read_permille"`
	WritePermille int      `json:"write_permille"`
}

// Fault is a scheduled fault for the timed run.
type Fault struct {
	ID     string    `json:"id"`
	AtS    int       `json:"at_s"`
	Kind   FaultKind `json:"kind"`
	Target string    `json:"target"`
	Count  int       `json:"count"`
	AddMs  int       `json:"add_ms"`
	DurS   int       `json:"dur_s"` // 0 = until the end
}

// New returns an empty design. The seed comes from a fresh ULID and is kept below
// 2^53 so it survives JavaScript numbers on the server.
func New(title string) *Design {
	h := fnv.New64a()
	_, _ = h.Write([]byte(ids.New()))
	return &Design{SchemaVersion: SchemaVersion, Title: title, Seed: h.Sum64() & (1<<53 - 1), DurationS: 300}
}

// NewID returns "<prefix>_<ULID>" ('n' node, 'b' box, 'e' edge, 'f' fault).
func NewID(prefix byte) string {
	return string([]byte{prefix, '_'}) + ids.New()
}

// DefaultEdge returns an edge with default settings and no endpoints.
func DefaultEdge() Edge {
	return Edge{Protocol: ProtoHTTP1, LatencyMicros: -1, ReadPermille: 1000, WritePermille: 1000}
}

// DefaultTraffic is the client traffic model a new client starts with.
func DefaultTraffic() Traffic {
	return Traffic{RPS: 100, ReadPermille: 800, ReqKB: 2, RespKB: 16, Curve: CurveConstant, SpikeMult: 5, SpikeAtS: 60, SpikeDurS: 30}
}

// Defaults returns a node of kind with that kind's default settings (no ID, label
// or position). The AWS instance types match pricing.DefaultType (checked there).
// An unknown kind gets the API defaults with the kind kept.
func Defaults(kind NodeKind) Node {
	n := Node{
		Kind:            kind,
		Provider:        "aws",
		CPUCores:        2,
		MemoryMiB:       4096,
		Instances:       2,
		Autoscale:       Autoscale{Min: 2, Max: 10, TargetCPUPermille: 600, WarmupS: 90, CooldownS: 300},
		NICMbps:         12500,
		Disk:            Disk{Type: DiskSSD, SizeGB: 100, IOPS: 3000},
		CPUMicrosPerReq: 8000,
		TimeoutMs:       2000,
		QueueCap:        1000,
	}
	noDisk := Disk{Type: DiskSSD}
	switch kind {
	case KindClient:
		t := DefaultTraffic()
		n.Provider, n.CPUCores, n.MemoryMiB, n.Instances, n.NICMbps = "", 1, 1024, 1, 10000
		n.Disk, n.CPUMicrosPerReq, n.Traffic = noDisk, 1, &t
	case KindCDN:
		n.CPUCores, n.MemoryMiB, n.Instances, n.NICMbps = 64, 131072, 1, 400000
		n.Disk, n.CPUMicrosPerReq = noDisk, 50
		n.Params.HitPermille = 800
	case KindExternal:
		n.Provider, n.CPUCores, n.MemoryMiB, n.Instances, n.NICMbps = "", 64, 1024, 1, 100000
		n.Disk, n.CPUMicrosPerReq = noDisk, 1
		n.Params.FixedLatencyMs, n.Params.MaxRPS = 100, 1000
	case KindLB:
		n.InstanceType, n.NICMbps, n.Disk, n.CPUMicrosPerReq = "alb", 100000, noDisk, 100
		n.Params.LBAlgo, n.Params.HealthIntervalS = LBRoundRobin, 10
	case KindWorker:
		n.InstanceType, n.CPUMicrosPerReq = "c7g.large", 20000
	case KindCache:
		n.InstanceType, n.MemoryMiB, n.Disk, n.CPUMicrosPerReq = "cache.r7g.large", 13384, noDisk, 200
		n.Params.HitPermille, n.Params.WorkingSetMiB, n.Params.TTLS = 900, 4096, 300
	case KindDBPrimary:
		n.InstanceType, n.MemoryMiB, n.Instances, n.CPUMicrosPerReq = "db.r7g.large", 16384, 1, 2000
		n.Params.ReadIOPSMilli, n.Params.WriteIOPSMilli, n.Params.PoolMax, n.Params.FailoverS = 500, 1250, 100, 30
	case KindDBReplica:
		n.InstanceType, n.MemoryMiB, n.Instances, n.CPUMicrosPerReq = "db.r7g.large", 16384, 1, 2000
		n.Params.ReadIOPSMilli, n.Params.PoolMax, n.Params.ReplLagMs = 500, 100, 20
	case KindQueue:
		n.InstanceType, n.MemoryMiB, n.Instances, n.NICMbps = "mq.m5.large", 8192, 1, 10000
		n.Disk, n.CPUMicrosPerReq = noDisk, 100
		n.Params.MaxRPS, n.Params.RetentionMsgs = 10000, 1000000
	case KindObjectStorage:
		n.InstanceType, n.CPUCores, n.MemoryMiB, n.Instances, n.NICMbps = "s3", 64, 1024, 1, 100000
		n.Disk, n.CPUMicrosPerReq = noDisk, 100
		n.Params.MaxRPS, n.Params.FixedLatencyMs = 5500, 20
	case KindSearch:
		n.InstanceType, n.MemoryMiB, n.CPUMicrosPerReq = "r7g.large.search", 16384, 30000
		n.Params.IndexWritePermille = 100
	default: // KindAPI and unknown kinds
		n.InstanceType = "c7g.large"
	}
	return n
}
