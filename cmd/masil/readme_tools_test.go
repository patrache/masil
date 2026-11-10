package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// readmeSection is the text of a "## heading" section (up to the next "## ").
func readmeSection(t *testing.T, doc, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(doc, "\n"+heading+"\n")
	if !ok {
		t.Fatalf("no %q section", heading)
	}
	if i := strings.Index(rest, "\n## "); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// TestReadmeToolsSection (P0 Task 31): both READMEs have a tools section with
// one table row per released tool — its ctrl+t hub number, its name as the
// app shows it in that README's language, and a one-line description — and
// nothing for a tool outside the release set. This binary links every tool
// (register_*.go), so ui.Tools() is the hub as the release shows it.
func TestReadmeToolsSection(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "all")
	all := ui.Tools()
	if len(all) != len(features.Known()) {
		t.Fatalf("registered tools = %d, want %d (a register_*.go is missing)", len(all), len(features.Known()))
	}
	for _, c := range []struct {
		rel, heading string
		lang         i18n.Locale
	}{
		{"README.md", "## Tools", i18n.En},
		{"README.ko.md", "## 도구", i18n.Ko},
	} {
		sec := readmeSection(t, readRepo(t, c.rel), c.heading)
		tr := i18n.For(c.lang)
		for _, spec := range all {
			row := fmt.Sprintf("| `%d` | %s | ", spec.Order, spec.Name(tr))
			released := false
			for _, id := range features.Released() {
				released = released || id == spec.ID
			}
			n := strings.Count(sec, row)
			switch {
			case released && n != 1:
				t.Errorf("%s %s: want one row starting %q, got %d", c.rel, c.heading, row, n)
			case !released && n != 0:
				t.Errorf("%s %s: %q is not released but has a row", c.rel, c.heading, spec.ID)
			}
		}
		if !strings.Contains(sec, "MASIL_FEATURES=none") {
			t.Errorf("%s %s: does not say how to hide every tool (MASIL_FEATURES=none)", c.rel, c.heading)
		}
	}
}
