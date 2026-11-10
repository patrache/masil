package archdoc

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"
	"strings"
)

// Objects is a design as CollabHub objects (arch.md 2): object key → field → JSON
// value. Keys are "meta", "n:<id>", "b:<id>", "e:<id>" and "f:<id>"; field names
// are the JSON tags flattened with dots. Only live objects are present.
type Objects map[string]map[string]json.RawMessage

// MetaKey is the design-wide object (title, seed, duration_s).
const MetaKey = "meta"

// ObjKey returns the object key of an item: prefix 'n' node, 'b' box, 'e' edge, 'f' fault.
func ObjKey(prefix byte, id string) string {
	return string([]byte{prefix, ':'}) + id
}

// ObjType maps an object key to the collab object type ("" = not a design object).
func ObjType(key string) string {
	if key == MetaKey {
		return "meta"
	}
	if len(key) < 3 || key[1] != ':' {
		return ""
	}
	switch key[0] {
	case 'n':
		return "node"
	case 'b':
		return "box"
	case 'e':
		return "edge"
	case 'f':
		return "fault"
	}
	return ""
}

func fieldsOf[T any](v *T, defs []fieldDef[T]) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage, len(defs))
	for _, def := range defs {
		if ptr := def.at(v, false); ptr != nil {
			m[def.name] = marshalField(ptr)
		}
	}
	return m
}

// ToObjects maps d onto collab objects. Every field is written.
func (d *Design) ToObjects() Objects {
	o := Objects{MetaKey: fieldsOf(d, metaFields)}
	for i := range d.Nodes {
		o[ObjKey('n', d.Nodes[i].ID)] = fieldsOf(&d.Nodes[i], nodeFields)
	}
	for i := range d.Boxes {
		o[ObjKey('b', d.Boxes[i].ID)] = fieldsOf(&d.Boxes[i], boxFields)
	}
	for i := range d.Edges {
		o[ObjKey('e', d.Edges[i].ID)] = fieldsOf(&d.Edges[i], edgeFields)
	}
	for i := range d.Faults {
		o[ObjKey('f', d.Faults[i].ID)] = fieldsOf(&d.Faults[i], faultFields)
	}
	return o
}

// defaultBox is the box a missing field falls back to.
func defaultBox() Box { return Box{Rect: [4]int{0, 0, 2 * (NodeW + 4), 3 * (NodeH + 2)}} }

func copyField(dst, src any) {
	switch d := dst.(type) {
	case *int:
		*d = *src.(*int)
	case *int64:
		*d = *src.(*int64)
	case *uint64:
		*d = *src.(*uint64)
	case *string:
		*d = *src.(*string)
	case *bool:
		*d = *src.(*bool)
	case *[2]int:
		*d = *src.(*[2]int)
	case *[4]int:
		*d = *src.(*[4]int)
	}
}

// loadFields overlays m onto v; a value of the wrong type or out of range keeps
// the field from def and adds an issue.
func loadFields[T any](v, def *T, m map[string]json.RawMessage, defs []fieldDef[T], key string, is *[]Issue) {
	for _, fd := range defs {
		r, ok := m[fd.name]
		if !ok || isNull(r) {
			continue
		}
		ptr := fd.at(v, true)
		code := IssueRange
		if setRaw(fd.kind, ptr, r) {
			if code = checkField(fd, ptr); code == "" {
				continue
			}
		}
		copyField(ptr, fd.at(def, true))
		*is = append(*is, Issue{Path: key + "." + fd.name, Code: code})
	}
}

func rawString(r json.RawMessage) (string, bool) {
	var s string
	if r == nil || !setRaw(fString, &s, r) {
		return "", false
	}
	return s, true
}

