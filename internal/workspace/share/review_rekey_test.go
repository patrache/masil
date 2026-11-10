package share_test

import (
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// Review 4: taking the keyboard back (owner lowers, expiry) makes a new K_s
// for the members; other keyboard holders keep the keyboard.
func TestKeyboardRevokeRekeys(t *testing.T) {
	r := newRig(t, 1, 2)
	r.join(1, wire.LevelEditor, true)
	r.join(2, wire.LevelEditor, true)
	must(t, r.host.GrantKeyboard(1, 10*time.Minute))
	must(t, r.host.GrantKeyboard(2, 30*time.Minute))
	r.settle()
	e0 := share.GuestSessionKey(r.guests[1]).Epoch
	must(t, r.host.SetLevel(1, wire.LevelEditor))
	r.settle()
	if e1, e2 := share.GuestSessionKey(r.guests[1]).Epoch, share.GuestSessionKey(r.guests[2]).Epoch; e1 != e0+1 || e2 != e0+1 {
		t.Fatalf("epochs after lowering = %d %d (was %d)", e1, e2, e0)
	}
	if lv := memberLevel(t, r.host, 2); lv != wire.LevelKeyboard {
		t.Fatalf("other keyboard holder dropped to %v", lv)
	}
	r.host.OwnerActivity(r.now.Add(29 * time.Minute))
	r.advance(30 * time.Minute)
	if e := share.GuestSessionKey(r.guests[2]).Epoch; e != e0+2 {
		t.Fatalf("epoch after expiry = %d (want %d)", e, e0+2)
	}
	if hasEvent[share.EventTamperWarning](r.takeGuest(1)) || hasEvent[share.EventTamperWarning](r.takeGuest(2)) {
		t.Fatal("members reject frames after the rekey")
	}
	// The broadcast counter at its limit also rekeys.
	fired := false
	restore := share.SetNeedsRekey(func(*wscrypto.Sealer) bool {
		if fired {
			return false
		}
		fired = true
		return true
	})
	defer restore()
	r.advance(time.Second)
	if e := share.GuestSessionKey(r.guests[1]).Epoch; e != e0+3 {
		t.Fatalf("epoch after the counter limit = %d (want %d)", e, e0+3)
	}
}
