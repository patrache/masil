// Package apitest is a fake masil HTTP server for tests (CA 4.3.3).
package apitest

import (
	"net/http"
	"testing"

	"github.com/patrache/masil/internal/api"
)

// Server is an httptest server with canned routes and a request log.
type Server struct {
	URL string
	// TODO(core): Task 8 — unexported mux, recorder, httptest.Server
}

// Recorded is one request seen by the server (token redacted).
type Recorded struct {
	Method, Path, Query string
	Body                []byte
}

// New starts a server, closed on tb.Cleanup.
func New(tb testing.TB) *Server {
	// TODO(core): Task 8
	return nil
}

// Handle registers a Go 1.22 mux pattern, e.g. "GET /v1/channels/{id}/messages".
func (s *Server) Handle(pattern string, h http.HandlerFunc) {
	// TODO(core): Task 8
}

// JSON registers a canned JSON response.
func (s *Server) JSON(pattern string, status int, body any) {
	// TODO(core): Task 8
}

// Error registers an error envelope response.
func (s *Server) Error(pattern string, status int, code string) {
	// TODO(core): Task 8
}

// Requests returns every recorded request: method, path, query, body (token redacted).
func (s *Server) Requests() []Recorded {
	// TODO(core): Task 8
	return nil
}

// Client returns an api.Client for this server with token "msl_test".
func (s *Server) Client(opts ...api.Option) *api.Client {
	// TODO(core): Task 8
	return nil
}
