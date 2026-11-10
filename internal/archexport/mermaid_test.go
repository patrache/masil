package archexport

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/archdoc"
)

func loadDesign(t *testing.T, name string) *archdoc.Design {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "designs", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := archdoc.Decode(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return d
}

// hostileLabels reads testdata/hostile_labels.txt (one Go-quoted string per line).
func hostileLabels(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "hostile_labels.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s, err := strconv.Unquote(sc.Text())
		if err != nil {
			t.Fatalf("%q: %v", sc.Text(), err)
		}
		out = append(out, s)
	}
	return out
}

// hostileDesign puts label everywhere a user can type: node and box labels,
// edge protocols (unvalidated copies), region codes and instance types.
func hostileDesign(t *testing.T, label string) *archdoc.Design {
	d := loadDesign(t, "three_tier")
	d.Title = label
	for i := range d.Nodes {
		d.Nodes[i].Label, d.Nodes[i].InstanceType = label, label
	}
	for i := range d.Boxes {
		d.Boxes[i].Label, d.Boxes[i].RegionCode = label, label
	}
	for i := range d.Edges {
		d.Edges[i].Protocol = archdoc.Protocol(label)
	}
	return d
}

var testHeader = []string{"자동 생성된 그림 · masil", "%%{init: {\"theme\":\"dark\"}}%%", "two\nlines"}

func TestMermaidGolden(t *testing.T) {
	golden.RequireEqual(t, Mermaid(loadDesign(t, "three_tier"), Options{Header: testHeader}))
}

// outsideQuotes drops every "…" segment of a line (escaped quotes do not end
// one when esc is true) and reports whether the quotes were balanced.
func outsideQuotes(line string, esc bool) (string, bool) {
	var b strings.Builder
	in := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case in && esc && c == '\\':
			i++
		case c == '"':
			in = !in
		case !in:
			b.WriteByte(c)
		}
	}
	return b.String(), !in
}

var forbiddenRunes = regexp.MustCompile("[؜‎‏  ‪-‮⁦-⁩]")

func checkClean(t *testing.T, label, out string) {
	t.Helper()
	for i := 0; i < len(out); i++ {
		if c := out[i]; c < 0x20 && c != '\n' || c == 0x7f {
			t.Fatalf("label %q: control byte %#x in output", label, c)
		}
	}
	if forbiddenRunes.MatchString(out) {
		t.Fatalf("label %q: bidi or separator character in output", label)
	}
}

func TestMermaidHostileLabels(t *testing.T) {
	for _, label := range hostileLabels(t) {
		out := string(Mermaid(hostileDesign(t, label), Options{Header: []string{label}}))
		checkClean(t, label, out)
		for _, line := range strings.Split(out, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "click") || strings.HasPrefix(trim, "%%{") {
				t.Fatalf("label %q: directive line %q", label, line)
			}
			if strings.HasPrefix(trim, "%%") {
				if strings.Contains(trim, "%%{") {
					t.Fatalf("label %q: header %q", label, line)
				}
				continue
			}
			rest, ok := outsideQuotes(line, false)
			if !ok {
				t.Fatalf("label %q: unbalanced quotes in %q", label, line)
			}
			if strings.Contains(strings.ToLower(rest), "click") || strings.Contains(rest, "%") || strings.Contains(rest, "`") {
				t.Fatalf("label %q: user text outside quotes in %q", label, line)
			}
		}
	}
}

func TestSafeIDs(t *testing.T) {
	d := loadDesign(t, "three_tier")
	ids := safeIDs(d)
	if ids["api-a"] != "n1" || ids["users"] != "n12" || ids["az-a"] != "b1" || ids["vpc"] != "b4" {
		t.Fatalf("ids = %v", ids)
	}
	// a box sharing a node's ID is dropped; a box parent cycle is cut at the smallest ID
	d.Boxes = append(d.Boxes, archdoc.Box{ID: "users", Kind: archdoc.BoxAZ},
		archdoc.Box{ID: "x1", Kind: archdoc.BoxVPC, Parent: "x2"}, archdoc.Box{ID: "x2", Kind: archdoc.BoxVPC, Parent: "x1"})
	ids = safeIDs(d)
	if ids["users"] != "n12" {
		t.Fatalf("node lost its ID to a box: %v", ids)
	}
	out := string(Mermaid(d, Options{}))
	if strings.Count(out, "subgraph") != 6 || strings.Count(out, "    end\n") < 2 {
		t.Fatalf("boxes:\n%s", out)
	}
}
