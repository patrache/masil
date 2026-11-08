package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/version"
)

// Hidden test commands, registered once for the whole test binary.
func init() {
	register(command{Name: "zz-hidden", Usage: "masil zz-hidden — 시험용", Hidden: true,
		Run: func(context.Context, *Env, []string) error { return nil }})
	register(command{Name: "zz-ok", Hidden: true,
		Run: func(context.Context, *Env, []string) error { return nil }})
	register(command{Name: "zz-fail", Hidden: true,
		Run: func(context.Context, *Env, []string) error { return errors.New("boom") }})
	register(command{Name: "zz-usage", Hidden: true,
		Run: func(context.Context, *Env, []string) error { return usageErr(nil) }})
	register(command{Name: "zz-wrapped", Hidden: true,
		Run: func(context.Context, *Env, []string) error {
			return fmt.Errorf("admin: %w", usageErr(errors.New("admincli: usage")))
		}})
}

// testEnv is an Env on a temp config dir with captured output.
type testEnv struct {
	*Env
	out, err *bytes.Buffer
}

func newEnv(t *testing.T, server string) *testEnv {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", t.TempDir())
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	t.Setenv("MASIL_SERVER", server)
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	return &testEnv{Env: &Env{Paths: p, Config: config.Defaults(), Stdin: strings.NewReader(""), Stdout: out, Stderr: errb}, out: out, err: errb}
}

func (e *testEnv) run(args ...string) int {
	e.out.Reset()
	e.err.Reset()
	return run(context.Background(), args, e.Env)
}

// fakeTUI replaces runTUI and records every call's Options.
func fakeTUI(t *testing.T) *[]app.Options {
	t.Helper()
	var calls []app.Options
	old := runTUI
	runTUI = func(_ context.Context, o app.Options) error {
		calls = append(calls, o)
		return nil
	}
	t.Cleanup(func() { runTUI = old })
	return &calls
}

func TestHelpListsVisibleCommands(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	if code := e.run("help"); code != 0 {
		t.Fatalf("help => %d, stderr %q", code, e.err)
	}
	out := e.out.String()
	for _, want := range []string{
		"masil [tui] — 터미널 마실을 열어요",
		"masil login [--handle <아이디> --password-stdin] — 로그인해요",
		"masil signup — 가입해요",
		"masil logout — 이 기기에서 로그아웃해요",
		"masil join <초대 코드> — 초대받은 방에 들어가요",
		"masil version — 버전을 보여 줘요",
		"masil config path — 설정 폴더 위치를 보여 줘요",
		"masil help — 도움말",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "zz-") {
		t.Errorf("hidden command listed:\n%s", out)
	}
	// core commands keep their fixed order: tui before login before ... help.
	if i, j := strings.Index(out, "masil [tui]"), strings.Index(out, "masil help"); i < 0 || j < i {
		t.Errorf("help order wrong:\n%s", out)
	}
	if e.run("--help") != 0 || !strings.Contains(e.out.String(), "masil login") {
		t.Errorf("--help should print the same list")
	}
}

func TestUnknownCommand(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	if code := e.run("nope"); code != 2 {
		t.Fatalf("nope => %d, want 2", code)
	}
	if s := e.err.String(); !strings.Contains(s, "알 수 없는 명령이에요: nope") || !strings.Contains(s, "masil help") {
		t.Fatalf("stderr = %q", s)
	}
	// Control bytes in the unknown name are never echoed raw.
	e.run("bad\x1b[31mname")
	if strings.Contains(e.err.String(), "\x1b") {
		t.Fatalf("raw escape echoed: %q", e.err.String())
	}
}

func TestVersion(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	ov, oc, od := version.Version, version.Commit, version.Date
	t.Cleanup(func() { version.Version, version.Commit, version.Date = ov, oc, od })
	version.Version, version.Commit, version.Date = "0.1.0", "abc", "2026-11-05"
	if code := e.run("version"); code != 0 {
		t.Fatalf("version => %d", code)
	}
	if got := e.out.String(); got != "masil 0.1.0 (abc, 2026-11-05)\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestConfigPath(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	d := os.Getenv("MASIL_CONFIG_DIR")
	if code := e.run("config", "path"); code != 0 {
		t.Fatalf("config path => %d", code)
	}
	if got := e.out.String(); got != d+"\n" {
		t.Fatalf("stdout = %q, want %q", got, d+"\n")
	}
	if code := e.run("config"); code != 2 {
		t.Fatalf("config => %d, want 2", code)
	}
	if !strings.Contains(e.err.String(), "masil config path") {
		t.Fatalf("config usage not printed: %q", e.err.String())
	}
}

