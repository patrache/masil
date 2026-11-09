package fsroot

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteTextRestoresCRLFAndBOM(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w.txt")
	if err := os.WriteFile(p, []byte("\ufeffa\r\nb\r\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	r := openRoot(t, dir)
	text, info, err := r.ReadText("w.txt")
	if err != nil || text != "a\nb\n" || info.EOL != EOLCRLF || !info.BOM {
		t.Fatalf("ReadText = %q, %+v, %v", text, info, err)
	}
	next, err := r.WriteText("w.txt", "a\nX\n", info)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "\ufeffa\r\nX\r\n" {
		t.Errorf("disk = %q", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
			t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
		}
	}
	// The returned info is the new base: a second save succeeds.
	if next.FP.Size != int64(len(got)) || next.EOL != EOLCRLF || !next.BOM {
		t.Errorf("next info = %+v", next)
	}
	if _, err := r.WriteText("w.txt", "a\nY\n", next); err != nil {
		t.Errorf("second save: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "\ufeffa\r\nY\r\n" {
		t.Errorf("disk after second save = %q", got)
	}
}

func TestWriteTextDetectsExternalChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\n")
	r := openRoot(t, dir)
	_, info, err := r.ReadText("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteText("a.txt", "mine\n", info); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("WriteText err = %v, want ErrChangedOnDisk", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "external\n" {
		t.Errorf("disk = %q, want external content kept", got)
	}
}

func TestWriteTextNewFileAndDeleted(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)
	info, err := r.WriteText("new/n.txt", "hi\n", FileInfo{})
	if !errors.Is(err, os.ErrNotExist) {
		// parent folder missing: the write must fail, not create folders
		t.Fatalf("WriteText into missing folder = %+v, %v", info, err)
	}
	info, err = r.WriteText("n.txt", "hi\n", FileInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "n.txt")); fi.Mode().Perm() != 0o644 {
			t.Errorf("new file mode = %v, want 0644", fi.Mode().Perm())
		}
	}
	// A zero base for an existing file means someone created it meanwhile.
	if _, err := r.WriteText("n.txt", "again\n", FileInfo{}); !errors.Is(err, ErrChangedOnDisk) {
		t.Errorf("zero base over existing file: %v, want ErrChangedOnDisk", err)
	}
	if err := os.Remove(filepath.Join(dir, "n.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteText("n.txt", "x\n", info); !errors.Is(err, ErrChangedOnDisk) {
		t.Errorf("save over deleted file: %v, want ErrChangedOnDisk", err)
	}
}

func TestWriteTextAtomic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\n")
	r := openRoot(t, dir)
	_, info, err := r.ReadText("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteText("a.txt", "two\n", info); err != nil {
		t.Fatal(err)
	}
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		if strings.Contains(de.Name(), ".masil-") {
			t.Errorf("leftover temp file %q", de.Name())
		}
	}
	if len(des) != 1 {
		t.Errorf("dir has %d entries, want 1", len(des))
	}
}

func TestWriteTextThroughLinkKeepsLink(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "real.txt", "one\n")
	symlink(t, dir, "real.txt", "alias.txt")
	r := openRoot(t, dir)
	_, info, err := r.ReadText("alias.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteText("alias.txt", "two\n", info); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "alias.txt")); fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link replaced by a regular file")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "real.txt")); string(got) != "two\n" {
		t.Errorf("target = %q", got)
	}
}

func TestWriteTextRefusesReadOnlyKinds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "m.txt", "a\r\nb\n")
	r := openRoot(t, dir)
	_, info, _ := r.ReadText("m.txt")
	if _, err := r.WriteText("m.txt", "x", info); !errors.Is(err, ErrNotText) {
		t.Errorf("WriteText on mixed EOL = %v, want ErrNotText", err)
	}
}

func TestCreateRenameRemoveChecked(t *testing.T) {
	dir := t.TempDir()
	r := openRoot(t, dir)
	if err := r.Create("a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := r.Create("a.txt"); !errors.Is(err, os.ErrExist) {
		t.Errorf("second Create = %v, want ErrExist", err)
	}
	if err := r.Rename("a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []error{r.Create(".env"), r.Rename("b.txt", "../b.txt"), r.Rename("b.txt", ".git/b"), r.Remove("../x")} {
		if _, ok := reasonOf(bad); !ok {
			t.Errorf("unsafe op error = %v, want *PathError", bad)
		}
	}
	if err := r.Remove("b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("b.txt still there")
	}
}
