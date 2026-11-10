package apitool

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// cardFixture is a sent "Create payment" with base_url and a secret token.
func cardFixture() (*Request, *Prepared, *Response, []AssertResult) {
	r := &Request{ID: "r-pay", Name: "Create payment", Method: "POST", URL: "{{base_url}}/v1/payments?api_key={{key}}",
		Headers: []KV{{Key: "Authorization", Value: "Bearer {{token}}"}, {Key: "X-Trace", Value: "t-{{token}}"}}}
	p := &Prepared{RequestID: "r-pay", Method: "POST", URL: "https://api.example.com/v1/payments?api_key=KEY-SECRET-1",
		Header: http.Header{"Authorization": {"Bearer canary-Q9x7Z2"}, "X-Trace": {"t-canary-Q9x7Z2"}, "Content-Type": {"application/json"}},
		Used: []Value{
			{Name: "base_url", Text: "https://api.example.com", Source: SourceSharedEnv},
			{Name: "key", Text: "KEY-SECRET-1", Secret: true, Source: SourceMine},
			{Name: "token", Text: "canary-Q9x7Z2", Secret: true, Source: SourceMine},
		}}
	resp := &Response{Status: 201, Size: 98, Timing: Timing{Total: 142 * time.Millisecond},
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   []byte("{\n  \"id\": \"pay_1\",\n  \"echo\": \"canary-Q9x7Z2\"\n}")}
	as := []AssertResult{{ID: "a1", Pass: true}, {ID: "a2", Pass: true}, {ID: "a3", Pass: false}, {ID: "a4", Pass: true}}
	return r, p, resp, as
}

func decodeCard(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("card %s: %v", raw, err)
	}
	return m
}

