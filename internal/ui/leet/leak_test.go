package leet_test

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// The cookie leak sweep (v0.1.3 A9.1, A9.5 3번; P1 Task 24): the LeetCode
// cookies are used only between this PC and LeetCode.

const (
	leakSession = "sess-SECRET-1234"
	leakCSRF    = "csrf-SECRET-5678"
)

// downOS is an OS store that cannot be used (a headless Linux): the login
// asks for consent and the file choice keeps the cookies in the real
// secrets/ folder under MASIL_CONFIG_DIR.
type downOS struct{}

func (downOS) Name() secrets.Backend { return secrets.BackendSecretService }
func (downOS) Set(context.Context, string, string, string) error {
	return secrets.ErrUnavailable
}
func (downOS) Get(context.Context, string, string) (string, error) {
	return "", secrets.ErrUnavailable
}
func (downOS) Delete(context.Context, string, string) error { return secrets.ErrUnavailable }

// debugLog makes slog's default logger write every level to a buffer until
// the test ends.
func debugLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	saved := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(saved) })
	return &buf
}

// sweep is what one leak test looks at besides the masil requests and logs/.
type sweep struct {
	screens []string
}

func (s *sweep) shot(h *uitest.Harness) { s.screens = append(s.screens, h.Screen()) }

// noValueIn fails t when any of values is in hay; where names the place, never the value.
func noValueIn(t *testing.T, where, hay string, values ...string) {
	t.Helper()
	for i, v := range values {
		if v != "" && strings.Contains(hay, v) {
			t.Errorf("cookie #%d leaked in %s", i, where)
		}
	}
}

