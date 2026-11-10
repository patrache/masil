package e2e

// Cross-tool integration (P0 Task 31): the five real 0.1.3 tools, linked as
// cmd/masil links them (main_test.go), open together. P0's own tests run on
// fake tools; the tool packages' tests run one tool at a time.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/workspace"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/share/sharetest"
)

// toolsDate is the harness clock's UTC date (uitest.Start).
const toolsDate = "2026-11-05"

// leetRunID is the run id of leetcodetest's interpret_ok.json.
const leetRunID = "runcode_1762650000.123456_r1"

// paneLimitText is the start of termpane.pane_limit (ko).
const paneLimitText = "패널은 동시에 4개까지예요"

// toolsRig is an environment where every tool can open for real: all flags
// on, a terminal that takes panes, a fake LeetCode, and stand-in lazygit,
// nvim and login shell first on PATH.
type toolsRig struct {
	bin  string // the stand-in programs
	repo string // a folder lazygit opens
	lc   *leetcodetest.Server
}

// setupTools prepares the rig; call it before uitest.New.
func setupTools(t *testing.T) *toolsRig {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "all")
	t.Setenv("TERM", "xterm-256color") // the tools predict pane or not from the real environment
	t.Setenv("TMUX", "")
	t.Setenv("LG_CONFIG_FILE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{leetcode.EnvSession, leetcode.EnvCSRF, leetcode.EnvCFClearance, "EDITOR"} {
		t.Setenv(k, "")
	}
	r := &toolsRig{repo: t.TempDir()}
	// A short folder: termpanetest's fake pane draws the program path.
	bin, err := os.MkdirTemp("/tmp", "mt")
	if err != nil {
		t.Skipf("no short temp folder: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bin) })
	r.bin = bin
	cfg := t.TempDir()
	// Each stand-in stays alive reading its terminal until the pane hangs up.
	scripts := map[string]string{
		"lazygit": "#!/bin/sh\ncase \"$1\" in\n" +
			"--version) echo 'commit=abc, build date=2026-11-07T00:00:00Z, build source=homebrew, version=0.66.3, os=darwin, arch=arm64, git version=2.47.1' ;;\n" +
			"-cd) echo '" + cfg + "' ;;\n*) exec cat ;;\nesac\n",
		"nvim": "#!/bin/sh\nexec cat\n",
		"sh0":  "#!/bin/sh\nexec cat\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHELL", filepath.Join(bin, "sh0"))
	r.lc = leetcodetest.New(t)
	r.lc.SetDailyDate(toolsDate)
	t.Cleanup(leet.SetLeetcodeForTest(r.lc.URL))
	t.Cleanup(leet.SetImageTransportForTest(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("no network in tests") // never assets.leetcode.com
	}))
	return r
}

// leetAPI answers what today's problem asks the masil server.
func leetAPI(t *testing.T, h *uitest.Harness) {
	t.Helper()
	if err := h.Context().ToolState.SetSection(ui.ToolLeet, map[string]any{"first_run_done": true}); err != nil {
		t.Fatal(err)
	}
	h.API.JSON("GET /v1/leet/me", http.StatusOK, api.LeetMe{Current: 3, Best: 5, Recent: []api.LeetSolve{}, DailySpaces: []api.LeetDailySpace{}})
	h.API.JSON("PUT /v1/leet/daily/{date}", http.StatusOK, api.LeetDailyReport{
		Daily: api.LeetDaily{Date: toolsDate, LeetDailyFacts: api.LeetDailyFacts{FrontendID: "1606",
			Slug: "find-servers-that-handled-most-number-of-requests", Title: "x", Difficulty: "Hard", Tags: []string{}}},
		Roots: []api.LeetDailyRoot{},
	})
}

// Instance ids / routes of the tools the scenarios open.
var (
	leetRoute = ui.Route{Kind: ui.RouteTool, ID: "leet:today", Arg: ui.ToolLeet}
	hubRoute  = ui.Route{Kind: ui.RouteTools}
)

