package share_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// flipIgnorer stands for ignore rules masil cannot watch as files (a parent
// repository's .gitignore, core.excludesFile, the gitdir of a gitfile
// worktree): it hides the paths in hide and counts its calls.
type flipIgnorer struct {
	mu    sync.Mutex
	hide  map[string]bool
	calls int
}

func (f *flipIgnorer) Ignored(rels []string, _ []bool) ([]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	out := make([]bool, len(rels))
	for i, rel := range rels {
		out[i] = f.hide[rel]
	}
	return out, nil
}

func (f *flipIgnorer) set(rel string) {
	f.mu.Lock()
	f.hide[rel] = true
	f.mu.Unlock()
}

func (f *flipIgnorer) take() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.calls
	f.calls = 0
	return n
}

// assertClosedFor checks that every seat got rel closed as ignored and no
// longer holds it.
func (d *docRig) assertClosedFor(rel string, seats ...uint8) {
	d.t.Helper()
	for _, s := range seats {
		if _, ok := d.guests[s].DocText(rel); ok {
			d.t.Errorf("seat %d still has %s open", s, rel)
		}
		closed := false
		for _, ev := range d.takeGuest(s) {
			if st, ok := ev.(share.EventDocState); ok && st.Doc == rel && st.Error == share.DocErrIgnored {
				closed = true
			}
		}
		if !closed {
			d.t.Errorf("seat %d got no doc_state closing %s as ignored", s, rel)
		}
	}
}

// v0.1.3 security (af): a rule that hides an open document without any rule
// file inside the folder changing still closes it on the next DiskPoll
// (fail closed), and the owner's later edits are not relayed.
func TestOutsideRuleClosesPeerDocNextPoll(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1, 2)
	ig := &flipIgnorer{hide: map[string]bool{}}
	d.root.SetIgnorer(ig)
	_, err := d.docs.Open(hubTS, false)
	must(t, err)
	d.open(hubTS, 1, 2)
	got := d.captureBroadcasts(1)
	ig.set(hubTS)
	d.advance(share.DiskPoll)
	text, seq, _ := d.docs.Text(hubTS)
	_, _, err = d.docs.Submit(hubTS, 0, "", seq, insertAt(text, 0, "PROD_PASSWORD=hunter2\n"))
	must(t, err)
	d.settle()
	assertNoBroadcast(t, got(), "hunter2")
	d.assertClosedFor(hubTS, 1, 2)
}

// The re-check is one batched ignore-rule question per DiskPoll for all open
// documents, none for edits between polls, and none while no participant
// has a document open.
func TestRecheckBatchedPerPoll(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	ig := &flipIgnorer{hide: map[string]bool{}}
	d.root.SetIgnorer(ig)
	d.advance(share.DiskPoll)
	if n := ig.take(); n != 0 {
		t.Fatalf("%d ignore-rule calls with nothing open", n)
	}
	for _, rel := range []string{hubTS, "src/index.ts", "README.md"} {
		d.open(rel, 1)
	}
	d.advance(share.DiskPoll)
	ig.take()
	for i := range 10 {
		text := d.guestText(1, hubTS)
		must(t, d.guests[1].Edit(hubTS, insertAt(text, 0, string(rune('a'+i)))))
		d.settle()
	}
	if n := ig.take(); n != 0 {
		t.Fatalf("%d ignore-rule calls for edits between polls", n)
	}
	d.advance(share.DiskPoll)
	if n := ig.take(); n != 1 {
		t.Fatalf("%d ignore-rule calls for one poll of three documents, want 1", n)
	}
}

// gitRig turns the rig's folder into what setup makes of it and asks the
// real git for the ignore rules, with no user or system git config.
func gitRig(t *testing.T, seats ...uint8) *docRig {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	empty := filepath.Join(t.TempDir(), "gitconfig")
	must(t, os.WriteFile(empty, nil, 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	d := newDocRig(t, wire.LevelViewer, seats...)
	// copyProj's .git is a stand-in folder; the cases below make real ones.
	must(t, os.RemoveAll(filepath.Join(d.dir, ".git")))
	return d
}

func git(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func appendFile(t *testing.T, p, s string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	must(t, err)
	_, err = f.WriteString(s)
	must(t, err)
	must(t, f.Close())
}

// Each rule source outside the shared folder that git applies: hiding an
// open document there closes it for the participant on the next poll.
func TestOutsideGitRuleSourcesClosePeerDoc(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string) (hide func()){
		"core.excludesFile": func(t *testing.T, dir string) func() {
			git(t, "init", "-q", dir)
			excl := filepath.Join(t.TempDir(), "excludes")
			git(t, "-C", dir, "config", "core.excludesFile", excl)
			return func() { appendFile(t, excl, "src/server/\n") }
		},
		"gitdir of a gitfile": func(t *testing.T, dir string) func() {
			gitdir := filepath.Join(t.TempDir(), "repo.git")
			git(t, "init", "-q", "--separate-git-dir", gitdir, dir)
			return func() { appendFile(t, filepath.Join(gitdir, "info", "exclude"), "src/server/\n") }
		},
		"repository top above the folder": func(t *testing.T, dir string) func() {
			top := filepath.Dir(dir)
			git(t, "init", "-q", top)
			return func() { appendFile(t, filepath.Join(top, ".gitignore"), filepath.Base(dir)+"/src/server/\n") }
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			d := gitRig(t, 1)
			hide := setup(t, d.dir)
			d.root.SetIgnorer(fsroot.NewGitIgnorer(d.dir, nil))
			d.open(hubTS, 1)
			d.advance(share.DiskPoll)
			if _, ok := d.guests[1].DocText(hubTS); !ok {
				t.Fatal("document closed before any rule changed")
			}
			d.takeGuest(1)
			hide()
			d.advance(share.DiskPoll)
			d.assertClosedFor(hubTS, 1)
		})
	}
}

// A link retargeted at an ignored file with the same bytes: the watcher sees
// no change and nothing is merged, yet the per-poll re-check follows the
// link and closes it (owner list item 3 of the security-fix log).
func TestLinkRetargetedToSameContentCloses(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1)
	link := filepath.Join(d.dir, "src", "notes.txt")
	if err := os.Symlink("../README.md", link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	d.open("src/notes.txt", 1)
	d.takeGuest(1)
	b, err := os.ReadFile(filepath.Join(d.dir, "README.md"))
	must(t, err)
	must(t, os.WriteFile(filepath.Join(d.dir, "node_modules", "same.md"), b, 0o644))
	must(t, os.Remove(link))
	must(t, os.Symlink("../node_modules/same.md", link))
	d.advance(share.DiskPoll)
	d.assertClosedFor("src/notes.txt", 1)
}
