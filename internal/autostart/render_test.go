package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

func TestLaunchdPlist(t *testing.T) {
	for name, c := range map[string]struct{ exe, dir string }{
		"default":             {"/opt/homebrew/bin/masil", ""},
		"config_dir":          {"/Users/me/.local/bin/masil", "/Users/me/.config/masil-work"},
		"spaces_and_specials": {"/Users/me/My Apps & Tools/<masil>", "/Users/me/설정 폴더"},
	} {
		t.Run(name, func(t *testing.T) {
			out := launchdPlist(c.exe, c.dir)
			golden.RequireEqual(t, out)
			if got := plistExe(out); got != c.exe {
				t.Fatalf("plistExe round-trip = %q", got)
			}
		})
	}
}

func TestSystemdUnit(t *testing.T) {
	for name, c := range map[string]struct{ exe, dir string }{
		"default":             {"/home/me/.local/bin/masil", ""},
		"config_dir":          {"/home/me/.local/bin/masil", "/home/me/.config/masil-work"},
		"spaces_and_specials": {`/home/me/my apps/100% "masil"\bin/masil`, "/home/me/설정 폴더"},
	} {
		t.Run(name, func(t *testing.T) {
			out := systemdUnit(c.exe, c.dir)
			golden.RequireEqual(t, out)
			if got := unitExe(out); got != c.exe {
				t.Fatalf("unitExe round-trip = %q", got)
			}
		})
	}
}

func TestRunValue(t *testing.T) {
	for name, exe := range map[string]string{
		"default":             `C:\Users\me\AppData\Local\Programs\masil\masil.exe`,
		"spaces_and_specials": `C:\Users\Kim Min\AppData\Local\Programs\masil & co\masil.exe`,
	} {
		t.Run(name, func(t *testing.T) {
			v := runValue(exe)
			golden.RequireEqual(t, v)
			if got := runValueExe(v); got != exe {
				t.Fatalf("runValueExe round-trip = %q", got)
			}
		})
	}
}

// TestUnitExeDollar covers a '$' in the path: systemd expands $VAR in ExecStart, so the
// renderer must double it and the parser must undo that.
func TestUnitExeDollar(t *testing.T) {
	exe := "/home/me/$HOME/masil"
	out := systemdUnit(exe, "")
	if !strings.Contains(string(out), `ExecStart="/home/me/$$HOME/masil" daemon run`) {
		t.Fatalf("ExecStart not escaped:\n%s", out)
	}
	if got := unitExe(out); got != exe {
		t.Fatalf("unitExe round-trip = %q", got)
	}
}

func TestParsersAbsent(t *testing.T) {
	if got := plistExe([]byte("<plist><dict></dict></plist>")); got != "" {
		t.Fatalf("plistExe = %q", got)
	}
	if got := unitExe([]byte("[Service]\nType=simple\n")); got != "" {
		t.Fatalf("unitExe = %q", got)
	}
	if got := runValueExe(""); got != "" {
		t.Fatalf("runValueExe = %q", got)
	}
}

func TestStableExecutable(t *testing.T) {
	if got, _ := StableExecutable("/usr/local/bin/masil"); got != "/usr/local/bin/masil" {
		t.Fatal(got)
	}
	_, err := StableExecutable("/var/folders/x/T/go-build1234/b001/exe/masil")
	if err == nil || !strings.Contains(err.Error(), "go run") {
		t.Fatalf("go-build: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	prefix := t.TempDir()
	cellar := filepath.Join(prefix, "Cellar", "masil", "0.1.0", "bin", "masil")
	link := filepath.Join(prefix, "bin", "masil")
	if err := os.MkdirAll(filepath.Dir(cellar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cellar, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := StableExecutable(cellar); got != cellar {
		t.Fatalf("no link yet: %q", got)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellar, link); err != nil {
		t.Fatal(err)
	}
	if got, _ := StableExecutable(cellar); got != link {
		t.Fatalf("cellar -> %q, want %q", got, link)
	}
}
