package app_test

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// bgSeq is the truecolor background SGR of t's BG ("48;2;r;g;b").
func bgSeq(t *theme.Theme) string {
	r, g, b, _ := t.P.BG.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func runCmd(h *uitest.Harness, id string) {
	h.T.Helper()
	h.Send(ui.RunCommandMsg{ID: id})
}

func TestThemeCommandsPersist(t *testing.T) {
	dev := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
	h := uitest.New(t, uitest.WithRoute(dev))
	var got []theme.Name
	h.Instance(dev.Key()).(*stub.Chat).OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if tm, ok := msg.(ui.ThemeChangedMsg); ok {
			got = append(got, tm.Theme.Name)
		}
	}
	charmBG := bgSeq(theme.Get(theme.Charm))
	if strings.Contains(h.Styled(), charmBG) {
		t.Fatal("amber frame already contains the charm background")
	}

	runCmd(h, "theme.set.charm")
	if n := h.Context().Theme.Name; n != theme.Charm {
		t.Fatalf("theme.set.charm: Theme = %v", n)
	}
	if !reflect.DeepEqual(got, []theme.Name{theme.Charm}) {
		t.Fatalf("ThemeChangedMsg received = %v, want [charm]", got)
	}
	if cfg, err := config.Load(h.Context().Paths); err != nil || cfg.Theme != "charm" {
		t.Fatalf("config.Load: Theme = %q, err %v; want charm", cfg.Theme, err)
	}
	if h.Context().Config.Theme != "charm" {
		t.Fatalf("Context().Config.Theme = %q", h.Context().Config.Theme)
	}
	if !strings.Contains(h.Styled(), charmBG) {
		t.Fatal("frame not redrawn with the charm background")
	}
	h.Check()

	// theme.cycle: A → B → C → D → A.
	runCmd(h, "theme.set.amber")
	for _, want := range []theme.Name{theme.Charm, theme.Midnight, theme.Cream, theme.Amber, theme.Charm} {
		runCmd(h, "theme.cycle")
		if n := h.Context().Theme.Name; n != want {
			t.Fatalf("theme.cycle: %v, want %v", n, want)
		}
		if cfg, _ := config.Load(h.Context().Paths); cfg.Theme != string(want) {
			t.Fatalf("theme.cycle saved %q, want %q", cfg.Theme, want)
		}
	}
	if !strings.Contains(h.Styled(), charmBG) {
		t.Fatal("cycled to charm: frame lacks the charm background")
	}

	// An open overlay receives ThemeChangedMsg too (help redraws with the new theme).
	h.Press("?")
	runCmd(h, "theme.set.midnight")
	if !strings.Contains(h.Styled(), bgSeq(theme.Get(theme.Midnight))) {
		t.Fatal("midnight background missing with the help overlay open")
	}
	h.Check()
}

func TestThemeSaveFailureToasts(t *testing.T) {
	h := uitest.New(t)
	paths := h.Context().Paths
	_ = os.Remove(paths.Config)
	if err := os.Mkdir(paths.Config, 0o700); err != nil { // a directory where config.toml goes
		t.Fatal(err)
	}
	h.Send(ui.SetThemeMsg{Name: theme.Cream})
	if n := h.Context().Theme.Name; n != theme.Cream {
		t.Fatalf("theme must switch even when saving fails: %v", n)
	}
	if s := h.Screen(); !strings.Contains(s, "설정을 저장하지 못했어요") {
		t.Fatalf("save failure toast missing:\n%s", s)
	}
	h.Send(ui.SetThemeMsg{Name: "neon"}) // unknown name: ignored
	if n := h.Context().Theme.Name; n != theme.Cream {
		t.Fatalf("unknown theme name changed the theme to %v", n)
	}
}