// noValueUnder fails t when any of values is in a file under root, skipping
// the folder skip (and everything in it).
func noValueUnder(t *testing.T, root, skip string, values ...string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip != "" && p == skip {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		noValueIn(t, "file "+rel, string(b), values...)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// sweepAll checks every place the cookies must not reach (a–e of Task 24).
func sweepAll(t *testing.T, e *env, h *uitest.Harness, logBuf *bytes.Buffer, sw *sweep, values ...string) {
	t.Helper()
	paths := h.Context().Paths
	// (a) every masil request — method, path, query, headers, body; (c) logs/.
	secrettest.AssertNoLeak(t, h.API, paths.LogDir, values...)
	// (b) every file masil keeps (secrets/ is the consented store) and the solutions folder.
	noValueUnder(t, paths.Dir, paths.SecretsDir, values...)
	noValueUnder(t, paths.Cache, "", values...)
	noValueUnder(t, e.home, "", values...)
	// (c) the slog buffer.
	noValueIn(t, "slog", logBuf.String(), values...)
	// (d) the screen after every step, and this package's goldens.
	sw.shot(h)
	for i, s := range sw.screens {
		noValueIn(t, "screen "+itoa(i), s, values...)
	}
	noValueUnder(t, "testdata", "", values...)
	// (e) what the tool asked to start, and the environment that program
	// really gets: termhost starts it with os.Environ() and then Request.Env
	// (termhost.StartPane, the exec mode's Run), so what masil inherited
	// counts as much as what it adds (A9.1 rule 4).
	launches := e.pane.Started()
	if len(launches) == 0 {
		t.Fatal("no program was launched (the launch sweep would be vacuous)")
	}
	for i, r := range launches {
		noValueIn(t, "launch "+itoa(i)+" args", strings.Join(r.Args, "\x00"), values...)
		noValueIn(t, "launch "+itoa(i)+" env", strings.Join(r.Env, "\x00"), values...)
		noValueIn(t, "launch "+itoa(i)+" program", r.Program+"\x00"+r.Dir+"\x00"+r.Title, values...)
		child := append(os.Environ(), r.Env...)
		noValueIn(t, "launch "+itoa(i)+" child env", strings.Join(child, "\x00"), values...)
	}
}

func TestCookieNeverLeavesPC(t *testing.T) {
	logBuf := debugLog(t)
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	e.lc.SetUserStatus(true, "sam_lc", false)
	e.lc.QueueCheck(runID, "check_pending.json", "check_run_success.json")
	withNvim(t, true)
	h := uitest.New(t)
	h.Context().Secrets = secrets.Open(secrets.Options{OS: downOS{}, Paths: h.Context().Paths})
	leetMe(h, api.LeetMe{Current: 2, DailySpaces: []api.LeetDailySpace{
		{SpaceID: fixtures.SpaceNova, ChannelID: "01JB0000000000000000000DLY"},
	}})
	dailyReport(h, "2026-11-05", slug1606)
	solveOK(h)
	boardAPI(h)
	sw := &sweep{}

	openToday(t, h)
	h.Press("e") // nvim in a pane
	sw.shot(h)
	h.Press("ctrl+\\", "esc")

	// Log in through the dialog (r asks for it), keep it in the file.
	h.Press("r")
	h.Paste("LEETCODE_SESSION=" + leakSession + ";")
	h.Press("tab")
	h.Paste(leakCSRF)
	sw.shot(h)
	h.Press("enter")
	sw.shot(h)
	if !strings.Contains(h.Screen(), "① 파일에 저장") {
		t.Fatalf("no consent dialog:\n%s", h.Screen())
	}
	h.Press("1", "enter") // ① the file
	sw.shot(h)
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret after login = %d:\n%s", n, h.Screen())
	}
	if where := h.Context().Secrets.Where(credNS(t, h).Key(leetcode.NameSession)); where != secrets.BackendFile {
		t.Fatalf("session kept in %q, want the file", where)
	}
	h.Advance(time.Second)
	h.Advance(time.Second)
	sw.shot(h)

	// Submit (after the free gap), accepted, share with the code, open the board.
	h.Advance(11 * time.Second)
	accept(t, e, h)
	sw.shot(h)
	h.Press("c", "up", "space")
	sw.shot(h)
	h.Press("enter")
	sw.shot(h)
	if n := len(solveBodies(t, h)); n != 2 {
		t.Fatalf("solve records = %d:\n%s", n, h.Screen())
	}
	openBoard(h)
	sw.shot(h)
	h.Press("esc")

	// The cookies did reach LeetCode (the sweep is not vacuous).
	if got := sent(e, "/submit/"); len(got) != 1 || !strings.Contains(got[0].Header.Get("Cookie"), leakSession) {
		t.Fatalf("LeetCode submit did not carry the session cookie")
	}
	sweepAll(t, e, h, logBuf, sw, leakSession, leakCSRF)

	// The consented file store: 0700 folder, 0600 files, and it does hold them.
	sec := h.Context().Paths.SecretsDir
	found := false
	err := filepath.WalkDir(sec, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			want := fs.FileMode(0o600)
			if d.IsDir() {
				want = 0o700
			}
			if perm := info.Mode().Perm(); perm != want {
				t.Errorf("%s: mode %v, want %v", filepath.Base(p), perm, want)
			}
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			found = found || bytes.Contains(b, []byte(leakSession))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("the file store does not hold the session cookie (consent not exercised)")
	}
}

// Cookies from the environment are used for this run only: never stored,
// never sent to masil, never in a launched program's arguments or env —
// neither the env masil adds nor the one the program inherits from masil.
func TestCookieFromEnvNeverStored(t *testing.T) {
	logBuf := debugLog(t)
	e := setup(t)
	const envSession, envCSRF = "env-SECRET-sess-4321", "env-SECRET-csrf-8765"
	t.Setenv(leetcode.EnvSession, envSession)
	t.Setenv(leetcode.EnvCSRF, envCSRF)
	leet.TakeEnvCookiesForTest(t) // as masil's start does
	for _, k := range []string{leetcode.EnvSession, leetcode.EnvCSRF} {
		if v, ok := os.LookupEnv(k); ok {
			t.Fatalf("%s still in the process environment (%d bytes)", k, len(v))
		}
	}
	e.lc.SetDailyDate("2026-11-05")
	e.lc.SetUserStatus(true, "sam_lc", false)
	e.lc.QueueCheck(runID, "check_run_success.json")
	withNvim(t, true)
	st := secrettest.NewStore(t, true)
	h := uitest.New(t, uitest.WithSecrets(st))
	leetMe(h, api.LeetMe{Current: 2, DailySpaces: []api.LeetDailySpace{
		{SpaceID: fixtures.SpaceNova, ChannelID: "01JB0000000000000000000DLY"},
	}})
	dailyReport(h, "2026-11-05", slug1606)
	solveOK(h)
	sw := &sweep{}

	openToday(t, h)
	h.Press("e")
	h.Press("ctrl+\\", "esc", "r") // no dialog: the environment signs in
	h.Advance(time.Second)
	sw.shot(h)
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret with env cookies = %d:\n%s", n, h.Screen())
	}
	h.Advance(11 * time.Second)
	accept(t, e, h)
	h.Press("c", "up", "space", "enter")
	sw.shot(h)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	sw.shot(h)

	if got := sent(e, "interpret_solution"); !strings.Contains(got[0].Header.Get("Cookie"), envSession) {
		t.Fatal("LeetCode run did not carry the env session cookie")
	}
	for k, v := range secrettest.Values(st) {
		noValueIn(t, "secret store "+k, v, envSession, envCSRF)
	}
	sweepAll(t, e, h, logBuf, sw, envSession, envCSRF)
	noValueUnder(t, h.Context().Paths.SecretsDir, "", envSession, envCSRF)
}
