package fsroot

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestWatcherReportsRealChangesOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\n")
	p := filepath.Join(dir, "a.txt")
	r := openRoot(t, dir)
	_, info, err := r.ReadText("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(r)
	w.Track("a.txt", info.FP)
	if got := w.Poll(); got != nil {
		t.Errorf("Poll with no change = %v", got)
	}

	// Same content rewritten with a new mtime: not a change.
	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(p, later, later)
	if got := w.Poll(); got != nil {
		t.Errorf("Poll after same-content touch = %v", got)
	}

	if err := os.WriteFile(p, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.Poll(); !reflect.DeepEqual(got, []Change{{Rel: "a.txt"}}) {
		t.Errorf("Poll after change = %v", got)
	}
	if got := w.Poll(); got != nil {
		t.Errorf("change reported twice: %v", got)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if got := w.Poll(); !reflect.DeepEqual(got, []Change{{Rel: "a.txt", Deleted: true}}) {
		t.Errorf("Poll after remove = %v", got)
	}
	if got := w.Poll(); got != nil {
		t.Errorf("delete reported twice: %v", got)
	}

	// Recreated: reported as a change again.
	if err := os.WriteFile(p, []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.Poll(); !reflect.DeepEqual(got, []Change{{Rel: "a.txt"}}) {
		t.Errorf("Poll after recreate = %v", got)
	}

	w.Untrack("a.txt")
	_ = os.WriteFile(p, []byte("four\n"), 0o644)
	if got := w.Poll(); got != nil {
		t.Errorf("untracked file reported: %v", got)
	}
}

// A tracked file that grows past MaxView is reported as changed without
// being read whole, and WriteText refuses it as changed on disk.
func TestWatcherLargeGrowthNotRead(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.log", "start\n")
	r := openRoot(t, dir)
	_, info, err := r.ReadText("app.log")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(r)
	w.Track("app.log", info.FP)
	big := make([]byte, MaxView+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(dir, "app.log"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := w.Poll(); len(got) != 1 || got[0].Rel != "app.log" || got[0].Deleted {
		t.Errorf("Poll after growth = %v", got)
	}
	if _, err := r.WriteText("app.log", "x", info); !errors.Is(err, ErrChangedOnDisk) {
		t.Errorf("WriteText over a grown file err = %v, want ErrChangedOnDisk", err)
	}
	w.mu.Lock()
	fp := w.files["app.log"]
	w.mu.Unlock()
	if fp.Sum == sha256.Sum256(big) {
		t.Errorf("Poll hashed the whole file above MaxView")
	}
	// Grows again with the same size but a new mtime: still a change.
	later := time.Now().Add(3 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "app.log"), later, later); err != nil {
		t.Fatal(err)
	}
	if got := w.Poll(); len(got) != 1 {
		t.Errorf("Poll after mtime change of a large file = %v", got)
	}
}
