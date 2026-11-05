package realtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/version"
)

func TestCoderDialSendsAuthHeaders(t *testing.T) {
	type seen struct {
		auth, ua, mode, device string
	}
	seenc := make(chan seen, 1)
	big := strings.Repeat("x", 200_000) // > coder's 32 KiB default read limit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenc <- seen{r.Header.Get("Authorization"), r.Header.Get("User-Agent"), r.URL.Query().Get("mode"), r.URL.Query().Get("device_id")}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := context.Background()
		typ, b, err := c.Read(ctx)
		if err != nil || typ != websocket.MessageText {
			return
		}
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			return
		}
		if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"pong","pad":"`+big+`"}`)); err != nil {
			return
		}
		_, _, _ = c.Read(ctx) // until the client closes
	}))
	defer srv.Close()

	cl := api.New(srv.URL, api.WithToken("msl_test"))
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := coderDial(dctx, cl.WebSocketURL(string(ModeFull), "D1"), cl.AuthHeader())
	dcancel() // the socket must outlive the dial context
	if err != nil {
		t.Fatalf("coderDial: %v", err)
	}
	s := <-seenc
	if s.auth != "Bearer msl_test" || s.ua != version.UserAgent() || s.mode != "full" || s.device != "D1" {
		t.Errorf("server saw %+v", s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, pingFrame); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := conn.Read(ctx)
	if err != nil || string(got) != string(pingFrame) {
		t.Fatalf("Read = %q, %v", got, err)
	}
	got, err = conn.Read(ctx)
	if err != nil || len(got) < len(big) {
		t.Fatalf("large frame: len %d, %v", len(got), err)
	}
	if err := conn.Close(1000, "bye"); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestCoderDialCloseCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = c.Close(websocket.StatusCode(4001), "unauthorized")
	}))
	defer srv.Close()
	cl := api.New(srv.URL, api.WithToken("msl_test"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := coderDial(ctx, cl.WebSocketURL(string(ModeFull), "D1"), cl.AuthHeader())
	if err != nil {
		t.Fatalf("coderDial: %v", err)
	}
	_, err = conn.Read(ctx)
	if code := websocket.CloseStatus(err); code != 4001 {
		t.Fatalf("close status = %d (%v), want 4001", code, err)
	}
}

func TestCoderDialHTTPError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		header map[string]string
		want   api.Error
	}{
		{"401", 401, `{"error":{"code":"unauthorized","message":"session expired","details":{}}}`, nil,
			api.Error{Status: 401, Code: "unauthorized", Message: "session expired"}},
		{"403", 403, `{"error":{"code":"account_suspended","message":"suspended","details":{}}}`, nil,
			api.Error{Status: 403, Code: "account_suspended", Message: "suspended"}},
		{"429", 429, `{"error":{"code":"rate_limited","message":"slow down","details":{"limiter":"RL_WS","retry_after_sec":7}}}`, nil,
			api.Error{Status: 429, Code: "rate_limited", Message: "slow down", RetryAfter: 7 * time.Second}},
		{"429 header", 429, `nope`, map[string]string{"Retry-After": "3"},
			api.Error{Status: 429, Code: "invalid_request", RetryAfter: 3 * time.Second}},
		{"502 garbage", 502, `<html>bad gateway</html>`, nil,
			api.Error{Status: 502, Code: "internal_error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.Header().Set("X-Request-Id", "req-1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			cl := api.New(srv.URL, api.WithToken("msl_test"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := coderDial(ctx, cl.WebSocketURL(string(ModeFull), "D1"), cl.AuthHeader())
			if conn != nil {
				t.Fatalf("conn = %v, want nil", conn)
			}
			var e *api.Error
			if !errors.As(err, &e) {
				t.Fatalf("err = %v (%T), want *api.Error", err, err)
			}
			if e.Status != tc.want.Status || e.Code != tc.want.Code || e.RetryAfter != tc.want.RetryAfter || e.RequestID != "req-1" {
				t.Errorf("error = %+v, want %+v", e, tc.want)
			}
			if tc.want.Message != "" && e.Message != tc.want.Message {
				t.Errorf("message = %q", e.Message)
			}
			if tc.name == "429" && e.Limiter() != "RL_WS" {
				t.Errorf("limiter = %q", e.Limiter())
			}
		})
	}
}

func TestCoderDialNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	cl := api.New(url)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := coderDial(ctx, cl.WebSocketURL(string(ModeFull), "D1"), cl.AuthHeader())
	var e *api.Error
	if err == nil || errors.As(err, &e) {
		t.Fatalf("err = %v, want a transport error", err)
	}
}
