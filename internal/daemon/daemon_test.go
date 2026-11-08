package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/notify"
)

// testOptions builds Options for run with a fast poll. A nil backend becomes a
// recorder so no test ever pops a real OS notification.
func testOptions(t *testing.T, p config.Paths, b notify.Backend) Options {
	t.Helper()
	if b == nil {
		b = &lockedRecorder{}
	}
	return Options{
		Paths:           p,
		Config:          config.Defaults(),
		Backend:         b,
		Logger:          slog.New(slog.DiscardHandler),
		Now:             func() time.Time { return t0 }, // fixed test clock (plan 06 Global Constraints)
		PollCredentials: 50 * time.Millisecond,
	}
}

// testDeps wires the fake hub; the TUI probe is the real tui.lock check.
func testDeps(hub *fakeHub) deps {
	return deps{dial: hub.dial, after: time.After, tuiRunning: instance.TUIRunning}
}

// runHarness is one run() in a goroutine.
type runHarness struct {
	p        config.Paths
	hub      *fakeHub
	logs     *syncBuffer
	rec      *lockedRecorder
	cancel   context.CancelFunc
	result   chan error    // run's return value (buffered 1)
	finished chan struct{} // closed once run returned
	err      error         // valid after finished
}

func startRun(t *testing.T, exe string) *runHarness {
	t.Helper()
	p := testPaths(t)
	t.Setenv("MASIL_SERVER", "") // config server = built-in http://localhost:8787
	h := &runHarness{
		p: p, hub: newFakeHub(t), logs: &syncBuffer{}, rec: &lockedRecorder{},
		result: make(chan error, 1), finished: make(chan struct{}),
	}
	o := testOptions(t, p, h.rec)
	o.Logger = slog.New(slog.NewTextHandler(h.logs, nil))
	d := testDeps(h.hub)
	d.exe = exe
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		err := run(ctx, o, d)
		h.err = err
		h.result <- err
		close(h.finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.finished:
		case <-time.After(5 * time.Second):
			t.Error("run did not return after cancel")
		}
	})
	return h
}

func setupRun(t *testing.T) (config.Paths, *fakeHub, *syncBuffer) {
	t.Helper()
	h := startRun(t, "")
	return h.p, h.hub, h.logs
}

// setupRunWithBackend also returns the recorder backend and stop(), which
// cancels run and requires it to return nil.
func setupRunWithBackend(t *testing.T) (config.Paths, *fakeHub, *syncBuffer, *lockedRecorder, func()) {
	t.Helper()
	h := startRun(t, "")
	stop := func() {
		t.Helper()
		h.cancel()
		select {
		case <-h.finished:
		case <-time.After(5 * time.Second):
			t.Fatal("run did not return after cancel")
		}
		if h.err != nil {
			t.Fatalf("run returned %v, want nil", h.err)
		}
	}
	return h.p, h.hub, h.logs, h.rec, stop
}

// setupRunWith runs with deps.exe = exe and hands back run's result.
func setupRunWith(t *testing.T, exe string) (config.Paths, *fakeHub, *syncBuffer, <-chan error) {
	t.Helper()
	h := startRun(t, exe)
	return h.p, h.hub, h.logs, h.result
}

