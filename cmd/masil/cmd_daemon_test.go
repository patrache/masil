package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/autostart"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/daemon"
	"github.com/patrache/masil/internal/instance"
)

// daemonTestEnv is an Env on a temp config dir (config.toml loaded from it) with
// captured stdout/stderr. MASIL_SERVER is left as the test set it.
func daemonTestEnv(t *testing.T) (*Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", t.TempDir())
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return &Env{Paths: p, Config: cfg, Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr}, stdout, stderr
}

// stubDaemonFns saves every *Fn seed, replaces them with inert fakes (so no test
// touches the real launchd/systemd/registry or starts a daemon), and restores
// the originals on cleanup.
func stubDaemonFns(t *testing.T) {
	t.Helper()
	run, inst, uninst, query, exe, detach, now, goos :=
		daemonRunFn, autostartInstallFn, autostartUninstallFn, autostartQueryFn, executableFn, detachFn, nowFn, daemonGOOS
	t.Cleanup(func() {
		daemonRunFn, autostartInstallFn, autostartUninstallFn, autostartQueryFn, executableFn, detachFn, nowFn, daemonGOOS =
			run, inst, uninst, query, exe, detach, now, goos
	})
	daemonRunFn = func(context.Context, daemon.Options) error { return nil }
	autostartInstallFn = func(string, config.Paths) error { return nil }
	autostartUninstallFn = func() error { return nil }
	autostartQueryFn = func() (autostart.Status, error) {
		return autostart.Status{Installed: true, Detail: "launchd /fake/com.patrache.masil.daemon.plist"}, nil
	}
	executableFn = func() (string, error) { return "/usr/local/bin/masil", nil }
	detachFn = func([]string) (bool, error) { return false, nil }
	nowFn = func() time.Time { return time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC) }
}

func withMismatch(r daemon.Report, credServer string) daemon.Report {
	r.CredServer, r.ServerMismatch = credServer, true
	return r
}

func withShellServer(r daemon.Report, s string) daemon.Report {
	r.ShellServer = s
	return r
}

func withDesktop(r daemon.Report, on bool) daemon.Report {
	r.DesktopEnabled = on
	return r
}

func withProblems(r daemon.Report, lines ...string) daemon.Report {
	r.Problems = append([]string(nil), lines...)
	return r
}

func TestDaemonStatus(t *testing.T) {
	seoul, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	st := &daemon.State{Version: 1, UserID: "01JA0000000000000000000SAM", Epoch: "E1", LastSeq: 1041, SavedAt: now.Add(-time.Minute)}
	ok := daemon.Report{Running: true, LoggedIn: true, Handle: "sam", CredServer: "http://localhost:8787", ConfigServer: "http://localhost:8787", DesktopEnabled: true, State: st}
	installed := autostart.Status{Installed: true, Running: true, Detail: "launchd /Users/sam/Library/LaunchAgents/com.patrache.masil.daemon.plist"}
	cases := map[string]struct {
		r     daemon.Report
		as    autostart.Status
		asErr error
	}{
		"ok":       {ok, installed, nil},
		"fresh":    {daemon.Report{DesktopEnabled: true, ConfigServer: "http://localhost:8787"}, autostart.Status{}, nil},
		"mismatch": {withMismatch(ok, "http://127.0.0.1:9999"), installed, nil},
		// logged in via MASIL_SERVER=127.0.0.1:9999 in this shell; the daemon uses the default
		"shell_env":   {withShellServer(withMismatch(ok, "http://127.0.0.1:9999"), "http://127.0.0.1:9999"), installed, nil},
		"desktop_off": {withDesktop(ok, false), installed, nil},
		"stale_exe":   {ok, autostart.Status{Installed: true, Detail: installed.Detail + " · 실행 파일 없음: /opt/homebrew/Cellar/masil/0.1.0/bin/masil"}, nil},
		"unsupported": {ok, autostart.Status{}, autostart.ErrUnsupported},
		"problems":    {withProblems(ok, `time=2026-11-05T11:04:00+09:00 level=WARN msg="session rejected; waiting for new credentials" reason=unauthorized token="msl_…(redacted)"`), installed, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			renderStatus(&b, c.r, c.as, c.asErr, now, seoul)
			golden.RequireEqual(t, b.String())
		})
	}
}