func saveCreds(t *testing.T, p config.Paths, server string) *auth.Credentials {
	t.Helper()
	cr := &auth.Credentials{Version: 1, Server: server, Token: "msl_savedtoken", SessionID: "ses1", UserID: "u1", Handle: "sam", SavedAt: time.Now()}
	if err := auth.Save(p, cr); err != nil {
		t.Fatal(err)
	}
	return cr
}

func TestDefaultRunsTUI(t *testing.T) {
	const server = "https://masil.test"
	e := newEnv(t, server)
	calls := fakeTUI(t)

	if code := e.run(); code != 0 {
		t.Fatalf("no args => %d", code)
	}
	if code := e.run("tui"); code != 0 {
		t.Fatalf("tui => %d", code)
	}
	if len(*calls) != 2 {
		t.Fatalf("runTUI called %d times, want 2", len(*calls))
	}
	for _, o := range *calls {
		if o.Credentials != nil || o.InitialRoute != nil || o.Config != e.Config || o.Paths != e.Paths {
			t.Fatalf("logged-out options = %+v", o)
		}
	}

	saveCreds(t, e.Paths, server+"/")
	e.run()
	if o := (*calls)[2]; o.Credentials == nil || o.Credentials.Token != "msl_savedtoken" {
		t.Fatalf("matching server: credentials = %+v", o.Credentials)
	}

	saveCreds(t, e.Paths, "https://other.test")
	e.run("tui")
	if o := (*calls)[3]; o.Credentials != nil {
		t.Fatalf("credentials of another server must not be used: %+v", o.Credentials)
	}

	if code := e.run("tui", "extra"); code != 2 {
		t.Fatalf("tui extra => %d, want 2", code)
	}
	if len(*calls) != 4 {
		t.Fatalf("usage error must not start the TUI")
	}

	// A TUI failure is exit 1 and reported by the command itself.
	runTUI = func(context.Context, app.Options) error { return errors.New("lock busy") }
	if code := e.run(); code != 1 {
		t.Fatalf("failing TUI => %d, want 1", code)
	}
	if e.err.Len() == 0 {
		t.Fatalf("failing TUI should explain on stderr")
	}
}

func TestJoin(t *testing.T) {
	const server = "https://masil.test"
	e := newEnv(t, server)
	calls := fakeTUI(t)
	saveCreds(t, e.Paths, server)
	if code := e.run("join", "AbC12345"); code != 0 {
		t.Fatalf("join => %d, stderr %q", code, e.err)
	}
	if len(*calls) != 1 {
		t.Fatalf("runTUI calls = %d", len(*calls))
	}
	o := (*calls)[0]
	want := ui.Route{Kind: ui.RouteInvite, ID: "AbC12345"}
	if o.InitialRoute == nil || o.InitialRoute.Kind != want.Kind || o.InitialRoute.ID != want.ID || o.InitialRoute.Arg != "" {
		t.Fatalf("InitialRoute = %+v, want %+v", o.InitialRoute, want)
	}
	if o.Credentials == nil {
		t.Fatalf("join should load credentials")
	}
	if code := e.run("join"); code != 2 {
		t.Fatalf("join without code => %d, want 2", code)
	}
	if code := e.run("join", "a", "b"); code != 2 {
		t.Fatalf("join with two codes => %d, want 2", code)
	}
	if len(*calls) != 1 {
		t.Fatalf("usage errors must not start the TUI")
	}
}

// C5 (v0.1.1): `masil join <code>` without saved credentials still hands the
// invite route to the TUI (the app opens it after the login screen).
func TestJoinLoggedOut(t *testing.T) {
	e := newEnv(t, "https://masil.test")
	calls := fakeTUI(t)
	if code := e.run("join", "AbC12345"); code != 0 {
		t.Fatalf("join => %d, stderr %q", code, e.err)
	}
	if len(*calls) != 1 {
		t.Fatalf("runTUI calls = %d", len(*calls))
	}
	o := (*calls)[0]
	if o.Credentials != nil {
		t.Fatalf("no credentials saved, got %+v", o.Credentials)
	}
	if o.InitialRoute == nil || o.InitialRoute.Kind != ui.RouteInvite || o.InitialRoute.ID != "AbC12345" {
		t.Fatalf("InitialRoute = %+v, want the invite", o.InitialRoute)
	}
}

