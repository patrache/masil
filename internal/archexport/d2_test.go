package archexport

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

func TestD2Golden(t *testing.T) {
	golden.RequireEqual(t, D2(loadDesign(t, "three_tier"), Options{Header: testHeader}))
}

func TestD2HostileLabels(t *testing.T) {
	for _, label := range hostileLabels(t) {
		out := string(D2(hostileDesign(t, label), Options{Header: []string{label}}))
		checkClean(t, label, out)
		for i := 0; i < len(out); i++ {
			if out[i] == '$' && (i == 0 || out[i-1] != '\\') {
				t.Fatalf("label %q: unescaped $ in output", label)
			}
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			rest, ok := outsideQuotes(line, true)
			if !ok {
				t.Fatalf("label %q: unbalanced quotes in %q", label, line)
			}
			for _, bad := range []string{"click", "direction: down", "${", "`", "<", "|"} {
				if strings.Contains(rest, bad) {
					t.Fatalf("label %q: %q outside quotes in %q", label, bad, line)
				}
			}
		}
		if strings.Count(out, "{") != strings.Count(out, "}") && !strings.ContainsAny(label, "{}") {
			t.Fatalf("label %q: unbalanced braces", label)
		}
	}
}
