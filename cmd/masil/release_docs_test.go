package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	plan06 = "docs/superpowers/plans/2026-11-05-masil-06-daemon-release.md"
	spec   = "docs/superpowers/specs/2026-11-05-masil-design.md"
	// The server refuses clients below MIN_CLIENT_VERSION (0.1.0); an
	// unstamped build reports 0.0.0-dev, so the gate commands stamp one.
	gateVersionFlag = `-ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev"`
)

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// v0.1.1 R4: the license is decided (owner decision D6: MIT), so the spec and
// plan 06 must stop saying it is open.
func TestReleaseDocsLicenseMIT(t *testing.T) {
	for _, rel := range []string{plan06, spec} {
		doc := readDoc(t, rel)
		for _, stale := range []string{
			"공개 전환 시 오너가 결정",
			"공개 전환 때 결정",
			"공개 전환 때 오너가 정하므로",
			"공개 전환 때 오너가 정할 때까지",
			"라이선스 미정",
		} {
			if strings.Contains(doc, stale) {
				t.Errorf("%s: still says %q (license is MIT, D6)", rel, stale)
			}
		}
	}
	for _, line := range strings.Split(readDoc(t, spec), "\n") {
		if strings.Contains(line, "라이선스") && !strings.Contains(line, "MIT") {
			t.Errorf("%s: license line without MIT: %s", spec, line)
		}
	}
	plan := readDoc(t, plan06)
	if !strings.Contains(plan, "12. **라이선스**: MIT") {
		t.Errorf("%s: Task 13 item 12 must say MIT", plan06)
	}
}

// v0.1.1 R4: the gate commands of plan 06 Tasks 14-18 run against a server
// with MIN_CLIENT_VERSION=0.1.0, and the binary check cannot be a
// `go version -m | grep DefaultServer=` (-trimpath drops -ldflags from build
// info; scripts/check-release-binary.sh does it).
func TestReleaseDocsGateCommands(t *testing.T) {
	plan := readDoc(t, plan06)
	start := strings.Index(plan, "\n### Task 14:")
	if start < 0 {
		t.Fatalf("%s: no Task 14", plan06)
	}
	gates := plan[start:]
	checked := 0
	for _, cmd := range []string{"go test -tags e2e", "go build"} {
		for rest := gates; ; {
			i := strings.Index(rest, cmd)
			if i < 0 {
				break
			}
			rest = rest[i+len(cmd):]
			checked++
			if !strings.HasPrefix(rest, " "+gateVersionFlag+" ") {
				end := strings.IndexAny(rest, "`\n")
				if end < 0 {
					end = len(rest)
				}
				t.Errorf("%s Tasks 14-18: `%s%s` lacks %s", plan06, cmd, rest[:end], gateVersionFlag)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("%s: found only %d gate commands; the check would pass vacuously", plan06, checked)
	}

	if strings.Contains(plan, "grep -E 'CGO_ENABLED=0|DefaultServer=") {
		t.Errorf("%s: `go version -m | grep DefaultServer=` can never match under -trimpath", plan06)
	}
	if !strings.Contains(plan, "sh scripts/check-release-binary.sh dist/masil_linux_amd64") {
		t.Errorf("%s: Task 9 Step 4 must check the binary with scripts/check-release-binary.sh", plan06)
	}
}