func authResult(token string) api.AuthResult {
	var r api.AuthResult
	r.Token = token
	r.Session.ID = "ses_new"
	r.User.ID = "u_sam"
	r.User.Handle = "sam"
	return r
}

func TestLoginPasswordStdin(t *testing.T) {
	srv := apitest.New(t)
	const token = "msl_supersecrettoken123"
	srv.JSON("POST /v1/auth/login", 200, authResult(token))
	e := newEnv(t, srv.URL)
	calls := fakeTUI(t)

	e.Stdin = strings.NewReader("secret pw\n")
	if code := e.run("login", "--handle", "Sam", "--password-stdin"); code != 0 {
		t.Fatalf("login => %d, stderr %q", code, e.err)
	}
	if len(*calls) != 0 {
		t.Fatalf("non-interactive login must not open the TUI")
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/v1/auth/login" {
		t.Fatalf("requests = %+v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["handle"] != "sam" || body["password"] != "secret pw" || body["client"] != "tui" {
		t.Fatalf("login body = %v", body)
	}
	cr, err := auth.Load(e.Paths)
	if err != nil {
		t.Fatalf("credentials not saved: %v", err)
	}
	if cr.Token != token || cr.Server != srv.URL || cr.Handle != "sam" || cr.UserID != "u_sam" || cr.SessionID != "ses_new" || cr.Version != 1 {
		t.Fatalf("saved credentials = %+v", cr)
	}
	if got := e.out.String(); got != "로그인했어요: @sam\n" {
		t.Fatalf("stdout = %q", got)
	}
	if strings.Contains(e.out.String()+e.err.String(), token) {
		t.Fatalf("token leaked to output")
	}

	// Windows line ending is stripped too, inner spaces kept.
	e.Stdin = strings.NewReader("secret pw\r\n")
	e.run("login", "--handle", "sam", "--password-stdin")
	if err := json.Unmarshal(srv.Requests()[1].Body, &body); err != nil || body["password"] != "secret pw" {
		t.Fatalf("CRLF password = %v (%v)", body["password"], err)
	}

	// Wrong password => 1, Korean message, credentials untouched.
	srv.Error("POST /v1/auth/login", 401, "invalid_credentials")
	e.Stdin = strings.NewReader("wrong\n")
	if code := e.run("login", "--handle", "sam", "--password-stdin"); code != 1 {
		t.Fatalf("401 => %d, want 1", code)
	}
	if s := e.err.String(); !strings.Contains(s, "아이디 또는 비밀번호가 맞지 않아요") {
		t.Fatalf("stderr = %q", s)
	}
	if strings.Contains(e.out.String()+e.err.String(), token) {
		t.Fatalf("token leaked to output")
	}

	// --password-stdin without --handle, or --handle alone => usage.
	e.Stdin = strings.NewReader("x\n")
	if code := e.run("login", "--password-stdin"); code != 2 {
		t.Fatalf("--password-stdin alone => %d, want 2", code)
	}
	if code := e.run("login", "--handle", "sam"); code != 2 {
		t.Fatalf("--handle alone => %d, want 2", code)
	}
	e.Stdin = strings.NewReader("")
	if code := e.run("login", "--handle", "sam", "--password-stdin"); code != 2 {
		t.Fatalf("empty stdin => %d, want 2", code)
	}
}

func TestLoginInteractiveOpensTUI(t *testing.T) {
	const server = "https://masil.test"
	e := newEnv(t, server)
	calls := fakeTUI(t)
	saveCreds(t, e.Paths, server) // even when logged in, `masil login` shows the form
	for i, tc := range []struct{ cmd, arg string }{{"login", "login"}, {"signup", "signup"}} {
		if code := e.run(tc.cmd); code != 0 {
			t.Fatalf("%s => %d, stderr %q", tc.cmd, code, e.err)
		}
		o := (*calls)[i]
		if o.Credentials != nil {
			t.Fatalf("%s: Credentials must be nil", tc.cmd)
		}
		if o.InitialRoute == nil || o.InitialRoute.Kind != ui.RouteLogin || o.InitialRoute.Arg != tc.arg {
			t.Fatalf("%s: InitialRoute = %+v", tc.cmd, o.InitialRoute)
		}
	}
	if code := e.run("signup", "x"); code != 2 {
		t.Fatalf("signup x => %d, want 2", code)
	}
}

func TestLogout(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("POST /v1/auth/logout", 204, nil)
	e := newEnv(t, srv.URL)
	saveCreds(t, e.Paths, srv.URL)
	if code := e.run("logout"); code != 0 {
		t.Fatalf("logout => %d, stderr %q", code, e.err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/v1/auth/logout" {
		t.Fatalf("requests = %+v", reqs)
	}
	if _, err := auth.Load(e.Paths); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("credentials not removed: %v", err)
	}
	if got := e.out.String(); got != "로그아웃했어요\n" {
		t.Fatalf("stdout = %q", got)
	}

	// Server unreachable: still removed locally, exit 0, warning on stderr.
	saveCreds(t, e.Paths, "http://127.0.0.1:1")
	if code := e.run("logout"); code != 0 {
		t.Fatalf("unreachable logout => %d", code)
	}
	if _, err := auth.Load(e.Paths); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("credentials not removed: %v", err)
	}
	if e.err.Len() == 0 || !strings.Contains(e.out.String(), "로그아웃했어요") {
		t.Fatalf("stdout %q stderr %q", e.out, e.err)
	}
	if strings.Contains(e.out.String()+e.err.String(), "msl_savedtoken") {
		t.Fatalf("token leaked")
	}

	// Not logged in.
	if code := e.run("logout"); code != 0 {
		t.Fatalf("logged-out logout => %d", code)
	}
	if got := e.out.String(); got != "로그인돼 있지 않아요\n" {
		t.Fatalf("stdout = %q", got)
	}
	if code := e.run("logout", "x"); code != 2 {
		t.Fatalf("logout x => %d, want 2", code)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering an existing core command must panic")
		}
	}()
	register(command{Name: "login", Run: func(context.Context, *Env, []string) error { return nil }})
}