func gitRoute(repo string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: "git:" + repo, Arg: ui.ToolGit}
}

func workspaceRoute(id, dir string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: id, Arg: ui.ToolWorkspace, Query: map[string]string{"open": dir}}
}

// openFromHub opens the hub and presses the tool's number.
func openFromHub(h *uitest.Harness, digit string) {
	h.Send(ui.NavigateMsg{Route: hubRoute})
	h.Press(digit)
}

// openTools is the tool surface's open instances, by tool id.
func openTools(h *uitest.Harness) map[string][]ui.Route {
	out := map[string][]ui.Route{}
	if h.Context().OpenTools == nil {
		return out
	}
	for _, o := range h.Context().OpenTools() {
		out[o.Route.Arg] = append(out[o.Route.Arg], o.Route)
	}
	return out
}

// workspaceFolder is a small folder to open as a workspace.
func workspaceFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# proj\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// waitPanes waits until n pane processes are alive, delivering what they post.
func waitPanes(t *testing.T, h *uitest.Harness, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for termhost.LivePanes() != n {
		if time.Now().After(deadline) {
			t.Fatalf("live panes = %d (%v), want %d", termhost.LivePanes(), termhost.Running(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.Flush()
}

// TestPaneCapAcrossTools (JX13, A7.4): the cap of 4 live panes is one count
// for every tool. Real termpane.Launch (no termpanetest) with stand-in
// programs: nvim from today's problem, lazygit, and two workspace terminals
// fill it; the 5th pane is refused with the pane-limit toast whichever tool
// asks; closing one lets the next one open.
func TestPaneCapAcrossTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no embedded panes on Windows (termhost.Choose rule 1)")
	}
	if n := termhost.LivePanes(); n != 0 {
		t.Fatalf("%d panes alive before the test", n)
	}
	t.Cleanup(func() { termhost.CloseAll(2 * time.Second) })
	r := setupTools(t)
	h := uitest.New(t)
	h.Resize(200, 60)
	leetAPI(t, h)

	// 1: nvim in today's problem.
	openFromHub(h, "1")
	if h.Instance(leetRoute.Key()) == nil {
		t.Fatalf("today's problem did not open:\n%s", h.Screen())
	}
	h.Press("e")
	waitPanes(t, h, 1)
	if got := termhost.Running(); len(got) != 1 || got[0] != "nvim" {
		t.Fatalf("running = %v, want nvim", got)
	}

	// 2: lazygit.
	h.Send(ui.OpenTool(ui.ToolGit, gitRoute(r.repo).ID, nil)())
	waitPanes(t, h, 2)

	// 3, 4: two terminals in one workspace.
	h.Send(ui.NavigateMsg{Route: workspaceRoute("workspace:cap", workspaceFolder(t))})
	if h.Instance(workspaceRoute("workspace:cap", "").Key()) == nil {
		t.Fatalf("the workspace did not open:\n%s", h.Screen())
	}
	h.Press("ctrl+\\", "n", "enter")
	waitPanes(t, h, 3)
	h.Press("ctrl+\\", "n", "enter")
	waitPanes(t, h, 4)
	if got := termhost.Running(); len(got) != 4 {
		t.Fatalf("running = %v, want 4 programs", got)
	}

	// The 5th, from the workspace and from the git tool: the toast, no process.
	h.Press("ctrl+\\", "n", "enter")
	if s := h.Screen(); !strings.Contains(s, paneLimitText) {
		t.Fatalf("5th pane from the workspace: no pane-limit toast:\n%s", s)
	}
	h.Advance(5 * time.Second) // the toast goes
	repo2 := t.TempDir()
	h.Send(ui.OpenTool(ui.ToolGit, gitRoute(repo2).ID, nil)())
	if s := h.Screen(); !strings.Contains(s, paneLimitText) {
		t.Fatalf("5th pane from lazygit: no pane-limit toast:\n%s", s)
	}
	if n := termhost.LivePanes(); n != 4 {
		t.Fatalf("live panes = %d after two refusals, want 4", n)
	}
	h.Advance(5 * time.Second)

	// Close lazygit: its pane ends, and the workspace can open one more.
	h.Send(ui.CloseToolMsg{InstanceID: gitRoute(r.repo).ID})
	waitPanes(t, h, 3)
	h.Send(ui.NavigateMsg{Route: workspaceRoute("workspace:cap", "")})
	h.Press("ctrl+\\", "n", "enter")
	waitPanes(t, h, 4)
	if s := h.Screen(); strings.Contains(s, paneLimitText) {
		t.Fatalf("a pane was refused after one closed:\n%s", s)
	}
	h.Check()
}

// sentFrames records every frame a share connection sends.
type sentFrames struct {
	mu     sync.Mutex
	frames [][]byte
}

func (f *sentFrames) all() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.frames)
}