// The ok golden is the plan's example, character for character.
func TestDaemonStatusOKMatchesSpec(t *testing.T) {
	seoul, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	r := daemon.Report{Running: true, LoggedIn: true, Handle: "sam", CredServer: "http://localhost:8787",
		ConfigServer: "http://localhost:8787", DesktopEnabled: true,
		State: &daemon.State{Version: 1, LastSeq: 1041, SavedAt: now.Add(-time.Minute)}}
	as := autostart.Status{Installed: true, Running: true, Detail: "launchd /Users/sam/Library/LaunchAgents/com.patrache.masil.daemon.plist"}
	var b strings.Builder
	renderStatus(&b, r, as, nil, now, seoul)
	want := "masil 알림 데몬\n" +
		"  자동 시작  등록됨 · launchd /Users/sam/Library/LaunchAgents/com.patrache.masil.daemon.plist\n" +
		"  실행       실행 중\n" +
		"  계정       @sam · http://localhost:8787\n" +
		"  알림       켜짐\n" +
		"  동기화     11:04 저장 · seq 1041\n"
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// Control characters in values read from disk (log lines, handle) never reach the terminal.
func TestDaemonStatusSanitizes(t *testing.T) {
	r := daemon.Report{LoggedIn: true, Handle: "sam\x1b[2J", CredServer: "http://x", ConfigServer: "http://x",
		Problems: []string{"time=x level=WARN msg=\"a\x1b]0;pwn\x07\""}}
	var b strings.Builder
	renderStatus(&b, r, autostart.Status{}, nil, time.Now(), time.UTC)
	if strings.ContainsAny(b.String(), "\x1b\x07") {
		t.Fatalf("control characters leaked: %q", b.String())
	}
}

// CA 2.4: usage errors exit 2 (not 1) through core's usage marker.
func TestDaemonUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"run", "--nope"}, {"status", "extra"}} {
		env, _, stderr := daemonTestEnv(t)
		if err := daemonCmd(t.Context(), env, args); !errors.Is(err, errUsage) {
			t.Fatalf("%v: err = %v, want errUsage", args, err)
		}
		if !strings.Contains(stderr.String(), "사용법: masil daemon run|install|uninstall|status") {
			t.Fatal(stderr.String())
		}
		env, _, _ = daemonTestEnv(t)
		if code := run(t.Context(), append([]string{"daemon"}, args...), env); code != 2 {
			t.Fatalf("%v: exit code %d, want 2", args, code)
		}
	}
	stubDaemonFns(t)
	executableFn = func() (string, error) { return "/var/folders/x/T/go-build99/b001/exe/masil", nil }
	env, _, _ := daemonTestEnv(t)
	if code := run(t.Context(), []string{"daemon", "install"}, env); code != 1 {
		t.Fatalf("runtime failure exit code %d, want 1", code)
	}
}

func TestDaemonRunAlreadyRunningExitsZero(t *testing.T) {
	stubDaemonFns(t) // restores every *Fn seed on cleanup
	daemonRunFn = func(context.Context, daemon.Options) error { return instance.ErrAlreadyRunning }
	env, stdout, _ := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--foreground"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "데몬이 이미 실행 중입니다.") {
		t.Fatal(stdout.String())
	}
}

// Upgrade path: a replaced binary must exit NON-zero so KeepAlive{SuccessfulExit:false} /
// Restart=on-failure relaunch the new version.
func TestDaemonRunRestartExitsNonZero(t *testing.T) {
	stubDaemonFns(t)
	daemonRunFn = func(context.Context, daemon.Options) error { return daemon.ErrRestartRequired }
	env, _, stderr := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--foreground"}); !errors.Is(err, daemon.ErrRestartRequired) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr.String(), "실행 파일이 바뀌어 종료합니다") {
		t.Fatal(stderr.String())
	}
	env, _, _ = daemonTestEnv(t)
	if code := run(t.Context(), []string{"daemon", "run", "--foreground"}, env); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
}

