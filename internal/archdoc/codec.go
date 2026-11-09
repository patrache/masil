package archdoc

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

// One hand-written field table per object kind drives decoding, validation, the
// collab object mapping (objects.go) and the contract table in arch.md 1–2.
// Names are the JSON tags flattened with dots ("autoscale.min").

type fieldKind uint8

const (
	fInt    fieldKind = iota // *int
	fInt64                   // *int64
	fUint64                  // *uint64 (any value)
	fString                  // *string
	fBool                    // *bool
	fPair                    // *[2]int
	fQuad                    // *[4]int
)

type fieldDef[T any] struct {
	name     string
	kind     fieldKind
	lo, hi   int64    // integer range, inclusive (each element for fPair/fQuad)
	enum     []string // fString: allowed values; nil = any
	maxBytes int      // fString: byte cap; 0 = none
	maxG     int      // fString: grapheme cap → label_too_long
	// at returns a pointer to the field, or nil when it lives in an absent
	// optional part (Traffic) and alloc is false.
	at func(v *T, alloc bool) any
}

func fieldNames[T any](defs []fieldDef[T]) []string {
	out := make([]string, len(defs))
	for i, f := range defs {
		out[i] = f.name
	}
	return out
}

const (
	coordMin, coordMax = -100_000, 100_000
	shortText          = 64 // provider, instance type, region codes
)

func iField[T any](name string, lo, hi int64, at func(*T) *int) fieldDef[T] {
	return fieldDef[T]{name: name, kind: fInt, lo: lo, hi: hi, at: func(v *T, _ bool) any { return at(v) }}
}

func sField[T any](name string, enum []string, maxBytes, maxG int, at func(*T) *string) fieldDef[T] {
	return fieldDef[T]{name: name, kind: fString, enum: enum, maxBytes: maxBytes, maxG: maxG, at: func(v *T, _ bool) any { return at(v) }}
}

func tField(name string, lo, hi int64, at func(*Traffic) *int) fieldDef[Node] {
	return fieldDef[Node]{name: "traffic." + name, kind: fInt, lo: lo, hi: hi, at: func(n *Node, alloc bool) any {
		if t := traffic(n, alloc); t != nil {
			return at(t)
		}
		return nil
	}}
}

func traffic(n *Node, alloc bool) *Traffic {
	if n.Traffic == nil && alloc {
		t := DefaultTraffic()
		n.Traffic = &t
	}
	return n.Traffic
}

var metaFields = []fieldDef[Design]{
	sField[Design]("title", nil, 0, MaxTitle, func(d *Design) *string { return &d.Title }),
	{name: "seed", kind: fUint64, at: func(d *Design, _ bool) any { return &d.Seed }},
	iField[Design]("duration_s", 60, 600, func(d *Design) *int { return &d.DurationS }),
}

