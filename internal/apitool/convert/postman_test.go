package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/apitool"
)

func importFixture(t *testing.T) (*apitool.Collection, Report) {
	t.Helper()
	c, rep, err := ImportPostman(fixture(t, "postman", "collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	return c, rep
}

func TestImportPostmanFolders(t *testing.T) {
	c, rep := importFixture(t)
	if c.Name != "Payments" || c.ID == "" || len(c.Requests) != 7 {
		t.Fatalf("collection %q %q, %d requests", c.Name, c.ID, len(c.Requests))
	}
	want := []string{"Auth/Login", "Create payment", "Upload", "Plain text", "OAuth thing", "GraphQL", "String request"}
	if got := treeNames(c, &c.Root); !slices.Equal(got, want) {
		t.Fatalf("tree = %q", got)
	}
	wantVars := []apitool.Var{
		{Name: "base_url", Value: "https://api.test"},
		{Name: "client_secret", Secret: true},
		{Name: "retries", Value: "3"},
		{Name: "fileId", Value: "f1"},
	}
	if !reflect.DeepEqual(c.Vars, wantVars) {
		t.Fatalf("vars = %+v", c.Vars)
	}
	if !slices.Contains(rep.Dropped, "variable value: client_secret") || !slices.Contains(rep.Ignored, "variable: off (disabled)") {
		t.Fatalf("report = %+v", rep)
	}
	login := byName(t, c, "Login")
	if login.Method != "POST" || login.URL != "{{base_url}}/login" ||
		!reflect.DeepEqual(login.Query, []apitool.KV{{Key: "mode", Value: "fast"}, {Key: "debug", Value: "1", Off: true}}) ||
		!reflect.DeepEqual(login.Headers, []apitool.KV{{Key: "Accept", Value: "application/json"}, {Key: "X-Off", Value: "1", Off: true}}) {
		t.Fatalf("login = %+v", login)
	}
	up := byName(t, c, "Upload")
	if up.URL != "{{base_url}}/files/{{fileId}}" || up.Settings != (apitool.Settings{NoFollow: true, Insecure: true, MaxRedirects: 3}) {
		t.Fatalf("upload = %+v", up)
	}
	plain := byName(t, c, "Plain text")
	if !reflect.DeepEqual(plain.Headers, []apitool.KV{{Key: "X-A", Value: "1"}, {Key: "X-B", Value: "two"}}) {
		t.Fatalf("string headers = %+v", plain.Headers)
	}
	s := byName(t, c, "String request")
	if s.Method != "GET" || s.URL != "https://example.test/ping" || !reflect.DeepEqual(s.Query, []apitool.KV{{Key: "x", Value: "1"}, {Key: "flag"}}) {
		t.Fatalf("string request = %+v", s)
	}
}

func TestImportPostmanBodies(t *testing.T) {
	c, rep := importFixture(t)
	if b := byName(t, c, "Login").Body; !reflect.DeepEqual(b, apitool.Body{Kind: apitool.BodyURLEncoded, Form: []apitool.FormField{
		{KV: apitool.KV{Key: "grant", Value: "password"}}, {KV: apitool.KV{Key: "scope", Value: "all", Off: true}}}}) {
		t.Fatalf("urlencoded = %+v", b)
	}
	if b := byName(t, c, "Create payment").Body; !reflect.DeepEqual(b, apitool.Body{Kind: apitool.BodyJSON, Text: `{"amount": 12000}`}) {
		t.Fatalf("json = %+v", b)
	}
	if b := byName(t, c, "Upload").Body; !reflect.DeepEqual(b, apitool.Body{Kind: apitool.BodyMultipart, Form: []apitool.FormField{
		{KV: apitool.KV{Key: "title", Value: "doc"}}, {KV: apitool.KV{Key: "file"}, FileName: "a.png"}}}) {
		t.Fatalf("formdata = %+v", b)
	}
	// The file path from the collection is never kept: the user picks the
	// file again (an imported src could name any file on this PC).
	if !slices.Contains(rep.Dropped, "file: file (/home/me/a.png)") {
		t.Fatalf("dropped = %q", rep.Dropped)
	}
	if b := byName(t, c, "Plain text").Body; !reflect.DeepEqual(b, apitool.Body{Kind: apitool.BodyRaw, Text: "hello"}) {
		t.Fatalf("raw = %+v", b)
	}
	if b := byName(t, c, "GraphQL").Body; !reflect.DeepEqual(b, apitool.Body{Kind: apitool.BodyJSON, Text: "{\n  \"query\": \"{ me { id } }\",\n  \"variables\": {\n    \"a\": 1\n  }\n}"}) {
		t.Fatalf("graphql = %+v", b)
	}
	if len(rep.Suggestions) != 0 {
		t.Fatalf("suggestions = %+v", rep.Suggestions)
	}
}

func TestImportPostmanSuggestsVariableSecrets(t *testing.T) {
	tok := "ghp_" + strings.Repeat("aB3d", 9)
	doc := `{"info":{"name":"v","schema":"` + postmanSchema + `"},"item":[],
		"variable":[{"key":"gh","value":"` + tok + `"},{"key":"base_url","value":"https://api.test"}]}`
	c, rep, err := ImportPostman([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Vars) != 2 {
		t.Fatalf("vars = %+v", c.Vars)
	}
	if len(rep.Suggestions) != 1 || rep.Suggestions[0].Where != "variable:gh" || rep.Suggestions[0].RequestID != "" ||
		rep.Suggestions[0].Finding.Rule != "github_token" {
		t.Fatalf("suggestions = %+v", rep.Suggestions)
	}
}

func TestImportPostmanAuth(t *testing.T) {
	c, rep := importFixture(t)
	cases := map[string]apitool.Auth{
		"Login":          {Kind: apitool.AuthBasic, User: "{{user}}", Pass: "{{pass}}"},
		"Create payment": {Kind: apitool.AuthBearer, Token: "{{token}}"}, // inherited from the collection
		"Upload":         {Kind: apitool.AuthAPIKey, Name: "X-Api-Key", Value: "{{api_key}}", In: "header"},
		"Plain text":     {},
		"OAuth thing":    {},
		"String request": {Kind: apitool.AuthBearer, Token: "{{token}}"},
	}
	for name, want := range cases {
		if got := byName(t, c, name).Auth; got != want {
			t.Errorf("%s auth = %+v, want %+v", name, got, want)
		}
	}
	if !slices.Contains(rep.Dropped, "auth: oauth2 (OAuth thing)") {
		t.Fatalf("dropped = %q", rep.Dropped)
	}
}

func TestImportPostmanDropsScripts(t *testing.T) {
	_, rep := importFixture(t)
	var scripts []string
	for _, d := range rep.Dropped {
		if strings.HasPrefix(d, "prerequest: ") || strings.HasPrefix(d, "test: ") {
			scripts = append(scripts, d)
		}
	}
	if !slices.Equal(scripts, []string{"prerequest: Login", "test: Create payment"}) {
		t.Fatalf("dropped scripts = %q", scripts)
	}
}

func TestImportPostmanRejects(t *testing.T) {
	v20 := bytes.Replace(fixture(t, "postman", "collection.json"), []byte("v2.1.0"), []byte("v2.0.0"), 1)
	if _, _, err := ImportPostman(v20); !errors.Is(err, ErrNotPostman) {
		t.Fatalf("v2.0 = %v", err)
	}
	newHost := bytes.Replace(fixture(t, "postman", "collection.json"), []byte("schema.getpostman.com"), []byte("schema.postman.com"), 1)
	if _, _, err := ImportPostman(newHost); err != nil {
		t.Fatalf("schema.postman.com = %v", err)
	}
	for _, doc := range []string{`[]`, `{}`, `{"info":{"schema":1}}`, `not json`} {
		if _, _, err := ImportPostman([]byte(doc)); err == nil {
			t.Fatalf("%q imported", doc)
		}
	}
	big := append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), MaxImportBytes)...)
	if _, _, err := ImportPostman(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big = %v", err)
	}
	deep := []byte(`{"info":{},"item":` + strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth) + `}`)
	if _, _, err := ImportPostman(deep); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("deep = %v", err)
	}
}

