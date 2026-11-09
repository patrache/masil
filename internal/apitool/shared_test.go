package apitool

import (
	"cmp"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/secrets"
)

// sharedFixture is a collection with every part the document carries.
func sharedFixture() *Collection {
	pay := &Request{
		ID: "r-pay", Name: "Create payment", Method: "POST", URL: "{{base_url}}/v1/payments",
		Query:    []KV{{ID: "q1", Key: "dry", Value: "1", Off: true}},
		Headers:  []KV{{ID: "h1", Key: "Authorization", Value: "Bearer {{token}}"}, {ID: "h2", Key: "Content-Type", Value: "application/json"}},
		Body:     Body{Kind: BodyJSON, Text: `{"amount": 12000}`},
		Auth:     Auth{Kind: AuthBearer, Token: "{{token}}"},
		Asserts:  []Assertion{{ID: "a1", Target: "status", Op: "eq", Value: "201"}, {ID: "a2", Target: "json", Path: "$.id", Op: "exists"}},
		Extracts: []Extract{{ID: "x1", From: "json", Path: "$.id", Var: "pay_id"}},
		Settings: Settings{TimeoutMS: 5000, NoFollow: true},
	}
	up := &Request{
		ID: "r-up", Name: "Upload", Method: "POST", URL: "{{base_url}}/files",
		Body: Body{Kind: BodyMultipart, Form: []FormField{
			{KV: KV{ID: "f1", Key: "note", Value: "hi"}},
			{KV: KV{ID: "f2", Key: "file"}, File: "/home/sam/secret/report.pdf", FileName: "report.pdf"},
		}},
	}
	get := &Request{ID: "r-get", Name: "Get payment", Method: "GET", URL: "{{base_url}}/v1/payments/{{pay_id}}"}
	return &Collection{
		Version: CollectionVersion, Name: "Payments team",
		Vars: []Var{{Name: "base_url", Value: "https://api.example.com"}, {Name: "col_key", Secret: true}},
		Root: Folder{Items: []Node{
			{Request: "r-get"},
			{Folder: &Folder{ID: "fo-pay", Name: "Payments", Items: []Node{{Request: "r-pay"}, {Folder: &Folder{ID: "fo-in", Name: "Inner", Items: []Node{{Request: "r-up"}}}}}}},
		}},
		Requests: map[string]*Request{"r-pay": pay, "r-up": up, "r-get": get},
		Envs: []Env{
			{ID: "e-stg", Name: "staging", Vars: []Var{{ID: "v1", Name: "base_url", Value: "https://stg.example.com"}, {ID: "v2", Name: "token", Secret: true, AllowedHosts: []string{"stg.example.com"}}}},
			{ID: "e-prod", Name: "prod", Vars: []Var{{ID: "v3", Name: "base_url", Value: "https://api.example.com"}}},
		},
	}
}

func TestDocRoundTrip(t *testing.T) {
	c := sharedFixture()
	snap := DocFromCollection(c)
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "/home/sam/secret") {
		t.Fatalf("the form file path went into the document: %s", raw)
	}
	for _, o := range snap.Objects {
		if !collab.IsName(o.ID) || !collab.IsName(o.Type) {
			t.Fatalf("bad object name %q / %q", o.ID, o.Type)
		}
	}
	// The snapshot passes the client frame's checks (put of each object).
	back, err := CollectionFromDoc(snap)
	if err != nil {
		t.Fatal(err)
	}
	want := sharedFixture()
	want.Requests["r-up"].Body.Form[1].File = "" // the path stays on this PC
	want.Envs[0].Vars[1].AllowedHosts = nil      // my hosts live in .mine.json
	if !reflect.DeepEqual(back, want) {
		gb, _ := json.MarshalIndent(back, "", " ")
		gw, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("round trip differs:\n got %s %v\nwant %s %v", gb, back.Requests, gw, want.Requests)
	}
	for id, r := range want.Requests {
		if !reflect.DeepEqual(back.Requests[id], r) {
			t.Fatalf("request %s:\n got %+v\nwant %+v", id, back.Requests[id], r)
		}
	}
}