var nodeFields = []fieldDef[Node]{
	sField[Node]("kind", nodeKindNames, 0, 0, func(n *Node) *string { return (*string)(&n.Kind) }),
	sField[Node]("label", nil, 0, MaxLabel, func(n *Node) *string { return &n.Label }),
	{name: "pos", kind: fPair, lo: coordMin, hi: coordMax, at: func(n *Node, _ bool) any { return &n.Pos }},
	sField[Node]("parent", nil, shortText, 0, func(n *Node) *string { return &n.Parent }),
	sField[Node]("provider", nil, shortText, 0, func(n *Node) *string { return &n.Provider }),
	sField[Node]("instance_type", nil, shortText, 0, func(n *Node) *string { return &n.InstanceType }),
	sField[Node]("region", nil, shortText, 0, func(n *Node) *string { return &n.Region }),
	iField[Node]("cpu_cores", 1, 448, func(n *Node) *int { return &n.CPUCores }),
	iField[Node]("memory_mib", 128, 24_576_000, func(n *Node) *int { return &n.MemoryMiB }),
	iField[Node]("instances", 1, 1000, func(n *Node) *int { return &n.Instances }),
	{name: "autoscale.enabled", kind: fBool, at: func(n *Node, _ bool) any { return &n.Autoscale.Enabled }},
	iField[Node]("autoscale.min", 1, 1000, func(n *Node) *int { return &n.Autoscale.Min }),
	iField[Node]("autoscale.max", 1, 1000, func(n *Node) *int { return &n.Autoscale.Max }),
	iField[Node]("autoscale.target_cpu_permille", 1, 1000, func(n *Node) *int { return &n.Autoscale.TargetCPUPermille }),
	iField[Node]("autoscale.warmup_s", 0, 3600, func(n *Node) *int { return &n.Autoscale.WarmupS }),
	iField[Node]("autoscale.cooldown_s", 0, 3600, func(n *Node) *int { return &n.Autoscale.CooldownS }),
	iField[Node]("nic_mbps", 1, 400_000, func(n *Node) *int { return &n.NICMbps }),
	sField[Node]("disk.type", diskTypeNames, 0, 0, func(n *Node) *string { return (*string)(&n.Disk.Type) }),
	iField[Node]("disk.size_gb", 0, 65_536, func(n *Node) *int { return &n.Disk.SizeGB }),
	iField[Node]("disk.iops", 0, 256_000, func(n *Node) *int { return &n.Disk.IOPS }),
	iField[Node]("cpu_micros_per_req", 1, 60_000_000, func(n *Node) *int { return &n.CPUMicrosPerReq }),
	iField[Node]("timeout_ms", 1, 600_000, func(n *Node) *int { return &n.TimeoutMs }),
	iField[Node]("retries", 0, 10, func(n *Node) *int { return &n.Retries }),
	iField[Node]("queue_cap", 0, 10_000_000, func(n *Node) *int { return &n.QueueCap }),
	{name: "custom_hourly_micro_usd", kind: fInt64, lo: 0, hi: 1_000_000_000_000, at: func(n *Node, _ bool) any { return &n.CustomHourlyMicroUSD }},
	iField[Node]("params.hit_permille", 0, 1000, func(n *Node) *int { return &n.Params.HitPermille }),
	iField[Node]("params.working_set_mib", 0, 24_576_000, func(n *Node) *int { return &n.Params.WorkingSetMiB }),
	iField[Node]("params.ttl_s", 0, 31_536_000, func(n *Node) *int { return &n.Params.TTLS }),
	sField[Node]("params.lb_algo", lbAlgoNames, 0, 0, func(n *Node) *string { return (*string)(&n.Params.LBAlgo) }),
	iField[Node]("params.health_interval_s", 0, 3600, func(n *Node) *int { return &n.Params.HealthIntervalS }),
	iField[Node]("params.read_iops_milli", 0, 1_000_000, func(n *Node) *int { return &n.Params.ReadIOPSMilli }),
	iField[Node]("params.write_iops_milli", 0, 1_000_000, func(n *Node) *int { return &n.Params.WriteIOPSMilli }),
	iField[Node]("params.pool_max", 0, 100_000, func(n *Node) *int { return &n.Params.PoolMax }),
	sField[Node]("params.failover_to", nil, shortText, 0, func(n *Node) *string { return &n.Params.FailoverTo }),
	iField[Node]("params.failover_s", 0, 3600, func(n *Node) *int { return &n.Params.FailoverS }),
	iField[Node]("params.repl_lag_ms", 0, 600_000, func(n *Node) *int { return &n.Params.ReplLagMs }),
	iField[Node]("params.max_rps", 0, 1_000_000_000, func(n *Node) *int { return &n.Params.MaxRPS }),
	iField[Node]("params.retention_msgs", 0, 1_000_000_000, func(n *Node) *int { return &n.Params.RetentionMsgs }),
	iField[Node]("params.fixed_latency_ms", 0, 600_000, func(n *Node) *int { return &n.Params.FixedLatencyMs }),
	iField[Node]("params.error_permille", 0, 1000, func(n *Node) *int { return &n.Params.ErrorPermille }),
	iField[Node]("params.index_write_permille", 0, 1000, func(n *Node) *int { return &n.Params.IndexWritePermille }),
	tField("rps", 0, 10_000_000, func(t *Traffic) *int { return &t.RPS }),
	tField("read_permille", 0, 1000, func(t *Traffic) *int { return &t.ReadPermille }),
	tField("req_kb", 0, 102_400, func(t *Traffic) *int { return &t.ReqKB }),
	tField("resp_kb", 0, 102_400, func(t *Traffic) *int { return &t.RespKB }),
	{name: "traffic.curve", kind: fString, enum: curveNames, at: func(n *Node, alloc bool) any {
		if t := traffic(n, alloc); t != nil {
			return (*string)(&t.Curve)
		}
		return nil
	}},
	tField("spike_mult", 1, 100, func(t *Traffic) *int { return &t.SpikeMult }),
	tField("spike_at_s", 0, 600, func(t *Traffic) *int { return &t.SpikeAtS }),
	tField("spike_dur_s", 0, 600, func(t *Traffic) *int { return &t.SpikeDurS }),
}

