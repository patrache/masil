package toolstate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/toolstate"
)

func paths(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	return config.Paths{Dir: dir, ToolsState: filepath.Join(dir, "tools-state.json")}
}

type gitState struct {
	Recent []string `json:"recent"`
}

func TestToolStateRoundTrip(t *testing.T) {
	p := paths(t)
	f := toolstate.Load(p) // missing file => empty
	var g gitState
	if f.Section("git", &g) {
		t.Fatal("empty file has a git section")
	}
	if err := f.SetSection("git", gitState{Recent: []string{"/src/masil"}}); err != nil {
		t.Fatal(err)
	}
	z := true
	if err := f.SetCommon("git", toolstate.Common{LastOpen: termhost.ModeWindow, Zoomed: &z}); err != nil {
		t.Fatal(err)
	}
	// SetSection replaces the tool's own fields but keeps the common ones.
	if err := f.SetSection("git", gitState{Recent: []string{"/src/masil", "/src/other"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSection("leet", map[string]string{"lang": "go"}); err != nil {
		t.Fatal(err)
	}

	again := toolstate.Load(p)
	if !again.Section("git", &g) || len(g.Recent) != 2 || g.Recent[1] != "/src/other" {
		t.Errorf("git section = %+v", g)
	}
	c := again.Common("git")
	if c.LastOpen != termhost.ModeWindow || c.Zoomed == nil || !*c.Zoomed {
		t.Errorf("git common = %+v", c)
	}
	if c := again.Common("leet"); c.LastOpen != "" || c.Zoomed != nil {
		t.Errorf("leet common = %+v, want empty", c)
	}
	var leet map[string]string
	if !again.Section("leet", &leet) || leet["lang"] != "go" {
		t.Errorf("leet = %v", leet)
	}

	b, err := os.ReadFile(p.ToolsState)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b)
	}
	if top["version"] != float64(1) {
		t.Errorf("version = %v, want 1", top["version"])
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p.ToolsState)
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Errorf("perm = %o, want 600", perm)
		}
	}
	// atomic: no temp file is left beside it
	ents, _ := os.ReadDir(p.Dir)
	if len(ents) != 1 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("dir holds %v, want only tools-state.json", names)
	}
}

func TestToolStateBrokenFileIsEmpty(t *testing.T) {
	for _, body := range []string{"{", "[]", `"x"`, `{"version":1,"git":"not an object"}`} {
		p := paths(t)
		if err := os.WriteFile(p.ToolsState, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		f := toolstate.Load(p)
		var g gitState
		if f.Section("git", &g) {
			t.Errorf("%q: git section read", body)
		}
		if c := f.Common("git"); c.LastOpen != "" || c.Zoomed != nil {
			t.Errorf("%q: common = %+v", body, c)
		}
		if err := f.SetSection("git", gitState{Recent: []string{"a"}}); err != nil {
			t.Errorf("%q: SetSection after a broken file: %v", body, err)
		}
	}
}

// Unknown top-level keys (a newer masil, another tool) survive a write.
func TestToolStateKeepsUnknownKeys(t *testing.T) {
	p := paths(t)
	if err := os.WriteFile(p.ToolsState, []byte(`{"version":1,"future":{"x":1},"git":{"recent":["a"],"extra":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := toolstate.Load(p)
	if err := f.SetCommon("api", toolstate.Common{LastOpen: termhost.ModeExec}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.ToolsState)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	if string(top["future"]) != `{"x":1}` {
		t.Errorf("future = %s", top["future"])
	}
	var git map[string]any
	_ = json.Unmarshal(top["git"], &git)
	if git["extra"] != true {
		t.Errorf("git = %s", top["git"])
	}
}

func TestToolStateRejectsBadNames(t *testing.T) {
	f := toolstate.Load(paths(t))
	for _, name := range []string{"", "version"} {
		if err := f.SetSection(name, map[string]int{"a": 1}); err == nil {
			t.Errorf("SetSection(%q) accepted", name)
		}
	}
	if err := f.SetSection("git", []int{1}); err == nil {
		t.Error("SetSection with a non-object accepted")
	}
}

// A File without a path (a hand-built ui.Context, zero Paths) works in memory.
func TestToolStateWithoutPath(t *testing.T) {
	f := toolstate.Load(config.Paths{})
	if err := f.SetSection("git", gitState{Recent: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	var g gitState
	if !f.Section("git", &g) || len(g.Recent) != 1 {
		t.Errorf("in-memory section = %+v", g)
	}
	var nilFile *toolstate.File
	if nilFile.Section("git", &g) {
		t.Error("nil File has a section")
	}
}
