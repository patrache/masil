package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Owner decision D6 (2026-11-06): masil ships under the MIT license — a root
// LICENSE (copyright patrache), the LICENSE file in every goreleaser archive and
// `license "MIT"` in the Homebrew formula.
func TestReleaseLicenseMIT(t *testing.T) {
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}

	license := read("LICENSE")
	for _, want := range []string{
		"MIT License",
		"Copyright (c) 2026 patrache",
		"Permission is hereby granted, free of charge, to any person obtaining a copy",
		`THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND`,
	} {
		if !strings.Contains(license, want) {
			t.Errorf("LICENSE: missing %q", want)
		}
	}

	gr := read(".goreleaser.yaml")
	archives := strings.Index(gr, "\narchives:")
	if archives < 0 {
		t.Fatal(".goreleaser.yaml: no archives section")
	}
	section := gr[archives+1:]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end]
	}
	if !strings.Contains(section, "\n      - LICENSE\n") && !strings.HasSuffix(section, "\n      - LICENSE") {
		t.Errorf(".goreleaser.yaml: archives[].files must list LICENSE:\n%s", section)
	}

	if formula := read("packaging/homebrew/masil.rb.tmpl"); !strings.Contains(formula, "\n  license \"MIT\"\n") {
		t.Error(`packaging/homebrew/masil.rb.tmpl: formula must declare license "MIT"`)
	}

	// P5 Task 21: the English README and the Korean README.ko.md both say it.
	for _, c := range []struct{ rel, heading string }{
		{"README.md", "## License"},
		{"README.ko.md", "## 라이선스"},
	} {
		readme := read(c.rel)
		i := strings.Index(readme, "\n"+c.heading+"\n")
		if i < 0 {
			t.Errorf("%s: no %q section", c.rel, c.heading)
			continue
		}
		if sec := readme[i:]; !strings.Contains(sec, "MIT") || !strings.Contains(sec, "LICENSE") {
			t.Errorf("%s: %q section must name MIT and point at LICENSE:\n%s", c.rel, c.heading, sec)
		}
	}
}
