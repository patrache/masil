package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type wfStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   string            `yaml:"if"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type wfJob struct {
	Steps []wfStep `yaml:"steps"`
}

func readReleaseWorkflow(t *testing.T) map[string]wfJob {
	t.Helper()
	_, wf := readReleaseFiles(t)
	var doc struct {
		Jobs map[string]wfJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(wf), &doc); err != nil {
		t.Fatalf("release.yml: %v", err)
	}
	return doc.Jobs
}

func findStep(steps []wfStep, match func(wfStep) bool) int {
	for i, s := range steps {
		if match(s) {
			return i
		}
	}
	return -1
}

const goreleaserInstall = "go install github.com/goreleaser/goreleaser/v2@v2.19.0"

// v0.1.1 R1: on 2026-11-08 GitHub release downloads answered 504 and
// goreleaser-action, which downloads the goreleaser binary from GitHub
// Releases, failed the release before anything was built. goreleaser is now
// built from proxy.golang.org (checked against sum.golang.org) with retries,
// and then run directly.
func TestReleaseInstallsGoreleaserFromGoProxy(t *testing.T) {
	steps := readReleaseWorkflow(t)["goreleaser"].Steps
	if len(steps) == 0 {
		t.Fatal("release.yml: no goreleaser job")
	}
	if i := findStep(steps, func(s wfStep) bool { return strings.HasPrefix(s.Uses, "goreleaser/goreleaser-action") }); i >= 0 {
		t.Errorf("release.yml: step %d still uses goreleaser-action (downloads from GitHub Releases)", i)
	}
	guard := findStep(steps, func(s wfStep) bool { return strings.Contains(s.Run, "MASIL_DEFAULT_SERVER") })
	install := findStep(steps, func(s wfStep) bool { return strings.Contains(s.Run, goreleaserInstall) })
	release := findStep(steps, func(s wfStep) bool { return strings.TrimSpace(s.Run) == "goreleaser release --clean" })
	upload := findStep(steps, func(s wfStep) bool { return strings.HasPrefix(s.Uses, "actions/upload-artifact") })
	if install < 0 {
		t.Fatalf("release.yml: no step runs %q", goreleaserInstall)
	}
	if release < 0 {
		t.Fatal("release.yml: no step runs `goreleaser release --clean`")
	}
	if !(guard >= 0 && guard < install && install < release && release < upload) {
		t.Errorf("release.yml: want guard < install < release < upload, got %d %d %d %d", guard, install, release, upload)
	}
	// setup-go exports GOTOOLCHAIN=local, and goreleaser v2.19.0's go.mod
	// says go 1.27.3: without auto the install fails ("requires go >= 1.27.3
	// (running go 1.26.x; GOTOOLCHAIN=local)").
	if got := steps[install].Env["GOTOOLCHAIN"]; got != "auto" {
		t.Errorf("release.yml: install step needs env GOTOOLCHAIN: auto, got %q", got)
	}
	if got := steps[install].Env["GOBIN"]; got == "" {
		t.Error("release.yml: install step must set GOBIN so the binary path is known")
	}
	if _, ok := steps[release].Env["GOTOOLCHAIN"]; ok {
		t.Error("release.yml: the release step must build masil with setup-go's toolchain, not override GOTOOLCHAIN")
	}
	if got := steps[release].Env["GITHUB_TOKEN"]; got != "${{ secrets.GITHUB_TOKEN }}" {
		t.Errorf("release.yml: release step env GITHUB_TOKEN = %q", got)
	}

	runInstallStep(t, steps[install].Run)
}

// runInstallStep runs the install step's script the way an ubuntu runner does
// (bash -eo pipefail) against a fake `go` that fails a few times.
func runInstallStep(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the goreleaser job runs on ubuntu-latest")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	run := func(failures int) (out string, code int, calls []string, ghPath string, gobin string) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "fake")
		gobin = filepath.Join(dir, "gobin")
		ghPath = filepath.Join(dir, "github_path")
		if err := os.MkdirAll(fake, 0o755); err != nil {
			t.Fatal(err)
		}
		writeExe(t, filepath.Join(fake, "go"), `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
