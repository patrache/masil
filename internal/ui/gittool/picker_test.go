package gittool_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/gittool"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	_ "github.com/patrache/masil/internal/ui/tools" // the hub and ctrl+t
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // home/channel stand-ins beside the tool surface
	os.Exit(m.Run())
}

// lgBin is where the fake finds lazygit.
var lgBin = native("/usr/local/bin/lazygit")

// native turns a slash fixture path ("/r/masil") into an absolute path of
// this OS: itself on Unix; on Windows the same path on a fixed drive
// (C:\r\masil). lazygit.Locate and RepoRoot make paths absolute, and on
// Windows a rooted path without a drive is not: the runner's current drive
// would go in front and the fixtures would match nothing. Other paths (a
// real temp folder) stay as they are.
func native(p string) string {
	if runtime.GOOS == "windows" && strings.HasPrefix(p, "/") {
		return "C:" + filepath.FromSlash(p)
	}
	return p
}

// goldenName is name, with "_windows" on Windows: the running pane draws
// lazygit's absolute path, which is C:\usr\local\bin\lazygit there.
func goldenName(name string) string {
	if runtime.GOOS == "windows" {
		return name + "_windows"
	}
	return name
}

// runner answers "name arg…" from out; anything else fails like git outside
// a repository.
type runner struct {
	out   map[string]string
	calls *[]string
}

func (r runner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	k := strings.Join(append([]string{name}, args...), " ")
	if r.calls != nil {
		*r.calls = append(*r.calls, k)
	}
	if v, ok := r.out[k]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("exit status 128")
}

// fakeEnv: lazygit installed (or not) on goos; repos are git repositories.
func fakeEnv(installed bool, goos string, repos ...string) (lazygit.Env, runner) {
	r := runner{out: map[string]string{
		lgBin + " --version": "commit=abc, build date=2026-11-07T00:00:00Z, build source=homebrew, version=0.66.3, os=darwin, arch=arm64, git version=2.47.1\n",
	}, calls: new([]string)}
	for _, repo := range repos {
		// Git for Windows answers with forward slashes ("C:/r/masil").
		repo = native(repo)
		r.out["git -C "+repo+" rev-parse --show-toplevel"] = filepath.ToSlash(repo) + "\n"
	}
	return lazygit.Env{
		LookPath: func(name string) (string, error) {
			if installed && name == "lazygit" {
				return lgBin, nil
			}
			return "", exec.ErrNotFound
		},
		Getenv: func(string) string { return "" },
		Run:    r,
		GOOS:   goos,
	}, r
}

// setup turns the git flag on and uses env; the harness is built by the caller.
func setup(t *testing.T, env lazygit.Env) {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "git")
	t.Cleanup(gittool.SetEnvForTest(env))
	termpanetest.Install(t)
}

func setRecent(t *testing.T, h *uitest.Harness, recent ...string) {
	t.Helper()
	if err := h.Context().ToolState.SetSection("git", map[string]any{"recent": recent}); err != nil {
		t.Fatal(err)
	}
}

// hubRow is the hub's line for lazygit, once its lookup (started by the
// first draw, off the UI goroutine) is in.
func hubRow(t *testing.T, h *uitest.Harness) string {
	t.Helper()
	settle(t, h)
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, "4 🌿 lazygit") {
			return l
		}
	}
	t.Fatalf("no lazygit hub row:\n%s", h.Screen())
	return ""
}

// settle draws once (the hub starts its lazygit lookup) and waits for the
// lookup to finish.
func settle(t *testing.T, h *uitest.Harness) {
	t.Helper()
	_ = h.Screen()
	gittool.WaitProbeForTest(t)
}

