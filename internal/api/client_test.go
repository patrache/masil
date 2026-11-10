package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/version"
)

var ctx = context.Background()

func jsonEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got body is not JSON: %q (%v)", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %q (%v)", want, err)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("body = %s, want %s", gb, wb)
	}
}

func asError(t *testing.T, err error) *api.Error {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), want *api.Error", err, err)
	}
	return e
}

func TestRequestHeaders(t *testing.T) {
	s := apitest.New(t)
	var got http.Header
	s.Handle("PATCH /v1/me/profile", func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"user":{}}`))
	})
	if _, err := s.Client().UpdateProfile(ctx, api.UpdateProfileRequest{Bio: api.Some("hi")}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	for k, want := range map[string]string{
		"Authorization": "Bearer msl_test",
		"User-Agent":    version.UserAgent(),
		"Accept":        "application/json",
		"Content-Type":  "application/json; charset=utf-8",
	} {
		if g := got.Get(k); g != want {
			t.Errorf("%s = %q, want %q", k, g, want)
		}
	}
	reqs := s.Requests()
	if len(reqs) != 1 {
		t.Fatalf("len(Requests) = %d, want 1", len(reqs))
	}
	if reqs[0].Method != "PATCH" || reqs[0].Path != "/v1/me/profile" {
		t.Errorf("recorded %s %s", reqs[0].Method, reqs[0].Path)
	}
	jsonEqual(t, reqs[0].Body, `{"bio":"hi"}`)
}

func TestRequestHeadersCustomUserAgentAndNoBody(t *testing.T) {
	s := apitest.New(t)
	var got http.Header
	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"user":{}}`))
	})
	if _, err := s.Client(api.WithUserAgent("x/1")).GetMe(ctx); err != nil {
		t.Fatal(err)
	}
	if got.Get("User-Agent") != "x/1" {
		t.Errorf("User-Agent = %q", got.Get("User-Agent"))
	}
	if got.Get("Content-Type") != "" {
		t.Errorf("Content-Type on GET = %q, want none", got.Get("Content-Type"))
	}
	if len(s.Requests()[0].Body) != 0 {
		t.Errorf("GET body = %q", s.Requests()[0].Body)
	}
}

func TestErrorUserIDs(t *testing.T) {
	for _, tc := range []struct {
		details string
		want    []string
	}{
		{`{"user_ids":["01A","01B"]}`, []string{"01A", "01B"}},
		{`{}`, nil}, // a group DM block pair names nobody (api.md 3.1)
		{``, nil},
	} {
		e := &api.Error{Code: "dm_not_allowed"}
		if tc.details != "" {
			e.Details = json.RawMessage(tc.details)
		}
		if got := e.UserIDs(); !slices.Equal(got, tc.want) {
			t.Errorf("UserIDs(%s) = %v, want %v", tc.details, got, tc.want)
		}
	}
}

func TestErrorEnvelope(t *testing.T) {
	s := apitest.New(t)
	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "01REQ")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":"invalid_request","message":"body: too_long","details":{"fields":{"body":"too_long"}}}}`))
	})
	_, err := s.Client().GetMe(ctx)
	e := asError(t, err)
	if e.Status != 400 || e.Code != "invalid_request" || e.RequestID != "01REQ" || e.Message != "body: too_long" {
		t.Errorf("err = %+v", e)
	}
	if e.Fields()["body"] != "too_long" {
		t.Errorf("Fields = %v", e.Fields())
	}
	if !api.IsCode(err, "invalid_request") || api.CodeOf(err) != "invalid_request" {
		t.Errorf("CodeOf = %q", api.CodeOf(err))
	}
	if !strings.Contains(e.Error(), "invalid_request") || !strings.Contains(e.Error(), "01REQ") {
		t.Errorf("Error() = %q", e.Error())
	}

	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		w.Write([]byte("bad gateway"))
	})
	_, err = s.Client().GetMe(ctx)
	e = asError(t, err)
	if e.Status != 502 || e.Code != "internal_error" {
		t.Errorf("non-JSON 502 => %+v", e)
	}
	if e.Fields() != nil || e.Limiter() != "" || e.UserIDs() != nil {
		t.Errorf("Fields/Limiter/UserIDs on empty details = %v %q %v", e.Fields(), e.Limiter(), e.UserIDs())
	}

	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("<html>nope</html>"))
	})
	_, err = s.Client().GetMe(ctx)
	if e := asError(t, err); e.Status != 404 || e.Code != "invalid_request" {
		t.Errorf("non-JSON 404 => %+v", e)
	}
}