n=$(wc -l < "$FAKE_LOG")
if [ "$n" -le "$FAKE_FAILURES" ]; then
	echo "go: proxy.golang.org: 504 Gateway Timeout" >&2
	exit 1
fi
mkdir -p "$GOBIN"
printf '#!/bin/sh\necho "goreleaser fake"\n' > "$GOBIN/goreleaser"
chmod +x "$GOBIN/goreleaser"
`)
		writeExe(t, filepath.Join(fake, "sleep"), "#!/bin/sh\necho \"sleep $*\" >> \"$FAKE_SLEEPS\"\n")
		sf := filepath.Join(dir, "step.sh")
		if err := os.WriteFile(sf, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(dir, "go.log")
		if err := os.WriteFile(log, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bash, "--noprofile", "--norc", "-eo", "pipefail", sf)
		cmd.Env = append(os.Environ(),
			"PATH="+fake+string(os.PathListSeparator)+os.Getenv("PATH"),
			"GOBIN="+gobin, "GOTOOLCHAIN=auto", "GITHUB_PATH="+ghPath,
			"FAKE_LOG="+log, "FAKE_SLEEPS="+filepath.Join(dir, "sleeps"),
			"FAKE_FAILURES="+strconv.Itoa(failures))
		b, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		lb, _ := os.ReadFile(log)
		calls = strings.Split(strings.TrimSpace(string(lb)), "\n")
		return string(b), code, calls, ghPath, gobin
	}

	out, code, calls, ghPath, gobin := run(2)
	if code != 0 {
		t.Fatalf("install step with 2 transient failures exited %d:\n%s", code, out)
	}
	if len(calls) != 3 {
		t.Fatalf("go called %d times, want 3 (2 failures + success): %q", len(calls), calls)
	}
	for _, c := range calls {
		if c != strings.TrimPrefix(goreleaserInstall, "go ") {
			t.Fatalf("unexpected go invocation %q", c)
		}
	}
	if !strings.Contains(out, "goreleaser fake") {
		t.Errorf("install step should run the installed goreleaser once to prove it works:\n%s", out)
	}
	if b, _ := os.ReadFile(ghPath); strings.TrimSpace(string(b)) != gobin {
		t.Errorf("GITHUB_PATH = %q, want GOBIN %q so the next step finds goreleaser", b, gobin)
	}

	out, code, calls, _, _ = run(9)
	if code == 0 {
		t.Fatalf("install step succeeded although every go install failed:\n%s", out)
	}
	if len(calls) != 5 {
		t.Errorf("go called %d times before giving up, want 5", len(calls))
	}
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// v0.1.1 R3: patrache/homebrew-tap's default (and only) branch is `formula`,
// and a hook refuses direct pushes to main. The tap job names the branch it
// checks out and pushes to, so a later change of the default branch cannot
// send the formula commit anywhere else.
func TestReleaseTapTargetsFormulaBranch(t *testing.T) {
	steps := readReleaseWorkflow(t)["homebrew-tap"].Steps
	co := findStep(steps, func(s wfStep) bool { return s.With["repository"] == "patrache/homebrew-tap" })
	if co < 0 {
		t.Fatal("release.yml: homebrew-tap job does not check out patrache/homebrew-tap")
	}
	if got := steps[co].With["ref"]; got != "formula" {
		t.Errorf("release.yml: tap checkout ref = %q, want formula", got)
	}
	push := findStep(steps, func(s wfStep) bool { return strings.Contains(s.Run, "git -C tap push") })
	if push < 0 {
		t.Fatal("release.yml: homebrew-tap job never pushes")
	}
	for _, line := range strings.Split(steps[push].Run, "\n") {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, "git -C tap push") && l != "git -C tap push origin HEAD:formula" {
			t.Errorf("release.yml: tap push must be `git -C tap push origin HEAD:formula`, got %q", l)
		}
	}
}
