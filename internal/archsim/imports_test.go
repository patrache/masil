package archsim_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/patrache/masil/"

// targetOSes are the release platforms. Import edges differ per GOOS (a
// _windows.go file may pull in a package that the darwin build never sees:
// golang.org/x/sys/windows imports net), so the purity check lists the
// dependencies for every one of them, not only the host's.
var targetOSes = []string{"darwin", "linux", "windows"}

// deps lists pkg's dependencies (not its tests') for goos with go list.
func deps(t *testing.T, pkg, goos string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", module+pkg)
	cmd.Env = append(os.Environ(), "GOOS="+goos)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("GOOS=%s go list -deps %s: %v", goos, pkg, err)
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
	for _, goos := range targetOSes {
		for _, pkg := range []string{"internal/archdoc", "internal/archsim", "internal/archsim/pricing", "internal/archsim/challenge", "internal/archdraw", "internal/archexport"} {
			for _, d := range deps(t, pkg, goos) {
				for _, b := range banned {
					if d == b {
						t.Errorf("GOOS=%s: %s imports %s", goos, pkg, d)
					}
				}
				for _, b := range bannedPrefix {
					if d == b || strings.HasPrefix(d, b+"/") {
						t.Errorf("GOOS=%s: %s imports %s", goos, pkg, d)
					}
				}
			}
		}
		for _, d := range deps(t, "internal/ui/chat", goos) {
			if d == module+"internal/ui/arch" || strings.HasPrefix(d, module+"internal/ui/arch/") || strings.HasPrefix(d, module+"internal/arch") {
				t.Errorf("GOOS=%s: internal/ui/chat imports %s", goos, d)
			}
		}
	}
}
