package apitool

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrache/masil/internal/collab"
)

// A shared collection is a room document of type "apicol" (collab.md 7):
// objects col, folder:<id>, req:<id>, row:<id>, env:<id>, envvar:<id>. It
// carries the definition only — never a secret variable's value, a form
// file's path on this PC or anything a response held.

// DocType is the collab document type of a shared collection.
const DocType = "apicol"

// Shared collection limits (C3.8, JC14; collab.md 7.2).
const (
	MaxSharedRequests = 1000
	MaxSharedBody     = 64 << 10 // request body bytes
	MaxSharedDoc      = 2 << 20  // canonical snapshot JSON bytes
	MaxSharedField    = 61_440   // one field value's JSON bytes (collab.md 5)
	MaxLost           = 20       // overwritten bodies kept in .lost.json

	// Folder caps: a peer's document past them is read within them
	// (CollectionFromDoc), my own edits and uploads are refused.
	MaxSharedFolders     = 1000
	MaxSharedFolderDepth = 32
)

// Shared collection errors.
var (
	ErrNotCollection         = errors.New("apitool: not an api collection document")
	ErrSharedTooManyRequests = errors.New("apitool: a shared collection holds at most 1000 requests")
	ErrSharedBodyTooLarge    = errors.New("apitool: a shared request body is too large")
	ErrSharedFieldTooLarge   = errors.New("apitool: a shared field is too large")
	ErrSharedDocTooLarge     = errors.New("apitool: the shared collection is too large")
	ErrSharedTooManyFolders  = errors.New("apitool: a shared collection holds at most 1000 folders")
	ErrSharedFoldersTooDeep  = errors.New("apitool: shared folders nest at most 32 deep")
)

// Object types and id prefixes of an apicol document (collab.md 7.1).
const (
	ObjCol     = "col"
	ObjFolder  = "folder"
	ObjRequest = "request"
	ObjRow     = "row"
	ObjEnv     = "env"
	ObjEnvVar  = "envvar"

	colID        = "col"
	prefixFolder = "folder:"
	prefixReq    = "req:"
	prefixRow    = "row:"
	prefixEnv    = "env:"
	prefixEnvVar = "envvar:"
)

// Row lists (the row object's "list" field).
const (
	ListQuery   = "query"
	ListHeader  = "header"
	ListForm    = "form"
	ListAssert  = "assert"
	ListExtract = "extract"
)

// rawJSON is v as compact JSON without HTML escaping.
func rawJSON(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(bytes.TrimRight(b.Bytes(), "\n"))
}

// idAlloc hands out document object ids: a valid, unused own id is kept,
// else one is derived from seed (deterministic).
type idAlloc map[string]bool

func (a idAlloc) pick(prefix, id, seed string) string {
	if id != "" && collab.IsName(prefix+id) && !a[prefix+id] {
		a[prefix+id] = true
		return id
	}
	for n := 0; ; n++ {
		sum := sha256.Sum256([]byte(seed + "\x00" + strconv.Itoa(n)))
		gen := hex.EncodeToString(sum[:8])
		if !a[prefix+gen] {
			a[prefix+gen] = true
			return gen
		}
	}
}

func fillRandom(b []byte) { _, _ = rand.Read(b) }

// AssignIDs gives every folder, request, row, environment and variable of c
// a valid id unique in its document (collab.md 1.2), keeping the valid
// unique ones. A shared collection's edits are diffed by these ids.
func AssignIDs(c *Collection) {
	a := idAlloc{}
	newID := func(prefix, id string) string {
		if id != "" && collab.IsName(prefix+id) && !a[prefix+id] {
			a[prefix+id] = true
			return id
		}
		for {
			var b [8]byte
			fillRandom(b[:])
			gen := hex.EncodeToString(b[:])
			if !a[prefix+gen] {
				a[prefix+gen] = true
				return gen
			}
		}
	}
	renamed := map[string]string{}
	var walk func(f *Folder)
	walk = func(f *Folder) {
		for i := range f.Items {
			it := &f.Items[i]
			if it.Folder != nil {
				it.Folder.ID = newID(prefixFolder, it.Folder.ID)
				walk(it.Folder)
			} else if it.Request != "" {
				old := it.Request
				id, ok := renamed[old]
				if !ok {
					id = newID(prefixReq, old)
					renamed[old] = id
				}
				it.Request = id
			}
		}
	}
	walk(&c.Root)
	reqs := make(map[string]*Request, len(c.Requests))
	for old, r := range c.Requests {
		id, ok := renamed[old]
		if !ok {
			continue // not in the tree: not part of the document
		}
		r.ID = id
		reqs[id] = r
	}
	if c.Requests != nil {
		c.Requests = reqs
	}
	for _, id := range c.Root.RequestIDs() {
		r := c.Requests[id]
		if r == nil {
			continue
		}
		for i := range r.Query {
			r.Query[i].ID = newID(prefixRow, r.Query[i].ID)
		}
		for i := range r.Headers {
			r.Headers[i].ID = newID(prefixRow, r.Headers[i].ID)
		}
		for i := range r.Body.Form {
			r.Body.Form[i].ID = newID(prefixRow, r.Body.Form[i].ID)
		}
		for i := range r.Asserts {
			r.Asserts[i].ID = newID(prefixRow, r.Asserts[i].ID)
		}
		for i := range r.Extracts {
			r.Extracts[i].ID = newID(prefixRow, r.Extracts[i].ID)
		}
	}
	for i := range c.Envs {
		e := &c.Envs[i]
		e.ID = newID(prefixEnv, e.ID)
		for j := range e.Vars {
			e.Vars[j].ID = newID(prefixEnvVar, e.Vars[j].ID)
		}
	}
}

