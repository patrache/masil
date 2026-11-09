package arch

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
)

// unit is how a field's stored integer is typed and shown: the stored value
// is the typed one × its scale.
type unit int

const (
	unitNone  unit = iota
	unitGiB        // stored MiB, typed GiB
	unitPct        // stored permille, typed percent
	unitCents      // stored micro-USD, typed cents
)

func (u unit) scale() int64 {
	switch u {
	case unitGiB:
		return 1024
	case unitPct:
		return 10
	case unitCents:
		return 10_000
	}
	return 1
}

// inputKind is how a field is edited.
type inputKind int

const (
	inputInt    inputKind = iota // typed number in [Min, Max]
	inputChoice                  // a fixed list (provider, region, type, enums)
	inputBool                    // enter toggles
	inputRef                     // another node (failover target)
	inputText                    // a label (40 graphemes)
)

// fieldOn is which object a field belongs to.
type fieldOn int

const (
	onSel    fieldOn = iota // the selected node, box or edge
	onClient                // the first client (ID order): the traffic model
	onMeta                  // the design itself (seed, run length)
)

// fieldDef is one row of the settings panel (spec C2.8): Field is the
// archdoc object field name (arch.md 2.1), Min and Max are in typed units,
// Kinds limits a node field to those kinds (nil = every computing node).
type fieldDef struct {
	Field string
	Label func(t *i18n.T) string
	Unit  unit
	Min   int64
	Max   int64
	Kinds []archdoc.NodeKind
	Input inputKind
	On    fieldOn
}

// computing are the kinds the common settings apply to (client and external
// have none: traffic, and a service we cannot change).
var computing = []archdoc.NodeKind{
	archdoc.KindCDN, archdoc.KindLB, archdoc.KindAPI, archdoc.KindWorker, archdoc.KindCache,
	archdoc.KindDBPrimary, archdoc.KindDBReplica, archdoc.KindQueue, archdoc.KindObjectStorage, archdoc.KindSearch,
}

func lbl(f func(a i18n.ArchcanvasT) string) func(t *i18n.T) string {
	return func(t *i18n.T) string { return f(t.Archcanvas) }
}

// field label shorthands
var (
	labelField = fieldDef{Field: "label", Label: lbl(i18n.ArchcanvasT.FieldLabel), Input: inputText}
)

