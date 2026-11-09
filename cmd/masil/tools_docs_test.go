package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.1.3 A11.1 (JA19): lazygit is the user's own install. The Homebrew
// formula only mentions it in caveats and never depends on it.
func TestFormulaCaveatsMentionLazygit(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "packaging", "homebrew", "masil.rb.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	formula := strings.ReplaceAll(string(b), "\r\n", "\n")
	if strings.Contains(formula, `depends_on "lazygit"`) || strings.Contains(formula, "depends_on 'lazygit'") {
		t.Error(`masil.rb.tmpl: lazygit must not be a dependency (depends_on "lazygit")`)
	}
	_, caveats, ok := strings.Cut(formula, "def caveats")
	if !ok {
		t.Fatal("masil.rb.tmpl: no caveats")
	}
	caveats, _, _ = strings.Cut(caveats, "\n  end\n")
	if !strings.Contains(caveats, "brew install lazygit") {
		t.Errorf("masil.rb.tmpl: caveats do not mention brew install lazygit:\n%s", caveats)
	}
}
