package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	f, err := openRotating(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(bytes.Repeat([]byte("a"), 80))
	f.Write(bytes.Repeat([]byte("b"), 40)) // would exceed 100 -> rotate first
	f.Close()
	old, _ := os.ReadFile(path + ".1")
	cur, _ := os.ReadFile(path)
	if len(old) != 80 || string(cur) != strings.Repeat("b", 40) {
		t.Fatalf("old=%d cur=%q", len(old), cur)
	}
}