// recordingConn is a share.Conn that keeps a copy of what it sends.
type recordingConn struct {
	share.Conn
	rec *sentFrames
}

func (c recordingConn) Send(frame []byte) error {
	c.rec.mu.Lock()
	c.rec.frames = append(c.rec.frames, slices.Clone(frame))
	c.rec.mu.Unlock()
	return c.Conn.Send(frame)
}

// shareRelay makes the next workspace share connect to a sharetest relay
// (no members), answering the share REST calls on h's API; the frames the
// owner sends are recorded.
func shareRelay(t *testing.T, h *uitest.Harness) (*sharetest.Relay, *sentFrames) {
	t.Helper()
	const shareID = "ws_e2e"
	relay := sharetest.NewRelay(shareID, fixtures.UserSam, map[uint8]string{})
	relay.SetClock(h.Clock().Now)
	rec := &sentFrames{}
	dial := relay.Dial(fixtures.UserSam)
	t.Cleanup(workspace.SetDialForTest(func(ctx context.Context, id string) (share.Conn, error) {
		c, err := dial(ctx, id)
		if err != nil {
			return nil, err
		}
		return recordingConn{Conn: c, rec: rec}, nil
	}))
	h.API.JSON("POST /v1/workspace-shares", http.StatusCreated, map[string]any{"share": api.WorkspaceShare{
		ID: shareID, Title: "sam의 작업공간", Status: "live",
		Owner: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"}, Members: []api.WorkspaceShareMember{},
	}})
	h.API.JSON("DELETE /v1/workspace-shares/{id}", http.StatusNoContent, nil)
	return relay, rec
}

// The tool secrets the leak checks look for. Each is stored under the key
// its tool reads, and each scenario makes the tool really use it (useSecrets)
// before the checks: a value no tool loaded could not leak, and the check
// would pass whatever the tools did.
const (
	leetSession = "LEETCODE-COOKIE-e2e-5f1c"
	leetCSRF    = "LEETCODE-CSRF-e2e-77b0"
	apiToken    = "API-TOKEN-e2e-9a7d"
)

// The API tool's seeded collection, request and environment (C3.7 files).
const (
	apiCol = "e2e"
	apiReq = "ping"
	apiEnv = "e2e"
)

// apiTarget is the API the API tool calls: it records the Authorization
// header of each request.
type apiTarget struct {
	URL   string
	mu    sync.Mutex
	auths []string
}

func (a *apiTarget) got() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.auths)
}

