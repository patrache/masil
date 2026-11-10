package apitool

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/stub"

	_ "github.com/patrache/masil/internal/ui/tools" // the hub and the tool route
)

// Test helpers shared with P5's per-language goldens (locale_golden_test.go
// in this package reuses openAPI, seedFirstScreen and waitScreen).

func TestMain(m *testing.M) {
	stub.Install() // chat stand-ins under the tool surface
	os.Exit(m.Run())
}

// toolRoute is the API tool instance's route.
var toolRoute = ui.Route{Kind: ui.RouteTool, ID: instanceID, Arg: ui.ToolAPI}

// openAPI builds a harness with the api flag on and the API tool open.
func openAPI(t testing.TB, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "api")
	h := uitest.New(t, opts...)
	openIn(h)
	return h
}

// openIn opens (or shows) the API tool in h.
func openIn(h *uitest.Harness) {
	h.T.Helper()
	h.Send(ui.NavigateMsg{Route: toolRoute})
	if toolOf(h) == nil {
		h.T.Fatalf("the API tool did not open:\n%s", h.Screen())
	}
}

// toolOf is the open API tool instance (nil when none).
func toolOf(h *uitest.Harness) *Tool {
	tl, _ := h.Instance(toolRoute.Key()).(*Tool)
	return tl
}

// waitScreen advances the fake clock 50ms at a time (the job polls) with a
// real 2ms pause, until the screen shows want (400 rounds at most).
func waitScreen(t testing.TB, h *uitest.Harness, want string) {
	t.Helper()
	for i, start := 0, time.Now(); i < 400 || time.Since(start) < 10*time.Second; i++ {
		if strings.Contains(h.Screen(), want) {
			return
		}
		// the fake clock moves at most 20s (as before); after that only real time
		// passes, so slow machines (Windows CI) get up to 10s more for real I/O.
		if i < 400 {
			h.Advance(50 * time.Millisecond)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, h.Screen())
}

// githubCollection is the first-screen collection "깃허브 API".
func githubCollection() *apitool.Collection {
	user := &apitool.Request{
		ID: "get-user", Name: "내 정보", Method: "GET", URL: "{{base_url}}/user",
		Headers: []apitool.KV{
			{ID: "h1", Key: "Authorization", Value: "Bearer {{token}}"},
			{ID: "h2", Key: "Accept", Value: "application/vnd.github+json"},
			{ID: "h3", Key: "X-GitHub-Api-Version", Value: "2022-11-28"},
		},
		Asserts: []apitool.Assertion{
			{ID: "a1", Target: "status", Op: "eq", Value: "200"},
			{ID: "a2", Target: "time", Op: "lt", Value: "500ms"},
			{ID: "a3", Target: "json", Path: "$.login", Op: "exists"},
			{ID: "a4", Target: "header", Path: "Content-Type", Op: "contains", Value: "json"},
		},
	}
	repos := &apitool.Request{ID: "list-repos", Name: "저장소 목록", Method: "GET", URL: "{{base_url}}/user/repos",
		Query: []apitool.KV{{ID: "q1", Key: "per_page", Value: "20"}}}
	issue := &apitool.Request{ID: "create-issue", Name: "이슈 만들기", Method: "POST", URL: "{{base_url}}/repos/{{owner}}/masil/issues",
		Body: apitool.Body{Kind: apitool.BodyJSON, Text: `{"title": "hello"}`}}
	return &apitool.Collection{
		Version: apitool.CollectionVersion, ID: "github", Name: "깃허브 API",
		Root: apitool.Folder{Items: []apitool.Node{
			{Request: "get-user"},
			{Folder: &apitool.Folder{ID: "f-repos", Name: "저장소", Items: []apitool.Node{{Request: "list-repos"}, {Request: "create-issue"}}}},
		}},
		Requests: map[string]*apitool.Request{"get-user": user, "list-repos": repos, "create-issue": issue},
	}
}

// githubBody is the seeded last response's body.
const githubBody = `{"login":"sam","id":1024,"name":"Sam","company":{"name":"masil","location":"Seoul","size":3,"public":true},"repos":["masil","dotfiles","notes"],"plan":{"name":"free","private_repos":0}}`

// seedFirstScreen fills the first screen: the personal collection "깃허브
// API" with environment staging, the request 내 정보 selected, its last
// response (fixed timing, "company" and "repos" folded) and two history rows.
func seedFirstScreen(h *uitest.Harness) {
	h.T.Helper()
	ctx := h.Context()
	ls := apitool.OpenLocal(ctx.Paths)
	col := githubCollection()
	if err := ls.SaveCollection(col); err != nil {
		h.T.Fatalf("seed collection: %v", err)
	}
	env := &apitool.Env{Name: "staging", Vars: []apitool.Var{
		{Name: "base_url", Value: "https://api.github.com"},
		{Name: "owner", Value: "patrache"},
		{Name: "token", Secret: true, AllowedHosts: []string{"api.github.com"}},
	}}
	if err := ls.SaveEnv(env); err != nil {
		h.T.Fatalf("seed env: %v", err)
	}
	tl := toolOf(h)
	tl.reload(ctx)
	tl.selectEnv(ctx, "staging")
	tl.openFirst(col.Name, "get-user")
	tl.tree.follow(ctx, tl)
	tl.save(ctx)

	resp := &apitool.Response{
		Status: 200, StatusText: "OK", Proto: "HTTP/1.1", ContentType: "application/json; charset=utf-8",
		Header: http.Header{
			"Content-Type":          {"application/json; charset=utf-8"},
			"Cache-Control":         {"private, max-age=60"},
			"Etag":                  {`W/"5e1d"`},
			"X-Github-Media-Type":   {"github.v3"},
			"X-Ratelimit-Limit":     {"5000"},
			"X-Ratelimit-Remaining": {"4987"},
		},
		Body: []byte(githubBody), Size: int64(len(githubBody)), WireSize: int64(len(githubBody)),
		Timing: apitool.Timing{DNS: 3 * time.Millisecond, Connect: 21 * time.Millisecond, TLS: 38 * time.Millisecond, FirstByte: 71 * time.Millisecond, Total: 142 * time.Millisecond},
	}
	tl.showResponse(tl.respKey(), tl.req, resp)
	tl.rv.fold()["$.company"] = true
	tl.rv.fold()["$.repos"] = true

	for _, r := range []*apitool.Request{col.Requests["list-repos"], col.Requests["get-user"]} {
		p := &apitool.Prepared{Method: r.Method, URL: "https://api.github.com" + strings.TrimPrefix(r.URL, "{{base_url}}"), Header: http.Header{}}
		if err := tl.hist.Add(r, p, resp); err != nil {
			h.T.Fatalf("seed history: %v", err)
		}
	}
	h.Send(tl.loadHistory()())
}
