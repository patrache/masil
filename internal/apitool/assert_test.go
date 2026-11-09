package apitool

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func jsonResp(body string) *Response {
	return &Response{
		Status: 201, StatusText: "Created", ContentType: "application/json",
		Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req-42"}},
		Body:   []byte(body), Size: int64(len(body)),
		Timing: Timing{Total: 142 * time.Millisecond},
	}
}

const payment = `{"id":"pay_1","amount":12000,"paid":true,"memo":null,"customer":{"name":"hana","tags":["a","b"]},"items":[{"id":1},{"id":2},{"id":3}],"x-y":"dash"}`

func TestAssertions(t *testing.T) {
	r := jsonResp(payment)
	cases := []struct {
		a    Assertion
		pass bool
	}{
		{Assertion{Target: "status", Op: "eq", Value: "201"}, true},
		{Assertion{Target: "status", Op: "eq", Value: "200"}, false},
		{Assertion{Target: "status", Op: "in", Value: "2xx"}, true},
		{Assertion{Target: "status", Op: "in", Value: "4xx,200"}, false},
		{Assertion{Target: "time", Op: "lt", Value: "500ms"}, true},
		{Assertion{Target: "time", Op: "lt", Value: "100ms"}, false},
		{Assertion{Target: "size", Op: "lt", Value: "1MB"}, true},
		{Assertion{Target: "size", Op: "lt", Value: "10B"}, false},
		{Assertion{Target: "header", Path: "x-request-id", Op: "exists"}, true},
		{Assertion{Target: "header", Path: "X-Missing", Op: "exists"}, false},
		{Assertion{Target: "header", Path: "X-Request-Id", Op: "eq", Value: "req-42"}, true},
		{Assertion{Target: "header", Path: "X-Request-Id", Op: "eq", Value: "req-4"}, false},
		{Assertion{Target: "header", Path: "Content-Type", Op: "contains", Value: "json"}, true},
		{Assertion{Target: "header", Path: "Content-Type", Op: "contains", Value: "xml"}, false},
		{Assertion{Target: "header", Path: "X-Request-Id", Op: "regex", Value: `^req-\d+$`}, true},
		{Assertion{Target: "header", Path: "X-Request-Id", Op: "regex", Value: `^id-`}, false},
		{Assertion{Target: "json", Path: "$.id", Op: "exists"}, true},
		{Assertion{Target: "json", Path: "$.nope", Op: "exists"}, false},
		{Assertion{Target: "json", Path: "$.id", Op: "eq", Value: "pay_1"}, true},
		{Assertion{Target: "json", Path: "$.id", Op: "eq", Value: "pay_2"}, false},
		{Assertion{Target: "json", Path: "$.paid", Op: "eq", Value: "true"}, true},
		{Assertion{Target: "json", Path: "$.memo", Op: "eq", Value: "null"}, true},
		{Assertion{Target: "json", Path: "$.customer.tags", Op: "eq", Value: `["a", "b"]`}, true},
		{Assertion{Target: "json", Path: "$.id", Op: "type", Value: "string"}, true},
		{Assertion{Target: "json", Path: "$.amount", Op: "type", Value: "number"}, true},
		{Assertion{Target: "json", Path: "$.paid", Op: "type", Value: "boolean"}, true},
		{Assertion{Target: "json", Path: "$.items", Op: "type", Value: "array"}, true},
		{Assertion{Target: "json", Path: "$.customer", Op: "type", Value: "object"}, true},
		{Assertion{Target: "json", Path: "$.memo", Op: "type", Value: "null"}, true},
		{Assertion{Target: "json", Path: "$.id", Op: "type", Value: "number"}, false},
		{Assertion{Target: "json", Path: "$.customer.name", Op: "contains", Value: "an"}, true},
		{Assertion{Target: "json", Path: "$.customer.tags", Op: "contains", Value: "b"}, true},
		{Assertion{Target: "json", Path: "$.items[*].id", Op: "contains", Value: "2"}, true},
		{Assertion{Target: "json", Path: "$.customer", Op: "contains", Value: "name"}, true},
		{Assertion{Target: "json", Path: "$.customer.tags", Op: "contains", Value: "z"}, false},
		{Assertion{Target: "json", Path: "$.id", Op: "regex", Value: `^pay_\d$`}, true},
		{Assertion{Target: "json", Path: "$.id", Op: "regex", Value: `^x`}, false},
		{Assertion{Target: "json", Path: "$.items", Op: "len", Value: "3"}, true},
		{Assertion{Target: "json", Path: "$.items[*].id", Op: "len", Value: "3"}, true},
		{Assertion{Target: "json", Path: "$.customer.name", Op: "len", Value: "4"}, true},
		{Assertion{Target: "json", Path: "$.items", Op: "len", Value: "2"}, false},
		{Assertion{Target: "body", Op: "contains", Value: `"paid":true`}, true},
		{Assertion{Target: "body", Op: "contains", Value: "refund"}, false},
		{Assertion{Target: "body", Op: "regex", Value: `"amount":\d+`}, true},
		{Assertion{Target: "body", Op: "regex", Value: `"amount":"`}, false},
		{Assertion{Target: "nope", Op: "eq"}, false},
		{Assertion{Target: "status", Op: "nope", Value: "1"}, false},
	}
	as := make([]Assertion, len(cases))
	for i, c := range cases {
		c.a.ID = string(rune('A' + i))
		as[i] = c.a
	}
	got := Check(as, r)
	if len(got) != len(cases) {
		t.Fatalf("%d results", len(got))
	}
	for i, c := range cases {
		if got[i].ID != as[i].ID || got[i].Pass != c.pass {
			t.Errorf("%+v: pass=%v got=%q err=%q, want %v", c.a, got[i].Pass, got[i].Got, got[i].Err, c.pass)
		}
	}
	// Got shows the actual value
	if got[0].Got != "201" || got[18].Got != "pay_1" {
		t.Errorf("Got = %q, %q", got[0].Got, got[18].Got)
	}
}

