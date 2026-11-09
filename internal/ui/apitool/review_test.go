package apitool

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/apitool"
)

// Regression tests for the Task 13–17 review findings.

// formServer records each multipart request's text fields and file parts.
type formServer struct {
	mu    sync.Mutex
	texts map[string]string
	files map[string]string
}

func (s *formServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts, s.files = map[string]string{}, map[string]string{}
	mr, err := r.MultipartReader()
	if err == nil {
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FileName() != "" {
				s.files[p.FormName()] = string(b)
			} else {
				s.texts[p.FormName()] = string(b)
			}
		}
	}
	created(w, r)
}

// An imported (or shared) form text value that starts with "@" is text: it
// never turns into a file upload of a path on this computer.
func TestFormTextAtValueSendsText(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(secret, []byte("PRIVATE-KEY-CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := &formServer{}
	srv, _ := hitServer(t, fs.handle)
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "form", Name: "form", Method: "POST", URL: srv.URL + "/upload",
		Body: apitool.Body{Kind: apitool.BodyMultipart, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "note", Value: "@" + secret}},           // as the Postman importer makes it (no id)
			{KV: apitool.KV{ID: "f2", Key: "memo", Value: "@" + secret}}, // a saved text row
		}}})
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.files) != 0 {
		t.Fatalf("a text value was uploaded as a file: %v", fs.files)
	}
	if fs.texts["note"] != "@"+secret || fs.texts["memo"] != "@"+secret {
		t.Fatalf("text fields = %v", fs.texts)
	}
	for _, f := range tl.req.Body.Form {
		if f.File != "" {
			t.Fatalf("the edited request holds a file field: %+v", f)
		}
	}
}

// A real file field (a curl -F x=@path, or one the user added) stays a file.
func TestFormFileFieldStaysFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("FILE-BODY"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := &formServer{}
	srv, _ := hitServer(t, fs.handle)
	h := openAPI(t)
	seedRequests(h, nil, &apitool.Request{ID: "form", Name: "form", Method: "POST", URL: srv.URL + "/upload",
		Body: apitool.Body{Kind: apitool.BodyMultipart, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "doc"}, File: file},
		}}})
	h.Press("3", "j") // the body tab (form rows)
	h.Press("a")      // a new row typed by the user: "@path" picks a file
	h.Type("up")
	h.Press("tab")
	h.Type("@" + file)
	h.Press("enter", "k", "k", "k")
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.files["doc"] != "FILE-BODY" || fs.files["up"] != "FILE-BODY" || strings.Contains(fs.texts["doc"]+fs.texts["up"], "@") {
		t.Fatalf("files = %v, texts = %v", fs.files, fs.texts)
	}
}
