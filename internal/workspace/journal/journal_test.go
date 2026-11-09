package journal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/fsroot"
)

func TestJournalRoundTrip(t *testing.T) {
	cache := t.TempDir()
	j, err := Open(cache, "/home/u/proj")
	if err != nil {
		t.Fatal(err)
	}
	base := fsroot.Fingerprint{Size: 3, ModTime: time.Unix(1700000000, 0).UTC(), Sum: [32]byte{1, 2, 3}}
	if err := j.Put("src/a.go", "package a\n", base); err != nil {
		t.Fatal(err)
	}
	if err := j.Put("b.txt", "bee", fsroot.Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	if err := j.Put("src/a.go", "package a // v2\n", base); err != nil {
		t.Fatal(err)
	}
	es, err := j.Pending()
	if err != nil || len(es) != 2 {
		t.Fatalf("Pending = %+v, %v", es, err)
	}
	got := map[string]Entry{}
	for _, e := range es {
		got[e.Rel] = e
	}
	if a := got["src/a.go"]; a.Text != "package a // v2\n" || a.Base.Sum != base.Sum || !a.Base.ModTime.Equal(base.ModTime) || a.At.IsZero() {
		t.Errorf("entry a = %+v", a)
	}
	if b := got["b.txt"]; b.Text != "bee" || !b.Base.IsZero() {
		t.Errorf("entry b = %+v", b)
	}

	dir := filepath.Dir(j.path("b.txt"))
	if filepath.Dir(filepath.Dir(dir)) != cache || filepath.Base(filepath.Dir(dir)) != "workspace-journal" || len(filepath.Base(dir)) != 16 {
		t.Errorf("journal dir = %q", dir)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(j.path("b.txt")); fi.Mode().Perm() != 0o600 {
			t.Errorf("entry mode = %v", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %v", fi.Mode().Perm())
		}
	}

	if err := j.Drop("src/a.go"); err != nil {
		t.Fatal(err)
	}
	if err := j.Drop("never-put"); err != nil {
		t.Errorf("Drop of a missing entry = %v", err)
	}
	if es, _ := j.Pending(); len(es) != 1 || es[0].Rel != "b.txt" {
		t.Errorf("after Drop: %+v", es)
	}

	// A second folder gets its own journal.
	k, err := Open(cache, "/home/u/other")
	if err != nil {
		t.Fatal(err)
	}
	if es, _ := k.Pending(); len(es) != 0 {
		t.Errorf("other folder sees %+v", es)
	}
}

func TestJournalSkipsBrokenFiles(t *testing.T) {
	j, err := Open(t.TempDir(), "/p")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Put("a", "x", fsroot.Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(j.path("a")), "junk.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if es, err := j.Pending(); err != nil || len(es) != 1 {
		t.Errorf("Pending = %+v, %v", es, err)
	}
}
