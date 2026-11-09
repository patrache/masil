package lazygit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolveRepoOrder(t *testing.T) {
	ctx := context.Background()
	r := &fakeRunner{out: map[string]string{
		"git -C /cfg/repo rev-parse --show-toplevel":  "/cfg/repo\n",
		"git -C /start/sub rev-parse --show-toplevel": "/start\n",
	}}
	env := fakeEnv(r, nil)
	// 1. the configured repo first.
	if repo, pick, err := ResolveRepo(ctx, env, "/cfg/repo", "/start/sub"); err != nil || pick || repo != filepath.Clean("/cfg/repo") {
		t.Fatalf("cfg: %q %v %v", repo, pick, err)
	}
	// 2. the start folder's top level.
	if repo, pick, err := ResolveRepo(ctx, env, "", "/start/sub"); err != nil || pick || repo != filepath.Clean("/start") {
		t.Fatalf("start: %q %v %v", repo, pick, err)
	}
	// A configured path that is not a repo falls through to the start folder.
	if repo, pick, err := ResolveRepo(ctx, env, "/not/repo", "/start/sub"); err != nil || pick || repo != filepath.Clean("/start") {
		t.Fatalf("bad cfg: %q %v %v", repo, pick, err)
	}
	// 3. neither: the picker.
	if repo, pick, err := ResolveRepo(ctx, env, "", "/home/me"); err != nil || !pick || repo != "" {
		t.Fatalf("pick: %q %v %v", repo, pick, err)
	}
	if repo, pick, err := ResolveRepo(ctx, env, "", ""); err != nil || !pick || repo != "" {
		t.Fatalf("no start: %q %v %v", repo, pick, err)
	}
}

func TestRepoRootNotRepo(t *testing.T) {
	env := fakeEnv(&fakeRunner{}, nil)
	if _, err := RepoRoot(context.Background(), env, "/tmp/x"); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
	r := &fakeRunner{out: map[string]string{"git -C /tmp/y rev-parse --show-toplevel": "\n"}}
	if _, err := RepoRoot(context.Background(), fakeEnv(r, nil), "/tmp/y"); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("empty output: err = %v, want ErrNotRepo", err)
	}
}

func TestRepoRootSymlink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is required for this test")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "real repo 한글")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	link := filepath.Join(base, "-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RepoRoot(context.Background(), SystemEnv(), filepath.Join(link, "sub"))
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	if got != want {
		t.Fatalf("RepoRoot = %q, want %q", got, want)
	}
	if _, err := RepoRoot(context.Background(), SystemEnv(), base); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("non-repo: err = %v", err)
	}
}

func TestPushRecent(t *testing.T) {
	var list []string
	for i := range 12 {
		list = PushRecent(list, fmt.Sprintf("/r/%d", i))
	}
	if len(list) != RecentMax || list[0] != "/r/11" || list[9] != "/r/2" {
		t.Fatalf("list = %q", list)
	}
	in := []string{"/a", "/b", "/c"}
	got := PushRecent(in, "/b")
	if !slices.Equal(got, []string{"/b", "/a", "/c"}) {
		t.Fatalf("dedupe: %q", got)
	}
	if !slices.Equal(in, []string{"/a", "/b", "/c"}) {
		t.Fatalf("input changed: %q", in)
	}
	if got := PushRecent(in, ""); !slices.Equal(got, in) {
		t.Fatalf("empty repo: %q", got)
	}
	if RecentMax != 10 {
		t.Fatal("RecentMax")
	}
}
