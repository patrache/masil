package main

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/patrache/masil/internal/version"
)

// 0.1.3 patch: the owner releases it as tag v0.1.3-1 — a "-N" patch
// revision, a normal release that must become releases/latest (install.sh,
// install.ps1 and Homebrew all follow it). goreleaser's `prerelease: auto`
// would mark any "-…" tag a pre-release (semver), and the tap and install
// smoke jobs skipped every tag containing '-'.

type patchJob struct {
	Needs any `yaml:"needs"`
	If    string
	Steps []wfStep `yaml:"steps"`
}

func (j patchJob) needs() []string {
	switch v := j.Needs.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, x := range v {
			out = append(out, x.(string))
		}
		return out
	}
	return nil
}

func readPatchWorkflow(t *testing.T) map[string]patchJob {
	t.Helper()
	_, wf := readReleaseFiles(t)
	var doc struct {
		Jobs map[string]patchJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(wf), &doc); err != nil {
		t.Fatalf("release.yml: %v", err)
	}
	return doc.Jobs
}

func TestReleaseNeverPrerelease(t *testing.T) {
	gr, _ := readReleaseFiles(t)
	var doc struct {
		Release struct {
			Prerelease string `yaml:"prerelease"`
			MakeLatest string `yaml:"make_latest"`
		} `yaml:"release"`
	}
	if err := yaml.Unmarshal([]byte(gr), &doc); err != nil {
		t.Fatalf(".goreleaser.yaml: %v", err)
	}
	if doc.Release.Prerelease != "false" {
		t.Errorf(".goreleaser.yaml release.prerelease = %q, want false (auto marks v0.1.3-1 a pre-release)", doc.Release.Prerelease)
	}
	if doc.Release.MakeLatest != "" && doc.Release.MakeLatest != "true" {
		t.Errorf(".goreleaser.yaml release.make_latest = %q", doc.Release.MakeLatest)
	}
}

// With prerelease off, a real pre-release tag (v0.2.0-rc.1) would be
// published as the latest release: the tag-check job refuses every tag but
// vX.Y.Z and vX.Y.Z-N before goreleaser runs.
func TestReleaseTagCheck(t *testing.T) {
	jobs := readPatchWorkflow(t)
	tc, ok := jobs["tag-check"]
	if !ok || len(tc.Steps) != 1 {
		t.Fatal("release.yml: no one-step tag-check job")
	}
	if !slices.Contains(jobs["goreleaser"].needs(), "tag-check") {
		t.Fatalf("release.yml: goreleaser needs %v, want tag-check among them", jobs["goreleaser"].needs())
	}
	for _, name := range []string{"homebrew-tap", "install-smoke"} {
		if strings.Contains(jobs[name].If, "'-'") {
			t.Errorf("release.yml: %s still skips tags with '-': if = %q", name, jobs[name].If)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("the tag-check step runs on ubuntu-latest (bash)")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	for tag, ok := range map[string]bool{
		"v0.1.3": true, "v0.1.3-1": true, "v1.20.300-12": true,
		"v0.2.0-rc.1": false, "v0.1.3-1a": false, "v0.1.3-": false, "v0.1": false, "0.1.3": false,
		"v0.1.3-1-2": false, "v0.1.3+meta": false,
	} {
		cmd := exec.Command(bash, "-e", "-c", tc.Steps[0].Run)
		cmd.Env = append(os.Environ(), "GITHUB_REF_NAME="+tag)
		out, err := cmd.CombinedOutput()
		if (err == nil) != ok {
			t.Errorf("tag-check %s: err = %v, want ok = %v (%s)", tag, err, ok, out)
		}
	}
}

// The tap formula is rendered from the tag without the v; its test matches
// `masil version` output, which carries goreleaser's {{ .Version }}.
func TestPatchVersionStrings(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	ov, oc, od := version.Version, version.Commit, version.Date
	t.Cleanup(func() { version.Version, version.Commit, version.Date = ov, oc, od })
	version.Version, version.Commit, version.Date = "0.1.3-1", "abc1234", "2026-11-10T08:00:00Z"
	if code := e.run("version"); code != 0 {
		t.Fatalf("version => %d", code)
	}
	if got := e.out.String(); got != "masil 0.1.3-1 (abc1234, 2026-11-10T08:00:00Z)\n" || !strings.Contains(got, "0.1.3-1") {
		t.Fatalf("stdout = %q", got)
	}
	// server/src/lib/version.ts reads the x.y.z prefix of the User-Agent.
	ua := version.UserAgent()
	if m := regexp.MustCompile(`^masil/(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(ua); m == nil || m[1]+"."+m[2]+"."+m[3] != "0.1.3" {
		t.Fatalf("User-Agent %q does not pass the server's version gate as 0.1.3", ua)
	}
	// The formula: VERSION=${GITHUB_REF_NAME#v} names both the version and the release URL.
	tmpl, err := os.ReadFile("../../packaging/homebrew/masil.rb.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	f := strings.ReplaceAll(string(tmpl), "__VERSION__", strings.TrimPrefix("v0.1.3-1", "v"))
	if !strings.Contains(f, `version "0.1.3-1"`) || !strings.Contains(f, "/releases/download/v0.1.3-1/masil_darwin_arm64.tar.gz") ||
		!strings.Contains(f, `assert_match version.to_s, shell_output("#{bin}/masil version")`) {
		t.Fatalf("formula for v0.1.3-1:\n%s", f)
	}
}
