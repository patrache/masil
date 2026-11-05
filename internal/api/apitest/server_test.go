package apitest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

func get(t *testing.T, s *apitest.Server, method, path, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServerRoutes(t *testing.T) {
	s := apitest.New(t)
	if s.URL == "" || !strings.HasPrefix(s.URL, "http://") {
		t.Fatalf("URL = %q", s.URL)
	}
	s.Handle("GET /v1/channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ch":"` + r.PathValue("id") + `"}`))
	})
	s.JSON("GET /v1/me", 200, map[string]any{"user": map[string]any{"handle": "sam"}})
	s.JSON("POST /v1/raw", 201, json.RawMessage(`{"raw":true}`))
	s.JSON("DELETE /v1/me/status", 204, nil)
	s.Error("POST /v1/posts", 409, "conflict")

	if st, b := get(t, s, "GET", "/v1/channels/C1/messages", ""); st != 200 || b != `{"ch":"C1"}` {
		t.Errorf("Handle: %d %s", st, b)
	}
	if st, b := get(t, s, "GET", "/v1/me", ""); st != 200 || b != `{"user":{"handle":"sam"}}` {
		t.Errorf("JSON: %d %s", st, b)
	}
	if st, b := get(t, s, "POST", "/v1/raw", ""); st != 201 || b != `{"raw":true}` {
		t.Errorf("JSON raw: %d %s", st, b)
	}
	if st, b := get(t, s, "DELETE", "/v1/me/status", ""); st != 204 || b != "" {
		t.Errorf("JSON 204: %d %q", st, b)
	}
	st, b := get(t, s, "POST", "/v1/posts", "")
	var env struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(b), &env); err != nil || st != 409 || env.Error.Code != "conflict" || string(env.Error.Details) != "{}" {
		t.Errorf("Error: %d %s (%v)", st, b, err)
	}

	// Unregistered path and wrong method => 404 not_found envelope, still recorded.
	for _, tc := range []struct{ m, p string }{{"GET", "/v1/nope"}, {"POST", "/v1/me"}} {
		st, b := get(t, s, tc.m, tc.p, "")
		env.Error.Code = ""
		json.Unmarshal([]byte(b), &env)
		if st != 404 || env.Error.Code != "not_found" {
			t.Errorf("%s %s => %d %s, want 404 not_found", tc.m, tc.p, st, b)
		}
	}
	reqs := s.Requests()
	if len(reqs) != 7 {
		t.Fatalf("len(Requests) = %d, want 7", len(reqs))
	}
	if reqs[5].Method != "GET" || reqs[5].Path != "/v1/nope" || reqs[6].Method != "POST" || reqs[6].Path != "/v1/me" {
		t.Errorf("unmatched not recorded: %+v %+v", reqs[5], reqs[6])
	}
	// Requests returns a copy.
	reqs[0].Path = "mutated"
	if s.Requests()[0].Path == "mutated" {
		t.Error("Requests is not a copy")
	}
}

func TestServerRedactsTokens(t *testing.T) {
	s := apitest.New(t)
	s.Handle("POST /v1/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		// The handler still sees the real body.
		if !strings.Contains(string(b), "msl_Q2xh-dWRl_IGlz") {
			t.Errorf("handler body = %s", b)
		}
		w.WriteHeader(204)
	})
	get(t, s, "POST", "/v1/echo", `{"token":"msl_Q2xh-dWRl_IGlz","other":"msl_test"}`)
	rec := s.Requests()[0]
	if strings.Contains(string(rec.Body), "Q2xh") || strings.Contains(string(rec.Body), "msl_test") {
		t.Errorf("token not redacted: %s", rec.Body)
	}
	if string(rec.Body) != `{"token":"msl_…(redacted)","other":"msl_…(redacted)"}` {
		t.Errorf("recorded body = %s", rec.Body)
	}
}

func TestServerClient(t *testing.T) {
	s := apitest.New(t)
	var auth string
	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"user":{}}`))
	})
	c := s.Client()
	if c.Token() != "msl_test" || c.BaseURL() != s.URL {
		t.Fatalf("Client token=%q base=%q", c.Token(), c.BaseURL())
	}
	if _, err := c.GetMe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer msl_test" {
		t.Errorf("Authorization = %q", auth)
	}
	if s.Client(api.WithToken("msl_other")).Token() != "msl_other" {
		t.Error("Client options must apply after the default token")
	}
}

