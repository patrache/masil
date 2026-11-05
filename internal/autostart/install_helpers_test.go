package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// ignoredDir is an absolute Paths.Dir on every OS; Install must use it only when MASIL_CONFIG_DIR is set.
var ignoredDir = filepath.Join(os.TempDir(), "masil-cfg-ignored")

// writeExe creates an empty executable file in a temp dir and returns its absolute path.
func writeExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "masil")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// fakeEnv: temp HOME, uid 501, and a recording runner. Lines starting with a prefix in okPrefixes
// succeed with the mapped output; everything else fails like a not-loaded launchd job.
// systemctl is reported as present on PATH and the bootstrap retry delay is zeroed.
func fakeEnv(t *testing.T, okPrefixes map[string]string) (home string, calls *[]string) {
	t.Helper()
	prevHome, prevUID, prevRun, prevLook, prevDelay := homeDir, getuid, run, lookPath, bootstrapRetryDelay
	t.Cleanup(func() {
		homeDir, getuid, run, lookPath, bootstrapRetryDelay = prevHome, prevUID, prevRun, prevLook, prevDelay
	})
	home = t.TempDir()
	homeDir = func() (string, error) { return home, nil }
	getuid = func() int { return 501 }
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	bootstrapRetryDelay = 0
	var got []string
	run = func(name string, args ...string) ([]byte, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		got = append(got, line)
		for p, out := range okPrefixes {
			if strings.HasPrefix(line, p) {
				return []byte(out), nil
			}
		}
		return []byte("Boot-out failed: 3: No such process"), errors.New("exit status 3")
	}
	return home, &got
}

func TestInstallRejectsBadExe(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	fakeEnv(t, map[string]string{"launchctl bootstrap ": "", "systemctl ": ""})
	missing := filepath.Join(t.TempDir(), "nope", "masil")
	for name, exe := range map[string]string{
		"relative": "bin/masil",
		"missing":  missing,
		"dir":      t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := launchdInstall(exe, config.Paths{Dir: ignoredDir}); err == nil {
				t.Fatal("launchd: want error")
			}
			if err := systemdInstall(exe, config.Paths{Dir: ignoredDir}); err == nil {
				t.Fatal("systemd: want error")
			}
			if err := runKeyInstall(mapRunKey{}, exe); err == nil {
				t.Fatal("run key: want error")
			}
		})
	}
}

func TestConfigDirOnlyWhenEnvSet(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	if got, err := configDirFor(config.Paths{Dir: ignoredDir}); err != nil || got != "" {
		t.Fatalf("env unset: %q, %v", got, err)
	}
	dir := t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", dir)
	if got, err := configDirFor(config.Paths{Dir: ignoredDir}); err != nil || got != ignoredDir {
		t.Fatalf("env set, absolute Paths.Dir: %q, %v", got, err)
	}
	got, err := configDirFor(config.Paths{Dir: filepath.Join("rel", "masil")})
	if err != nil || !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join("rel", "masil")) {
		t.Fatalf("relative Paths.Dir must become absolute: %q, %v", got, err)
	}
}
