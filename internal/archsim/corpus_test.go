package archsim

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xgolden "github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/archdoc"
)

// corpus loads testdata/corpus/*.json (names without the extension). Every
// file must pass archdoc validation.
func corpus(t *testing.T) ([]string, map[string]*archdoc.Design) {
	t.Helper()
	files, err := filepath.Glob("testdata/corpus/*.json")
	if err != nil || len(files) != 20 {
		t.Fatalf("corpus has %d files: %v", len(files), err)
	}
	var names []string
	out := map[string]*archdoc.Design{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := archdoc.Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if is := d.Validate(); len(is) > 0 {
			t.Fatalf("%s: %v", f, is)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		names = append(names, name)
		out[name] = d
	}
	return names, out
}

// corpusOutput is the bytes the goldens fix: the steady-state analysis and the
// summary of a 120-second timed run.
func corpusOutput(t *testing.T, d *archdoc.Design) []byte {
	t.Helper()
	tl, err := RunTimed(context.Background(), d, TimedOptions{DurationS: 120})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	b.Write(Analyze(d, Options{}).Canonical())
	b.WriteByte('\n')
	b.Write(tl.Summary.Canonical())
	b.WriteByte('\n')
	return b.Bytes()
}

// TestCorpusGolden pins every number of 20 designs. It is not skipped under
// -short: CI runs it on linux/amd64 and darwin/arm64, and both must match the
// same goldens (JC8). Regenerate with -args -update only on purpose, and bump
// EngineVersion when the numbers change.
func TestCorpusGolden(t *testing.T) {
	names, ds := corpus(t)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			xgolden.RequireEqual(t, corpusOutput(t, ds[name]))
		})
	}
}

func TestCorpusTwice(t *testing.T) {
	names, ds := corpus(t)
	for _, name := range names {
		if a, b := corpusOutput(t, ds[name]), corpusOutput(t, ds[name]); !bytes.Equal(a, b) {
			t.Fatalf("%s: two runs differ", name)
		}
	}
}
