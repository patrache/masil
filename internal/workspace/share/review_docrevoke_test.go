package share_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// captureBroadcasts records the broadcast plaintexts the owner sends seat
// from now on (sealed under the session key, so a guest that already
// dropped the document would not show them).
func (d *docRig) captureBroadcasts(seat uint8) func() []string {
	var frames [][]byte
	d.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if from == wire.SeatOwner && to == seat {
			frames = append(frames, append([]byte(nil), f...))
		}
		return f
	})
	return func() []string {
		key := share.GuestSessionKey(d.guests[seat])
		var out []string
		for _, f := range frames {
			h, _, err := wire.Parse(f)
			if err != nil || h.To != wire.SeatAll {
				continue
			}
			if _, pt, err := wscrypto.NewOpener(key.Key, wscrypto.DirBroadcast).Open(f, wscrypto.SignatureLen); err == nil {
				out = append(out, string(pt))
			}
		}
		return out
	}
}

func assertNoBroadcast(t *testing.T, got []string, marker string) {
	t.Helper()
	for _, s := range got {
		if strings.Contains(s, marker) {
			t.Fatalf("participant received %q", s)
		}
	}
}

// v0.1.3 security (v) follow-up: a document a participant opened while
// visible is closed for every seat when a rule file later hides it, and the
// owner's later edits are not relayed.
func TestIgnoredAfterOpenClosesPeerDoc(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1, 2)
	_, err := d.docs.Open(hubTS, false) // the owner has it open too
	must(t, err)
	d.open(hubTS, 1, 2)
	got := d.captureBroadcasts(1)
	must(t, os.WriteFile(filepath.Join(d.dir, ".masilignore"), []byte("src/server/\n"), 0o644))
	d.advance(share.DiskPoll)
	text, seq, _ := d.docs.Text(hubTS)
	_, _, err = d.docs.Submit(hubTS, 0, "", seq, insertAt(text, 0, "PROD_PASSWORD=hunter2\n"))
	must(t, err)
	d.host.Presence(hubTS, 3, 0, 0)
	d.settle()
	d.advance(share.DiskPoll)
	assertNoBroadcast(t, got(), "hunter2")
	assertNoBroadcast(t, got(), hubTS)
	for _, s := range []uint8{1, 2} {
		if _, ok := d.guests[s].DocText(hubTS); ok {
			t.Errorf("seat %d still has %s open", s, hubTS)
		}
		closed := false
		for _, ev := range d.takeGuest(s) {
			if st, ok := ev.(share.EventDocState); ok && st.Doc == hubTS && st.Error == share.DocErrIgnored {
				closed = true
			}
		}
		if !closed {
			t.Errorf("seat %d got no doc_state closing %s as ignored", s, hubTS)
		}
	}
	// A participant edit after the close is refused, not sequenced.
	if err := d.guests[1].Edit(hubTS, insertAt("", 0, "x")); err == nil {
		d.settle()
		if strings.HasPrefix(d.hostText(hubTS), "x") {
			t.Fatal("edit of a closed document applied")
		}
	}
	// The owner keeps the document.
	if !strings.HasPrefix(d.hostText(hubTS), "PROD_PASSWORD=hunter2\n") {
		t.Fatal("owner lost the document")
	}
}

// A link a participant has open that is retargeted at a deny-listed file:
// the disk merge reads the new target, and its text must not be relayed.
func TestRetargetedLinkNotRelayed(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1)
	link := filepath.Join(d.dir, "src", "notes.txt")
	if err := os.Symlink("../README.md", link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	d.open("src/notes.txt", 1)
	got := d.captureBroadcasts(1)
	must(t, os.Remove(link))
	must(t, os.Symlink("../.env", link))
	d.advance(share.DiskPoll)
	d.advance(share.DiskPoll)
	assertNoBroadcast(t, got(), "not-a-real-secret")
	if _, ok := d.guests[1].DocText("src/notes.txt"); ok {
		t.Error("participant still has the retargeted link open")
	}
}
