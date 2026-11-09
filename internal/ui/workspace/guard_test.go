package workspace

import (
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

func TestKillFromChatScreen(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	r.shareFriends("minji")
	r.join(1, "편집", true)
	host := r.s.own.host
	if !shareGuardActive() {
		t.Fatal("guard not active while sharing")
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
	if strings.Contains(h.Screen(), "🔴 공유 중") {
		t.Fatalf("still on the workspace:\n%s", h.Screen())
	}
	h.Press("ctrl+\\", "x")
	if !host.Killed() {
		t.Fatal("ctrl+\\ x on the chat screen did not kill the share")
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("%d overlays opened (a confirm?):\n%s", n, h.Screen())
	}
	start := time.Now()
	for !r.relay.Ended() {
		if time.Since(start) > time.Second {
			t.Fatal("ctl_end not delivered within 1s")
		}
		time.Sleep(time.Millisecond)
	}
	r.settle()
	if r.s.sharing() || shareGuardActive() {
		t.Fatal("still sharing after the kill")
	}
	h.Send(ui.NavigateMsg{Route: r.s.route})
	if strings.Contains(h.Screen(), "🔴 공유 중") {
		t.Fatalf("the sharing band stayed:\n%s", h.Screen())
	}
}

func TestPrefixXWithoutShareIsPaneQuit(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	openFolder(t, h, "workspace:solo", copyFixture(t))
	if shareGuardActive() || ui.KillSwitchActive() {
		t.Fatal("kill switch active without a share")
	}
}

// On the workspace itself the surface reads ctrl+\ (its own keys keep
// working while sharing) and x still cuts the share at once.
func TestKillOnWorkspaceItself(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	r.shareFriends("minji")
	r.join(1, "편집", true)
	h.Press("ctrl+\\", "4")
	if r.s.focus != PanePeople {
		t.Fatalf("ctrl+\\ 4 while sharing did not reach the surface (focus %d)", r.s.focus)
	}
	host := r.s.own.host
	h.Press("ctrl+\\", "x")
	if !host.Killed() {
		t.Fatal("ctrl+\\ x on the workspace did not kill the share")
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("%d overlays opened:\n%s", n, h.Screen())
	}
	r.settle()
	if r.s.sharing() || shareGuardActive() {
		t.Fatal("still sharing after the kill")
	}
}
