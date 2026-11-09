package apitool

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const modPrefix = "github.com/patrache/masil/"

// goDeps lists the transitive (non-test) deps of pkgs, module-relative.
func goDeps(t *testing.T, pkgs ...string) []string {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	args := append([]string{"list", "-deps"}, pkgs...)
	cmd := exec.Command(goBin, args...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var rel []string
	for _, l := range strings.Fields(string(out)) {
		if r, ok := strings.CutPrefix(l, modPrefix); ok {
			rel = append(rel, r)
		}
	}
	return rel
}

func under(pkg, prefix string) bool { return pkg == prefix || strings.HasPrefix(pkg, prefix+"/") }

// TestApitoolImportRules: the engine never reaches the masil API client, the
// UI, the realtime socket or the store (CA 2.2). internal/collab (types only)
// itself depends on api and realtime, so what collab brings in is set aside.
func TestApitoolImportRules(t *testing.T) {
	deps := goDeps(t, "./internal/apitool/...")
	viaCollab := goDeps(t, "./internal/collab")
	allowedDirect := []string{"internal/apitool", "internal/secrets", "internal/collab", "internal/config", "internal/ids", "internal/textwidth"}
	for _, d := range deps {
		if under(d, "internal/apitool") {
			continue
		}
		if slices.Contains(viaCollab, d) && !slices.ContainsFunc(allowedDirect, func(p string) bool { return under(d, p) }) {
			continue // only reachable through collab
		}
		for _, bad := range []string{"internal/api", "internal/ui", "internal/realtime", "internal/store"} {
			if under(d, bad) {
				t.Errorf("internal/apitool depends on %s", d)
			}
		}
	}
	if !slices.Contains(deps, "internal/apitool") {
		t.Fatalf("go list did not report internal/apitool: %v", deps)
	}
}
