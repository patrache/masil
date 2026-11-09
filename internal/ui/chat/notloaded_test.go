package chat

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// Before the first bootstrap (connecting, or offline at start) the home and the
// sidebar say the data is not in yet; "no rooms, make one" is only true after a
// successful bootstrap.
func TestHomeAndSidebarBeforeBootstrap(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"loading", nil, "불러오는 중…"},
		{"offline", errors.New("dial tcp: connection refused"), "서버에 연결할 수 없어요"},
	}
	for _, c := range cases {
		for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}, {W: 50, H: 20}} {
			t.Run(fmt.Sprintf("%s/%dx%d", c.name, sz.W, sz.H), func(t *testing.T) {
				seed := func(s *store.Store) { s.Sync.Err = c.err }
				h := open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
				s := h.Screen()
				for _, bad := range []string{homeEmptyTitle, "로 방을 만들어", "방이 없어요", "＋ 방 만들기"} {
					if strings.Contains(s, bad) {
						t.Fatalf("not bootstrapped yet, but the screen shows %q:\n%s", bad, s)
					}
				}
				main := paneText(t, h, layout.SlotMain)
				if !strings.Contains(main, c.want) {
					t.Fatalf("home must show %q:\n%s", c.want, main)
				}
				if c.err != nil && !strings.Contains(main, "다시 연결 중") {
					t.Fatalf("offline home must say it reconnects:\n%s", main)
				}
				if sz.W == 120 {
					side := strings.Join(strings.Fields(paneText(t, h, layout.SlotSidebar)), " ")
					if !strings.Contains(side, c.want) {
						t.Fatalf("sidebar must show %q:\n%s", c.want, side)
					}
				}
				h.Check()
			})
		}
	}
}

// After a successful bootstrap with nothing in it the home offers its entry rows
// (방 만들기 / 초대 코드로 방 참여 among them) and the sidebar its empty notice.
func TestHomeEmptyAfterBootstrap(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome},
		uitest.WithSeed(func(s *store.Store) { s.Sync.Bootstrapped = true }))
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "피드 보기", "친구 목록", "＋ 방 만들기", "초대 코드로 방 참여")
	side := strings.Join(strings.Fields(paneText(t, h, layout.SlotSidebar)), " ")
	if !strings.Contains(side, "방이 없어요 · + 로 만들기") {
		t.Fatalf("bootstrapped sidebar must offer +:\n%s", side)
	}
}
