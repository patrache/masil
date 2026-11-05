package app

import (
	"errors"
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Core palette commands (CA 5.6 core ui/app row) and the ui.RunCommandMsg dispatch
// (CR-B). Every registered command (core, chat, feed, fun) runs through runCommand.

// Texts.
const (
	unavailableText = "지금은 쓸 수 없는 명령이에요"
	saveFailedText  = "설정을 저장하지 못했어요"
	fixConfigText   = "config.toml 을 고친 뒤 다시 해 주세요"
	reconnectText   = "다시 연결해요"
)

// helpMsg opens the help overlay (the "help" command).
type helpMsg struct{}

// logoutConfirmedMsg is sent when the logout Confirm is accepted (logoutConfirmed, sync.go).
type logoutConfirmedMsg struct{}

// reconnectMsg restarts the realtime client (the "reconnect" command, bridge.go).
type reconnectMsg struct{}

// themeKeywords are the theme.set.<name> search keywords (titles use theme.Label).
var themeKeywords = map[theme.Name][]string{
	theme.Charm:    {"theme", "charm", "purple", "보라"},
	theme.Midnight: {"theme", "midnight", "blue", "블루"},
	theme.Cream:    {"theme", "cream", "pastel", "light", "파스텔"},
	theme.Amber:    {"theme", "amber", "retro", "레트로"},
}

// pinCommands are the layout.pin.<mode> commands in ctrl+w digit order.
var pinCommands = []struct {
	mode  layout.Mode
	title string
	hint  string
}{
	{layout.ModeCommand, "지휘소", "ctrl+w 1"},
	{layout.ModeStandard, "표준", "ctrl+w 2"},
	{layout.ModeCompact, "컴팩트", "ctrl+w 3"},
	{layout.ModeWidget, "위젯", "ctrl+w 4"},
	{layout.ModeTiny, "초소형", "ctrl+w 5"},
}

func msgCmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func init() {
	palette.Register(palette.Command{
		ID: "theme.cycle", Title: "테마 바꾸기", Group: "설정",
		Keywords: []string{"theme", "cycle", "color", "색"},
		Run: func(ctx *ui.Context, _ string) tea.Cmd {
			return msgCmd(ui.SetThemeMsg{Name: nextTheme(ctx.Theme)})
		},
	})
	for _, t := range theme.All() {
		name := t.Name
		palette.Register(palette.Command{
			ID: "theme.set." + string(name), Title: "테마: " + t.Label, Group: "설정",
			Keywords: themeKeywords[name],
			Run:      func(*ui.Context, string) tea.Cmd { return msgCmd(ui.SetThemeMsg{Name: name}) },
		})
	}
	for _, p := range pinCommands {
		mode := p.mode
		palette.Register(palette.Command{
			ID: "layout.pin." + mode.ID(), Title: "레이아웃 고정: " + p.title, Group: "설정", KeyHint: p.hint,
			Keywords: []string{"layout", "pin", mode.ID()},
			Run:      func(*ui.Context, string) tea.Cmd { return msgCmd(pinMsg{mode: &mode}) },
		})
	}
	palette.Register(palette.Command{
		ID: "layout.auto", Title: "레이아웃 자동", Group: "설정", KeyHint: "ctrl+w 0",
		Keywords: []string{"layout", "auto", "unpin"},
		Run:      func(*ui.Context, string) tea.Cmd { return msgCmd(pinMsg{}) },
	})
	palette.Register(palette.Command{
		ID: "help", Title: "도움말", Group: "이동", KeyHint: "?",
		Keywords: []string{"help", "keys", "단축키"},
		Run:      func(*ui.Context, string) tea.Cmd { return msgCmd(helpMsg{}) },
	})
	palette.Register(palette.Command{
		ID: "settings", Title: "설정", Group: "이동",
		Keywords: []string{"settings", "preferences", "config"},
		Run: func(*ui.Context, string) tea.Cmd {
			return ui.Navigate(ui.Route{Kind: ui.RouteSettings})
		},
	})
	palette.Register(palette.Command{
		ID: "blocks", Title: "차단 목록", Group: "이동",
		Keywords: []string{"blocks", "blocked", "차단 해제"},
		Run: func(*ui.Context, string) tea.Cmd {
			return ui.Navigate(ui.Route{Kind: ui.RouteSettings, Anchor: "blocks"})
		},
	})
	palette.Register(palette.Command{
		ID: "logout", Title: "로그아웃", Group: "명령",
		Keywords: []string{"logout", "sign out"},
		Run: func(*ui.Context, string) tea.Cmd {
			return ui.OpenOverlay(widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소",
				msgCmd(logoutConfirmedMsg{})))
		},
	})
	palette.Register(palette.Command{
		ID: "quit", Title: "종료", Group: "명령", KeyHint: "ctrl+c ctrl+c",
		Keywords: []string{"quit", "exit"},
		Run:      func(*ui.Context, string) tea.Cmd { return tea.Quit },
	})
	palette.Register(palette.Command{
		ID: "reconnect", Title: "다시 연결", Group: "명령",
		Keywords: []string{"reconnect", "connect", "연결"},
		Run:      func(*ui.Context, string) tea.Cmd { return msgCmd(reconnectMsg{}) },
	})
}