// nodeFields are the node settings: the common ones (C2.8 공통 설정) then the
// kind-specific ones.
var nodeFields = []fieldDef{
	labelField,
	{Field: "provider", Label: lbl(i18n.ArchcanvasT.FieldProvider), Input: inputChoice, Kinds: computing},
	{Field: "region", Label: lbl(i18n.ArchcanvasT.FieldRegion), Input: inputChoice, Kinds: computing},
	{Field: "instance_type", Label: lbl(i18n.ArchcanvasT.FieldInstanceType), Input: inputChoice, Kinds: computing},
	{Field: "custom_hourly_micro_usd", Label: lbl(i18n.ArchcanvasT.FieldCustomHourly), Unit: unitCents, Min: 0, Max: 100_000_000, Kinds: computing},
	{Field: "cpu_cores", Label: lbl(i18n.ArchcanvasT.FieldCpuCores), Min: 1, Max: 448, Kinds: computing},
	{Field: "memory_mib", Label: lbl(i18n.ArchcanvasT.FieldMemory), Unit: unitGiB, Min: 1, Max: 24_000, Kinds: computing},
	{Field: "instances", Label: lbl(i18n.ArchcanvasT.FieldInstances), Min: 1, Max: 1000, Kinds: computing},
	{Field: "autoscale.enabled", Label: lbl(i18n.ArchcanvasT.FieldAutoscale), Input: inputBool, Kinds: computing},
	{Field: "autoscale.min", Label: lbl(i18n.ArchcanvasT.FieldAutoscaleMin), Min: 1, Max: 1000, Kinds: computing},
	{Field: "autoscale.max", Label: lbl(i18n.ArchcanvasT.FieldAutoscaleMax), Min: 1, Max: 1000, Kinds: computing},
	{Field: "autoscale.target_cpu_permille", Label: lbl(i18n.ArchcanvasT.FieldAutoscaleTarget), Unit: unitPct, Min: 1, Max: 100, Kinds: computing},
	{Field: "autoscale.warmup_s", Label: lbl(i18n.ArchcanvasT.FieldWarmup), Min: 0, Max: 3600, Kinds: computing},
	{Field: "autoscale.cooldown_s", Label: lbl(i18n.ArchcanvasT.FieldCooldown), Min: 0, Max: 3600, Kinds: computing},
	{Field: "nic_mbps", Label: lbl(i18n.ArchcanvasT.FieldNic), Min: 1, Max: 400_000, Kinds: computing},
	{Field: "disk.type", Label: lbl(i18n.ArchcanvasT.FieldDiskType), Input: inputChoice, Kinds: computing},
	{Field: "disk.size_gb", Label: lbl(i18n.ArchcanvasT.FieldDiskSize), Min: 0, Max: 65_536, Kinds: computing},
	{Field: "disk.iops", Label: lbl(i18n.ArchcanvasT.FieldDiskIops), Min: 0, Max: 256_000, Kinds: computing},
	{Field: "cpu_micros_per_req", Label: lbl(i18n.ArchcanvasT.FieldCpuPerReq), Min: 1, Max: 60_000_000, Kinds: computing},
	{Field: "timeout_ms", Label: lbl(i18n.ArchcanvasT.FieldTimeout), Min: 1, Max: 600_000, Kinds: computing},
	{Field: "retries", Label: lbl(i18n.ArchcanvasT.FieldRetries), Min: 0, Max: 10, Kinds: computing},
	{Field: "queue_cap", Label: lbl(i18n.ArchcanvasT.FieldQueueCap), Min: 0, Max: 10_000_000, Kinds: computing},
	// C2.8 부품별 추가 설정
	{Field: "params.hit_permille", Label: lbl(i18n.ArchcanvasT.FieldHit), Unit: unitPct, Min: 0, Max: 100, Kinds: []archdoc.NodeKind{archdoc.KindCDN, archdoc.KindCache}},
	{Field: "params.working_set_mib", Label: lbl(i18n.ArchcanvasT.FieldWorkingSet), Min: 0, Max: 24_576_000, Kinds: []archdoc.NodeKind{archdoc.KindCache, archdoc.KindDBPrimary, archdoc.KindDBReplica}},
	{Field: "params.ttl_s", Label: lbl(i18n.ArchcanvasT.FieldTtl), Min: 0, Max: 31_536_000, Kinds: []archdoc.NodeKind{archdoc.KindCache}},
	{Field: "params.lb_algo", Label: lbl(i18n.ArchcanvasT.FieldLbAlgo), Input: inputChoice, Kinds: []archdoc.NodeKind{archdoc.KindLB}},
	{Field: "params.health_interval_s", Label: lbl(i18n.ArchcanvasT.FieldHealth), Min: 0, Max: 3600, Kinds: []archdoc.NodeKind{archdoc.KindLB}},
	{Field: "params.read_iops_milli", Label: lbl(i18n.ArchcanvasT.FieldReadIops), Min: 0, Max: 1_000_000, Kinds: []archdoc.NodeKind{archdoc.KindDBPrimary, archdoc.KindDBReplica}},
	{Field: "params.write_iops_milli", Label: lbl(i18n.ArchcanvasT.FieldWriteIops), Min: 0, Max: 1_000_000, Kinds: []archdoc.NodeKind{archdoc.KindDBPrimary}},
	{Field: "params.pool_max", Label: lbl(i18n.ArchcanvasT.FieldPool), Min: 0, Max: 100_000, Kinds: []archdoc.NodeKind{archdoc.KindDBPrimary, archdoc.KindDBReplica}},
	{Field: "params.failover_to", Label: lbl(i18n.ArchcanvasT.FieldFailoverTo), Input: inputRef, Kinds: []archdoc.NodeKind{archdoc.KindDBPrimary}},
	{Field: "params.failover_s", Label: lbl(i18n.ArchcanvasT.FieldFailoverS), Min: 0, Max: 3600, Kinds: []archdoc.NodeKind{archdoc.KindDBPrimary}},
	{Field: "params.repl_lag_ms", Label: lbl(i18n.ArchcanvasT.FieldReplLag), Min: 0, Max: 600_000, Kinds: []archdoc.NodeKind{archdoc.KindDBReplica}},
	{Field: "params.max_rps", Label: lbl(i18n.ArchcanvasT.FieldMaxRps), Min: 0, Max: 1_000_000_000, Kinds: []archdoc.NodeKind{archdoc.KindQueue, archdoc.KindObjectStorage, archdoc.KindExternal}},
	{Field: "params.retention_msgs", Label: lbl(i18n.ArchcanvasT.FieldRetention), Min: 0, Max: 1_000_000_000, Kinds: []archdoc.NodeKind{archdoc.KindQueue}},
	{Field: "params.fixed_latency_ms", Label: lbl(i18n.ArchcanvasT.FieldFixedLatency), Min: 0, Max: 600_000, Kinds: []archdoc.NodeKind{archdoc.KindExternal, archdoc.KindObjectStorage}},
	{Field: "params.error_permille", Label: lbl(i18n.ArchcanvasT.FieldErrorRate), Min: 0, Max: 1000, Kinds: []archdoc.NodeKind{archdoc.KindExternal}},
	{Field: "params.index_write_permille", Label: lbl(i18n.ArchcanvasT.FieldIndexWrite), Min: 0, Max: 1000, Kinds: []archdoc.NodeKind{archdoc.KindSearch}},
}

