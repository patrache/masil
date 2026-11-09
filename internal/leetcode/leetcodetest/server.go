// Package leetcodetest is a fake LeetCode (httptest) for internal/leetcode and
// the leet tool's UI tests. It answers from fixtures in testdata/ — the
// GraphQL queries masil sends, interpret_solution, submit and the check of a
// run or submission — can be told to fail a path with any status, content
// type and body (a Cloudflare page, a 429 …), and records every request.
package leetcodetest

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

//go:embed testdata
var fixtures embed.FS

// Fixture is the content of testdata/<name>.
func Fixture(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		tb.Fatalf("leetcodetest: fixture %q: %v", name, err)
	}
	return b
}

// Recorded is one request the fake got.
type Recorded struct {
	Method, Path string
	Header       http.Header
	Body         string
}

type failure struct {
	status            int
	contentType, body string
}

// Server is the fake. URL is its base URL (leetcode.Options.BaseURL).
type Server struct {
	URL string

	tb         testing.TB
	mu         sync.Mutex
	daily      string            // fixture of activeDailyCodingChallengeQuestion
	questions  map[string]string // slug -> fixture (question- or daily-shaped)
	fails      map[string]failure
	userStatus map[string]any
	reqs       []Recorded
	interpret  string              // fixture answering interpret_solution
	submit     string              // fixture answering submit
	checks     map[string][]string // id -> check fixtures still to answer (the last repeats)
}

// New starts a fake with today's problem = daily_1606.json, two-sum
// openable and a signed-out userStatus. It closes on cleanup.
func New(tb testing.TB) *Server {
	tb.Helper()
	s := &Server{
		tb:         tb,
		daily:      "daily_1606.json",
		questions:  map[string]string{"two-sum": "question_two_sum.json"},
		fails:      map[string]failure{},
		userStatus: map[string]any{"isSignedIn": false, "username": "", "isPremium": nil},
		interpret:  "interpret_ok.json",
		submit:     "submit_ok.json",
		checks:     map[string][]string{},
	}
	hs := httptest.NewServer(http.HandlerFunc(s.serve))
	tb.Cleanup(hs.Close)
	s.URL = hs.URL
	return s
}

// SetDaily makes fixture the answer to today's-problem queries; its question
// can also be opened by slug.
func (s *Server) SetDaily(fixture string) {
	Fixture(s.tb, fixture)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.daily = fixture
}

// SetQuestion makes fixture (question- or daily-shaped) the problem slug.
func (s *Server) SetQuestion(slug, fixture string) {
	Fixture(s.tb, fixture)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.questions[slug] = fixture
}

// SetUserStatus sets who the cookies sign in as.
func (s *Server) SetUserStatus(signedIn bool, username string, premium bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userStatus = map[string]any{"isSignedIn": signedIn, "username": username, "isPremium": premium}
}

// Fail answers every request to path (e.g. "/graphql/") with status,
// contentType and body from now on.
func (s *Server) Fail(path string, status int, contentType, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails[path] = failure{status, contentType, body}
}

// Requests is every request so far, oldest first.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recorded(nil), s.reqs...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.reqs = append(s.reqs, Recorded{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: string(body)})
	f, failing := s.fails[r.URL.Path]
	s.mu.Unlock()
	if failing {
		if f.contentType != "" {
			w.Header().Set("Content-Type", f.contentType)
		}
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.body)
		return
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/graphql/":
		s.graphql(w, body)
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/problems/") && strings.HasSuffix(r.URL.Path, "/interpret_solution/"):
		s.mu.Lock()
		fx := s.interpret
		s.mu.Unlock()
		writeRaw(w, Fixture(s.tb, fx))
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/problems/") && strings.HasSuffix(r.URL.Path, "/submit/"):
		s.mu.Lock()
		fx := s.submit
		s.mu.Unlock()
		writeRaw(w, Fixture(s.tb, fx))
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/submissions/detail/") && strings.HasSuffix(r.URL.Path, "/check/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/submissions/detail/"), "/check/")
		s.mu.Lock()
		q := s.checks[id]
		var fx string
		if len(q) > 0 {
			fx = q[0]
			if len(q) > 1 {
				s.checks[id] = q[1:]
			}
		}
		s.mu.Unlock()
		if fx == "" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such run"})
			return
		}
		writeRaw(w, Fixture(s.tb, fx))
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

// SetInterpret makes fixture the answer to every interpret_solution POST
// (default interpret_ok.json).
func (s *Server) SetInterpret(fixture string) {
	Fixture(s.tb, fixture)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interpret = fixture
}

// SetSubmit makes fixture the answer to every submit POST (default
// submit_ok.json: submission 1234567890).
func (s *Server) SetSubmit(fixture string) {
	Fixture(s.tb, fixture)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submit = fixture
}

// QueueCheck makes the check of id answer fixtures in order; the last one
// keeps answering after that. An id with nothing queued is 404.
func (s *Server) QueueCheck(id string, fixtures ...string) {
	for _, f := range fixtures {
		Fixture(s.tb, f)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[id] = append(s.checks[id], fixtures...)
}

func writeRaw(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) graphql(w http.ResponseWriter, body []byte) {
	var q struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []any{map[string]any{"message": "bad body"}}})
		return
	}
	s.mu.Lock()
	daily, status := s.daily, s.userStatus
	s.mu.Unlock()
	switch {
	case strings.Contains(q.Query, "activeDailyCodingChallengeQuestion"):
		var d map[string]any
		_ = json.Unmarshal(Fixture(s.tb, daily), &d)
		if a, ok := dig(d, "data", "activeDailyCodingChallengeQuestion").(map[string]any); ok {
			if qq, ok := a["question"].(map[string]any); ok {
				onlyAsked(qq, q.Query)
			}
		}
		writeJSON(w, http.StatusOK, d)
	case strings.Contains(q.Query, "userStatus"):
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"userStatus": status}})
	case strings.Contains(q.Query, "question(titleSlug"):
		slug, _ := q.Variables["titleSlug"].(string)
		qq := s.questionObject(slug)
		if qq != nil {
			onlyAsked(qq, q.Query)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"question": qq}})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []any{map[string]any{"message": "unknown query"}}})
	}
}

// questionObject is the question slug from its fixture, or from today's
// fixture when that is the same problem; nil = no such problem.
func (s *Server) questionObject(slug string) map[string]any {
	s.mu.Lock()
	fx, ok := s.questions[slug]
	daily := s.daily
	s.mu.Unlock()
	for _, name := range []string{fx, daily} {
		if name == "" || (name == fx && !ok) {
			continue
		}
		var d map[string]any
		_ = json.Unmarshal(Fixture(s.tb, name), &d)
		for _, path := range [][]string{{"data", "question"}, {"data", "activeDailyCodingChallengeQuestion", "question"}} {
			if qq, ok := dig(d, path...).(map[string]any); ok && qq["titleSlug"] == slug {
				return qq
			}
		}
	}
	return nil
}

// onlyAsked drops the body fields a query did not name (as LeetCode would).
func onlyAsked(q map[string]any, query string) {
	for _, f := range []string{"content", "codeSnippets", "exampleTestcaseList"} {
		if !strings.Contains(query, f) {
			delete(q, f)
		}
	}
}

func dig(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}