func TestCodeOfTransportAndCancel(t *testing.T) {
	hs := httptest.NewServer(http.NotFoundHandler())
	url := hs.URL
	hs.Close()
	_, err := api.New(url, api.WithToken("msl_x")).GetMe(ctx)
	if err == nil || api.CodeOf(err) != "network" {
		t.Errorf("closed server: CodeOf(%v) = %q, want network", err, api.CodeOf(err))
	}
	var e *api.Error
	if errors.As(err, &e) {
		t.Errorf("transport error should not be *api.Error: %v", err)
	}

	s := apitest.New(t)
	s.JSON("GET /v1/me", 200, map[string]any{"user": map[string]any{}})
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Client().GetMe(cctx)
	if api.CodeOf(err) != "canceled" {
		t.Errorf("canceled ctx: CodeOf(%v) = %q, want canceled", err, api.CodeOf(err))
	}
	if api.CodeOf(nil) != "" || api.CodeOf(errors.New("x")) != "" {
		t.Errorf("CodeOf(nil/plain) not empty")
	}
}

func TestUnauthorizedHookOncePerToken(t *testing.T) {
	s := apitest.New(t)
	s.Error("GET /v1/me", 401, "unauthorized")
	var n atomic.Int32
	c := s.Client(api.WithOnUnauthorized(func() { n.Add(1) }))
	for range 3 {
		_, err := c.GetMe(ctx)
		if !api.IsCode(err, "unauthorized") {
			t.Fatalf("err = %v", err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("hook called %d times for one token, want 1", n.Load())
	}
	c.SetToken("msl_new")
	if c.Token() != "msl_new" {
		t.Fatalf("Token = %q", c.Token())
	}
	c.GetMe(ctx)
	c.GetMe(ctx)
	if n.Load() != 2 {
		t.Fatalf("hook called %d times after SetToken, want 2", n.Load())
	}
}

func TestUnauthorizedHookOnlyForUnauthorizedCode(t *testing.T) {
	s := apitest.New(t)
	var n atomic.Int32
	c := s.Client(api.WithOnUnauthorized(func() { n.Add(1) }))

	s.Error("POST /v1/auth/password", 401, "invalid_credentials")
	err := c.ChangePassword(ctx, "a", "b")
	if e := asError(t, err); e.Code != "invalid_credentials" || e.Status != 401 {
		t.Errorf("ChangePassword err = %+v", e)
	}

	var authHeaders []string
	var mu sync.Mutex
	unauth := func(code string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			mu.Unlock()
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"code":"` + code + `","message":"x","details":{}}}`))
		}
	}
	s.Handle("POST /v1/auth/login", unauth("invalid_credentials"))
	_, err = c.Login(ctx, api.LoginRequest{Handle: "minji", Password: "pw"})
	if !api.IsCode(err, "invalid_credentials") {
		t.Errorf("Login err = %v", err)
	}
	s.Handle("POST /v1/auth/login", unauth("unauthorized"))
	s.Handle("POST /v1/auth/signup", unauth("unauthorized"))
	s.Handle("GET /v1/meta", unauth("unauthorized"))
	c.Login(ctx, api.LoginRequest{Handle: "minji", Password: "pw"})
	c.Signup(ctx, api.SignupRequest{Handle: "minji", Password: "pw"})
	c.Meta(ctx)
	for i, h := range authHeaders {
		if h != "" {
			t.Errorf("unauthenticated request %d sent Authorization %q", i, h)
		}
	}
	if len(authHeaders) != 4 {
		t.Errorf("unauthenticated requests seen = %d, want 4", len(authHeaders))
	}

	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("nope"))
	})
	_, err = c.GetMe(ctx)
	if e := asError(t, err); e.Code != "invalid_request" {
		t.Errorf("non-JSON 401 code = %q, want invalid_request", e.Code)
	}
	if n.Load() != 0 {
		t.Fatalf("hook called %d times, want 0", n.Load())
	}

	// No token at all: an authed call that gets 401 unauthorized does not fire the hook.
	s.Error("GET /v1/me", 401, "unauthorized")
	_, err = s.Client(api.WithToken(""), api.WithOnUnauthorized(func() { n.Add(1) })).GetMe(ctx)
	if !api.IsCode(err, "unauthorized") || n.Load() != 0 {
		t.Errorf("tokenless 401: err=%v hook=%d", err, n.Load())
	}
}

func TestUpgradeRequiredHook(t *testing.T) {
	s := apitest.New(t)
	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(426)
		w.Write([]byte(`{"error":{"code":"upgrade_required","message":"upgrade","details":{"min_client_version":"0.2.0"}}}`))
	})
	var got []string
	_, err := s.Client(api.WithOnUpgradeRequired(func(min string) { got = append(got, min) })).GetMe(ctx)
	if !api.IsCode(err, "upgrade_required") {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0] != "0.2.0" {
		t.Fatalf("hook got %v, want [0.2.0]", got)
	}
}

func TestRateLimitCooldown(t *testing.T) {
	T := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	var mu sync.Mutex
	now := T
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	set := func(d time.Duration) { mu.Lock(); now = T.Add(d); mu.Unlock() }

	s := apitest.New(t)
	s.Handle("POST /v1/posts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","details":{"limiter":"RL_POST","retry_after_sec":30}}}`))
	})
	s.JSON("PUT /v1/posts/{id}/like", 200, map[string]any{"post_id": "P", "liked": true, "like_count": 1})
	s.JSON("POST /v1/auth/login", 200, map[string]any{"token": "msl_zzz"})
	c := s.Client(api.WithClock(clock))

	if !c.Cooldown("RL_POST").IsZero() {
		t.Fatal("cooldown before any 429")
	}
	_, _, err := c.CreatePost(ctx, api.CreatePostRequest{ClientNonce: "n", Body: "x"})
	if e := asError(t, err); e.Status != 429 || e.Code != "rate_limited" || e.Limiter() != "RL_POST" || e.RetryAfter != 30*time.Second {
		t.Fatalf("first 429 = %+v", e)
	}
	if got := c.Cooldown("RL_POST"); !got.Equal(T.Add(30 * time.Second)) {
		t.Fatalf("Cooldown = %v, want T+30s", got)
	}

	set(10 * time.Second)
	_, _, err = c.CreatePost(ctx, api.CreatePostRequest{ClientNonce: "n2", Body: "y"})
	e := asError(t, err)
	if e.Status != 429 || e.Code != "rate_limited" || e.RetryAfter != 20*time.Second || e.Limiter() != "RL_POST" {
		t.Fatalf("local cooldown err = %+v", e)
	}
	if n := len(s.Requests()); n != 1 {
		t.Fatalf("requests = %d, want 1 (cooldown must not hit the network)", n)
	}
	if got := api.Explain(err).RetryAfterSec; got != 20 {
		t.Errorf("Explain RetryAfterSec = %d, want 20", got)
	}

	if _, err := c.Like(ctx, "P"); err != nil {
		t.Fatalf("Like during RL_POST cooldown: %v", err)
	}
	if n := len(s.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}

	// After the cooldown ends the limiter is free again.
	set(31 * time.Second)
	if !c.Cooldown("RL_POST").IsZero() {
		t.Errorf("Cooldown after expiry = %v, want zero", c.Cooldown("RL_POST"))
	}

	// RL_API cooldown blocks every authenticated call, but not unauthenticated ones.
	s.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","details":{"limiter":"RL_API","retry_after_sec":60}}}`))
	})
	c.GetMe(ctx)
	before := len(s.Requests())
	if _, err := c.Like(ctx, "P"); !api.IsCode(err, "rate_limited") {
		t.Errorf("Like during RL_API cooldown: %v", err)
	}
	if _, err := c.GetMe(ctx); !api.IsCode(err, "rate_limited") {
		t.Errorf("GetMe during RL_API cooldown: %v", err)
	}
	if len(s.Requests()) != before {
		t.Errorf("RL_API cooldown let %d requests out", len(s.Requests())-before)
	}
	if _, err := c.Login(ctx, api.LoginRequest{Handle: "a", Password: "b"}); err != nil {
		t.Errorf("unauthenticated Login during RL_API cooldown: %v", err)
	}
	if len(s.Requests()) != before+1 {
		t.Errorf("Login did not go out")
	}
}

