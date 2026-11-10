package workspace

import (
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/share"
)

// Security (ae) follow-up: the tamper warning opens by itself (B9 "경고하고
// 공유를 멈출지 묻는다"), but the keys the owner was typing never stop the
// share: input is held for a moment after it opens and the default choice is
// to continue. Stopping on purpose still works.
func TestTamperDialogIgnoresTypedKeys(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	r.shareFriends("minji")
	warn := func() {
		r.s.ownerEvent(h.Context(), share.EventTamperWarning{})
		if r.s.dlg == nil {
			t.Fatal("the tamper warning did not open")
		}
	}
	warn()
	h.Press("l", "s", "enter", "up", "enter")
	if r.s.own == nil || r.s.own.host.Killed() {
		t.Fatal("keys typed as the warning opened stopped the share")
	}
	if r.s.dlg == nil {
		t.Fatal("keys typed as the warning opened closed it")
	}
	h.Advance(time.Second)
	h.Press("enter")
	if r.s.own == nil || r.s.own.host.Killed() {
		t.Fatal("enter on the default choice stopped the share")
	}
	if r.s.dlg != nil {
		t.Fatal("enter on the default choice did not close the warning")
	}
	warn()
	h.Advance(time.Second)
	h.Press("up", "enter")
	if r.s.own != nil && !r.s.own.host.Killed() {
		t.Fatal("choosing stop did not stop the share")
	}
}
