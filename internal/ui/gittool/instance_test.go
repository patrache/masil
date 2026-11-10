package gittool_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/gittool"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	"github.com/patrache/masil/internal/ui/uitest"
)

var (
	macEnv     = termhost.Env{GOOS: "darwin", TERM: "xterm-256color", Embedded: true, ToolOpen: termhost.ModeAuto}
	windowsEnv = termhost.Env{GOOS: "windows", TERM: "xterm-256color", Embedded: true, ToolOpen: termhost.ModeAuto}
)

// running sets up an installed lazygit with recents and repos, the fake
// launcher deciding in tenv (the tool predicts in the same env).
func running(t *testing.T, tenv termhost.Env, repos ...string) (*termpanetest.Fake, lazygit.Env) {
	t.Helper()
	env, _ := fakeEnv(true, "darwin", repos...)
	t.Setenv("MASIL_FEATURES", "git")
	t.Cleanup(gittool.SetEnvForTest(env))
	f := termpanetest.Install(t)
	if tenv != macEnv { // the fake's default is macOS with tiny from the layout
		f.SetEnv(tenv)
	}
	t.Cleanup(gittool.SetTermEnvForTest(tenv))
	return f, env
}

// firstStarted is the first request the fake launcher got (Fatal, not an
// index panic that would end the whole package run, when there is none).
func firstStarted(t *testing.T, f *termpanetest.Fake) termhost.Request {
	t.Helper()
	got := f.Started()
	if len(got) == 0 {
		t.Fatal("nothing was started")
	}
	return got[0]
}

func openMasil(t *testing.T, h *uitest.Harness) {
	t.Helper()
	setRecent(t, h, native("/r/masil"), native("/r/blog"))
	h.Press("ctrl+t", "4", "enter")
}

func TestOpenTwoPresses(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"))
	h := uitest.New(t)
	openMasil(t, h)
	got := f.Started()
	if len(got) != 1 {
		t.Fatalf("Started = %+v", got)
	}
	req := got[0]
	if req.Program != lgBin || len(req.Args) < 2 || !slices.Equal(req.Args[:2], []string{"-p", native("/r/masil")}) {
		t.Fatalf("request = %+v", req)
	}
	if req.ToolID != "git" || req.Title != "lazygit" || req.Dir != native("/r/masil") {
		t.Fatalf("request = %+v", req)
	}
	for _, e := range req.Env {
		if strings.HasPrefix(e, "LEETCODE") {
			t.Fatalf("secret-looking env %q", e)
		}
	}
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil")}) {
		t.Fatalf("instances = %v", ids)
	}
	if s := h.Screen(); !strings.Contains(s, "lazygit pane") {
		t.Fatalf("no pane:\n%s", s)
	}
	// Opening the same repository again shows the running instance.
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	h.Press("4", "enter")
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil")}) {
		t.Fatalf("instances = %v", ids)
	}
	if n := len(f.Started()); n != 1 {
		t.Fatalf("Started = %d after showing it again", n)
	}
}

