package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Review Focus 2 (plan 06): the release binary must embed an https default server.
// goreleaser builds with -trimpath, and with -trimpath the go command leaves
// -ldflags out of the embedded build info (go.dev/issue/52372), so
// `go version -m <bin> | grep DefaultServer=` can never match. The check lives
// in scripts/check-release-binary.sh instead; this test builds cmd/masil the way
// .goreleaser.yaml does and runs that script against it.
func TestReleaseBinaryCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/masil; skipped with -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("scripts/check-release-binary.sh is a POSIX sh script")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not on PATH")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not on PATH")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "check-release-binary.sh"))
	if err != nil {
		t.Fatal(err)
	}

	const server = "https://masil-release-check.invalid"
	bin := filepath.Join(t.TempDir(), "masil")
	build := exec.Command(goBin, "build", "-o", bin, "-trimpath",
		"-ldflags", "-s -w -X github.com/patrache/masil/internal/config.DefaultServer="+server, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(sh, append([]string{script}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return string(out), ee.ExitCode()
			}
			t.Fatalf("run %v: %v", args, err)
		}
		return string(out), 0
	}

	if out, code := run(bin, server); code != 0 {
		t.Fatalf("embedded https server rejected (exit %d):\n%s", code, out)
	} else if !strings.Contains(out, server) {
		t.Fatalf("ok output should name the server:\n%s", out)
	}
	if out, code := run(bin, "https://not-embedded.invalid"); code != 1 {
		t.Fatalf("server that is not embedded => exit %d, want 1:\n%s", code, out)
	}
	if out, code := run(bin, "http://localhost:8787"); code != 1 || !strings.Contains(out, "https://") {
		t.Fatalf("non-https expected server => exit %d, want 1 with an https hint:\n%s", code, out)
	}
	if out, code := run(bin, ""); code != 1 {
		t.Fatalf("empty expected server => exit %d, want 1:\n%s", code, out)
	}
	if out, code := run(filepath.Join(t.TempDir(), "missing"), server); code != 1 {
		t.Fatalf("missing binary => exit %d, want 1:\n%s", code, out)
	}
	if out, code := run(bin); code != 2 {
		t.Fatalf("wrong arg count => exit %d, want 2:\n%s", code, out)
	}
}
