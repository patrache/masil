package config_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/config"
)

func testPaths(t *testing.T) config.Paths {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", filepath.Join(t.TempDir(), "masil"))
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	return p
}

func writeConfig(t *testing.T, p config.Paths, body string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p.Config, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.Config, perm); err != nil { // defeat umask
		t.Fatal(err)
	}
}

func mustEqualDefaults(t *testing.T, c *config.Config) {
	t.Helper()
	if c == nil {
		t.Fatal("config is nil")
	}
	// Every exported value; Load's internal "did not load" mark is checked apart
	// (TestSaveRefusesAfterFailedLoad).
	want := config.Defaults()
	if c.Server != want.Server || c.Theme != want.Theme || c.Layout != want.Layout ||
		c.UI != want.UI || c.Notify != want.Notify || !reflect.DeepEqual(c.Keys, want.Keys) {
		t.Fatalf("config = %+v, want defaults %+v", *c, *want)
	}
}

func TestResolvePathsEnv(t *testing.T) {
	d, c := t.TempDir(), t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", d)
	t.Setenv("MASIL_CACHE_DIR", c)
	t.Setenv("XDG_CONFIG_HOME", "/nope")
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	want := config.Paths{
		Dir:             d,
		Cache:           c,
		Config:          filepath.Join(d, "config.toml"),
		Credentials:     filepath.Join(d, "credentials.json"),
		DeviceID:        filepath.Join(d, "device_id"),
		State:           filepath.Join(d, "state.json"),
		DaemonState:     filepath.Join(d, "daemon-state.json"),
		AlertsShown:     filepath.Join(d, "alerts-shown.json"),
		AlertsShownLock: filepath.Join(d, "alerts-shown.lock"),
		TUILock:         filepath.Join(d, "tui.lock"),
		DaemonLock:      filepath.Join(d, "daemon.lock"),
		LogDir:          filepath.Join(d, "logs"),
		ToolsState:      filepath.Join(d, "tools-state.json"),
		SecretsDir:      filepath.Join(d, "secrets"),
	}
	if p != want {
		t.Fatalf("paths = %+v\nwant   %+v", p, want)
	}
}

func TestResolvePathsHome(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("MASIL_CONFIG_DIR", "")
	t.Setenv("MASIL_CACHE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if want := filepath.Join(h, ".config", "masil"); p.Dir != want {
		t.Fatalf("Dir = %q, want %q", p.Dir, want)
	}
	if p.Config != filepath.Join(p.Dir, "config.toml") {
		t.Fatalf("Config = %q", p.Config)
	}
	uc, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache dir: %v", err)
	}
	if want := filepath.Join(uc, "masil"); p.Cache != want {
		t.Fatalf("Cache = %q, want %q", p.Cache, want)
	}
}

func TestDefaults(t *testing.T) {
	c := config.Defaults()
	if c == nil {
		t.Fatal("Defaults() = nil")
	}
	if c.Server != "" || c.Theme != "amber" {
		t.Fatalf("server/theme = %q/%q", c.Server, c.Theme)
	}
	wantLayout := config.LayoutConfig{Pin: "auto", CommandMinCols: 200, CommandMinRows: 50, StandardMinCols: 100,
		StandardMinRows: 40, TinyMaxCols: 43, TinyMaxRows: 15, WidgetMinRows: 24}
	if c.Layout != wantLayout {
		t.Fatalf("layout = %+v, want %+v", c.Layout, wantLayout)
	}
	wantUI := config.UIConfig{Mouse: true, Images: "auto", Animations: true, PaintBackground: true, Clock: "24h", Language: "auto",
		CodeHighlight: true, CodeComplete: true}
	if c.UI != wantUI {
		t.Fatalf("ui = %+v, want %+v", c.UI, wantUI)
	}
	if c.Notify != (config.NotifyConfig{Desktop: true}) {
		t.Fatalf("notify = %+v", c.Notify)
	}
	if c.Keys == nil || len(c.Keys) != 0 {
		t.Fatalf("keys = %#v, want empty non-nil", c.Keys)
	}
	// Each call returns an independent value.
	c.Keys["x"] = []string{"y"}
	c.Theme = "charm"
	if d := config.Defaults(); len(d.Keys) != 0 || d.Theme != "amber" {
		t.Fatal("Defaults() shares state between calls")
	}
}

