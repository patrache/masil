package convert

import (
	"bytes"
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
