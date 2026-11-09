package apitool

import (
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

const modPrefix = "github.com/patrache/masil/"

// importGraph maps a module-relative package to its direct (non-test)
// module-relative imports.
type importGraph map[string][]string

// loadGraph is the import graph of pkgs and all their deps (go list -deps).
func loadGraph(t *testing.T, pkgs ...string) importGraph {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	args := append([]string{"list", "-deps", "-json=ImportPath,Imports"}, pkgs...)
	cmd := exec.Command(goBin, args...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	g := importGraph{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("go list json: %v", err)
		}
		rel, ok := strings.CutPrefix(p.ImportPath, modPrefix)
		if !ok {
			continue
		}
		var imps []string
		for _, imp := range p.Imports {
			if r, ok := strings.CutPrefix(imp, modPrefix); ok {
				imps = append(imps, r)
			}
		}
		g[rel] = imps
	}
	return g
}

func under(pkg, prefix string) bool { return pkg == prefix || strings.HasPrefix(pkg, prefix+"/") }

var forbiddenForApitool = []string{"internal/api", "internal/ui", "internal/realtime", "internal/store"}

// forbiddenReach walks the graph from roots without descending into
// internal/collab and returns, for every forbidden package reached, the
// import path that reaches it ("a -> b -> c"). What only collab brings in is
// therefore allowed; a direct import, or any path around collab, is not.
func forbiddenReach(g importGraph, roots []string) []string {
	type item struct {
		pkg  string
		path []string
	}
	seen := map[string]bool{}
	var queue []item
	for _, r := range roots {
		queue = append(queue, item{r, []string{r}})
		seen[r] = true
	}
	var bad []string
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if slices.ContainsFunc(forbiddenForApitool, func(p string) bool { return under(it.pkg, p) }) {
			bad = append(bad, strings.Join(it.path, " -> "))
			continue
		}
		if under(it.pkg, "internal/collab") {
			continue // collab (types only) may itself use api and realtime
		}
		for _, imp := range g[it.pkg] {
			if !seen[imp] {
				seen[imp] = true
				queue = append(queue, item{imp, append(slices.Clone(it.path), imp)})
			}
		}
	}
	slices.Sort(bad)
	return bad
}

func TestForbiddenReachByEdges(t *testing.T) {
	g := importGraph{
		"internal/apitool":         {"internal/collab", "internal/config"},
		"internal/apitool/convert": {"internal/apitool"},
		"internal/collab":          {"internal/api", "internal/realtime"},
		"internal/config":          {},
	}
	roots := []string{"internal/apitool", "internal/apitool/convert"}
	if bad := forbiddenReach(g, roots); len(bad) != 0 {
		t.Fatalf("only via collab: %v", bad)
	}
	// a direct import of api is caught even though collab also brings it in
	g["internal/apitool"] = append(g["internal/apitool"], "internal/api")
	if bad := forbiddenReach(g, roots); !slices.Equal(bad, []string{"internal/apitool -> internal/api"}) {
		t.Fatalf("direct api: %v", bad)
	}
	// a path around collab is caught too
	g["internal/apitool"] = []string{"internal/collab", "internal/config"}
	g["internal/config"] = []string{"internal/realtime"}
	if bad := forbiddenReach(g, roots); !slices.Equal(bad, []string{"internal/apitool -> internal/config -> internal/realtime"}) {
		t.Fatalf("around collab: %v", bad)
	}
}

// TestApitoolImportRules: the engine never reaches the masil API client, the
// UI, the realtime socket or the store (CA 2.2) — neither directly nor on any
// import path that does not go through internal/collab (types only), which
// itself depends on api and realtime.
func TestApitoolImportRules(t *testing.T) {
	g := loadGraph(t, "./internal/apitool/...")
	var roots []string
	for p := range g {
		if under(p, "internal/apitool") {
			roots = append(roots, p)
		}
	}
	if !slices.Contains(roots, "internal/apitool") {
		t.Fatalf("go list did not report internal/apitool: %v", roots)
	}
	for _, path := range forbiddenReach(g, roots) {
		t.Errorf("forbidden import path: %s", path)
	}
}