func TestServerReRegisterReplaces(t *testing.T) {
	s := apitest.New(t)
	s.JSON("GET /v1/me", 200, map[string]string{"v": "A"})
	s.JSON("GET /v1/me", 200, map[string]string{"v": "B"})
	if _, b := get(t, s, "GET", "/v1/me", ""); b != `{"v":"B"}` {
		t.Errorf("re-register: %s, want B", b)
	}

	h := func(tag string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tag)) }
	}
	s.Handle("GET /v1/channels/{id}/messages", h("h1"))
	s.Handle("GET /v1/channels/{cid}/messages", h("h2"))
	if _, b := get(t, s, "GET", "/v1/channels/C1/messages", ""); b != "h2" {
		t.Errorf("same shape, other wildcard name: %s, want h2", b)
	}

	s.Handle("GET /v1/channels/{id}/pins", h("p1"))
	s.Handle("GET /v1/channels/C9/pins", h("p2"))
	if _, b := get(t, s, "GET", "/v1/channels/C9/pins", ""); b != "p2" {
		t.Errorf("specific: %s, want p2", b)
	}
	if _, b := get(t, s, "GET", "/v1/channels/C1/pins", ""); b != "p1" {
		t.Errorf("wildcard: %s, want p1", b)
	}

	s.Handle("/", h("catch"))
	if _, b := get(t, s, "GET", "/v1/me", ""); b != `{"v":"B"}` {
		t.Errorf("specific over catch-all: %s", b)
	}
	if _, b := get(t, s, "GET", "/x", ""); b != "catch" {
		t.Errorf("catch-all: %s", b)
	}
	if _, b := get(t, s, "POST", "/v1/me", ""); b != "catch" {
		t.Errorf("catch-all for other method: %s", b)
	}

	s.Error("POST /v1/posts", 429, "rate_limited")
	s.JSON("POST /v1/posts", 201, map[string]string{"ok": "1"})
	if st, b := get(t, s, "POST", "/v1/posts", ""); st != 201 || b != `{"ok":"1"}` {
		t.Errorf("Error then JSON: %d %s", st, b)
	}
}

// fatalRecorder captures Fatalf instead of stopping the real test.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = format
	for _, a := range args {
		f.msg += "|" + a.(string)
	}
}

func TestServerConflictingPatternsFatal(t *testing.T) {
	fr := &fatalRecorder{TB: t}
	s := apitest.New(fr)
	s.Handle("GET /v1/posts/{id}", func(w http.ResponseWriter, r *http.Request) {})
	s.Handle("/v1/posts/x", func(w http.ResponseWriter, r *http.Request) {})
	if !strings.Contains(fr.msg, "conflicts with") || !strings.Contains(fr.msg, "/v1/posts/x") || !strings.Contains(fr.msg, "GET /v1/posts/{id}") {
		t.Fatalf("conflict not reported: %q", fr.msg)
	}
	// The server keeps working with the earlier route.
	if st, _ := get(t, s, "GET", "/v1/posts/P", ""); st != 200 {
		t.Errorf("after conflict: %d", st)
	}
}

func TestServerRecordsEscapedPathAndRawQuery(t *testing.T) {
	s := apitest.New(t)
	var tag string
	s.Handle("GET /v1/hashtags/{tag}/posts", func(w http.ResponseWriter, r *http.Request) {
		tag = r.PathValue("tag")
		w.Write([]byte(`{"items":[],"next_cursor":null}`))
	})
	s.Handle("PUT /v1/messages/{id}/reactions/{emoji}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	c := s.Client()
	if _, err := c.HashtagPosts(context.Background(), "첫눈", api.PageOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddReaction(context.Background(), "M", "👍"); err != nil {
		t.Fatal(err)
	}
	get(t, s, "GET", "/v1/hashtags/%EC%B2%AB%EB%88%88/posts?q=a+b&z=1&a=2", "")
	reqs := s.Requests()
	if tag != "첫눈" {
		t.Errorf("PathValue = %q", tag)
	}
	if reqs[0].Path != "/v1/hashtags/%EC%B2%AB%EB%88%88/posts" {
		t.Errorf("hashtag Path = %q", reqs[0].Path)
	}
	if reqs[1].Path != "/v1/messages/M/reactions/%F0%9F%91%8D" {
		t.Errorf("reaction Path = %q", reqs[1].Path)
	}
	if reqs[2].Query != "q=a+b&z=1&a=2" {
		t.Errorf("Query = %q, want raw as sent", reqs[2].Query)
	}
}
