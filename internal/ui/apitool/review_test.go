package apitool

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui/uitest"
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

// bigJSON is about n bytes of minified JSON (one line).
func bigJSON(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; b.Len() < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"item-%d","ok":true}`, i, i)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// A failed runner line with a big JSON response is indexed in the run job:
// ⏎ on it does no tree building on the UI goroutine.
func TestRunnerFailRowBigResponseIndexedInJob(t *testing.T) {
	body := bigJSON(8_000_000)
	srv, _ := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "big", Name: "big", Method: "GET", URL: srv.URL + "/big",
		Asserts: []apitool.Assertion{{ID: "s", Target: "status", Op: "eq", Value: "201"}}})
	h.Press("r", "enter")
	waitRun(t, h, tl)
	if r := tl.run.rows[tl.run.sel]; !r.failed {
		t.Fatalf("the selection is not on the failed row: %+v", r)
	}
	start := time.Now()
	h.Press("enter")
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("opening the failed line took %v on the UI goroutine", d)
	}
	if s := tl.rv.current(); s == nil || s.tree == nil || tl.focus != paneResponse {
		t.Fatalf("the failed response is not shown as a tree: %+v", s)
	}
}

// A long run keeps the responses of the first runKeep failed lines only;
// a later failed line's ⏎ says its response was not kept.
func TestRunnerKeepsFewFailedResponses(t *testing.T) {
	srv, _ := hitServer(t, created)
	h := openAPI(t)
	tl := seedRequests(h, nil, runRequests(srv)...)
	h.Press("r", "j", "backspace")
	h.Type("10")
	h.Press("enter")
	waitRun(t, h, tl)
	kept, last := 0, -1
	for i, r := range tl.run.rows {
		if r.kept() {
			kept++
		}
		if r.failed {
			last = i
		}
	}
	if tl.run.fail != 10 || kept != runKeep {
		t.Fatalf("failed = %d, kept responses = %d, want 10 and %d", tl.run.fail, kept, runKeep)
	}
	tl.run.sel = last
	h.Press("enter")
	if s := h.Screen(); !strings.Contains(s, ko.RunRespNotKept(runKeep)) {
		t.Fatalf("no 'not kept' notice:\n%s", s)
	}
	if tl.req != nil && tl.req.ID == "bad" && tl.focus == paneResponse {
		t.Fatalf("a not-kept line opened a response")
	}
}

// A secret pasted into the URL being edited is bound to the host of that
// new URL, not to the request's old one.
func TestURLSecretBindsPastedHost(t *testing.T) {
	srv, _ := hitServer(t, created)
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithSecrets(store))
	seedRequests(h, &apitool.Env{Name: "dev"}, paymentRequest(srv))
	h.Press("u", "ctrl+u")
	h.Paste("https://api.newhost.example/v1?token=" + testJWT)
	if s := h.Screen(); !hasWhole(s, ko.SecretDetected()) {
		t.Fatalf("no secret_detected dialog:\n%s", s)
	}
	h.Press("ctrl+u")
	h.Type("tok")
	h.Press("enter")
	env, err := apitool.OpenLocal(h.Context().Paths).LoadEnv("dev")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(env.Vars, func(v apitool.Var) bool { return v.Name == "tok" })
	if i < 0 || !env.Vars[i].Secret || !slices.Equal(env.Vars[i].AllowedHosts, []string{"api.newhost.example"}) {
		t.Fatalf("env vars = %+v", env.Vars)
	}
}

// A server-named dotfile is not offered as is.
func TestSaveNameNoDotfile(t *testing.T) {
	for cd, want := range map[string]string{
		`attachment; filename=".bashrc"`:        "bashrc",
		`attachment; filename="../.ssh/config"`: "config",
		`attachment; filename="..."`:            "response.bin",
	} {
		r := jsonResponse(`{}`)
		r.Header.Set("Content-Disposition", cd)
		if got := saveName(r, "https://example.com/x"); got != want {
			t.Errorf("saveName(%q) = %q, want %q", cd, got, want)
		}
	}
}

// Saving a body over an existing file asks first; without ~/Downloads the
// default path is still ~/Downloads (made on save), never the bare home.
func TestSaveBodyAsksBeforeOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "x", Method: "GET", URL: "https://example.com/x"})
	r := jsonResponse(`{"new":1}`)
	r.Header.Set("Content-Disposition", `attachment; filename=".bashrc"`)
	showOn(h, tl, r)

	h.Press("w", "enter") // the default path
	def := filepath.Join(home, "Downloads", "bashrc")
	waitWhole(t, h, ko.BodySaved(def))
	assertFile(t, def, `{"new":1}`)
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		t.Fatalf("a dotfile was written in home")
	}
	h.Press("esc")

	old := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(old, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Press("w", "ctrl+u")
	h.Type(old)
	h.Press("enter")
	waitWhole(t, h, ko.FileExistsBody(old))
	h.Press("down", "enter") // cancel
	h.Advance(time.Second)
	if b, _ := os.ReadFile(old); string(b) != "OLD" {
		t.Fatalf("canceled overwrite changed the file: %q", b)
	}
	h.Press("w", "ctrl+u")
	h.Type(old)
	h.Press("enter")
	waitWhole(t, h, ko.FileExistsBody(old))
	h.Press("enter") // overwrite
	waitWhole(t, h, ko.BodySaved(old))
	assertFile(t, old, `{"new":1}`)
}

// An extracted value of a secret variable goes to the vault off the UI
// goroutine; a missing OS store asks for consent instead of losing it.
func TestSecretExtractAsksConsent(t *testing.T) {
	srv, _ := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-from-login-0042"}`))
	})
	store := secrettest.NewStore(t, false)
	h := openAPI(t, uitest.WithSecrets(store))
	env := &apitool.Env{Name: "dev", Vars: []apitool.Var{{Name: "token", Secret: true, AllowedHosts: []string{"127.0.0.1"}}}}
	tl := seedRequests(h, env, &apitool.Request{ID: "login", Name: "login", Method: "POST", URL: srv.URL + "/login",
		Extracts: []apitool.Extract{{ID: "x", Var: "token", From: "json", Path: "$.access_token"}}})
	h.Press("enter")
	for i := 0; i < 400 && tl.running(jobSend); i++ {
		h.Advance(50 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
	if len(tl.pendingPuts) != 1 || overlayText(h) == "" {
		t.Fatalf("the extracted secret is not waiting for consent (%d pending):\n%s", len(tl.pendingPuts), h.Screen())
	}
	h.Press("enter") // the consent default (this run only)
	key, _, _ := tl.scope(h.Context()).SecretSlot("token")
	v, err := tl.vault(h.Context()).Get(key)
	if err != nil || v.Reveal() != "tok-from-login-0042" {
		t.Fatalf("the extracted token was lost: %v", err)
	}
}
