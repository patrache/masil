package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v0.1.1 R2: the install-smoke job met GitHub release downloads that answered
// 504 for a while (2026-11-08). install.sh retries network downloads (curl
// --retry 5, plus --retry-all-errors where curl supports it; a loop for wget,
// since busybox wget has no --tries) and its failure message says the cause
// may be an HTTP error, not only "private repo or missing version".
func TestInstallShRetriesDownloads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX sh script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not on PATH")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dist := fakeRelease(t, "masil_linux_amd64.tar.gz")

	type result struct {
		out, log, sleeps string
		code             int
		installed        bool
	}
	run := func(fetcher string, base string, env ...string) result {
		t.Helper()
		dir := t.TempDir()
		bin := sandboxPath(t, filepath.Join(dir, "path"))
		switch fetcher {
		case "curl":
			writeExe(t, filepath.Join(bin, "curl"), fakeCurl)
		case "wget":
			writeExe(t, filepath.Join(bin, "wget"), fakeWget)
		}
		writeExe(t, filepath.Join(bin, "sleep"), "#!/bin/sh\necho \"sleep $*\" >> \"$FAKE_SLEEPS\"\n")
		log, sleeps := filepath.Join(dir, "fetch.log"), filepath.Join(dir, "sleeps")
		for _, f := range []string{log, sleeps} {
			if err := os.WriteFile(f, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(dir, "inst")
		cmd := exec.Command(sh, script)
		cmd.Env = append([]string{
			"PATH=" + bin, "HOME=" + dir, "TMPDIR=" + dir,
			"MASIL_OS=Linux", "MASIL_ARCH=x86_64",
			"MASIL_DOWNLOAD_BASE=" + base, "MASIL_INSTALL_DIR=" + target,
			"FAKE_DIST=" + dist, "FAKE_LOG=" + log, "FAKE_SLEEPS=" + sleeps,
		}, env...)
		out, err := cmd.CombinedOutput()
		r := result{out: string(out)}
		if ee, ok := err.(*exec.ExitError); ok {
			r.code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		lb, _ := os.ReadFile(log)
		sb, _ := os.ReadFile(sleeps)
		r.log, r.sleeps = string(lb), string(sb)
		_, err = os.Stat(filepath.Join(target, "masil"))
		r.installed = err == nil
		return r
	}
	lines := func(s string) []string {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	const base = "https://dl.invalid/v9.9.9"

	t.Run("curl retries with --retry-all-errors", func(t *testing.T) {
		r := run("curl", base)
		if r.code != 0 || !r.installed {
			t.Fatalf("exit %d, installed %v:\n%s", r.code, r.installed, r.out)
		}
		calls := lines(r.log)
		if len(calls) != 2 {
			t.Fatalf("curl calls = %q, want archive + checksums", calls)
		}
		for _, c := range calls {
			if !strings.Contains(c, "--retry 5") || !strings.Contains(c, "--retry-all-errors") {
				t.Errorf("curl call without --retry 5 --retry-all-errors: %q", c)
			}
		}
	})

	t.Run("old curl without --retry-all-errors still retries", func(t *testing.T) {
		r := run("curl", base, "FAKE_OLD_CURL=1")
		if r.code != 0 || !r.installed {
			t.Fatalf("curl 7.68 (no --retry-all-errors) => exit %d:\n%s", r.code, r.out)
		}
		for _, c := range lines(r.log) {
			if !strings.Contains(c, "--retry 5") || strings.Contains(c, "--retry-all-errors") {
				t.Errorf("old curl call = %q, want --retry 5 only", c)
			}
		}
	})

	t.Run("local file:// base is not retried", func(t *testing.T) {
		r := run("curl", "file://"+dist)
		if r.code != 0 || !r.installed {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
		for _, c := range lines(r.log) {
			if strings.Contains(c, "--retry") {
				t.Errorf("file:// download should not retry: %q", c)
			}
		}
	})

	// P5 Task 22: the messages are Korean when the locale starts with ko,
	// English otherwise.
	for _, lc := range []struct {
		lang, failed, http, later string
	}{
		{"LANG=ko_KR.UTF-8", "내려받기 실패: ", "HTTP 오류", "잠시 뒤 다시"},
		{"LANG=en_US.UTF-8", "Download failed: ", "HTTP error", "try again in a moment"},
	} {
		t.Run("curl HTTP error names the HTTP cause/"+lc.lang, func(t *testing.T) {
			r := run("curl", base, "FAKE_HTTP_ERROR=504", lc.lang)
			if r.code != 1 || r.installed {
				t.Fatalf("HTTP 504 => exit %d, installed %v, want 1:\n%s", r.code, r.installed, r.out)
			}
			for _, want := range []string{
				lc.failed + base + "/masil_linux_amd64.tar.gz",
				"returned error: 504", // curl's own line is shown, not sent to /dev/null
				lc.http,
				"5xx",
				lc.later,
			} {
				if !strings.Contains(r.out, want) {
					t.Errorf("failure output lacks %q:\n%s", want, r.out)
				}
			}
		})
	}

	t.Run("wget retries transient failures", func(t *testing.T) {
		r := run("wget", base, "FAKE_WGET_FAILURES=2")
		if r.code != 0 || !r.installed {
			t.Fatalf("exit %d:\n%s", r.code, r.out)
		}
		if n := len(lines(r.log)); n != 4 {
			t.Errorf("wget calls = %d, want 2 failures + archive + checksums = 4:\n%s", n, r.log)
		}
		if got := lines(r.sleeps); len(got) != 2 {
			t.Errorf("sleeps = %q, want two waits between three tries", got)
		}
	})

	t.Run("wget gives up after 5 tries", func(t *testing.T) {
		r := run("wget", base, "FAKE_WGET_FAILURES=99", "LANG=ko_KR.UTF-8")
		if r.code != 1 || r.installed {
			t.Fatalf("exit %d, installed %v, want 1:\n%s", r.code, r.installed, r.out)
		}
		if n := len(lines(r.log)); n != 5 {
			t.Errorf("wget calls = %d, want 5", n)
		}
		if !strings.Contains(r.out, "HTTP 오류") {
			t.Errorf("failure output lacks the HTTP hint:\n%s", r.out)
		}
	})
}

// P5 Task 22: outside a Korean locale install.sh speaks English only — on
// success, on failure and in the next-step hints. MASIL_INSTALL_LANG=ko|en
// wins over the locale; LC_ALL wins over LANG.
func TestInstallShEnglishNoHangul(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX sh script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not on PATH")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dist := fakeRelease(t, "masil_linux_amd64.tar.gz")
	run := func(env ...string) string {
		t.Helper()
		dir := t.TempDir()
		bin := sandboxPath(t, filepath.Join(dir, "path"))
		writeExe(t, filepath.Join(bin, "curl"), fakeCurl)
		log := filepath.Join(dir, "fetch.log")
		if err := os.WriteFile(log, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(sh, script)
		cmd.Env = append([]string{
			"PATH=" + bin, "HOME=" + dir, "TMPDIR=" + dir,
			"MASIL_OS=Linux", "MASIL_ARCH=x86_64",
			"MASIL_DOWNLOAD_BASE=file://" + dist, "MASIL_INSTALL_DIR=" + filepath.Join(dir, "inst"),
			"FAKE_DIST=" + dist, "FAKE_LOG=" + log,
		}, env...)
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	hangul := func(s string) bool {
		return strings.IndexFunc(s, func(r rune) bool { return r >= 0xAC00 && r <= 0xD7A3 }) >= 0
	}

	for _, c := range []struct {
		name string
		env  []string
		ko   bool
	}{
		{"LANG=C", []string{"LANG=C"}, false},
		{"no locale", nil, false},
		{"LANG=en_US", []string{"LANG=en_US.UTF-8"}, false},
		{"LANG=ko_KR", []string{"LANG=ko_KR.UTF-8"}, true},
		{"LC_ALL beats LANG (en)", []string{"LC_ALL=en_US.UTF-8", "LANG=ko_KR.UTF-8"}, false},
		{"LC_ALL beats LANG (ko)", []string{"LC_ALL=ko_KR.UTF-8", "LANG=C"}, true},
		{"MASIL_INSTALL_LANG=en", []string{"MASIL_INSTALL_LANG=en", "LANG=ko_KR.UTF-8"}, false},
		{"MASIL_INSTALL_LANG=ko", []string{"MASIL_INSTALL_LANG=ko", "LANG=C"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, extra := range [][]string{nil, {"FAKE_HTTP_ERROR=404"}, {"MASIL_OS=Plan9"}} {
				out := run(append(append([]string{}, c.env...), extra...)...)
				if hangul(out) != c.ko {
					t.Errorf("%v: Korean output = %v, want %v:\n%s", extra, hangul(out), c.ko, out)
				}
				if strings.TrimSpace(out) == "" {
					t.Errorf("%v: no output", extra)
				}
			}
		})
	}
}

// P5 Task 22: every user-facing message of the install scripts is one line
// holding the Korean and the English text together (say_t/die_t/t in
// install.sh, T/Say-T/Fail-T in install.ps1 — and `if ($masilKo) {…}` for the
// last line outside its script block), so neither language can drift.
func TestInstallScriptMessagesPaired(t *testing.T) {
	hangul := func(s string) bool {
		return strings.IndexFunc(s, func(r rune) bool { return r >= 0xAC00 && r <= 0xD7A3 }) >= 0
	}
	for _, c := range []struct {
		file, comment string
		helpers       []string
	}{
		{"install.sh", "#", []string{"say_t ", "die_t ", "$(t "}},
		{"install.ps1", "#", []string{"(T ", "Say-T ", "Fail-T ", "if ($masilKo) {"}},
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "scripts", c.file))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for i, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			l := strings.TrimSpace(line)
			if !hangul(l) || strings.HasPrefix(l, c.comment) {
				continue
			}
			n++
			paired := false
			for _, h := range c.helpers {
				if strings.Contains(l, h) {
					paired = true
				}
			}
			if !paired {
				t.Errorf("%s:%d: Korean text outside a two-language message: %s", c.file, i+1, l)
			}
		}
		if n == 0 {
			t.Errorf("%s: no Korean messages found", c.file)
		}
	}
}

// install.ps1 cannot run here (no pwsh locally; plan 06 Task 10 says not to
// install it), and release.yml's install-smoke job runs it on windows-latest.
// This pins the retry loop and the message so the change cannot silently go.
func TestInstallPs1RetriesDownloads(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	i := strings.Index(s, "\tfunction Save-Url(")
	if i < 0 {
		t.Fatal("install.ps1: no Save-Url function")
	}
	fn := s[i:]
	if j := strings.Index(fn[1:], "\n\tfunction "); j >= 0 {
		fn = fn[:j+1]
	}
	for _, want := range []string{
		"for ($attempt = 1; $attempt -le 5; $attempt++)",
		"Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing",
		"Start-Sleep -Seconds",
		"$_.Exception.Message",
		"HTTP 오류",
		"5xx",
		"잠시 뒤 다시",
		// P5 Task 22: the same hint in English
		"HTTP error",
		"try again in a moment",
	} {
		if !strings.Contains(fn, want) {
			t.Errorf("install.ps1 Save-Url lacks %q:\n%s", want, fn)
		}
	}
	// P5 Task 22: Korean when MASIL_INSTALL_LANG=ko or (unset) the Windows UI
	// culture is ko-*; English otherwise.
	for _, want := range []string{"MASIL_INSTALL_LANG", "(Get-UICulture).Name -like 'ko*'"} {
		if !strings.Contains(s, want) {
			t.Errorf("install.ps1 lacks %q", want)
		}
	}
	// A file:// source is a local copy: it is not retried.
	if f, l := strings.Index(fn, "$uri.IsFile"), strings.Index(fn, "for ($attempt"); f < 0 || l < 0 || f > l {
		t.Errorf("install.ps1 Save-Url: handle file:// before the retry loop:\n%s", fn)
	}
}

const fakeCurl = `#!/bin/sh
for a in "$@"; do
	if [ "$a" = "--version" ]; then
		if [ -n "${FAKE_OLD_CURL:-}" ]; then
			for b in "$@"; do
				if [ "$b" = "--retry-all-errors" ]; then
					echo "curl: option --retry-all-errors: is unknown" >&2
					exit 2
				fi
			done
		fi
		echo "curl 7.68.0 (fake)"
		exit 0
	fi
done
echo "$*" >> "$FAKE_LOG"
out=
url=
while [ $# -gt 0 ]; do
	case "$1" in
		-o) out=$2; shift 2 ;;
		*) url=$1; shift ;;
	esac
done
if [ -n "${FAKE_HTTP_ERROR:-}" ]; then
	echo "curl: (22) The requested URL returned error: $FAKE_HTTP_ERROR" >&2
	exit 22
fi
cp "$FAKE_DIST/${url##*/}" "$out"
`

const fakeWget = `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
n=$(awk 'END { print NR }' "$FAKE_LOG")
if [ "$n" -le "${FAKE_WGET_FAILURES:-0}" ]; then
	exit 8
fi
[ "$1" = "-qO" ] || { echo "fake wget: want -qO DEST URL, got $*" >&2; exit 2; }
cp "$FAKE_DIST/${3##*/}" "$2"
`

// sandboxPath makes dir hold only the tools install.sh needs besides curl/wget,
// so a test decides which downloader exists.
func sandboxPath(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mktemp", "rm", "awk", "sha256sum", "shasum", "mkdir", "tar", "gzip", "install", "mv", "cp", "cat"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fakeRelease writes <archive> (holding a masil stub) and checksums.txt.
func fakeRelease(t *testing.T, archive string) string {
	t.Helper()
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho masil 9.9.9\n")
	if err := tw.WriteHeader(&tar.Header{Name: "masil", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, archive), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	sums := hex.EncodeToString(sum[:]) + "  " + archive + "\n"
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
