package lsp

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportRules: internal/lsp is engine code like internal/workspace — no
// UI, no bubbletea, no keychain, no masil server client, no third-party
// module (the protocol is written by hand).
func TestImportRules(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not on PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "github.com/patrache/masil/internal/lsp").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	const mod = "github.com/patrache/masil/"
	allowed := map[string]bool{mod + "internal/lsp": true, mod + "internal/version": true}
	for _, d := range strings.Fields(string(out)) {
		if !allowed[d] {
			t.Errorf("internal/lsp depends on %s", d)
		}
	}
}