// shareVars are variables as the document keeps them: no allowed hosts (my
// hosts live in .mine.json) and, by Var.MarshalJSON, no secret value.
func shareVars(vs []Var) json.RawMessage {
	out := make([]Var, len(vs))
	for i, v := range vs {
		v.AllowedHosts = nil
		if v.Secret {
			v.Value = ""
		}
		out[i] = v
	}
	return rawJSON(out)
}

type assertExtra struct {
	Op   string `json:"op"`
	Path string `json:"path,omitempty"`
}

type extractExtra struct {
	From string `json:"from"`
}

type formExtra struct {
	FileName string `json:"file_name"`
}

func rowFields(req, list string, order int, key, value string, off bool, extra any) map[string]json.RawMessage {
	if extra == nil {
		extra = struct{}{}
	}
	return map[string]json.RawMessage{
		"req": rawJSON(req), "list": rawJSON(list), "order": rawJSON(order),
		"key": rawJSON(key), "value": rawJSON(value), "off": rawJSON(off), "extra": rawJSON(extra),
	}
}

// DocFromCollection is c as an apicol document snapshot (seq 0, canonical
// order) for CreateDocRequest.Snapshot and Diff: no secret values, no form
// file paths (file names only). Ids that are missing or taken get a
// deterministic one; run AssignIDs first to keep them in c too.
func DocFromCollection(c *Collection) collab.Snapshot {
	a := idAlloc{colID: true}
	var objs []collab.Object
	add := func(id, typ string, f map[string]json.RawMessage) {
		objs = append(objs, collab.Object{ID: id, Type: typ, Fields: f})
	}
	add(colID, ObjCol, map[string]json.RawMessage{"name": rawJSON(c.Name), "vars": shareVars(c.Vars)})

	reqIDs := map[string]string{}
	var walk func(f *Folder, parent, path string)
	walk = func(f *Folder, parent, path string) {
		for i, it := range f.Items {
			p := path + "/" + strconv.Itoa(i)
			switch {
			case it.Folder != nil:
				id := a.pick(prefixFolder, it.Folder.ID, "folder"+p)
				add(prefixFolder+id, ObjFolder, map[string]json.RawMessage{
					"name": rawJSON(it.Folder.Name), "parent": rawJSON(parent), "order": rawJSON(i),
				})
				walk(it.Folder, id, p)
			case it.Request != "":
				r := c.Requests[it.Request]
				if r == nil {
					continue
				}
				if _, dup := reqIDs[it.Request]; dup {
					continue
				}
				id := a.pick(prefixReq, it.Request, "req"+p)
				reqIDs[it.Request] = id
				addRequest(a, add, id, parent, i, r)
			}
		}
	}
	walk(&c.Root, "", "")

	for i, e := range c.Envs {
		eid := a.pick(prefixEnv, e.ID, "env/"+strconv.Itoa(i))
		add(prefixEnv+eid, ObjEnv, map[string]json.RawMessage{"name": rawJSON(e.Name), "order": rawJSON(i)})
		for j, v := range e.Vars {
			vid := a.pick(prefixEnvVar, v.ID, "envvar/"+eid+"/"+strconv.Itoa(j))
			f := map[string]json.RawMessage{
				"env": rawJSON(eid), "name": rawJSON(v.Name), "secret": rawJSON(v.Secret), "order": rawJSON(j),
			}
			if !v.Secret {
				f["value"] = rawJSON(v.Value)
			}
			add(prefixEnvVar+vid, ObjEnvVar, f)
		}
	}
	return collab.NewDoc(collab.Snapshot{Objects: objs}).Snapshot()
}

