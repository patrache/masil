//go:build e2e

package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/version"
)

// Plan 06 Task 17 (gate G4): the real daemon against a local `wrangler dev`.
//
//	MASIL_E2E_SERVER=http://localhost:8787 go test -tags e2e \
//	  -ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev" \
//	  ./internal/daemon/ -run TestLocalDaemonE2E -v -count=1

// e2ePassword is the password of every gate account (local wrangler dev only).
const e2ePassword = "e2e-pass-1234"

// e2eShownWait bounds how long shownWaiter waits for the n-th notification.
const e2eShownWait = 10 * time.Second

// e2eServerURL returns MASIL_E2E_SERVER (trailing slashes trimmed) or skips.
func e2eServerURL(t *testing.T) string {
	t.Helper()
	srv := os.Getenv("MASIL_E2E_SERVER")
	if srv == "" {
		t.Skip("set MASIL_E2E_SERVER=http://localhost:8787")
	}
	// An unversioned build sends User-Agent masil/0.0.0-dev, which the server's
	// MIN_CLIENT_VERSION gate answers with 426 (server-architecture.md 10장).
	if version.Semver() == "0.0.0" {
		t.Fatalf("version %q is below the server's MIN_CLIENT_VERSION; run with "+
			`-ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev"`, version.Version)
	}
	return strings.TrimRight(srv, "/")
}

type e2eUser struct {
	API                   *api.Client
	Handle, UserID, Token string
	SessionID             string
}

