package archexport

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/textwidth"
)

var testSnapText = SnapshotText{
	Columns:  [6]string{"이름", "종류", "대수", "사용률", "p99", "비용"},
	Kind:     func(k archdoc.NodeKind) string { return "k:" + string(k) },
	CostNote: "/월 (추정 · 2026-11 가격표)",
}

func checkLines(t *testing.T, lines []string, width int) {
	t.Helper()
	for i, l := range lines {
		if w := textwidth.Width(l); w > width {
			t.Errorf("line %d width %d > %d: %q", i, w, width, l)
		}
		for _, b := range []byte(l) {
			if b < 0x20 || b == 0x7f {
				t.Errorf("line %d has control byte %#x: %q", i, b, l)
			}
		}
		if strings.ContainsRune(l, '‮') || strings.ContainsRune(l, '‏') {
			t.Errorf("line %d has a bidi control: %q", i, l)
		}
	}
}

func TestSnapshotGolden(t *testing.T) {
	d := loadDesign(t, "three_tier")
	r := archsim.Analyze(d, archsim.Options{})
	for _, w := range []int{80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			out := Snapshot(d, r, w, testSnapText)
			checkLines(t, strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), w)
			if !strings.Contains(string(out), "추정") || !strings.Contains(string(out), "$") {
				t.Errorf("no cost estimate note:\n%s", out)
			}
			golden.RequireEqual(t, out)
		})
	}
	// without an analysis: the picture and the names, no numbers
	out := string(Snapshot(d, nil, 80, testSnapText))
	if !strings.Contains(out, "결제 API") || strings.Contains(out, "$") {
		t.Errorf("nil result snapshot:\n%s", out)
	}
}

func TestSnapshotWidthAndControls(t *testing.T) {
	for _, label := range hostileLabels(t) {
		d := hostileDesign(t, label)
		d.Title = label
		r := archsim.Analyze(d, archsim.Options{Samples: 500})
		for _, w := range []int{80, 120} {
			out := Snapshot(d, r, w, SnapshotText{Columns: [6]string{label, label, label, label, label, label}, Kind: func(archdoc.NodeKind) string { return label }, CostNote: label})
			if strings.ContainsAny(string(out), "\x1b\r\t\x00") {
				t.Fatalf("control bytes for %q", label)
			}
			checkLines(t, strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), w)
		}
		checkLines(t, Mini(d, r), 60)
	}
}

func TestMiniBounds(t *testing.T) {
	d := archdoc.New("big")
	for i := range 300 {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID = fmt.Sprintf("n%03d", i)
		n.Label = fmt.Sprintf("결제-API-%03d", i)
		n.Pos = [2]int{(i % 20) * 16, (i / 20) * 6}
		d.Nodes = append(d.Nodes, n)
	}
	r := archsim.Analyze(d, archsim.Options{Samples: 500})
	lines := Mini(d, r)
	if len(lines) == 0 || len(lines) > 8 {
		t.Fatalf("Mini = %d lines, want 1..8", len(lines))
	}
	checkLines(t, lines, 60)
	if lines[len(lines)-1] != "…" {
		t.Errorf("cut picture does not end with …: %q", lines[len(lines)-1])
	}
	small := loadDesign(t, "three_tier")
	small.Boxes = nil
	for i := range small.Nodes {
		small.Nodes[i].Parent = ""
		small.Nodes[i].Pos = [2]int{(i % 3) * 16, (i / 3) * 6}
	}
	got := Mini(small, archsim.Analyze(small, archsim.Options{Samples: 500}))
	if len(got) > 8 {
		t.Fatalf("small Mini = %d lines", len(got))
	}
	checkLines(t, got, 60)
	if len(Mini(archdoc.New("empty"), nil)) > 8 {
		t.Error("empty design Mini too tall")
	}
}
