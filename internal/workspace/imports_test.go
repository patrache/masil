package workspace_test

import (
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/patrache/masil/"

func goListDeps(t *testing.T, pattern string) []string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not on PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", pattern).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pattern, err)
	}
	return strings.Fields(string(out))
}

// TestImportRules enforces client-architecture 2.2: the workspace engine
// packages stay free of UI, bubbletea and the keychain, and the API client
// knows nothing about the workspace.
func TestImportRules(t *testing.T) {
	forbidden := []string{module + "internal/ui", "charm.land/bubbletea/v2", module + "internal/secrets"}
	deps := goListDeps(t, module+"internal/workspace/...")
	if len(deps) == 0 {
		t.Fatal("no deps listed")
	}
	for _, d := range deps {
		for _, f := range forbidden {
			if d == f || strings.HasPrefix(d, f+"/") {
				t.Errorf("internal/workspace/... depends on %s", d)
			}
		}
	}
	for _, d := range goListDeps(t, module+"internal/api") {
		if d == module+"internal/workspace" || strings.HasPrefix(d, module+"internal/workspace/") {
			t.Errorf("internal/api depends on %s", d)
		}
	}
}
