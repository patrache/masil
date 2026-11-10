package lazygit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/patrache/masil/internal/theme"
)

func envWith(r *fakeRunner, vars map[string]string) Env {
	e := fakeEnv(r, nil)
	e.Getenv = func(k string) string { return vars[k] }
	return e
}

func TestUserConfigFilesEnvList(t *testing.T) {
	r := &fakeRunner{}
	files, ok := UserConfigFiles(context.Background(), envWith(r, map[string]string{"LG_CONFIG_FILE": "/a.yml,,/b.yml"}), "/bin/lg")
	if !ok || !slices.Equal(files, []string{"/a.yml", "/b.yml"}) {
		t.Fatalf("files = %q, ok = %v", files, ok)
	}
	if len(r.calls) != 0 {
		t.Errorf("ran %q with LG_CONFIG_FILE set", r.calls)
	}
}

func TestUserConfigFilesCd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("gui: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{out: map[string]string{"/bin/lg -cd": dir + "\n"}}
	files, ok := UserConfigFiles(context.Background(), envWith(r, nil), "/bin/lg")
	if !ok || !slices.Equal(files, []string{filepath.Join(dir, "config.yml")}) {
		t.Fatalf("files = %q, ok = %v", files, ok)
	}
}

func TestUserConfigFilesCdMissingFile(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"/bin/lg -cd": t.TempDir() + "\n"}}
	files, ok := UserConfigFiles(context.Background(), envWith(r, nil), "/bin/lg")
	if !ok || len(files) != 0 {
		t.Fatalf("files = %q, ok = %v; want [] ok", files, ok)
	}
}

func TestUserConfigFilesCdFails(t *testing.T) {
	r := &fakeRunner{fail: map[string]error{"/bin/lg -cd": errors.New("boom")}}
	if _, ok := UserConfigFiles(context.Background(), envWith(r, nil), "/bin/lg"); ok {
		t.Fatal("ok = true on -cd failure")
	}
	r = &fakeRunner{out: map[string]string{"/bin/lg -cd": "\n"}}
	if _, ok := UserConfigFiles(context.Background(), envWith(r, nil), "/bin/lg"); ok {
		t.Fatal("ok = true on empty -cd output")
	}
}

var hexRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func TestOverlayOnlyColors(t *testing.T) {
	cache := t.TempDir()
	seen := map[string]bool{}
	for _, th := range theme.All() {
		c := OverlayColorsFrom(th)
		for _, v := range []string{c.ActiveBorder, c.InactiveBorder, c.SelectedLineBg, c.OptionsText} {
			if !hexRe.MatchString(v) {
				t.Fatalf("%s: color %q", th.Name, v)
			}
		}
		p, err := WriteOverlay(cache, c)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(p) != filepath.Join(cache, "lazygit") || !strings.HasPrefix(filepath.Base(p), "theme-") || filepath.Ext(p) != ".yml" {
			t.Fatalf("path %q", p)
		}
		seen[p] = true
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]map[string]map[string][]string
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: yaml: %v\n%s", th.Name, err, b)
		}
		if len(doc) != 1 || len(doc["gui"]) != 1 {
			t.Fatalf("%s: top keys %v", th.Name, doc)
		}
		tm := doc["gui"]["theme"]
		keys := []string{}
		for k := range tm {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		want := []string{"activeBorderColor", "inactiveBorderColor", "optionsTextColor", "selectedLineBgColor"}
		if !slices.Equal(keys, want) {
			t.Fatalf("%s: theme keys %v", th.Name, keys)
		}
		if got := tm["activeBorderColor"]; len(got) != 1 || got[0] != c.ActiveBorder {
			t.Errorf("%s: activeBorderColor %v", th.Name, got)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
			t.Errorf("%s: perm %v %v", th.Name, fi.Mode(), err)
		}
		// Same colors: same file, rewritten idempotently.
		if p2, err := WriteOverlay(cache, c); err != nil || p2 != p {
			t.Errorf("rewrite: %q %v", p2, err)
		}
	}
	if len(seen) != len(theme.All()) {
		t.Errorf("themes share overlay files: %v", seen)
	}
	if _, err := WriteOverlay(cache, OverlayColors{ActiveBorder: "#fff\nos:", InactiveBorder: "#000000", SelectedLineBg: "#000000", OptionsText: "#000000"}); err == nil {
		t.Error("bad color accepted")
	}
}
