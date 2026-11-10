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
	cfgRepo, start, startSub := native("/cfg/repo"), native("/start"), native("/start/sub")
	r := &fakeRunner{out: map[string]string{
		"git -C " + cfgRepo + " rev-parse --show-toplevel":  gitTop(cfgRepo),
		"git -C " + startSub + " rev-parse --show-toplevel": gitTop(start),
	}}
	env := fakeEnv(r, nil)
	// 1. the configured repo first.
	if repo, pick, err := ResolveRepo(ctx, env, cfgRepo, startSub); err != nil || pick || repo != cfgRepo {
		t.Fatalf("cfg: %q %v %v (git ran %q)", repo, pick, err, r.calls)
	}
	// 2. the start folder's top level.
	if repo, pick, err := ResolveRepo(ctx, env, "", startSub); err != nil || pick || repo != start {
		t.Fatalf("start: %q %v %v", repo, pick, err)
	}
	// A configured path that is not a repo falls through to the start folder.
	if repo, pick, err := ResolveRepo(ctx, env, native("/not/repo"), startSub); err != nil || pick || repo != start {
		t.Fatalf("bad cfg: %q %v %v", repo, pick, err)
	}
	// 3. neither: the picker.
	if repo, pick, err := ResolveRepo(ctx, env, "", native("/home/me")); err != nil || !pick || repo != "" {
		t.Fatalf("pick: %q %v %v", repo, pick, err)
	}
	if repo, pick, err := ResolveRepo(ctx, env, "", ""); err != nil || !pick || repo != "" {
		t.Fatalf("no start: %q %v %v", repo, pick, err)
	}
}

// RepoRoot asks git about the absolute folder and gives back git's answer
// in this OS's form (Git for Windows answers "C:/r/masil"; CRLF too).
func TestRepoRootAbsoluteNative(t *testing.T) {
	ctx := context.Background()
	repo := native("/r/masil")
	r := &fakeRunner{out: map[string]string{
		"git -C " + repo + " rev-parse --show-toplevel": filepath.ToSlash(repo) + "\r\n",
	}}
	got, err := RepoRoot(ctx, fakeEnv(r, nil), repo)
	if err != nil || got != repo {
		t.Fatalf("RepoRoot = %q, %v; want %q (git ran %q)", got, err, repo, r.calls)
	}
	// A rooted folder without a drive is asked about as an absolute one.
	r = &fakeRunner{}
	_, _ = RepoRoot(ctx, fakeEnv(r, nil), "/r/masil")
	abs, _ := filepath.Abs("/r/masil")
	if want := "git -C " + abs + " rev-parse --show-toplevel"; len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("git ran %q, want %q", r.calls, want)
	}
}

func TestRepoRootNotRepo(t *testing.T) {
	env := fakeEnv(&fakeRunner{}, nil)
	if _, err := RepoRoot(context.Background(), env, native("/tmp/x")); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
	y := native("/tmp/y")
	r := &fakeRunner{out: map[string]string{"git -C " + y + " rev-parse --show-toplevel": "\n"}}
	if _, err := RepoRoot(context.Background(), fakeEnv(r, nil), y); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("empty output: err = %v, want ErrNotRepo", err)
	}
	if len(r.calls) != 1 || r.calls[0] != "git -C "+y+" rev-parse --show-toplevel" {
		t.Fatalf("empty output: git ran %q", r.calls)
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