var boxFields = []fieldDef[Box]{
	sField[Box]("kind", boxKindNames, 0, 0, func(b *Box) *string { return (*string)(&b.Kind) }),
	sField[Box]("label", nil, 0, MaxLabel, func(b *Box) *string { return &b.Label }),
	sField[Box]("region_code", nil, shortText, 0, func(b *Box) *string { return &b.RegionCode }),
	{name: "rect", kind: fQuad, lo: coordMin, hi: coordMax, at: func(b *Box, _ bool) any { return &b.Rect }},
	sField[Box]("parent", nil, shortText, 0, func(b *Box) *string { return &b.Parent }),
}

var edgeFields = []fieldDef[Edge]{
	sField[Edge]("from", nil, shortText, 0, func(e *Edge) *string { return &e.From }),
	sField[Edge]("to", nil, shortText, 0, func(e *Edge) *string { return &e.To }),
	sField[Edge]("protocol", protocolNames, 0, 0, func(e *Edge) *string { return (*string)(&e.Protocol) }),
	iField[Edge]("mbps", 0, 400_000, func(e *Edge) *int { return &e.Mbps }),
	iField[Edge]("latency_micros", -1, 60_000_000, func(e *Edge) *int { return &e.LatencyMicros }),
	iField[Edge]("read_permille", 0, 1000, func(e *Edge) *int { return &e.ReadPermille }),
	iField[Edge]("write_permille", 0, 1000, func(e *Edge) *int { return &e.WritePermille }),
}

var faultFields = []fieldDef[Fault]{
	iField[Fault]("at_s", 0, 600, func(f *Fault) *int { return &f.AtS }),
	sField[Fault]("kind", faultKindNames, 0, 0, func(f *Fault) *string { return (*string)(&f.Kind) }),
	sField[Fault]("target", nil, shortText, 0, func(f *Fault) *string { return &f.Target }),
	iField[Fault]("count", 0, 1000, func(f *Fault) *int { return &f.Count }),
	iField[Fault]("add_ms", 0, 600_000, func(f *Fault) *int { return &f.AddMs }),
	iField[Fault]("dur_s", 0, 600, func(f *Fault) *int { return &f.DurS }),
}

// setRaw stores raw into ptr when raw has the field's JSON type. It does not check
// ranges (checkField does). "null" is treated by callers as absent.
func setRaw(kind fieldKind, ptr any, raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	switch kind {
	case fInt:
		n, ok := rawInt(raw)
		if !ok || n < math.MinInt || n > math.MaxInt {
			return false
		}
		*ptr.(*int) = int(n)
	case fInt64:
		n, ok := rawInt(raw)
		if !ok {
			return false
		}
		*ptr.(*int64) = n
	case fUint64:
		if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
			return false
		}
		n, err := strconv.ParseUint(string(raw), 10, 64)
		if err != nil {
			return false
		}
		*ptr.(*uint64) = n
	case fString:
		if len(raw) == 0 || raw[0] != '"' {
			return false
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		*ptr.(*string) = s
	case fBool:
		switch string(raw) {
		case "true":
			*ptr.(*bool) = true
		case "false":
			*ptr.(*bool) = false
		default:
			return false
		}
	case fPair, fQuad:
		var arr []json.RawMessage
		if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &arr) != nil {
			return false
		}
		want := 2
		if kind == fQuad {
			want = 4
		}
		if len(arr) != want {
			return false
		}
		vals := make([]int, want)
		for i, r := range arr {
			n, ok := rawInt(bytes.TrimSpace(r))
			if !ok || n < math.MinInt32 || n > math.MaxInt32 {
				return false
			}
			vals[i] = int(n)
		}
		if kind == fPair {
			copy(ptr.(*[2]int)[:], vals)
		} else {
			copy(ptr.(*[4]int)[:], vals)
		}
	}
	return true
}

// rawInt parses a JSON number that is written as a plain integer.
func rawInt(raw []byte) (int64, bool) {
	if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return 0, false
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return n, err == nil
}

// marshalField returns the field's JSON value.
func marshalField(ptr any) json.RawMessage {
	var b []byte
	switch p := ptr.(type) {
	case *int:
		b = strconv.AppendInt(nil, int64(*p), 10)
	case *int64:
		b = strconv.AppendInt(nil, *p, 10)
	case *uint64:
		b = strconv.AppendUint(nil, *p, 10)
	case *bool:
		b = strconv.AppendBool(nil, *p)
	case *string:
		b, _ = json.Marshal(*p)
	case *[2]int:
		b, _ = json.Marshal(*p)
	case *[4]int:
		b, _ = json.Marshal(*p)
	}
	return b
}

