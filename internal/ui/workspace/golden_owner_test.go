package workspace

import (
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/workspace/share"
)

var (
	minjiUser = api.UserSummary{ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"}
	hanaUser  = api.UserSummary{ID: fixtures.UserHana, Handle: "hana", DisplayName: "하나"}
)

// ownerScene is the B6.7 picture: minji edits, jun has the keyboard
// (28:41 left), hana views unverified. snap is called at each step.
func ownerScene(t *testing.T, h *uitest.Harness, fake *termpanetest.Fake, snap func(name string)) *shareRig {
	t.Helper()
	t.Cleanup(SetSASForTest("482 913"))
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser, 2: junUser, 3: hanaUser})
	h.Press("ctrl+\\", "S")
	snap("share_dialog")
	h.Press("enter")
	r.settle()
	r.join(1, "편집", true)
	r.join(2, "편집", true)
	r.addGuest(3)
	h.Press("ctrl+\\", "enter")
	snap("sas_owner")
	r.pickRow("확인 없이 허용")
	h.Press("enter")
	r.settle()
	if err := r.s.own.host.GrantKeyboard(2, share.DefaultKeyboard); err != nil {
		t.Fatal(err)
	}
	h.Advance(79 * time.Second)
	r.settle()
	return r
}

func TestGoldenOwner(t *testing.T) {
	fake := setup(t)
	uitest.ForEachSize(t, uitest.Required, func(t *testing.T, h *uitest.Harness) {
		r := ownerScene(t, h, fake, func(name string) { h.Golden(name) })
		h.Golden("sharing_band")
		h.Press("ctrl+\\", "4")
		h.Golden("people")
		r.s.dlg = newKeyboardDialog(r.s, h.Context(), r.s.peer(h.Context(), 1, minjiUser.ID))
		h.Golden("keyboard_warning")
	})
}
