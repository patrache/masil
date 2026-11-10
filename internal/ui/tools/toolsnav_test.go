package tools_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
)

// 0.1.3 patch (tools-nav fix 1): ctrl+t from the chat always shows the hub —
// never straight back into the last tool — with the cursor on the row of the
// instance that was last active, so ctrl+t enter goes back to that same
// instance (not a new one). Number keys still open the matching tool.

var (
	gitRoute  = ui.Route{Kind: ui.RouteTool, ID: "git-1", Arg: ui.ToolGit}
	leetRoute = ui.Route{Kind: ui.RouteTool, ID: "leet-1", Arg: ui.ToolLeet}
)

func TestCtrlTFromChatShowsHubOnLastTool(t *testing.T) {
	fx := installTools(t, "all")
	h := uitest.New(t)
	h.Resize(120, 40)
	h.Press("ctrl+t", "4") // the hub, then lazygit
	in := fx.instances["git-1"]
	if in == nil {
		t.Fatal("4 did not open git-1")
	}
	h.Press("esc") // back to the chat, git kept
	if n := h.Model.Nav(); n.Front != app.FrontTab || len(n.Tools) != 2 {
		t.Fatalf("esc: front %v tools %v, want the chat with the hub and git kept", n.Front, n.Tools)
	}
	h.Press("ctrl+t")
	onHub(t, h) // not straight into git
	if n := h.Model.Nav(); len(n.Tools) != 2 {
		t.Fatalf("ctrl+t closed or added an entry: %v", n.Tools)
	}
	h.Press("enter") // the cursor is on git's open row
	if got := h.Model.Focus(); got.RouteKey != gitRoute.Key() {
		t.Fatalf("ctrl+t enter: focus = %+v, want git-1", got)
	}
	if fx.instances["git-1"] != in || len(fx.open.list()) != 1 {
		t.Fatalf("ctrl+t enter rebuilt or reopened git (Open calls %v)", fx.open.list())
	}
	// From git: ctrl+t the chat, ctrl+t the hub, ctrl+t the chat again.
	h.Press("ctrl+t", "ctrl+t")
	onHub(t, h)
	h.Press("ctrl+t")
	if n := h.Model.Nav(); n.Front != app.FrontTab || len(n.Tools) != 2 {
		t.Fatalf("hub ctrl+t: front %v tools %v, want the chat, tools kept", n.Front, n.Tools)
	}
}

// With two instances open the hub's cursor starts on the one last shown.
func TestCtrlTHubCursorFollowsLastInstance(t *testing.T) {
	installTools(t, "all")
	h := uitest.New(t)
	h.Resize(120, 40)
	h.Press("ctrl+t", "1") // leet
	showHub(h)
	h.Press("4") // git
	h.Send(ui.NavigateMsg{Route: leetRoute})
	h.Press("esc", "ctrl+t")
	onHub(t, h)
	h.Press("enter")
	if got := h.Model.Focus(); got.RouteKey != leetRoute.Key() {
		t.Fatalf("ctrl+t enter: focus = %+v, want leet-1 (last shown)", got)
	}
	h.Press("esc", "ctrl+t", "4") // a digit still raises its tool
	if got := h.Model.Focus(); got.RouteKey != gitRoute.Key() {
		t.Fatalf("ctrl+t 4: focus = %+v, want git-1", got)
	}
	if n := h.Model.Nav(); !slices.ContainsFunc(n.Tools, func(r ui.Route) bool { return r.Key() == leetRoute.Key() }) {
		t.Fatalf("ctrl+t 4 closed leet: %v", n.Tools)
	}
}

// Review fix (tools-nav): the hub moves its cursor to the last instance only
// when ctrl+t brings it forward from the chat. A zoom on the hub (ctrl+w z)
// re-broadcasts ToolSwitchedMsg for the hub; that must not pull the cursor
// off the row the user picked.
func TestHubZoomKeepsCursor(t *testing.T) {
	fx := installTools(t, "all")
	h := uitest.New(t)
	h.Resize(120, 40)
	h.Press("ctrl+t", "4", "ctrl+t", "ctrl+t") // git, the chat, the hub (cursor on git)
	onHub(t, h)
	h.Press("home", "ctrl+w", "z") // the first tool's row, then zoom the hub
	onHub(t, h)
	h.Press("enter")
	if got := fx.open.list(); len(got) != 2 || !strings.HasPrefix(got[1], ui.ToolLeet+"|") {
		t.Fatalf("enter after zoom opened %v, want leet (the row picked), not git's open row", got)
	}
}

// Review fix (tools-nav): the hub was the last entry in front (the user moved
// its cursor, then ctrl+t to the chat): ctrl+t again shows the hub with the
// cursor where it was left, not on an older instance.
func TestHubChatHubKeepsCursor(t *testing.T) {
	fx := installTools(t, "all")
	h := uitest.New(t)
	h.Press("ctrl+t", "4", "ctrl+t", "ctrl+t") // git, the chat, the hub (cursor on git)
	onHub(t, h)
	h.Press("home", "ctrl+t", "ctrl+t") // the first tool's row, the chat, the hub
	onHub(t, h)
	h.Press("enter")
	if got := fx.open.list(); len(got) != 2 || !strings.HasPrefix(got[1], ui.ToolLeet+"|") {
		t.Fatalf("hub -> chat -> hub enter opened %v, want leet (the row left), not git", got)
	}
}
