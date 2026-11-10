package leetcode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The UI-free packages never reach the masil server client, the UI, the
// realtime socket or the local store (P1 Global Constraints).
func TestNoForbiddenImports(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := []string{"./internal/leetcode/..."}
	if _, err := os.Stat(filepath.Join(root, "internal", "lazygit")); err == nil {
		pkgs = append(pkgs, "./internal/lazygit/...")
	}
	cmd := exec.Command("go", append([]string{"list", "-deps"}, pkgs...)...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	const mod = "github.com/patrache/masil/internal/"
	for _, dep := range strings.Fields(string(out)) {
		for _, bad := range []string{"api", "ui", "realtime", "store"} {
			if dep == mod+bad || strings.HasPrefix(dep, mod+bad+"/") {
				t.Errorf("forbidden dependency %s", dep)
			}
		}
	}
}
