package lazygit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/theme"
)

func modeOf(s string) termhost.Mode { return termhost.Mode(s) }

func TestArgsUserFirst(t *testing.T) {
	repo := native("/r/masil")
	got := Args(repo, []string{"/a.yml", "/b.yml"}, "/c/lazygit/theme-x.yml")
	want := []string{"-p", repo, "--use-config-file", "/a.yml,/b.yml,/c/lazygit/theme-x.yml"}
	if !slices.Equal(got, want) {
		t.Fatalf("Args = %q, want %q", got, want)
	}
	if got := Args("/r/masil", nil, "/o.yml"); got[3] != "/o.yml" {
		t.Fatalf("no user files: %q", got)
	}
	if got := Args("/r/masil", []string{"/a.yml"}, ""); len(got) != 2 {
		t.Fatalf("no overlay: %q", got)
	}
}

func TestArgsAbsoluteRepo(t *testing.T) {
	got := Args("-weird dir/한글", nil, "")
	if len(got) != 2 || got[0] != "-p" {
		t.Fatalf("Args = %q", got)
	}
	if !filepath.IsAbs(got[1]) || strings.HasPrefix(got[1], "-") || !strings.HasSuffix(got[1], filepath.Join("-weird dir", "한글")) {
		t.Fatalf("repo arg %q", got[1])
	}
}

func paneSetup(t *testing.T) (Env, string) {
	t.Helper()
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte("keybinding: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{out: map[string]string{"/bin/lg -cd": cfgDir}}
	return envWith(r, nil), filepath.Join(cfgDir, "config.yml")
}

func TestLaunchArgsPaneOverlay(t *testing.T) {
	env, user := paneSetup(t)
	got := LaunchArgs(context.Background(), env, "/bin/lg", "/r/masil", "pane", true, theme.Get(theme.Default), t.TempDir())
	if len(got) != 4 || got[2] != "--use-config-file" {
		t.Fatalf("args = %q", got)
	}
	parts := strings.Split(got[3], ",")
	if len(parts) != 2 || parts[0] != user || !strings.Contains(parts[1], "theme-") {
		t.Fatalf("config files = %q", parts)
	}
	// Overlay off.
	if got := LaunchArgs(context.Background(), env, "/bin/lg", "/r/masil", "pane", false, theme.Get(theme.Default), t.TempDir()); len(got) != 2 {
		t.Fatalf("overlay off: %q", got)
	}
}

func TestLaunchArgsExecNoOverlay(t *testing.T) {
	env, _ := paneSetup(t)
	for _, m := range []string{"exec", "window", "auto", ""} {
		got := LaunchArgs(context.Background(), env, "/bin/lg", "/r/masil", modeOf(m), true, theme.Get(theme.Default), t.TempDir())
		if len(got) != 2 || got[0] != "-p" {
			t.Errorf("mode %q: args = %q", m, got)
		}
	}
}

func TestLaunchArgsNoUserConfigNoOverlay(t *testing.T) {
	r := &fakeRunner{fail: map[string]error{"/bin/lg -cd": errors.New("boom")}}
	got := LaunchArgs(context.Background(), envWith(r, nil), "/bin/lg", "/r/masil", "pane", true, theme.Get(theme.Default), t.TempDir())
	if slices.Contains(got, "--use-config-file") {
		t.Fatalf("args = %q", got)
	}
	// A config path holding a comma cannot be passed in the list: no overlay.
	got = LaunchArgs(context.Background(), envWith(&fakeRunner{}, map[string]string{"LG_CONFIG_FILE": "/odd,dir/x.yml"}), "/bin/lg", "/r/masil", "pane", true, theme.Get(theme.Default), t.TempDir()+"/a,b")
	if slices.Contains(got, "--use-config-file") {
		t.Fatalf("comma cache dir: args = %q", got)
	}
	// The user's config folder (lazygit -cd) holding a comma: lazygit would
	// split its config.yml in two and fail on the missing halves — no overlay.
	odd := filepath.Join(t.TempDir(), "conf,ig")
	if err := os.Mkdir(odd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(odd, "config.yml"), []byte("keybinding: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r = &fakeRunner{out: map[string]string{"/bin/lg -cd": odd}}
	got = LaunchArgs(context.Background(), envWith(r, nil), "/bin/lg", "/r/masil", "pane", true, theme.Get(theme.Default), t.TempDir())
	if slices.Contains(got, "--use-config-file") {
		t.Fatalf("comma config dir: args = %q", got)
	}
}