// checkField reports why the field's current value is out of the table's rules.
func checkField[T any](def fieldDef[T], ptr any) IssueCode {
	inRange := func(v int64) bool { return v >= def.lo && v <= def.hi }
	switch p := ptr.(type) {
	case *int:
		if !inRange(int64(*p)) {
			return IssueRange
		}
	case *int64:
		if !inRange(*p) {
			return IssueRange
		}
	case *string:
		if def.enum != nil && !slices.Contains(def.enum, *p) {
			return IssueUnknownKind
		}
		if def.maxBytes > 0 && len(*p) > def.maxBytes {
			return IssueRange
		}
		if def.maxG > 0 && graphemes(*p) > def.maxG {
			return IssueLabelTooLong
		}
	case *[2]int:
		if !inRange(int64(p[0])) || !inRange(int64(p[1])) {
			return IssueRange
		}
	case *[4]int:
		// x, y anywhere on the board; w, h at least one cell.
		if !inRange(int64(p[0])) || !inRange(int64(p[1])) || p[2] < 1 || p[3] < 1 || int64(p[2]) > def.hi || int64(p[3]) > def.hi {
			return IssueRange
		}
	}
	return ""
}

// obj is one JSON object with lazily parsed sub-objects ("autoscale", "traffic"…).
type obj struct {
	top  map[string]json.RawMessage
	subs map[string]map[string]json.RawMessage
	bad  map[string]bool
}

