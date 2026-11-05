package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCallDefaultTimeoutIsNetworkError(t *testing.T) {
	release := make(chan struct{})
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer hs.Close()
	defer close(release)
	c := New(hs.URL, WithToken("msl_x"))
	_, err := c.call(context.Background(), request{Method: "GET", Path: "/v1/slow", Auth: true, Timeout: 50 * time.Millisecond}, nil)
	if CodeOf(err) != "network" {
		t.Fatalf("CodeOf(%v) = %q, want network (our own timeout, caller ctx alive)", err, CodeOf(err))
	}
	if strings.Contains(err.Error(), "msl_x") {
		t.Errorf("error leaks token: %v", err)
	}

	// A caller deadline keeps priority over r.Timeout and maps to "canceled".
	cctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.call(cctx, request{Method: "GET", Path: "/v1/slow", Auth: true, Timeout: time.Hour}, nil)
	if CodeOf(err) != "canceled" {
		t.Fatalf("caller deadline: CodeOf(%v) = %q, want canceled", err, CodeOf(err))
	}
}

func TestCallRawBodyQueryHeaderAnd204(t *testing.T) {
	var got struct {
		ct, cl, extra, auth, query string
		body                       []byte
	}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.ct = r.Header.Get("Content-Type")
		got.cl = r.Header.Get("Content-Length")
		got.extra = r.Header.Get("If-None-Match")
		got.auth = r.Header.Get("Authorization")
		got.query = r.URL.RawQuery
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(204)
	}))
	defer hs.Close()
	c := New(hs.URL, WithToken("msl_x"))
	var out struct{ X int }
	st, err := c.call(context.Background(), request{
		Method: "POST", Path: "/v1/uploads", Query: url.Values{"purpose": {"post"}},
		RawBody: strings.NewReader("\x01\x02\x03\x04"), ContentType: "image/png", ContentLength: 4,
		Auth: true, Header: http.Header{"If-None-Match": {"e1"}},
	}, &out)
	if err != nil || st != 204 {
		t.Fatalf("st=%d err=%v", st, err)
	}
	if got.ct != "image/png" || got.cl != "4" || got.extra != "e1" || got.auth != "Bearer msl_x" || got.query != "purpose=post" || string(got.body) != "\x01\x02\x03\x04" {
		t.Errorf("got %+v", got)
	}
}

func TestCallDecodeFailureIsInternalError(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer hs.Close()
	var out struct{}
	st, err := New(hs.URL).call(context.Background(), request{Method: "GET", Path: "/v1/meta"}, &out)
	if st != 200 || CodeOf(err) != "internal_error" {
		t.Fatalf("st=%d CodeOf=%q err=%v", st, CodeOf(err), err)
	}
}

func TestEscapePath(t *testing.T) {
	if got := escPath("v1", "users", "@minji", "posts"); got != "/v1/users/@minji/posts" {
		t.Errorf("escPath = %q", got)
	}
	if got := escPath("v1", "x", "a/b", "c d", "?"); got != "/v1/x/a%2Fb/c%20d/%3F" {
		t.Errorf("escPath = %q", got)
	}
}
