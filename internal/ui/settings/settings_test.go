package settings_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // home/channel stand-ins under the settings route
	os.Exit(m.Run())
}

const (
	sessionCurrent = "01JSESSION0000000000000001"
	sessionOther   = "01JSESSION0000000000000002"
)

// fakeServer holds the settings state served by the canned /v1/me/settings routes.
type fakeServer struct {
	mu       sync.Mutex
	settings api.Settings
	blocks   []api.BlockEntry
}

// serverSettings are the initial server settings (social off so a toggle sends true).
func serverSettings() api.Settings {
	return api.Settings{
		DMPolicy: "friends_and_spaces",
		Notify: api.NotifySettings{
			DM: true, Mention: true, Invite: true, FriendRequest: true, ThreadReply: true, Social: false,
		},
		FocusDefaultMin: 25,
	}
}

func blockOf(u api.UserSummary) api.BlockEntry {
	return api.BlockEntry{User: u, CreatedAt: time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)}
}

var (
	hana  = api.UserSummary{ID: fixtures.UserHana, Handle: "hana", DisplayName: "하나"}
	junho = api.UserSummary{ID: fixtures.UserJunho, Handle: "junho", DisplayName: "준호"}
	minji = api.UserSummary{ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"}
)

// setup registers GET/PATCH /v1/me/settings, GET /v1/blocks (2 entries), DELETE
// /v1/blocks/{user}, GET /v1/auth/sessions (current + 1 other) and DELETE
// /v1/auth/sessions/{id} on the harness server.
func setup(h *uitest.Harness) *fakeServer {
	fs := &fakeServer{settings: serverSettings(), blocks: []api.BlockEntry{blockOf(hana), blockOf(junho)}}
	h.API.Handle("GET /v1/me/settings", func(w http.ResponseWriter, _ *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"settings": fs.settings})
	})
	h.API.Handle("PATCH /v1/me/settings", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if err := json.NewDecoder(r.Body).Decode(&fs.settings); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": fs.settings})
	})
	h.API.Handle("GET /v1/blocks", func(w http.ResponseWriter, _ *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"items": fs.blocks})
	})
	h.API.Handle("DELETE /v1/blocks/{user}", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		u := r.PathValue("user")
		kept := fs.blocks[:0:0]
		for _, b := range fs.blocks {
			if "@"+b.User.Handle != u && b.User.ID != u {
				kept = append(kept, b)
			}
		}
		fs.blocks = kept
		w.WriteHeader(http.StatusNoContent)
	})
	h.API.JSON("GET /v1/auth/sessions", http.StatusOK, map[string]any{"items": []api.Session{
		{ID: sessionCurrent, Client: "tui", DeviceName: "sam-macbook", Current: true,
			CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), LastUsedAt: time.Date(2026, 11, 5, 2, 4, 0, 0, time.UTC)},
		{ID: sessionOther, Client: "daemon", DeviceName: "work-linux",
			CreatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), LastUsedAt: time.Date(2026, 11, 4, 23, 5, 0, 0, time.UTC)},
	}})
	h.API.Handle("DELETE /v1/auth/sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return fs
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// open navigates to the settings screen (anchor = section id or "").
func open(h *uitest.Harness, anchor string) {
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSettings, Anchor: anchor}})
}

// newSettings is a harness at 120x40 showing the settings screen.
func newSettings(t *testing.T, anchor string, opts ...uitest.Option) (*uitest.Harness, *fakeServer) {
	t.Helper()
	h := uitest.New(t, opts...)
	fs := setup(h)
	open(h, anchor)
	return h, fs
}

// selectedLine is the screen line whose row starts with the selection marker "› "
// (after the pane border and, in the wide layout, the section list).
func selectedLine(h *uitest.Harness) string {
	for _, l := range strings.Split(h.Screen(), "\n") {
		for _, part := range strings.Split(l, "│") {
			if strings.HasPrefix(strings.TrimLeft(part, " ║╔╚═|─╭╰"), "› ") {
				return l
			}
		}
	}
	return ""
}

// selectRow moves the selection down (j) until the selected line contains label.
func selectRow(t *testing.T, h *uitest.Harness, label string) {
	t.Helper()
	for range 80 {
		if strings.Contains(selectedLine(h), label) {
			return
		}
		h.Press("j")
	}
	t.Fatalf("row %q not reachable with j:\n%s", label, h.Screen())
}

