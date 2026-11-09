package convert

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/patrache/masil/internal/apitool"
)

func fixture(t testing.TB, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// treeNames renders the tree as "name" / "folder/[...]" for comparison.
func treeNames(c *apitool.Collection, f *apitool.Folder) []string {
	var out []string
	for _, n := range f.Items {
		if n.Folder != nil {
			out = append(out, n.Folder.Name+"/"+strings.Join(treeNames(c, n.Folder), ","))
			continue
		}
		out = append(out, c.Requests[n.Request].Name)
	}
	return out
}

func byName(t *testing.T, c *apitool.Collection, name string) *apitool.Request {
	t.Helper()
	for _, r := range c.Requests {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no request %q", name)
	return nil
}

func hasPrefix(list []string, p string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.HasPrefix(s, p) })
}

func TestImportOpenAPI30(t *testing.T) {
	c, rep, err := ImportOpenAPI(fixture(t, "openapi", "petstore-3.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Petstore" || c.ID == "" || len(c.Requests) != 8 {
		t.Fatalf("collection %q %q, %d requests", c.Name, c.ID, len(c.Requests))
	}
	want := []string{
		"pets/List pets,createPet,Show a pet,DELETE /pets/{petId}",
		"store/getInventory,Upload",
		"GET /health",
		"Login",
	}
	if got := treeNames(c, &c.Root); !slices.Equal(got, want) {
		t.Fatalf("tree = %q", got)
	}
	if ids := c.Root.RequestIDs(); len(ids) != 8 {
		t.Fatalf("tree ids = %v", ids)
	}

	list := byName(t, c, "List pets")
	if list.Method != "GET" || list.URL != "{{base_url}}/pets" {
		t.Fatalf("list = %+v", list)
	}
	if !reflect.DeepEqual(list.Query, []apitool.KV{{Key: "limit", Value: "20", Off: true}, {Key: "page", Value: "1"}}) {
		t.Fatalf("query = %+v", list.Query)
	}
	if !reflect.DeepEqual(list.Headers, []apitool.KV{{Key: "X-Request-Id", Off: true}}) {
		t.Fatalf("headers = %+v", list.Headers)
	}
	if list.Auth != (apitool.Auth{Kind: apitool.AuthBearer, Token: "{{token}}"}) {
		t.Fatalf("auth = %+v", list.Auth)
	}
	if !hasPrefix(rep.Ignored, "cookie parameter: session") {
		t.Fatalf("ignored = %q", rep.Ignored)
	}

	create := byName(t, c, "createPet")
	if create.Method != "POST" || create.Body.Kind != apitool.BodyJSON || create.Body.Text != "{\n  \"name\": \"Tom\",\n  \"tag\": \"cat\",\n  \"age\": 3\n}" {
		t.Fatalf("create = %+v", create.Body)
	}
	show := byName(t, c, "Show a pet")
	if show.URL != "{{base_url}}/pets/{{petId}}" {
		t.Fatalf("show URL = %q", show.URL)
	}
	if !reflect.DeepEqual(c.Vars, []apitool.Var{{Name: "petId"}}) { // path variables start empty (plan Task 10)
		t.Fatalf("vars = %+v", c.Vars)
	}
	del := byName(t, c, "DELETE /pets/{petId}")
	if del.Method != "DELETE" || del.Auth.Kind != "" {
		t.Fatalf("delete = %+v", del)
	}
	login := byName(t, c, "Login")
	if login.Body.Kind != apitool.BodyURLEncoded || !reflect.DeepEqual(login.Body.Form, []apitool.FormField{
		{KV: apitool.KV{Key: "user", Value: "me"}}, {KV: apitool.KV{Key: "pass"}}}) {
		t.Fatalf("login body = %+v", login.Body)
	}
	up := byName(t, c, "Upload")
	if up.Method != "PUT" || up.Body.Kind != apitool.BodyMultipart || !reflect.DeepEqual(up.Body.Form, []apitool.FormField{
		{KV: apitool.KV{Key: "title"}}, {KV: apitool.KV{Key: "file"}, FileName: "file"}}) {
		t.Fatalf("upload body = %+v", up.Body)
	}
	if h := byName(t, c, "GET /health"); h.Body.Kind != "" || h.Auth.Kind != apitool.AuthBearer {
		t.Fatalf("health = %+v", h)
	}
}

func TestSecuritySchemesToAuth(t *testing.T) {
	c, rep, err := ImportOpenAPI(fixture(t, "openapi", "petstore-3.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	if a := byName(t, c, "getInventory").Auth; a != (apitool.Auth{Kind: apitool.AuthAPIKey, Name: "X-Api-Key", Value: "{{token}}", In: "header"}) {
		t.Fatalf("apikey = %+v", a)
	}
	if a := byName(t, c, "Login").Auth; a != (apitool.Auth{Kind: apitool.AuthBasic, User: "{{username}}", Pass: "{{password}}"}) {
		t.Fatalf("basic = %+v", a)
	}
	env := c.Envs[0]
	want := []apitool.Var{
		{Name: "base_url", Value: "https://api.petstore.test/v1"},
		{Name: "token", Secret: true},
		{Name: "username"},
		{Name: "password", Secret: true},
	}
	if !reflect.DeepEqual(env.Vars, want) {
		t.Fatalf("env vars = %+v", env.Vars)
	}
	if len(rep.Suggestions) != 0 {
		t.Fatalf("suggestions = %+v", rep.Suggestions)
	}

	doc := `{"openapi":"3.0.0","info":{"title":"S"},"components":{"securitySchemes":{
		"q":{"type":"apiKey","in":"query","name":"api_key"},
		"c":{"type":"apiKey","in":"cookie","name":"sid"},
		"o":{"type":"oauth2","flows":{}},
		"d":{"type":"http","scheme":"digest"}}},
	  "paths":{"/q":{"get":{"security":[{"q":[]}]}},"/c":{"get":{"security":[{"c":[]}]}},
	           "/o":{"get":{"security":[{},{"o":["read"]}]}},"/d":{"get":{"security":[{"d":[]}]}}}}`
	c, rep, err = ImportOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if a := byName(t, c, "GET /q").Auth; a != (apitool.Auth{Kind: apitool.AuthAPIKey, Name: "api_key", Value: "{{token}}", In: "query"}) {
		t.Fatalf("query key = %+v", a)
	}
	if a := byName(t, c, "GET /o").Auth; a.Kind != "" {
		t.Fatalf("optional auth ({}) first = %+v", a)
	}
	if a := byName(t, c, "GET /c").Auth; a.Kind != "" {
		t.Fatalf("cookie key = %+v", a)
	}
	if !hasPrefix(rep.Ignored, "security: c") || !hasPrefix(rep.Ignored, "security: d") {
		t.Fatalf("ignored = %q", rep.Ignored)
	}
	doc = strings.Replace(doc, `[{},{"o":["read"]}]`, `[{"o":["read"]}]`, 1)
	c, _, _ = ImportOpenAPI([]byte(doc))
	if a := byName(t, c, "GET /o").Auth; a != (apitool.Auth{Kind: apitool.AuthBearer, Token: "{{token}}"}) {
		t.Fatalf("oauth2 = %+v", a)
	}
}

func TestImportOpenAPI31(t *testing.T) {
	c, rep, err := ImportOpenAPI(fixture(t, "openapi", "webhooks-3.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Requests) != 2 {
		t.Fatalf("%d requests", len(c.Requests))
	}
	list := byName(t, c, "listItems")
	if !reflect.DeepEqual(list.Query, []apitool.KV{{Key: "q", Value: "shoes", Off: true}}) {
		t.Fatalf("query = %+v", list.Query)
	}
	create := byName(t, c, "createItem")
	if !reflect.DeepEqual(create.Body, apitool.Body{Kind: apitool.BodyJSON, Text: "{\n  \"id\": null\n}", ContentType: "application/vnd.item+json"}) {
		t.Fatalf("body = %+v", create.Body)
	}
	if !slices.Contains(rep.Ignored, "webhooks: newItem") || !slices.Contains(rep.Ignored, "webhooks: deleted") {
		t.Fatalf("ignored = %q", rep.Ignored)
	}
	if !reflect.DeepEqual(c.Envs[0].Vars, []apitool.Var{{Name: "base_url"}}) {
		t.Fatalf("env = %+v", c.Envs)
	}
}

func TestServersToBaseURL(t *testing.T) {
	cases := map[string]string{
		`[{"url":"https://{env}.petstore.test/v1/","variables":{"env":{"default":"api"}}}]`: "https://api.petstore.test/v1",
		`[{"url":"/v1"},{"url":"https://second.test"}]`:                                     "/v1",
		`[{"url":"https://{a}.test:{port}","variables":{"a":{"default":"x"}}}]`:             "https://x.test:{port}",
		`[]`: "",
	}
	for servers, want := range cases {
		doc := `{"openapi":"3.0.0","info":{"title":"S"},"servers":` + servers + `,"paths":{"/a":{"get":{}}}}`
		c, _, err := ImportOpenAPI([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Envs) != 1 || c.Envs[0].Name != "default" || !reflect.DeepEqual(c.Envs[0].Vars[0], apitool.Var{Name: "base_url", Value: want}) {
			t.Fatalf("%s → %+v", servers, c.Envs)
		}
		if r := byName(t, c, "GET /a"); r.URL != "{{base_url}}/a" {
			t.Fatalf("URL = %q", r.URL)
		}
	}
}

func TestRemoteRefNoNetwork(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()
	addr := ln.Addr().String()
	doc := bytes.ReplaceAll(fixture(t, "openapi", "remote-ref.json"), []byte("LISTENER"), []byte(addr))
	c, rep, err := ImportOpenAPI(doc)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if accepted.Load() != 0 {
		t.Fatalf("import connected %d times", accepted.Load())
	}
	for _, ref := range []string{"$ref http://" + addr + "/params.json#/Limit", "$ref http://" + addr + "/bodies.json", "$ref other.json#/paths/~1y"} {
		if !slices.Contains(rep.Ignored, ref) {
			t.Fatalf("ignored = %q, want %q", rep.Ignored, ref)
		}
	}
	if len(c.Requests) != 1 {
		t.Fatalf("%d requests", len(c.Requests))
	}
	if r := byName(t, c, "GET /x"); !reflect.DeepEqual(r.Query, []apitool.KV{{Key: "ok", Off: true}}) || r.Body.Kind != "" {
		t.Fatalf("request = %+v", r)
	}
}

func TestRefCycleTerminates(t *testing.T) {
	c, rep, err := ImportOpenAPI(fixture(t, "openapi", "cycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	post := byName(t, c, "POST /nodes")
	if post.Body.Kind != apitool.BodyJSON || post.Body.Text != "{}" || len(post.Query) != 0 {
		t.Fatalf("post = %+v", post)
	}
	put := byName(t, c, "PUT /nodes")
	if put.Body.Kind != apitool.BodyURLEncoded || len(put.Body.Form) != 0 {
		t.Fatalf("put = %+v", put.Body)
	}
	if !hasPrefix(rep.Ignored, "$ref #/components/parameters/P1") || !hasPrefix(rep.Ignored, "$ref #/components/schemas/Loop") {
		t.Fatalf("ignored = %q", rep.Ignored)
	}
}

func TestYAMLRejected(t *testing.T) {
	for _, doc := range []string{"openapi: 3.0.0\ninfo:\n  title: x\n", "---\nopenapi: 3.1.0", "\ufeff  # comment\nopenapi: 3.0.0"} {
		if _, _, err := ImportOpenAPI([]byte(doc)); !errors.Is(err, ErrYAMLUnsupported) {
			t.Fatalf("%q: %v", doc, err)
		}
	}
	for _, doc := range []string{`{"swagger":"2.0","paths":{}}`, `{"openapi":"4.0.0"}`, `[1]`, `{"openapi":"3.0.0"} trailing`, `{"openapi":`} {
		_, _, err := ImportOpenAPI([]byte(doc))
		if err == nil || errors.Is(err, ErrYAMLUnsupported) {
			t.Fatalf("%q: %v", doc, err)
		}
	}
	if _, _, err := ImportOpenAPI([]byte("\ufeff" + `{"openapi":"3.0.0","info":{},"paths":{}}`)); err != nil {
		t.Fatalf("BOM: %v", err)
	}
}

func TestImportSizeDepthLimits(t *testing.T) {
	big := append([]byte(`{"openapi":"3.0.0","x":"`), bytes.Repeat([]byte("a"), MaxImportBytes)...)
	big = append(big, `"}`...)
	if _, _, err := ImportOpenAPI(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big = %v", err)
	}
	nest := func(n int) []byte {
		// the outer object is level 1
		return []byte(`{"openapi":"3.0.0","info":{},"paths":{},"x":` + strings.Repeat("[", n-1) + strings.Repeat("]", n-1) + `}`)
	}
	if _, _, err := ImportOpenAPI(nest(MaxDepth + 1)); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("depth 65 = %v", err)
	}
	if _, _, err := ImportOpenAPI(nest(MaxDepth)); err != nil {
		t.Fatalf("depth 64 = %v", err)
	}
}

func TestOpenAPISuggestsSecrets(t *testing.T) {
	doc := `{"openapi":"3.0.0","info":{"title":"S"},"paths":{"/a":{"get":{"parameters":[
		{"name":"Authorization","in":"header","example":"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJlLXZhbHVl"},
		{"name":"X-Token","in":"header","example":"Zx81kQ0pLm2Vn7Rt5Yb4Wc9Qe"}]}}}}`
	c, rep, err := ImportOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	r := byName(t, c, "GET /a")
	if len(r.Headers) != 1 || r.Headers[0].Key != "X-Token" {
		t.Fatalf("headers = %+v", r.Headers)
	}
	if len(rep.Suggestions) != 1 || rep.Suggestions[0].RequestID != r.ID || rep.Suggestions[0].Where != "header:X-Token" {
		t.Fatalf("suggestions = %+v", rep.Suggestions)
	}
}

func TestOpenAPISuggestsVariableSecrets(t *testing.T) {
	tok := "ghp_" + strings.Repeat("aB3d", 9)
	doc := `{"openapi":"3.0.0","info":{"title":"S"},"servers":[{"url":"https://api.test/?key=` + tok + `"}],"paths":{}}`
	_, rep, err := ImportOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Suggestions) != 1 || rep.Suggestions[0].Where != "variable:base_url" || rep.Suggestions[0].RequestID != "" {
		t.Fatalf("suggestions = %+v", rep.Suggestions)
	}
}

func FuzzImportOpenAPI(f *testing.F) {
	for _, name := range []string{"petstore-3.0.json", "webhooks-3.1.json", "remote-ref.json", "cycle.json"} {
		f.Add(fixture(f, "openapi", name))
	}
	f.Add([]byte(`{"openapi":"3.1.0","paths":{"/{a}{b}":{"get":{"parameters":[{"$ref":"#/"}]}}}}`))
	f.Add([]byte(`{"openapi":"3.0.0","paths":{"/":{"$ref":"#/paths/~1"}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, _, err := ImportOpenAPI(data)
		if err != nil {
			return
		}
		if len(c.Root.RequestIDs()) != len(c.Requests) {
			t.Fatalf("tree has %d ids, map %d", len(c.Root.RequestIDs()), len(c.Requests))
		}
	})
}
