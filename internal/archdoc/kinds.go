package archdoc

// NodeKind is the component type of a node (spec C2.8).
type NodeKind string

const (
	KindClient        NodeKind = "client"
	KindCDN           NodeKind = "cdn"
	KindLB            NodeKind = "lb"
	KindAPI           NodeKind = "api"
	KindWorker        NodeKind = "worker"
	KindCache         NodeKind = "cache"
	KindDBPrimary     NodeKind = "db_primary"
	KindDBReplica     NodeKind = "db_replica"
	KindQueue         NodeKind = "queue"
	KindObjectStorage NodeKind = "object_storage"
	KindSearch        NodeKind = "search"
	KindExternal      NodeKind = "external"
)

// NodeKinds lists every node kind in hub palette order.
func NodeKinds() []NodeKind {
	return []NodeKind{
		KindClient, KindCDN, KindLB, KindAPI, KindWorker, KindCache,
		KindDBPrimary, KindDBReplica, KindQueue, KindObjectStorage, KindSearch, KindExternal,
	}
}

// BoxKind is the kind of a grouping box.
type BoxKind string

const (
	BoxRegion BoxKind = "region"
	BoxVPC    BoxKind = "vpc"
	BoxAZ     BoxKind = "az"
)

// BoxKinds lists every box kind, outermost first.
func BoxKinds() []BoxKind { return []BoxKind{BoxRegion, BoxVPC, BoxAZ} }

// Protocol is an edge protocol.
type Protocol string

const (
	ProtoHTTP1 Protocol = "http1"
	ProtoHTTP2 Protocol = "http2"
	ProtoGRPC  Protocol = "grpc"
	ProtoTCP   Protocol = "tcp"
	ProtoAsync Protocol = "async"
)

// Curve is the time shape of client traffic.
type Curve string

const (
	CurveConstant Curve = "constant"
	CurveRamp     Curve = "ramp"
	CurveSpike    Curve = "spike"
	CurveWave     Curve = "wave"
)

// FaultKind is a scheduled fault.
type FaultKind string

const (
	FaultNodeOff      FaultKind = "node_off"
	FaultInstancesOff FaultKind = "instances_off"
	FaultLatency      FaultKind = "latency"
	FaultAZOff        FaultKind = "az_off"
	FaultRegionOff    FaultKind = "region_off"
)

// DiskType is a disk class.
type DiskType string

const (
	DiskSSD     DiskType = "ssd"
	DiskSSDFast DiskType = "ssd_fast"
	DiskHDD     DiskType = "hdd"
)

// DiskTypes lists every disk type.
func DiskTypes() []DiskType { return []DiskType{DiskSSD, DiskSSDFast, DiskHDD} }

// LBAlgo is a load-balancing algorithm.
type LBAlgo string

const (
	LBRoundRobin LBAlgo = "round_robin"
	LBLeastConn  LBAlgo = "least_conn"
)

func strs[T ~string](vs ...T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

var (
	nodeKindNames  = strs(NodeKinds()...)
	boxKindNames   = strs(BoxKinds()...)
	protocolNames  = strs(ProtoHTTP1, ProtoHTTP2, ProtoGRPC, ProtoTCP, ProtoAsync)
	curveNames     = strs(CurveConstant, CurveRamp, CurveSpike, CurveWave)
	faultKindNames = strs(FaultNodeOff, FaultInstancesOff, FaultLatency, FaultAZOff, FaultRegionOff)
	diskTypeNames  = strs(DiskTypes()...)
	lbAlgoNames    = strs("", LBRoundRobin, LBLeastConn) // "" = not a load balancer
)