func TestPaneThemeOverlay(t *testing.T) {
	cfgDir := t.TempDir()
	userCfg := filepath.Join(cfgDir, "config.yml")
	if err := os.WriteFile(userCfg, []byte("keybinding: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withCd := func(t *testing.T) {
		env, r := fakeEnv(true, "darwin", native("/r/masil"), native("/r/blog"))
		r.out[lgBin+" -cd"] = cfgDir + "\n"
		t.Cleanup(gittool.SetEnvForTest(env))
	}

	t.Run("pane", func(t *testing.T) {
		f, _ := running(t, macEnv)
		withCd(t)
		h := uitest.New(t)
		openMasil(t, h)
		req := firstStarted(t, f)
		i := slices.Index(req.Args, "--use-config-file")
		if i < 0 || i+1 >= len(req.Args) {
			t.Fatalf("args = %q", req.Args)
		}
		files := strings.Split(req.Args[i+1], ",")
		if len(files) != 2 || files[0] != userCfg || !strings.Contains(files[1], "theme-") {
			t.Fatalf("config files = %q", files)
		}
	})
	t.Run("exec", func(t *testing.T) {
		f, _ := running(t, windowsEnv)
		withCd(t)
		h := uitest.New(t)
		openMasil(t, h)
		req := firstStarted(t, f)
		if slices.Contains(req.Args, "--use-config-file") {
			t.Fatalf("exec args = %q", req.Args)
		}
		if s := h.Screen(); !strings.Contains(s, "화면 전환으로 실행 중") {
			t.Fatalf("no exec line:\n%s", s)
		}
	})
	t.Run("overlay off", func(t *testing.T) {
		f, _ := running(t, macEnv)
		withCd(t)
		h := uitest.New(t)
		h.Context().Config.Tools = map[string]map[string]any{"lazygit": {"theme_overlay": false}}
		openMasil(t, h)
		if req := firstStarted(t, f); slices.Contains(req.Args, "--use-config-file") {
			t.Fatalf("overlay off: args = %q", req.Args)
		}
	})
}

func TestExitedReopen(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"))
	h := uitest.New(t)
	openMasil(t, h)
	id := "git:" + native("/r/masil")
	h.Send(f.Exit(id, 0))
	if s := h.Screen(); !strings.Contains(s, "종료됨 · enter 다시 열기") {
		t.Fatalf("no exited line:\n%s", s)
	}
	h.Press("enter")
	if n := len(f.Started()); n != 2 {
		t.Fatalf("Started = %d after enter, want 2", n)
	}
	if s := h.Screen(); !strings.Contains(s, "lazygit pane") {
		t.Fatalf("not running again:\n%s", s)
	}
	h.Send(f.Exit(id, 0))
	h.Press("x")
	if ids := toolIDs(h); len(ids) != 0 {
		t.Fatalf("x: instances = %v", ids)
	}
	// Another instance's exit is not ours.
	h2 := uitest.New(t)
	openMasil(t, h2)
	h2.Send(f.Exit("git:"+native("/r/blog"), 0))
	if s := h2.Screen(); strings.Contains(s, "종료됨") {
		t.Fatalf("someone else's exit ended us:\n%s", s)
	}
}

func TestPaneLimitToast(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"))
	f.SetLimit(0)
	h := uitest.New(t)
	openMasil(t, h)
	s := h.Screen()
	if !strings.Contains(s, "패널은 동시에 0개까지예요") {
		t.Fatalf("no pane-limit toast:\n%s", s)
	}
	if !strings.Contains(s, "경로 직접 입력") || !strings.Contains(s, "masil") {
		t.Fatalf("not back to picking:\n%s", s)
	}
	if ids := toolIDs(h); !slices.Equal(ids, []string{"git:" + native("/r/masil")}) {
		t.Fatalf("instances = %v", ids)
	}
	// Choosing again once a pane is free launches in place.
	f.SetLimit(4)
	h.Press("enter")
	if n := len(f.Started()); n != 2 {
		t.Fatalf("Started = %d", n)
	}
	if s := h.Screen(); !strings.Contains(s, "lazygit pane") {
		t.Fatalf("not running:\n%s", s)
	}
}

func TestCwdUpdatesRecent(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"))
	h := uitest.New(t)
	setRecent(t, h, native("/r/masil"))
	h.Press("ctrl+t", "4", "enter")
	h.Send(f.Cwd("git:"+native("/r/masil"), native("/r/blog")))
	var st struct {
		Recent []string `json:"recent"`
	}
	if !h.Context().ToolState.Section("git", &st) || len(st.Recent) == 0 || st.Recent[0] != native("/r/blog") {
		t.Fatalf("recent = %v", st.Recent)
	}
	// A folder outside any repository changes nothing; another instance's cwd neither.
	h.Send(f.Cwd("git:"+native("/r/masil"), "/tmp/none"))
	h.Send(f.Cwd("git:"+native("/r/other"), native("/r/masil")))
	h.Context().ToolState.Section("git", &st)
	if !slices.Equal(st.Recent, []string{native("/r/blog"), native("/r/masil")}) {
		t.Fatalf("recent = %v", st.Recent)
	}
}

func TestLaunchPushesRecent(t *testing.T) {
	_, _ = running(t, macEnv, native("/r/conf"))
	h := uitest.New(t)
	h.Context().Config.Tools = map[string]map[string]any{"lazygit": {"repo": native("/r/conf")}}
	h.Press("ctrl+t", "4")
	var st struct {
		Recent []string `json:"recent"`
	}
	h.Context().ToolState.Section("git", &st)
	if !slices.Equal(st.Recent, []string{native("/r/conf")}) {
		t.Fatalf("recent = %v", st.Recent)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	if s := h.Screen(); !strings.Contains(s, "lazygit · conf") || !strings.Contains(s, "패널 · 실행 중") {
		t.Fatalf("hub open list:\n%s", s)
	}
}

func TestGitRunningGolden(t *testing.T) {
	running(t, macEnv, native("/r/masil"), native("/r/blog"))
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		openMasil(t, h)
		h.Golden(goldenName("git_running"))
	})
}