// storeSecrets keeps today's problem's LeetCode cookies and the API tool's
// {{token}} in st under the keys the two tools read (leetcode.CredNS.Key,
// apitool.SecretKey of a personal environment), and seeds the API tool's
// files so that it opens on a request using {{token}} against a local API.
// Call it before the tools open. It returns the values.
func storeSecrets(t *testing.T, h *uitest.Harness, st *secrets.Store) (*apiTarget, []string) {
	t.Helper()
	ctx := h.Context()
	server, user, ok := ctx.SecretScope()
	if !ok {
		t.Fatal("no secret scope")
	}
	if err := leetcode.SaveCredentials(context.Background(), st, leetcode.CredNS{ServerHost: server, UserID: user},
		leetcode.Credentials{Session: secrets.New(leetSession), CSRF: secrets.New(leetCSRF)}); err != nil {
		t.Fatal(err)
	}

	target := &apiTarget{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target.mu.Lock()
		target.auths = append(target.auths, r.Header.Get("Authorization"))
		target.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	target.URL = srv.URL
	env := &apitool.Env{ID: "env-e2e", Name: apiEnv, Vars: []apitool.Var{
		{Name: "base_url", Value: srv.URL},
		{Name: "token", Secret: true, AllowedHosts: []string{"127.0.0.1"}},
	}}
	ls := apitool.OpenLocal(ctx.Paths)
	if err := ls.SaveEnv(env); err != nil {
		t.Fatal(err)
	}
	col := &apitool.Collection{Version: apitool.CollectionVersion, ID: "c-e2e", Name: apiCol,
		Root: apitool.Folder{Items: []apitool.Node{{Request: apiReq}}},
		Requests: map[string]*apitool.Request{apiReq: {ID: apiReq, Name: "ping", Method: "GET", URL: "{{base_url}}/ping",
			Headers: []apitool.KV{{ID: "h1", Key: "Authorization", Value: "Bearer {{token}}"}}}}}
	if err := ls.SaveCollection(col); err != nil {
		t.Fatal(err)
	}
	if err := ctx.ToolState.SetSection(ui.ToolAPI, map[string]any{"collection": apiCol, "request": apiReq, "env": apiEnv}); err != nil {
		t.Fatal(err)
	}
	key, ok := ctx.SecretKey(ui.ToolAPI, apitool.SecretKey("", env.ID, "token"))
	if !ok {
		t.Fatal("no secret scope")
	}
	if _, err := st.Put(context.Background(), key, secrets.New(apiToken)); err != nil {
		t.Fatal(err)
	}
	return target, []string{leetSession, leetCSRF, apiToken}
}

// useSecrets makes both tools use their stored secret, and fails t unless
// they did: r in today's problem sends the cookies to the fake LeetCode, and
// enter in the API tool sends {{token}} to the local API. It leaves the API
// tool in front.
func useSecrets(t *testing.T, h *uitest.Harness, r *toolsRig, target *apiTarget) {
	t.Helper()
	r.lc.QueueCheck(leetRunID, "check_run_success.json")
	h.Send(ui.NavigateMsg{Route: leetRoute})
	if h.Instance(leetRoute.Key()) == nil {
		openFromHub(h, "1")
	}
	h.Press("r")
	cookie := ""
	for _, rec := range r.lc.Requests() {
		if strings.HasSuffix(rec.Path, "/interpret_solution/") {
			cookie = rec.Header.Get("Cookie")
		}
	}
	if !strings.Contains(cookie, leetcode.NameSession+"="+leetSession) || !strings.Contains(cookie, leetcode.NameCSRF+"="+leetCSRF) {
		t.Fatalf("today's problem did not run with the stored cookies (cookie set: %t):\n%s", cookie != "", h.Screen())
	}
	h.Advance(time.Second) // the run's check

	openFromHub(h, "5")
	if len(openTools(h)[ui.ToolAPI]) != 1 {
		t.Fatalf("the API tool did not open:\n%s", h.Screen())
	}
	h.Press("enter")
	for i := 0; len(target.got()) == 0; i++ {
		if i == 400 {
			t.Fatalf("the API tool sent no request:\n%s", h.Screen())
		}
		h.Advance(50 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
	if got := target.got(); got[0] != "Bearer "+apiToken {
		t.Fatalf("the API tool did not resolve the stored {{token}} (Authorization %q)", strings.ReplaceAll(got[0], apiToken, "<token>"))
	}
	for range 10 { // the response lands
		h.Advance(50 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
}

// settleShare pumps the relay and the harness until the relay is quiet.
func settleShare(h *uitest.Harness, relay *sharetest.Relay) {
	quiet := 0
	for i := 0; i < 300 && quiet < 3; i++ {
		n := relay.Pump()
		h.Advance(33 * time.Millisecond)
		h.Flush()
		if n == 0 {
			quiet++
		} else {
			quiet = 0
		}
	}
}

// TestKillSwitchFromArchScreen (JX6, B8.5): while a workspace share is live,
// ctrl+\ x cuts it at once — no confirm — whatever is in front: the arch
// tool, the API tool, chat, or the hub. The tool secrets of this PC are in
// none of the frames the share sent nor in what masil sent its server (A9.5).
func TestKillSwitchFromArchScreen(t *testing.T) {
	for _, front := range []string{"arch", "api", "chat", "hub"} {
		t.Run(front, func(t *testing.T) {
			r := setupTools(t)
			termpanetest.Install(t)
			st := secrettest.NewStore(t, true)
			h := uitest.New(t, uitest.WithSecrets(st))
			h.Resize(200, 60)
			leetAPI(t, h)
			target, values := storeSecrets(t, h, st)
			relay, sent := shareRelay(t, h)
			ws := workspaceRoute("workspace:kill", workspaceFolder(t))
			h.Send(ui.NavigateMsg{Route: ws})
			h.Press("ctrl+\\", "S") // the share dialog
			h.Press("enter")        // start: no one invited yet
			settleShare(h, relay)
			if !strings.Contains(h.Screen(), "🔴 공유 중") {
				t.Fatalf("not sharing:\n%s", h.Screen())
			}
			t.Cleanup(func() { h.Send(ui.CloseToolMsg{InstanceID: ws.ID}) })
			useSecrets(t, h, r, target) // while the share is live

			switch front {
			case "arch":
				openFromHub(h, "3")
				if len(openTools(h)[ui.ToolArch]) != 1 {
					t.Fatalf("the arch tool did not open:\n%s", h.Screen())
				}
			case "api":
				openFromHub(h, "5")
				if len(openTools(h)[ui.ToolAPI]) != 1 {
					t.Fatalf("the API tool did not open:\n%s", h.Screen())
				}
			case "chat":
				h.Send(ui.NavigateMsg{Route: devRoute})
			case "hub":
				h.Send(ui.NavigateMsg{Route: hubRoute})
			}
			if !ui.KillSwitchActive() {
				t.Fatalf("%s in front: the kill switch is not active while sharing:\n%s", front, h.Screen())
			}
			h.Press("ctrl+\\", "x")
			if n := len(h.Model.Frame().Overlays); n != 0 {
				t.Fatalf("%s: ctrl+\\ x opened %d overlays (a confirm?):\n%s", front, n, h.Screen())
			}
			deadline := time.Now().Add(2 * time.Second)
			for !relay.Ended() {
				if time.Now().After(deadline) {
					t.Fatalf("%s: ctrl+\\ x did not end the share", front)
				}
				relay.Pump()
				time.Sleep(time.Millisecond)
			}
			settleShare(h, relay)
			if ui.KillSwitchActive() {
				t.Fatalf("%s: still sharing after ctrl+\\ x", front)
			}
			if len(sent.all()) == 0 {
				t.Fatal("the share sent no frames")
			}
			secrettest.AssertNoLeakFrames(t, sent.all(), values...)
			secrettest.AssertNoLeak(t, h.API, h.Context().Paths.LogDir, values...)
		})
	}
}

// fiveSizes is the walk of TestFiveToolsOpenTogether.
var fiveSizes = []uitest.Size{{W: 200, H: 60}, {W: 80, H: 24}, {W: 40, H: 14}, {W: 120, H: 40}}

// TestFiveToolsOpenTogether: all five tools open at once on one tool surface
// (the 5-instance limit, the hub's place kept); through 200x60 → 80x24 →
// 40x14 → 120x40 every one of them draws a whole frame, keeps its instance,
// and no stored tool secret reaches the masil server or the logs.
func TestFiveToolsOpenTogether(t *testing.T) {
	r := setupTools(t)
	fake := termpanetest.Install(t)
	st := secrettest.NewStore(t, true)
	h := uitest.New(t, uitest.WithSecrets(st))
	h.Resize(200, 60)
	leetAPI(t, h)
	target, values := storeSecrets(t, h, st)

	openFromHub(h, "1")
	h.Send(ui.NavigateMsg{Route: workspaceRoute("workspace:five", workspaceFolder(t))})
	openFromHub(h, "3")
	h.Send(ui.OpenTool(ui.ToolGit, gitRoute(r.repo).ID, nil)())
	openFromHub(h, "5")

	open := openTools(h)
	var routes []ui.Route
	for _, id := range []string{ui.ToolLeet, ui.ToolWorkspace, ui.ToolArch, ui.ToolGit, ui.ToolAPI} {
		if len(open[id]) != 1 {
			t.Fatalf("%s: %d instances open (%v), want 1:\n%s", id, len(open[id]), open, h.Screen())
		}
		routes = append(routes, open[id][0])
	}
	if got := fake.Started(); len(got) != 1 || !strings.HasSuffix(got[0].Program, "lazygit") {
		t.Fatalf("started = %+v, want lazygit only", got)
	}
	inst := map[string]ui.Component{}
	for _, rt := range routes {
		c := h.Instance(rt.Key())
		if c == nil {
			t.Fatalf("%s is not an instance", rt.Key())
		}
		inst[rt.Key()] = c
	}
	useSecrets(t, h, r, target)

	for _, sz := range fiveSizes {
		h.Resize(sz.W, sz.H)
		for _, rt := range routes {
			h.Send(ui.NavigateMsg{Route: rt})
			h.Check() // AssertIntegrity on the whole frame
			if h.Instance(rt.Key()) != inst[rt.Key()] {
				t.Fatalf("%dx%d: %s was rebuilt", sz.W, sz.H, rt.Key())
			}
		}
		h.Send(ui.NavigateMsg{Route: hubRoute})
		h.Check()
		if n := len(openTools(h)); n != 5 {
			t.Fatalf("%dx%d: %d tools open, want 5", sz.W, sz.H, n)
		}
	}
	if n := len(fake.Started()); n != 1 {
		t.Fatalf("a resize restarted a program: %d starts", n)
	}
	secrettest.AssertNoLeak(t, h.API, h.Context().Paths.LogDir, values...)
}

// releaseDefault runs t with MASIL_FEATURES unset: the release set is on.
func releaseDefault(t *testing.T) {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "") // restored at cleanup
	if err := os.Unsetenv("MASIL_FEATURES"); err != nil {
		t.Fatal(err)
	}
}

// TestReleaseEntriesGolden (JX9): the release's new entries, for the owner's
// review. The 0.1.2 help and palette goldens did not change when the release
// set went on (the tool rows sit at the end of the help, past its first
// page, and their queries show no tool command), so these goldens show them:
// the end of the help (G) with the tool rows, and the palette on "도구".
func TestReleaseEntriesGolden(t *testing.T) {
	releaseDefault(t)
	uitest.ForEachSize(t, append(append([]uitest.Size{}, uitest.Required...), uitest.Tiny), func(t *testing.T, h *uitest.Harness) {
		h.Press("?", "G")
		h.Check()
		h.Golden("help_tools")
		h.Press("esc")
		h.Press("ctrl+k")
		h.Type("도구")
		h.Check()
		h.Golden("palette_tools")
	})
}