// run passes Paths, Config and a daemon.log logger; --verbose mirrors the log to stderr.
func TestDaemonRunOptions(t *testing.T) {
	stubDaemonFns(t)
	var got daemon.Options
	daemonRunFn = func(_ context.Context, o daemon.Options) error {
		got = o
		o.Logger.Info("realtime ready")
		return nil
	}
	detachCalled := false
	detachFn = func([]string) (bool, error) { detachCalled = true; return false, nil }
	env, _, stderr := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--foreground", "--verbose"}); err != nil {
		t.Fatal(err)
	}
	if detachCalled {
		t.Fatal("--foreground must not detach")
	}
	if got.Paths != env.Paths || got.Config != env.Config || got.Logger == nil {
		t.Fatalf("options = %+v", got)
	}
	if !strings.Contains(stderr.String(), "realtime ready") {
		t.Fatalf("--verbose must mirror the log to stderr: %q", stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(env.Paths.LogDir, "daemon.log"))
	if err != nil || !strings.Contains(string(b), "realtime ready") {
		t.Fatalf("daemon.log = %q, %v", b, err)
	}

	// without --verbose nothing goes to stderr
	env, _, stderr = daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run"}); err != nil {
		t.Fatal(err)
	}
	if !detachCalled || stderr.Len() != 0 {
		t.Fatalf("detach called=%v stderr=%q", detachCalled, stderr.String())
	}
}

// Windows: the parent re-launched itself hidden and returns at once.
func TestDaemonRunDetached(t *testing.T) {
	stubDaemonFns(t)
	var gotArgs []string
	detachFn = func(a []string) (bool, error) { gotArgs = a; return true, nil }
	ran := false
	daemonRunFn = func(context.Context, daemon.Options) error { ran = true; return nil }
	env, stdout, _ := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--verbose"}); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("a detached parent must not run the daemon itself")
	}
	if strings.Join(gotArgs, " ") != "daemon run --verbose" {
		t.Fatalf("detach args = %q", gotArgs)
	}
	want := "백그라운드에서 데몬을 시작했습니다 (로그: " + filepath.Join(env.Paths.LogDir, "daemon.log") + ")"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDaemonInstall(t *testing.T) {
	stubDaemonFns(t)
	t.Setenv("MASIL_SERVER", "")
	var gotExe string
	autostartInstallFn = func(exe string, _ config.Paths) error { gotExe = exe; return nil }
	executableFn = func() (string, error) { return "/var/folders/x/T/go-build99/b001/exe/masil", nil }
	env, _, _ := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"install"}); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Fatalf("go-build path must be refused: %v", err)
	}
	executableFn = func() (string, error) { return "/usr/local/bin/masil", nil }
	env, stdout, _ := daemonTestEnv(t) // not logged in, config.toml server unset
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if gotExe != "/usr/local/bin/masil" || !strings.Contains(stdout.String(), "'masil login' 뒤부터 알림이 옵니다") {
		t.Fatalf("exe=%q out=%q", gotExe, stdout.String())
	}
	if !strings.Contains(stdout.String(), "자동 시작을 등록했습니다: launchd /fake/com.patrache.masil.daemon.plist") {
		t.Fatalf("missing registration line: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "MASIL_SERVER") {
		t.Fatal("no shell override -> no warning")
	}

	// Review Focus 1: the shell override will not reach launchd/systemd.
	t.Setenv("MASIL_SERVER", "http://127.0.0.1:9999")
	env, stdout, _ = daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "셸의 MASIL_SERVER(http://127.0.0.1:9999)는 자동 시작된 데몬에 전달되지 않습니다") ||
		!strings.Contains(stdout.String(), "데몬은 http://localhost:8787 에 접속합니다") {
		t.Fatalf("missing MASIL_SERVER warning: %q", stdout.String())
	}

	// the same server with a trailing "/" is not a mismatch
	t.Setenv("MASIL_SERVER", "http://localhost:8787/")
	env, stdout, _ = daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "MASIL_SERVER") {
		t.Fatalf("trailing slash must not warn: %q", stdout.String())
	}
}