func TestRateLimitRetryAfterHeaderFallback(t *testing.T) {
	T := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	s := apitest.New(t)
	s.Handle("PUT /v1/posts/{id}/like", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"code":"rate_limited","message":"x","details":{}}}`))
	})
	c := s.Client(api.WithClock(func() time.Time { return T }))
	_, err := c.Like(ctx, "P")
	if e := asError(t, err); e.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", e.RetryAfter)
	}
	// Without details.limiter the request's own limiter gets the cooldown.
	if got := c.Cooldown("RL_REACT"); !got.Equal(T.Add(7 * time.Second)) {
		t.Errorf("Cooldown(RL_REACT) = %v", got)
	}
}

func TestSendMessageRetriesOnceWithSameNonce(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		s := apitest.New(t)
		var n atomic.Int32
		s.Handle("POST /v1/channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
			if n.Add(1) == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				w.Write([]byte(`{"error":{"code":"rate_limited","message":"x","details":{"limiter":"RL_MESSAGE","retry_after_sec":1}}}`))
				return
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"message":{"id":"M1","channel_id":"C","body":"hi","client_nonce":"n1"}}`))
		})
		start := time.Now()
		msg, created, err := s.Client().SendMessage(ctx, "C", api.SendMessageRequest{ClientNonce: "n1", Body: "hi"})
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		if !created || msg == nil || msg.ID != "M1" {
			t.Fatalf("created=%v msg=%+v", created, msg)
		}
		if el := time.Since(start); el < 900*time.Millisecond {
			t.Errorf("retried after %v, want >= ~1s", el)
		}
		reqs := s.Requests()
		if len(reqs) != 2 {
			t.Fatalf("requests = %d, want 2", len(reqs))
		}
		for i, r := range reqs {
			if r.Path != "/v1/channels/C/messages" {
				t.Errorf("req %d path %q", i, r.Path)
			}
			var b struct {
				ClientNonce string `json:"client_nonce"`
			}
			json.Unmarshal(r.Body, &b)
			if b.ClientNonce != "n1" {
				t.Errorf("req %d client_nonce = %q, want n1", i, b.ClientNonce)
			}
		}
	})
	t.Run("duplicate nonce 200", func(t *testing.T) {
		s := apitest.New(t)
		s.JSON("POST /v1/channels/{id}/messages", 200, map[string]any{"message": map[string]any{"id": "M1"}})
		_, created, err := s.Client().SendMessage(ctx, "C", api.SendMessageRequest{ClientNonce: "n1", Body: "hi"})
		if err != nil || created {
			t.Fatalf("created=%v err=%v", created, err)
		}
	})
	t.Run("too long to wait", func(t *testing.T) {
		s := apitest.New(t)
		s.Handle("POST /v1/channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"x","details":{"limiter":"RL_MESSAGE","retry_after_sec":11}}}`))
		})
		_, _, err := s.Client().SendMessage(ctx, "C", api.SendMessageRequest{ClientNonce: "n1", Body: "hi"})
		if e := asError(t, err); e.Status != 429 {
			t.Fatalf("err = %+v", e)
		}
		if n := len(s.Requests()); n != 1 {
			t.Fatalf("requests = %d, want 1 (no retry above 10s)", n)
		}
	})
	t.Run("canceled during wait", func(t *testing.T) {
		s := apitest.New(t)
		s.Handle("POST /v1/channels/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"x","details":{"limiter":"RL_MESSAGE","retry_after_sec":5}}}`))
		})
		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(100*time.Millisecond, cancel)
		start := time.Now()
		_, _, err := s.Client().SendMessage(cctx, "C", api.SendMessageRequest{ClientNonce: "n1", Body: "hi"})
		if api.CodeOf(err) != "canceled" {
			t.Fatalf("CodeOf(%v) = %q, want canceled", err, api.CodeOf(err))
		}
		if el := time.Since(start); el > 3*time.Second {
			t.Errorf("cancel took %v", el)
		}
		if n := len(s.Requests()); n != 1 {
			t.Fatalf("requests = %d, want 1", n)
		}
	})
}

