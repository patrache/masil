package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The import rules (v0.1.3 spec A9.3, CA 2.2): structurally, a tool secret can
// never ride a masil server request and the chat core never pulls a tool in.

// modulePrefix is the module path plus "/".
const modulePrefix = "github.com/patrache/masil/"

// toolPackagePrefixes are the tool packages (module-relative prefixes) that
// internal/api and internal/ui/chat must never depend on. Each tool stream adds
// its own line here.
var toolPackagePrefixes = []string{
	"internal/leetcode",
	"internal/apitool",
	"internal/arch",
	"internal/workspace",
}

// listedPackage is the part of `go list -json` the rules read.
type listedPackage struct {
	ImportPath string
	Imports    []string // direct
	Deps       []string // transitive
}

// goList runs `go list -json` on module-relative packages.
func goList(t *testing.T, pkgs ...string) map[string]listedPackage {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	args := []string{"list", "-json"}
	for _, p := range pkgs {
		args = append(args, modulePrefix+p)
	}
	cmd := exec.Command(goBin, args...)
	cmd.Dir = filepath.Join("..", "..")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	got := map[string]listedPackage{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		got[strings.TrimPrefix(p.ImportPath, modulePrefix)] = p
	}
	return got
}

// moduleRel keeps the module's own packages, module-relative.
func moduleRel(paths []string) []string {
	var out []string
	for _, p := range paths {
		if rel, ok := strings.CutPrefix(p, modulePrefix); ok {
			out = append(out, rel)
		}
	}
	return out
}

// under reports whether pkg is prefix or inside it.
func under(pkg, prefix string) bool {
	return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
}

// startsWith reports whether pkg's path starts with prefix (internal/arch
// covers internal/archsim, internal/archdoc …).
func startsWith(pkg, prefix string) bool { return strings.HasPrefix(pkg, prefix) }

// violations lists the deps of pkg matching any of the rules.
func violations(deps []string, bad func(string) bool) []string {
	var out []string
	for _, d := range moduleRel(deps) {
		if bad(d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out
}

func TestImportGraph(t *testing.T) {
	pk := goList(t, "internal/api", "internal/termhost", "internal/ui/chat")
	for _, name := range []string{"internal/api", "internal/termhost", "internal/ui/chat"} {
		if _, ok := pk[name]; !ok {
			t.Fatalf("go list did not report %s", name)
		}
	}

	// internal/api (the masil server client): no secrets, termhost, collab, UI, tools.
	apiBad := func(d string) bool {
		for _, p := range []string{"internal/secrets", "internal/termhost", "internal/collab", "internal/ui"} {
			if under(d, p) {
				return true
			}
		}
		for _, p := range toolPackagePrefixes {
			if startsWith(d, p) {
				return true
			}
		}
		return false
	}
	if v := violations(pk["internal/api"].Deps, apiBad); len(v) > 0 {
		t.Errorf("internal/api depends on %v", v)
	}

	// internal/termhost: no UI.
	if v := violations(pk["internal/termhost"].Deps, func(d string) bool { return under(d, "internal/ui") }); len(v) > 0 {
		t.Errorf("internal/termhost depends on %v", v)
	}

	// internal/ui/chat: no direct import of the tool surface, panels, secrets or
	// termhost (reaching them through core internal/ui is allowed), and no tool
	// package at all.
	chatDirect := func(d string) bool {
		for _, p := range []string{"internal/ui/tools", "internal/ui/termpane", "internal/secrets", "internal/termhost"} {
			if under(d, p) {
				return true
			}
		}
		return false
	}
	if v := violations(pk["internal/ui/chat"].Imports, chatDirect); len(v) > 0 {
		t.Errorf("internal/ui/chat imports %v directly", v)
	}
	toolBad := func(d string) bool {
		for _, p := range toolPackagePrefixes {
			if startsWith(d, p) {
				return true
			}
		}
		return false
	}
	if v := violations(pk["internal/ui/chat"].Deps, toolBad); len(v) > 0 {
		t.Errorf("internal/ui/chat depends on tool packages %v", v)
	}
}

// TestImportGraphRules checks the rule helpers on made-up paths.
func TestImportGraphRules(t *testing.T) {
	if !startsWith("internal/archsim", "internal/arch") || !startsWith("internal/apitool/vars", "internal/apitool") {
		t.Error("tool prefixes must cover the packages they name")
	}
	if under("internal/uix", "internal/ui") || !under("internal/ui/chat", "internal/ui") || !under("internal/ui", "internal/ui") {
		t.Error("under")
	}
	if startsWith("internal/api/apitest", "internal/apitool") {
		t.Error("internal/api/apitest is not a tool package")
	}
	got := violations([]string{modulePrefix + "internal/secrets", "fmt", modulePrefix + "internal/store"}, func(d string) bool { return under(d, "internal/secrets") })
	if !slices.Equal(got, []string{"internal/secrets"}) {
		t.Errorf("violations = %v", got)
	}
}