// README*.md's `MASIL_SERVER=… masil login` (inline, not exported) saves
// credentials for another server; a later install from a plain shell must warn
// that the autostarted daemon will never connect, like `masil daemon status`.
func TestDaemonInstallWarnsCredentialsServerMismatch(t *testing.T) {
	stubDaemonFns(t)
	t.Setenv("MASIL_SERVER", "")
	env, stdout, _ := daemonTestEnv(t)
	if err := auth.Save(env.Paths, &auth.Credentials{
		Version: 1, Server: "http://127.0.0.1:9999", Token: "msl_test_token_A", SessionID: "01JA7000000000000000000000",
		UserID: "01JA0000000000000000000SAM", Handle: "sam", SavedAt: time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"⚠ 로그인한 서버(http://127.0.0.1:9999)와 데몬이 쓰는 서버(http://localhost:8787)가 달라 알림을 받지 않습니다.",
		"같은 서버로 다시 로그인하거나 config.toml 의 server 를 맞추세요.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "masil login' 뒤부터") || strings.Contains(out, "msl_test_token_") {
		t.Fatalf("unexpected output:\n%s", out)
	}

	// Matching server: no warning.
	env, stdout, _ = daemonTestEnv(t)
	if err := auth.Save(env.Paths, &auth.Credentials{
		Version: 1, Server: "http://localhost:8787", Token: "msl_test_token_A", SessionID: "01JA7000000000000000000000",
		UserID: "01JA0000000000000000000SAM", Handle: "sam", SavedAt: time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "⚠") {
		t.Fatalf("matching server must not warn:\n%s", stdout.String())
	}
}

func TestDaemonInstallDesktopOff(t *testing.T) {
	stubDaemonFns(t)
	t.Setenv("MASIL_SERVER", "")
	env, stdout, _ := daemonTestEnv(t)
	env.Config.Notify.Desktop = false
	if err := daemonCmd(t.Context(), env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "config.toml 의 [notify] desktop 이 꺼져 있어 알림이 뜨지 않습니다.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDaemonInstallFailureExitsOne(t *testing.T) {
	stubDaemonFns(t)
	autostartInstallFn = func(string, config.Paths) error { return errors.New("autostart: launchctl bootstrap failed") }
	env, _, stderr := daemonTestEnv(t)
	if code := run(t.Context(), []string{"daemon", "install"}, env); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "launchctl bootstrap failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDaemonUninstall(t *testing.T) {
	stubDaemonFns(t)
	called := false
	autostartUninstallFn = func() error { called = true; return nil }
	for _, goos := range []string{"darwin", "windows"} {
		daemonGOOS = goos
		env, stdout, _ := daemonTestEnv(t)
		if err := daemonCmd(t.Context(), env, []string{"uninstall"}); err != nil {
			t.Fatal(err)
		}
		out := stdout.String()
		if !called || !strings.Contains(out, "자동 시작을 해제했습니다.") {
			t.Fatalf("%s: called=%v out=%q", goos, called, out)
		}
		if hasNote := strings.Contains(out, "지금 실행 중인 데몬은 다음 로그인부터 뜨지 않습니다."); hasNote != (goos == "windows") {
			t.Fatalf("%s: windows note = %v: %q", goos, hasNote, out)
		}
	}
}

// status wires Inspect + Query into renderStatus.
func TestDaemonStatusCommand(t *testing.T) {
	stubDaemonFns(t)
	t.Setenv("MASIL_SERVER", "")
	env, stdout, _ := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"masil 알림 데몬", "등록됨 · launchd /fake/com.patrache.masil.daemon.plist", "멈춤", "로그인 안 됨 — masil login", "기록 없음"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
}

// `masil help` lists the daemon in core's "masil <cmd> — <설명>" format.
func TestDaemonInHelp(t *testing.T) {
	if !strings.Contains(helpText(), "  masil daemon run|install|uninstall|status — 알림 데몬") {
		t.Fatalf("help:\n%s", helpText())
	}
}
