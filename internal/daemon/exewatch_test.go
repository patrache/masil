package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExeWatch(t *testing.T) {
	if newExeWatch("") != nil {
		t.Fatal(`"" must disable watching`)
	}
	if newExeWatch(filepath.Join(t.TempDir(), "missing")) != nil {
		t.Fatal("unstattable path must disable watching")
	}
	exe := writeTempExe(t, "v1")
	w := newExeWatch(exe)
	if w.changed() {
		t.Fatal("unchanged file reported as changed")
	}
	os.Remove(exe)
	if w.changed() {
		t.Fatal("missing file (mid-upgrade) must not trigger")
	}
	replaceExe(t, exe, "v2-longer")
	if w.changed() {
		t.Fatal("first sighting must only arm (settle check)")
	}
	if !w.changed() {
		t.Fatal("second identical sighting must trigger")
	}
	if runtime.GOOS != "windows" { // brew-style symlink swap: path is a link, target changes
		dir := t.TempDir()
		a, b, link := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "masil")
		os.WriteFile(a, []byte("a"), 0o755)
		os.WriteFile(b, []byte("bb"), 0o755)
		os.Symlink(a, link)
		lw := newExeWatch(link)
		os.Remove(link)
		os.Symlink(b, link)
		lw.changed()
		if !lw.changed() {
			t.Fatal("symlink retarget not detected")
		}
	}
}

// A nil watch (Windows, or no executable path) never reports a change.
func TestExeWatchNil(t *testing.T) {
	var w *exeWatch
	if w.changed() {
		t.Fatal("nil watch reported a change")
	}
}

func TestWatchedExecutable(t *testing.T) {
	got := watchedExecutable()
	if runtime.GOOS == "windows" {
		if got != "" {
			t.Fatalf("windows must disable watching, got %q", got)
		}
		return
	}
	if got == "" || !filepath.IsAbs(got) {
		t.Fatalf("watchedExecutable() = %q, want an absolute path", got)
	}
}