func saveCreds(t *testing.T, p config.Paths, server, token string) {
	t.Helper()
	if err := auth.Save(p, &auth.Credentials{
		Version: 1, Server: server, Token: token, SessionID: "01JA7000000000000000000000",
		UserID: "01JA0000000000000000000SAM", Handle: "sam", SavedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}
}

func setDesktop(t *testing.T, p config.Paths, on bool) {
	t.Helper()
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c.Notify.Desktop = on
	if err := config.Save(p, c); err != nil {
		t.Fatal(err)
	}
}

// canonical re-marshals raw through map[string]any (sorted keys).
func canonical(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "invalid: " + string(raw)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// alertFrame is TestRunAlertEndToEndAndState's alert frame with seq, id and kind swapped.
func alertFrame(seq int64, id, kind string) string {
	return fmt.Sprintf(`{"type":"alert","seq":%d,"ts":"2026-11-05T02:05:00Z","data":{"alert":{"id":%q,"kind":%q,"title":"민지","body":"점심?","actor":null,"target":{"type":"message","id":"M1","channel_id":"C1","message_id":"M1","thread_root_id":null,"post_id":null,"space_id":null},"created_at":"2026-11-05T02:05:00Z","suppressed":false,"suppressed_reason":null}}}`, seq, id, kind)
}

func TestRunSecondInstance(t *testing.T) {
	p := testPaths(t)
	release, err := instance.AcquireDaemon(p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	err = run(t.Context(), testOptions(t, p, nil), testDeps(newFakeHub(t)))
	if !errors.Is(err, instance.ErrAlreadyRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunWaitsForCredentials(t *testing.T) {
	p, hub, logs := setupRun(t) // starts run() in a goroutine, PollCredentials=50ms, cancels on cleanup
	expectNoDial(t, hub, 200*time.Millisecond)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	d := waitDial(t, hub)
	if !strings.Contains(d.URL, "mode=notify") || d.Auth != "Bearer msl_test_token_A" {
		t.Fatalf("dial %+v", d)
	}
	if !strings.Contains(logs.String(), `level=INFO msg="not logged in; waiting for credentials"`) {
		t.Fatal("missing INFO wait log")
	}
}

func TestRunServerMismatchStaysOffline(t *testing.T) {
	p, hub, logs := setupRun(t)
	saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A") // config server is http://localhost:8787
	expectNoDial(t, hub, 300*time.Millisecond)
	out := logs.String()
	if strings.Count(out, `level=WARN msg="credentials server mismatch"`) != 1 {
		t.Fatalf("want exactly one WARN mismatch log:\n%s", out)
	}
	if strings.Contains(out, "msl_test_token_A") {
		t.Fatal("token leaked into log")
	}
	rs, err := readRunStatus(p) // what the daemon itself saw, for `masil daemon status`
	if err != nil || rs == nil || rs.ConfigServer != "http://localhost:8787" || rs.CredentialsServer != "http://127.0.0.1:9999" || !rs.ServerMismatch || rs.EnvServer {
		t.Fatalf("run status %+v, %v", rs, err)
	}
	if rs.Version != 1 || rs.PID != os.Getpid() {
		t.Fatalf("run status version/pid %+v", rs)
	}
}

// [notify] desktop flipped in config.toml while a session is live takes effect without reconnecting.
func TestRunDesktopToggleMidSession(t *testing.T) {
	p, hub, logs, rec, _ := setupRunWithBackend(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	hub.Push(alertFrame(5, "01JC9ZA0000000000000000005", "dm")) // same shape as TestRunAlertEndToEndAndState's frame
	waitFor(t, func() bool { return len(rec.ids()) == 1 })

	setDesktop(t, p, false) // config.Load -> Notify.Desktop=false -> config.Save
	waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="desktop setting changed" desktop=false`) })
	hub.Push(alertFrame(6, "01JC9ZA0000000000000000006", "dm"))
	expectStable(t, func() []string { return rec.ids() }, 200*time.Millisecond)
	if len(rec.ids()) != 1 {
		t.Fatalf("desktop=false must not pop up: %v", rec.ids())
	}

	setDesktop(t, p, true)
	waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="desktop setting changed" desktop=true`) })
	hub.Push(alertFrame(7, "01JC9ZA0000000000000000007", "dm"))
	waitFor(t, func() bool { return len(rec.ids()) == 2 })
	if len(hub.Dials()) != 1 {
		t.Fatalf("toggling desktop must not reconnect: %d dials", len(hub.Dials()))
	}
}

// brew upgrade / install.sh replace the binary: the daemon exits with ErrRestartRequired so
// launchd/systemd relaunch the new one — also when it was parked on 4010 upgrade_required.
func TestRunRestartsWhenExecutableReplaced(t *testing.T) {
	for _, upgradeRequired := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_4010=%v", upgradeRequired), func(t *testing.T) {
			exe := writeTempExe(t, "v1") // absolute temp file standing in for <prefix>/bin/masil
			p, hub, logs, done := setupRunWith(t, exe)
			saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
			waitDial(t, hub)
			if upgradeRequired {
				hub.CloseWith(4010)
				expectNoDial(t, hub, 200*time.Millisecond)
				if !strings.Contains(logs.String(), `level=WARN msg="session rejected; waiting for new credentials" reason=upgrade_required`) {
					t.Fatal(logs.String())
				}
			}
			replaceExe(t, exe, "v2-longer") // write sibling temp file, then os.Rename over exe
			select {
			case err := <-done:
				if !errors.Is(err, ErrRestartRequired) {
					t.Fatalf("run returned %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("daemon kept running on the old binary")
			}
			if !strings.Contains(logs.String(), `level=INFO msg="executable replaced; exiting for restart"`) {
				t.Fatal(logs.String())
			}
		})
	}
}

func TestRunUnauthorizedWaitsForNewToken(t *testing.T) {
	p, hub, logs := setupRun(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	hub.CloseWith(4001)
	expectNoDial(t, hub, 300*time.Millisecond) // same token: never redial
	if _, err := os.Stat(p.Credentials); err != nil {
		t.Fatal("credentials.json must not be deleted")
	}
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_B")
	if d := waitDial(t, hub); d.Auth != "Bearer msl_test_token_B" {
		t.Fatalf("redial %+v", d)
	}
	out := logs.String()
	if strings.Contains(out, "msl_test_token_") {
		t.Fatal("token leaked into log")
	}
	if !strings.Contains(out, `level=WARN msg="session rejected; waiting for new credentials" reason=unauthorized token=msl_…(redacted)`) {
		t.Fatalf("missing rejected WARN:\n%s", out)
	}
}

func TestRunAlertEndToEndAndState(t *testing.T) {
	p, hub, _, rec, stop := setupRunWithBackend(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	waitFor(t, func() bool { return len(hub.Resumes()) == 1 })
	if got := canonical(hub.Resumes()[0]); got != `{"use_device_ack":true}` {
		t.Fatalf("resume %s", got)
	}
	hub.Push(`{"type":"alert","seq":5,"ts":"2026-11-05T02:05:00Z","data":{"alert":{"id":"01JC9ZA0000000000000000000","kind":"dm","title":"민지","body":"점심?","actor":null,"target":{"type":"message","id":"M1","channel_id":"C1","message_id":"M1","thread_root_id":null,"post_id":null,"space_id":null},"created_at":"2026-11-05T02:05:00Z","suppressed":false,"suppressed_reason":null}}}`)
	waitFor(t, func() bool { return len(rec.ids()) == 1 })
	stop() // cancel ctx, wait for run() to return nil
	st, err := loadState(p)
	if err != nil || st == nil || st.LastSeq != 5 || st.Epoch != "E1" || st.UserID != "01JA0000000000000000000SAM" {
		t.Fatalf("state %+v, %v", st, err)
	}
}

func TestRunResumePoint(t *testing.T) {
	for name, tc := range map[string]struct{ stateUser, want string }{
		"same_user":  {"01JA0000000000000000000SAM", `{"epoch":"E1","last_seq":41}`},
		"other_user": {"01JA00000000000000000OTHER", `{"use_device_ack":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			p, hub, _ := setupRun(t)
			saveState(p, State{UserID: tc.stateUser, Epoch: "E1", LastSeq: 41, SavedAt: t0})
			saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
			waitFor(t, func() bool { return len(hub.Resumes()) == 1 })
			if got := canonical(hub.Resumes()[0]); got != tc.want {
				t.Fatalf("resume %s want %s", got, tc.want)
			}
		})
	}
}

// Logging out (credentials.json removed) mid-session drops the socket and the
// daemon goes back to waiting; logging in again reconnects.
func TestRunCredentialsRemovedMidSession(t *testing.T) {
	p, hub, logs := setupRun(t)
	waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="not logged in; waiting for credentials"`) })
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	if err := auth.Clear(p); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return strings.Count(logs.String(), `msg="not logged in; waiting for credentials"`) == 2
	})
	expectNoDial(t, hub, 150*time.Millisecond)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
}

// A config.toml server change mid-session reconnects to the new address only
// once credentials match it; until then the daemon stays offline with one WARN.
func TestRunConfigServerChangeMidSession(t *testing.T) {
	p, hub, logs := setupRun(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	c := config.Defaults()
	c.Server = "http://127.0.0.1:9999"
	if err := config.Save(p, c); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(logs.String(), `level=WARN msg="credentials server mismatch"`) })
	expectNoDial(t, hub, 150*time.Millisecond)
	waitFor(t, func() bool {
		rs, err := readRunStatus(p)
		return err == nil && rs != nil && rs.ConfigServer == "http://127.0.0.1:9999" && rs.ServerMismatch
	})
	saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A")
	if d := waitDial(t, hub); !strings.HasPrefix(d.URL, "ws://127.0.0.1:9999/") {
		t.Fatalf("redial %+v", d)
	}
}

