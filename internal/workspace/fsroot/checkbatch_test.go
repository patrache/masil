package fsroot

import (
	"errors"
	"os/exec"
	"slices"
	"testing"
)

// countIgnorer hides the paths in hide and counts its calls.
type countIgnorer struct {
	hide  map[string]bool
	err   error
	calls int
	seen  [][]string
}

func (c *countIgnorer) Ignored(rels []string, dirs []bool) ([]bool, error) {
	c.calls++
	c.seen = append(c.seen, slices.Clone(rels))
	if c.err != nil {
		return nil, c.err
	}
	out := make([]bool, len(rels))
	for i, rel := range rels {
		out[i] = c.hide[rel]
	}
	return out, nil
}

// CheckBatch answers every path like Check but asks the ignore rules once for
// all of them (one git check-ignore per poll, not one per document).
func TestCheckBatchAsksIgnorerOnce(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "a.txt", "a")
	writeFile(t, proj, "src/b.txt", "b")
	writeFile(t, proj, "src/c.txt", "c")
	writeFile(t, proj, ".env", "K=V")
	writeFile(t, proj, ".gitignore", "")
	r := openRoot(t, proj)
	ig := &countIgnorer{hide: map[string]bool{"src/b.txt": true}}
	r.SetIgnorer(ig)

	rels := []string{"a.txt", "src/b.txt", "./src/c.txt", ".env", "../x", ".gitignore"}
	cleans, errs := r.CheckBatch(rels, true)
	if ig.calls != 1 {
		t.Fatalf("ignorer called %d times, want 1 (%v)", ig.calls, ig.seen)
	}
	want := []struct {
		clean  string
		reason Reason
		ok     bool
	}{
		{"a.txt", 0, true},
		{"", ReasonIgnored, false},
		{"src/c.txt", 0, true},
		{"", ReasonDenied, false},
		{"", ReasonEscape, false},
		{"", ReasonIgnored, false},
	}
	for i, w := range want {
		if w.ok {
			if errs[i] != nil || cleans[i] != w.clean {
				t.Errorf("%s: got (%q, %v), want (%q, nil)", rels[i], cleans[i], errs[i], w.clean)
			}
			continue
		}
		if reason, ok := reasonOf(errs[i]); !ok || reason != w.reason || cleans[i] != "" {
			t.Errorf("%s: got (%q, %v), want reason %v", rels[i], cleans[i], errs[i], w.reason)
		}
	}
	// Each answer matches Check on the same path.
	for i, rel := range rels {
		clean, err := r.Check(rel, true)
		if clean != cleans[i] || (err == nil) != (errs[i] == nil) {
			t.Errorf("%s: Check = (%q, %v), CheckBatch = (%q, %v)", rel, clean, err, cleans[i], errs[i])
		}
	}
	// Without forPeer the ignore rules are not asked.
	ig.calls = 0
	if _, errs := r.CheckBatch([]string{"src/b.txt"}, false); errs[0] != nil || ig.calls != 0 {
		t.Errorf("owner check: err %v, ignorer calls %d", errs[0], ig.calls)
	}
}

// An ignore-rule failure hides every path it was asked about (fail closed);
// paths already refused keep their own reason.
func TestCheckBatchIgnorerErrorFailsClosed(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "a.txt", "a")
	writeFile(t, proj, "b.txt", "b")
	writeFile(t, proj, ".env", "K=V")
	r := openRoot(t, proj)
	r.SetIgnorer(&countIgnorer{err: errors.New("git broke")})
	_, errs := r.CheckBatch([]string{"a.txt", "b.txt", ".env"}, true)
	for i, want := range []Reason{ReasonIgnored, ReasonIgnored, ReasonDenied} {
		if reason, ok := reasonOf(errs[i]); !ok || reason != want {
			t.Errorf("path %d: %v, want reason %v", i, errs[i], want)
		}
	}
}

// One path git refuses (a path through a linked folder: "fatal: pathspec
// 'lnk/a.txt' is beyond a symbolic link", exit 128) must not drop the rules
// from outside the folder (.git/info/exclude, core.excludesFile) for the
// other paths of the batch: every answer still equals Check on that path.
func TestCheckBatchLinkedFolderKeepsGitRules(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGit(t)
	proj := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", proj).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, proj, "real/a.txt", "a")
	writeFile(t, proj, "b.txt", "b")
	symlink(t, proj, "real", "lnk")
	writeFile(t, proj, ".git/info/exclude", "b.txt\n")
	r := openRoot(t, proj)

	rels := []string{"lnk/a.txt", "b.txt"}
	cleans, errs := r.CheckBatch(rels, true)
	if reason, ok := reasonOf(errs[1]); !ok || reason != ReasonIgnored {
		t.Errorf("b.txt in a batch with lnk/a.txt: (%q, %v), want ignored (info/exclude)", cleans[1], errs[1])
	}
	if errs[0] != nil || cleans[0] != "lnk/a.txt" {
		t.Errorf("lnk/a.txt: (%q, %v), want open", cleans[0], errs[0])
	}
	for i, rel := range rels {
		clean, err := r.Check(rel, true)
		if clean != cleans[i] || (err == nil) != (errs[i] == nil) {
			t.Errorf("%s: Check = (%q, %v), CheckBatch = (%q, %v)", rel, clean, err, cleans[i], errs[i])
		}
	}
}