func TestExitCodes(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	fakeTUI(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"zz-ok"}, 0},
		{[]string{"zz-fail"}, 1},
		{[]string{"zz-usage"}, 2},
		{[]string{"zz-wrapped"}, 2},
		{[]string{"config"}, 2},
		{[]string{"config", "nope"}, 2},
		{[]string{"join"}, 2},
		{[]string{"login", "--handle"}, 2},
		{[]string{"login", "--bogus"}, 2},
		{[]string{"nope"}, 2},
		{[]string{"login", "-h"}, 0},
		{[]string{"version", "--help"}, 0},
	} {
		if got := e.run(tc.args...); got != tc.want {
			t.Errorf("%v => %d, want %d (stderr %q)", tc.args, got, tc.want, e.err)
		}
		if strings.HasPrefix(tc.args[0], "zz-") && e.err.Len() != 0 {
			t.Errorf("%v: run re-printed the error: %q", tc.args, e.err)
		}
	}
	// -h prints the usage on stdout.
	e.run("login", "-h")
	if !strings.Contains(e.out.String(), "masil login") || e.err.Len() != 0 {
		t.Errorf("login -h: stdout %q stderr %q", e.out, e.err)
	}
	// A core usage error writes its usage line to stderr.
	e.run("login", "--handle")
	if !strings.Contains(e.err.String(), "masil login") {
		t.Errorf("login --handle stderr = %q", e.err)
	}
}

func TestOpenLogRotates(t *testing.T) {
	e := newEnv(t, "http://127.0.0.1:1")
	p := e.Paths
	logPath := filepath.Join(p.LogDir, "masil.log")
	old := bytes.Repeat([]byte("x"), 5<<20+1)
	if err := os.WriteFile(logPath, old, 0o644); err != nil {
		t.Fatal(err)
	}
	lg, closeLog, err := openLog(p, "masil")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(logPath + ".1")
	if err != nil || fi.Size() != int64(len(old)) {
		t.Fatalf("rotated file: %v %v", fi, err)
	}
	fi, err = os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Fatalf("new log size = %d, want 0", fi.Size())
	}
	if fi.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Fatalf("log mode = %v, want 0600", fi.Mode().Perm())
	}
	lg.Info("hello", "k", "v")
	closeLog()
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "hello") {
		t.Fatalf("log not written: %q", b)
	}

	// A small log is appended to, not rotated.
	if err := os.Remove(logPath + ".1"); err != nil {
		t.Fatal(err)
	}
	_, closeLog, err = openLog(p, "masil")
	if err != nil {
		t.Fatal(err)
	}
	closeLog()
	if _, err := os.Stat(logPath + ".1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("small log rotated: %v", err)
	}
	if b2, _ := os.ReadFile(logPath); !bytes.Equal(b, b2) {
		t.Fatalf("small log truncated")
	}
}