func TestOptMarshal(t *testing.T) {
	type X struct {
		A api.Opt[string] `json:"a,omitzero"`
		B api.Opt[int]    `json:"b,omitzero"`
	}
	for _, tc := range []struct {
		v    X
		want string
	}{
		{X{}, `{}`},
		{X{A: api.Some("x")}, `{"a":"x"}`},
		{X{A: api.Null[string]()}, `{"a":null}`},
		{X{B: api.Some(0)}, `{"b":0}`},
		{X{A: api.Some(""), B: api.Null[int]()}, `{"a":"","b":null}`},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.want {
			t.Errorf("Marshal(%+v) = %s, want %s", tc.v, b, tc.want)
		}
	}
	if !(api.Opt[int]{}).IsZero() || api.Some(0).IsZero() || api.Null[int]().IsZero() {
		t.Error("IsZero wrong")
	}
	if o := api.Some(3); !o.Set || o.Null || o.Value != 3 {
		t.Errorf("Some = %+v", o)
	}
	if o := api.Null[string](); !o.Set || !o.Null {
		t.Errorf("Null = %+v", o)
	}
}

func TestUserRefAndEscaping(t *testing.T) {
	if api.Handle("MinJi") != "@minji" {
		t.Errorf("Handle = %q", api.Handle("MinJi"))
	}
	if api.Handle("@MinJi") != "@minji" {
		t.Errorf("Handle(@MinJi) = %q", api.Handle("@MinJi"))
	}
	if api.UserID("01X") != "01X" {
		t.Errorf("UserID = %q", api.UserID("01X"))
	}
	if api.MeRef != "@me" {
		t.Errorf("MeRef = %q", api.MeRef)
	}

	s := apitest.New(t)
	s.Handle("PUT /v1/messages/{id}/reactions/{emoji}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("emoji") != "👍" {
			t.Errorf("emoji path value = %q", r.PathValue("emoji"))
		}
		w.WriteHeader(204)
	})
	s.JSON("GET /v1/hashtags/{tag}/posts", 200, map[string]any{"items": []any{}, "next_cursor": nil})
	c := s.Client()
	if err := c.AddReaction(ctx, "M", "👍"); err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
	if _, err := c.HashtagPosts(ctx, "첫눈", api.PageOpts{}); err != nil {
		t.Fatalf("HashtagPosts: %v", err)
	}
	if _, err := c.HashtagPosts(ctx, "#첫눈", api.PageOpts{Cursor: "c1", Limit: 10}); err != nil {
		t.Fatalf("HashtagPosts: %v", err)
	}
	reqs := s.Requests()
	if reqs[0].Path != "/v1/messages/M/reactions/%F0%9F%91%8D" {
		t.Errorf("reaction path = %q", reqs[0].Path)
	}
	if reqs[1].Path != "/v1/hashtags/%EC%B2%AB%EB%88%88/posts" || reqs[1].Query != "" {
		t.Errorf("hashtag path = %q query %q", reqs[1].Path, reqs[1].Query)
	}
	if reqs[2].Path != "/v1/hashtags/%EC%B2%AB%EB%88%88/posts" || reqs[2].Query != "cursor=c1&limit=10" {
		t.Errorf("hashtag (#) path = %q query %q", reqs[2].Path, reqs[2].Query)
	}
}

