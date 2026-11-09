package share_test

import (
	"errors"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Spec B16 acceptance criteria that no single Task's tests covered (P2
// Task 25). The table of B16 items to test names is in the P2 exec log.

// B16 #10: the share ended from another device (REST EndWorkspaceShare —
// the server ends the relay) reaches the owner's engine as EventEnded, and
// nothing typed after it reaches the program.
func TestEndFromOtherDevice(t *testing.T) {
	r, ft := keyboardRig(t, nil, 30*time.Minute)
	r.host.OwnerActivity(r.now)
	r.takeHost()
	r.relay.End() // what DELETE /v1/workspace-shares/{id} does on the server
	r.settle()
	e, ok := findEvent[share.EventEnded](r.takeHost())
	if !ok {
		t.Fatal("owner engine did not hear the end")
	}
	if e.Reason == "" {
		t.Fatal("EventEnded without a reason")
	}
	if !hasEvent[share.EventEnded](r.takeGuest(1)) {
		t.Fatal("participant did not hear the end")
	}
	_ = r.guests[1].Input(0, []byte("rm -rf ~\r"))
	r.advance(time.Second)
	if ft.typed() != "" {
		t.Fatalf("typed after the end: %q", ft.typed())
	}
	if err := r.host.GrantKeyboard(1, share.DefaultKeyboard); !errors.Is(err, share.ErrEnded) {
		t.Fatalf("GrantKeyboard after the end = %v", err)
	}
}

// B16 #15: one round of the ladder leaves every step in the owner's record
// (join request, check number, level, proposal, its handling, the
// participant's keys, the participant's save) — and never the owner's own
// keys (seat 0).
func TestAuditCoversLadder(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	ft := newFakeTerm()
	must(t, d.host.ShareTerminal(0, ft))
	d.settle()
	// the owner moves seat 1 down the ladder and back up
	must(t, d.host.SetLevel(1, wire.LevelPrompter))
	d.settle()
	must(t, d.host.SetLevel(1, wire.LevelEditor))
	d.settle()

	d.open(hubTS, 1)
	must(t, d.guests[1].Edit(hubTS, insertAt(d.guestText(1, hubTS), 0, "// x\n")))
	d.settle()
	must(t, d.guests[1].Save(hubTS))
	d.settle()

	_, p := proposeAndWait(t, d.rig, 1, "npm test")
	if _, err := d.host.HandleProposal(p.ID, share.ActInsert, ""); err != nil {
		t.Fatal(err)
	}
	d.settle()

	must(t, d.host.GrantKeyboard(1, 10*time.Minute))
	d.host.OwnerActivity(d.now)
	d.settle()
	must(t, d.guests[1].Input(0, []byte("ls\r")))
	d.settle()
	if typed := ft.typed(); typed != "npm testls\r" {
		t.Fatalf("the program got %q (the proposal, then the participant's keys)", typed)
	}

	seen := map[string]bool{}
	for _, e := range d.auditEntries() {
		seen[e.Ev] = true
		if e.Ev == audit.EvKeyboardInput && e.Seat == 0 {
			if s, ok := e.Data["seat"].(float64); !ok || s == 0 {
				t.Fatalf("owner keys recorded: %#v", e)
			}
		}
	}
	for _, ev := range []string{audit.EvJoinRequest, audit.EvSAS, audit.EvLevel, audit.EvProposal,
		audit.EvProposalAction, audit.EvKeyboardInput, audit.EvPeerSave} {
		if !seen[ev] {
			t.Errorf("%s missing from the record: %#v", ev, d.auditEntries())
		}
	}
}
