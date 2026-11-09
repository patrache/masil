package leetcode_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
	"github.com/patrache/masil/internal/secrets"
)

func newClient(srv *leetcodetest.Server, creds *leetcode.Credentials, log *slog.Logger) *leetcode.Client {
	return leetcode.New(leetcode.Options{BaseURL: srv.URL, Creds: creds, Log: log})
}

func testCreds() *leetcode.Credentials {
	return &leetcode.Credentials{Session: secrets.New("sess-SECRET"), CSRF: secrets.New("csrf-SECRET"), CFClearance: secrets.New("cf-SECRET")}
}

func TestDailyParses1606(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.SetDaily("daily_1606.json")
	q, date, err := newClient(srv, nil, nil).Daily(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-11-09" || q.FrontendID != "1606" || q.QuestionID != "1710" || q.Difficulty != "Hard" ||
		q.Slug != "find-servers-that-handled-most-number-of-requests" || q.IsPaidOnly {
		t.Fatalf("q = %+v date %q", q, date)
	}
	found := false
	for _, tag := range q.Tags {
		found = found || tag == "Ordered Set"
	}
	if !found {
		t.Fatalf("tags = %v", q.Tags)
	}
	if !strings.HasPrefix(q.Snippets["golang"], "func busiestServers(") {
		t.Fatalf("golang snippet = %q", q.Snippets["golang"])
	}
	if len(q.ExampleTestcases) != 3 || !strings.Contains(q.ContentHTML, "<strong>busiest</strong>") {
		t.Fatalf("examples %q content %q", q.ExampleTestcases, q.ContentHTML)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want meta + body", len(reqs))
	}
	for _, r := range reqs {
		if r.Method != "POST" || r.Path != "/graphql/" || r.Header.Get("Cookie") != "" || r.Header.Get("x-csrftoken") != "" {
			t.Errorf("request %+v", r)
		}
	}
	if strings.Contains(reqs[0].Body, "content") || !strings.Contains(reqs[1].Body, "content") {
		t.Errorf("meta query asks for the body, or the body query does not:\n%s\n%s", reqs[0].Body, reqs[1].Body)
	}
}

func TestPremiumDailyNoContentRequest(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.SetDaily("premium.json")
	q, date, err := newClient(srv, testCreds(), nil).Daily(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-11-10" || !q.IsPaidOnly || q.ContentHTML != "" || len(q.Snippets) != 0 || q.Slug != "meeting-rooms-ii" {
		t.Fatalf("q = %+v", q)
	}
	for _, r := range srv.Requests() {
		if strings.Contains(r.Body, "content") {
			t.Fatalf("a request asked for the body: %s", r.Body)
		}
	}
	// opening a premium problem by slug: meta only, ErrPremiumOnly
	srv.SetQuestion("meeting-rooms-ii", "premium.json")
	q, err = newClient(srv, testCreds(), nil).Question(t.Context(), "meeting-rooms-ii")
	if !errors.Is(err, leetcode.ErrPremiumOnly) || q == nil || q.Title != "Meeting Rooms II" || q.ContentHTML != "" {
		t.Fatalf("question = %+v, %v", q, err)
	}
	for _, r := range srv.Requests() {
		if strings.Contains(r.Body, "content") {
			t.Fatalf("a request asked for the body: %s", r.Body)
		}
	}
}

func TestQuestionTwoSum(t *testing.T) {
	srv := leetcodetest.New(t)
	q, err := newClient(srv, nil, nil).Question(t.Context(), "two-sum")
	if err != nil {
		t.Fatal(err)
	}
	if q.FrontendID != "1" || q.Title != "Two Sum" || len(q.ExampleTestcases) != 3 || q.Snippets["golang"] == "" {
		t.Fatalf("q = %+v", q)
	}
	if _, err := newClient(srv, nil, nil).Question(t.Context(), "../../etc"); !errors.Is(err, leetcode.ErrBadSlug) {
		t.Fatalf("hostile slug: %v", err)
	}
	if _, err := newClient(srv, nil, nil).Question(t.Context(), "no-such-problem"); !errors.Is(err, leetcode.ErrUnexpected) {
		t.Fatalf("missing problem: %v", err)
	}
}

func TestClassifyCloudflareHTML200(t *testing.T) {
	page, err := os.ReadFile("leetcodetest/testdata/cf_challenge.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		status      int
		contentType string
		body        string
		want        error
	}{
		{200, "text/html; charset=UTF-8", string(page), leetcode.ErrBlocked},
		{403, "text/html", string(page), leetcode.ErrBlocked},
		{200, "application/json", "  <html>oops</html>", leetcode.ErrBlocked},
		{503, "text/plain", "<!DOCTYPE html>", leetcode.ErrBlocked},
		{403, "application/json", `{"detail":"Authentication credentials were not provided."}`, leetcode.ErrAuthExpired},
		{403, "application/json", "", leetcode.ErrAuthExpired},
		{401, "application/json", "{}", leetcode.ErrAuthExpired},
		{429, "application/json", `{"error":"too many"}`, leetcode.ErrRateLimited},
		{500, "application/json", `{"error":"boom"}`, leetcode.ErrUnexpected},
		{200, "application/json", `not json`, leetcode.ErrUnexpected},
		{200, "application/json", `{"data":null,"errors":[{"message":"x"}]}`, leetcode.ErrUnexpected},
	} {
		srv := leetcodetest.New(t)
		srv.Fail("/graphql/", c.status, c.contentType, c.body)
		_, _, err := newClient(srv, testCreds(), nil).Daily(t.Context())
		if !errors.Is(err, c.want) {
			t.Errorf("%d %s %.20q: err = %v, want %v", c.status, c.contentType, c.body, err, c.want)
		}
		if n := len(srv.Requests()); n != 1 {
			t.Errorf("%d %s: %d requests (no retry wanted)", c.status, c.contentType, n)
		}
	}
}

func TestResponseBodyLimit(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.Fail("/graphql/", 200, "application/json", `{"data":{"x":"`+strings.Repeat("a", 4<<20)+`"}}`)
	if _, _, err := newClient(srv, nil, nil).Daily(t.Context()); !errors.Is(err, leetcode.ErrUnexpected) {
		t.Fatalf("err = %v", err)
	}
}

func TestCookiesOnlyToLeetCodeHost(t *testing.T) {
	srv := leetcodetest.New(t)
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := newClient(srv, testCreds(), log).Question(t.Context(), "two-sum"); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request")
	}
	r := reqs[0]
	cookie := r.Header.Get("Cookie")
	for _, want := range []string{"LEETCODE_SESSION=sess-SECRET", "csrftoken=csrf-SECRET", "cf_clearance=cf-SECRET"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("Cookie %q lacks %q", cookie, want)
		}
	}
	if r.Header.Get("x-csrftoken") != "csrf-SECRET" {
		t.Errorf("x-csrftoken = %q", r.Header.Get("x-csrftoken"))
	}
	if ref := r.Header.Get("Referer"); !strings.HasSuffix(ref, "/problems/two-sum/") {
		t.Errorf("Referer = %q", ref)
	}
	if logBuf.Len() == 0 {
		t.Error("nothing logged at debug")
	}
	for _, v := range []string{"sess-SECRET", "csrf-SECRET", "cf-SECRET"} {
		if strings.Contains(logBuf.String(), v) {
			t.Fatalf("log leaks %q:\n%s", v, logBuf.String())
		}
	}
	// a redirect to another host is not followed: the cookies never leave the LeetCode host
	var leaked []http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = append(leaked, r.Header.Clone())
	}))
	defer other.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/graphql/", http.StatusTemporaryRedirect)
	}))
	defer redir.Close()
	c := leetcode.New(leetcode.Options{BaseURL: redir.URL, Creds: testCreds()})
	if _, err := c.Question(t.Context(), "two-sum"); !errors.Is(err, leetcode.ErrUnexpected) {
		t.Fatalf("redirect: %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("redirect followed: %v", leaked)
	}
}

