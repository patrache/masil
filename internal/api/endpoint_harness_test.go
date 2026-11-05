package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

// epCase is one endpoint method exercised against the fake server (plan 03
// Task 10/11 shared harness): one table row per endpoint.
type epCase struct {
	name      string // method name; "Method/variant" for extra rows of the same method
	route     string // apitest pattern, e.g. "POST /v1/friends/requests"
	status    int    // response status (0 => 200, or 204 when resp is "")
	resp      string // canned JSON ("" for 204)
	call      func(c *api.Client) (any, error)
	wantPath  string // exact escaped path (Recorded.Path == r.URL.EscapedPath()); "" => route path
	wantQuery string // url.Values.Encode() of the expected query ("" if none); compared semantically
	wantBody  string // expected JSON body (semantic compare) or "" for no body
	noAuth    bool   // the request must NOT carry Authorization
	limiter   string // dedicated api.md 0.7 limiter ("" => only RL_API)
	check     func(t *testing.T, got any)
}

// dedicatedLimiters are every api.md 0.7 limiter an endpoint can name
// besides RL_API (RL_WS belongs to realtime).
var dedicatedLimiters = []string{"RL_SIGNUP", "RL_LOGIN", "RL_MESSAGE", "RL_POST", "RL_REACT", "RL_UPLOAD", "RL_SOCIAL", "RL_SEARCH"}

// runCases runs every case in its own subtest with a fresh fake server and
// asserts the recorded method/path/query/body, the auth header, the decoded
// result (check, or "decoded into a non-zero value" when resp is non-empty),
// and the dedicated limiter.
func runCases(t *testing.T, cases []epCase) {
	t.Helper()
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.name] {
			t.Fatalf("duplicate case name %q", c.name)
		}
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			runCase(t, c)
			checkLimiter(t, c)
		})
	}
}

func routeParts(t *testing.T, route string) (method, path string) {
	t.Helper()
	method, path, ok := strings.Cut(route, " ")
	if !ok {
		t.Fatalf("route %q has no method", route)
	}
	return method, path
}

func runCase(t *testing.T, c epCase) {
	method, routePath := routeParts(t, c.route)
	status := c.status
	if status == 0 {
		status = http.StatusOK
		if c.resp == "" {
			status = http.StatusNoContent
		}
	}
	s := apitest.New(t)
	var (
		mu  sync.Mutex
		hdr http.Header
	)
	s.Handle(c.route, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hdr = r.Header.Clone()
		mu.Unlock()
		if c.resp == "" {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		w.Write([]byte(c.resp))
	})
	got, err := c.call(s.Client())
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", c.name, err)
	}
	reqs := s.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	rec := reqs[0]
	if rec.Method != method {
		t.Errorf("method = %s, want %s", rec.Method, method)
	}
	wantPath := c.wantPath
	if wantPath == "" {
		if strings.Contains(routePath, "{") {
			t.Fatalf("case %q: route has wildcards, set wantPath", c.name)
		}
		wantPath = routePath
	}
	if rec.Path != wantPath {
		t.Errorf("path = %q, want %q", rec.Path, wantPath)
	}
	gotQ, err := url.ParseQuery(rec.Query)
	if err != nil {
		t.Fatalf("recorded query %q: %v", rec.Query, err)
	}
	wantQ, err := url.ParseQuery(c.wantQuery)
	if err != nil {
		t.Fatalf("wantQuery %q: %v", c.wantQuery, err)
	}
	if len(gotQ) != 0 || len(wantQ) != 0 {
		if !reflect.DeepEqual(gotQ, wantQ) {
			t.Errorf("query = %q, want %q", rec.Query, c.wantQuery)
		}
	}
	if c.wantBody == "" {
		if len(rec.Body) != 0 {
			t.Errorf("body = %s, want none", rec.Body)
		}
	} else {
		jsonEqual(t, rec.Body, c.wantBody)
	}
	mu.Lock()
	auth := hdr.Get("Authorization")
	mu.Unlock()
	switch {
	case c.noAuth && auth != "":
		t.Errorf("Authorization sent on an unauthenticated endpoint")
	case !c.noAuth && auth != "Bearer msl_test":
		t.Errorf("Authorization = %q, want the bearer token", auth)
	}
	if c.check != nil {
		c.check(t, got)
	} else if c.resp != "" {
		mustNonZero(t, got)
	}
}

