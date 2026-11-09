package fsroot

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestMatcherSyntax(t *testing.T) {
	m := ParseIgnore("", []string{
		"# comment", "", "node_modules/", "*.log", "!keep.log", "/build", "docs/**/tmp", `\#lit`,
		"[ab].txt", "?.md", "**/cache", "out/**", `\!bang`, "trail\\ ", "spaces   ",
	})
	type tc struct {
		rel     string
		dir     bool
		ignored bool
	}
	for _, c := range []tc{
		{"node_modules", true, true},
		{"a/node_modules", true, true},
		{"node_modules", false, false}, // dir-only pattern
		{"x.log", false, true},
		{"a/b/x.log", false, true},
		{"keep.log", false, false},
		{"build", true, true},
		{"src/build", true, false},
		{"docs/a/b/tmp", true, true},
		{"docs/tmp", true, true},
		{"x/docs/a/tmp", true, false},
		{"#lit", false, true},
		{"a.txt", false, true},
		{"c.txt", false, false},
		{"z.md", false, true},
		{"zz.md", false, false},
		{"cache", true, true},
		{"deep/er/cache", true, true},
		{"out/a/b", false, true},
		{"out", true, false},
		{"!bang", false, true},
		{"trail ", false, true},
		{"spaces", false, true},
		{"comment", false, false},
	} {
		got, decided := m.Match(c.rel, c.dir)
		if got != c.ignored {
			t.Errorf("Match(%q, %v) = %v (decided %v), want %v", c.rel, c.dir, got, decided, c.ignored)
		}
	}
	if _, decided := m.Match("src/main.go", false); decided {
		t.Errorf("unmatched path reported as decided")
	}
}

func TestMatcherBaseDir(t *testing.T) {
	m := ParseIgnore("sub", []string{"/local", "*.tmp", "!important.tmp"})
	for rel, want := range map[string]bool{
		"sub/local": true, "sub/x/local": false, "local": false, "sub/a.tmp": true,
		"sub/d/b.tmp": true, "sub/important.tmp": false, "a.tmp": false,
	} {
		if got, _ := m.Match(rel, false); got != want {
			t.Errorf("Match(%q) = %v, want %v", rel, got, want)
		}
	}
}

// ignoreFixture builds a folder with root and nested .gitignore files and a
// .masilignore, and returns every path in it (slash, without .git) with its
// dir flag.
func ignoreFixture(t *testing.T, dir string) (rels []string, dirs []bool) {
	t.Helper()
	files := map[string]string{
		".gitignore":          "node_modules/\n*.log\n!keep.log\n/build\ndocs/**/tmp\n[ab].txt\n**/cache\nout/**\n",
		"sub/.gitignore":      "*.tmp\n!important.tmp\n/local\n",
		"sub/deep/.gitignore": "!x.log\n",
		".masilignore":        "notes.txt\nprivate/\n",
		"sub/.masilignore":    "draft.md\n",
		"src/a.go":            "a",
		"src/b.log":           "b",
		"keep.log":            "k",
		"x.log":               "x",
		"node_modules/m/i.js": "m",
		"build/o":             "o",
		"src/build/o":         "o",
		"docs/a/b/tmp/t":      "t",
		"docs/tmp/t":          "t",
		"a.txt":               "a",
		"c.txt":               "c",
		"z/cache/c":           "c",
		"out/a/b":             "b",
		"sub/a.tmp":           "a",
		"sub/important.tmp":   "i",
		"sub/local/l":         "l",
		"sub/x/local":         "l",
		"sub/deep/x.log":      "x",
		"sub/draft.md":        "d",
		"draft.md":            "d",
		"notes.txt":           "n",
		"private/p":           "p",
		"README.md":           "r",
	}
	for rel, content := range files {
		writeFile(t, dir, rel, content)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		rels = append(rels, rel)
		dirs = append(dirs, d.IsDir())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return rels, dirs
}

func isolateGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	empty := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestGitIgnorerMatchesFallback(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGit(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	rels, dirs := ignoreFixture(t, dir)
	r := openRoot(t, dir)

	gitRes, err := NewGitIgnorer(dir, nil).Ignored(rels, dirs)
	if err != nil {
		t.Fatal(err)
	}
	fileRes, err := NewFileIgnorer(r).Ignored(rels, dirs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"src/a.go": false, "src/b.log": true, "keep.log": false, "x.log": true,
		"node_modules": true, "node_modules/m/i.js": true, "build": true, "src/build": false,
		"docs/a/b/tmp/t": true, "a.txt": true, "c.txt": false, "z/cache/c": true, "out/a/b": true,
		"sub/a.tmp": true, "sub/important.tmp": false, "sub/local": true, "sub/x/local": false,
		"sub/deep/x.log": false, "sub/draft.md": true, "draft.md": false, "notes.txt": true,
		"private": true, "private/p": true, "README.md": false,
	}
	var mismatch []string
	for i, rel := range rels {
		if gitRes[i] != fileRes[i] {
			mismatch = append(mismatch, rel)
		}
		if w, ok := want[rel]; ok && gitRes[i] != w {
			t.Errorf("git: %q ignored = %v, want %v", rel, gitRes[i], w)
		}
	}
	sort.Strings(mismatch)
	if len(mismatch) > 0 {
		t.Errorf("git and fallback disagree on %v", mismatch)
	}
}

func TestGitIgnorerFallsBack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "*.log\n")
	writeFile(t, dir, ".masilignore", "notes.txt\n")
	calls := 0
	broken := func(_ context.Context, _ string, _ []byte) ([]byte, int, error) {
		calls++
		return nil, 128, nil
	}
	got, err := NewGitIgnorer(dir, broken).Ignored([]string{"a.log", "notes.txt", "a.go"}, []bool{false, false, false})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !got[0] || !got[1] || got[2] {
		t.Errorf("fallback result = %v (calls %d), want [true true false]", got, calls)
	}
}