func TestImportPostmanEnvSecrets(t *testing.T) {
	env, secretValues, rep, err := ImportPostmanEnv(fixture(t, "postman", "env.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []apitool.Var{
		{Name: "base_url", Value: "https://staging.api.test"},
		{Name: "client_key", Secret: true},
		{Name: "api_token", Secret: true},
		{Name: "empty_secret", Secret: true},
		{Name: "page", Value: "2"},
	}
	if env.Name != "staging" || env.ID == "" || !reflect.DeepEqual(env.Vars, want) {
		t.Fatalf("env = %+v", env)
	}
	if !reflect.DeepEqual(secretValues, map[string]string{"client_key": "ck-123", "api_token": "tok-456"}) {
		t.Fatalf("secret values = %v", secretValues)
	}
	if !slices.Contains(rep.Ignored, "variable: old (disabled)") {
		t.Fatalf("ignored = %q", rep.Ignored)
	}
	b, _ := json.Marshal(env)
	if bytes.Contains(b, []byte("ck-123")) || bytes.Contains(b, []byte("tok-456")) {
		t.Fatalf("env carries a secret: %s", b)
	}
	if _, _, _, err := ImportPostmanEnv([]byte(`{"name":"x"}`)); !errors.Is(err, ErrNotPostman) {
		t.Fatalf("no values = %v", err)
	}
	if _, _, _, err := ImportPostmanEnv([]byte(`{"name":"x","values":[],"_postman_variable_scope":"globals"}`)); !errors.Is(err, ErrNotPostman) {
		t.Fatalf("globals scope = %v", err)
	}
}

// normalized lists the requests in tree order (folder path + request, ids
// cleared) for comparing two imports.
func normalized(c *apitool.Collection) []string {
	var out []string
	var walk func(prefix string, f *apitool.Folder)
	walk = func(prefix string, f *apitool.Folder) {
		for _, n := range f.Items {
			if n.Folder != nil {
				walk(prefix+n.Folder.Name+"/", n.Folder)
				continue
			}
			r := *c.Requests[n.Request]
			r.ID = ""
			b, _ := json.Marshal(r)
			out = append(out, prefix+string(b))
		}
	}
	walk("", &c.Root)
	return out
}

func TestExportPostmanRoundTrip(t *testing.T) {
	c, _ := importFixture(t)
	data, rep, err := ExportPostman(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 0 {
		t.Fatalf("dropped = %q", rep.Dropped)
	}
	if !bytes.Contains(data, []byte(postmanSchema)) || !json.Valid(data) {
		t.Fatalf("export = %s", data)
	}
	back, _, err := ImportPostman(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if back.Name != c.Name || !reflect.DeepEqual(back.Vars, c.Vars) {
		t.Fatalf("name/vars = %q %+v", back.Name, back.Vars)
	}
	if a, b := normalized(c), normalized(back); !slices.Equal(a, b) {
		t.Fatalf("round trip differs:\n%s\n---\n%s\n---\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"), data)
	}

	// Checks and extracts cannot be exported; typed-in credentials are hidden.
	c.Requests[c.Root.RequestIDs()[1]].Asserts = []apitool.Assertion{{Target: "status", Op: "eq", Value: "201"}}
	c.Requests[c.Root.RequestIDs()[1]].Extracts = []apitool.Extract{{From: "json", Path: "$.id", Var: "id"}}
	c.Requests[c.Root.RequestIDs()[1]].Settings.TimeoutMS = 5000
	c.Requests[c.Root.RequestIDs()[2]].Headers = []apitool.KV{{Key: "Authorization", Value: "Bearer literal-secret-token"}}
	c.Requests[c.Root.RequestIDs()[2]].Body.Form[0].Value = "{{x}}"
	c.Envs = []apitool.Env{{Name: "staging"}}
	data, rep, err = ExportPostman(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"asserts", "extracts", "timeout: Create payment", "environments"} {
		if !slices.Contains(rep.Dropped, d) {
			t.Fatalf("dropped = %q, want %q", rep.Dropped, d)
		}
	}
	if bytes.Contains(data, []byte("literal-secret-token")) || !bytes.Contains(data, []byte(apitool.Mask)) {
		t.Fatalf("credential not hidden: %s", data)
	}
}

func TestExportPostmanRawContentType(t *testing.T) {
	c := &apitool.Collection{Name: "x", Root: apitool.Folder{Items: []apitool.Node{{Request: "a"}, {Request: "b"}}},
		Requests: map[string]*apitool.Request{
			"a": {ID: "a", Name: "a", Method: "POST", URL: "https://x", Body: apitool.Body{Kind: apitool.BodyRaw, Text: "<a/>", ContentType: "application/xml"}},
			"b": {ID: "b", Name: "b", Method: "POST", URL: "https://x", Body: apitool.Body{Kind: apitool.BodyRaw, Text: "x", ContentType: "application/vnd.custom"}},
		}}
	data, _, err := ExportPostman(c)
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := ImportPostman(data)
	if err != nil {
		t.Fatal(err)
	}
	if b := byName(t, back, "a"); b.Body.ContentType != "application/xml" || len(b.Headers) != 0 {
		t.Fatalf("xml = %+v", b)
	}
	if b := byName(t, back, "b"); !reflect.DeepEqual(b.Headers, []apitool.KV{{Key: "Content-Type", Value: "application/vnd.custom"}}) {
		t.Fatalf("custom = %+v", b)
	}
}

func FuzzImportPostman(f *testing.F) {
	f.Add(fixture(f, "postman", "collection.json"))
	f.Add([]byte(`{"info":{"schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"item":[{"request":{"url":{"host":"a","path":"b"},"header":"x"}}]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, _, err := ImportPostman(data)
		if err != nil {
			return
		}
		if len(c.Root.RequestIDs()) != len(c.Requests) {
			t.Fatalf("tree %d, map %d", len(c.Root.RequestIDs()), len(c.Requests))
		}
		if _, _, err := ExportPostman(c); err != nil {
			t.Fatalf("export after import: %v", err)
		}
	})
}