func toolIDs(h *uitest.Harness) []string {
	var ids []string
	for _, r := range h.Model.Nav().Tools {
		if r.Kind == ui.RouteTool {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func TestHubStatusInstalled(t *testing.T) {
	env, _ := fakeEnv(true, "darwin")
	setup(t, env)
	h := uitest.New(t)
	setRecent(t, h, native("/r/masil"), native("/r/blog"))
	h.Press("ctrl+t")
	if row := hubRow(t, h); !strings.Contains(row, "설치됨 v0.66.3 · 최근 masil") {
		t.Fatalf("hub row = %q", row)
	}
}

func TestHubStatusInstalledNoRecent(t *testing.T) {
	env, _ := fakeEnv(true, "darwin")
	setup(t, env)
	h := uitest.New(t)
	h.Press("ctrl+t")
	if row := hubRow(t, h); !strings.Contains(row, "설치됨 v0.66.3") || strings.Contains(row, "최근") {
		t.Fatalf("hub row = %q", row)
	}
}

func TestHubStatusMissing(t *testing.T) {
	env, _ := fakeEnv(false, "darwin")
	setup(t, env)
	h := uitest.New(t)
	h.Press("ctrl+t")
	if row := hubRow(t, h); !strings.Contains(row, "설치 필요 · enter 안내") {
		t.Fatalf("hub row = %q", row)
	}
}

func TestMissingOpensInstallDialog(t *testing.T) {
	env, _ := fakeEnv(false, "darwin")
	setup(t, env)
	h := uitest.New(t)
	h.Press("ctrl+t", "4")
	s := h.Screen()
	if !strings.Contains(s, "brew install lazygit") || !strings.Contains(s, "lazygit 설치 안내") {
		t.Fatalf("no install dialog:\n%s", s)
	}
	if ids := toolIDs(h); len(ids) != 0 {
		t.Fatalf("a tool instance opened: %v", ids)
	}
	h.Press("y")
	clips := h.Clipboards()
	if len(clips) == 0 || clips[len(clips)-1] != "brew install lazygit" {
		t.Fatalf("clipboard = %q", clips)
	}
	h.Press("esc")
	if strings.Contains(h.Screen(), "brew install lazygit") {
		t.Fatal("esc did not close the dialog")
	}
}

func TestInstallDialogWindowsTwoCommands(t *testing.T) {
	env, _ := fakeEnv(false, "windows")
	setup(t, env)
	h := uitest.New(t)
	h.Press("ctrl+t", "4")
	s := h.Screen()
	for _, c := range lazygit.InstallHint("windows") {
		if !strings.Contains(s, c) {
			t.Fatalf("no %q:\n%s", c, s)
		}
	}
	h.Press("down", "y")
	if clips := h.Clipboards(); len(clips) == 0 || clips[len(clips)-1] != "scoop install lazygit" {
		t.Fatalf("clipboard = %q", clips)
	}
}

func TestPickerRecentFirst(t *testing.T) {
	env, _ := fakeEnv(true, "darwin", native("/r/masil"), native("/r/blog"))
	setup(t, env)
	h := uitest.New(t)
	setRecent(t, h, native("/r/masil"), native("/r/blog"))
	h.Press("ctrl+t", "4")
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:pick"}) {
		t.Fatalf("instances = %v, want the picker", ids)
	}
	s := h.Screen()
	if !strings.Contains(s, "› masil") {
		t.Fatalf("masil is not the selected first row:\n%s", s)
	}
	if i, j := strings.Index(s, "masil "), strings.Index(s, "blog "); i < 0 || j < 0 || i > j {
		t.Fatalf("order wrong (masil %d, blog %d):\n%s", i, j, s)
	}
	if !strings.Contains(s, "경로 직접 입력") {
		t.Fatalf("no path row:\n%s", s)
	}
	h.Press("enter")
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil")}) {
		t.Fatalf("after enter: instances = %v, want git:/r/masil only", ids)
	}
}

func TestPickerNotRepo(t *testing.T) {
	env, _ := fakeEnv(true, "darwin", native("/r/masil"))
	setup(t, env)
	h := uitest.New(t)
	setRecent(t, h, native("/r/masil"))
	h.Press("ctrl+t", "4", "G", "enter")
	h.Type("/tmp/x")
	h.Press("enter")
	if s := h.Screen(); !strings.Contains(s, "git 저장소가 아니에요") {
		t.Fatalf("no not-a-repo line:\n%s", s)
	}
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:pick"}) {
		t.Fatalf("instances = %v, want the picker only", ids)
	}
	// esc goes back to the list; the error clears on the next choice.
	h.Press("esc")
	h.Press("g", "enter")
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil")}) {
		t.Fatalf("instances = %v", ids)
	}
}

