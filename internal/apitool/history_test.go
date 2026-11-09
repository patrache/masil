package apitool

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/secrets"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

func newClock() *testClock {
	return &testClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
}

func testHistory(t *testing.T, o HistoryOptions) (*History, string, *testClock) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "api", "history")
	c := newClock()
	return OpenHistory(dir, o, c.now), dir, c
}

func histFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func plainResp(body string) *Response {
	return &Response{Status: 200, StatusText: "OK", ContentType: "application/json",
		Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(body), Size: int64(len(body)),
		Timing: Timing{Total: 42 * time.Millisecond}}
}

func TestHistoryKeepsTemplateAndExpandedURL(t *testing.T) {
	h, _, _ := testHistory(t, DefaultHistoryOptions())
	r := &Request{ID: "r1", Method: "POST", URL: "{{base_url}}/v1/payments"}
	sc := &Scope{Env: &Env{ID: "e1", Vars: []Var{{Name: "base_url", Value: "https://api.test"}}}}
	p, err := Prepare(r, "col1", sc, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Add(r, p, plainResp(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	es, err := h.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 {
		t.Fatalf("List = %d entries", len(es))
	}
	e := es[0]
	if e.Kind != "request" || e.Request.URL != "{{base_url}}/v1/payments" || e.URL != "https://api.test/v1/payments" {
		t.Fatalf("entry = %+v", e)
	}
	if e.Status != 200 || e.Timing.Total != 42*time.Millisecond || e.Size != 8 || string(e.BodyHead) != `{"id":1}` {
		t.Fatalf("entry = %+v", e)
	}
	if !e.At.Equal(newClock().t) || e.ID == "" {
		t.Fatalf("At/ID = %v %q", e.At, e.ID)
	}
}

func TestHistoryMasksSecretHeadersAndValues(t *testing.T) {
	h, dir, _ := testHistory(t, DefaultHistoryOptions())
	sc := secretScope(t, "https://api.test", "api.test")
	r := &Request{ID: "r1", Method: "POST", URL: "{{base_url}}/v1",
		Headers: []KV{
			{Key: "Authorization", Value: "Bearer {{token}}"},
			{Key: "X-Literal-Token", Value: "lit-abc-123"},
			{Key: "X-Trace", Value: "t-{{key}}"},
		},
		Body: Body{Kind: BodyJSON, Text: `{"k":"{{key}}"}`},
		Auth: Auth{Kind: AuthBasic, User: "me", Pass: "hunter2"},
	}
	p, err := Prepare(r, "col1", sc, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	resp := plainResp(`{"echo":"sv-key","t":"sv-token"}`)
	resp.Header.Set("Set-Cookie", "sid=abc")
	resp.Header.Set("Authorization", "Bearer sv-token")
	resp.Header.Set("X-Echo", "value sv-key here")
	if err := h.Add(r, p, resp); err != nil {
		t.Fatal(err)
	}
	all := readAll(t, dir)
	for _, leak := range []string{"sv-token", "sv-key", "lit-abc-123", "hunter2", "sid=abc"} {
		if bytes.Contains(all, []byte(leak)) {
			t.Fatalf("history file contains %q:\n%s", leak, all)
		}
	}
	es, _ := h.List(1)
	e := es[0]
	if got := e.Header.Get("Set-Cookie"); got != Mask {
		t.Fatalf("Set-Cookie = %q", got)
	}
	if got := e.Header.Get("X-Echo"); got != "value "+Mask+" here" {
		t.Fatalf("X-Echo = %q", got)
	}
	if bytes.Contains(e.BodyHead, []byte("sv-")) || !bytes.Contains(e.BodyHead, []byte(Mask)) {
		t.Fatalf("BodyHead = %s", e.BodyHead)
	}
	hs := e.Request.Headers
	if hs[0].Value != "Bearer {{token}}" || hs[1].Value != Mask || hs[2].Value != "t-{{key}}" {
		t.Fatalf("request headers = %+v", hs)
	}
	if e.Request.Auth.Pass != Mask || e.Request.Auth.User != "me" {
		t.Fatalf("auth = %+v", e.Request.Auth)
	}
	if r.Headers[1].Value != "lit-abc-123" || r.Auth.Pass != "hunter2" {
		t.Fatal("Add changed the caller's request")
	}
}

func TestHistoryMasksSecretInExpandedURL(t *testing.T) {
	h, dir, _ := testHistory(t, DefaultHistoryOptions())
	sc := secretScope(t, "https://api.test", "api.test")
	sc.Env.Vars = append(sc.Env.Vars, Var{Name: "odd", Secret: true, AllowedHosts: []string{"api.test"}})
	if err := sc.Vault.Put(SecretKey("", "e1", "odd"), secrets.New("a b/c&d")); err != nil {
		t.Fatal(err)
	}
	r := &Request{ID: "r1", Method: "GET", URL: "{{base_url}}/v1/{{odd}}?api_key={{key}}",
		Query: []KV{{Key: "o", Value: "{{odd}}"}, {Key: "page_token", Value: "literal-cursor"}}}
	p, err := Prepare(r, "col1", sc, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Add(r, p, plainResp(`{}`)); err != nil {
		t.Fatal(err)
	}
	es, _ := h.List(1)
	u := es[0].URL
	if !strings.HasPrefix(u, "https://api.test/v1/") || !strings.Contains(u, "api_key="+Mask) || !strings.Contains(u, "page_token="+Mask) {
		t.Fatalf("URL = %q", u)
	}
	all := readAll(t, dir)
	for _, leak := range []string{"sv-key", "a b/c&d", "a+b%2Fc%26d", "a%20b", "literal-cursor"} {
		if bytes.Contains(all, []byte(leak)) {
			t.Fatalf("history contains %q:\n%s", leak, all)
		}
	}
	if es[0].Request.URL != r.URL || es[0].Request.Query[0].Value != "{{odd}}" || es[0].Request.Query[1].Value != Mask {
		t.Fatalf("template = %+v", es[0].Request)
	}
}

func TestHistoryBodyHead256KB(t *testing.T) {
	h, _, _ := testHistory(t, DefaultHistoryOptions())
	r := &Request{ID: "r1", Method: "GET", URL: "https://api.test/big"}
	p, err := Prepare(r, "c", &Scope{}, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Add(r, p, plainResp(strings.Repeat("x", 1<<20))); err != nil {
		t.Fatal(err)
	}
	es, _ := h.List(1)
	if len(es[0].BodyHead) != HistoryBodyMax || HistoryBodyMax != 262144 {
		t.Fatalf("BodyHead = %d", len(es[0].BodyHead))
	}
	if es[0].Size != 1<<20 {
		t.Fatalf("Size = %d", es[0].Size)
	}
}

func TestHistoryPrunes(t *testing.T) {
	h, dir, c := testHistory(t, HistoryOptions{Enabled: true, Max: 500, MaxAge: 30 * 24 * time.Hour})
	r := &Request{ID: "r1", Method: "GET", URL: "https://api.test/x"}
	p, err := Prepare(r, "c", &Scope{}, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for i := 0; i < 500; i++ {
		if err := h.Add(r, p, plainResp(`{}`)); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = histFiles(t, dir)[0]
		}
	}
	if n := len(histFiles(t, dir)); n != 500 {
		t.Fatalf("files = %d", n)
	}
	if err := h.Add(r, p, plainResp(`{}`)); err != nil {
		t.Fatal(err)
	}
	fs := histFiles(t, dir)
	if len(fs) != 500 {
		t.Fatalf("after 501st: files = %d", len(fs))
	}
	for _, f := range fs {
		if f == first {
			t.Fatal("oldest entry kept")
		}
	}

	// Entries older than MaxAge go on the next Add.
	c.t = c.t.Add(31 * 24 * time.Hour)
	if err := h.AddRun(RunSummary{Collection: "c", Requests: 1, Passed: 1}); err != nil {
		t.Fatal(err)
	}
	es, err := h.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 || es[0].Kind != "run" || es[0].Run == nil || es[0].Run.Passed != 1 {
		t.Fatalf("after age prune: %+v", es)
	}
}

func TestHistoryListNewestFirst(t *testing.T) {
	h, _, c := testHistory(t, DefaultHistoryOptions())
	for i := 0; i < 3; i++ {
		if err := h.AddRun(RunSummary{Collection: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(time.Second)
	}
	es, err := h.List(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Run.Collection != "c" || es[1].Run.Collection != "b" {
		t.Fatalf("List(2) = %+v", es)
	}
}

func TestHistoryDisabledWritesNothing(t *testing.T) {
	h, dir, _ := testHistory(t, HistoryOptions{Enabled: false})
	r := &Request{ID: "r1", Method: "GET", URL: "https://api.test/x"}
	p, _ := Prepare(r, "c", &Scope{}, fixedDyn())
	if err := h.Add(r, p, plainResp(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := h.AddRun(RunSummary{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("history dir exists: %v", err)
	}
	if es, err := h.List(0); err != nil || len(es) != 0 {
		t.Fatalf("List = %v %v", es, err)
	}
}

func TestHistoryClear(t *testing.T) {
	h, dir, _ := testHistory(t, DefaultHistoryOptions())
	for i := 0; i < 3; i++ {
		if err := h.AddRun(RunSummary{Collection: "c"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}
	if fs := histFiles(t, dir); len(fs) != 0 {
		t.Fatalf("files after Clear = %v", fs)
	}
	if es, _ := h.List(0); len(es) != 0 {
		t.Fatalf("List after Clear = %d", len(es))
	}
}

func TestHistoryFilePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	h, dir, _ := testHistory(t, DefaultHistoryOptions())
	if err := h.AddRun(RunSummary{Collection: "c"}); err != nil {
		t.Fatal(err)
	}
	fs := histFiles(t, dir)
	if len(fs) != 1 || !strings.HasSuffix(fs[0], ".json") || len(strings.TrimSuffix(fs[0], ".json")) != 26 {
		t.Fatalf("files = %v", fs)
	}
	st, err := os.Stat(filepath.Join(dir, fs[0]))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode())
	}
	dst, _ := os.Stat(dir)
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", dst.Mode())
	}
}

func TestRedactRequestTemplate(t *testing.T) {
	r := &Request{URL: "https://x/a?token=lit&q={{q}}&key={{k}}",
		Headers: []KV{{Key: "Authorization", Value: "Basic dXNlcjpw"}, {Key: "Cookie", Value: "{{c}}"}, {Key: "Accept", Value: "sv-1"}},
		Auth:    Auth{Kind: AuthAPIKey, Name: "X-Key", Value: "{{k}}", Token: "abc"},
		Body:    Body{Kind: BodyMultipart, Form: []FormField{{KV: KV{Key: "password", Value: "pw"}}, {KV: KV{Key: "n", Value: "sv-1"}}}},
	}
	got := RedactRequest(r, []string{"sv-1"})
	if got.URL != "https://x/a?token="+Mask+"&q={{q}}&key={{k}}" {
		t.Fatalf("URL = %q", got.URL)
	}
	if got.Headers[0].Value != Mask || got.Headers[1].Value != "{{c}}" || got.Headers[2].Value != Mask {
		t.Fatalf("headers = %+v", got.Headers)
	}
	if got.Auth.Value != "{{k}}" || got.Auth.Token != Mask {
		t.Fatalf("auth = %+v", got.Auth)
	}
	if got.Body.Form[0].Value != Mask || got.Body.Form[1].Value != Mask {
		t.Fatalf("form = %+v", got.Body.Form)
	}
	if r.Headers[0].Value != "Basic dXNlcjpw" || r.Body.Form[0].Value != "pw" {
		t.Fatal("RedactRequest changed its input")
	}
}

// A secret that shrinks to Mask several times before the cut must not leave
// its unreplaced start inside the head (review: P4 history).
func TestRedactHeadSecretOnCutAfterShrink(t *testing.T) {
	secret := strings.Repeat("S", 80) + "TOPSECRETTAIL" + strings.Repeat("Q", 7) // 100 bytes
	n := 1000
	var b strings.Builder
	for range 8 {
		b.WriteString(secret + "..")
	}
	for b.Len() < n+250 { // the 9th copy straddles n+3*len
		b.WriteByte('x')
	}
	body := []byte(b.String() + secret + strings.Repeat("y", 400))
	head := redactHead(body, []string{secret}, n, true)
	if bytes.Contains(head, []byte("SSSSSSSSSS")) || bytes.Contains(head, []byte("TOPSECRET")) {
		t.Fatalf("head keeps part of the secret: %q", head[max(len(head)-200, 0):])
	}
	if len(head) > n {
		t.Fatalf("head %d > %d", len(head), n)
	}
}

// An API key sent in the query under a name that does not look secret is
// masked in the expanded URL like it is in Request.Auth (review: P4 history).
func TestHistoryMasksAPIKeyQueryInURL(t *testing.T) {
	h, dir, _ := testHistory(t, DefaultHistoryOptions())
	key := "9f8e7d6c5b4a39281706f5e4d3c2b1a0"
	r := &Request{ID: "r1", Method: "GET", URL: "https://api.test/weather?q=Seoul",
		Auth: Auth{Kind: AuthAPIKey, Name: "appid", Value: key, In: "query"}}
	p, err := Prepare(r, "c", &Scope{}, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.URL, key) {
		t.Fatalf("prepared URL = %q", p.URL)
	}
	if err := h.Add(r, p, plainResp(`{}`)); err != nil {
		t.Fatal(err)
	}
	if all := readAll(t, dir); bytes.Contains(all, []byte(key)) {
		t.Fatalf("history contains the api key:\n%s", all)
	}
	es, _ := h.List(1)
	if !strings.Contains(es[0].URL, "appid="+Mask) || !strings.Contains(es[0].URL, "q=Seoul") {
		t.Fatalf("URL = %q", es[0].URL)
	}
}