func addRequest(a idAlloc, add func(id, typ string, f map[string]json.RawMessage), id, folder string, order int, r *Request) {
	add(prefixReq+id, ObjRequest, map[string]json.RawMessage{
		"name": rawJSON(r.Name), "folder": rawJSON(folder), "order": rawJSON(order),
		"method": rawJSON(r.Method), "url": rawJSON(r.URL),
		"body_kind": rawJSON(string(r.Body.Kind)), "body": rawJSON(r.Body.Text), "content_type": rawJSON(r.Body.ContentType),
		"auth": rawJSON(r.Auth), "settings": rawJSON(r.Settings),
	})
	row := func(list string, i int, rid, key, value string, off bool, extra any) {
		oid := a.pick(prefixRow, rid, "row/"+id+"/"+list+"/"+strconv.Itoa(i))
		add(prefixRow+oid, ObjRow, rowFields(id, list, i, key, value, off, extra))
	}
	for i, kv := range r.Query {
		row(ListQuery, i, kv.ID, kv.Key, kv.Value, kv.Off, nil)
	}
	for i, kv := range r.Headers {
		row(ListHeader, i, kv.ID, kv.Key, kv.Value, kv.Off, nil)
	}
	for i, f := range r.Body.Form {
		var extra any
		if f.File != "" || f.FileName != "" {
			name := f.FileName
			if name == "" {
				name = filepath.Base(f.File)
			}
			extra = formExtra{FileName: name}
		}
		row(ListForm, i, f.ID, f.Key, f.Value, f.Off, extra)
	}
	for i, as := range r.Asserts {
		row(ListAssert, i, as.ID, as.Target, as.Value, false, assertExtra{Op: as.Op, Path: as.Path})
	}
	for i, ex := range r.Extracts {
		row(ListExtract, i, ex.ID, ex.Var, ex.Path, false, extractExtra{From: ex.From})
	}
}

// fieldReader reads typed fields of one object, leniently: a value of the
// wrong JSON type reads as the zero value (a peer may write anything the
// server's shape check lets through).
type fieldReader map[string]json.RawMessage

func (f fieldReader) str(k string) string {
	var s string
	_ = json.Unmarshal(f[k], &s)
	return s
}

func (f fieldReader) num(k string) float64 {
	var n float64
	_ = json.Unmarshal(f[k], &n)
	return n
}

func (f fieldReader) flag(k string) bool {
	var b bool
	_ = json.Unmarshal(f[k], &b)
	return b
}

func (f fieldReader) into(k string, v any) { _ = json.Unmarshal(f[k], v) }

type docItem struct {
	id    string // without prefix
	order float64
	obj   string // object id (tie break)
}

func sortItems(items []docItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].order != items[j].order {
			return items[i].order < items[j].order
		}
		return items[i].obj < items[j].obj
	})
}