func TestDocNoSecretValue(t *testing.T) {
	c := sharedFixture()
	c.Envs[0].Vars[1].Value = "canary-Q9x7Z2" // never set on a secret var, but even so
	c.Vars[1].Value = "canary-Q9x7Z2"
	raw, _ := json.Marshal(DocFromCollection(c))
	if strings.Contains(string(raw), "canary") {
		t.Fatalf("a secret value went into the document: %s", raw)
	}
}

func TestCollectionFromDocNeedsCol(t *testing.T) {
	if _, err := CollectionFromDoc(collab.Snapshot{}); !errors.Is(err, ErrNotCollection) {
		t.Fatalf("err = %v, want ErrNotCollection", err)
	}
}

func TestAssignIDs(t *testing.T) {
	c := sharedFixture()
	c.Requests["r-get"].Headers = []KV{{Key: "A"}, {ID: "h1", Key: "B"}} // empty and duplicate (h1 is r-pay's)
	c.Root.Items[1].Folder.ID = ""
	AssignIDs(c)
	rowIDs := map[string]bool{}
	for _, r := range c.Requests {
		for _, kv := range append(slices.Clone(r.Query), r.Headers...) {
			if kv.ID == "" || rowIDs[kv.ID] {
				t.Fatalf("row id %q empty or duplicate", kv.ID)
			}
			rowIDs[kv.ID] = true
		}
	}
	if c.Root.Items[1].Folder.ID == "" || c.Requests["r-get"].Headers[1].ID != "h1" {
		t.Fatalf("ids: %+v %+v", c.Requests["r-get"].Headers, c.Root.Items[1].Folder)
	}
	if c.Requests["r-pay"].Headers[0].ID == "h1" {
		t.Fatal("the duplicate id stayed")
	}
	if c.Requests["r-pay"].Headers[1].ID != "h2" {
		t.Fatal("a valid unique id changed")
	}
	// The document keeps the assigned ids: a round trip gives them back.
	back, _ := CollectionFromDoc(DocFromCollection(c))
	if back.Requests["r-get"].Headers[0].ID != c.Requests["r-get"].Headers[0].ID {
		t.Fatal("assigned id not kept by the document")
	}
}

func TestDiffRowLevel(t *testing.T) {
	prev := sharedFixture()
	next := sharedFixture()
	next.Requests["r-pay"].Headers[0].Value = "Bearer {{token2}}"
	next.Requests["r-pay"].Headers[1].Value = "application/json; charset=utf-8"
	ops := Diff(prev, next)
	if len(ops) != 2 {
		t.Fatalf("ops = %+v, want two sets", ops)
	}
	for _, o := range ops {
		if o.Op != collab.OpSet || !strings.HasPrefix(o.ID, "row:") || o.Field != "value" {
			t.Fatalf("op = %+v", o)
		}
	}
	if ops[0].ID == ops[1].ID {
		t.Fatal("both sets on one row")
	}
	// Applying the diff to prev's document gives next's.
	d := collab.NewDoc(DocFromCollection(prev))
	d.Apply(1, ops)
	got, _ := CollectionFromDoc(d.Snapshot())
	want, _ := CollectionFromDoc(DocFromCollection(next))
	if !reflect.DeepEqual(got, want) {
		t.Fatal("diff applied does not give next")
	}
}

func TestDiffAddMoveDelete(t *testing.T) {
	prev := sharedFixture()
	next := sharedFixture()
	next.Requests["r-new"] = &Request{ID: "r-new", Method: "GET", URL: "/x", Headers: []KV{{ID: "hn", Key: "X", Value: "1"}}}
	next.Root.Items = append(next.Root.Items, Node{Request: "r-new"})
	next.Root.removeRequest("r-up")
	delete(next.Requests, "r-up")
	next.Envs[1].Vars = append(next.Envs[1].Vars, Var{ID: "v9", Name: "token", Secret: true})
	ops := Diff(prev, next)
	if err := collab.CheckOps(ops); err != nil {
		t.Fatalf("ops fail the frame check: %v", err)
	}
	d := collab.NewDoc(DocFromCollection(prev))
	d.Apply(1, ops)
	got, _ := CollectionFromDoc(d.Snapshot())
	want, _ := CollectionFromDoc(DocFromCollection(next))
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(want)
		t.Fatalf("diff applied:\n got %s\nwant %s", gb, wb)
	}
	if len(Diff(next, next)) != 0 {
		t.Fatal("no change must give no ops")
	}
}

