package fsroot

import (
	"errors"
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
