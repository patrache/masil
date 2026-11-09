//go:build unix

package fsroot

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// withTimeout fails the test when fn has not returned after 2 s (a FIFO
// open blocks until a writer appears).
func withTimeout(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s blocked on a FIFO", name)
	}
}

func TestFIFONotOpened(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\n")
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	r := openRoot(t, dir)

	entries, _, err := r.List(".", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "pipe" {
			t.Errorf("peer List shows the FIFO: %+v", e)
		}
	}
	withTimeout(t, "ReadText", func() {
		if _, _, err := r.ReadText("pipe"); !errors.Is(err, ErrNotText) {
			t.Errorf("ReadText(pipe) err = %v, want ErrNotText", err)
		}
	})
	withTimeout(t, "WriteText", func() {
		if _, err := r.WriteText("pipe", "x", FileInfo{Kind: KindText}); err == nil {
			t.Errorf("WriteText(pipe) succeeded")
		}
	})

	// A tracked file replaced by a FIFO must not block Poll.
	_, info, err := r.ReadText("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(r)
	w.Track("a.txt", info.FP)
	if err := syscall.Unlink(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "a.txt"), 0o644); err != nil {
		t.Fatal(err)
	}
	withTimeout(t, "Poll", func() { _ = w.Poll() })
}
