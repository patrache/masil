package i18n_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// TestGeneratedFresh regenerates into a temp dir and compares with the
// committed zz_*_gen.go files. It also runs under -short: CI runs
// "go test -short ./..." and this is its only "go generate left no diff" check
// (i18n spec 6.4.3); the generator takes well under a second.
func TestGeneratedFresh(t *testing.T) {
	out := t.TempDir()
	cmd := exec.Command("go", "run", "./gen", "-out", out, "-fmt=false", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generator failed: %v\n%s", err, b)
	}
	gen, _ := filepath.Glob(filepath.Join(out, "zz_*_gen.go"))
	have, _ := filepath.Glob("zz_*_gen.go")
	names := func(ps []string) []string {
		var ns []string
		for _, p := range ps {
			ns = append(ns, filepath.Base(p))
		}
		slices.Sort(ns)
		return ns
	}
	if !slices.Equal(names(gen), names(have)) {
		t.Fatalf("generated files %v, committed %v — run go generate ./internal/i18n", names(gen), names(have))
	}
	for _, p := range gen {
		want, _ := os.ReadFile(p)
		got, err := os.ReadFile(filepath.Base(p))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is stale — run go generate ./internal/i18n", filepath.Base(p))
		}
	}
}