func TestLoadMissingIsDefaults(t *testing.T) {
	p := testPaths(t)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mustEqualDefaults(t, c)
}

func TestLoadInvalidValuesPartial(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "theme=\"neon\"\n[layout]\npin=\"huge\"\ncommand_min_cols=-5\n[ui]\nimages=\"sixel\"\nclock=\"25h\"\nfoo=1\n[weird]\nx=1\n", 0o600)
	c, err := config.Load(p)
	if !errors.Is(err, config.ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	if c == nil {
		t.Fatal("config is nil")
	}
	if c.Theme != "amber" || c.Layout.Pin != "auto" || c.Layout.CommandMinCols != 200 || c.UI.Images != "auto" || c.UI.Clock != "24h" {
		t.Fatalf("invalid values not reset: %+v", *c)
	}
	for _, field := range []string{"theme", "layout.pin", "layout.command_min_cols", "ui.images", "ui.clock"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name %s", err, field)
		}
	}
}

func TestLoadValidValuesAccepted(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "theme=\"D\"\n[layout]\npin=\"tiny\"\ncommand_min_cols=180\n[ui]\nmouse=false\nimages=\"halfblock\"\nclock=\"12h\"\n[notify]\ndesktop=false\n[keys]\n\"palette.open\"=[\"ctrl+k\"]\n", 0o600)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Theme != "D" || c.Layout.Pin != "tiny" || c.Layout.CommandMinCols != 180 || c.UI.Mouse || c.UI.Images != "halfblock" ||
		c.UI.Clock != "12h" || c.Notify.Desktop || !reflect.DeepEqual(c.Keys, map[string][]string{"palette.open": {"ctrl+k"}}) {
		t.Fatalf("values not loaded: %+v", *c)
	}
}

func TestLoadPartialFileKeepsOtherDefaults(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "server=\"https://x.example\"\n", 0o600)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.Defaults()
	want.Server = "https://x.example"
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("config = %+v, want %+v", *c, *want)
	}
}

func TestLoadCorruptTOMLFallsBack(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "theme = [unterminated", 0o600)
	c, err := config.Load(p)
	if !errors.Is(err, config.ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	mustEqualDefaults(t, c)
}

func TestLoadFixesPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits ignored on windows")
	}
	p := testPaths(t)
	writeConfig(t, p, "theme=\"cream\"\n", 0o644)
	if _, err := config.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	st, err := os.Stat(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", st.Mode().Perm())
	}
}