func TestLayoutCommands(t *testing.T) {
	h := uitest.New(t)
	h.Resize(200, 60)
	runCmd(h, "layout.pin.compact")
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeCompact {
		t.Fatalf("layout.pin.compact at 200x60: mode %v", m)
	}
	if s := statusRow(h); !strings.Contains(s, "📌") {
		t.Fatalf("pinned: status %q, want 📌", s)
	}
	h.Check()
	for id, want := range map[string]layout.Mode{
		"layout.pin.command":  layout.ModeCommand,
		"layout.pin.standard": layout.ModeStandard,
		"layout.pin.widget":   layout.ModeWidget,
		"layout.pin.tiny":     layout.ModeTiny,
	} {
		runCmd(h, id)
		if m := h.Model.Frame().Layout.Mode; m != want {
			t.Fatalf("%s: mode %v, want %v", id, m, want)
		}
	}
	runCmd(h, "layout.auto")
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeCommand {
		t.Fatalf("layout.auto at 200x60: mode %v, want command", m)
	}
	if s := statusRow(h); strings.Contains(s, "📌") {
		t.Fatalf("auto: status %q still pinned", s)
	}
}

func TestNavCommands(t *testing.T) {
	h := uitest.New(t)
	runCmd(h, "help")
	if got := overlayTypes(h.Model); !reflect.DeepEqual(got, []string{"*app.helpView"}) {
		t.Fatalf("help: overlays %v", got)
	}
	h.Press("esc")

	runCmd(h, "settings")
	nav := h.Nav()
	st := nav.Tabs[nav.Active].Stack
	if top := st[len(st)-1]; top.Kind != ui.RouteSettings || top.Anchor != "" {
		t.Fatalf("settings: top %+v", top)
	}

	h2 := uitest.New(t)
	runCmd(h2, "blocks")
	nav = h2.Nav()
	st = nav.Tabs[nav.Active].Stack
	if top := st[len(st)-1]; !reflect.DeepEqual(top, ui.Route{Kind: ui.RouteSettings, Anchor: "blocks"}) {
		t.Fatalf("blocks: top %+v", top)
	}

	// quit asks first like logout: the empty palette lists it next to 로그아웃, and one
	// enter too many must not skip the ctrl+c-twice guard.
	runCmd(h2, "quit")
	if got := overlayTypes(h2.Model); !reflect.DeepEqual(got, []string{"*widgets.confirmDialog"}) {
		t.Fatalf("quit: overlays %v, want the Confirm", got)
	}
	if s := h2.Screen(); !strings.Contains(s, "종료할까요?") {
		t.Fatalf("quit confirm text missing:\n%s", s)
	}
	if quitSeen(h2) {
		t.Fatalf("quit: tea.QuitMsg before confirming in %v", h2.TeaMsgs())
	}
	h2.Press("esc")
	if n := len(h2.Model.Frame().Overlays); n != 0 || quitSeen(h2) {
		t.Fatalf("quit: esc must close the Confirm without quitting: %v %v", overlayTypes(h2.Model), h2.TeaMsgs())
	}
	h2.Press("ctrl+k")
	h2.Type("종료")
	h2.Press("enter")
	if got := overlayTypes(h2.Model); !reflect.DeepEqual(got, []string{"*widgets.confirmDialog"}) || quitSeen(h2) {
		t.Fatalf("palette enter on 종료: overlays %v, want only the Confirm and no quit", got)
	}
	h2.Press("y")
	if !quitSeen(h2) {
		t.Fatalf("quit: no tea.QuitMsg after y in %v", h2.TeaMsgs())
	}

	runCmd(h2, "logout")
	// The release has tools on, so logout asks with the tool-login wipe option (P0 Task 23).
	if got := overlayTypes(h2.Model); !reflect.DeepEqual(got, []string{"*widgets.confirmCheck"}) {
		t.Fatalf("logout: overlays %v, want the Confirm", got)
	}
	if s := h2.Screen(); !strings.Contains(s, "로그아웃할까요?") || !strings.Contains(s, "이 기기에서 로그아웃해요.") {
		t.Fatalf("logout confirm text missing:\n%s", s)
	}
	h2.Press("esc")

	runCmd(h2, "reconnect") // DisableRealtime: toast only
	if s := h2.Screen(); !strings.Contains(s, "다시 연결해요") {
		t.Fatalf("reconnect toast missing:\n%s", s)
	}
}