func TestUserStatus(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.SetUserStatus(true, "hana_lc", true)
	st, err := newClient(srv, testCreds(), nil).UserStatus(t.Context())
	if err != nil || !st.SignedIn || st.Username != "hana_lc" || !st.Premium {
		t.Fatalf("status = %+v, %v", st, err)
	}
	srv.SetUserStatus(false, "", false)
	st, err = newClient(srv, nil, nil).WithCreds(testCreds()).UserStatus(t.Context())
	if err != nil || st.SignedIn {
		t.Fatalf("signed out = %+v, %v", st, err)
	}
	if got := srv.Requests()[1].Header.Get("Cookie"); got == "" {
		t.Fatal("WithCreds did not add the cookies")
	}
}

// A Django login redirect carries text/html and an empty body: it is an
// expired sign-in, not a block page (A10.5 — the login dialog reopens).
func TestLoginRedirectIsAuthExpired(t *testing.T) {
	for _, c := range []struct {
		location string
		want     error
	}{
		{"/accounts/login/?next=/problems/two-sum/submit/", leetcode.ErrAuthExpired},
		{"/somewhere/else/", leetcode.ErrUnexpected},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", c.location)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusFound)
		}))
		cl := leetcode.New(leetcode.Options{BaseURL: srv.URL, Creds: testCreds()})
		q := &leetcode.Question{QuestionID: "1", FrontendID: "1", Slug: "two-sum"}
		_, err := cl.Submit(t.Context(), q, "golang", "x")
		if !errors.Is(err, c.want) || errors.Is(err, leetcode.ErrBlocked) {
			t.Errorf("302 -> %s: err = %v, want %v", c.location, err, c.want)
		}
		srv.Close()
	}
}
