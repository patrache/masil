// Package collab is the room document client (docs/contract/collab.md, v0.1.3
// design C1.1): the object model and its deterministic merge (Doc.Apply, the
// same docs/contract/collab-vectors.json the server's applyOps passes), the
// document socket frames, and an optimistic Session that shows my edits at
// once and lines them up again in the server's order.
//
// Tools (P3 arch, P4 apicol) open a Session with ctx.CollabDial and hand its
// events to the screen with ctx.Post; tests use collabtest.Hub.
package collab

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Op kinds (collab.md 1).
const (
	OpPut   = "put"
	OpSet   = "set"
	OpDel   = "del"
	OpUndel = "undel"
	OpReset = "reset" // server only (version restore)
)

// OpsMax is the number of ops one frame may carry (collab.md 3.1).
const OpsMax = 200

// Op is one edit. Which fields count depends on Op: put = ID, Type, Fields;
// set = ID, Field, Value; del / undel = ID; reset = Snapshot.
type Op struct {
	Op       string
	ID       string
	Type     string
	Field    string
	Value    json.RawMessage
	Fields   map[string]json.RawMessage
	Snapshot *Snapshot
}

// Object is one document object.
type Object struct {
	ID      string                     `json:"id"`
	Type    string                     `json:"type"`
	Fields  map[string]json.RawMessage `json:"fields"`
	Deleted bool                       `json:"deleted,omitempty"`
}

// Snapshot is a whole document state: Seq is the last sequence number it
// reflects; Objects are in id byte order (the canonical form, collab.md 1.1).
type Snapshot struct {
	Seq     int64    `json:"seq"`
	Objects []Object `json:"objects"`
}

// Change is what one op changed. Field "" = the object as a whole (put
// creating it, del, undel, reset); Removed = the object is deleted or gone
// after the change.
type Change struct {
	ID      string
	Field   string
	Removed bool
}

// MarshalJSON writes the op's wire shape (collab.md 1): only the keys of its
// kind.
func (o Op) MarshalJSON() ([]byte, error) {
	switch o.Op {
	case OpPut:
		fields := o.Fields
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		return json.Marshal(struct {
			Op     string                     `json:"op"`
			ID     string                     `json:"id"`
			Type   string                     `json:"type"`
			Fields map[string]json.RawMessage `json:"fields"`
		}{o.Op, o.ID, o.Type, fields})
	case OpSet:
		v := o.Value
		if len(v) == 0 {
			v = json.RawMessage("null")
		}
		return json.Marshal(struct {
			Op    string          `json:"op"`
			ID    string          `json:"id"`
			Field string          `json:"field"`
			Value json.RawMessage `json:"value"`
		}{o.Op, o.ID, o.Field, v})
	case OpReset:
		s := Snapshot{}
		if o.Snapshot != nil {
			s = *o.Snapshot
		}
		return json.Marshal(struct {
			Op       string   `json:"op"`
			Snapshot Snapshot `json:"snapshot"`
		}{o.Op, s})
	default: // del, undel (and anything else: its op and id)
		return json.Marshal(struct {
			Op string `json:"op"`
			ID string `json:"id"`
		}{o.Op, o.ID})
	}
}

