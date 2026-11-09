package fsroot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates dir/rel (slash path) with content, making parents.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// symlink makes dir/rel point at target, skipping the test where the OS
// refuses (Windows without developer mode).
func symlink(t *testing.T, dir, target, rel string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
}

func openRoot(t *testing.T, dir string) *Root {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func reasonOf(err error) (Reason, bool) {
	var pe *PathError
	if errors.As(err, &pe) {
		return pe.Reason, true
	}
	return 0, false
}

func TestCheckRejects(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	writeFile(t, base, "outside/f", "secret")
	writeFile(t, proj, "src/a.go", "package a")
	writeFile(t, proj, ".env", "K=V")
	writeFile(t, proj, ".git/config", "[core]")
	writeFile(t, proj, "deep/x", "x")

	symlink(t, proj, "../outside", "link_out")
	symlink(t, proj, ".env", "link_env")
	r := openRoot(t, proj)

	cases := map[string]Reason{
		"../outside":   ReasonEscape,
		"/etc/passwd":  ReasonAbsolute,
		"C:/x":         ReasonAbsolute,
		"src/../../x":  ReasonEscape,
		"deep/../../x": ReasonEscape,
		".env":         ReasonDenied,
		".git/config":  ReasonDenied,
		"link_out/f":   ReasonEscape,
		"link_out":     ReasonEscape,
		"link_env":     ReasonDenied,
		"a\x00b":       ReasonInvalid,
		`src\a.go`:     ReasonInvalid,
		"":             ReasonInvalid,
	}
	for rel, want := range cases {
		got, err := r.Check(rel, true)
		if err == nil {
			t.Errorf("Check(%q) = %q, nil; want reason %v", rel, got, want)
			continue
		}
		reason, ok := reasonOf(err)
		if !ok {
			t.Errorf("Check(%q) error %v is not *PathError", rel, err)
			continue
		}
		if reason != want {
			t.Errorf("Check(%q) reason = %v, want %v", rel, reason, want)
		}
	}

	for in, want := range map[string]string{"src/a.go": "src/a.go", "./src//a.go": "src/a.go", ".": ".", "src/new.go": "src/new.go"} {
		got, err := r.Check(in, true)
		if err != nil || got != want {
			t.Errorf("Check(%q) = %q, %v; want %q, nil", in, got, err, want)
		}
	}
}

func TestCheckLinkInsideRoot(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "src/a.go", "package a")
	symlink(t, proj, "src", "alias")
	symlink(t, proj, "a.go", "src/b.go")
	r := openRoot(t, proj)
	for _, rel := range []string{"alias/a.go", "src/b.go", "alias/b.go"} {
		if _, err := r.Check(rel, true); err != nil {
			t.Errorf("Check(%q) = %v, want ok (link stays inside)", rel, err)
		}
	}
}

func TestCheckAbsoluteLinkInside(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "src/a.go", "package a")
	writeFile(t, proj, "keys/.env", "K=V")
	symlink(t, proj, filepath.Join(proj, "src"), "abs_in")
	symlink(t, proj, filepath.Join(proj, "keys", ".env"), "abs_env")
	r := openRoot(t, proj)
	if _, err := r.Check("abs_in/a.go", true); err != nil {
		t.Errorf("absolute link inside root rejected: %v", err)
	}
	if _, err := r.Check("abs_env", true); err == nil {
		t.Errorf("absolute link to .env accepted")
	} else if reason, _ := reasonOf(err); reason != ReasonDenied {
		t.Errorf("absolute link to .env reason = %v, want Denied", reason)
	}
}

func TestCheckLinkLoop(t *testing.T) {
	proj := t.TempDir()
	symlink(t, proj, "b", "a")
	symlink(t, proj, "a", "b")
	r := openRoot(t, proj)
	_, err := r.Check("a/x", true)
	if reason, ok := reasonOf(err); !ok || reason != ReasonInvalid {
		t.Errorf("link loop: err = %v, want Invalid", err)
	}
}

func TestLstatGoesThroughRoot(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	writeFile(t, base, "outside/f", "secret")
	writeFile(t, proj, "src/a.go", "package a")
	symlink(t, proj, "../outside", "link_out")
	r := openRoot(t, proj)
	fi, err := r.Lstat("src/a.go")
	if err != nil || fi.Name() != "a.go" {
		t.Fatalf("Lstat(src/a.go) = %v, %v", fi, err)
	}
	if _, err := r.Lstat("link_out/f"); err == nil {
		t.Errorf("Lstat through escaping link succeeded")
	}
	if _, err := r.Lstat("../outside/f"); err == nil {
		t.Errorf("Lstat of ../ succeeded")
	}
	if !strings.HasSuffix(filepath.ToSlash(r.Dir()), "/proj") {
		t.Errorf("Dir() = %q", r.Dir())
	}
}

func TestPathErrorHasNoPath(t *testing.T) {
	for _, reason := range []Reason{ReasonEscape, ReasonAbsolute, ReasonDenied, ReasonIgnored, ReasonInvalid} {
		msg := (&PathError{Rel: "x/.env", Reason: reason}).Error()
		if strings.Contains(msg, ".env") || strings.Contains(msg, "x/") {
			t.Errorf("PathError.Error() leaks path: %q", msg)
		}
	}
}

// A ".." inside a link target is resolved after the links before it, the
// way the OS does: d -> s/.. with s -> .ssh/keys lands in .ssh.
func TestCheckLinkDotDotFollowsOS(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, ".ssh/keys/k", "k")
	writeFile(t, proj, ".ssh/config", "SECRET")
	symlink(t, proj, ".ssh/keys", "s")
	symlink(t, proj, "s/..", "d")
	r := openRoot(t, proj)
	for _, rel := range []string{"d/config", "d"} {
		_, err := r.Check(rel, true)
		if reason, ok := reasonOf(err); !ok || reason != ReasonDenied {
			t.Errorf("Check(%q) err = %v, want Denied", rel, err)
		}
	}
	if err := r.Rename("d/config", "visible.txt"); err == nil {
		t.Errorf("Rename through d/.. into .ssh succeeded")
	}
	if err := r.Create("d/new"); err == nil {
		t.Errorf("Create through d/.. into .ssh succeeded")
	}
}

func TestReadTextLinkDotDotReadsOSTarget(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "config", "root")
	writeFile(t, proj, "sub/config", "sub")
	writeFile(t, proj, "sub/inner/x", "x")
	symlink(t, proj, "sub/inner", "s")
	symlink(t, proj, "s/../config", "a")
	symlink(t, proj, proj+string(filepath.Separator)+filepath.FromSlash("s/../config"), "abs") // raw, not cleaned
	r := openRoot(t, proj)
	for _, rel := range []string{"a", "abs"} {
		text, _, err := r.ReadText(rel)
		if err != nil || text != "sub" {
			t.Errorf("ReadText(%s) = %q, %v; want %q (the file the OS opens)", rel, text, err, "sub")
		}
	}
}

func TestCheckLinkDotDotAboveRoot(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "sub/x", "x")
	symlink(t, proj, "sub", "s")
	symlink(t, proj, "s/../..", "up")
	r := openRoot(t, proj)
	_, err := r.Check("up", true)
	if reason, ok := reasonOf(err); !ok || reason != ReasonEscape {
		t.Errorf("Check(up) err = %v, want Escape", err)
	}
}
