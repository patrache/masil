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
	// Both lines of the plan's form (P1 Task 17), whatever the caveats around
	// them become (P5 Task 21 makes them English + Korean).
	for _, line := range []string{
		"To use the lazygit tool: brew install lazygit",
		"lazygit 도구를 쓰려면: brew install lazygit",
	} {
		if !strings.Contains(caveats, line) {
			t.Errorf("masil.rb.tmpl: caveats lack %q:\n%s", line, caveats)
		}
	}
}

// v0.1.3 A11.7: lazygit is MIT and not bundled, so the README tools section
// names it and links it — in both READMEs (P1 Task 17 Step 6).
func TestReadmeLazygitLink(t *testing.T) {
	for _, c := range []struct{ rel, heading, line string }{
		{"README.md", "## Tools", "lazygit — https://github.com/jesseduffield/lazygit (MIT, install it yourself)"},
		{"README.ko.md", "## 도구", "lazygit — https://github.com/jesseduffield/lazygit (MIT, 직접 설치)"},
	} {
		if sec := readmeSection(t, readRepo(t, c.rel), c.heading); !strings.Contains(sec, c.line) {
			t.Errorf("%s %s: no line %q", c.rel, c.heading, c.line)
		}
	}
}