func TestSaveRoundTripAndPerms(t *testing.T) {
	p := testPaths(t)
	c := config.Defaults()
	c.Server = "https://s.example"
	c.Theme = "midnight"
	c.Layout.Pin = "compact"
	c.Layout.TinyMaxRows = 12
	c.UI.Mouse = false
	c.UI.Images = "kitty"
	c.Notify.Desktop = false
	c.Keys["palette.open"] = []string{"ctrl+k", "ctrl+p"}
	if err := config.Save(p, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip = %+v, want %+v", *got, *c)
	}
	// Defaults round-trip too (empty keys table).
	if err := config.Save(p, config.Defaults()); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	got, err = config.Load(p)
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	mustEqualDefaults(t, got)

	if runtime.GOOS != "windows" {
		st, err := os.Stat(p.Config)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("file mode = %o, want 600", st.Mode().Perm())
		}
		dst, err := os.Stat(p.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if dst.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode = %o, want 700", dst.Mode().Perm())
		}
	}
	left, err := filepath.Glob(filepath.Join(p.Dir, ".config.toml.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestServerURLPrecedence(t *testing.T) {
	old := config.DefaultServer
	t.Cleanup(func() { config.DefaultServer = old })
	c := config.Defaults()
	config.DefaultServer = "http://d:1"
	t.Setenv("MASIL_SERVER", "") // empty env = unset
	if got := c.ServerURL(); got != "http://d:1" {
		t.Fatalf("default = %q", got)
	}
	config.DefaultServer = "http://d:1/"
	if got := c.ServerURL(); got != "http://d:1" {
		t.Fatalf("default trimmed = %q", got)
	}
	c.Server = "https://s/"
	if got := c.ServerURL(); got != "https://s" {
		t.Fatalf("config = %q", got)
	}
	t.Setenv("MASIL_SERVER", "https://e/")
	if got := c.ServerURL(); got != "https://e" {
		t.Fatalf("env = %q", got)
	}
}

func TestWriteFileAtomicConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	const writers, rounds, size = 8, 20, 64 * 1024
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, size)
	}
	var wg sync.WaitGroup
	errs := make(chan error, writers*rounds)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				if err := config.WriteFileAtomic(path, payloads[i], 0o600); err != nil {
					errs <- fmt.Errorf("writer %d: %w", i, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	match := false
	for _, pl := range payloads {
		if bytes.Equal(got, pl) {
			match = true
			break
		}
	}
	if !match {
		t.Fatalf("final content (%d bytes) matches no payload", len(got))
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600", st.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory has leftovers: %v", names)
	}
}

func TestWriteFileAtomicMissingDirFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no", "such", "file")
	if err := config.WriteFileAtomic(path, []byte("x"), 0o600); err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestEnsureDirFixesPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits ignored on windows")
	}
	d := filepath.Join(t.TempDir(), "masil")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MASIL_CONFIG_DIR", d)
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	st, err := os.Stat(d)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 700", st.Mode().Perm())
	}
	lst, err := os.Stat(p.LogDir)
	if err != nil || !lst.IsDir() {
		t.Fatalf("logs dir missing: %v", err)
	}
}

func TestLoadWrongTypeKeepsOtherFields(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "server=\"https://my.example\"\ntheme=\"cream\"\n[layout]\ncommand_min_cols=\"wide\"\ncommand_min_rows=60\n[ui]\nmouse=\"no\"\nclock=\"12h\"\n[notify]\ndesktop=false\n[keys]\n\"palette.open\"=[\"ctrl+k\"]\n\"bad.key\"=3\n", 0o600)
	c, err := config.Load(p)
	if !errors.Is(err, config.ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	want := config.Defaults()
	want.Server = "https://my.example"
	want.Theme = "cream"
	want.Layout.CommandMinRows = 60
	want.UI.Clock = "12h"
	want.Notify.Desktop = false
	want.Keys = map[string][]string{"palette.open": {"ctrl+k"}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("config = %+v, want %+v", *c, *want)
	}
	for _, field := range []string{"ui.mouse", "layout.command_min_cols", "keys.bad.key"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name %s", err, field)
		}
	}
	if strings.Contains(err.Error(), "server") || strings.Contains(err.Error(), "ui.clock") {
		t.Errorf("error %q names a valid field", err)
	}
}

func TestLoadWrongTypeTableKeepsOtherFields(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "server=\"https://my.example\"\nui=3\nlayout=\"x\"\n", 0o600)
	c, err := config.Load(p)
	if !errors.Is(err, config.ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	want := config.Defaults()
	want.Server = "https://my.example"
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("config = %+v, want %+v", *c, *want)
	}
	for _, field := range []string{"ui", "layout"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not name %s", err, field)
		}
	}
}