// lineWith is the first screen line containing s.
func lineWith(h *uitest.Harness, s string) string {
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// requests returns the recorded requests to method path.
func requests(srv *apitest.Server, method, path string) []apitest.Recorded {
	var out []apitest.Recorded
	for _, r := range srv.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// lastJSON is the compact JSON body of the last request to method path.
func lastJSON(t *testing.T, srv *apitest.Server, method, path string) string {
	t.Helper()
	rs := requests(srv, method, path)
	if len(rs) == 0 {
		t.Fatalf("no %s %s request", method, path)
	}
	var v any
	if err := json.Unmarshal(rs[len(rs)-1].Body, &v); err != nil {
		t.Fatalf("decode %s %s body %q: %v", method, path, rs[len(rs)-1].Body, err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func loadConfig(t *testing.T, h *uitest.Harness) *config.Config {
	t.Helper()
	c, err := config.Load(h.Context().Paths)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return c
}

func TestSettingsGolden(t *testing.T) {
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		setup(h)
		open(h, "")
		h.Golden("settings")
	})
}

func TestSettingsSectionGoldens(t *testing.T) {
	for _, sec := range []string{"blocks", "sessions", "notify"} {
		t.Run(sec, func(t *testing.T) {
			uitest.ForEachSize(t, uitest.Required, func(t *testing.T, h *uitest.Harness) {
				setup(h)
				open(h, sec)
				h.Golden(sec)
			})
		})
	}
}

func TestSettingsAllSizesIntegrity(t *testing.T) {
	h, _ := newSettings(t, "")
	for _, sec := range []string{"theme", "layout", "display", "notify", "privacy", "focus", "blocks", "sessions", "password", "logout"} {
		open(h, sec)
		for _, sz := range uitest.AllSizes() {
			h.Resize(sz.W, sz.H)
			h.Check()
			if h.Model.Frame().Layout.Mode == layout.ModeTooSmall {
				continue
			}
			if selectedLine(h) == "" {
				t.Fatalf("%s %dx%d: no selected row visible:\n%s", sec, sz.W, sz.H, h.Screen())
			}
		}
		h.Resize(120, 40)
	}
}

func TestSettingsSectionTitles(t *testing.T) {
	h, _ := newSettings(t, "")
	h.Resize(200, 60)
	for _, title := range []string{"테마", "레이아웃", "화면", "알림", "개인정보", "집중 모드", "차단 목록", "기기·세션", "비밀번호", "로그아웃"} {
		if !strings.Contains(h.Screen(), title) {
			t.Errorf("section %q missing from the wide section list:\n%s", title, h.Screen())
		}
	}
}

func TestThemeRowPreviewAndApply(t *testing.T) {
	h, _ := newSettings(t, "", uitest.WithTheme(theme.Midnight))
	if !strings.Contains(selectedLine(h), "테마") {
		t.Fatalf("the theme row must be selected first:\n%s", h.Screen())
	}
	for _, th := range theme.All() {
		l := lineWith(h, th.Label)
		if l == "" || !strings.Contains(l, th.Letter+" ") {
			t.Fatalf("swatch %s %q missing:\n%s", th.Letter, th.Label, h.Screen())
		}
	}
	h.Press("right")
	if got := h.Context().Theme.Name; got != theme.Cream {
		t.Fatalf("theme after → = %s, want cream", got)
	}
	if c := loadConfig(t, h); c.Theme != "cream" {
		t.Fatalf("config theme = %q, want cream", c.Theme)
	}
	h.Press("left")
	if got := h.Context().Theme.Name; got != theme.Midnight {
		t.Fatalf("theme after ← = %s, want midnight", got)
	}
	h.Check()
}

func TestLayoutPinRow(t *testing.T) {
	h, _ := newSettings(t, "layout")
	if !strings.Contains(selectedLine(h), "레이아웃 고정") {
		t.Fatalf("layout row not selected:\n%s", h.Screen())
	}
	h.Press("right") // auto -> command
	if c := loadConfig(t, h); c.Layout.Pin != "command" {
		t.Fatalf("config layout.pin = %q, want command", c.Layout.Pin)
	}
	h.Resize(200, 60)
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeCommand {
		t.Fatalf("mode = %v, want command (pinned)", m)
	}
	h.Resize(120, 40)
	h.Press("left") // command -> auto
	if c := loadConfig(t, h); c.Layout.Pin != "auto" {
		t.Fatalf("config layout.pin = %q, want auto", c.Layout.Pin)
	}
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeStandard {
		t.Fatalf("mode = %v, want standard (auto at 120x40)", m)
	}
}

func TestLocalToggles(t *testing.T) {
	t.Run("mouse", func(t *testing.T) {
		h, _ := newSettings(t, "display")
		if h.Model.View().MouseMode == tea.MouseModeNone {
			t.Fatal("mouse must start on")
		}
		selectRow(t, h, "마우스")
		h.Press("enter")
		if h.Model.View().MouseMode != tea.MouseModeNone {
			t.Fatal("mouse off => View().MouseMode must be None")
		}
		if c := loadConfig(t, h); c.UI.Mouse {
			t.Fatal("config ui.mouse not saved")
		}
		h.Press("space")
		if h.Model.View().MouseMode == tea.MouseModeNone || !loadConfig(t, h).UI.Mouse {
			t.Fatal("space must toggle mouse back on")
		}
	})
	t.Run("animations", func(t *testing.T) {
		h, _ := newSettings(t, "display")
		selectRow(t, h, "애니메이션")
		before := h.Context().Config.UI.Animations
		h.Press("enter")
		if h.Context().Config.UI.Animations == before || loadConfig(t, h).UI.Animations == before {
			t.Fatal("animations not toggled/saved")
		}
	})
	t.Run("images", func(t *testing.T) {
		h, _ := newSettings(t, "display", uitest.WithUnicodeCore(true))
		h.Send(tea.FocusMsg{}) // the app learns FocusReports at run time
		selectRow(t, h, "이미지")
		h.Press("right") // halfblock -> off
		if g := h.Context().Caps.Graphics; g != termcaps.GraphicsNone {
			t.Fatalf("Caps.Graphics = %v, want None", g)
		}
		if c := loadConfig(t, h); c.UI.Images != "off" {
			t.Fatalf("config ui.images = %q, want off", c.UI.Images)
		}
		if !h.Context().Caps.UnicodeCore || !h.Context().Caps.FocusReports {
			t.Fatal("UnicodeCore / FocusReports learned at run time must be kept")
		}
		h.Press("left") // off -> halfblock
		if g := h.Context().Caps.Graphics; g != termcaps.GraphicsHalfBlock {
			t.Fatalf("Caps.Graphics = %v, want HalfBlock", g)
		}
	})
	t.Run("paint_background", func(t *testing.T) {
		h, _ := newSettings(t, "display")
		if h.Model.View().BackgroundColor == nil {
			t.Fatal("paint_background must start on")
		}
		selectRow(t, h, "배경색")
		h.Press("enter")
		if h.Model.View().BackgroundColor != nil {
			t.Fatal("paint_background off => View().BackgroundColor must be nil")
		}
		if loadConfig(t, h).UI.PaintBackground {
			t.Fatal("config ui.paint_background not saved")
		}
	})
	t.Run("clock and desktop", func(t *testing.T) {
		h, _ := newSettings(t, "display")
		selectRow(t, h, "시계")
		h.Press("right")
		if c := loadConfig(t, h); c.UI.Clock != "12h" {
			t.Fatalf("config ui.clock = %q, want 12h", c.UI.Clock)
		}
		open(h, "notify")
		selectRow(t, h, "데스크톱 알림")
		h.Press("enter")
		if loadConfig(t, h).Notify.Desktop {
			t.Fatal("config notify.desktop not saved")
		}
	})
}

// A config.toml that failed to parse is never overwritten from the settings screen:
// the change applies for this run and the row asks to fix the file first.
func TestLocalSaveKeepsBrokenConfigFile(t *testing.T) {
	h, _ := newSettings(t, "display")
	paths := h.Context().Paths
	body := "server = \"https://my.server.example\"\ntheme = \"cream\n[keys]\n\"palette.open\" = [\"ctrl+p\"]\n"
	if err := os.WriteFile(paths.Config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(paths)
	*h.Context().Config = *cfg
	selectRow(t, h, "시계")
	h.Press("right")
	if h.Context().Config.UI.Clock != "12h" {
		t.Fatalf("clock must change in memory: %q", h.Context().Config.UI.Clock)
	}
	if b, _ := os.ReadFile(paths.Config); string(b) != body {
		t.Fatalf("config.toml overwritten:\n%s", b)
	}
	if !strings.Contains(h.Screen(), "config.toml 을 고친 뒤 다시 해 주세요") {
		t.Fatalf("fix-the-file hint missing:\n%s", h.Screen())
	}
}

func TestServerSettings(t *testing.T) {
	h, _ := newSettings(t, "notify")
	if n := len(requests(h.API, "GET", "/v1/me/settings")); n != 1 {
		t.Fatalf("%d GET /v1/me/settings on open, want 1", n)
	}
	if !strings.Contains(lineWith(h, "소셜 알림"), "꺼짐") {
		t.Fatalf("social must show the server value (off):\n%s", h.Screen())
	}

	selectRow(t, h, "소셜 알림")
	h.Press("enter")
	if got := lastJSON(t, h.API, "PATCH", "/v1/me/settings"); got != `{"notify":{"social":true}}` {
		t.Fatalf("social body = %s", got)
	}
	if !h.Context().Store.Settings.Notify.Social || !strings.Contains(lineWith(h, "소셜 알림"), "켜짐") {
		t.Fatalf("Store.Settings not updated from the response:\n%s", h.Screen())
	}

	open(h, "privacy")
	selectRow(t, h, "DM 받기")
	h.Press("right")
	if got := lastJSON(t, h.API, "PATCH", "/v1/me/settings"); got != `{"dm_policy":"friends_only"}` {
		t.Fatalf("dm policy body = %s", got)
	}
	if !strings.Contains(lineWith(h, "DM 받기"), "친구만") {
		t.Fatalf("dm policy row:\n%s", h.Screen())
	}
	selectRow(t, h, "비공개 계정")
	h.Press("enter")
	if got := lastJSON(t, h.API, "PATCH", "/v1/me/settings"); got != `{"is_private":true}` {
		t.Fatalf("private body = %s", got)
	}

	open(h, "focus")
	selectRow(t, h, "집중 기본 시간")
	if !strings.Contains(selectedLine(h), "25분") {
		t.Fatalf("focus default row:\n%s", h.Screen())
	}
	h.Press("enter", "backspace", "backspace")
	h.Type("30")
	h.Press("enter")
	if got := lastJSON(t, h.API, "PATCH", "/v1/me/settings"); got != `{"focus_default_min":30}` {
		t.Fatalf("focus body = %s", got)
	}
	if !strings.Contains(lineWith(h, "집중 기본 시간"), "30분") || h.Context().Store.Settings.FocusDefaultMin != 30 {
		t.Fatalf("focus default not applied:\n%s", h.Screen())
	}

	// out of range: no request, inline error
	n := len(requests(h.API, "PATCH", "/v1/me/settings"))
	h.Press("enter", "backspace", "backspace")
	h.Type("0")
	h.Press("enter")
	if len(requests(h.API, "PATCH", "/v1/me/settings")) != n {
		t.Fatal("0 minutes must not be sent")
	}
	if !strings.Contains(h.Screen(), "1~720") {
		t.Fatalf("range error missing:\n%s", h.Screen())
	}
	h.Press("esc") // leaves the field, value back to 30
	if !strings.Contains(lineWith(h, "집중 기본 시간"), "30분") {
		t.Fatalf("esc must restore the value:\n%s", h.Screen())
	}

	// 400: inline error, value reverted
	h.API.Error("PATCH /v1/me/settings", http.StatusBadRequest, "invalid_request")
	open(h, "notify")
	selectRow(t, h, "소셜 알림")
	h.Press("enter")
	if !strings.Contains(h.Screen(), "입력값을 확인해 주세요") {
		t.Fatalf("inline error missing:\n%s", h.Screen())
	}
	if !strings.Contains(lineWith(h, "소셜 알림"), "켜짐") || !h.Context().Store.Settings.Notify.Social {
		t.Fatalf("value must be reverted to on:\n%s", h.Screen())
	}
	for _, sz := range uitest.Required {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

func TestSettingsUpdatedEvent(t *testing.T) {
	h, _ := newSettings(t, "privacy")
	s := serverSettings()
	s.IsPrivate = true
	h.Event("settings.updated", map[string]any{"settings": s})
	if !strings.Contains(lineWith(h, "비공개 계정"), "켜짐") {
		t.Fatalf("settings.updated must refresh the rows:\n%s", h.Screen())
	}
}

func TestBlocksSection(t *testing.T) {
	h, fs := newSettings(t, "blocks")
	if !strings.Contains(selectedLine(h), "@hana") {
		t.Fatalf("Anchor blocks must open on the first entry:\n%s", h.Screen())
	}
	if !strings.Contains(h.Screen(), "@junho") {
		t.Fatalf("second entry missing:\n%s", h.Screen())
	}
	if !h.Context().Store.IsBlocked(fixtures.UserHana) || !h.Context().Store.IsBlocked(fixtures.UserJunho) {
		t.Fatal("Store.Blocks must hold the listed set")
	}
	h.Press("enter")
	if !strings.Contains(h.Screen(), "차단을 풀까요?") {
		t.Fatalf("confirm missing:\n%s", h.Screen())
	}
	h.Check()
	h.Press("y")
	if len(requests(h.API, "DELETE", "/v1/blocks/@hana")) != 1 {
		t.Fatalf("DELETE /v1/blocks/@hana not sent: %+v", h.API.Requests())
	}
	if strings.Contains(h.Screen(), "@hana") {
		t.Fatalf("unblocked row still shown:\n%s", h.Screen())
	}
	if h.Context().Store.IsBlocked(fixtures.UserHana) {
		t.Fatal("Store.Blocks still holds hana")
	}
	if !strings.Contains(selectedLine(h), "@junho") {
		t.Fatalf("selection must move to the next entry:\n%s", h.Screen())
	}

	// n on the confirm: nothing sent
	h.Press("enter", "n")
	if len(requests(h.API, "DELETE", "/v1/blocks/@junho")) != 0 || !strings.Contains(h.Screen(), "@junho") {
		t.Fatal("cancel must not unblock")
	}

	// block.updated => refetch
	gets := len(requests(h.API, "GET", "/v1/blocks"))
	fs.mu.Lock()
	fs.blocks = append(fs.blocks, blockOf(minji))
	fs.mu.Unlock()
	h.Event("block.updated", map[string]any{"user_id": fixtures.UserMinji, "blocked": true})
	if len(requests(h.API, "GET", "/v1/blocks")) != gets+1 {
		t.Fatal("block.updated must refetch the list")
	}
	if !strings.Contains(h.Screen(), "@minji") {
		t.Fatalf("refetched entry missing:\n%s", h.Screen())
	}
}

func TestBlocksEmptyAndError(t *testing.T) {
	h := uitest.New(t)
	h.API.JSON("GET /v1/blocks", http.StatusOK, map[string]any{"items": []api.BlockEntry{}})
	open(h, "blocks")
	if !strings.Contains(h.Screen(), "차단한 사람이 없어요") {
		t.Fatalf("empty state missing:\n%s", h.Screen())
	}
	h.API.Error("GET /v1/blocks", http.StatusInternalServerError, "internal_error")
	h.Event("block.updated", map[string]any{"user_id": fixtures.UserMinji, "blocked": true})
	if !strings.Contains(h.Screen(), api.Describe(&api.Error{Code: "internal_error"})) {
		t.Fatalf("load error missing:\n%s", h.Screen())
	}
	h.Check()
}

func TestSessionsSection(t *testing.T) {
	h, _ := newSettings(t, "sessions")
	cur := lineWith(h, "sam-macbook")
	if !strings.Contains(cur, "현재 기기") {
		t.Fatalf("current session must be marked:\n%s", h.Screen())
	}
	if strings.Contains(lineWith(h, "work-linux"), "현재 기기") {
		t.Fatal("other session marked current")
	}
	if !strings.Contains(selectedLine(h), "sam-macbook") {
		t.Fatalf("sessions anchor must select the first session:\n%s", h.Screen())
	}
	h.Press("enter") // current: not revocable
	if strings.Contains(h.Screen(), "로그아웃할까요?") {
		t.Fatalf("the current session must not be revocable:\n%s", h.Screen())
	}
	h.Press("j", "enter")
	if !strings.Contains(h.Screen(), "이 기기를 로그아웃할까요?") {
		t.Fatalf("confirm missing:\n%s", h.Screen())
	}
	h.Press("y")
	if len(requests(h.API, "DELETE", "/v1/auth/sessions/"+sessionOther)) != 1 {
		t.Fatalf("DELETE session not sent: %+v", h.API.Requests())
	}
	if strings.Contains(h.Screen(), "work-linux") {
		t.Fatalf("revoked session still listed:\n%s", h.Screen())
	}
	if len(requests(h.API, "DELETE", "/v1/auth/sessions/"+sessionCurrent)) != 0 {
		t.Fatal("current session revoked")
	}
}

func TestChangePassword(t *testing.T) {
	h, _ := newSettings(t, "password")
	creds := &auth.Credentials{Version: 1, Server: h.API.URL, Token: "msl_test", UserID: fixtures.UserSam, Handle: "sam"}
	if err := auth.Save(h.Context().Paths, creds); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(selectedLine(h), "현재 비밀번호") {
		t.Fatalf("password anchor:\n%s", h.Screen())
	}
	// enter edits; enter moves on to the next field; the last enter submits
	h.Press("enter")
	h.Type("old password1")
	h.Press("enter")
	h.Type("new password1")
	h.Press("enter")
	h.Type("new password2")
	h.Press("enter")
	if !strings.Contains(h.Screen(), "비밀번호가 서로 달라요") {
		t.Fatalf("mismatch error missing:\n%s", h.Screen())
	}
	if len(requests(h.API, "POST", "/v1/auth/password")) != 0 {
		t.Fatal("mismatch must not be sent")
	}
	for _, s := range []string{"old password1", "new password1", "new password2"} {
		if strings.Contains(h.Styled(), s) {
			t.Fatalf("password %q drawn", s)
		}
	}

	// 401 invalid_credentials: own text, still logged in
	h.API.Error("POST /v1/auth/password", http.StatusUnauthorized, "invalid_credentials")
	h.Press("ctrl+c") // clears the confirm field (value present)
	if strings.Contains(h.Screen(), "한 번 더 누르면 종료해요") {
		t.Fatal("ctrl+c on a non-empty field must only clear it")
	}
	h.Type("new password1")
	h.Press("enter")
	if got := lastJSON(t, h.API, "POST", "/v1/auth/password"); got != `{"current_password":"old password1","new_password":"new password1"}` {
		t.Fatalf("password body = %s", got)
	}
	if !strings.Contains(h.Screen(), "현재 비밀번호가 맞지 않아요") {
		t.Fatalf("invalid current password text missing:\n%s", h.Screen())
	}
	if strings.Contains(h.Screen(), "다른 곳에서 로그아웃됐어요") {
		t.Fatal("a wrong current password is not a logout")
	}
	if _, err := auth.Load(h.Context().Paths); err != nil {
		t.Fatalf("credentials must be kept: %v", err)
	}
	if h.Context().API.Token() != "msl_test" {
		t.Fatal("the API token must be kept")
	}
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteSettings {
		t.Fatalf("Main = %+v, want settings (still logged in)", r)
	}

	// the current field was cleared and is edited again; 204 => toast
	h.API.Handle("POST /v1/auth/password", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	sessions := len(requests(h.API, "GET", "/v1/auth/sessions"))
	h.Type("old password2")
	h.Press("down", "down", "down", "enter") // new, confirm (kept), button
	if got := lastJSON(t, h.API, "POST", "/v1/auth/password"); got != `{"current_password":"old password2","new_password":"new password1"}` {
		t.Fatalf("retry body = %s", got)
	}
	if !strings.Contains(h.Screen(), "비밀번호를 바꿨어요") {
		t.Fatalf("success toast missing:\n%s", h.Screen())
	}
	if len(requests(h.API, "GET", "/v1/auth/sessions")) != sessions+1 {
		t.Fatal("sessions must be refetched (other sessions were revoked)")
	}
	h.Check()
}

func TestPasswordValidation(t *testing.T) {
	h, _ := newSettings(t, "password")
	selectRow(t, h, "비밀번호 바꾸기")
	h.Press("enter")
	if !strings.Contains(h.Screen(), "현재 비밀번호를 입력해 주세요") {
		t.Fatalf("empty current error missing:\n%s", h.Screen())
	}
	h.Type("old password1")
	h.Press("enter")
	h.Type("short")
	h.Press("enter", "enter")
	if !strings.Contains(h.Screen(), "비밀번호는 8자 이상이에요") {
		t.Fatalf("short error missing:\n%s", h.Screen())
	}
	if len(requests(h.API, "POST", "/v1/auth/password")) != 0 {
		t.Fatal("invalid form sent")
	}
}

func TestSettingsCtrlC(t *testing.T) {
	h, _ := newSettings(t, "focus")
	h.Press("enter") // edits "25"
	h.Press("ctrl+c")
	if strings.Contains(h.Screen(), "한 번 더 누르면 종료해요") {
		t.Fatal("ctrl+c on a non-empty field must only clear it")
	}
	h.Press("ctrl+c")
	if !strings.Contains(h.Screen(), "한 번 더 누르면 종료해요") {
		t.Fatalf("ctrl+c on an empty field must arm quitting:\n%s", h.Screen())
	}
	for _, m := range h.TeaMsgs() {
		if _, ok := m.(tea.QuitMsg); ok {
			t.Fatal("one ctrl+c must not quit")
		}
	}
}

func TestSettingsPasteSanitized(t *testing.T) {
	h, _ := newSettings(t, "focus")
	h.Press("enter", "backspace", "backspace")
	h.Paste("4\x1b[31m5\r\n")
	h.Press("enter")
	if got := lastJSON(t, h.API, "PATCH", "/v1/me/settings"); got != `{"focus_default_min":45}` {
		t.Fatalf("pasted body = %s", got)
	}
}

func TestLogoutRow(t *testing.T) {
	h, _ := newSettings(t, "logout")
	if !strings.Contains(selectedLine(h), "로그아웃") {
		t.Fatalf("logout row:\n%s", h.Screen())
	}
	h.Press("enter")
	if !strings.Contains(h.Screen(), "로그아웃할까요?") {
		t.Fatalf("logout confirm missing:\n%s", h.Screen())
	}
	h.Press("y")
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin {
		t.Fatalf("Main = %+v after logout, want login", r)
	}
}

func TestAnchorRouteUpdated(t *testing.T) {
	h, _ := newSettings(t, "")
	inst := h.Instance(ui.Route{Kind: ui.RouteSettings}.Key())
	if inst == nil {
		t.Fatal("settings instance missing")
	}
	h.Send(ui.RunCommandMsg{ID: "blocks"})
	if h.Instance(ui.Route{Kind: ui.RouteSettings}.Key()) != inst {
		t.Fatal("the cached instance must be reused")
	}
	if !strings.Contains(selectedLine(h), "@hana") {
		t.Fatalf("RouteUpdatedMsg must move to the blocks section:\n%s", h.Screen())
	}
}

func TestSettingsNavigationKeys(t *testing.T) {
	h, _ := newSettings(t, "")
	h.Press("pgdown")
	if !strings.Contains(selectedLine(h), "레이아웃 고정") {
		t.Fatalf("pgdown must jump to the next section:\n%s", h.Screen())
	}
	h.Press("pgup", "k") // first row; k stays there
	if !strings.Contains(selectedLine(h), "테마") {
		t.Fatalf("pgup/k:\n%s", h.Screen())
	}
	h.Press("G")
	if !strings.Contains(selectedLine(h), "로그아웃") {
		t.Fatalf("G must select the last row:\n%s", h.Screen())
	}
	h.Press("esc") // back: leaves the settings screen
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind == ui.RouteSettings {
		t.Fatal("esc must go back")
	}
}

func TestSettingsMouse(t *testing.T) {
	h, _ := newSettings(t, "")
	h.Resize(200, 60)
	y, x := -1, -1
	for i, l := range strings.Split(h.Screen(), "\n") {
		if j := strings.Index(l, "기기·세션"); j >= 0 {
			y, x = i, textwidth.Width(l[:j])
			break
		}
	}
	if y < 0 {
		t.Fatalf("section list entry missing:\n%s", h.Screen())
	}
	h.Click(x+1, y)
	if !strings.Contains(selectedLine(h), "sam-macbook") {
		t.Fatalf("clicking a section must jump there:\n%s", h.Screen())
	}
	h.Check()
}

func TestSettingsCursor(t *testing.T) {
	h, _ := newSettings(t, "focus")
	if h.Cursor() != nil {
		t.Fatal("no cursor while not editing")
	}
	h.Press("enter")
	c := h.Cursor()
	if c == nil {
		t.Fatal("cursor missing while editing")
	}
	lines := strings.Split(h.Screen(), "\n")
	if c.Y < 0 || c.Y >= len(lines) || !strings.Contains(lines[c.Y], "집중 기본 시간") {
		t.Fatalf("cursor row %d not on the field:\n%s", c.Y, h.Screen())
	}
}