// UnmarshalJSON reads an op without checking its shape (frames from the
// server); ParseOps is the strict client-frame check.
func (o *Op) UnmarshalJSON(b []byte) error {
	var w struct {
		Op       string                     `json:"op"`
		ID       string                     `json:"id"`
		Type     string                     `json:"type"`
		Field    string                     `json:"field"`
		Value    json.RawMessage            `json:"value"`
		Fields   map[string]json.RawMessage `json:"fields"`
		Snapshot *Snapshot                  `json:"snapshot"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*o = Op{Op: w.Op, ID: w.ID, Type: w.Type, Field: w.Field, Value: compact(w.Value), Fields: compactFields(w.Fields), Snapshot: w.Snapshot}
	return nil
}

// ---- names (collab.md 1.2) ----

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

var reservedNames = map[string]bool{"__proto__": true, "constructor": true, "prototype": true}

// IsName reports whether s may be an object id, type or field name.
func IsName(s string) bool { return nameRe.MatchString(s) && !reservedNames[s] }

// ErrInvalid is a frame the common shape check (collab.md 3.3 step 1) refuses:
// the server answers it with reject{code:"invalid"}.
var ErrInvalid = errors.New("collab: invalid ops")

// ParseOps is the client frame's `ops` as the server checks them (collab.md
// 3.3 step 1): 1..200 known ops, no reset, required keys, the name rule.
func ParseOps(raw json.RawMessage) ([]Op, error) { return parseOps(raw, false) }

// parseOps is ParseOps; allowReset admits reset (server-sequenced batches in
// the shared vectors).
func parseOps(raw json.RawMessage, allowReset bool) ([]Op, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || arr == nil || len(arr) < 1 || len(arr) > OpsMax {
		return nil, ErrInvalid
	}
	out := make([]Op, 0, len(arr))
	for _, r := range arr {
		op, ok := parseOp(r, allowReset)
		if !ok {
			return nil, ErrInvalid
		}
		out = append(out, op)
	}
	return out, nil
}

// CheckOps runs ParseOps over ops as they would go on the wire.
func CheckOps(ops []Op) error {
	b, err := json.Marshal(ops)
	if err != nil {
		return ErrInvalid
	}
	_, err = ParseOps(b)
	return err
}

func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	return m, true
}

func str(m map[string]json.RawMessage, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	v = bytes.TrimSpace(v)
	if len(v) == 0 || v[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return "", false
	}
	return s, true
}

func name(m map[string]json.RawMessage, k string) (string, bool) {
	s, ok := str(m, k)
	return s, ok && IsName(s)
}

// fieldsOf is a fields object whose keys all follow the name rule.
func fieldsOf(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	m, ok := jsonObject(raw)
	if !ok {
		return nil, false
	}
	for k := range m {
		if !IsName(k) {
			return nil, false
		}
	}
	return compactFields(m), true
}

func parseOp(raw json.RawMessage, allowReset bool) (Op, bool) {
	m, ok := jsonObject(raw)
	if !ok {
		return Op{}, false
	}
	kind, _ := str(m, "op")
	switch kind {
	case OpPut:
		id, ok1 := name(m, "id")
		typ, ok2 := name(m, "type")
		fields, ok3 := fieldsOf(m["fields"])
		if !ok1 || !ok2 || !ok3 {
			return Op{}, false
		}
		return Op{Op: OpPut, ID: id, Type: typ, Fields: fields}, true
	case OpSet:
		id, ok1 := name(m, "id")
		field, ok2 := name(m, "field")
		v, ok3 := m["value"]
		if !ok1 || !ok2 || !ok3 {
			return Op{}, false
		}
		return Op{Op: OpSet, ID: id, Field: field, Value: compact(v)}, true
	case OpDel, OpUndel:
		id, ok := name(m, "id")
		if !ok {
			return Op{}, false
		}
		return Op{Op: kind, ID: id}, true
	case OpReset:
		if !allowReset {
			return Op{}, false
		}
		s, err := parseSnapshot(m["snapshot"])
		if err != nil {
			return Op{}, false
		}
		return Op{Op: OpReset, Snapshot: &s}, true
	default:
		return Op{}, false
	}
}

// DecodeSnapshot reads a snapshot (api.DocVersionSnapshot.Snapshot, a hello
// frame's snapshot) with the server's checks: the name rule, unique ids, a
// non-negative integer seq (absent = 0). Objects come back in canonical order.
func DecodeSnapshot(b json.RawMessage) (Snapshot, error) { return parseSnapshot(b) }

func parseSnapshot(raw json.RawMessage) (Snapshot, error) {
	m, ok := jsonObject(raw)
	if !ok {
		return Snapshot{}, fmt.Errorf("collab: snapshot is not an object")
	}
	var s Snapshot
	if v, ok := m["seq"]; ok {
		if err := json.Unmarshal(v, &s.Seq); err != nil || s.Seq < 0 {
			return Snapshot{}, fmt.Errorf("collab: snapshot seq: not a non-negative integer")
		}
	}
	var objs []json.RawMessage
	if v := bytes.TrimSpace(m["objects"]); len(v) == 0 || v[0] != '[' || json.Unmarshal(v, &objs) != nil {
		return Snapshot{}, fmt.Errorf("collab: snapshot objects: not an array")
	}
	seen := map[string]bool{}
	for _, r := range objs {
		om, ok := jsonObject(r)
		if !ok {
			return Snapshot{}, fmt.Errorf("collab: snapshot object: not an object")
		}
		id, ok1 := name(om, "id")
		typ, ok2 := name(om, "type")
		fields, ok3 := fieldsOf(om["fields"])
		if !ok1 || !ok2 || !ok3 || seen[id] {
			return Snapshot{}, fmt.Errorf("collab: snapshot object %q: bad id, type or fields", id)
		}
		seen[id] = true
		o := Object{ID: id, Type: typ, Fields: fields}
		if d, ok := om["deleted"]; ok {
			if err := json.Unmarshal(d, &o.Deleted); err != nil || string(bytes.TrimSpace(d)) == "null" {
				return Snapshot{}, fmt.Errorf("collab: snapshot object %q: deleted is not a boolean", id)
			}
		}
		s.Objects = append(s.Objects, o)
	}
	return NewDoc(s).Snapshot(), nil
}

// compact is v without insignificant whitespace (nil stays nil).
func compact(v json.RawMessage) json.RawMessage {
	if v == nil {
		return nil
	}
	var b bytes.Buffer
	if json.Compact(&b, v) != nil {
		return append(json.RawMessage(nil), v...)
	}
	return json.RawMessage(b.Bytes())
}

func compactFields(m map[string]json.RawMessage) map[string]json.RawMessage {
	if m == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = compact(v)
	}
	return out
}
