package archsim_test

import (
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/patrache/masil/"

// deps lists pkg's dependencies (not its tests') with go list.
func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", module+pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

// TestPureImports (P3 Task 27, CA 2.1·2.2): the design engine, its price
// tables, the drawing and the exports are pure — no UI, no server client, no
// collaboration, no network, no programs (the archexport tests may run
// programs; the package may not). Chat draws cards through the core card
// registry only: it never imports the arch tool.
func TestPureImports(t *testing.T) {
	banned := []string{"net", "net/http", "os/exec"}
	bannedPrefix := []string{module + "internal/ui", module + "internal/api", module + "internal/collab"}
	for _, pkg := range []string{"internal/archdoc", "internal/archsim", "internal/archsim/pricing", "internal/archsim/challenge", "internal/archdraw", "internal/archexport"} {
		for _, d := range deps(t, pkg) {
			for _, b := range banned {
				if d == b {
					t.Errorf("%s imports %s", pkg, d)
				}
			}
			for _, b := range bannedPrefix {
				if d == b || strings.HasPrefix(d, b+"/") {
					t.Errorf("%s imports %s", pkg, d)
				}
			}
		}
	}
	for _, d := range deps(t, "internal/ui/chat") {
		if d == module+"internal/ui/arch" || strings.HasPrefix(d, module+"internal/ui/arch/") || strings.HasPrefix(d, module+"internal/arch") {
			t.Errorf("internal/ui/chat imports %s", d)
		}
	}
}
