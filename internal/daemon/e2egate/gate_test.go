//go:build e2e

package e2egate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
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

// M2: friends, DM/group DM, spaces/channels/invites, threads, reactions, mentions, edit/delete,
// pins, unread, typing, presence, search — all delivered live (no refresh).
func TestGateM2Chat(t *testing.T) {
	srv := e2eServer(t)
	ctx := t.Context()
	a, b, c := signup(t, srv, "g2a"), signup(t, srv, "g2b"), signup(t, srv, "g2c")
	text := func(body string) api.SendMessageRequest {
		return api.SendMessageRequest{ClientNonce: ids.New(), Kind: "text", Body: body}
	}
	bs := connect(t, b, realtime.ModeFull)
	bs.next(t, "ready")

	fr, err := a.API.SendFriendRequest(ctx, api.Handle(b.Handle))
	must(t, err)
	bs.next(t, "friend.request_received")
	_, err = b.API.AcceptFriendRequest(ctx, fr.Request.ID)
	must(t, err)
	as := connect(t, a, realtime.ModeFull)
	as.next(t, "ready")
	bs.next(t, "presence.updated", match("user_id", a.UserID), match("status", "online"))

	dm, _, err := a.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(b.Handle)}})
	must(t, err)
	m1, _, err := a.API.SendMessage(ctx, dm.ID, text("안녕 밥 👍"))
	must(t, err)
	bs.next(t, "message.created", match("message.id", m1.ID))
	bs.next(t, "alert", match("alert.kind", "dm"))
	// anti-goal: no DM bombs from strangers (no friendship, no shared space)
	if _, _, err := c.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(b.Handle)}}); !api.IsCode(err, "dm_not_allowed") {
		t.Fatalf("stranger DM: %v", err)
	}

	sp, err := a.API.CreateSpace(ctx, api.CreateSpaceRequest{Name: "관문방"})
	must(t, err)
	inv, err := a.API.CreateInvite(ctx, sp.ID, api.CreateInviteRequest{MaxUses: api.Some(2)})
	must(t, err)
	_, err = b.API.AcceptInvite(ctx, inv.Code)
	must(t, err)
	_, err = c.API.AcceptInvite(ctx, inv.Code)
	must(t, err)
	gen := sp.DefaultChannelID
	root, _, err := c.API.SendMessage(ctx, gen, text("@"+b.Handle+" 회의 몇 시?"))
	must(t, err)
	bs.next(t, "message.created", match("message.id", root.ID))
	bs.next(t, "alert", match("alert.kind", "mention"))
	reply, _, err := b.API.ReplyThread(ctx, root.ID, api.ThreadReplyRequest{ClientNonce: ids.New(), Kind: "text", Body: "3시요"})
	must(t, err)
	as.next(t, "thread.updated", match("root_id", root.ID))
	if tw, err := c.API.GetThread(ctx, root.ID, api.ThreadQuery{}); err != nil || len(tw.Items) != 1 {
		t.Fatalf("thread %+v, %v", tw, err)
	}
	must(t, a.API.AddReaction(ctx, root.ID, "👍"))
	bs.next(t, "reaction.added", match("message_id", root.ID), match("emoji", "👍"))
	_, err = c.API.EditMessage(ctx, root.ID, "@"+b.Handle+" 회의 4시로 바꿀까요?")
	must(t, err)
	bs.next(t, "message.updated", match("message.id", root.ID))
	must(t, a.API.Pin(ctx, root.ID))
	bs.next(t, "message.pinned", match("message_id", root.ID))
	if pins, err := c.API.ListPins(ctx, gen); err != nil || len(pins) != 1 {
		t.Fatalf("pins %d, %v", len(pins), err)
	}
	must(t, b.API.DeleteMessage(ctx, reply.ID))
	as.next(t, "message.deleted", match("message_id", reply.ID))
	as.client.Typing(gen, nil)
	bs.next(t, "typing", match("channel_id", gen), match("user_id", a.UserID))

	unread := func() int {
		us, err := a.API.ListUnreads(ctx)
		must(t, err)
		for _, u := range us {
			if u.ChannelID == gen {
				return u.UnreadCount
			}
		}
		return 0
	}
	if unread() < 1 {
		t.Fatal("c's message must be unread for a")
	}
	_, err = a.API.MarkRead(ctx, gen, root.ID)
	must(t, err)
	if n := unread(); n != 0 {
		t.Fatalf("unread after MarkRead = %d", n)
	}
	if hits, err := b.API.SearchMessages(ctx, api.MessageSearchQuery{Q: "회의", SpaceID: sp.ID}); err != nil || len(hits.Items) == 0 {
		t.Fatalf("search: %v", err)
	}

	g, _, err := a.API.OpenDM(ctx, api.OpenDMRequest{Users: []api.UserRef{api.Handle(b.Handle), api.Handle(c.Handle)}}) // friend + shared-space member
	must(t, err)
	if g.Kind != "group_dm" {
		t.Fatalf("group dm kind %q", g.Kind)
	}
	if page, err := b.API.ListDMs(ctx, api.PageOpts{}); err != nil || !slices.ContainsFunc(page.Items, func(ch api.Channel) bool { return ch.ID == g.ID }) {
		t.Fatalf("group dm not listed for b: %v", err)
	}
}