// FromObjects rebuilds a design from collab objects, tolerating anything a
// shared document can hold (spec Review Focus 1). The result does not depend on
// map order. Rules (arch.md 2.3): unknown fields are ignored; a bad value resets
// that field to its default; an object of unknown kind is dropped; references to
// missing or deleted objects are cleared (parents) or the referrer is dropped
// (edges, faults); an ID already used by another kind is dropped (node, box,
// edge, fault order); a box parent cycle is cut at its smallest ID; a box deeper
// than MaxDepth moves to its grandparent; a box under a parent of the wrong kind
// moves to the top level; over-limit items are dropped from the end in ID order.
// The result always passes Validate. deleted may be nil.
func FromObjects(o Objects, deleted func(obj string) bool) (*Design, []Issue) {
	gone := func(key string) bool { return deleted != nil && deleted(key) }
	var is []Issue
	add := func(path string, code IssueCode) { is = append(is, Issue{Path: path, Code: code}) }

	d := &Design{SchemaVersion: SchemaVersion, DurationS: 300}
	if m, ok := o[MetaKey]; ok && !gone(MetaKey) {
		def := *d
		loadFields(d, &def, m, metaFields, MetaKey, &is)
	}
	keys := make([]string, 0, len(o))
	for k := range o {
		if k != MetaKey && ObjType(k) != "" && !gone(k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		id, m := key[2:], o[key]
		if !validID(id) {
			add(key, IssueRange)
			continue
		}
		switch key[0] {
		case 'n':
			kind, _ := rawString(m["kind"])
			if !slices.Contains(nodeKindNames, kind) {
				add(key+".kind", IssueUnknownKind)
				continue
			}
			def := Defaults(NodeKind(kind))
			n := def
			n.ID, n.Traffic = id, nil
			for f := range m {
				if strings.HasPrefix(f, "traffic.") {
					t := DefaultTraffic()
					n.Traffic = &t
					break
				}
			}
			loadFields(&n, &def, m, nodeFields, key, &is)
			if a := &n.Autoscale; a.Enabled && a.Min > a.Max {
				a.Min, a.Max = def.Autoscale.Min, def.Autoscale.Max
				add(key+".autoscale.max", IssueRange)
			}
			d.Nodes = append(d.Nodes, n)
		case 'b':
			kind, _ := rawString(m["kind"])
			if !slices.Contains(boxKindNames, kind) {
				add(key+".kind", IssueUnknownKind)
				continue
			}
			def := defaultBox()
			b := def
			b.ID = id
			loadFields(&b, &def, m, boxFields, key, &is)
			d.Boxes = append(d.Boxes, b)
		case 'e':
			def := DefaultEdge()
			e := def
			e.ID = id
			loadFields(&e, &def, m, edgeFields, key, &is)
			d.Edges = append(d.Edges, e)
		case 'f':
			kind, _ := rawString(m["kind"])
			if !slices.Contains(faultKindNames, kind) {
				add(key+".kind", IssueUnknownKind)
				continue
			}
			var def Fault
			f := def
			f.ID = id
			loadFields(&f, &def, m, faultFields, key, &is)
			d.Faults = append(d.Faults, f)
		}
	}

	// IDs are unique across kinds (arch.md 1.3) but "n:x" and "b:x" are two
	// collab keys: keep the first in the order node, box, edge, fault.
	ids := make(map[string]bool, len(keys))
	for _, n := range d.Nodes {
		ids[n.ID] = true
	}
	d.Boxes = dropTaken(d.Boxes, 'b', ids, add)
	d.Edges = dropTaken(d.Edges, 'e', ids, add)
	d.Faults = dropTaken(d.Faults, 'f', ids, add)

	d.Boxes = capList(d.Boxes, MaxBoxes, "boxes", IssueTooManyBoxes, add)
	d.Nodes = capList(d.Nodes, MaxNodes, "nodes", IssueTooManyNodes, add)
	repairBoxes(d.Boxes, add)

	boxes := map[string]*Box{}
	for i := range d.Boxes {
		boxes[d.Boxes[i].ID] = &d.Boxes[i]
	}
	nodes := map[string]*Node{}
	for i := range d.Nodes {
		nodes[d.Nodes[i].ID] = &d.Nodes[i]
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		key := ObjKey('n', n.ID)
		if n.Parent != "" && boxes[n.Parent] == nil {
			n.Parent = ""
			add(key+".parent", IssueDanglingRef)
		}
		if f := n.Params.FailoverTo; f != "" && (nodes[f] == nil || f == n.ID) {
			n.Params.FailoverTo = ""
			add(key+".params.failover_to", IssueDanglingRef)
		}
	}
	d.Edges = slices.DeleteFunc(d.Edges, func(e Edge) bool {
		if nodes[e.From] == nil || nodes[e.To] == nil {
			add(ObjKey('e', e.ID), IssueDanglingRef)
			return true
		}
		return false
	})
	d.Edges = capList(d.Edges, MaxEdges, "edges", IssueTooManyEdges, add)
	edges := map[string]bool{}
	for _, e := range d.Edges {
		edges[e.ID] = true
	}
	d.Faults = slices.DeleteFunc(d.Faults, func(f Fault) bool {
		if !faultTargetOK(&f, nodes, boxes, edges) {
			add(ObjKey('f', f.ID), IssueDanglingRef)
			return true
		}
		return false
	})
	d.Faults = capList(d.Faults, MaxFaults, "faults", IssueTooManyFaults, add)
	d.Nodes, d.Boxes, d.Edges, d.Faults = nilIfEmpty(d.Nodes), nilIfEmpty(d.Boxes), nilIfEmpty(d.Edges), nilIfEmpty(d.Faults)

	slices.SortStableFunc(is, func(a, b Issue) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Code, b.Code))
	})
	return d, is
}

