package ui_test

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
)

func testSpec(id string, order int) ui.ToolSpec {
	return ui.ToolSpec{
		ID: id, Order: order, Icon: "🧪",
		Name: func(*i18n.T) string { return id },
		Open: func(*ui.Context, termhost.Mode) tea.Cmd { return nil },
	}
}

func freshRegistry(t *testing.T) {
	t.Helper()
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
}

func TestRegisterToolRejectsReservedPrefix(t *testing.T) {
	freshRegistry(t)
	for _, k := range ui.ReservedPrefixKeys {
		s := testSpec(ui.ToolGit, 4)
		s.PrefixKeys = []ui.PrefixKey{{Key: k}}
		mustPanic(t, "reserved prefix key "+k, func() { ui.RegisterTool(s) })
	}
	s := testSpec(ui.ToolGit, 4)
	s.PrefixKeys = []ui.PrefixKey{{Key: "r", Help: func(*i18n.T) string { return "refresh" }}}
	mustNotPanic(t, "free prefix key", func() { ui.RegisterTool(s) })
	if got := ui.Registered().Tools[ui.ToolGit].PrefixKeys; len(got) != 1 || got[0].Key != "r" {
		t.Errorf("Registered().Tools[git].PrefixKeys = %+v", got)
	}
}

func TestReservedPrefixKeysContract(t *testing.T) {
	want := []string{"esc", "h", "j", "k", "l", "left", "down", "up", "right", "z", "p", "t", "[", "x", "?", "ctrl+\\"}
	if !slices.Equal(ui.ReservedPrefixKeys, want) {
		t.Errorf("ReservedPrefixKeys = %q, want %q", ui.ReservedPrefixKeys, want)
	}
}

func TestRegisterToolRejectsDuplicates(t *testing.T) {
	freshRegistry(t)
	ui.RegisterTool(testSpec(ui.ToolLeet, 1))
	mustPanic(t, "duplicate id", func() { ui.RegisterTool(testSpec(ui.ToolLeet, 9)) })
	mustPanic(t, "duplicate order", func() { ui.RegisterTool(testSpec(ui.ToolArch, 1)) })
	mustPanic(t, "unknown id", func() { ui.RegisterTool(testSpec("chess", 7)) })
}

func TestToolsSortedAndFlagged(t *testing.T) {
	freshRegistry(t)
	ui.RegisterTool(testSpec(ui.ToolAPI, 5))
	ui.RegisterTool(testSpec(ui.ToolLeet, 1))
	ui.RegisterTool(testSpec(ui.ToolGit, 4))
	ui.RegisterTool(testSpec(ui.ToolArch, 3))

	t.Setenv("MASIL_FEATURES", "")
	if got := ui.Tools(); len(got) != 0 {
		t.Errorf("flags off: Tools() = %v, want none", ids(got))
	}
	if _, ok := ui.ToolByID(ui.ToolGit); ok {
		t.Error("flags off: ToolByID(git) found")
	}

	t.Setenv("MASIL_FEATURES", "all")
	if got, want := ids(ui.Tools()), []string{"leet", "arch", "git", "api"}; !slices.Equal(got, want) {
		t.Errorf("all: Tools() = %v, want %v", got, want)
	}

	t.Setenv("MASIL_FEATURES", "git,leet")
	if got, want := ids(ui.Tools()), []string{"leet", "git"}; !slices.Equal(got, want) {
		t.Errorf("git,leet: Tools() = %v, want %v", got, want)
	}
	if s, ok := ui.ToolByID(ui.ToolGit); !ok || s.Order != 4 {
		t.Errorf("ToolByID(git) = %+v, %v", s, ok)
	}
	if _, ok := ui.ToolByID(ui.ToolAPI); ok {
		t.Error("api is off: ToolByID(api) found")
	}
	if _, ok := ui.ToolByID(ui.ToolWorkspace); ok {
		t.Error("workspace never registered: ToolByID found it")
	}
}

func TestOpenAndCloseToolCmds(t *testing.T) {
	msg := ui.OpenTool(ui.ToolGit, "g1", map[string]string{"repo": "masil"})()
	nav, ok := msg.(ui.NavigateMsg)
	if !ok {
		t.Fatalf("OpenTool msg = %T", msg)
	}
	want := ui.Route{Kind: ui.RouteTool, ID: "g1", Arg: ui.ToolGit, Query: map[string]string{"repo": "masil"}}
	if nav.Route.Key() != want.Key() || nav.Route.Query["repo"] != "masil" {
		t.Errorf("OpenTool route = %+v, want %+v", nav.Route, want)
	}
	if got := ui.CloseTool("g1")(); got != (ui.CloseToolMsg{InstanceID: "g1"}) {
		t.Errorf("CloseTool msg = %#v", got)
	}
}

func ids(ss []ui.ToolSpec) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}