func TestURLs(t *testing.T) {
	c := api.New("https://h/")
	if c.BaseURL() != "https://h" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
	if got := c.MediaURL("/v1/media/01X"); got != "https://h/v1/media/01X" {
		t.Errorf("MediaURL = %q", got)
	}
	if got := c.MediaURL("https://other/v1/media/01X"); got != "https://other/v1/media/01X" {
		t.Errorf("MediaURL(abs) = %q", got)
	}
	if got := c.WebSocketURL("full", "D"); got != "wss://h/v1/ws?device_id=D&mode=full" {
		t.Errorf("WebSocketURL = %q", got)
	}
	if got := api.New("http://localhost:8787").WebSocketURL("alerts", "D 1"); got != "ws://localhost:8787/v1/ws?device_id=D+1&mode=alerts" {
		t.Errorf("WebSocketURL(http) = %q", got)
	}
	if got := api.New("https://h/base/").MediaURL("/v1/media/x"); got != "https://h/base/v1/media/x" {
		t.Errorf("MediaURL with base path = %q", got)
	}

	h := api.New("https://h", api.WithToken("msl_abc"), api.WithUserAgent("ua/1")).AuthHeader()
	if h.Get("Authorization") != "Bearer msl_abc" || h.Get("User-Agent") != "ua/1" {
		t.Errorf("AuthHeader = %v", h)
	}
	if h := api.New("https://h").AuthHeader(); h.Get("Authorization") != "" || h.Get("User-Agent") != version.UserAgent() {
		t.Errorf("tokenless AuthHeader = %v", h)
	}
	if api.New("https://h").Token() != "" {
		t.Error("default token not empty")
	}
}

