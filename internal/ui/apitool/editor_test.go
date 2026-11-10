package apitool

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
)

var ko = i18n.KoT().Apitest

// hitServer counts requests and answers with h.
func hitServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// created answers 201 Created with a small JSON payment.
func created(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"id":"pay_1","amount":12000,"customer":{"id":"c1","name":"Kim","email":"k@example.com","tier":"gold"},"items":[1,2,3]}`))
}

// overlayText is the screen inside the top overlay's box ("" without one):
// a wrapped dialog text is matched without the screen behind it.
func overlayText(h *uitest.Harness) string {
	boxes := h.Model.Frame().Overlays
	if len(boxes) == 0 {
		return ""
	}
	r := boxes[len(boxes)-1].Rect
	lines := strings.Split(h.Screen(), "\n")
	var out []string
	for y := r.Y; y < r.Y+r.H && y < len(lines); y++ {
		out = append(out, ansi.Cut(lines[y], r.X, r.X+r.W))
	}
	return strings.Join(out, "\n")
}

// waitWhole is waitScreen for a text that may be wrapped (a dialog).
func waitWhole(t testing.TB, h *uitest.Harness, want string) {
	t.Helper()
	for range 400 {
		if hasWhole(h.Screen(), want) {
			return
		}
		h.Advance(50 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, h.Screen())
}

// seedRequests saves the personal collection "시험" with reqs (and env, when
// given, selected) and opens its first request.
func seedRequests(h *uitest.Harness, env *apitool.Env, reqs ...*apitool.Request) *Tool {
	h.T.Helper()
	ctx := h.Context()
	ls := apitool.OpenLocal(ctx.Paths)
	col := &apitool.Collection{Version: apitool.CollectionVersion, ID: "c-test", Name: "시험", Requests: map[string]*apitool.Request{}}
	for _, r := range reqs {
		col.Root.Items = append(col.Root.Items, apitool.Node{Request: r.ID})
		col.Requests[r.ID] = r
	}
	if err := ls.SaveCollection(col); err != nil {
		h.T.Fatalf("seed: %v", err)
	}
	tl := toolOf(h)
	if env != nil {
		if err := ls.SaveEnv(env); err != nil {
			h.T.Fatalf("seed env: %v", err)
		}
	}
	tl.reload(ctx)
	if env != nil {
		tl.selectEnv(ctx, env.Name)
	}
	tl.openFirst(col.Name, reqs[0].ID)
	tl.tree.follow(ctx, tl)
	return tl
}

// putSecret stores a secret variable's value the way the tool does.
func putSecret(t *testing.T, h *uitest.Harness, tl *Tool, name, value string) {
	t.Helper()
	key, _, ok := tl.scope(h.Context()).SecretSlot(name)
	if !ok {
		t.Fatalf("%s is not a secret variable", name)
	}
	if err := tl.vault(h.Context()).Put(key, secrets.New(value)); err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
}

func TestCurlPasteFillsRequest(t *testing.T) {
	h := openAPI(t)
	h.Press("n")
	h.Paste(`curl -X POST 'https://api.example.com/v1/items?limit=5' \
  -H 'X-Trace: abc' -H 'Content-Type: application/json' \
  -d '{"name":"pen"}'`)
	r := toolOf(h).req
	if r == nil || r.Method != "POST" || r.URL != "https://api.example.com/v1/items" {
		t.Fatalf("request = %+v", r)
	}
	if !slices.ContainsFunc(r.Query, func(kv apitool.KV) bool { return kv.Key == "limit" && kv.Value == "5" }) {
		t.Fatalf("query = %+v", r.Query)
	}
	if !slices.ContainsFunc(r.Headers, func(kv apitool.KV) bool { return kv.Key == "X-Trace" && kv.Value == "abc" }) {
		t.Fatalf("headers = %+v", r.Headers)
	}
	if r.Body.Kind != apitool.BodyJSON || !strings.Contains(r.Body.Text, `"pen"`) {
		t.Fatalf("body = %+v", r.Body)
	}
	if s := h.Screen(); !strings.Contains(s, "POST") || !strings.Contains(s, "api.example.com/v1/items") {
		t.Fatalf("screen:\n%s", s)
	}
}

// paymentRequest posts to srv with four checks that a created() answer passes.
func paymentRequest(srv *httptest.Server) *apitool.Request {
	return &apitool.Request{ID: "pay", Name: "결제 만들기", Method: "POST", URL: srv.URL + "/v1/payments",
		Body: apitool.Body{Kind: apitool.BodyJSON, Text: `{"amount":12000}`},
		Asserts: []apitool.Assertion{
			{ID: "a1", Target: "status", Op: "eq", Value: "201"},
			{ID: "a2", Target: "json", Path: "$.id", Op: "exists"},
			{ID: "a3", Target: "json", Path: "$.amount", Op: "eq", Value: "12000"},
			{ID: "a4", Target: "header", Path: "Content-Type", Op: "contains", Value: "json"},
		}}
}

func TestSendShowsStatusLine(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	seedRequests(h, nil, paymentRequest(srv))
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	if s := h.Screen(); !strings.Contains(s, "검사 4/4 ✓") {
		t.Fatalf("status line lacks the checks:\n%s", s)
	}
	if hits.Load() != 1 {
		t.Fatalf("server hits = %d", hits.Load())
	}
	h.Check()
}

const testJWT = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlLXZhbHVlLWhlcmU"

func TestPasteBearerOffersVariable(t *testing.T) {
	srv, _ := hitServer(t, created)
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithSecrets(store))
	tl := seedRequests(h, &apitool.Env{Name: "dev"}, paymentRequest(srv))
	h.Press("2", "j", "a")
	h.Type("Authorization")
	h.Press("tab")
	h.Paste("Bearer " + testJWT)
	if s := h.Screen(); !hasWhole(s, ko.SecretDetected()) {
		t.Fatalf("no secret_detected dialog:\n%s", s)
	}
	h.Press("ctrl+u")
	h.Type("token")
	h.Press("enter")
	if !slices.ContainsFunc(tl.req.Headers, func(kv apitool.KV) bool { return kv.Key == "Authorization" && kv.Value == "Bearer {{token}}" }) {
		t.Fatalf("headers = %+v", tl.req.Headers)
	}
	found := false
	for _, v := range secrettest.Values(store) {
		found = found || v == testJWT
	}
	if !found {
		t.Fatalf("the token is not in the secret store: %v", secrettest.Values(store))
	}
	env, err := apitool.OpenLocal(h.Context().Paths).LoadEnv("dev")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(env.Vars, func(v apitool.Var) bool { return v.Name == "token" })
	if i < 0 || !env.Vars[i].Secret || !slices.Equal(env.Vars[i].AllowedHosts, []string{"127.0.0.1"}) {
		t.Fatalf("env vars = %+v", env.Vars)
	}
	b, _ := os.ReadFile(filepath.Join(apitool.OpenLocal(h.Context().Paths).Dir, "envs", "dev.json"))
	if strings.Contains(string(b), testJWT) {
		t.Fatalf("the env file holds the token:\n%s", b)
	}
}

func TestUndefinedVarBlocksSend(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	r := paymentRequest(srv)
	r.Query = []apitool.KV{{ID: "q", Key: "x", Value: "{{nope}}"}}
	seedRequests(h, nil, r)
	h.Press("enter")
	waitScreen(t, h, ko.UndefinedVar("nope"))
	if hits.Load() != 0 {
		t.Fatalf("server hits = %d, want 0", hits.Load())
	}
}

// tokenSetup is a request sending {{token}} (allowed for api.example.com
// only) to srv through {{base_url}}.
func tokenSetup(t *testing.T, srv *httptest.Server) (*uitest.Harness, *Tool) {
	t.Helper()
	h := openAPI(t)
	env := &apitool.Env{Name: "dev", Vars: []apitool.Var{
		{Name: "base_url", Value: srv.URL},
		{Name: "token", Secret: true, AllowedHosts: []string{"api.example.com"}},
	}}
	r := &apitool.Request{ID: "me", Name: "me", Method: "GET", URL: "{{base_url}}/me",
		Headers: []apitool.KV{{ID: "h", Key: "Authorization", Value: "Bearer {{token}}"}}}
	tl := seedRequests(h, env, r)
	putSecret(t, h, tl, "token", "tok-123")
	return h, tl
}

func TestHostBlockPrompt(t *testing.T) {
	srv, hits := hitServer(t, created)
	h, tl := tokenSetup(t, srv)
	text := ko.HostBlock("token", "127.0.0.1")
	h.Press("enter")
	waitScreen(t, h, ko.HostAllow())
	for _, sz := range firstSizes {
		h.Resize(sz.W, sz.H)
		if s := overlayText(h); !hasWhole(s, text) {
			t.Fatalf("%dx%d: host_block cut:\n%s", sz.W, sz.H, h.Screen())
		}
	}
	h.Resize(120, 40)
	h.Press("down", "enter") // 취소
	h.Advance(time.Second)
	if hits.Load() != 0 {
		t.Fatalf("cancel: hits = %d", hits.Load())
	}
	h.Press("enter")
	waitScreen(t, h, ko.HostAllow())
	h.Press("enter") // 이 호스트 허용
	waitScreen(t, h, "201 Created")
	if hits.Load() != 1 {
		t.Fatalf("allow: hits = %d", hits.Load())
	}
	i := slices.IndexFunc(tl.env.Vars, func(v apitool.Var) bool { return v.Name == "token" })
	if !slices.Contains(tl.env.Vars[i].AllowedHosts, "127.0.0.1") {
		t.Fatalf("allowed hosts = %v", tl.env.Vars[i].AllowedHosts)
	}
}

func TestPrivateTargetPromptOnlyForeign(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	theirs := paymentRequest(srv)
	mine := paymentRequest(srv)
	mine.ID, mine.Name = "mine", "내 요청"
	tl := seedRequests(h, nil, theirs, mine)
	tl.foreign[theirs.ID] = foreignInfo{By: "hana", Seq: 1}
	question := ko.PrivateTarget("hana")
	h.Press("enter")
	waitScreen(t, h, ko.PrivateSend())
	if !hasWhole(overlayText(h), question) {
		t.Fatalf("private_target cut:\n%s", h.Screen())
	}
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	tl.rv.set(tl.respKey(), nil)
	h.Press("enter") // the same version: no question
	waitScreen(t, h, "201 Created")
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
	tl.selectRequest(tl.colName, mine.ID)
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	if hits.Load() != 3 || strings.Contains(h.Screen(), ko.PrivateSend()) {
		t.Fatalf("my request asked or did not send: hits=%d\n%s", hits.Load(), h.Screen())
	}
}

func TestTLSOffWarningAlwaysVisible(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "api")
	uitest.ForEachSize(t, firstSizes, func(t *testing.T, h *uitest.Harness) {
		openIn(h)
		r := &apitool.Request{ID: "x", Method: "GET", URL: "https://self-signed.example/x", Settings: apitool.Settings{Insecure: true}}
		seedRequests(h, nil, r)
		s := h.Screen()
		if toolOf(h).mode(h.Context()) == vmTiny {
			if !strings.Contains(s, "⚠") {
				t.Fatalf("tiny: no ⚠:\n%s", s)
			}
			return
		}
		if !hasWhole(s, ko.TlsOffWarning()) {
			t.Fatalf("no TLS warning:\n%s", s)
		}
		h.Press("]")
		if !hasWhole(h.Screen(), ko.TlsOffWarning()) && toolOf(h).mode(h.Context()) == vmTabs {
			t.Fatalf("response tab: no TLS warning:\n%s", h.Screen())
		}
	})
}

func TestInFlightDoesNotBlockUI(t *testing.T) {
	release := make(chan struct{})
	srv, _ := hitServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	})
	h := openAPI(t)
	seedRequests(h, nil, &apitool.Request{ID: "slow", Method: "GET", URL: srv.URL + "/slow"})
	h.Press("enter")
	if !strings.Contains(h.Screen(), ko.Sending()) {
		t.Fatalf("no sending state:\n%s", h.Screen())
	}
	h.Press("ctrl+t")
	if h.Nav().Front == app.FrontTools {
		t.Fatalf("ctrl+t did not go to chat while sending")
	}
	h.Press("ctrl+t")
	if h.Nav().Front != app.FrontTools {
		t.Fatalf("ctrl+t did not come back")
	}
	close(release)
	waitScreen(t, h, "200 OK")
}

func TestCancelSendWithEsc(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	srv, _ := hitServer(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	})
	h := openAPI(t)
	seedRequests(h, nil, &apitool.Request{ID: "stream", Method: "GET", URL: srv.URL + "/stream"})
	h.Press("enter")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatalf("the request never reached the server")
	}
	h.Advance(100 * time.Millisecond)
	h.Press("esc")
	waitScreen(t, h, ko.SendCanceled())
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatalf("the server connection stayed open")
	}
}

func TestSaveAndExternalChange(t *testing.T) {
	h := openAPI(t)
	h.Press("n")
	h.Type("https://example.com/a")
	h.Press("enter", "ctrl+s")
	name := ko.DefaultCollection()
	ls := apitool.OpenLocal(h.Context().Paths)
	c, err := ls.LoadCollection(name)
	if err != nil || len(c.Requests) != 1 {
		t.Fatalf("saved collection: %v %+v", err, c)
	}
	var id string
	for k, r := range c.Requests {
		id = k
		if r.URL != "https://example.com/a" {
			t.Fatalf("saved URL = %q", r.URL)
		}
	}
	c.Requests[id].URL = "https://example.com/changed"
	if err := ls.SaveCollection(c); err != nil {
		t.Fatal(err)
	}
	h.Press("ctrl+t", "ctrl+t") // away and back: the tool looks at its files
	waitScreen(t, h, ko.FileChanged())
	h.Press("R")
	if got := toolOf(h).req.URL; got != "https://example.com/changed" {
		t.Fatalf("after R: URL = %q", got)
	}
	if strings.Contains(h.Screen(), ko.FileChanged()) {
		t.Fatalf("the band stays after R")
	}
}

// The X-Api-Key value is a made-up key; it is split so secret scanners do not read it as a live key.
const postmanWithScripts = `{
 "info": {"name": "결제 예제", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
 "item": [
  {"name": "결제", "item": [
    {"name": "만들기", "event": [{"listen": "prerequest", "script": {"exec": ["pm.variables.set('a', 1)"]}}],
     "request": {"method": "POST", "url": "https://pay.example.com/v1/payments",
       "header": [{"key": "X-Api-Key", "value": "sk_` + "live_" + `9fQ2xLm8Rt4vNw7Yz3Kp1Hd6"}]}},
    {"name": "조회", "event": [{"listen": "test", "script": {"exec": ["pm.test('ok', () => {})"]}}],
     "request": {"method": "GET", "url": "https://pay.example.com/v1/payments/1"}}
  ]}
 ]
}`

func TestImportFileShowsReport(t *testing.T) {
	h := openAPI(t)
	dir := t.TempDir()
	pm := filepath.Join(dir, "pay.postman_collection.json")
	if err := os.WriteFile(pm, []byte(postmanWithScripts), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Press("i")
	h.Type(pm)
	h.Press("enter")
	waitWhole(t, h, ko.ScriptsDropped(2))
	s := h.Screen()
	if !hasWhole(s, ko.SecretDetected()) {
		t.Fatalf("report lacks the secret suggestion:\n%s", s)
	}
	h.Press("esc")
	if s := h.Screen(); !strings.Contains(s, "결제 예제") || !strings.Contains(s, "결제") {
		t.Fatalf("tree lacks the import:\n%s", s)
	}
	c, err := apitool.OpenLocal(h.Context().Paths).LoadCollection("결제 예제")
	if err != nil || len(c.Requests) != 2 {
		t.Fatalf("imported collection: %v %+v", err, c)
	}

	yml := filepath.Join(dir, "api.yaml")
	if err := os.WriteFile(yml, []byte("openapi: 3.0.0\ninfo:\n  title: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Press("i")
	h.Type(yml)
	h.Press("enter")
	waitWhole(t, h, ko.OpenapiYamlUnsupported())
}

func TestExportCurlCopies(t *testing.T) {
	srv, _ := hitServer(t, created)
	h, tl := tokenSetup(t, srv)
	h.Press("x")
	h.Press("enter") // curl, variables as they are
	clips := h.Clipboards()
	if len(clips) == 0 || !strings.Contains(clips[len(clips)-1], "{{token}}") || !strings.HasPrefix(clips[len(clips)-1], "curl ") {
		t.Fatalf("clipboard = %q", clips)
	}
	if strings.Contains(clips[len(clips)-1], "tok-123") {
		t.Fatalf("the secret value was copied")
	}
	out := filepath.Join(t.TempDir(), "out.postman.json")
	h.Press("x", "down", "down", "enter")
	h.Press("ctrl+u")
	h.Type(out)
	h.Press("enter")
	waitWhole(t, h, ko.ExportSaved(out))
	st, err := os.Stat(out)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("export file: %v %v", err, st)
	}
	_ = tl
}