// mustNonZero asserts got decoded into something (wrong envelope keys decode to zero values).
func mustNonZero(t *testing.T, got any) {
	t.Helper()
	v := reflect.ValueOf(got)
	if !v.IsValid() {
		t.Fatalf("result is nil")
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			t.Fatalf("result is a nil %T", got)
		}
		if v.Elem().IsZero() {
			t.Fatalf("result %T decoded to its zero value", got)
		}
		if items := v.Elem().FieldByName("Items"); items.IsValid() && items.Len() == 0 {
			t.Fatalf("result %T has no items", got)
		}
	case reflect.Slice:
		if v.Len() == 0 {
			t.Fatalf("result %T is empty", got)
		}
		if v.Index(0).IsZero() {
			t.Fatalf("result %T[0] decoded to its zero value", got)
		}
	default:
		if v.IsZero() {
			t.Fatalf("result %T is zero", got)
		}
	}
}

// checkLimiter proves the method's dedicated limiter: after a 429 naming
// limiter L, a second call is held locally (no request) iff L is the
// method's limiter.
func checkLimiter(t *testing.T, c epCase) {
	t.Helper()
	if c.limiter != "" && !contains(dedicatedLimiters, c.limiter) {
		t.Fatalf("case %q: unknown limiter %q", c.name, c.limiter)
	}
	for _, l := range dedicatedLimiters {
		s := apitest.New(t)
		s.Handle(c.route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error":{"code":"rate_limited","message":"slow down","details":{"limiter":%q,"retry_after_sec":30}}}`, l)
		})
		cl := s.Client()
		if _, err := c.call(cl); !api.IsCode(err, "rate_limited") {
			t.Fatalf("limiter %s: first call err = %v, want rate_limited", l, err)
		}
		_, err := c.call(cl)
		if !api.IsCode(err, "rate_limited") {
			t.Fatalf("limiter %s: second call err = %v, want rate_limited", l, err)
		}
		want := 2
		if l == c.limiter {
			want = 1
		}
		if n := len(s.Requests()); n != want {
			t.Errorf("after a %s cooldown: %d requests, want %d (method limiter %q)", l, n, want, c.limiter)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// checkCoverage asserts every method in want exists on *api.Client and has
// at least one case (case names "M" or "M/variant"), and no case names an
// out-of-range method.
func checkCoverage(t *testing.T, want []string, cases []epCase) {
	t.Helper()
	ct := reflect.TypeOf(&api.Client{})
	inRange := map[string]bool{}
	for _, m := range want {
		inRange[m] = true
		if _, ok := ct.MethodByName(m); !ok {
			t.Errorf("*api.Client has no method %s", m)
		}
	}
	covered := map[string]bool{}
	for _, c := range cases {
		m, _, _ := strings.Cut(c.name, "/")
		if !inRange[m] {
			t.Errorf("case %q is not a method in this range", c.name)
		}
		covered[m] = true
	}
	var missing []string
	for _, m := range want {
		if !covered[m] {
			missing = append(missing, m)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("methods without a case: %v", missing)
	}
}

// sample returns testdata/types/<name>.json (a fully populated example).
func sample(t *testing.T, name string) string {
	t.Helper()
	return string(mustRead(t, "testdata/types/"+name+".json"))
}

// env wraps a sample in {"<key>": sample}.
func env(t *testing.T, key, typeName string) string {
	t.Helper()
	k, _ := json.Marshal(key)
	return fmt.Sprintf(`{%s:%s}`, k, sample(t, typeName))
}

// itemsOf is {"items":[sample]}.
func itemsOf(t *testing.T, typeName string) string {
	t.Helper()
	return fmt.Sprintf(`{"items":[%s]}`, sample(t, typeName))
}

// pageOf is {"items":[sample],"next_cursor":"c2"}.
func pageOf(t *testing.T, typeName string) string {
	t.Helper()
	return fmt.Sprintf(`{"items":[%s],"next_cursor":"c2"}`, sample(t, typeName))
}