func TestPageDone(t *testing.T) {
	cur := "c"
	if !(&api.Page[int]{}).Done() {
		t.Error("nil NextCursor should be Done")
	}
	if (&api.Page[int]{NextCursor: &cur}).Done() {
		t.Error("non-nil NextCursor should not be Done")
	}
	var p *api.Page[int]
	if !p.Done() {
		t.Error("nil page should be Done")
	}
}

func TestAcceptLanguageOnEveryRequest(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" => "+r.Header.Get("Accept-Language"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := api.New(srv.URL, api.WithToken("msl_test"))
	if got := c.AuthHeader().Get("Accept-Language"); got != "" {
		t.Fatalf("before SetLanguage: Accept-Language = %q, want none", got)
	}
	c.SetLanguage("ko")
	_, _ = c.GetMe(ctx)
	_, _ = c.UpdateProfile(ctx, api.UpdateProfileRequest{Bio: api.Some("hi")})
	_, _ = c.Login(ctx, api.LoginRequest{Handle: "sam", Password: "pw"})
	_ = c.Logout(ctx)
	mu.Lock()
	got := slices.Clone(seen)
	mu.Unlock()
	if len(got) < 4 {
		t.Fatalf("requests = %q, want at least 4", got)
	}
	for _, line := range got {
		if !strings.HasSuffix(line, "=> ko") {
			t.Errorf("request without Accept-Language ko: %s", line)
		}
	}
	if got := c.AuthHeader().Get("Accept-Language"); got != "ko" {
		t.Fatalf("AuthHeader Accept-Language = %q, want ko", got)
	}
	if got := c.AuthHeader().Get("Authorization"); got != "Bearer msl_test" {
		t.Fatalf("AuthHeader Authorization = %q", got)
	}
	c.SetLanguage("")
	if got := c.AuthHeader().Get("Accept-Language"); got != "" {
		t.Fatalf("SetLanguage(\"\"): Accept-Language = %q, want none", got)
	}
}