// CollectionFromDoc reads an apicol document (Session.View().Snapshot()).
// Deleted objects, unknown types and rows of missing requests are ignored;
// a folder or request whose parent is missing (or in a cycle) goes to the
// root. The result's ID is "" (the document id is the caller's).
func CollectionFromDoc(s collab.Snapshot) (*Collection, error) {
	byType := map[string][]collab.Object{}
	var col *collab.Object
	for i := range s.Objects {
		o := s.Objects[i]
		if o.Deleted {
			continue
		}
		if o.ID == colID && o.Type == ObjCol {
			col = &s.Objects[i]
			continue
		}
		byType[o.Type] = append(byType[o.Type], o)
	}
	if col == nil {
		return nil, ErrNotCollection
	}
	cf := fieldReader(col.Fields)
	c := &Collection{Version: CollectionVersion, Name: cf.str("name"), Requests: map[string]*Request{}}
	var vars []Var
	cf.into("vars", &vars)
	if len(vars) > 0 {
		for i := range vars {
			vars[i].AllowedHosts = nil
		}
		c.Vars = vars
	}

	// folders and their effective parents
	folders := map[string]fieldReader{}
	var folderIDs []string
	for _, o := range byType[ObjFolder] {
		if id, ok := strings.CutPrefix(o.ID, prefixFolder); ok {
			folders[id] = fieldReader(o.Fields)
			folderIDs = append(folderIDs, id)
		}
	}
	if len(folders) > MaxSharedFolders {
		// Past the cap (a peer's document): the first ones by id are kept,
		// the items of the rest go to the root.
		slices.Sort(folderIDs)
		for _, id := range folderIDs[MaxSharedFolders:] {
			delete(folders, id)
		}
	}
	// Each folder's parent is read once; a chain is walked once (memoised),
	// so a peer's deep chain costs linear time. A folder whose chain meets a
	// missing folder or a cycle goes to the root; one nested past
	// MaxSharedFolderDepth starts a new chain at the root.
	parent := make(map[string]string, len(folders))
	for id, f := range folders {
		parent[id] = f.str("parent")
	}
	const (
		visiting = iota + 1
		rooted
		broken
	)
	state := make(map[string]int8, len(folders))
	depth := make(map[string]int, len(folders))
	cut := map[string]bool{}
	var path []string
	for id := range folders {
		path = path[:0]
		res := int8(0)
		for cur := id; res == 0; {
			switch state[cur] {
			case rooted, broken:
				res = state[cur]
				continue
			case visiting:
				res = broken // a cycle
				continue
			}
			state[cur] = visiting
			path = append(path, cur)
			p := parent[cur]
			if p == "" {
				res = rooted
			} else if _, ok := folders[p]; !ok {
				res = broken
			} else {
				cur = p
			}
		}
		for i := len(path) - 1; i >= 0; i-- { // parents first
			x := path[i]
			state[x] = res
			if res != rooted {
				continue
			}
			d := 1
			if p := parent[x]; p != "" {
				d = depth[p] + 1
			}
			if d > MaxSharedFolderDepth {
				d, cut[x] = 1, true
			}
			depth[x] = d
		}
	}
	parentOf := func(id string) string {
		if state[id] != rooted || cut[id] {
			return ""
		}
		return parent[id]
	}
	children := map[string][]docItem{} // parent folder id -> items ("f:"/"r:" + id)
	for id, f := range folders {
		p := parentOf(id)
		children[p] = append(children[p], docItem{id: "f:" + id, order: f.num("order"), obj: prefixFolder + id})
	}
	for _, o := range byType[ObjRequest] {
		id, ok := strings.CutPrefix(o.ID, prefixReq)
		if !ok {
			continue
		}
		f := fieldReader(o.Fields)
		r := &Request{
			ID: id, Name: f.str("name"), Method: f.str("method"), URL: f.str("url"),
			Body: Body{Kind: docBodyKind(f.str("body_kind")), Text: f.str("body"), ContentType: f.str("content_type")},
		}
		f.into("auth", &r.Auth)
		f.into("settings", &r.Settings)
		c.Requests[id] = r
		p := f.str("folder")
		if _, ok := folders[p]; !ok {
			p = ""
		}
		children[p] = append(children[p], docItem{id: "r:" + id, order: f.num("order"), obj: prefixReq + id})
	}
	var build func(f *Folder, id string)
	build = func(f *Folder, id string) {
		items := children[id]
		sortItems(items)
		for _, it := range items {
			if fid, ok := strings.CutPrefix(it.id, "f:"); ok {
				sub := &Folder{ID: fid, Name: folders[fid].str("name")}
				build(sub, fid)
				f.Items = append(f.Items, Node{Folder: sub})
			} else {
				f.Items = append(f.Items, Node{Request: strings.TrimPrefix(it.id, "r:")})
			}
		}
	}
	build(&c.Root, "")

	// rows, by request and order
	rows := byType[ObjRow]
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := fieldReader(rows[i].Fields), fieldReader(rows[j].Fields)
		if a.num("order") != b.num("order") {
			return a.num("order") < b.num("order")
		}
		return rows[i].ID < rows[j].ID
	})
	for _, o := range rows {
		id, ok := strings.CutPrefix(o.ID, prefixRow)
		if !ok {
			continue
		}
		f := fieldReader(o.Fields)
		r := c.Requests[f.str("req")]
		if r == nil {
			continue
		}
		kv := KV{ID: id, Key: f.str("key"), Value: f.str("value"), Off: f.flag("off")}
		switch f.str("list") {
		case ListQuery:
			r.Query = append(r.Query, kv)
		case ListHeader:
			r.Headers = append(r.Headers, kv)
		case ListForm:
			var x struct {
				FileName *string `json:"file_name"`
			}
			f.into("extra", &x)
			ff := FormField{KV: kv}
			if x.FileName != nil {
				ff.FileName = *x.FileName
			}
			r.Body.Form = append(r.Body.Form, ff)
		case ListAssert:
			var x assertExtra
			f.into("extra", &x)
			r.Asserts = append(r.Asserts, Assertion{ID: id, Target: kv.Key, Path: x.Path, Op: x.Op, Value: kv.Value})
		case ListExtract:
			var x extractExtra
			f.into("extra", &x)
			r.Extracts = append(r.Extracts, Extract{ID: id, From: x.From, Path: kv.Value, Var: kv.Key})
		}
	}

	// environments
	var envItems []docItem
	envs := map[string]*Env{}
	for _, o := range byType[ObjEnv] {
		id, ok := strings.CutPrefix(o.ID, prefixEnv)
		if !ok {
			continue
		}
		f := fieldReader(o.Fields)
		envs[id] = &Env{ID: id, Name: f.str("name")}
		envItems = append(envItems, docItem{id: id, order: f.num("order"), obj: o.ID})
	}
	vs := byType[ObjEnvVar]
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := fieldReader(vs[i].Fields), fieldReader(vs[j].Fields)
		if a.num("order") != b.num("order") {
			return a.num("order") < b.num("order")
		}
		return vs[i].ID < vs[j].ID
	})
	for _, o := range vs {
		id, ok := strings.CutPrefix(o.ID, prefixEnvVar)
		if !ok {
			continue
		}
		f := fieldReader(o.Fields)
		e := envs[f.str("env")]
		if e == nil {
			continue
		}
		v := Var{ID: id, Name: f.str("name"), Secret: f.flag("secret")}
		if !v.Secret {
			v.Value = f.str("value")
		}
		e.Vars = append(e.Vars, v)
	}
	sortItems(envItems)
	for _, it := range envItems {
		c.Envs = append(c.Envs, *envs[it.id])
	}
	return c, nil
}