func (o *obj) get(name string) (json.RawMessage, string, bool) {
	head, tail, nested := strings.Cut(name, ".")
	if !nested {
		v, ok := o.top[name]
		return v, "", ok && !isNull(v)
	}
	sub, ok := o.subs[head]
	if !ok {
		raw, present := o.top[head]
		if !present || isNull(raw) {
			return nil, "", false
		}
		if o.subs == nil {
			o.subs = map[string]map[string]json.RawMessage{}
			o.bad = map[string]bool{}
		}
		if err := json.Unmarshal(raw, &sub); err != nil || !isObject(raw) {
			o.bad[head] = true
			sub = nil
		}
		o.subs[head] = sub
	}
	if o.bad[head] {
		return nil, head, false
	}
	v, ok := sub[tail]
	return v, "", ok && !isNull(v)
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

func isObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

// decodeInto fills v from o using defs, reporting type errors under path.
func decodeInto[T any](v *T, o *obj, defs []fieldDef[T], path string, is *[]Issue) {
	badSub := map[string]bool{}
	for _, def := range defs {
		raw, bad, ok := o.get(def.name)
		if bad != "" {
			if !badSub[bad] {
				badSub[bad] = true
				*is = append(*is, Issue{Path: path + "." + bad, Code: IssueRange})
			}
			continue
		}
		if !ok {
			continue
		}
		ptr := def.at(v, true)
		if !setRaw(def.kind, ptr, raw) {
			*is = append(*is, Issue{Path: path + "." + def.name, Code: IssueRange})
		}
	}
}

func readID(o *obj) (string, bool) {
	raw, _, ok := o.get("id")
	if !ok {
		return "", true
	}
	var s string
	if !setRaw(fString, &s, raw) {
		return "", false
	}
	return s, true
}

// Decode parses and validates a design JSON v1 document. Unknown fields are
// ignored (later versions may add some); fields that are absent take the
// defaults of their object (Defaults(kind) for nodes). Integers must be written
// as plain integers. Any problem returns an *InvalidError (errors.Is ErrInvalid).
// Both the input and the canonical Encode of the result must fit in MaxBytes.
// The slices come back sorted by ID; empty lists are nil.
func Decode(data []byte) (*Design, error) {
	if len(data) > MaxBytes {
		return nil, invalid(Issue{Code: IssueTooLarge})
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil || top == nil {
		return nil, invalid(Issue{Code: IssueSyntax})
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid(Issue{Code: IssueSyntax})
	}
	root := &obj{top: top}
	var is []Issue
	if raw, _, _ := root.get("schema_version"); string(bytes.TrimSpace(raw)) != strconv.Itoa(SchemaVersion) {
		return nil, invalid(Issue{Path: "schema_version", Code: IssueSchemaVersion})
	}
	d := &Design{SchemaVersion: SchemaVersion, DurationS: 300}
	decodeInto(d, root, metaFields, "", &is)
	for i := range is { // meta paths have no leading object
		is[i].Path = strings.TrimPrefix(is[i].Path, ".")
	}
	d.Nodes = decodeList(root, "nodes", MaxNodes, IssueTooManyNodes, &is, func(o *obj, path string) Node {
		var n Node
		if raw, _, ok := o.get("kind"); ok {
			var k string
			if setRaw(fString, &k, raw) {
				n = Defaults(NodeKind(k))
			}
		}
		n.Traffic = nil
		if raw, ok := o.top["traffic"]; ok && !isNull(raw) {
			t := DefaultTraffic()
			n.Traffic = &t
		}
		decodeInto(&n, o, nodeFields, path, &is)
		return n
	})
	d.Boxes = decodeList(root, "boxes", MaxBoxes, IssueTooManyBoxes, &is, func(o *obj, path string) Box {
		b := defaultBox()
		decodeInto(&b, o, boxFields, path, &is)
		return b
	})
	d.Edges = decodeList(root, "edges", MaxEdges, IssueTooManyEdges, &is, func(o *obj, path string) Edge {
		e := DefaultEdge()
		decodeInto(&e, o, edgeFields, path, &is)
		return e
	})
	d.Faults = decodeList(root, "faults", MaxFaults, IssueTooManyFaults, &is, func(o *obj, path string) Fault {
		var f Fault
		decodeInto(&f, o, faultFields, path, &is)
		return f
	})
	is = append(is, d.Validate()...)
	if len(is) > 0 {
		return nil, invalid(is...)
	}
	d.sortByID()
	// The 1 MB limit holds for the canonical form too: missing fields filled in
	// by default can make it larger than the input, and a design whose Encode
	// would not decode again must not be accepted (arch.md 1.2).
	if enc, err := d.Encode(); err != nil || len(enc) > MaxBytes {
		return nil, invalid(Issue{Code: IssueTooLarge})
	}
	return d, nil
}

type identified interface{ Node | Box | Edge | Fault }

func idOf[T identified](v *T) *string {
	switch p := any(v).(type) {
	case *Node:
		return &p.ID
	case *Box:
		return &p.ID
	case *Edge:
		return &p.ID
	case *Fault:
		return &p.ID
	}
	return nil
}

// decodeList decodes one list. A list longer than limit is rejected with code
// before any item is read, so a small hostile file cannot make Decode build
// hundreds of thousands of objects and issues.
func decodeList[T identified](root *obj, key string, limit int, code IssueCode, is *[]Issue, one func(o *obj, path string) T) []T {
	raw, _, ok := root.get(key)
	if !ok {
		return nil
	}
	var items []json.RawMessage
	if bytes.TrimSpace(raw)[0] != '[' || json.Unmarshal(raw, &items) != nil {
		*is = append(*is, Issue{Path: key, Code: IssueRange})
		return nil
	}
	if len(items) > limit {
		*is = append(*is, Issue{Path: key, Code: code})
		return nil
	}
	var out []T
	for i, item := range items {
		path := key + "[" + strconv.Itoa(i) + "]"
		var m map[string]json.RawMessage
		if !isObject(item) || json.Unmarshal(item, &m) != nil {
			*is = append(*is, Issue{Path: path, Code: IssueRange})
			continue
		}
		o := &obj{top: m}
		id, ok := readID(o)
		if !ok {
			*is = append(*is, Issue{Path: path + ".id", Code: IssueRange})
		}
		v := one(o, path)
		*idOf(&v) = id
		out = append(out, v)
	}
	return out
}

func sortByIDs[T identified](s []T) {
	slices.SortStableFunc(s, func(a, b T) int { return cmp.Compare(*idOf(&a), *idOf(&b)) })
}

func (d *Design) sortByID() {
	sortByIDs(d.Nodes)
	sortByIDs(d.Boxes)
	sortByIDs(d.Edges)
	sortByIDs(d.Faults)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Encode returns the canonical bytes: lists sorted by ID, two-space indent, a
// final newline. The same design always gives the same bytes. It writes the
// current SchemaVersion and does not change d.
func (d *Design) Encode() ([]byte, error) {
	c := *d
	c.SchemaVersion = SchemaVersion
	c.Nodes = orEmpty(slices.Clone(d.Nodes))
	c.Boxes = orEmpty(slices.Clone(d.Boxes))
	c.Edges = orEmpty(slices.Clone(d.Edges))
	c.Faults = orEmpty(slices.Clone(d.Faults))
	c.sortByID()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&c); err != nil { // Encode appends the final newline
		return nil, err
	}
	return buf.Bytes(), nil
}
