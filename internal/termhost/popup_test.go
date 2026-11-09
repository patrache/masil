//go:build !windows

package termhost

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeTmux installs a shell script as tmuxPath: `-V` prints "tmux <version>",
// anything else writes its arguments (one per line) to a file and exits with code.
func fakeTmux(t *testing.T, version string, code int) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := filepath.Join(dir, "tmux")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-V\" ]; then echo \"tmux " + version + "\"; exit 0; fi\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + argsFile + "'; done\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// macOS scans a freshly written executable on its first run (hundreds of
	// ms), which would trip TmuxVersion's 300ms limit: run it once first.
	_ = exec.Command(script, "-V").Run()
	restore := tmuxPath
	tmuxPath = script
	t.Cleanup(func() { tmuxPath = restore })
	t.Setenv("TMUX", "/tmp/tmux-501/default,1234,0")
	return argsFile
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func waitExit(t *testing.T, ch <-chan ExitInfo) ExitInfo {
	t.Helper()
	select {
	case info := <-ch:
		return info
	case <-time.After(5 * time.Second):
		t.Fatal("popup did not report its exit")
		return ExitInfo{}
	}
}

func TestPopupArgs(t *testing.T) {
	req := Request{Program: "lazygit", Args: []string{"-p", "/r"}, Dir: "/r", Title: "lazygit"}
	cases := []struct {
		version string
		want    string
	}{
		{"3.2", "display-popup -E -d /r -w 90% -h 90% -- lazygit -p /r"},
		{"3.2a", "display-popup -E -d /r -w 90% -h 90% -- lazygit -p /r"},
		{"3.3", "display-popup -E -d /r -w 90% -h 90% -T lazygit -- lazygit -p /r"},
		{"3.3a", "display-popup -E -d /r -w 90% -h 90% -T lazygit -- lazygit -p /r"},
		{"next-3.5", "display-popup -E -d /r -w 90% -h 90% -T lazygit -- lazygit -p /r"},
	}
	for _, c := range cases {
		t.Run(c.version, func(t *testing.T) {
			args := fakeTmux(t, c.version, 7)
			ch, err := Popup(req)
			if err != nil {
				t.Fatalf("Popup: %v", err)
			}
			info := waitExit(t, ch)
			if got := strings.Join(readArgs(t, args), " "); got != c.want {
				t.Fatalf("tmux args =\n %s\nwant\n %s", got, c.want)
			}
			if info.Code != 7 || info.Mode != ModeWindow || info.Program != "lazygit" || info.Title != "lazygit" || info.Err != nil {
				t.Fatalf("ExitInfo = %+v, want code 7 window", info)
			}
		})
	}
}

func TestPopupTitleSanitizedOrOmitted(t *testing.T) {
	args := fakeTmux(t, "3.4", 0)
	ch, err := Popup(Request{Program: "nvim", Dir: "/w", Title: "a‮b"})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ch)
	if got := strings.Join(readArgs(t, args), " "); got != "display-popup -E -d /w -w 90% -h 90% -T ab -- env nvim" {
		t.Fatalf("args = %s", got)
	}

	args = fakeTmux(t, "3.4", 0)
	ch, err = Popup(Request{Program: "nvim"})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ch)
	if got := strings.Join(readArgs(t, args), " "); got != "display-popup -E -w 90% -h 90% -- env nvim" {
		t.Fatalf("args without dir/title = %s", got)
	}
}

// TestPopupEscapesFormats: tmux expands -d and -T as formats ("#(cmd)" runs a
// shell command), so every '#' is doubled.
func TestPopupEscapesFormats(t *testing.T) {
	args := fakeTmux(t, "3.4", 0)
	ch, err := Popup(Request{Program: "lazygit", Args: []string{"-p", "/c/C#/app"}, Dir: "/c/C#/app#(touch x)", Title: "#(touch y) #S"})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ch)
	got := readArgs(t, args)
	want := []string{"display-popup", "-E", "-d", "/c/C##/app##(touch x)", "-w", "90%", "-h", "90%",
		"-T", "##(touch y) ##S", "--", "lazygit", "-p", "/c/C#/app"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %q\nwant   %q", got, want)
	}
}

// TestPopupSingleProgramStaysArgv: tmux runs a lone argument through sh -c,
// so a program without arguments or env still goes as argv (via env(1)).
func TestPopupSingleProgramStaysArgv(t *testing.T) {
	args := fakeTmux(t, "3.4", 0)
	ch, err := Popup(Request{Program: "/Users/me/Library/Application Support/x/prog"})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ch)
	got := readArgs(t, args)
	want := []string{"display-popup", "-E", "-w", "90%", "-h", "90%", "--", "env", "/Users/me/Library/Application Support/x/prog"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %q\nwant   %q", got, want)
	}
}

func TestPopupEnv(t *testing.T) {
	args := fakeTmux(t, "3.2", 0)
	ch, err := Popup(Request{Program: "lazygit", Dir: "/r", Env: []string{"LG_CONFIG_FILE=/x/y.yml"}})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, ch)
	if got := strings.Join(readArgs(t, args), " "); got != "display-popup -E -d /r -w 90% -h 90% -- env LG_CONFIG_FILE=/x/y.yml lazygit" {
		t.Fatalf("args = %s", got)
	}
}

func TestPopupNoTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if _, err := Popup(Request{Program: "lazygit"}); !errors.Is(err, ErrNoTmux) {
		t.Fatalf("err = %v, want ErrNoTmux", err)
	}
}

func TestTmuxVersion(t *testing.T) {
	fakeTmux(t, "3.3a", 0)
	if got := TmuxVersion(); got != "3.3a" {
		t.Fatalf("TmuxVersion() = %q, want 3.3a", got)
	}
	if got := detectTmuxVersion(); got != "3.3a" {
		t.Fatalf("detectTmuxVersion() = %q, want TmuxVersion", got)
	}
	tmuxPath = filepath.Join(t.TempDir(), "missing")
	if got := TmuxVersion(); got != "" {
		t.Fatalf("missing tmux = %q, want empty", got)
	}
}

func TestTmuxVersionTimeout(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restore := tmuxPath
	tmuxPath = script
	t.Cleanup(func() { tmuxPath = restore })
	start := time.Now()
	if got := TmuxVersion(); got != "" {
		t.Fatalf("hung tmux = %q, want empty", got)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("TmuxVersion took %v, want ~300ms", d)
	}
}