// Diff is the ops that turn prev's document into next's: put for a new
// object (all its fields), set for each changed field (a dropped field is
// set to null), del for a removed object. Puts come parents first, then the
// sets, then the dels. An envvar turning secret drops its value before the
// flag (the server refuses a secret with a value, collab.md 7.2).
func Diff(prev, next *Collection) []collab.Op {
	var a, b collab.Snapshot
	if prev != nil {
		a = DocFromCollection(prev)
	}
	if next != nil {
		b = DocFromCollection(next)
	}
	old := map[string]collab.Object{}
	for _, o := range a.Objects {
		old[o.ID] = o
	}
	var puts, sets, dels []collab.Op
	seen := map[string]bool{}
	for _, o := range b.Objects {
		seen[o.ID] = true
		po, ok := old[o.ID]
		if !ok || po.Type != o.Type {
			if ok {
				dels = append(dels, collab.Op{Op: collab.OpDel, ID: o.ID})
			}
			puts = append(puts, collab.Op{Op: collab.OpPut, ID: o.ID, Type: o.Type, Fields: o.Fields})
			continue
		}
		keys := map[string]bool{}
		for k := range o.Fields {
			keys[k] = true
		}
		for k := range po.Fields {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		secretOn := o.Type == ObjEnvVar && string(o.Fields["secret"]) == "true"
		slices.SortFunc(names, func(x, y string) int {
			if o.Type == ObjEnvVar && (x == "value" || y == "value") && x != y {
				// value first when the variable becomes secret, last otherwise
				if (x == "value") == secretOn {
					return -1
				}
				return 1
			}
			return cmp.Compare(x, y)
		})
		for _, k := range names {
			nv, pv := o.Fields[k], po.Fields[k]
			if nv == nil {
				nv = json.RawMessage("null")
			}
			if pv == nil {
				pv = json.RawMessage("null")
			}
			if !sameJSON(nv, pv) {
				sets = append(sets, collab.Op{Op: collab.OpSet, ID: o.ID, Field: k, Value: nv})
			}
		}
	}
	for _, o := range a.Objects {
		if !seen[o.ID] {
			dels = append(dels, collab.Op{Op: collab.OpDel, ID: o.ID})
		}
	}
	rank := map[string]int{ObjCol: 0, ObjEnv: 1, ObjEnvVar: 2, ObjFolder: 3, ObjRequest: 4, ObjRow: 5}
	slices.SortStableFunc(puts, func(x, y collab.Op) int { return cmp.Compare(rank[x.Type], rank[y.Type]) })
	out := append(puts, sets...)
	return append(out, dels...)
}

// sameJSON compares two JSON values by meaning (key order ignored).
func sameJSON(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

// HeldField is a field Gate kept back: op OpIndex of the input, object Obj,
// field F, and the detector's first finding in it; Text is the string the
// finding's offsets are in (the field's value, or a string inside it).
type HeldField struct {
	OpIndex int
	Obj, F  string
	Finding Finding
	Text    string
}

// Gate is GateIn with no base document: a set of an existing row's or
// variable's value then has no name to read (use GateIn for edits).
func Gate(ops []collab.Op) (send []collab.Op, held []HeldField) { return GateIn(nil, ops) }

// GateIn runs the secret detector over every string an op would share
// (C3.10 layer 2). A set whose value looks secret is held whole; a put keeps
// its other fields and drops the held ones. Names: a row's "key" or a
// variable's "name" names its "value" — from the op itself, an earlier set
// in ops, or else base (the shared state the ops apply to); inside an object
// value the sibling "name"/"key" does; a URL's query values are scanned
// under their parameter names.
func GateIn(base *Collection, ops []collab.Op) (send []collab.Op, held []HeldField) {
	names := valueNames(base, ops)
	for i, op := range ops {
		switch op.Op {
		case collab.OpSet:
			if f, text, ok := scanField(op.Field, op.Value, names[op.ID]); ok {
				held = append(held, HeldField{OpIndex: i, Obj: op.ID, F: op.Field, Finding: f, Text: text})
				continue
			}
			send = append(send, op)
		case collab.OpPut:
			fr := fieldReader(op.Fields)
			hint := fr.str("key")
			if hint == "" {
				hint = fr.str("name")
			}
			names := make([]string, 0, len(op.Fields))
			for k := range op.Fields {
				names = append(names, k)
			}
			slices.Sort(names)
			var keep map[string]json.RawMessage
			for _, k := range names {
				if f, text, ok := scanField(k, op.Fields[k], hint); ok {
					held = append(held, HeldField{OpIndex: i, Obj: op.ID, F: k, Finding: f, Text: text})
					if keep == nil {
						keep = maps.Clone(op.Fields)
					}
					delete(keep, k)
				}
			}
			if keep != nil {
				op.Fields = keep
			}
			send = append(send, op)
		default:
			send = append(send, op)
		}
	}
	return send, held
}

// valueNames maps each row and variable object to the name of its value:
// the row's key or the variable's name in base, replaced by a put or set of
// it in ops.
func valueNames(base *Collection, ops []collab.Op) map[string]string {
	names := map[string]string{}
	named := func(id string) bool { return strings.HasPrefix(id, prefixRow) || strings.HasPrefix(id, prefixEnvVar) }
	if base != nil {
		for _, o := range DocFromCollection(base).Objects {
			if !named(o.ID) {
				continue
			}
			f := fieldReader(o.Fields)
			if n := cmp.Or(f.str("key"), f.str("name")); n != "" {
				names[o.ID] = n
			}
		}
	}
	for _, op := range ops {
		if !named(op.ID) {
			continue
		}
		switch op.Op {
		case collab.OpPut:
			f := fieldReader(op.Fields)
			names[op.ID] = cmp.Or(f.str("key"), f.str("name"))
		case collab.OpSet:
			if op.Field == "key" || op.Field == "name" {
				var n string
				_ = json.Unmarshal(op.Value, &n)
				names[op.ID] = n
			}
		}
	}
	return names
}

// scanField finds the first secret-looking string in a field value.
func scanField(field string, raw json.RawMessage, hint string) (Finding, string, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return Finding{}, "", false
	}
	if field == "auth" {
		if f, s, ok := scanAuth(raw); ok {
			return f, s, true
		}
	}
	name := field
	if field == "value" {
		name = hint
	}
	if f, s, ok := scanValue(v, name); ok {
		return f, s, true
	}
	if u, ok := v.(string); ok && field == "url" {
		if f, ok := scanQuery(u); ok {
			return f, u, true
		}
	}
	return Finding{}, "", false
}

// scanAuth holds a request auth's typed-in password, token or key whatever
// its name (RedactRequest's rule): the detector's span if it has one, else
// the whole trimmed value.
func scanAuth(raw json.RawMessage) (Finding, string, bool) {
	var a Auth
	if json.Unmarshal(raw, &a) != nil {
		return Finding{}, "", false
	}
	for _, v := range []string{a.Pass, a.Token, a.Value} {
		if !literalCredential(v) {
			continue
		}
		if fs := Detect("", v); len(fs) > 0 {
			return fs[0], v, true
		}
		s, e := trimmedSpan(v)
		return Finding{Rule: "literal_credential", Start: s, End: e}, v, true
	}
	return Finding{}, "", false
}

// scanQuery finds the first secret-looking query value of a URL, scanned
// under its parameter name; the offsets are in u.
func scanQuery(u string) (Finding, bool) {
	q := strings.IndexByte(u, '?')
	if q < 0 {
		return Finding{}, false
	}
	end := len(u)
	if h := strings.IndexByte(u[q:], '#'); h >= 0 {
		end = q + h
	}
	at := q + 1
	for _, part := range strings.Split(u[q+1:end], "&") {
		k, v, ok := strings.Cut(part, "=")
		if ok && v != "" {
			name := k
			if dk, err := url.QueryUnescape(k); err == nil {
				name = dk
			}
			if fs := Detect(name, v); len(fs) > 0 {
				off := at + len(k) + 1
				f := fs[0]
				f.Start, f.End = f.Start+off, f.End+off
				return f, true
			}
		}
		at += len(part) + 1
	}
	return Finding{}, false
}

func scanValue(v any, name string) (Finding, string, bool) {
	switch x := v.(type) {
	case string:
		if fs := Detect(name, x); len(fs) > 0 {
			return fs[0], x, true
		}
	case []any:
		for _, e := range x {
			if f, s, ok := scanValue(e, name); ok {
				return f, s, true
			}
		}
	case map[string]any:
		hint, _ := x["name"].(string)
		if hint == "" {
			hint, _ = x["key"].(string)
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			n := k
			if k == "value" {
				n = hint
			}
			if f, s, ok := scanValue(x[k], n); ok {
				return f, s, true
			}
		}
	}
	return Finding{}, "", false
}

// docBytes is the size of c's canonical document JSON.
func (c *Collection) docBytes() int { return len(rawJSON(DocFromCollection(c))) }

// CheckSharedLimits checks c against the shared collection limits before
// it is uploaded or edited (collab.md 7.2): requests, body, field, document.
func CheckSharedLimits(c *Collection) error {
	folders, deepest := 0, 0
	var walk func(f *Folder, d int)
	walk = func(f *Folder, d int) {
		deepest = max(deepest, d)
		for _, it := range f.Items {
			if it.Folder != nil && folders <= MaxSharedFolders && deepest <= MaxSharedFolderDepth {
				folders++
				walk(it.Folder, d+1)
			}
		}
	}
	walk(&c.Root, 0)
	if folders > MaxSharedFolders {
		return fmt.Errorf("%w (%d)", ErrSharedTooManyFolders, folders)
	}
	if deepest > MaxSharedFolderDepth {
		return ErrSharedFoldersTooDeep
	}
	ids := c.Root.RequestIDs()
	if len(ids) > MaxSharedRequests {
		return fmt.Errorf("%w (%d)", ErrSharedTooManyRequests, len(ids))
	}
	for _, id := range ids {
		r := c.Requests[id]
		if r == nil {
			continue
		}
		if len(r.Body.Text) > MaxSharedBody || len(rawJSON(r.Body.Text)) > MaxSharedField {
			return ErrSharedBodyTooLarge
		}
	}
	snap := DocFromCollection(c)
	for _, o := range snap.Objects {
		for _, v := range o.Fields {
			if len(v) > MaxSharedField {
				return ErrSharedFieldTooLarge
			}
		}
	}
	if n := len(rawJSON(snap)); n > MaxSharedDoc {
		return fmt.Errorf("%w (%d bytes)", ErrSharedDocTooLarge, n)
	}
	return nil
}

// ---- my overlay ----

// MarkSecret records that I keep variable key ("<envID>/<name>") secret,
// with its allowed hosts. The mark holds whatever the document says.
func (m *Mine) MarkSecret(key string, hosts []string) {
	if m.Hosts == nil {
		m.Hosts = map[string][]string{}
	}
	if hosts == nil {
		hosts = []string{}
	}
	m.Hosts[key] = slices.Clone(hosts)
}

// IsSecret reports whether I keep variable key secret.
func (m *Mine) IsSecret(key string) bool {
	if m == nil {
		return false
	}
	_, ok := m.Hosts[key]
	return ok
}

// ApplyMine marks secret every variable of c I keep secret, whatever the
// document says (a peer turning the flag off never reveals or replaces my
// value: a secret reads only the vault), drops its shared value and shows
// my allowed hosts on it (DocFromCollection never shares them).
func ApplyMine(c *Collection, m *Mine) {
	if m == nil || c == nil {
		return
	}
	mark := func(v *Var, key string) {
		if m.IsSecret(key) {
			v.Secret, v.Value = true, ""
		}
		if v.Secret {
			v.AllowedHosts = slices.Clone(m.Hosts[key])
		}
	}
	for i := range c.Envs {
		e := &c.Envs[i]
		for j := range e.Vars {
			mark(&e.Vars[j], envSlot(e)+"/"+e.Vars[j].Name)
		}
	}
	for j := range c.Vars {
		mark(&c.Vars[j], "col:"+c.ID+"/"+c.Vars[j].Name)
	}
}

// ---- local files: api/shared/<doc>.json, .mine.json, .lost.json ----

// SharedCopy is the offline copy of a shared collection (no secrets: the
// document has none).
type SharedCopy struct {
	Title    string          `json:"title"`
	SpaceID  string          `json:"space_id,omitempty"`
	Snapshot collab.Snapshot `json:"snapshot"`
}

// LostEntry is my overwritten text (a body a peer replaced), kept so it can
// be restored.
type LostEntry struct {
	Request string    `json:"request"`
	Field   string    `json:"field"`
	Text    string    `json:"text"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at,omitzero"`
}

func (s *LocalStore) sharedPath(docID, suffix string) (string, error) {
	if !validName(docID) || strings.HasPrefix(docID, ".") {
		return "", fmt.Errorf("%w: %q", ErrBadName, docID)
	}
	return filepath.Join(s.Dir, "shared", docID+suffix), nil
}

// SaveSharedCopy writes api/shared/<docID>.json.
func (s *LocalStore) SaveSharedCopy(docID string, cp SharedCopy) error {
	p, err := s.sharedPath(docID, ".json")
	if err != nil {
		return err
	}
	return s.writeJSON(p, cp)
}

// LoadSharedCopy reads the offline copy.
func (s *LocalStore) LoadSharedCopy(docID string) (*SharedCopy, error) {
	p, err := s.sharedPath(docID, ".json")
	if err != nil {
		return nil, err
	}
	var cp SharedCopy
	if err := readJSON(p, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// ListShared lists the doc ids with an offline copy, sorted.
func (s *LocalStore) ListShared() ([]string, error) {
	names, err := listNames(filepath.Join(s.Dir, "shared"), false)
	if err != nil {
		return nil, err
	}
	out := names[:0]
	for _, n := range names {
		if !strings.HasSuffix(n, ".mine") && !strings.HasSuffix(n, ".lost") {
			out = append(out, n)
		}
	}
	return out, nil
}

// SaveMine writes my overlay api/shared/<docID>.mine.json.
func (s *LocalStore) SaveMine(docID string, m *Mine) error {
	p, err := s.sharedPath(docID, ".mine.json")
	if err != nil {
		return err
	}
	if m == nil {
		m = &Mine{}
	}
	return s.writeJSON(p, m)
}

// LoadMine reads my overlay (missing = empty).
func (s *LocalStore) LoadMine(docID string) (*Mine, error) {
	p, err := s.sharedPath(docID, ".mine.json")
	if err != nil {
		return nil, err
	}
	m := &Mine{}
	if err := readJSON(p, m); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return m, err
	}
	return m, nil
}

// AddLost appends e to api/shared/<docID>.lost.json, keeping the last
// MaxLost entries.
func (s *LocalStore) AddLost(docID string, e LostEntry) error {
	list, err := s.LoadLost(docID)
	if err != nil {
		return err
	}
	list = append(list, e)
	if len(list) > MaxLost {
		list = list[len(list)-MaxLost:]
	}
	p, _ := s.sharedPath(docID, ".lost.json")
	return s.writeJSON(p, list)
}

// LoadLost reads the overwritten texts (missing = none), oldest first.
func (s *LocalStore) LoadLost(docID string) ([]LostEntry, error) {
	p, err := s.sharedPath(docID, ".lost.json")
	if err != nil {
		return nil, err
	}
	var list []LostEntry
	if err := readJSON(p, &list); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return list, nil
}

// RemoveShared deletes the local files of a shared collection (I left it).
func (s *LocalStore) RemoveShared(docID string) error {
	for _, suf := range []string{".json", ".mine.json", ".lost.json"} {
		p, err := s.sharedPath(docID, suf)
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// docBodyKind is a document's body kind; one the server's enum does not
// know (an old or forged document) reads as no body.
func docBodyKind(s string) BodyKind {
	switch k := BodyKind(s); k {
	case "", BodyNone, BodyJSON, BodyURLEncoded, BodyMultipart, BodyRaw:
		return k
	}
	return BodyNone
}