func TestResultCardDefaultTemplateURL(t *testing.T) {
	r, p, resp, as := cardFixture()
	raw, err := ResultCard(r, p, resp, as, "staging", CardRef{Collection: "Payments", RequestID: "r-pay", DocID: "D1"}, CardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := decodeCard(t, raw)
	if m["url"] != "{{base_url}}/v1/payments?api_key={{key}}" || m["method"] != "POST" || m["status"] != 201.0 || m["time_ms"] != 142.0 || m["size"] != 98.0 {
		t.Fatalf("card = %s", raw)
	}
	if a := m["asserts"].(map[string]any); a["passed"] != 3.0 || a["total"] != 4.0 {
		t.Fatalf("asserts = %v", a)
	}
	if m["env"] != "staging" {
		t.Fatalf("env = %v", m["env"])
	}
	if ref := m["ref"].(map[string]any); ref["collection"] != "Payments" || ref["request_id"] != "r-pay" || ref["doc_id"] != "D1" {
		t.Fatalf("ref = %v", ref)
	}
	if _, ok := m["req_headers"]; ok {
		t.Fatal("headers are off by default")
	}
	if _, ok := m["body_head"]; ok {
		t.Fatal("the body is off by default")
	}
	if strings.Contains(string(raw), "canary") || strings.Contains(string(raw), "api.example.com") {
		t.Fatalf("default card leaks: %s", raw)
	}
	if got := ResultPath(r, p, CardOptions{}); got != "/v1/payments" {
		t.Fatalf("ResultPath = %q", got)
	}
}

func TestResultCardHostShownNonSecretOnly(t *testing.T) {
	r, p, resp, as := cardFixture()
	raw, err := ResultCard(r, p, resp, as, "", CardRef{Collection: "Payments", RequestID: "r-pay"}, CardOptions{ShowHost: true})
	if err != nil {
		t.Fatal(err)
	}
	m := decodeCard(t, raw)
	if m["url"] != "https://api.example.com/v1/payments?api_key={{key}}" {
		t.Fatalf("url = %v", m["url"])
	}
	if strings.Contains(string(raw), "KEY-SECRET-1") {
		t.Fatalf("secret in card: %s", raw)
	}
	if _, ok := m["env"]; ok {
		t.Fatal("no env name -> no env")
	}
	if ref := m["ref"].(map[string]any); ref["doc_id"] != nil {
		t.Fatalf("personal ref = %v", ref)
	}
}

func TestResultCardMasksHeaders(t *testing.T) {
	r, p, resp, as := cardFixture()
	raw, err := ResultCard(r, p, resp, as, "staging", CardRef{}, CardOptions{IncludeHeaders: true, IncludeBody: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "canary") {
		t.Fatalf("secret in card: %s", raw)
	}
	var c struct {
		ReqHeaders [][2]string `json:"req_headers"`
		BodyHead   []string    `json:"body_head"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, h := range c.ReqHeaders {
		got[h[0]] = h[1]
	}
	if got["Authorization"] != Mask || got["X-Trace"] != "t-"+Mask || got["Content-Type"] != "application/json" {
		t.Fatalf("headers = %v", c.ReqHeaders)
	}
	if len(c.BodyHead) != 4 || c.BodyHead[2] != `  "echo": "`+Mask+`"` {
		t.Fatalf("body_head = %q", c.BodyHead)
	}
}

func TestCardsUnder16KB(t *testing.T) {
	r, p, resp, as := cardFixture()
	var b strings.Builder
	for i := range 40 {
		b.WriteString(strings.Repeat("가\u0001", 100)) // 200 runes; the control char is \u0001 in JSON (6 bytes)
		if i < 39 {
			b.WriteString("\n")
		}
	}
	resp.Body = []byte(b.String())
	for i := range 30 {
		p.Header.Set("X-H"+itoa(i), strings.Repeat("v", 300))
	}
	raw, err := ResultCard(r, p, resp, as, "staging", CardRef{Collection: "Payments", RequestID: "r-pay"}, CardOptions{IncludeHeaders: true, IncludeBody: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > MaxCardBytes {
		t.Fatalf("card = %d bytes", len(raw))
	}
	var c struct {
		ReqHeaders [][2]string `json:"req_headers"`
		BodyHead   []string    `json:"body_head"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.ReqHeaders) > 20 || len(c.BodyHead) == 0 || len(c.BodyHead) >= 20 {
		t.Fatalf("headers %d, body lines %d", len(c.ReqHeaders), len(c.BodyHead))
	}
	for _, l := range c.BodyHead {
		if n := len([]rune(l)); n > 200 {
			t.Fatalf("a body line of %d runes", n)
		}
	}
}

func TestRunCardTop3Failures(t *testing.T) {
	s := RunSummary{Collection: "Payments", Requests: 6, Passed: 2, Failed: 4, Total: 1500 * time.Millisecond, Failures: []RunFailure{
		{Name: "a", Method: "GET", Status: 500, Reason: "status"},
		{Name: "b", Method: "POST", Reason: "network"},
		{Name: "c", Method: "PUT", Status: 404, Reason: "assert"},
		{Name: "d", Method: "DELETE", Status: 409, Reason: "assert"},
	}}
	raw, err := RunCard(s, "D1")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"collection":"Payments","requests":6,"passed":2,"failed":4,"time_ms":1500,"failures":[{"name":"a","method":"GET","status":500,"reason":"status"},{"name":"b","method":"POST","status":0,"reason":"network"},{"name":"c","method":"PUT","status":404,"reason":"assert"}],"ref":{"doc_id":"D1"}}`
	if string(raw) != want {
		t.Fatalf("run card =\n%s\nwant\n%s", raw, want)
	}
	raw, err = RunCard(RunSummary{Collection: "P", Requests: 1, Passed: 1}, "")
	if err != nil || string(raw) != `{"collection":"P","requests":1,"passed":1,"failed":0,"time_ms":0,"failures":[],"ref":{}}` {
		t.Fatalf("run card = %s, %v", raw, err)
	}
}

// The fallback body's path is the card URL's: the template by default, the
// non-secret variables filled in only with ShowHost (C3.11).
func TestResultPathFollowsCardURL(t *testing.T) {
	r := &Request{Method: "POST", URL: "{{base_url}}/tenants/{{tenant}}/v1/payments?x={{key}}"}
	p := &Prepared{Method: "POST", URL: "https://api.corp/acme-internal/tenants/t-123/v1/payments?x=KEY-SECRET-1",
		Used: []Value{
			{Name: "base_url", Text: "https://api.corp/acme-internal"},
			{Name: "tenant", Text: "t-123"},
			{Name: "key", Text: "KEY-SECRET-1", Secret: true},
		}}
	if got := ResultPath(r, p, CardOptions{}); got != "/tenants/{{tenant}}/v1/payments" {
		t.Fatalf("default path = %q", got)
	}
	if got := ResultPath(r, p, CardOptions{ShowHost: true}); got != "/acme-internal/tenants/t-123/v1/payments" {
		t.Fatalf("host path = %q", got)
	}
	for _, u := range []string{"https://h.example", "{{base_url}}", "h.example/a/b", "/plain"} {
		got := ResultPath(&Request{URL: u}, p, CardOptions{})
		want := map[string]string{"https://h.example": "/", "{{base_url}}": "/", "h.example/a/b": "/a/b", "/plain": "/plain"}[u]
		if got != want {
			t.Fatalf("ResultPath(%q) = %q, want %q", u, got, want)
		}
	}
}

// A typed-in API key sent under a header name that does not look secret is
// masked in the card's headers (as History masks it).
func TestResultCardMasksLiteralAuthKey(t *testing.T) {
	r, p, resp, as := cardFixture()
	r.Auth = Auth{Kind: AuthAPIKey, Name: "appid", In: "header", Value: "LITERAL-KEY-77"}
	p.Header.Set("appid", "LITERAL-KEY-77")
	resp.Body = []byte("echo LITERAL-KEY-77")
	raw, err := ResultCard(r, p, resp, as, "", CardRef{}, CardOptions{IncludeHeaders: true, IncludeBody: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "LITERAL-KEY-77") {
		t.Fatalf("the typed-in key is in the card: %s", raw)
	}
	if !strings.Contains(string(raw), `"Appid"`) {
		t.Fatalf("the header row is gone: %s", raw)
	}
}