// TestCoreCommandsRegistered: every core command id of CA 5.6 is registered with its
// title, group and key hint.
func TestCoreCommandsRegistered(t *testing.T) {
	want := []struct{ id, title, group, hint string }{
		{"theme.cycle", "테마 바꾸기", palette.GroupSettings, ""},
		{"theme.set.charm", "테마: Charm 보라", palette.GroupSettings, ""},
		{"theme.set.midnight", "테마: 미드나잇 블루", palette.GroupSettings, ""},
		{"theme.set.cream", "테마: 크림 파스텔", palette.GroupSettings, ""},
		{"theme.set.amber", "테마: 레트로 앰버", palette.GroupSettings, ""},
		{"layout.pin.command", "레이아웃 고정: 지휘소", palette.GroupSettings, "ctrl+w 1"},
		{"layout.pin.standard", "레이아웃 고정: 표준", palette.GroupSettings, "ctrl+w 2"},
		{"layout.pin.compact", "레이아웃 고정: 컴팩트", palette.GroupSettings, "ctrl+w 3"},
		{"layout.pin.widget", "레이아웃 고정: 위젯", palette.GroupSettings, "ctrl+w 4"},
		{"layout.pin.tiny", "레이아웃 고정: 초소형", palette.GroupSettings, "ctrl+w 5"},
		{"layout.auto", "레이아웃 자동", palette.GroupSettings, "ctrl+w 0"},
		{"help", "도움말", palette.GroupNav, "?"},
		{"settings", "설정", palette.GroupNav, ""},
		{"blocks", "차단 목록", palette.GroupNav, ""},
		{"logout", "로그아웃", palette.GroupCommand, ""},
		{"quit", "종료", palette.GroupCommand, "ctrl+c ctrl+c"},
		{"reconnect", "다시 연결", palette.GroupCommand, ""},
	}
	for _, w := range want {
		c, ok := palette.Lookup(w.id)
		if !ok {
			t.Fatalf("command %q not registered", w.id)
		}
		if c.Title != w.title || c.Group != w.group || c.KeyHint != w.hint || c.Run == nil {
			t.Fatalf("%s = {%q %q %q run:%v}, want {%q %q %q}", w.id, c.Title, c.Group, c.KeyHint, c.Run != nil, w.title, w.group, w.hint)
		}
	}
}

// A config.toml that failed to parse is not overwritten by a theme switch: the
// theme applies for this run only and a toast asks to fix the file.
func TestThemeKeepsBrokenConfigFile(t *testing.T) {
	h := uitest.New(t)
	paths := h.Context().Paths
	body := "server = \"https://my.server.example\"\ntheme = \"cream\n[keys]\n\"palette.open\" = [\"ctrl+p\"]\n"
	if err := os.WriteFile(paths.Config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(paths)
	*h.Context().Config = *cfg // what main hands the app after a failed Load
	runCmd(h, "theme.set.midnight")
	if n := h.Context().Theme.Name; n != theme.Midnight {
		t.Fatalf("theme must still switch in memory: %v", n)
	}
	if b, _ := os.ReadFile(paths.Config); string(b) != body {
		t.Fatalf("config.toml overwritten:\n%s", b)
	}
	if s := h.Screen(); !strings.Contains(s, "config.toml 을 고친 뒤 다시 해 주세요") {
		t.Fatalf("fix-the-file toast missing:\n%s", s)
	}
}

// TestThemeSetCommandsFollowLocale: theme.set.* palette rows say the theme
// name in the shown language (P5 Task 9: theme names come from theme.Label);
// ko is the 0.1.2 Title.
func TestThemeSetCommandsFollowLocale(t *testing.T) {
	en := i18n.For(i18n.En)
	for _, th := range theme.All() {
		c, ok := palette.Lookup("theme.set." + string(th.Name))
		if !ok {
			t.Fatalf("theme.set.%s not registered", th.Name)
		}
		if got, want := c.TitleIn(en), "Theme: "+theme.Label(en, th.Name); got != want {
			t.Errorf("theme.set.%s en = %q, want %q", th.Name, got, want)
		}
		if got := c.TitleIn(i18n.KoT()); got != c.Title {
			t.Errorf("theme.set.%s ko = %q, want the 0.1.2 Title %q", th.Name, got, c.Title)
		}
	}
}
