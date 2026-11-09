package workspace

import (
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

func TestAlertTemplatesRendered(t *testing.T) {
	a := api.Alert{Template: &api.AlertTemplate{ID: "workspace.invite", Params: map[string]any{"actor": "minji", "share": "x"}}}
	title, body := notify.Render(a, i18n.KoT())
	if title != "minji님이 작업공간을 공유했어요" || body != "x" {
		t.Fatalf("invite = %q / %q", title, body)
	}
	a.Template = &api.AlertTemplate{ID: "workspace.attention", Params: map[string]any{"share": "x"}}
	if title, body := notify.Render(a, i18n.KoT()); title != "x" || body != "공유 중인 작업공간에서 확인이 필요해요" {
		t.Fatalf("attention = %q / %q", title, body)
	}
}

func TestAlertEnterOpensJoin(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	cmd, ok := ui.AlertTargetCmd(h.Context(), api.Alert{Target: api.AlertTarget{Type: "workspace_share", ID: "S1"}})
	if !ok || cmd == nil {
		t.Fatalf("workspace_share target not registered (ok %v)", ok)
	}
	nav, isNav := cmd().(ui.NavigateMsg)
	if !isNav || nav.Route.Kind != ui.RouteTool || nav.Route.Arg != ui.ToolWorkspace || nav.Route.Query["join"] != "S1" {
		t.Fatalf("alert opens %#v", nav)
	}
	t.Setenv("MASIL_FEATURES", "")
	if cmd, _ := ui.AlertTargetCmd(h.Context(), api.Alert{Target: api.AlertTarget{Type: "workspace_share", ID: "S1"}}); cmd != nil {
		t.Fatal("flag off: the alert should fall back to the inbox")
	}
}
