package workspace

import (
	"encoding/json"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
)

var paletteIDs = []string{"ws.open", "ws.share.start", "ws.share.end", "ws.people", "ws.proposals",
	"ws.join", "ws.term.new", "ws.autosave.toggle", "ws.notify.toggle"}

func TestPaletteCommands(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	ctx := h.Context()
	for _, id := range paletteIDs {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Fatalf("palette command %s not registered", id)
		}
		if c.Label == nil || c.Label(ctx.T) == "" || c.Group != palette.GroupCommand {
			t.Errorf("%s: label/group %v %q", id, c.Label != nil, c.Group)
		}
	}
	// the titles of P2 Task 24 (the palette shows them as they are)
	titles := map[string]string{"ws.open": "작업공간 열기", "ws.share.start": "작업공간 공유 시작",
		"ws.share.end": "공유 끝", "ws.people": "참가자", "ws.proposals": "제안 대기 목록",
		"ws.join": "초대받은 공유 참가", "ws.term.new": "새 터미널",
		"ws.autosave.toggle": "자동 저장 켜기/끄기", "ws.notify.toggle": "작업공간 알림 켜기/끄기"}
	for id, want := range titles {
		c, _ := palette.Lookup(id)
		if got := c.Label(ctx.T); got != want {
			t.Errorf("%s: title %q, want %q", id, got, want)
		}
	}
	avail := func(id string) bool {
		c, _ := palette.Lookup(id)
		return c.Available(ctx)
	}
	if !avail("ws.open") || avail("ws.share.end") || avail("ws.proposals") || avail("ws.people") {
		t.Fatal("with no folder open: only ws.open (and notify) should be offered")
	}
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	if !avail("ws.share.start") || avail("ws.share.end") || !avail("ws.term.new") || !avail("ws.autosave.toggle") {
		t.Fatal("solo folder: share.start, term.new and autosave expected, not share.end")
	}
	r.shareFriends("minji")
	if avail("ws.share.start") || !avail("ws.share.end") || !avail("ws.people") || !avail("ws.proposals") {
		t.Fatal("sharing: share.end, people, proposals expected, not share.start")
	}
	t.Setenv("MASIL_FEATURES", "")
	for _, id := range paletteIDs {
		if avail(id) {
			t.Errorf("%s available with the flag off", id)
		}
	}
}

func TestNotifyToggle(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.API.JSON("PATCH /v1/me/settings", 200, map[string]any{"settings": api.Settings{}})
	ctx := h.Context()
	ctx.Store.Settings.Notify.Workspace = true
	c, _ := palette.Lookup("ws.notify.toggle")
	h.Send(c.Run(ctx, "")())
	h.Flush()
	var body map[string]map[string]any
	for _, q := range h.API.Requests() {
		if q.Method == "PATCH" && q.Path == "/v1/me/settings" {
			_ = json.Unmarshal(q.Body, &body)
		}
	}
	if v, ok := body["notify"]["workspace"]; !ok || v != false {
		t.Fatalf("PATCH body = %v", body)
	}
}