func decode(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestJSONPathSubset(t *testing.T) {
	doc := decode(t, `{"a":{"b":[{"c":"deep"}]},"items":[{"id":1},{"id":2},{"id":3}],"x-y":5,"sp ace":{"k":true}}`)
	cases := []struct {
		path string
		n    int
	}{
		{"$", 1},
		{"$.a.b[0].c", 1},
		{"$.items[*].id", 3},
		{"$['x-y']", 1},
		{`$["x-y"]`, 1},
		{"$['sp ace'].k", 1},
		{"$.items[1]", 1},
		{"$.items[9]", 0},
		{"$.missing", 0},
		{"$.a.missing.deeper", 0},
		{"$.items.id", 0},
	}
	for _, c := range cases {
		got, err := EvalPath(doc, c.path)
		if err != nil || len(got) != c.n {
			t.Errorf("%s = %v, %v (want %d)", c.path, got, err, c.n)
		}
	}
	if got, _ := EvalPath(doc, "$.a.b[0].c"); got[0] != "deep" {
		t.Errorf("value = %v", got)
	}
	for _, bad := range []string{"$..a", "a.b", "$.", "$[", "$[-1]", "$['x", "$.a[?(@.x)]", "$.*", ""} {
		if _, err := EvalPath(doc, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestJSONEqNumeric(t *testing.T) {
	r := jsonResp(`{"amount":12000,"rate":0.10,"big":12345678901234567890}`)
	got := Check([]Assertion{
		{Target: "json", Path: "$.amount", Op: "eq", Value: "12000"},
		{Target: "json", Path: "$.amount", Op: "eq", Value: "12000.0"},
		{Target: "json", Path: "$.rate", Op: "eq", Value: "0.1"},
		{Target: "json", Path: "$.big", Op: "eq", Value: "12345678901234567890"},
		{Target: "json", Path: "$.big", Op: "eq", Value: "12345678901234567891"},
	}, r)
	want := []bool{true, true, true, true, false}
	for i, w := range want {
		if got[i].Pass != w {
			t.Errorf("%d: %+v", i, got[i])
		}
	}
}

func TestBadRegexFailsNotPanics(t *testing.T) {
	r := jsonResp(payment)
	got := Check([]Assertion{
		{Target: "body", Op: "regex", Value: "("},
		{Target: "header", Path: "X-Request-Id", Op: "regex", Value: "("},
		{Target: "json", Path: "$.id", Op: "regex", Value: strings.Repeat("a", 1001)},
		{Target: "json", Path: "$..id", Op: "exists"},
		{Target: "time", Op: "lt", Value: "soon"},
	}, r)
	for i, g := range got {
		if g.Pass || g.Err == "" {
			t.Errorf("%d: %+v", i, g)
		}
	}
}

func TestAssertOnTruncatedBody(t *testing.T) {
	r := jsonResp(`{"a":1`)
	r.Truncated = true
	got := Check([]Assertion{{Target: "json", Path: "$.a", Op: "exists"}, {Target: "body", Op: "contains", Value: "zzz"}, {Target: "body", Op: "contains", Value: `"a"`}}, r)
	if got[0].Pass || got[0].Err != ErrBodyTruncated {
		t.Errorf("json on truncated = %+v", got[0])
	}
	if got[1].Pass || got[1].Err != ErrBodyTruncated {
		t.Errorf("body miss on truncated = %+v", got[1])
	}
	if !got[2].Pass {
		t.Errorf("body hit on truncated = %+v", got[2])
	}
	// not JSON at all
	nj := jsonResp("<html>")
	if g := Check([]Assertion{{Target: "json", Path: "$", Op: "exists"}}, nj)[0]; g.Pass || g.Err != ErrNotJSON {
		t.Errorf("not json = %+v", g)
	}
}

func TestExtractAll(t *testing.T) {
	r := jsonResp(payment)
	vals, errs := ExtractAll([]Extract{
		{ID: "1", From: "json", Path: "$.id", Var: "pid"},
		{ID: "2", From: "json", Path: "$.amount", Var: "amt"},
		{ID: "3", From: "json", Path: "$.customer.tags", Var: "tags"},
		{ID: "4", From: "header", Path: "x-request-id", Var: "rid"},
		{ID: "5", From: "json", Path: "$.nope", Var: "n"},
		{ID: "6", From: "header", Path: "X-Nope", Var: "h"},
		{ID: "7", From: "json", Path: "$.id"},
	}, r)
	want := map[string]string{"pid": "pay_1", "amt": "12000", "tags": `["a","b"]`, "rid": "req-42"}
	if len(vals) != len(want) {
		t.Fatalf("vals = %v", vals)
	}
	for k, v := range want {
		if vals[k] != v {
			t.Errorf("%s = %q, want %q", k, vals[k], v)
		}
	}
	if len(errs) != 3 {
		t.Errorf("errs = %v", errs)
	}
}

func TestExtractToSecretGoesToVault(t *testing.T) {
	vault := testVault(t)
	st := testStore(t)
	env := &Env{ID: "e1", Name: "staging", Vars: []Var{{ID: "v1", Name: "token", Secret: true, AllowedHosts: []string{"api.test"}}, {ID: "v2", Name: "user_id"}}}
	s := &Scope{Env: env, Vault: vault, Globals: []Var{{Name: "g"}}}
	r := jsonResp(`{"access_token":"tok-canary-77","user":{"id":"u9"}}`)
	vals, errs := ExtractAll([]Extract{{From: "json", Path: "$.access_token", Var: "token"}, {From: "json", Path: "$.user.id", Var: "user_id"}}, r)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	changed, err := ApplyExtracts(vals, s, "api.test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "token,user_id" {
		t.Errorf("changed = %v", changed)
	}
	sec, err := vault.Get(SecretKey("", "e1", "token"))
	if err != nil || sec.Reveal() != "tok-canary-77" {
		t.Fatalf("vault = %v %v", sec.Reveal(), err)
	}
	if env.Vars[0].Value != "" || env.Vars[1].Value != "u9" {
		t.Errorf("env vars = %+v", env.Vars)
	}
	if err := st.SaveEnv(env); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(st.Dir, "envs", "staging.json"))
	if bytes.Contains(b, []byte("tok-canary-77")) || !bytes.Contains(b, []byte("u9")) {
		t.Errorf("env file = %s", b)
	}
	// the secret now resolves from the vault
	if v, err := s.Lookup("token"); err != nil || v.Text != "tok-canary-77" {
		t.Errorf("Lookup = %+v %v", v, err)
	}
	// unknown variable: added to Env; without Env: to Globals
	if _, err := ApplyExtracts(map[string]string{"new": "1"}, s, "api.test"); err != nil || env.Vars[2].Name != "new" || env.Vars[2].Value != "1" {
		t.Errorf("new var: %v %+v", err, env.Vars)
	}
	g := &Scope{Globals: []Var{{Name: "g", Value: "old"}}}
	if ch, err := ApplyExtracts(map[string]string{"g": "new", "h": "2"}, g, "api.test"); err != nil || len(ch) != 2 || g.Globals[0].Value != "new" || g.Globals[1].Name != "h" {
		t.Errorf("globals: %v %v %+v", ch, err, g.Globals)
	}
	// unchanged values are not reported
	if ch, _ := ApplyExtracts(map[string]string{"g": "new"}, g, "api.test"); len(ch) != 0 {
		t.Errorf("unchanged reported: %v", ch)
	}
	// a shared collection keeps extracted plain values as my value
	sh := &Scope{Env: &Env{ID: "e1", Vars: []Var{{Name: "user_id", Value: "shared"}}}, DocID: "d1"}
	if _, err := ApplyExtracts(map[string]string{"user_id": "u9"}, sh, "api.test"); err != nil {
		t.Fatal(err)
	}
	if sh.Env.Vars[0].Value != "shared" || sh.Mine == nil || sh.Mine.Values["e1/user_id"] != "u9" {
		t.Errorf("shared extract: env=%+v mine=%+v", sh.Env.Vars, sh.Mine)
	}
	if v, _ := sh.Lookup("user_id"); v.Text != "u9" || v.Source != SourceMine {
		t.Errorf("shared Lookup = %+v", v)
	}
}
