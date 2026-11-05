package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/admincli"
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
)

// adminTestEnv is an Env on a temp config dir with default config and no credentials.
func adminTestEnv(t *testing.T) (env *Env, stdout, stderr *bytes.Buffer) {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", t.TempDir())
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	t.Setenv("MASIL_SERVER", "") // blank counts as unset: config decides
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	return &Env{Paths: p, Config: config.Defaults(), Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr}, stdout, stderr
}

// adminStubClient points adminClientFn at srv for the test.
func adminStubClient(t *testing.T, srv *apitest.Server) {
	t.Helper()
	old := adminClientFn
	adminClientFn = func(*Env) (*api.Client, error) { return srv.Client(), nil }
	t.Cleanup(func() { adminClientFn = old })
}

func TestAdminCmdNotLoggedIn(t *testing.T) {
	env, _, stderr := adminTestEnv(t)
	if err := adminCmd(context.Background(), env, []string{"reports"}); err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(stderr.String(), "로그인하지 않았습니다 — 먼저 'masil login'") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestAdminCmdServerMismatch(t *testing.T) {
	env, stdout, stderr := adminTestEnv(t)
	env.Config.Server = "http://localhost:8787"
	const token = "msl_admintoken_secret"
	cr := &auth.Credentials{Version: 1, Server: "http://127.0.0.1:9999", Token: token, SessionID: "s1", UserID: "u1", Handle: "sam", SavedAt: time.Now()}
	if err := auth.Save(env.Paths, cr); err != nil {
		t.Fatal(err)
	}
	err := adminCmd(context.Background(), env, []string{"reports"})
	if err == nil {
		t.Fatal("want error")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "http://127.0.0.1:9999") || !strings.Contains(msg, "http://localhost:8787") {
		t.Fatalf("stderr does not name both servers: %q", msg)
	}
	if strings.Contains(msg+stdout.String()+err.Error(), token) {
		t.Fatalf("token printed: stderr %q stdout %q err %v", msg, stdout, err)
	}
}

func TestAdminCmdRunsWithClient(t *testing.T) {
	env, stdout, stderr := adminTestEnv(t)
	srv := apitest.New(t)
	srv.JSON("GET /v1/admin/users", 200, api.Page[api.AdminUserView]{Items: []api.AdminUserView{}})
	adminStubClient(t, srv)
	if err := adminCmd(context.Background(), env, []string{"users"}); err != nil {
		t.Fatalf("adminCmd: %v (stderr %q)", err, stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Method != "GET" || reqs[0].Path != "/v1/admin/users" {
		t.Fatalf("requests = %+v", reqs)
	}
	if strings.Contains(stdout.String(), "msl_test") {
		t.Fatalf("token printed: %q", stdout)
	}
}

func TestAdminCmdUsesSavedCredentials(t *testing.T) {
	env, _, stderr := adminTestEnv(t)
	srv := apitest.New(t)
	srv.JSON("GET /v1/admin/users", 200, api.Page[api.AdminUserView]{Items: []api.AdminUserView{}})
	env.Config.Server = srv.URL
	cr := &auth.Credentials{Version: 1, Server: srv.URL, Token: "msl_saved", SessionID: "s1", UserID: "u1", Handle: "sam", SavedAt: time.Now()}
	if err := auth.Save(env.Paths, cr); err != nil {
		t.Fatal(err)
	}
	if err := adminCmd(context.Background(), env, []string{"users"}); err != nil {
		t.Fatalf("adminCmd: %v (stderr %q)", err, stderr)
	}
	if reqs := srv.Requests(); len(reqs) != 1 || reqs[0].Path != "/v1/admin/users" {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestAdminCmdUsageExitCode(t *testing.T) {
	env, _, stderr := adminTestEnv(t)
	srv := apitest.New(t)
	srv.Error("GET /v1/admin/reports", 403, "not_admin")
	adminStubClient(t, srv)

	err := adminCmd(context.Background(), env, []string{"bogus"})
	if !errors.Is(err, errUsage) || !errors.Is(err, admincli.ErrUsage) {
		t.Fatalf("err = %v, want both errUsage and admincli.ErrUsage", err)
	}
	if code := run(context.Background(), []string{"admin", "bogus"}, env); code != 2 {
		t.Fatalf("admin bogus => %d, want 2", code)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("bogus sent %d requests", n)
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"admin", "reports"}, env); code != 1 {
		t.Fatalf("admin reports (not_admin) => %d, want 1", code)
	}
	if stderr.String() != "운영자 권한이 없습니다.\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestAdminCmdHidden(t *testing.T) {
	c, ok := commands["admin"]
	if !ok || !c.Hidden {
		t.Fatalf("admin command registered=%v hidden=%v", ok, c.Hidden)
	}
}

// Help and usage errors are decided before credentials are loaded (CA 2.4, CR-D):
// logged out, `admin --help` prints usage on stdout and exits 0, and a bad or
// missing subcommand exits 2 without the login hint.
func TestAdminCmdHelpAndUsageWithoutCredentials(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"admin", "--help"}, 0},
		{[]string{"admin", "-h"}, 0},
		{[]string{"admin", "help"}, 0},
		{[]string{"admin", "users", "--help"}, 0},
		{[]string{"admin"}, 2},
		{[]string{"admin", "bogus"}, 2},
		{[]string{"admin", "users", "--nope"}, 2},
		{[]string{"admin", "suspend", "notahandle"}, 2},
		{[]string{"admin", "remove", "post", "01ARZ3NDEKTSV4RRFFQ69G5FAV"}, 2},
	} {
		env, stdout, stderr := adminTestEnv(t)
		code := run(context.Background(), tc.args, env)
		if code != tc.code {
			t.Errorf("%v => %d, want %d (stderr %q)", tc.args, code, tc.code, stderr)
		}
		if strings.Contains(stderr.String(), "로그인하지 않았습니다") {
			t.Errorf("%v: login hint printed before argument checks: %q", tc.args, stderr)
		}
		if tc.code == 0 && !strings.Contains(stdout.String(), admincli.Usage) {
			t.Errorf("%v: stdout = %q, want usage", tc.args, stdout)
		}
	}
}

// A valid command still needs credentials: the login hint and a non-zero exit.
func TestAdminCmdValidArgsStillNeedLogin(t *testing.T) {
	env, _, stderr := adminTestEnv(t)
	if code := run(context.Background(), []string{"admin", "users"}, env); code == 0 || code == 2 {
		t.Fatalf("admin users logged out => %d, want a non-usage failure", code)
	}
	if !strings.Contains(stderr.String(), "로그인하지 않았습니다 — 먼저 'masil login'") {
		t.Fatalf("stderr = %q", stderr)
	}
}