// dropTaken removes the items whose ID is already in ids (duplicate_id at the
// object key) and adds the rest.
func dropTaken[T identified](s []T, prefix byte, ids map[string]bool, add func(string, IssueCode)) []T {
	return slices.DeleteFunc(s, func(v T) bool {
		id := *idOf(&v)
		if ids[id] {
			add(ObjKey(prefix, id), IssueDuplicateID)
			return true
		}
		ids[id] = true
		return false
	})
}

func capList[T any](s []T, limit int, path string, code IssueCode, add func(string, IssueCode)) []T {
	if len(s) <= limit {
		return s
	}
	add(path, code)
	return s[:limit]
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// repairBoxes makes the box parent graph a forest no deeper than MaxDepth that
// follows the parent kind rule.
// boxes must be sorted by ID.
func repairBoxes(boxes []Box, add func(string, IssueCode)) {
	idx := make(map[string]*Box, len(boxes))
	for i := range boxes {
		idx[boxes[i].ID] = &boxes[i]
	}
	for i := range boxes {
		b := &boxes[i]
		if b.Parent != "" && idx[b.Parent] == nil {
			b.Parent = ""
			add(ObjKey('b', b.ID)+".parent", IssueDanglingRef)
		}
	}
	for _, id := range breakCycles(boxes, idx) {
		add(ObjKey('b', id)+".parent", IssueDepth)
	}
	for i := range boxes {
		b := &boxes[i]
		lifted := false
		for boxDepth(idx, b) > MaxDepth {
			b.Parent = idx[b.Parent].Parent
			lifted = true
		}
		if lifted {
			add(ObjKey('b', b.ID)+".parent", IssueDepth)
		}
	}
	// Last, the kind rule (region top-level, vpc in region, az in vpc): a box
	// under a parent of the wrong kind moves to the top level. Clearing a parent
	// never makes a cycle or a deeper box.
	for i := range boxes {
		b := &boxes[i]
		if b.Parent != "" && !parentKindOK(b.Kind, idx[b.Parent].Kind) {
			b.Parent = ""
			add(ObjKey('b', b.ID)+".parent", IssueParentKind)
		}
	}
}

// breakCycles cuts every parent cycle at its smallest ID and returns the cut IDs.
// Every Parent must name a box in idx or be "".
func breakCycles(boxes []Box, idx map[string]*Box) []string {
	const (
		unseen = iota
		onPath
		done
	)
	state := make(map[string]int, len(boxes))
	var cut []string
	for i := range boxes {
		var path []string
		for cur := boxes[i].ID; cur != ""; cur = idx[cur].Parent {
			if s := state[cur]; s == onPath {
				cycle := path[slices.Index(path, cur):]
				smallest := slices.Min(cycle)
				idx[smallest].Parent = ""
				cut = append(cut, smallest)
				break
			} else if s == done {
				break
			}
			state[cur] = onPath
			path = append(path, cur)
		}
		for _, p := range path {
			state[p] = done
		}
	}
	slices.Sort(cut)
	return cut
}

// rawEqual compares two JSON values ignoring insignificant whitespace.
func rawEqual(a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if bytes.Equal(a, b) {
		return true
	}
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}
