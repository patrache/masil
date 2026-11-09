package share_test

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Review 3: a doc_op from a seat whose level no longer allows it is
// answered with doc_reject{level}.
func TestDocOpBelowLevelRejected(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	d.open(hubTS, 1)
	g := d.guests[1]
	text, seq, _ := d.docs.Text(hubTS)
	must(t, d.host.SetLevel(1, wire.LevelPrompter))
	body := fmt.Sprintf(`{"doc":%q,"client_op_id":"lv-1","base_seq":%d,"ops":%s}`, hubTS, seq, mustMarshal(t, insertAt(text, 0, "x")))
	d.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindDocOp, []byte(body)))
	d.settle()
	rj, ok := findEvent[share.EventDocRejected](d.takeGuest(1))
	if !ok || rj.Code != share.RejectLevel || rj.ClientOpID != "lv-1" {
		t.Fatalf("doc_reject = %#v %v", rj, ok)
	}
	if d.hostText(hubTS) != text {
		t.Fatal("rejected edit applied")
	}
}
