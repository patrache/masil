package apitool

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/config"
)

func testStore(t *testing.T) *LocalStore {
	t.Helper()
	dir := t.TempDir()
	return OpenLocal(config.Paths{Dir: dir})
}

func sampleCollection() *Collection {
	return &Collection{
		ID:   "col1",
		Name: "github",
		Vars: []Var{{ID: "v1", Name: "base_url", Value: "https://api.github.com"}},
		Root: Folder{ID: "root", Items: []Node{
			{Request: "r1"},
			{Folder: &Folder{ID: "f1", Name: "repos", Items: []Node{{Request: "r2"}}}},
		}},
		Requests: map[string]*Request{
			"r1": {ID: "r1", Name: "me", Method: "GET", URL: "{{base_url}}/user",
				Headers: []KV{{ID: "h1", Key: "Accept", Value: "application/json"}}},
			"r2": {ID: "r2", Name: "create", Method: "POST", URL: "{{base_url}}/repos",
				Body: Body{Kind: BodyJSON, Text: `{"name":"x","html":"<b>&"}`}},
		},
	}
}

// readAll concatenates every file under dir.
func readAll(t *testing.T, dir string) []byte {
	t.Helper()
	var all bytes.Buffer
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			all.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.Bytes()
}

func TestSaveCollectionLayout(t *testing.T) {
	st := testStore(t)
	c := sampleCollection()
	if err := st.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	colDir := filepath.Join(st.Dir, "collections", "github")
	files := []string{
		filepath.Join(colDir, "collection.json"),
		filepath.Join(colDir, "requests", "r1.json"),
		filepath.Join(colDir, "requests", "r2.json"),
	}
	first := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
		first[f] = b
		if runtime.GOOS != "windows" {
			fi, _ := os.Stat(f)
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("%s perm = %v, want 0600", f, fi.Mode().Perm())
			}
		}
		if !bytes.HasSuffix(b, []byte("}\n")) {
			t.Errorf("%s does not end with a newline", f)
		}
		if !bytes.Contains(b, []byte("\n  \"")) {
			t.Errorf("%s is not 2-space indented:\n%s", f, b)
		}
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(colDir)
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("dir perm = %v, want 0700", fi.Mode().Perm())
		}
	}
	// keys sorted: "headers" before "id" before "method" before "name" before "url"
	r1 := string(first[files[1]])
	order := []string{`"headers"`, `"id"`, `"method"`, `"name"`, `"url"`}
	last := -1
	for _, k := range order {
		i := strings.Index(r1, k)
		if i < 0 || i < last {
			t.Fatalf("keys not sorted in %s", r1)
		}
		last = i
	}
	if !strings.Contains(string(first[files[2]]), `<b>&`) {
		t.Errorf("HTML characters escaped: %s", first[files[2]])
	}
	// same value saved twice => same bytes
	if err := st.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if !bytes.Equal(b, first[f]) {
			t.Errorf("%s changed on second save", f)
		}
	}
	// round trip
	got, err := st.LoadCollection("github")
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests["r2"].Body.Text != c.Requests["r2"].Body.Text || got.Root.Items[1].Folder.Items[0].Request != "r2" {
		t.Errorf("round trip mismatch: %+v", got)
	}
	// a request dropped from the tree is cleaned up on save
	delete(c.Requests, "r2")
	c.Root.Items[1].Folder.Items = nil
	if err := st.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(files[2]); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("removed request file still there: %v", err)
	}
	if names, err := st.ListCollections(); err != nil || len(names) != 1 || names[0] != "github" {
		t.Errorf("ListCollections = %v, %v", names, err)
	}
}

