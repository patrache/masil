package archdoc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, "arch")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, parent
}

func TestStoreSaveLoadList(t *testing.T) {
	s, dir, _ := openTestStore(t)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(dir)
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("dir mode = %v, %v", fi.Mode(), err)
		}
	}
	d := small()
	d.Title = "결제"
	if err := s.Save("결제 시스템", d, false); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "결제 시스템.json")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v", fi.Mode())
	}
	// Make the second save clearly newer.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("other", New("second"), false); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "other" || list[1].Name != "결제 시스템" || list[1].Title != "결제" || list[1].Nodes != 4 {
		t.Fatalf("List = %+v", list)
	}
	got, err := s.Load("결제 시스템")
	if err != nil {
		t.Fatal(err)
	}
	want := mustDecode(t, mustEncode(t, d))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v", got)
	}
	// NFC: a decomposed name finds the same file.
	if _, err := s.Load("결제 시스템"); err != nil {
		t.Fatalf("NFD name: %v", err)
	}

	if err := s.Rename("other", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("renamed", "결제 시스템"); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto existing = %v", err)
	}
	if err := s.Delete("renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("renamed"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load after delete = %v", err)
	}
	if err := s.Save("bad", &Design{SchemaVersion: 1, DurationS: 1}, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid design saved: %v", err)
	}
}

func TestStoreNamesAreSafe(t *testing.T) {
	s, dir, parent := openTestStore(t)
	bad := []string{
		"", "../x", "a/b", `a\b`, "x\x00", ".", "..", ".hidden", " lead", "trail ", "a:b", "a*b", "a?b", `a"b`, "a<b", "a>b", "a|b",
		"tab\there", "bidi‮exe", strings.Repeat("가", 65), strings.Repeat("é́́́", 60),
	}
	for _, name := range bad {
		if _, err := SafeName(name); !errors.Is(err, ErrBadName) {
			t.Errorf("SafeName(%q) = %v, want ErrBadName", name, err)
		}
		if err := s.Save(name, small(), true); !errors.Is(err, ErrBadName) {
			t.Errorf("Save(%q) = %v", name, err)
		}
		if _, err := s.Load(name); !errors.Is(err, ErrBadName) {
			t.Errorf("Load(%q) = %v", name, err)
		}
		if err := s.Delete(name); !errors.Is(err, ErrBadName) {
			t.Errorf("Delete(%q) = %v", name, err)
		}
		if err := s.Rename("x", name); !errors.Is(err, ErrBadName) {
			t.Errorf("Rename to %q = %v", name, err)
		}
	}
	for _, good := range []string{"결제 시스템", strings.Repeat("가", 64), "v1.2 draft", "a-b_c"} {
		if got, err := SafeName(good); err != nil || got != good {
			t.Errorf("SafeName(%q) = %q, %v", good, got, err)
		}
	}
	err := filepath.WalkDir(parent, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != parent && p != dir {
			t.Errorf("unexpected path %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A symlink planted in the store pointing outside is not followed.
	if runtime.GOOS != "windows" {
		outside := filepath.Join(parent, "secret.json")
		if err := os.WriteFile(outside, mustEncode(t, small()), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "link.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load("link"); err == nil {
			t.Fatal("Load followed a symlink out of the store")
		}
	}
}

func TestStoreNoSilentOverwrite(t *testing.T) {
	s, dir, _ := openTestStore(t)
	first := small()
	first.Title = "first"
	if err := s.Save("plan", first, false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "plan.json"))
	second := small()
	second.Title = "second"
	if err := s.Save("plan", second, false); !errors.Is(err, ErrExists) {
		t.Fatalf("second save = %v, want ErrExists", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "plan.json"))
	if string(before) != string(after) {
		t.Fatal("file changed without overwrite")
	}
	if err := s.Save("plan", second, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Load("plan"); got.Title != "second" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestStoreBrokenFileListed(t *testing.T) {
	s, dir, _ := openTestStore(t)
	if err := os.WriteFile(filepath.Join(dir, "x.json"), []byte("garbage{"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, junk := range []string{".x.json.tmp-1-2", "notes.txt", "a\\b.json"} {
		_ = os.WriteFile(filepath.Join(dir, junk), []byte("{}"), 0o600)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("ok", small(), false); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]Entry{}
	for _, e := range list {
		names[e.Name] = e
	}
	if len(list) != 2 || names["x"].Title != "" || names["x"].Name != "x" || names["ok"].Title != "small" {
		t.Fatalf("List = %+v", list)
	}
	if _, err := s.Load("x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load broken = %v", err)
	}
}

// TestStoreRenameCaseOnly: changing only the letter case of a name works on
// case-insensitive file systems too (APFS, NTFS), where the new name already
// "exists" as the old file (P3 review).
func TestStoreRenameCaseOnly(t *testing.T) {
	s, _, _ := openTestStore(t)
	if err := s.Save("Design", small(), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("Design", "design"); err != nil {
		t.Fatalf("case-only rename = %v", err)
	}
	ents, err := s.List()
	if err != nil || len(ents) != 1 || ents[0].Name != "design" {
		t.Fatalf("list = %+v, %v", ents, err)
	}
	// A different design of another case is still refused.
	if err := s.Save("Other", small(), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("design", "Other"); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto another design = %v, want ErrExists", err)
	}
}

// TestStoreLongestNameSaves: the temp file config.WriteFileAtomic creates,
// "." + name + ".json.tmp-<pid>-<rand>", must fit the 255-byte name limit of
// ext4, xfs and btrfs for every name SafeName accepts (P3 review).
func TestStoreLongestNameSaves(t *testing.T) {
	longest := ""
	for n := 255; n > 0 && longest == ""; n-- {
		// 4-byte letters (U+1D400) padded with ASCII, no NFC change
		name := strings.Repeat("\U0001D400", n/4) + strings.Repeat("x", n%4)
		if got, err := SafeName(name); err == nil && got == name {
			longest = name
		}
	}
	const pidDigits, randDigits = 10, 10 // uint32 pid and CreateTemp's uint32
	if tmp := 1 + len(longest) + len(".json") + len(".tmp-") + pidDigits + 1 + randDigits; tmp > 255 {
		t.Fatalf("a %d-byte name passes SafeName but its temp file name is %d bytes", len(longest), tmp)
	}
	s, _, _ := openTestStore(t)
	if err := s.Save(longest, small(), false); err != nil {
		t.Fatalf("Save(longest name) = %v", err)
	}
	if _, err := s.Load(longest); err != nil {
		t.Fatal(err)
	}
}