// signupE2E creates a fresh account whose handle is prefix + "_" + 8 random ULID chars.
func signupE2E(t *testing.T, srv, prefix string) e2eUser {
	t.Helper()
	handle := strings.ToLower(prefix + "_" + ids.New()[18:26])
	r, err := api.New(srv).Signup(t.Context(), api.SignupRequest{Handle: handle, Password: e2ePassword, Client: "tui"})
	if err != nil {
		t.Fatalf("signup %s: %v", handle, err)
	}
	return e2eUser{
		API: api.New(srv, api.WithToken(r.Token)), Handle: r.User.Handle, UserID: r.User.ID,
		Token: r.Token, SessionID: r.Session.ID,
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// saveCredsFor stores u's real session as the device's credentials for srv.
func saveCredsFor(t *testing.T, p config.Paths, srv string, u e2eUser) {
	t.Helper()
	must(t, auth.Save(p, &auth.Credentials{
		Version: 1, Server: srv, Token: u.Token, SessionID: u.SessionID,
		UserID: u.UserID, Handle: u.Handle, SavedAt: time.Now().UTC(),
	}))
}

func mustLoad(t *testing.T, p config.Paths) *config.Config {
	t.Helper()
	c, err := config.Load(p)
	must(t, err)
	return c
}

// shownWaiter returns a func that waits for the n-th notification on rec and
// checks the daemon log has exactly n `alert shown` lines, the last one of kind.
func shownWaiter(t *testing.T, rec *lockedRecorder, logs *syncBuffer) func(n int, kind string) notify.OSNotification {
	return func(n int, kind string) notify.OSNotification {
		t.Helper()
		deadline := time.Now().Add(e2eShownWait)
		for len(rec.all()) < n {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for notification #%d (%s): only %d shown\ndaemon log:\n%s", n, kind, len(rec.all()), logs.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		all := rec.all()
		if len(all) != n {
			t.Fatalf("want %d notifications, got %d: %+v", n, len(all), all)
		}
		// The log line is written right after Show returns; give it a moment.
		var lines []string
		waitForTimeout(t, 2*time.Second, func() bool {
			lines = shownLines(logs.String())
			return len(lines) >= n
		})
		if len(lines) != n {
			t.Fatalf("want %d `alert shown` log lines, got %d:\n%s", n, len(lines), logs.String())
		}
		if last := lines[n-1]; !strings.Contains(last, "kind="+kind+" ") && !strings.HasSuffix(last, "kind="+kind) {
			t.Fatalf("notification #%d: want kind=%s, log line %q", n, kind, last)
		}
		return all[n-1]
	}
}

func shownLines(log string) []string {
	var out []string
	for line := range strings.SplitSeq(log, "\n") {
		if strings.Contains(line, `msg="alert shown"`) {
			out = append(out, line)
		}
	}
	return out
}

// expectStableCount fails unless rec holds exactly n notifications for all of d.
func expectStableCount(t *testing.T, rec *lockedRecorder, n int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if got := len(rec.all()); got != n {
			t.Fatalf("want %d notifications to stay, got %d: %+v", n, got, rec.all())
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLocalDaemonE2E(t *testing.T) {
	srv := e2eServerURL(t)
	alice, bob := signupE2E(t, srv, "e2ea"), signupE2E(t, srv, "e2eb")

	// bob's daemon: temp config dir, bob's credentials, recorder backend, logs captured
	p := testPaths(t)
	t.Setenv("MASIL_SERVER", srv)
	saveCredsFor(t, p, srv, bob)
	rec, logs := &lockedRecorder{}, &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Paths: p, Config: mustLoad(t, p), Backend: rec,
			Logger: slog.New(slog.NewTextHandler(logs, nil)), PollCredentials: time.Second,
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	waitForTimeout(t, 15*time.Second, func() bool { return strings.Contains(logs.String(), "realtime ready") })
	waitShown := shownWaiter(t, rec, logs)

	// 1) friend request -> friend_request alert
	fr, err := alice.API.SendFriendRequest(ctx, api.Handle(bob.Handle))
	must(t, err)
	waitShown(1, "friend_request")
	_, err = bob.API.AcceptFriendRequest(ctx, fr.Request.ID)
	must(t, err)

	// 2) DM -> dm alert, body visible in the OS notification
	ch, _, err := alice.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(bob.Handle)}})
	must(t, err)
	_, _, err = alice.API.SendMessage(ctx, ch.ID, api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: "안녕 밥"})
	must(t, err)
	if n := waitShown(2, "dm"); !strings.Contains(n.Body, "안녕 밥") {
		t.Fatalf("dm body %q", n.Body)
	}

	// 3) TUI present on bob's device -> no popup
	release, err := instance.AcquireTUI(p)
	must(t, err)
	_, _, err = alice.API.SendMessage(ctx, ch.ID, api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: "TUI 켜져 있지?"})
	must(t, err)
	expectStableCount(t, rec, 2, 4*time.Second)
	release()

	// 4) post mentioning bob -> mention alert
	_, _, err = alice.API.CreatePost(ctx, api.CreatePostRequest{ClientNonce: ids.New(), Body: "@" + bob.Handle + " 첫 글 #masil"})
	must(t, err)
	waitShown(3, "mention")

	// 5) focus mode: suppressed while focusing, one summary at the end.
	// Early end (DELETE /v1/me/focus) must also emit alert{focus_summary} — plan 02 Task 13.
	_, err = bob.API.StartFocus(ctx, api.Some(600))
	must(t, err)
	_, _, err = alice.API.SendMessage(ctx, ch.ID, api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: "집중 중?"})
	must(t, err)
	expectStableCount(t, rec, 3, 4*time.Second)
	_, err = bob.API.EndFocus(ctx)
	must(t, err)
	waitShown(4, "focus_summary")
	expectStableCount(t, rec, 4, 3*time.Second)

	if strings.Contains(logs.String(), bob.Token) {
		t.Fatal("token leaked into daemon log")
	}
}

// TestFootprintFlood is the flood helper for the idle-footprint measurement
// (Task 17 Step 5): it sends 300 non-alert events (notifications.read) to the
// notify socket of the daemon whose config dir is MASIL_FP_DIR.
func TestFootprintFlood(t *testing.T) {
	dir := os.Getenv("MASIL_FP_DIR")
	if dir == "" {
		t.Skip("set MASIL_FP_DIR and MASIL_E2E_SERVER")
	}
	srv := e2eServerURL(t)
	t.Setenv("MASIL_CONFIG_DIR", dir)
	p, err := config.ResolvePaths()
	must(t, err)
	creds, err := auth.Load(p)
	must(t, err)
	c := api.New(srv, api.WithToken(creds.Token))
	for i := range 300 { // RL_API is 600/60s
		if _, err := c.MarkNotificationsRead(t.Context(), api.MarkNotificationsRead{All: true}); err != nil {
			t.Fatal(fmt.Errorf("mark read #%d: %w", i+1, err))
		}
	}
}