// An unparsable config.toml at startup keeps the daemon on the config it was
// given (here Defaults()) and running.
func TestRunBrokenConfigKeepsRunning(t *testing.T) {
	p, hub, logs := setupRun(t)
	if err := os.WriteFile(p.Config, []byte("server = [broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	expectNoDial(t, hub, 200*time.Millisecond)
	if n := strings.Count(logs.String(), `level=WARN msg="config reload failed"`); n != 1 {
		t.Fatalf("want exactly one reload WARN, got %d:\n%s", n, logs.String())
	}
}

// A config.toml broken mid-session (syntax error, or unreadable) is a reload
// failure: the previous config stays in force (server, [notify] desktop), the
// live session is kept, and the failure is WARNed once. Fixing the file
// resumes reloading.
func TestRunConfigBrokenMidSessionKeepsPrevious(t *testing.T) {
	for name, breakIt := range map[string]func(t *testing.T, p config.Paths){
		"syntax_error": func(t *testing.T, p config.Paths) {
			if err := os.WriteFile(p.Config, []byte("server = \"http://127.0.0.1:9999\"\n[notify\ndesktop = false\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable": func(t *testing.T, p config.Paths) {
			if err := os.Remove(p.Config); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p.Config, 0o700); err != nil { // ReadFile fails with a non-ErrNotExist error
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, hub, logs, rec, _ := setupRunWithBackend(t)
			good := "server = \"http://127.0.0.1:9999\"\n\n[notify]\ndesktop = false\n"
			if err := os.WriteFile(p.Config, []byte(good), 0o600); err != nil {
				t.Fatal(err)
			}
			saveCreds(t, p, "http://127.0.0.1:9999", "msl_test_token_A")
			if d := waitDial(t, hub); !strings.HasPrefix(d.URL, "ws://127.0.0.1:9999/") {
				t.Fatalf("dial %+v", d)
			}
			waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="desktop setting changed" desktop=false`) })

			breakIt(t, p)
			waitFor(t, func() bool { return strings.Contains(logs.String(), `level=WARN msg="config reload failed"`) })
			hub.Push(alertFrame(5, "01JC9ZA0000000000000000005", "dm"))
			expectNoDial(t, hub, 300*time.Millisecond) // several poll rounds
			out := logs.String()
			if n := strings.Count(out, `level=WARN msg="config reload failed"`); n != 1 {
				t.Fatalf("want exactly one reload WARN, got %d:\n%s", n, out)
			}
			if strings.Contains(out, "credentials server mismatch") {
				t.Fatalf("server must stay on the previous config:\n%s", out)
			}
			if strings.Contains(out, `msg="desktop setting changed" desktop=true`) {
				t.Fatalf("desktop must stay false:\n%s", out)
			}
			if len(rec.ids()) != 0 {
				t.Fatalf("desktop=false must not pop up: %v", rec.ids())
			}
			if len(hub.Dials()) != 1 {
				t.Fatalf("broken config must not reconnect: %d dials", len(hub.Dials()))
			}
			rs, err := readRunStatus(p)
			if err != nil || rs == nil || rs.ConfigServer != "http://127.0.0.1:9999" || rs.ServerMismatch {
				t.Fatalf("run status %+v, %v", rs, err)
			}

			// Fixed again (desktop now on): reloading resumes.
			_ = os.Remove(p.Config)
			if err := os.WriteFile(p.Config, []byte("server = \"http://127.0.0.1:9999\"\n\n[notify]\ndesktop = true\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool { return strings.Contains(logs.String(), `msg="desktop setting changed" desktop=true`) })
			if len(hub.Dials()) != 1 {
				t.Fatalf("fixing config must not reconnect: %d dials", len(hub.Dials()))
			}
		})
	}
}

func TestRunStatusFile(t *testing.T) {
	p := testPaths(t)
	if rs, err := readRunStatus(p); rs != nil || err != nil {
		t.Fatalf("missing file: %+v, %v", rs, err)
	}
	want := runStatus{PID: 42, UpdatedAt: t0, ConfigServer: "https://masil.example", EnvServer: true}
	if err := writeRunStatus(p, want); err != nil {
		t.Fatal(err)
	}
	rs, err := readRunStatus(p)
	want.Version = 1
	if err != nil || rs == nil || *rs != want {
		t.Fatalf("read %+v, %v; want %+v", rs, err, want)
	}
	if fi, err := os.Stat(runStatusPath(p)); err != nil || (fi.Mode().Perm() != 0o600 && os.PathSeparator == '/') {
		t.Fatalf("stat %v, %v", fi, err)
	}
	if err := os.WriteFile(runStatusPath(p), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRunStatus(p); err == nil {
		t.Fatal("corrupt file must be an error")
	}
}

// Two 4009 closes within 60s halt the realtime client for good (CA 4.4,
// events.md 1.2). 4009 means "evicted: more than 10 connections", and the stop
// rule is the brake on eviction storms, so the daemon must not redial on the
// poll cadence (each redial would evict the user's oldest connection, maybe a
// TUI on another device). It warns once — `masil daemon status` shows it under
// 최근 경고 — and stays offline until the credentials or the server change (or
// the process restarts), exactly like a rejection.
func TestRunReplacedTwiceStaysOffline(t *testing.T) {
	p, hub, logs := setupRun(t)
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_A")
	waitDial(t, hub)
	waitFor(t, func() bool { return len(hub.Resumes()) == 1 })
	hub.CloseWith(4009) // first replacement: the client backs off and redials itself
	waitDial(t, hub)
	waitFor(t, func() bool { return len(hub.Resumes()) == 2 })
	hub.CloseWith(4009) // second within 60s: the client halts
	const warn = `level=WARN msg="realtime stopped; staying offline until credentials or server change" reason="realtime: replaced twice within 60s (4009)"`
	waitFor(t, func() bool { return strings.Contains(logs.String(), warn) })
	expectNoDial(t, hub, 400*time.Millisecond) // eight poll rounds: no redial
	if n := strings.Count(logs.String(), "realtime stopped"); n != 1 {
		t.Fatalf("want exactly one stopped WARN, got %d:\n%s", n, logs.String())
	}

	// A new login lifts the halt.
	saveCreds(t, p, "http://localhost:8787", "msl_test_token_B")
	if d := waitDial(t, hub); d.Auth != "Bearer msl_test_token_B" {
		t.Fatalf("redial %+v", d)
	}
	if strings.Contains(logs.String(), "msl_test_token_") {
		t.Fatal("token leaked into log")
	}
}