// nextTheme is the theme after t in A → B → C → D → A order.
func nextTheme(t *theme.Theme) theme.Name {
	all := theme.All()
	for i, o := range all {
		if t != nil && o.Name == t.Name {
			return all[(i+1)%len(all)].Name
		}
	}
	return all[0].Name
}

// runCommand is the ui.RunCommandMsg dispatch (CR-B). The app consumes the msg (it is
// never broadcast): ① unknown id => ignored (debug log); ② a palette opened by the
// app on top is closed first (so overlays the command opens are not stacked on it);
// ③ Available false => toast, Run not called; ④ Run(ctx, Args) with Args untouched.
func (m *Model) runCommand(msg ui.RunCommandMsg) tea.Cmd {
	c, ok := palette.Lookup(msg.ID)
	if !ok {
		if m.ctx.Debug {
			slog.Debug("unknown command", "id", msg.ID)
		}
		return nil
	}
	if m.palette != nil && m.topOverlay() == m.palette {
		m.popOverlay()
	}
	if c.Available != nil && !c.Available(m.ctx) {
		return m.toasts.push(m.ctx, ui.ToastMsg{Text: unavailableText, Kind: ui.ToastInfo})
	}
	if c.Run == nil {
		return nil
	}
	return c.Run(m.ctx, msg.Args)
}

// setTheme switches the theme at runtime (CA 5.1): ctx.Theme, Config.Theme and
// config.toml (failure => toast; a config.toml that did not load is left alone =>
// "fix it" toast), then ThemeChangedMsg to every live component and
// overlay. An unknown name is ignored.
func (m *Model) setTheme(n theme.Name) tea.Cmd {
	name, ok := theme.Parse(string(n))
	if !ok {
		return nil
	}
	t := theme.Get(name)
	m.ctx.Theme = t
	m.ctx.Config.Theme = string(name)
	var cmds []tea.Cmd
	if err := config.Save(m.ctx.Paths, m.ctx.Config); errors.Is(err, config.ErrNotLoaded) {
		// config.toml did not load: keep the theme for this run, never overwrite the file.
		cmds = append(cmds, m.toasts.push(m.ctx, ui.ToastMsg{Text: fixConfigText, Kind: ui.ToastError}))
	} else if err != nil {
		slog.Warn("save config", "err", err)
		cmds = append(cmds, m.toasts.push(m.ctx, ui.ToastMsg{Text: saveFailedText, Kind: ui.ToastError}))
	}
	cmds = append(cmds, m.broadcast(ui.ThemeChangedMsg{Theme: t}))
	return tea.Batch(cmds...)
}
