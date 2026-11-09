package leet_test

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
	"github.com/patrache/masil/internal/ui"
	_ "github.com/patrache/masil/internal/ui/chat" // the real channel screen (cards)
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	_ "github.com/patrache/masil/internal/ui/tools" // the hub and ctrl+t
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // stand-ins for the routes no imported package registers
	os.Exit(m.Run())
}

// leetSizes are the tool's golden sizes (Global Constraints).
var leetSizes = append(slices.Clone(uitest.Required), uitest.Size{W: 120, H: 30}, uitest.Size{W: 40, H: 14})

// env is one test's fake LeetCode and pane launcher.
type env struct {
	lc   *leetcodetest.Server
	pane *termpanetest.Fake
	home string
}

// setup turns the tool on, points it at a fake LeetCode, gives it a temp
// home (the solutions folder) and a fake pane launcher.
func setup(t *testing.T) *env {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "leet")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{leetcode.EnvSession, leetcode.EnvCSRF, leetcode.EnvCFClearance, "EDITOR"} {
		t.Setenv(k, "")
	}
	lc := leetcodetest.New(t)
	t.Cleanup(leet.SetLeetcodeForTest(lc.URL))
	return &env{lc: lc, pane: termpanetest.Install(t), home: home}
}

// firstRunDone marks the first-run notice as seen.
func firstRunDone(t *testing.T, h *uitest.Harness) {
	t.Helper()
	if err := h.Context().ToolState.SetSection("leet", map[string]any{"first_run_done": true}); err != nil {
		t.Fatal(err)
	}
}

// leetMe answers GET /v1/leet/me.
func leetMe(h *uitest.Harness, me api.LeetMe) {
	if me.Recent == nil {
		me.Recent = []api.LeetSolve{}
	}
	if me.DailySpaces == nil {
		me.DailySpaces = []api.LeetDailySpace{}
	}
	h.API.JSON("GET /v1/leet/me", http.StatusOK, me)
}

// q1606 is today's fixture problem as the cache keeps it.
func q1606() *leetcode.Question {
	return &leetcode.Question{QuestionID: "1710", FrontendID: "1606", Slug: "find-servers-that-handled-most-number-of-requests",
		Title: "Find Servers That Handled Most Number of Requests", Difficulty: "Hard"}
}

// hubLine is the hub's line of the tool.
func hubLine(t *testing.T, h *uitest.Harness) string {
	t.Helper()
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, "1 🧩") {
			return l
		}
	}
	t.Fatalf("no leet hub row:\n%s", h.Screen())
	return ""
}

// lcRequests counts the fake LeetCode's requests whose body contains sub.
func lcRequests(e *env, sub string) int {
	n := 0
	for _, r := range e.lc.Requests() {
		if strings.Contains(r.Body, sub) || strings.Contains(r.Path, sub) {
			n++
		}
	}
	return n
}

// apiRequests are the masil requests with method and path prefix.
func apiRequests(h *uitest.Harness, method, path string) []string {
	var out []string
	for _, r := range h.API.Requests() {
		if r.Method == method && strings.HasPrefix(r.Path, path) {
			out = append(out, string(r.Body))
		}
	}
	return out
}

func TestHubStatusFromCache(t *testing.T) {
	e := setup(t)
	h := uitest.New(t)
	firstRunDone(t, h)
	leetMe(h, api.LeetMe{Current: 3, Best: 5})
	if err := leetcode.OpenCache(h.Context().Paths.Cache).PutDaily("2026-11-05", q1606()); err != nil {
		t.Fatal(err)
	}
	h.Press("ctrl+t")
	if l := hubLine(t, h); !strings.Contains(l, "1606 · Hard") || strings.Contains(l, "🔥") {
		t.Fatalf("before the streak is known: %q", l)
	}
	h.Press("1") // opening asks masil for my streak
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	if l := hubLine(t, h); !strings.Contains(l, "1606 · Hard · 아직 안 풂 · 🔥 3일") {
		t.Fatalf("hub row = %q", l)
	}
	if n := len(e.lc.Requests()); n != 0 {
		t.Fatalf("LeetCode requests = %d, want 0 (cache hit): %+v", n, e.lc.Requests())
	}
}

func TestHubStatusNotLoaded(t *testing.T) {
	e := setup(t)
	h := uitest.New(t)
	h.Press("ctrl+t")
	if l := hubLine(t, h); !strings.Contains(l, "오늘 문제를 아직 안 받았어요") {
		t.Fatalf("hub row = %q", l)
	}
	if n := len(e.lc.Requests()); n != 0 {
		t.Fatalf("the hub asked LeetCode: %d", n)
	}
}

// overlayText is the top overlay's inner text, lines joined, all
// whitespace removed (a wrapped no_truncate text reads whole in it).
func overlayText(t *testing.T, h *uitest.Harness) string {
	t.Helper()
	f := h.Model.Frame()
	if len(f.Overlays) == 0 {
		t.Fatalf("no overlay:\n%s", h.Screen())
	}
	o := f.Overlays[len(f.Overlays)-1]
	r := o.Rect
	if o.Bordered {
		r.X, r.Y, r.W, r.H = r.X+1, r.Y+1, r.W-2, r.H-2
	}
	lines := strings.Split(h.Screen(), "\n")
	var b strings.Builder
	for y := r.Y; y < r.Y+r.H && y < len(lines); y++ {
		b.WriteString(ansi.Cut(lines[y], r.X, r.X+r.W))
	}
	return compact(b.String())
}

// compact drops all whitespace.
func compact(s string) string { return strings.Join(strings.Fields(s), "") }

const firstRunNotice = "LeetCode 문제를 이 PC 가 직접 받아 와요. masil 은 LeetCode 와 관계없는 앱이에요."

