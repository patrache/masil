package collab

import (
	"encoding/json"
	"sort"
)

// Doc is a document state: objects with fields, merged field by field in
// sequence order (collab.md 1.1). Its zero value is not usable; use NewDoc.
// A Doc is not safe for concurrent use (Session.View hands out copies).
type Doc struct {
	seq     int64
	objects map[string]*object
}

type object struct {
	typ     string
	fields  map[string]json.RawMessage
	deleted bool
}

// NewDoc is the state s describes (s.Seq, its objects).
func NewDoc(s Snapshot) *Doc {
	d := &Doc{seq: s.Seq}
	d.replace(s.Objects)
	return d
}

func (d *Doc) replace(objs []Object) {
	d.objects = make(map[string]*object, len(objs))
	for _, o := range objs {
		fields := make(map[string]json.RawMessage, len(o.Fields))
		for k, v := range o.Fields {
			fields[k] = compact(v)
		}
		d.objects[o.ID] = &object{typ: o.Type, fields: fields, deleted: o.Deleted}
	}
}

// Seq is the last sequence number this state reflects.
func (d *Doc) Seq() int64 { return d.seq }

// Apply applies one sequenced frame's ops in order (collab.md 1.1) and moves
// the state to seq. It returns what changed; ops that change nothing (set on
// a deleted or missing object, put on a deleted one) leave no Change.
// Deterministic: the same vectors as the server's applyOps.
func (d *Doc) Apply(seq int64, ops []Op) []Change {
	var out []Change
	for _, op := range ops {
		out = d.apply(op, out)
	}
	d.seq = seq
	return out
}

func sortedKeys(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (d *Doc) apply(op Op, out []Change) []Change {
	switch op.Op {
	case OpPut:
		o, ok := d.objects[op.ID]
		if ok && o.deleted {
			return out // a deleted object ignores put
		}
		if !ok {
			o = &object{typ: op.Type, fields: map[string]json.RawMessage{}}
			d.objects[op.ID] = o
			out = append(out, Change{ID: op.ID})
		}
		for _, k := range sortedKeys(op.Fields) {
			o.fields[k] = compact(op.Fields[k]) // type stays
			out = append(out, Change{ID: op.ID, Field: k})
		}
	case OpSet:
		o, ok := d.objects[op.ID]
		if !ok || o.deleted {
			return out
		}
		v := compact(op.Value)
		if len(v) == 0 {
			v = json.RawMessage("null")
		}
		o.fields[op.Field] = v
		out = append(out, Change{ID: op.ID, Field: op.Field})
	case OpDel, OpUndel:
		o, ok := d.objects[op.ID]
		if !ok {
			return out
		}
		o.deleted = op.Op == OpDel
		out = append(out, Change{ID: op.ID, Removed: o.deleted})
	case OpReset:
		for _, id := range d.ids() {
			out = append(out, Change{ID: id, Removed: true})
		}
		var objs []Object
		if op.Snapshot != nil {
			objs = op.Snapshot.Objects
		}
		d.replace(objs)
		for _, id := range d.ids() {
			out = append(out, Change{ID: id, Removed: d.objects[id].deleted})
		}
	}
	return out
}

func (d *Doc) ids() []string {
	ids := make([]string, 0, len(d.objects))
	for id := range d.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (o *object) export(id string) Object {
	fields := make(map[string]json.RawMessage, len(o.fields))
	for k, v := range o.fields {
		fields[k] = append(json.RawMessage(nil), v...)
	}
	return Object{ID: id, Type: o.typ, Fields: fields, Deleted: o.deleted}
}

// Object returns the object (deleted ones too, with Deleted set); a copy.
func (d *Doc) Object(id string) (Object, bool) {
	o, ok := d.objects[id]
	if !ok {
		return Object{}, false
	}
	return o.export(id), true
}

// Field returns one field's JSON value (nil when the object or field is
// missing).
func (d *Doc) Field(id, field string) json.RawMessage {
	o, ok := d.objects[id]
	if !ok {
		return nil
	}
	return o.fields[field]
}

// Objects returns the live (not deleted) objects of type typ in id order;
// typ "" = every type.
func (d *Doc) Objects(typ string) []Object {
	var out []Object
	for _, id := range d.ids() {
		o := d.objects[id]
		if o.deleted || (typ != "" && o.typ != typ) {
			continue
		}
		out = append(out, o.export(id))
	}
	return out
}

// Snapshot is the canonical snapshot (collab.md 1.1): objects in id byte
// order, deleted ones included with Deleted set.
func (d *Doc) Snapshot() Snapshot {
	s := Snapshot{Seq: d.seq, Objects: []Object{}}
	for _, id := range d.ids() {
		s.Objects = append(s.Objects, d.objects[id].export(id))
	}
	return s
}

// Clone is an independent copy.
func (d *Doc) Clone() *Doc {
	c := &Doc{seq: d.seq, objects: make(map[string]*object, len(d.objects))}
	for id, o := range d.objects {
		fields := make(map[string]json.RawMessage, len(o.fields))
		for k, v := range o.fields {
			fields[k] = v // values are never mutated in place
		}
		c.objects[id] = &object{typ: o.typ, fields: fields, deleted: o.deleted}
	}
	return c
}
