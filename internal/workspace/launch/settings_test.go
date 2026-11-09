package launch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestPrepareSettings(t *testing.T) {
	tmp := t.TempDir()
	url := "http://127.0.0.1:5555/h/tok/t1"
	argv := []string{"claude", "--permission-mode", "plan"}
	out, cleanup, err := Prepare(argv, url, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(argv)+2 || out[len(out)-2] != "--settings" || !slices.Equal(out[:len(argv)], argv) {
		t.Fatalf("out = %v", out)
	}
	p := out[len(out)-1]
	if filepath.Dir(p) != tmp {
		t.Errorf("settings file %q not in tmpDir", p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(p)
	var doc struct {
		Disable string `json:"disableBypassPermissionsMode"`
		Hooks   map[string][]struct {
			Hooks []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Disable != "disable" || len(doc.Hooks) != 3 {
		t.Errorf("settings = %s", data)
	}
	for _, ev := range []string{"Notification", "PermissionRequest", "Stop"} {
		hs := doc.Hooks[ev]
		if len(hs) != 1 || len(hs[0].Hooks) != 1 || hs[0].Hooks[0].Type != "http" || hs[0].Hooks[0].URL != url {
			t.Errorf("hook %s = %+v", ev, hs)
		}
	}
	if argv[len(argv)-1] != "plan" || len(argv) != 3 {
		t.Errorf("Prepare changed the caller's argv: %v", argv)
	}
	cleanup()
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cleanup left %q", p)
	}
	cleanup() // twice is harmless
}

func TestPrepareBeforeDoubleDash(t *testing.T) {
	out, cleanup, err := Prepare([]string{"claude", "--", "hello"}, "http://x", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(out) != 7 || out[1] != "--permission-mode" || out[2] != "manual" || out[3] != "--settings" || out[5] != "--" || out[6] != "hello" {
		t.Errorf("out = %v", out)
	}
}

func TestPrepareNonClaude(t *testing.T) {
	tmp := t.TempDir()
	argv := []string{"zsh", "-l"}
	out, cleanup, err := Prepare(argv, "http://x", tmp)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if !slices.Equal(out, argv) {
		t.Errorf("out = %v", out)
	}
	if des, _ := os.ReadDir(tmp); len(des) != 0 {
		t.Errorf("files created for zsh: %v", des)
	}
}
