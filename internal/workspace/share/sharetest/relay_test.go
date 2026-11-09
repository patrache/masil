package sharetest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/share/sharetest"
	"github.com/patrache/masil/internal/workspace/wire"
)

func frame(kind wire.Kind, flags, from, to uint8, payload string) []byte {
	h := wire.Header{Kind: kind, Flags: flags, From: from, To: to, Len: uint32(len(payload))}
	b := h.Marshal()
	return append(b[:], payload...)
}

func ctl(kind wire.Kind, body any) []byte {
	raw, _ := json.Marshal(body)
	return frame(kind, 0, wire.SeatOwner, wire.SeatServer, string(raw))
}

// drain returns every frame waiting in c's inbox, as "kind:payload".
func drain(t *testing.T, c share.Conn) []string {
	t.Helper()
	var out []string
	for {
		select {
		case f, ok := <-c.Recv():
			if !ok {
				return out
			}
			h, p, err := wire.Parse(f)
			if err != nil {
				t.Fatalf("bad frame delivered: %v", err)
			}
			out = append(out, h.Kind.String()+":"+string(p))
		default:
			return out
		}
	}
}

func dial(t *testing.T, r *sharetest.Relay, user string) share.Conn {
	t.Helper()
	c, err := r.Dial(user)(context.Background(), "W1")
	if err != nil {
		t.Fatalf("dial %s: %v", user, err)
	}
	return c
}

func closedWith(t *testing.T, c share.Conn, code share.CloseCode) {
	t.Helper()
	select {
	case <-c.Done():
	default:
		t.Fatalf("conn still open, want close %d", code)
	}
	if got := share.CloseCodeOf(c.Err()); got != code {
		t.Fatalf("close code = %d (%v), want %d", got, c.Err(), code)
	}
}

func same(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %q, want %q", what, got, want)
		}
	}
}

type room struct {
	r                *sharetest.Relay
	owner, hana, bob share.Conn
}

// newRoom: owner sam (seat 0), hana seat 1, bob seat 2, all connected, inboxes drained.
func newRoom(t *testing.T) room {
	t.Helper()
	r := sharetest.NewRelay("W1", "U_SAM", map[uint8]string{1: "U_HANA", 2: "U_BOB"})
	rm := room{r: r, owner: dial(t, r, "U_SAM"), hana: dial(t, r, "U_HANA"), bob: dial(t, r, "U_BOB")}
	r.Pump()
	drain(t, rm.owner)
	drain(t, rm.hana)
	drain(t, rm.bob)
	return rm
}

// Rule 5: srv_hello on connect (seats = connected sockets), srv_peer joined/left to the owner.
func TestRelayHelloAndPeers(t *testing.T) {
	r := sharetest.NewRelay("W1", "U_SAM", map[uint8]string{1: "U_HANA"})
	owner := dial(t, r, "U_SAM")
	r.Pump()
	got := drain(t, owner)
	same(t, "owner hello", got, `srv_hello:{"share_id":"W1","seat":0,"owner_id":"U_SAM","seats":[{"seat":0,"user_id":"U_SAM"}],"max_frame":262144}`)
	hana := dial(t, r, "U_HANA")
	r.Pump()
	same(t, "hana hello", drain(t, hana),
		`srv_hello:{"share_id":"W1","seat":1,"owner_id":"U_SAM","seats":[{"seat":0,"user_id":"U_SAM"},{"seat":1,"user_id":"U_HANA"}],"max_frame":262144}`)
	same(t, "owner peer", drain(t, owner), `srv_peer:{"seat":1,"user_id":"U_HANA","state":"joined"}`)

	hana.Close(1000, "")
	r.Pump()
	same(t, "owner peer left", drain(t, owner), `srv_peer:{"seat":1,"user_id":"U_HANA","state":"left"}`)

	// Not the owner and not a member: refused like the server's 404.
	_, err := r.Dial("U_EVE")(context.Background(), "W1")
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Errorf("stranger dial err = %v, want 404", err)
	}
	// A new socket for the same user replaces the old one with 4102 and no srv_peer left.
	h1 := dial(t, r, "U_HANA")
	r.Pump()
	drain(t, owner)
	h2 := dial(t, r, "U_HANA")
	closedWith(t, h1, share.CloseReplaced)
	r.Pump()
	same(t, "owner after replace", drain(t, owner), `srv_peer:{"seat":1,"user_id":"U_HANA","state":"joined"}`)
	drain(t, h2)
}

