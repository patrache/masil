package archdoc

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
)

// ChangeKind is the kind of one local object change.
type ChangeKind int

const (
	ChangePut     ChangeKind = iota // create, or merge fields into a live object; Value = JSON object of fields
	ChangeSet                       // one field; Value nil removes the field (local only)
	ChangeDel                       // delete the object
	ChangeRestore                   // bring a deleted object back; Value = its fields (collab "undel")
)

// Change is one object change. Put maps to collab "put" (type ObjType(Obj)),
// Set to "set", Del to "del" and Restore to "undel".
type Change struct {
	Kind  ChangeKind
	Obj   string
	Field string
	Value json.RawMessage
}

func (o Objects) clone() Objects {
	out := make(Objects, len(o))
	for k, m := range o {
		out[k] = maps.Clone(m)
	}
	return out
}

func decodeFields(v json.RawMessage) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	if v != nil {
		_ = json.Unmarshal(v, &m)
	}
	if m == nil { // "null"
		m = map[string]json.RawMessage{}
	}
	return m
}

// Apply returns a copy of o with ch applied in order. A Set to a deleted or
// missing object is dropped. This is for local previews and undo; the real
// merge (server sequence + field LWW) is the collab layer's.
func (o Objects) Apply(ch []Change) Objects {
	out := o.clone()
	for _, c := range ch {
		switch c.Kind {
		case ChangePut:
			fields := decodeFields(c.Value)
			if cur, ok := out[c.Obj]; ok {
				maps.Copy(cur, fields)
			} else {
				out[c.Obj] = fields
			}
		case ChangeSet:
			cur, ok := out[c.Obj]
			if !ok {
				continue
			}
			if c.Value == nil {
				delete(cur, c.Field)
			} else {
				cur[c.Field] = c.Value
			}
		case ChangeDel:
			delete(out, c.Obj)
		case ChangeRestore:
			if _, ok := out[c.Obj]; !ok {
				out[c.Obj] = decodeFields(c.Value)
			}
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func union[V any](a, b map[string]V) []string {
	keys := sortedKeys(a)
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func marshalFields(m map[string]json.RawMessage) json.RawMessage {
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// DiffObjects returns the changes that turn a into b, sorted by (Obj, Field).
func DiffObjects(a, b Objects) []Change {
	var out []Change
	for _, key := range union(a, b) {
		am, inA := a[key]
		bm, inB := b[key]
		switch {
		case !inA:
			out = append(out, Change{Kind: ChangePut, Obj: key, Value: marshalFields(bm)})
		case !inB:
			out = append(out, Change{Kind: ChangeDel, Obj: key})
		default:
			for _, f := range union(am, bm) {
				av, bv := am[f], bm[f]
				if !rawEqual(av, bv) {
					out = append(out, Change{Kind: ChangeSet, Obj: key, Field: f, Value: bv})
				}
			}
		}
	}
	return out
}

// DesignDiff summarizes two designs for the version view.
type DesignDiff struct {
	Added, Removed []string // object keys, sorted
	Changed        []FieldChange
}

// FieldChange is one changed field; From/To are nil when the field is absent.
type FieldChange struct {
	Obj, Field string
	From, To   json.RawMessage
}

// Summarize compares a and b object by object.
func Summarize(a, b *Design) DesignDiff {
	var out DesignDiff
	ao, bo := a.ToObjects(), b.ToObjects()
	for _, ch := range DiffObjects(ao, bo) {
		switch ch.Kind {
		case ChangePut:
			out.Added = append(out.Added, ch.Obj)
		case ChangeDel:
			out.Removed = append(out.Removed, ch.Obj)
		case ChangeSet:
			out.Changed = append(out.Changed, FieldChange{Obj: ch.Obj, Field: ch.Field, From: ao[ch.Obj][ch.Field], To: ch.Value})
		}
	}
	slices.SortFunc(out.Changed, func(x, y FieldChange) int {
		return cmp.Or(cmp.Compare(x.Obj, y.Obj), cmp.Compare(x.Field, y.Field))
	})
	return out
}