// boxFields are a region, VPC or AZ box's settings.
var boxFields = []fieldDef{
	labelField,
	{Field: "region_code", Label: lbl(i18n.ArchcanvasT.FieldRegionCode), Input: inputChoice},
}

// edgeFields are a connection's settings (C2.8 연결 설정).
var edgeFields = []fieldDef{
	{Field: "protocol", Label: lbl(i18n.ArchcanvasT.FieldProtocol), Input: inputChoice},
	{Field: "mbps", Label: lbl(i18n.ArchcanvasT.FieldMbps), Min: 0, Max: 400_000},
	{Field: "latency_micros", Label: lbl(i18n.ArchcanvasT.FieldLatency), Min: -1, Max: 60_000_000},
	{Field: "read_permille", Label: lbl(i18n.ArchcanvasT.FieldEdgeRead), Unit: unitPct, Min: 0, Max: 100},
	{Field: "write_permille", Label: lbl(i18n.ArchcanvasT.FieldEdgeWrite), Unit: unitPct, Min: 0, Max: 100},
}

// trafficFields are the traffic model (C2.9): a client's (on the client
// itself, or the first client when nothing is selected) and the design's
// seed and run length.
var trafficFields = []fieldDef{
	{Field: "traffic.rps", Label: lbl(i18n.ArchcanvasT.FieldRps), Min: 0, Max: 10_000_000, On: onClient},
	{Field: "traffic.read_permille", Label: lbl(i18n.ArchcanvasT.FieldReadPct), Unit: unitPct, Min: 0, Max: 100, On: onClient},
	{Field: "traffic.req_kb", Label: lbl(i18n.ArchcanvasT.FieldReqKb), Min: 0, Max: 102_400, On: onClient},
	{Field: "traffic.resp_kb", Label: lbl(i18n.ArchcanvasT.FieldRespKb), Min: 0, Max: 102_400, On: onClient},
	{Field: "traffic.curve", Label: lbl(i18n.ArchcanvasT.FieldCurve), Input: inputChoice, On: onClient},
	{Field: "traffic.spike_mult", Label: lbl(i18n.ArchcanvasT.FieldSpikeMult), Min: 1, Max: 100, On: onClient},
	{Field: "traffic.spike_at_s", Label: lbl(i18n.ArchcanvasT.FieldSpikeAt), Min: 0, Max: 600, On: onClient},
	{Field: "traffic.spike_dur_s", Label: lbl(i18n.ArchcanvasT.FieldSpikeDur), Min: 0, Max: 600, On: onClient},
	{Field: "seed", Label: lbl(i18n.ArchcanvasT.FieldSeed), Min: 0, Max: 1<<53 - 1, On: onMeta},
	{Field: "duration_s", Label: lbl(i18n.ArchcanvasT.FieldDuration), Min: 60, Max: 600, On: onMeta},
}

// selection is what the settings panel edits: one node, box or edge (by
// object key), or nothing (the traffic model).
type selection struct {
	obj  string // object key ("" = nothing selected)
	node *archdoc.Node
	box  *archdoc.Box
	edge *archdoc.Edge
}

// fieldsFor are the panel rows of sel.
func fieldsFor(sel selection) []fieldDef {
	switch {
	case sel.node != nil:
		var out []fieldDef
		for _, f := range nodeFields {
			if f.Kinds == nil || slices.Contains(f.Kinds, sel.node.Kind) {
				out = append(out, f)
			}
		}
		if sel.node.Traffic != nil {
			for _, f := range trafficFields {
				if f.On == onClient {
					f.On = onSel
					out = append(out, f)
				}
			}
		}
		return out
	case sel.box != nil:
		return boxFields
	case sel.edge != nil:
		return edgeFields
	}
	return trafficFields
}