// Rules 1-4, the same cases as server test/realtime/workspace-hub.test.ts.
func TestRelayRoutingMirrorsServer(t *testing.T) {
	t.Run("owner broadcast reaches levelled seats only", func(t *testing.T) {
		rm := newRoom(t)
		rm.r.SetLevels(map[uint8]wire.Level{1: wire.LevelViewer}, nil)
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, wire.SeatAll, "out"))
		rm.r.Pump()
		same(t, "hana", drain(t, rm.hana), "term_out:out")
		same(t, "bob", drain(t, rm.bob))
		same(t, "owner", drain(t, rm.owner))
	})
	t.Run("owner To=n reaches that seat whatever its level", func(t *testing.T) {
		rm := newRoom(t)
		rm.owner.Send(frame(wire.KindHsOwnerPub, 0, 0, 2, "pub"))
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, 0, "self"))            // To=0 from the owner: dropped
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, wire.SeatServer, "s")) // To=254, not ctl_: dropped
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, 9, "nine"))            // no seat 9: dropped
		rm.r.Pump()
		same(t, "bob", drain(t, rm.bob), "hs_owner_pub:pub")
		same(t, "hana", drain(t, rm.hana))
		same(t, "owner", drain(t, rm.owner))
	})
	t.Run("participant frames go to the owner by MinLevel", func(t *testing.T) {
		rm := newRoom(t)
		rm.r.SetLevels(map[uint8]wire.Level{1: wire.LevelViewer, 2: wire.LevelKeyboard}, nil)
		rm.hana.Send(frame(wire.KindHsCommit, 0, 1, 0, "c"))       // MinLevel none: allowed
		rm.hana.Send(frame(wire.KindTermInput, 0, 1, 0, "ls"))     // keyboard needed: dropped
		rm.hana.Send(frame(wire.KindDocOpen, 0, 1, 0, "open"))     // viewer: allowed
		rm.hana.Send(frame(wire.KindTermOut, 0, 1, 0, "fake"))     // never from a participant: dropped
		rm.hana.Send(frame(wire.KindDocOpen, 0, 1, 2, "sideways")) // To!=0: dropped
		rm.bob.Send(frame(wire.KindTermInput, 0, 2, 0, "pwd"))
		rm.r.Pump()
		same(t, "owner", drain(t, rm.owner), "hs_commit:c", "doc_open:open", "term_input:pwd")
		same(t, "bob", drain(t, rm.bob))
	})
	t.Run("a seat with no level sends only handshake kinds", func(t *testing.T) {
		rm := newRoom(t)
		rm.bob.Send(frame(wire.KindDocOpen, 0, 2, 0, "open"))
		rm.bob.Send(frame(wire.KindHsReveal, 0, 2, 0, "rv"))
		rm.r.Pump()
		same(t, "owner", drain(t, rm.owner), "hs_reveal:rv")
	})
	t.Run("ctl_ frames are consumed, never forwarded", func(t *testing.T) {
		rm := newRoom(t)
		rm.r.SetLevels(map[uint8]wire.Level{1: wire.LevelViewer, 2: wire.LevelViewer}, nil)
		rm.owner.Send(ctl(wire.KindCtlAttention, map[string]any{}))
		rm.owner.Send(ctl(wire.KindCtlLevels, map[string]any{"levels": map[string]string{"1": "editor", "2": "viewer"}, "verified": map[string]bool{"1": true}}))
		rm.hana.Send(frame(wire.KindCtlEnd, 0, 1, wire.SeatServer, "{}")) // a participant's ctl_: dropped
		rm.owner.Send(frame(wire.KindSrvPeer, 0, 0, wire.SeatAll, "{}"))  // the owner's srv_: dropped
		rm.r.Pump()
		same(t, "hana", drain(t, rm.hana))
		same(t, "bob", drain(t, rm.bob))
		same(t, "owner", drain(t, rm.owner))
		if rm.r.Attention() != 1 {
			t.Errorf("Attention() = %d", rm.r.Attention())
		}
		if lv := rm.r.Levels(); lv[1] != wire.LevelEditor || lv[2] != wire.LevelViewer || !rm.r.Verified()[1] || rm.r.Ended() {
			t.Errorf("levels %v verified %v ended %v", lv, rm.r.Verified(), rm.r.Ended())
		}
		// A malformed table is ignored as a whole.
		rm.owner.Send(ctl(wire.KindCtlLevels, map[string]any{"levels": map[string]string{"1": "viewer", "9": "viewer"}}))
		rm.owner.Send(ctl(wire.KindCtlLevels, map[string]any{"levels": map[string]string{"1": "god"}}))
		rm.r.Pump()
		if lv := rm.r.Levels(); lv[1] != wire.LevelEditor {
			t.Errorf("malformed ctl_levels applied: %v", lv)
		}
		// The table is whole: a seat left out has no level.
		rm.owner.Send(ctl(wire.KindCtlLevels, map[string]any{"levels": map[string]string{"2": "viewer"}}))
		rm.r.Pump()
		if lv := rm.r.Levels(); lv[1] != wire.LevelNone || lv[2] != wire.LevelViewer {
			t.Errorf("levels after whole-table update = %v", lv)
		}
	})
	t.Run("From must be the socket's seat", func(t *testing.T) {
		rm := newRoom(t)
		rm.hana.Send(frame(wire.KindHsCommit, 0, 2, 0, "spoof"))
		closedWith(t, rm.hana, share.CloseProtocol)
		rm.bob.Send([]byte{1, 2, 3})
		closedWith(t, rm.bob, share.CloseProtocol)
		rm.r.Pump()
		got := drain(t, rm.owner)
		same(t, "owner", got, `srv_peer:{"seat":1,"user_id":"U_HANA","state":"left"}`, `srv_peer:{"seat":2,"user_id":"U_BOB","state":"left"}`)
	})
	t.Run("keyframe cache replays to a seat that gains a level", func(t *testing.T) {
		rm := newRoom(t)
		rm.r.SetLevels(map[uint8]wire.Level{1: wire.LevelViewer}, nil)
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, wire.SeatAll, "before"))
		rm.owner.Send(frame(wire.KindTermKey, wire.FlagGroupStart, 0, wire.SeatAll, "key"))
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, wire.SeatAll, "after"))
		rm.owner.Send(frame(wire.KindTermOut, 0, 0, 1, "direct")) // To=n: not cached
		rm.r.Pump()
		same(t, "hana", drain(t, rm.hana), "term_out:before", "term_key:key", "term_out:after", "term_out:direct")
		rm.owner.Send(ctl(wire.KindCtlLevels, map[string]any{"levels": map[string]string{"1": "viewer", "2": "viewer"}}))
		rm.r.Pump()
		same(t, "bob", drain(t, rm.bob), "term_key:key", "term_out:after")
		same(t, "hana (already levelled)", drain(t, rm.hana))
	})
	t.Run("ctl_kick closes 4101, tells the owner, and the seat cannot return", func(t *testing.T) {
		rm := newRoom(t)
		rm.r.SetLevels(map[uint8]wire.Level{1: wire.LevelViewer}, map[uint8]bool{1: true})
		rm.owner.Send(ctl(wire.KindCtlKick, map[string]any{"seat": 1}))
		closedWith(t, rm.hana, share.CloseKicked)
		rm.r.Pump()
		same(t, "owner", drain(t, rm.owner), `srv_peer:{"seat":1,"user_id":"U_HANA","state":"left"}`)
		if rm.r.Levels()[1] != wire.LevelNone || rm.r.Verified()[1] {
			t.Errorf("kicked seat keeps level %v / verified", rm.r.Levels()[1])
		}
		if _, err := rm.r.Dial("U_HANA")(context.Background(), "W1"); err == nil {
			t.Error("a kicked member dialled back in")
		}
	})
	t.Run("ctl_end and End close everyone 4100", func(t *testing.T) {
		rm := newRoom(t)
		rm.owner.Send(ctl(wire.KindCtlEnd, map[string]any{}))
		for _, c := range []share.Conn{rm.owner, rm.hana, rm.bob} {
			closedWith(t, c, share.CloseEnded)
		}
		if !rm.r.Ended() {
			t.Error("Ended() = false")
		}
		if _, err := rm.r.Dial("U_SAM")(context.Background(), "W1"); err == nil {
			t.Error("dial after end succeeded")
		}
		rm2 := newRoom(t)
		rm2.r.End()
		closedWith(t, rm2.bob, share.CloseEnded)
	})
}