// A config.toml that did not load must never be overwritten by the defaults
// Load fell back to (one typo would erase the server URL and key overrides).
func TestSaveRefusesAfterFailedLoad(t *testing.T) {
	t.Run("syntax error", func(t *testing.T) {
		p := testPaths(t)
		body := "server = \"https://my.server.example\"\ntheme = \"cream\n[keys]\n\"palette.open\" = [\"ctrl+p\"]\n"
		writeConfig(t, p, body, 0o600)
		c, err := config.Load(p)
		if !errors.Is(err, config.ErrPartial) {
			t.Fatalf("Load err = %v, want ErrPartial", err)
		}
		c.Theme = "midnight"
		if err := config.Save(p, c); !errors.Is(err, config.ErrNotLoaded) {
			t.Fatalf("Save err = %v, want ErrNotLoaded", err)
		}
		if b, _ := os.ReadFile(p.Config); string(b) != body {
			t.Fatalf("config.toml overwritten:\n%s", b)
		}
		// A copy keeps the mark (the app shares and copies *Config).
		cp := *c
		if err := config.Save(p, &cp); !errors.Is(err, config.ErrNotLoaded) {
			t.Fatalf("Save(copy) err = %v, want ErrNotLoaded", err)
		}
	})
	t.Run("read error", func(t *testing.T) {
		p := testPaths(t)
		if err := os.Mkdir(p.Config, 0o700); err != nil { // unreadable as a file
			t.Fatal(err)
		}
		c, err := config.Load(p)
		if !errors.Is(err, config.ErrPartial) {
			t.Fatalf("Load err = %v, want ErrPartial", err)
		}
		if err := config.Save(p, c); !errors.Is(err, config.ErrNotLoaded) {
			t.Fatalf("Save err = %v, want ErrNotLoaded", err)
		}
	})
	t.Run("invalid values still save", func(t *testing.T) {
		p := testPaths(t)
		writeConfig(t, p, "server = \"https://my.server.example\"\nlayout.pin = \"sideways\"\n", 0o600)
		c, err := config.Load(p)
		if !errors.Is(err, config.ErrPartial) {
			t.Fatalf("Load err = %v, want ErrPartial", err)
		}
		if err := config.Save(p, c); err != nil {
			t.Fatalf("Save after a partial (parsed) load: %v", err)
		}
		if got, _ := config.Load(p); got.Server != "https://my.server.example" {
			t.Fatalf("server = %q", got.Server)
		}
	})
}

func TestUILanguageValidated(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "[ui]\nlanguage=\"fr\"\nclock=\"12h\"\n", 0o600)
	c, err := config.Load(p)
	if !errors.Is(err, config.ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	if c.UI.Language != "auto" || c.UI.Clock != "12h" {
		t.Fatalf("ui = %+v, want language auto, clock kept", c.UI)
	}
	if !strings.Contains(err.Error(), "ui.language") {
		t.Fatalf("error %q does not name ui.language", err)
	}
	for _, v := range []string{"auto", "ko", "en", "zh-Hans"} {
		writeConfig(t, p, "[ui]\nlanguage=\""+v+"\"\n", 0o600)
		c, err := config.Load(p)
		if err != nil || c.UI.Language != v {
			t.Fatalf("language %q: got %q, err %v", v, c.UI.Language, err)
		}
	}
}

// The code editors' switches (v0.1.3-1): on unless the file turns them off;
// an old file without them keeps them on.
func TestCodeEditorSwitches(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "[ui]\nmouse=false\n", 0o600)
	c, err := config.Load(p)
	if err != nil || !c.UI.CodeHighlight || !c.UI.CodeComplete {
		t.Fatalf("old file: %+v, %v", c.UI, err)
	}
	writeConfig(t, p, "[ui]\ncode_highlight=false\ncode_complete=false\n", 0o600)
	c, err = config.Load(p)
	if err != nil || c.UI.CodeHighlight || c.UI.CodeComplete {
		t.Fatalf("turned off: %+v, %v", c.UI, err)
	}
}