func TestGateHoldsDetectedFields(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	ops := []collab.Op{
		{Op: collab.OpSet, ID: "row:h1", Field: "value", Value: mustRaw("Bearer " + jwt)},
		{Op: collab.OpSet, ID: "row:h2", Field: "value", Value: mustRaw("application/json")},
		{Op: collab.OpPut, ID: "row:h3", Type: "row", Fields: map[string]json.RawMessage{
			"req": mustRaw("r-pay"), "key": mustRaw("X-Api-Key"), "value": mustRaw("AKIAIOSFODNN7EXAMPLE"),
		}},
		{Op: collab.OpSet, ID: "req:r-pay", Field: "auth", Value: mustRaw(map[string]string{"kind": "bearer", "token": jwt})},
	}
	send, held := Gate(ops)
	if len(held) != 3 {
		t.Fatalf("held = %+v", held)
	}
	if held[0].OpIndex != 0 || held[0].Obj != "row:h1" || held[0].F != "value" || held[0].Finding.Rule != "jwt" {
		t.Fatalf("held[0] = %+v", held[0])
	}
	raw, _ := json.Marshal(send)
	if strings.Contains(string(raw), jwt) || strings.Contains(string(raw), "AKIA") {
		t.Fatalf("a detected value is in send: %s", raw)
	}
	if len(send) != 2 || send[0].ID != "row:h2" || send[1].ID != "row:h3" || send[1].Fields["value"] != nil || send[1].Fields["key"] == nil {
		t.Fatalf("send = %s", raw)
	}
}

func TestSharedLimits(t *testing.T) {
	if err := CheckSharedLimits(sharedFixture()); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	many := sharedFixture()
	for i := len(many.Requests); i < MaxSharedRequests+1; i++ {
		id := "r" + strings.Repeat("x", 1) + itoa(i)
		many.Requests[id] = &Request{ID: id, Method: "GET", URL: "/"}
		many.Root.Items = append(many.Root.Items, Node{Request: id})
	}
	if err := CheckSharedLimits(many); !errors.Is(err, ErrSharedTooManyRequests) {
		t.Fatalf("1001 requests: %v", err)
	}
	big := sharedFixture()
	big.Requests["r-pay"].Body.Text = strings.Repeat("a", MaxSharedBody+1)
	if err := CheckSharedLimits(big); !errors.Is(err, ErrSharedBodyTooLarge) {
		t.Fatalf("65537B body: %v", err)
	}
	field := sharedFixture()
	field.Requests["r-pay"].Body.Text = strings.Repeat("a", MaxSharedField)
	if err := CheckSharedLimits(field); !errors.Is(err, ErrSharedBodyTooLarge) {
		t.Fatalf("a body over the field limit: %v", err)
	}
	doc := sharedFixture()
	for i := 0; doc.docBytes() <= MaxSharedDoc; i++ {
		id := "big" + itoa(i)
		doc.Requests[id] = &Request{ID: id, Method: "POST", URL: "/", Body: Body{Kind: BodyRaw, Text: strings.Repeat("b", 60000)}}
		doc.Root.Items = append(doc.Root.Items, Node{Request: id})
	}
	if err := CheckSharedLimits(doc); !errors.Is(err, ErrSharedDocTooLarge) {
		t.Fatalf("a 2MB+ document: %v", err)
	}
}

