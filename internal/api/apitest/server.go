// Package apitest is a fake masil HTTP server for tests (CA 4.3.3).
//
// Routing (plan 03 Task 8): routes live in the server's own table keyed by
// method + path shape with wildcard names erased ("GET /v1/channels/{}/messages").
// Registering the same shape again REPLACES that route's handler (later wins);
// different shapes are kept and matched with Go 1.22 ServeMux rules (the more
// specific pattern wins). Two different shapes that overlap with neither more
// specific (a ServeMux conflict) are reported with tb.Fatalf at registration.
// Unmatched requests get 404 {"error":{"code":"not_found",...}}.
//
// Recording: every request (matched or not) is recorded with Method, Path =
// r.URL.EscapedPath() (escaped, not decoded), Query = r.URL.RawQuery (as sent),
// and Body with msl_ tokens redacted.
package apitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/api"
)

// Server is an httptest server with canned routes and a request log.
type Server struct {
	URL string

	tb     testing.TB
	srv    *httptest.Server
	mu     sync.Mutex
	routes []route
	mux    *http.ServeMux
	log    []Recorded
}

type route struct {
	key, pattern string
	handler      http.Handler
}

// Recorded is one request seen by the server (token redacted).
type Recorded struct {
	Method, Path, Query string
	Body                []byte
	Header              http.Header // as sent (masil tokens redacted); leak checks read it
}

var tokenRE = regexp.MustCompile(`msl_[A-Za-z0-9_-]+`)

const redacted = "msl_…(redacted)"

// New starts a server, closed on tb.Cleanup.
func New(tb testing.TB) *Server {
	tb.Helper()
	s := &Server{tb: tb, mux: http.NewServeMux()}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.URL = s.srv.URL
	tb.Cleanup(s.srv.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	s.mu.Lock()
	s.log = append(s.log, Recorded{
		Method: r.Method,
		Path:   r.URL.EscapedPath(),
		Query:  r.URL.RawQuery,
		Body:   tokenRE.ReplaceAll(body, []byte(redacted)),
		Header: redactHeader(r.Header),
	})
	mux := s.mux
	s.mu.Unlock()

	if _, pattern := mux.Handler(r); pattern == "" {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("apitest: no route for %s %s", r.Method, r.URL.EscapedPath()))
		return
	}
	mux.ServeHTTP(w, r)
}

// redactHeader is a copy of h with masil tokens redacted.
func redactHeader(h http.Header) http.Header {
	out := h.Clone()
	for k, vs := range out {
		for i, v := range vs {
			out[k][i] = tokenRE.ReplaceAllString(v, redacted)
		}
	}
	return out
}

// shapeKey erases wildcard names: "GET /a/{id}/b/{rest...}" => "GET /a/{}/b/{...}".
func shapeKey(pattern string) string {
	method, path := "", pattern
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		method, path = pattern[:i], strings.TrimLeft(pattern[i:], " \t")
	}
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		if !strings.HasPrefix(seg, "{") || !strings.HasSuffix(seg, "}") || seg == "{$}" {
			continue
		}
		if strings.HasSuffix(seg, "...}") {
			segs[i] = "{...}"
		} else {
			segs[i] = "{}"
		}
	}
	return method + " " + strings.Join(segs, "/")
}

// conflicts reports whether ServeMux refuses to hold both patterns.
func conflicts(a, b string) (bad bool) {
	defer func() {
		if recover() != nil {
			bad = true
		}
	}()
	m := http.NewServeMux()
	nop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	m.Handle(a, nop)
	m.Handle(b, nop)
	return false
}

// add inserts or replaces a route and swaps in a freshly built mux.
func (s *Server) add(pattern string, h http.Handler) {
	s.tb.Helper()
	key := shapeKey(pattern)
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, rt := range s.routes {
		if rt.key == key {
			idx = i
			continue
		}
		if conflicts(rt.pattern, pattern) {
			s.tb.Fatalf("apitest: pattern %q conflicts with %q", pattern, rt.pattern)
			return
		}
	}
	if idx >= 0 {
		s.routes[idx] = route{key: key, pattern: pattern, handler: h}
	} else {
		s.routes = append(s.routes, route{key: key, pattern: pattern, handler: h})
	}
	mux := http.NewServeMux()
	for _, rt := range s.routes {
		mux.Handle(rt.pattern, rt.handler)
	}
	s.mux = mux
}

// Handle registers a Go 1.22 mux pattern, e.g. "GET /v1/channels/{id}/messages".
func (s *Server) Handle(pattern string, h http.HandlerFunc) {
	s.tb.Helper()
	s.add(pattern, h)
}

// JSON registers a canned JSON response. body is marshaled with encoding/json,
// except []byte and json.RawMessage which are written as-is; 204 writes no body.
func (s *Server) JSON(pattern string, status int, body any) {
	s.tb.Helper()
	var raw []byte
	switch b := body.(type) {
	case json.RawMessage:
		raw = b
	case []byte:
		raw = b
	default:
		if status != http.StatusNoContent {
			var err error
			if raw, err = json.Marshal(body); err != nil {
				s.tb.Fatalf("apitest: JSON(%q): %v", pattern, err)
				return
			}
		}
	}
	s.add(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		w.Write(raw)
	}))
}

// Error registers an error envelope response ({"error":{"code","message","details":{}}}).
func (s *Server) Error(pattern string, status int, code string) {
	s.tb.Helper()
	s.add(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, status, code, code)
	}))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": message, "details": map[string]any{}}})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(b)
}

// Requests returns every recorded request: method, path, query, body, header (token redacted).
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recorded, len(s.log))
	for i, r := range s.log {
		r.Body = bytes.Clone(r.Body)
		r.Header = r.Header.Clone()
		out[i] = r
	}
	return out
}

// Client returns an api.Client for this server with token "msl_test".
func (s *Server) Client(opts ...api.Option) *api.Client {
	return api.New(s.URL, append([]api.Option{api.WithToken("msl_test")}, opts...)...)
}
