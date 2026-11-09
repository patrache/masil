package convert

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/patrache/masil/internal/apitool"
)

// (k) an imported formdata file src never becomes a live local path.
func TestImportPostmanFileSrcNotKept(t *testing.T) {
	doc := `{"info":{"name":"f","schema":"` + postmanSchema + `"},"item":[{"name":"up","request":{"method":"POST","url":"https://x.test",
		"body":{"mode":"formdata","formdata":[{"key":"k","type":"file","src":"/Users/me/.ssh/id_rsa"},{"key":"w","type":"file","src":["C:\\Users\\me\\a.txt"]}]}}}]}`
	c, rep, err := ImportPostman([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range c.Requests {
		for _, f := range r.Body.Form {
			if f.File != "" {
				t.Fatalf("file path kept: %+v", f)
			}
		}
		if r.Body.Form[0].FileName != "id_rsa" || r.Body.Form[1].FileName != "a.txt" {
			t.Fatalf("names = %+v", r.Body.Form)
		}
	}
	if !slices.Contains(rep.Dropped, "file: k (/Users/me/.ssh/id_rsa)") {
		t.Fatalf("dropped = %q", rep.Dropped)
	}
}

// (l) the export never writes a local file path, only the file name.
func TestExportPostmanNoLocalPath(t *testing.T) {
	c := &apitool.Collection{Version: apitool.CollectionVersion, Name: "x", Requests: map[string]*apitool.Request{
		"r": {ID: "r", Name: "up", Method: "POST", URL: "https://x.test", Body: apitool.Body{Kind: apitool.BodyMultipart, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "a"}, File: "/Users/sam/Documents/private/report.pdf"},
			{KV: apitool.KV{Key: "b"}, File: `C:\Users\sam\b.png`, FileName: "b.png"},
		}}}},
		Root: apitool.Folder{Items: []apitool.Node{{Request: "r"}}}}
	data, _, err := ExportPostman(c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("/Users/sam")) || bytes.Contains(data, []byte(`Users\\\\sam`)) || bytes.Contains(data, []byte("private")) {
		t.Fatalf("local path exported: %s", data)
	}
	if !bytes.Contains(data, []byte(`"report.pdf"`)) || !bytes.Contains(data, []byte(`"b.png"`)) {
		t.Fatalf("file names missing: %s", data)
	}
}

// (m) a collection variable with a credential-like name is a secret like an
// environment one: no value is imported, the drop is reported.
func TestImportPostmanSensitiveCollectionVarIsSecret(t *testing.T) {
	doc := `{"info":{"name":"v","schema":"` + postmanSchema + `"},"item":[],
		"variable":[{"key":"api_token","value":"tok-456"},{"key":"X-Api-Key","value":"k-1"},{"key":"base_url","value":"https://api.test"}]}`
	c, rep, err := ImportPostman([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := []apitool.Var{{Name: "api_token", Secret: true}, {Name: "X-Api-Key", Secret: true}, {Name: "base_url", Value: "https://api.test"}}
	if !reflect.DeepEqual(c.Vars, want) {
		t.Fatalf("vars = %+v", c.Vars)
	}
	for _, d := range []string{"variable value: api_token", "variable value: X-Api-Key"} {
		if !slices.Contains(rep.Dropped, d) {
			t.Errorf("dropped = %q, want %q", rep.Dropped, d)
		}
	}
}