// Test controls: Tamper rewrites or drops a delivery, Inject bypasses the
// rules, Drop cuts one connection like a network loss.
func TestRelayControls(t *testing.T) {
	rm := newRoom(t)
	rm.r.SetLevels(map[uint8]wire.Level{1: wire.LevelViewer, 2: wire.LevelViewer}, nil)
	rm.r.Tamper(func(from, to uint8, f []byte) []byte {
		if to == 2 {
			return nil
		}
		out := append([]byte(nil), f...)
		out[len(out)-1] = 'X'
		return out
	})
	rm.owner.Send(frame(wire.KindTermOut, 0, 0, wire.SeatAll, "abc"))
	rm.r.Pump()
	same(t, "hana tampered", drain(t, rm.hana), "term_out:abX")
	same(t, "bob dropped", drain(t, rm.bob))
	rm.r.Tamper(nil)

	rm.r.Inject(2, frame(wire.KindTermOut, 0, 7, 2, "raw"))
	if n := rm.r.Pump(); n != 1 {
		t.Errorf("Pump delivered %d, want 1", n)
	}
	same(t, "bob injected", drain(t, rm.bob), "term_out:raw")

	rm.r.Drop(1)
	select {
	case <-rm.hana.Done():
	default:
		t.Fatal("Drop left the conn open")
	}
	if share.CloseCodeOf(rm.hana.Err()) != 0 || rm.hana.Err() == nil {
		t.Errorf("dropped conn err = %v, want a non-close error", rm.hana.Err())
	}
	if err := rm.hana.Send(frame(wire.KindHsCommit, 0, 1, 0, "")); err == nil {
		t.Error("Send on a dropped conn succeeded")
	}
	rm.r.Pump()
	same(t, "owner", drain(t, rm.owner), `srv_peer:{"seat":1,"user_id":"U_HANA","state":"left"}`)
	if rm.r.Connected(1) || !rm.r.Connected(2) {
		t.Errorf("Connected(1)=%v Connected(2)=%v", rm.r.Connected(1), rm.r.Connected(2))
	}
}
