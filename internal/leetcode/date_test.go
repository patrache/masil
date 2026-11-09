package leetcode

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNextReset(t *testing.T) {
	now := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	if got := NextReset(now); got != 21*time.Hour+55*time.Minute {
		t.Fatalf("got %v", got)
	}
	if got := NextReset(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)); got != 24*time.Hour {
		t.Fatalf("at midnight: got %v", got)
	}
	if got := NextReset(time.Date(2026, 11, 5, 23, 59, 59, 0, time.FixedZone("KST", 9*3600))); got != 9*time.Hour+time.Second {
		t.Fatalf("KST: got %v", got)
	}
	if UTCDate(time.Date(2026, 11, 9, 23, 59, 0, 0, time.FixedZone("KST", 9*3600))) != "2026-11-09" {
		t.Fatal("KST 23:59 is 14:59Z same day")
	}
	if UTCDate(time.Date(2026, 11, 10, 8, 59, 0, 0, time.FixedZone("KST", 9*3600))) != "2026-11-09" {
		t.Fatal("KST 08:59 is 23:59Z the day before")
	}
}

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"two-sum", "a", "1606", strings.Repeat("a", 100)} {
		if !ValidSlug(s) {
			t.Errorf("%q rejected", s)
		}
	}
	for _, s := range []string{"", "Two-Sum", "a b", "../x", "a/b", "a_b", strings.Repeat("a", 101), "two-sum\n"} {
		if ValidSlug(s) {
			t.Errorf("%q accepted", s)
		}
	}
	if !FrontendIDRe.MatchString("1606") || FrontendIDRe.MatchString("1234567") || FrontendIDRe.MatchString("12a") {
		t.Error("FrontendIDRe")
	}
}

func TestDefaultDir(t *testing.T) {
	if got, want := DefaultDir("/home/u"), filepath.Join("/home/u", "masil", "leetcode"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSolutionPath(t *testing.T) {
	dir := t.TempDir()
	got, err := SolutionPath(dir, "2026-11-05", "1606", "find-servers-that-handled-most-number-of-requests", Languages[0])
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "2026-11-05-1606-find-servers-that-handled-most-number-of-requests.go"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	py, _ := LangBySlug("python3")
	if got, _ := SolutionPath(dir, "2026-11-05", "1", "two-sum", py); filepath.Base(got) != "2026-11-05-1-two-sum.py" {
		t.Fatalf("python: %q", got)
	}
}

func TestSolutionPathRejectsHostileSlug(t *testing.T) {
	for _, s := range []string{"../../.ssh/x", "a b", "Two-Sum", strings.Repeat("a", 101), ""} {
		if _, err := SolutionPath(t.TempDir(), "2026-11-05", "1606", s, Languages[0]); !errors.Is(err, ErrBadSlug) {
			t.Fatalf("%q: %v", s, err)
		}
	}
	for _, c := range []struct{ date, id string }{
		{"2026-13-01", "1"}, {"../2026-11-05", "1"}, {"20261105", "1"}, {"2026-11-05", "../1"}, {"2026-11-05", ""}, {"2026-11-05", "1234567"},
	} {
		if _, err := SolutionPath(t.TempDir(), c.date, c.id, "two-sum", Languages[0]); !errors.Is(err, ErrBadSlug) {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	if _, err := SolutionPath(t.TempDir(), "2026-11-05", "1", "two-sum", Lang{Slug: "x", Name: "X", Ext: "/../x"}); !errors.Is(err, ErrBadSlug) {
		t.Fatalf("bad ext: %v", err)
	}
}

func TestEnsureSolutionFileKeepsUserWork(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "masil", "leetcode")
	path := filepath.Join(dir, "2026-11-05-1-two-sum.go")
	created, err := EnsureSolutionFile(path, "snippet")
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "snippet" {
		t.Fatalf("content %q", b)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
			t.Fatalf("file mode %v", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o755 {
			t.Fatalf("dir mode %v", fi.Mode().Perm())
		}
	}
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = EnsureSolutionFile(path, "snippet")
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "mine" {
		t.Fatalf("user work overwritten: %q", b)
	}
}
