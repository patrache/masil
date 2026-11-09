package share_test

import (
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// Review 6: documents only the owner has open (a deny-listed .env) are
// never relayed — no doc_bcast, notice or presence carries their path or
// text.
func TestOwnerOnlyDocsNotRelayed(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1)
	d.open(hubTS, 1)
	var frames [][]byte
	d.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if from == wire.SeatOwner && to == 1 {
			frames = append(frames, append([]byte(nil), f...))
		}
		return f
	})
	v, err := d.docs.Open(".env", false)
	must(t, err)
	_, _, err = d.docs.Submit(v.Rel, 0, "", v.Seq, insertAt(v.Text, 0, "AWS_SECRET=hunter2\n"))
	must(t, err)
	d.host.Presence(v.Rel, 3, 0, 0)
	must(t, d.docs.Save(v.Rel, 0))
	d.settle()
	d.advance(time.Second)
	// A peer-visible document still reaches the participant.
	text, seq, _ := d.docs.Text(hubTS)
	_, _, err = d.docs.Submit(hubTS, 0, "", seq, insertAt(text, 0, ""))
	must(t, err)
	d.settle()
	if !strings.HasPrefix(d.guestText(1, hubTS), "") {
		t.Fatal("owner edit of a shared document not relayed")
	}
	key := share.GuestSessionKey(d.guests[1])
	opened := 0
	for _, f := range frames {
		h, _, err := wire.Parse(f)
		if err != nil || h.To != wire.SeatAll {
			continue
		}
		_, pt, err := wscrypto.NewOpener(key.Key, wscrypto.DirBroadcast).Open(f, wscrypto.SignatureLen)
		if err != nil {
			continue
		}
		opened++
		if s := string(pt); strings.Contains(s, "hunter2") || strings.Contains(s, ".env") {
			t.Fatalf("participant received %s: %s", h.Kind, s)
		}
	}
	if opened == 0 {
		t.Fatal("no broadcast captured")
	}
}