func TestPickerTabCompletes(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"masil-one", "other"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "masil-file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "masil-one")
	env, _ := fakeEnv(true, "darwin", repo)
	setup(t, env)
	h := uitest.New(t)
	h.Press("ctrl+t", "4", "enter") // no recents: the path row is selected
	h.Type(filepath.Join(dir, "ma"))
	h.Press("tab", "enter")
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + repo}) {
		t.Fatalf("instances = %v, want git:%s:\n%s", ids, repo, h.Screen())
	}
}

func TestConfiguredRepoSkipsPicker(t *testing.T) {
	env, _ := fakeEnv(true, "darwin", native("/r/conf"))
	setup(t, env)
	h := uitest.New(t)
	h.Context().Config.Tools = map[string]map[string]any{"lazygit": {"repo": native("/r/conf")}}
	h.Press("ctrl+t", "4")
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/conf")}) {
		t.Fatalf("instances = %v", ids)
	}
}

var leetSizes = append(slices.Clone(uitest.Required), uitest.Size{W: 120, H: 30}, uitest.Size{W: 40, H: 14})

func TestGitGolden(t *testing.T) {
	t.Run("picker", func(t *testing.T) {
		env, _ := fakeEnv(true, "darwin", native("/r/masil"), native("/r/blog"))
		setup(t, env)
		uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
			setRecent(t, h, "/Users/sam/src/masil", "/Users/sam/src/blog", "/Users/sam/work/terminal-sns")
			h.Press("ctrl+t", "4")
			h.Golden("git_picker")
		})
	})
	t.Run("install", func(t *testing.T) {
		env, _ := fakeEnv(false, "darwin")
		setup(t, env)
		uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
			h.Press("ctrl+t")
			settle(t, h) // the hub row behind the dialog shows the lookup's answer
			h.Press("4")
			h.Golden("git_install")
		})
	})
}

func TestGitIntegrityHostile(t *testing.T) {
	env, _ := fakeEnv(true, "linux")
	setup(t, env)
	hostile := []string{"/r/\x1b[31mevil", "/r/\u202emasil\u202c", "/r/" + strings.Repeat("긴폴더", 30), "/r/\x07bell\tname"}
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		setRecent(t, h, hostile...)
		h.Press("ctrl+t")
		h.Check()
		h.Press("4")
		h.Check()
		h.Press("G", "enter")
		h.Type("/tmp/\x1b[2Jx")
		h.Check()
		h.Press("enter")
		h.Check()
	})
	envMissing, _ := fakeEnv(false, "linux")
	t.Cleanup(gittool.SetEnvForTest(envMissing))
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		h.Press("ctrl+t", "4")
		h.Check()
	})
}

func TestPaletteHasGitOpenOnlyWithFlag(t *testing.T) {
	env, _ := fakeEnv(true, "darwin")
	setup(t, env)
	h := uitest.New(t)
	items := palette.ItemsForTest(h.Context(), "lazygit")
	if !slices.ContainsFunc(items, func(it palette.Item) bool { return it.Command != nil && it.Command.ID == "git.open" }) {
		t.Fatalf("flag on: no git.open in %+v", items)
	}
	h.Send(ui.RunCommand("git.open", "")())
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:pick"}) {
		t.Fatalf("git.open: instances = %v", ids)
	}

	t.Setenv("MASIL_FEATURES", "")
	if items := palette.ItemsForTest(h.Context(), "lazygit"); len(items) != 0 {
		t.Fatalf("flag off: items = %+v", items)
	}
}