func TestFirstRunShownOnce(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}, {W: 40, H: 14}} {
		t.Run(sizeName(sz), func(t *testing.T) {
			setup(t)
			h := uitest.New(t)
			h.Resize(sz.W, sz.H)
			leetMe(h, api.LeetMe{})
			h.Press("ctrl+t", "1")
			s := h.Screen()
			if !strings.Contains(s, "풀 언어") {
				t.Fatalf("no first-run notice:\n%s", s)
			}
			if !strings.Contains(overlayText(t, h), compact(firstRunNotice)) {
				t.Fatalf("notice cut:\n%s", s)
			}
			h.Check()
			h.Press("down", "enter") // Python3
			var st struct {
				Lang         string `json:"lang"`
				FirstRunDone bool   `json:"first_run_done"`
			}
			h.Context().ToolState.Section("leet", &st)
			if !st.FirstRunDone || st.Lang != "python3" {
				t.Fatalf("state = %+v", st)
			}
			h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
			h.Press("1")
			if s := h.Screen(); strings.Contains(s, "풀 언어") {
				t.Fatalf("notice shown again:\n%s", s)
			}
		})
	}
}

func sizeName(s uitest.Size) string {
	return strings.Join([]string{itoa(s.W), itoa(s.H)}, "x")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestDailyToggleOwnerOnly(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.API.JSON("PUT /v1/spaces/{space_id}/leet", http.StatusOK, map[string]any{"settings": map[string]any{
		"space_id": fixtures.SpaceNova, "enabled": true, "channel_id": "01JB0000000000000000000DLY", "enabled_by": fixtures.UserSam, "enabled_at": "2026-11-05T02:05:00Z"}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}})
	cmd, ok := palette.Lookup("leet.daily.toggle")
	if !ok {
		t.Fatal("leet.daily.toggle not registered")
	}
	h.Context().Store.Spaces[fixtures.SpaceNova].MyRole = api.RoleMember
	if cmd.Available(h.Context()) {
		t.Fatal("available to a plain member")
	}
	h.Send(ui.RunCommandMsg{ID: "leet.daily.toggle"})
	if got := apiRequests(h, "PUT", "/v1/spaces/"); len(got) != 0 {
		t.Fatalf("member sent %v", got)
	}
	h.Context().Store.Spaces[fixtures.SpaceNova].MyRole = api.RoleOwner
	if !cmd.Available(h.Context()) {
		t.Fatal("not available to the owner")
	}
	h.Send(ui.RunCommandMsg{ID: "leet.daily.toggle"})
	var got []string
	for _, r := range h.API.Requests() {
		if r.Method == "PUT" && r.Path == "/v1/spaces/"+fixtures.SpaceNova+"/leet" {
			got = append(got, string(r.Body))
		}
	}
	if len(got) != 1 || strings.TrimSpace(got[0]) != `{"enabled":true}` {
		t.Fatalf("PUT bodies = %q", got)
	}
}

func TestPaletteHiddenWithoutFlag(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "")
	h := uitest.New(t)
	for _, id := range []string{"leet.open", "leet.board", "leet.settings", "leet.daily.toggle"} {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Fatalf("%s not registered", id)
		}
		if c.Available(h.Context()) {
			t.Fatalf("%s available with the flag off", id)
		}
	}
}

func TestSettingsSaveLang(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.Send(ui.RunCommandMsg{ID: "leet.settings"})
	if s := h.Screen(); !strings.Contains(s, "오늘의 문제 설정") {
		t.Fatalf("no settings:\n%s", s)
	}
	h.Press("right") // Go -> Python3
	b, err := os.ReadFile(h.Context().Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "[tools.leet]") || !strings.Contains(string(b), `lang = "python3"`) {
		t.Fatalf("config.toml:\n%s", b)
	}
	if s := h.Screen(); !strings.Contains(s, "Python3") {
		t.Fatalf("settings do not show Python3:\n%s", s)
	}
	// The folder: typed, saved, shown with ~.
	h.Press("down", "enter", "ctrl+u")
	h.Type("~/lc")
	h.Press("enter")
	if b, _ = os.ReadFile(h.Context().Paths.Config); !strings.Contains(string(b), `dir = "~/lc"`) {
		t.Fatalf("config.toml:\n%s", b)
	}
	// Auto status: on.
	h.Press("down", "enter")
	if b, _ = os.ReadFile(h.Context().Paths.Config); !strings.Contains(string(b), `auto_status = true`) {
		t.Fatalf("config.toml:\n%s", b)
	}
}

func TestSettingsShareFriends(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.Context().Store.Settings.LeetShareWithFriends = true
	h.API.JSON("PATCH /v1/me/settings", http.StatusOK, map[string]any{"settings": map[string]any{"leet_share_with_friends": false, "dm_policy": "friends_and_spaces", "focus_default_min": 25}})
	h.Send(ui.RunCommandMsg{ID: "leet.settings"})
	h.Press("down", "down", "down", "enter")
	got := apiRequests(h, "PATCH", "/v1/me/settings")
	if len(got) != 1 || !strings.Contains(got[0], `"leet_share_with_friends":false`) {
		t.Fatalf("PATCH bodies = %q", got)
	}
}

func TestFirstRunAndSettingsGolden(t *testing.T) {
	setup(t)
	t.Run("firstrun", func(t *testing.T) {
		uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
			h.Send(ui.OpenOverlayMsg{ID: leet.OverlayFirstRun})
			h.Golden("leet_firstrun")
		})
	})
	t.Run("settings", func(t *testing.T) {
		uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
			h.Context().Store.Settings.LeetShareWithFriends = true
			h.Send(ui.OpenOverlayMsg{ID: leet.OverlaySettings})
			h.Golden("leet_settings")
		})
	})
}
