package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wantGoVersion is the toolchain the owner approved for v0.1.3 (spec 3.1,
// OD12). go.mod's go directive and CI's setup-go must name exactly this.
const wantGoVersion = "1.26.6"

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// goModRequires returns every module path and version in go.mod's require
// directives (direct and indirect, block and single-line forms).
func goModRequires(gomod string) map[string]string {
	mods := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		case !inBlock:
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 {
			mods[f[0]] = f[1]
		}
	}
	return mods
}

// A11.7 / JA30: x/vt, x/xpty, creack/pty, x/conpty and goeditor are MIT and
// compiled into the binary, so every release archive carries the notices of
// every module in go.mod (scripts/notices.sh regenerates the file).
func TestThirdPartyNoticesCoversModules(t *testing.T) {
	mods := goModRequires(readRepo(t, "go.mod"))
	if len(mods) == 0 {
		t.Fatal("go.mod: no require directives found; the check would pass vacuously")
	}
	notices := readRepo(t, "THIRD_PARTY_NOTICES")
	for mod, ver := range mods {
		if !strings.Contains(notices, "\n== "+mod+" "+ver+" ==\n") && !strings.HasPrefix(notices, "== "+mod+" "+ver+" ==\n") {
			t.Errorf("THIRD_PARTY_NOTICES: missing header %q (run scripts/notices.sh)", "== "+mod+" "+ver+" ==")
		}
	}

	gr := readRepo(t, ".goreleaser.yaml")
	archives := strings.Index(gr, "\narchives:")
	if archives < 0 {
		t.Fatal(".goreleaser.yaml: no archives section")
	}
	section := gr[archives+1:]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end]
	}
	if !strings.Contains(section+"\n", "\n      - THIRD_PARTY_NOTICES\n") {
		t.Errorf(".goreleaser.yaml: archives[].files must list THIRD_PARTY_NOTICES:\n%s", section)
	}
}

var (
	goDirectiveRE = regexp.MustCompile(`(?m)^go ([0-9][^\s]*)\s*$`)
	toolchainRE   = regexp.MustCompile(`(?m)^toolchain\s`)
	ciGoVersionRE = regexp.MustCompile(`(?m)^\s*go-version:\s*'([^']*)'\s*$`)
)

func TestGoDirective(t *testing.T) {
	gomod := readRepo(t, "go.mod")
	m := goDirectiveRE.FindStringSubmatch(gomod)
	if m == nil {
		t.Fatal("go.mod: no go directive")
	}
	if m[1] != wantGoVersion {
		t.Errorf("go.mod: go directive = %s, want %s", m[1], wantGoVersion)
	}
	if toolchainRE.MatchString(gomod) {
		t.Error("go.mod: must not carry a toolchain directive")
	}

	ci := readRepo(t, ".github/workflows/ci.yml")
	vs := ciGoVersionRE.FindAllStringSubmatch(ci, -1)
	if len(vs) == 0 {
		t.Fatal("ci.yml: no quoted go-version")
	}
	for _, v := range vs {
		if v[1] != wantGoVersion {
			t.Errorf("ci.yml: go-version = %q, want %q", v[1], wantGoVersion)
		}
	}
}
