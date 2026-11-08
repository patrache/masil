//go:build e2e

package e2egate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/version"
)

// e2ePassword is the password of every gate account (local wrangler dev only).
const e2ePassword = "e2e-pass-1234"

// eventWait bounds how long next waits for a matching event.
const eventWait = 10 * time.Second

// e2eServer returns MASIL_E2E_SERVER or skips the test when it is unset.
func e2eServer(t *testing.T) string {
	t.Helper()
	srv := os.Getenv("MASIL_E2E_SERVER")
	if srv == "" {
		t.Skip("MASIL_E2E_SERVER not set (local wrangler dev gate)")
	}
	// An unversioned build sends User-Agent masil/0.0.0-dev, which the server's
	// MIN_CLIENT_VERSION gate answers with 426 (server-architecture.md 10장).
	if version.Semver() == "0.0.0" {
		t.Fatalf("version %q is below the server's MIN_CLIENT_VERSION; run with "+
			`-ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev"`, version.Version)
	}
	return strings.TrimRight(srv, "/")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type user struct {
	API                   *api.Client
	Handle, UserID, Token string
}

// signup creates a fresh account whose handle is prefix + "_" + 8 random ULID chars.
func signup(t *testing.T, srv, prefix string) user {
	t.Helper()
	handle := strings.ToLower(prefix + "_" + ids.New()[18:26])
	r, err := api.New(srv).Signup(t.Context(), api.SignupRequest{Handle: handle, Password: e2ePassword, Client: "tui"})
	if err != nil {
		t.Fatalf("signup %s: %v", handle, err)
	}
	return user{API: api.New(srv, api.WithToken(r.Token)), Handle: r.User.Handle, UserID: r.User.ID, Token: r.Token}
}

// stream records every event of one realtime connection so tests can consume
// them by type without relying on the arrival order of unrelated types.
type stream struct {
	client *realtime.Client
	mu     sync.Mutex
	evs    []realtime.Event
	used   []bool
	signal chan struct{}
	done   chan struct{}
	err    error // realtime.Run result, valid after done is closed
}

func connect(t *testing.T, u user, mode realtime.Mode) *stream {
	t.Helper()
	s := &stream{signal: make(chan struct{}, 1), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	s.client = realtime.New(realtime.Options{
		API: u.API, DeviceID: ids.New(), Mode: mode, Resume: realtime.ResumePoint{UseDeviceAck: true},
		Logger: slog.New(slog.DiscardHandler),
		OnEvent: func(ev realtime.Event) {
			s.mu.Lock()
			s.evs = append(s.evs, ev)
			s.used = append(s.used, false)
			s.mu.Unlock()
			select {
			case s.signal <- struct{}{}:
			default:
			}
			if ev.Seq != nil {
				s.client.MarkProcessed(*ev.Seq)
			}
		},
	})
	go func() { s.err = s.client.Run(ctx); close(s.done) }()
	t.Cleanup(func() { cancel(); <-s.done })
	return s
}

// take consumes the first unused event of type typ matching every pred.
func (s *stream) take(typ string, preds []func(json.RawMessage) bool) (realtime.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
next:
	for i, ev := range s.evs {
		if s.used[i] || ev.Type != typ {
			continue
		}
		for _, p := range preds {
			if !p(ev.Data) {
				continue next
			}
		}
		s.used[i] = true
		return ev, true
	}
	return realtime.Event{}, false
}

func (s *stream) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.evs))
	for i, ev := range s.evs {
		out[i] = ev.Type
	}
	return out
}

// next waits up to 10s for the first not-yet-consumed event of type typ that
// matches every pred, and consumes it.
func (s *stream) next(t *testing.T, typ string, preds ...func(json.RawMessage) bool) realtime.Event {
	t.Helper()
	deadline := time.NewTimer(eventWait)
	defer deadline.Stop()
	for {
		if ev, ok := s.take(typ, preds); ok {
			return ev
		}
		select {
		case <-s.signal:
		case <-s.done:
			if ev, ok := s.take(typ, preds); ok {
				return ev
			}
			t.Fatalf("socket ended (%v) before %q; seen %v", s.err, typ, s.seen())
		case <-deadline.C:
			t.Fatalf("no %q within %s; seen %v", typ, eventWait, s.seen())
		}
	}
}

// wait returns the realtime.Run result, failing the test if it does not end within d.
func (s *stream) wait(t *testing.T, d time.Duration) error {
	t.Helper()
	select {
	case <-s.done:
		return s.err
	case <-time.After(d):
		t.Fatalf("socket still running after %s", d)
		return nil
	}
}

// match reports whether the value at the dotted path of the event data prints as want.
func match(path, want string) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		for _, k := range strings.Split(path, ".") {
			m, ok := v.(map[string]any)
			if !ok {
				return false
			}
			if v, ok = m[k]; !ok {
				return false
			}
		}
		return fmt.Sprint(v) == want
	}
}

// M1: account, auth, server-side sessions, realtime socket (full + notify), and the
// REST bootstrap the TUI runs right after `ready` (events.md 5.3 step 1).
func TestGateM1AuthRealtime(t *testing.T) {
	srv := e2eServer(t)
	ctx := t.Context()
	must(t, api.New(srv).Health(ctx))
	a := signup(t, srv, "g1a")
	if me, err := a.API.GetMe(ctx); err != nil || me.Handle != a.Handle {
		t.Fatalf("me %+v, %v", me, err)
	}

	// a second device logs in -> its own revocable session
	lr, err := api.New(srv).Login(ctx, api.LoginRequest{Handle: a.Handle, Password: e2ePassword, Client: "tui"})
	must(t, err)
	second := user{API: api.New(srv, api.WithToken(lr.Token)), Handle: a.Handle, UserID: a.UserID, Token: lr.Token}
	full := connect(t, second, realtime.ModeFull)
	full.next(t, "ready")
	// every bootstrap endpoint answers; otherwise the TUI status bar stays "✕ 오프라인" (core Task 30)
	if msg, ok := store.Bootstrap(second.API)().(store.BootstrapMsg); !ok || msg.Err != nil || msg.Me == nil {
		t.Fatalf("bootstrap: %+v", msg)
	}
	notify := connect(t, a, realtime.ModeNotify)
	notify.next(t, "ready")

	// logout revokes only that session: session.revoked, then the socket ends with ErrUnauthorized (4001)
	must(t, second.API.Logout(ctx))
	full.next(t, "session.revoked", match("reason", "logout"))
	if err := full.wait(t, 10*time.Second); !errors.Is(err, realtime.ErrUnauthorized) {
		t.Fatalf("revoked socket: %v", err)
	}
	if _, err := second.API.GetMe(ctx); !api.IsCode(err, "unauthorized") {
		t.Fatalf("revoked token still works: %v", err)
	}
	if _, err := a.API.GetMe(ctx); err != nil {
		t.Fatalf("other session must survive: %v", err)
	}
	if _, err := api.New(srv).Login(ctx, api.LoginRequest{Handle: a.Handle, Password: "wrong-pass-0000"}); !api.IsCode(err, "invalid_credentials") {
		t.Fatalf("wrong password: %v", err)
	}
}