// selectionOf is the design's id as a panel selection.
func (m *canvasModel) selectionOf(id string) selection {
	if n := m.node(id); n != nil {
		return selection{obj: archdoc.ObjKey('n', id), node: n}
	}
	if b := m.box(id); b != nil {
		return selection{obj: archdoc.ObjKey('b', id), box: b}
	}
	if e := m.edge(id); e != nil {
		return selection{obj: archdoc.ObjKey('e', id), edge: e}
	}
	return selection{}
}

// firstClient is the first client (ID order) with a traffic model.
func (m *canvasModel) firstClient() *archdoc.Node {
	var best *archdoc.Node
	for i := range m.design.Nodes {
		n := &m.design.Nodes[i]
		if n.Kind == archdoc.KindClient && n.Traffic != nil && (best == nil || n.ID < best.ID) {
			best = n
		}
	}
	return best
}

// objOf is the object key field f of sel lives on ("" = none).
func (m *canvasModel) objOf(sel selection, f fieldDef) string {
	switch f.On {
	case onClient:
		if c := m.firstClient(); c != nil {
			return archdoc.ObjKey('n', c.ID)
		}
		return ""
	case onMeta:
		return archdoc.MetaKey
	}
	return sel.obj
}

// raw is field of object obj in the (normalized) design.
func (m *canvasModel) raw(obj, field string) json.RawMessage {
	if m.objs == nil {
		return nil
	}
	return m.objs[obj][field]
}

func rawInt(v json.RawMessage) int64 {
	var n json.Number
	if json.Unmarshal(v, &n) != nil {
		return 0
	}
	i, err := n.Int64()
	if err != nil {
		return 0
	}
	return i
}

func rawString(v json.RawMessage) string {
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

func rawBool(v json.RawMessage) bool {
	var b bool
	_ = json.Unmarshal(v, &b)
	return b
}

// typed is a stored integer in f's typed unit ("4", "0.5" for 512MiB).
func typed(f fieldDef, stored int64) string {
	s := f.Unit.scale()
	if stored%s == 0 {
		return humanize.Grouped(stored / s)
	}
	neg := stored < 0
	if neg {
		stored = -stored
	}
	tenths := stored * 10 / s
	out := humanize.Grouped(tenths/10) + "." + strconv.FormatInt(tenths%10, 10)
	if neg {
		out = "-" + out
	}
	return out
}

// parseTyped reads a typed whole number ("1,000" and spaces allowed).
func parseTyped(s string) (int64, bool) {
	s = strings.NewReplacer(",", "", " ", "", "_", "").Replace(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// effRegion is the price region node n is priced in for provider p: its own
// region, else the enclosing region box's, else the table's first region.
func (m *canvasModel) effRegion(n *archdoc.Node, p pricing.Provider) string {
	r := n.Region
	if r == "" {
		for id, depth := n.Parent, 0; id != "" && depth <= len(m.design.Boxes); depth++ {
			b := m.box(id)
			if b == nil {
				break
			}
			if b.Kind == archdoc.BoxRegion {
				r = b.RegionCode
				break
			}
			id = b.Parent
		}
	}
	if _, ok := pricing.RegionOf(p, r); ok {
		return r
	}
	if rs := pricing.Regions(p); len(rs) > 0 {
		return rs[0]
	}
	return ""
}

// priced reports whether p has a price table.
func priced(p string) bool {
	switch pricing.Provider(p) {
	case pricing.AWS, pricing.GCP, pricing.Azure:
		return true
	}
	return false
}

// hourlyOf is node n's table price per instance hour (micro-USD).
func (m *canvasModel) hourlyOf(n *archdoc.Node) (int64, bool) {
	p := pricing.Provider(n.Provider)
	if p == pricing.Custom {
		return n.CustomHourlyMicroUSD, true
	}
	if !priced(n.Provider) {
		return 0, false
	}
	it, ok := pricing.Lookup(p, m.effRegion(n, p), n.InstanceType)
	return it.HourlyMicroUSD, ok
}

// usdMicro is micro-USD as dollars with up to four decimals ("0.1450" → "0.145").
func usdMicro(micro int64) string {
	micro = max(micro, 0)
	whole, frac := micro/1_000_000, (micro%1_000_000)/100
	s := humanize.Grouped(whole)
	if frac != 0 {
		f := strings.TrimRight(strconv.FormatInt(10_000+frac, 10)[1:], "0")
		s += "." + f
	}
	return s
}