func TestSecretVarValueNeverWritten(t *testing.T) {
	st := testStore(t)
	c := sampleCollection()
	c.Vars = append(c.Vars, Var{ID: "v2", Name: "token", Secret: true, Value: "canary-1", AllowedHosts: []string{"api.github.com"}})
	if err := st.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	env := &Env{ID: "e1", Name: "staging", Vars: []Var{{ID: "v3", Name: "key", Secret: true, Value: "canary-1"}, {ID: "v4", Name: "region", Value: "kr"}}}
	if err := st.SaveEnv(env); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveGlobals([]Var{{ID: "g1", Name: "pw", Secret: true, Value: "canary-1"}}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(readAll(t, st.Dir), []byte("canary-1")) {
		t.Fatal("secret value written to disk")
	}
	got, err := st.LoadCollection("github")
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Vars[1]; v.Value != "" || !v.Secret || len(v.AllowedHosts) != 1 {
		t.Errorf("collection var = %+v", v)
	}
	e, err := st.LoadEnv("staging")
	if err != nil {
		t.Fatal(err)
	}
	if e.Vars[0].Value != "" || !e.Vars[0].Secret || e.Vars[1].Value != "kr" {
		t.Errorf("env vars = %+v", e.Vars)
	}
	g, err := st.LoadGlobals()
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || g[0].Value != "" || !g[0].Secret {
		t.Errorf("globals = %+v", g)
	}
	if names, err := st.ListEnvs(); err != nil || len(names) != 1 || names[0] != "staging" {
		t.Errorf("ListEnvs = %v, %v", names, err)
	}
}

func TestFormFilePathKeptLocally(t *testing.T) {
	st := testStore(t)
	c := sampleCollection()
	c.Requests["r2"].Body = Body{Kind: BodyMultipart, Form: []FormField{
		{KV: KV{ID: "ff1", Key: "avatar"}, File: "/home/me/a.png", FileName: "a.png"},
		{KV: KV{ID: "ff2", Key: "name", Value: "x"}},
	}}
	if err := st.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCollection("github")
	if err != nil {
		t.Fatal(err)
	}
	f := got.Requests["r2"].Body.Form
	if len(f) != 2 || f[0].File != "/home/me/a.png" || f[0].FileName != "a.png" || f[0].Key != "avatar" || f[1].Value != "x" {
		t.Errorf("form = %+v", f)
	}
}

func TestCollectionNameRejectsPaths(t *testing.T) {
	st := testStore(t)
	for _, name := range []string{"../x", "a/b", `a\b`, "", ".", ".."} {
		c := sampleCollection()
		c.Name = name
		if err := st.SaveCollection(c); !errors.Is(err, ErrBadName) {
			t.Errorf("SaveCollection(%q) = %v, want ErrBadName", name, err)
		}
		if _, err := st.LoadCollection(name); !errors.Is(err, ErrBadName) {
			t.Errorf("LoadCollection(%q) = %v, want ErrBadName", name, err)
		}
		if err := st.SaveEnv(&Env{Name: name}); !errors.Is(err, ErrBadName) {
			t.Errorf("SaveEnv(%q) = %v, want ErrBadName", name, err)
		}
	}
	c := sampleCollection()
	c.Requests["../../evil"] = &Request{ID: "../../evil", Method: "GET"}
	c.Root.Items = append(c.Root.Items, Node{Request: "../../evil"})
	if err := st.SaveCollection(c); !errors.Is(err, ErrBadName) {
		t.Errorf("bad request id = %v, want ErrBadName", err)
	}
	parent := filepath.Dir(st.Dir)
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if e.Name() != filepath.Base(st.Dir) {
			t.Errorf("file outside the api dir: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "x")); err == nil {
		t.Error("../x escaped into the api dir")
	}
}

func TestChangedDetectsExternalWrite(t *testing.T) {
	st := testStore(t)
	if err := st.SaveCollection(sampleCollection()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadCollection("github"); err != nil {
		t.Fatal(err)
	}
	if ch, err := st.Changed("github"); err != nil || ch {
		t.Fatalf("Changed after load = %v, %v", ch, err)
	}
	p := filepath.Join(st.Dir, "collections", "github", "requests", "r1.json")
	if err := os.WriteFile(p, []byte(`{"id":"r1","method":"DELETE","url":"x"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ch, err := st.Changed("github"); err != nil || !ch {
		t.Fatalf("Changed after external write = %v, %v", ch, err)
	}
	got, err := st.LoadCollection("github")
	if err != nil {
		t.Fatal(err)
	}
	if got.Requests["r1"].Method != "DELETE" {
		t.Errorf("reload did not read the new file: %+v", got.Requests["r1"])
	}
	if ch, err := st.Changed("github"); err != nil || ch {
		t.Fatalf("Changed after reload = %v, %v", ch, err)
	}
	// our own save is not an external change
	got.Requests["r1"].Method = "PUT"
	if err := st.SaveCollection(got); err != nil {
		t.Fatal(err)
	}
	if ch, err := st.Changed("github"); err != nil || ch {
		t.Fatalf("Changed after own save = %v, %v", ch, err)
	}
}

func TestDeleteRequest(t *testing.T) {
	st := testStore(t)
	if err := st.SaveCollection(sampleCollection()); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteRequest("github", "r2"); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCollection("github")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Requests["r2"]; ok || len(got.Root.Items[1].Folder.Items) != 0 {
		t.Errorf("r2 still present: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "collections", "github", "requests", "r2.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("r2.json still there: %v", err)
	}
}