func TestPeerUnsetsSecretFlagKeepsMyValueLocal(t *testing.T) {
	c := sharedFixture()
	mine := &Mine{}
	mine.MarkSecret("e-stg/token", []string{"stg.example.com"})
	// hana turns the secret flag off and writes a value.
	d := collab.NewDoc(DocFromCollection(c))
	d.Apply(1, []collab.Op{
		{Op: collab.OpSet, ID: "envvar:v2", Field: "secret", Value: mustRaw(false)},
		{Op: collab.OpSet, ID: "envvar:v2", Field: "value", Value: mustRaw("x")},
	})
	got, err := CollectionFromDoc(d.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	ApplyMine(got, mine)
	v := got.Envs[0].Vars[1]
	if !v.Secret || v.Value != "" || !sameStrings(v.AllowedHosts, "stg.example.com") {
		t.Fatalf("my secret mark lost: %+v", v)
	}
	vault := testVault(t)
	if err := vault.Put(SecretKey("D1", "e-stg", "token"), secrets.New("canary-Q9x7Z2")); err != nil {
		t.Fatal(err)
	}
	s := &Scope{Env: &got.Envs[0], Mine: mine, Vault: vault, DocID: "D1", CollectionID: "D1"}
	val, err := s.Lookup("token")
	if err != nil || val.Text != "canary-Q9x7Z2" || !val.Secret || !sameStrings(val.AllowedHosts, "stg.example.com") {
		t.Fatalf("Lookup = %+v, %v", val, err)
	}
	// An edit after that diffs nothing about the secret.
	next, _ := CollectionFromDoc(d.Snapshot())
	ApplyMine(next, mine)
	next.Requests["r-get"].Name = "Get"
	for _, op := range Diff(got, next) {
		b, _ := json.Marshal(op)
		if strings.Contains(string(b), "canary") || op.ID == "envvar:v2" {
			t.Fatalf("op touches my secret: %s", b)
		}
	}
}

func TestSharedFiles(t *testing.T) {
	ls := OpenLocal(config.Paths{Dir: t.TempDir()})
	snap := DocFromCollection(sharedFixture())
	if err := ls.SaveSharedCopy("D1", SharedCopy{Title: "Payments team", SpaceID: "S1", Snapshot: snap}); err != nil {
		t.Fatal(err)
	}
	cp, err := ls.LoadSharedCopy("D1")
	if err != nil || cp.Title != "Payments team" || len(cp.Snapshot.Objects) != len(snap.Objects) {
		t.Fatalf("copy = %+v, %v", cp, err)
	}
	m := &Mine{}
	m.MarkSecret("e-stg/token", nil)
	if err := ls.SaveMine("D1", m); err != nil {
		t.Fatal(err)
	}
	m2, err := ls.LoadMine("D1")
	if err != nil || !m2.IsSecret("e-stg/token") {
		t.Fatalf("mine = %+v, %v", m2, err)
	}
	for i := range 25 {
		if err := ls.AddLost("D1", LostEntry{Request: "r-pay", Field: "body", Text: itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	lost, err := ls.LoadLost("D1")
	if err != nil || len(lost) != MaxLost || lost[len(lost)-1].Text != "24" {
		t.Fatalf("lost = %d %v", len(lost), err)
	}
	if _, err := ls.LoadSharedCopy("../x"); err == nil {
		t.Fatal("a doc id with a path was accepted")
	}
}

func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func sameStrings(got []string, want ...string) bool { return reflect.DeepEqual(got, want) }

// A set of an existing row's or variable's value is scanned under the
// object's name (named_high_entropy, C3.10 layer 2), a URL's query values
// under their parameter names; a value I kept local (the held put sent
// without it) is held again on the next edit.
func TestGateNamesSetValues(t *testing.T) {
	const key = "q8Zr3Kp9Lm2Xw7Tb5Nc4Vh6Jd1Fs0Gy" // no known prefix
	fixture := func() *Collection {
		c := sharedFixture()
		c.Requests["r-pay"].Headers = append(c.Requests["r-pay"].Headers, KV{ID: "h9", Key: "X-Api-Key", Value: "{{k}}"})
		c.Envs[1].Vars = append(c.Envs[1].Vars, Var{ID: "v9", Name: "API_TOKEN", Value: "x"})
		return c
	}
	base := fixture()
	ops := []collab.Op{
		{Op: collab.OpSet, ID: "row:h9", Field: "value", Value: mustRaw(key)},
		{Op: collab.OpSet, ID: "envvar:v9", Field: "value", Value: mustRaw(key)},
		{Op: collab.OpSet, ID: "row:h2", Field: "key", Value: mustRaw("X-Session")},
		{Op: collab.OpSet, ID: "row:h2", Field: "value", Value: mustRaw(key)},
		{Op: collab.OpSet, ID: "req:r-get", Field: "url", Value: mustRaw("https://x.example/a?page=2&api_key=" + key + "#top")},
		{Op: collab.OpSet, ID: "row:q1", Field: "value", Value: mustRaw(key)}, // query row "dry": not a secret name
	}
	send, held := GateIn(base, ops)
	if len(held) != 4 {
		t.Fatalf("held = %+v", held)
	}
	for i, h := range held {
		if h.Text[h.Finding.Start:h.Finding.End] != key || h.Finding.Rule != "named_high_entropy" {
			t.Fatalf("held[%d] = %+v (%q)", i, h, h.Text[h.Finding.Start:h.Finding.End])
		}
	}
	if held[3].Obj != "req:r-get" || held[3].F != "url" {
		t.Fatalf("held[3] = %+v", held[3])
	}
	if len(send) != 2 || send[0].Field != "key" || send[1].ID != "row:q1" {
		t.Fatalf("send = %+v", send)
	}

	// 취소 on a new header row: its put went out without the value; the
	// next edit of the request diffs the value again and holds it.
	next := fixture()
	next.Requests["r-pay"].Headers = append(next.Requests["r-pay"].Headers, KV{ID: "h10", Key: "X-API-Key", Value: key})
	s1, h1 := GateIn(base, Diff(base, next))
	if len(h1) != 1 {
		t.Fatalf("first held = %+v", h1)
	}
	d := collab.NewDoc(DocFromCollection(base))
	d.Apply(d.Seq()+1, s1)
	shared, err := CollectionFromDoc(d.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	next.Requests["r-pay"].Method = "PUT"
	s2, h2 := GateIn(shared, Diff(shared, next))
	raw, _ := json.Marshal(s2)
	if len(h2) != 1 || strings.Contains(string(raw), key) {
		t.Fatalf("the kept value left on the next edit: held=%+v send=%s", h2, raw)
	}
}

// A literal Basic password, bearer token or API key in a request's auth is
// held whatever its name (as RedactRequest masks it); placeholders pass.
func TestGateHoldsLiteralAuth(t *testing.T) {
	for _, a := range []Auth{
		{Kind: AuthBasic, User: "admin", Pass: "q8Zr3Kp9Lm2Xw7Tb5Nc4"},
		{Kind: AuthAPIKey, Name: "appid", In: "header", Value: "hunter2hunter2"},
		{Kind: AuthBearer, Token: "short-token"},
	} {
		c := sharedFixture()
		c.Requests["r-get"].Auth = a
		send, held := Gate(Diff(nil, c))
		if len(held) != 1 || held[0].Obj != "req:r-get" || held[0].F != "auth" {
			t.Fatalf("%+v: held = %+v", a, held)
		}
		lit := cmp.Or(a.Pass, a.Value, a.Token)
		if got := held[0].Text[held[0].Finding.Start:held[0].Finding.End]; got != lit {
			t.Fatalf("%+v: finding = %q", a, got)
		}
		raw, _ := json.Marshal(send)
		if strings.Contains(string(raw), lit) {
			t.Fatalf("%+v: the literal is in send", a)
		}
	}
	c := sharedFixture()
	c.Requests["r-get"].Auth = Auth{Kind: AuthBasic, User: "admin", Pass: "{{pw}}"}
	if _, held := Gate(Diff(nil, c)); len(held) != 0 {
		t.Fatalf("placeholders held: %+v", held)
	}
}
