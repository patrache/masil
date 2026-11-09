package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Pending item 8-a follow-up: the server side checks api.md 6 ("추가 리미터"
// column) against its routes (server/test/unit/contract-limiters.test.ts), but
// the client reads the same table as the contract for request.Limiter, and
// nothing compared the two. AddChannelMembers (RL_SOCIAL) and ChangePassword
// (RL_LOGIN) drifted: after a 429 cooldown the client still sent the request
// and got another 429. This test re-derives every case's limiter from the
// table, so a client method and its harness row can no longer agree with each
// other while disagreeing with the contract.

var contractMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

var (
	placeholderRe  = regexp.MustCompile(`\{[^}]+\}`)
	limiterCellRe  = regexp.MustCompile(`^(RL_[A-Z_]+)(?:\(([A-Z/]+)\))?$`)
	realtimeLimits = map[string]bool{"RL_WS": true, "RL_SHARE_WS": true} // socket upgrades, not request.Limiter
)

// contractNormPath turns {user}, {id}, {channel_id}... into * (names differ between doc and harness).
func contractNormPath(p string) string { return placeholderRe.ReplaceAllString(p, "*") }

// parseContractLimiters reads api.md section 6 into "METHOD /path" -> sorted
// additional limiters, with the same cell rules as the server-side test:
// "RL_X" applies to every method of the row, "RL_X(PUT/DELETE)" only to the
// listed ones, "—"/blank to none; a bare "/suffix" path replaces the last
// segment of the row's first path.
func parseContractLimiters(t *testing.T, md string) map[string][]string {
	t.Helper()
	start := strings.Index(md, "\n## 6. ")
	if start < 0 {
		t.Fatalf("api.md: section 6 not found")
	}
	section := md[start:]
	if end := strings.Index(section[1:], "\n## "); end >= 0 {
		section = section[:end+1]
	}
	out := map[string][]string{}
	for _, line := range strings.Split(section, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 6 {
			continue
		}
		cells := parts[1:5]
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "메서드" || strings.Trim(cells[0], "-") == "" {
			continue
		}
		methods := strings.Split(cells[0], "/")
		for _, m := range methods {
			if !contractMethods[m] {
				t.Fatalf("api.md 6: bad method %q in: %s", m, line)
			}
		}
		items := strings.Split(cells[1], ",")
		first := strings.TrimSpace(items[0])
		var paths []string
		for _, p := range items {
			p = strings.TrimSpace(p)
			if !strings.HasPrefix(p, "/v1/") {
				p = first[:strings.LastIndex(first, "/")] + p
			}
			paths = append(paths, p)
		}
		per := map[string]map[string]bool{}
		for _, m := range methods {
			per[m] = map[string]bool{}
		}
		if cell := cells[3]; cell != "" && cell != "—" {
			for _, part := range strings.Split(cell, ",") {
				part = strings.TrimSpace(part)
				mm := limiterCellRe.FindStringSubmatch(part)
				if mm == nil {
					t.Fatalf("api.md 6: bad limiter cell in: %s", line)
				}
				applies := methods
				if mm[2] != "" {
					applies = strings.Split(mm[2], "/")
				}
				for _, m := range applies {
					if per[m] == nil {
						t.Fatalf("api.md 6: limiter %s names a method the row lacks: %s", part, line)
					}
					per[m][mm[1]] = true
				}
			}
		}
		for _, p := range paths {
			for _, m := range methods {
				key := m + " " + contractNormPath(p)
				if _, dup := out[key]; dup {
					t.Fatalf("api.md 6: %s listed twice", key)
				}
				ls := []string{}
				for l := range per[m] {
					ls = append(ls, l)
				}
				sort.Strings(ls)
				out[key] = ls
			}
		}
	}
	return out
}

func loadContractLimiters(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "api.md"))
	if err != nil {
		t.Fatalf("read api.md: %v", err)
	}
	doc := parseContractLimiters(t, string(b))
	if len(doc) < 100 {
		t.Fatalf("api.md 6: parsed only %d (method, path) pairs", len(doc))
	}
	return doc
}

// pendingLimiterRows are api.md 6 rows whose server route landed before the
// client method that exercises them. Each entry names the Task that adds the
// client method and its endpoint case; once a case covers the row the test
// fails until the entry is removed, so the waiver cannot outlive its Task.
var pendingLimiterRows = map[string]string{}

// uncoveredLimiterRows reports every row that names a request limiter but no
// case exercises (so checkLimiter, which proves request.Limiter, runs against
// it), except rows in pending; a pending row that is covered, or that api.md
// no longer has, is reported too.
func uncoveredLimiterRows(doc map[string][]string, covered map[string]bool, pending map[string]string) []string {
	var wrong []string
	for key, ls := range doc {
		for _, l := range ls {
			if !realtimeLimits[l] && !covered[key] {
				if _, ok := pending[key]; !ok {
					wrong = append(wrong, key+": api.md 6 lists "+l+" but no endpoint case exercises it")
				}
				break
			}
		}
	}
	for key, task := range pending {
		switch {
		case doc[key] == nil:
			wrong = append(wrong, key+": pendingLimiterRows names a row api.md 6 does not have")
		case covered[key]:
			wrong = append(wrong, key+": covered by a case now; remove it from pendingLimiterRows ("+task+")")
		}
	}
	sort.Strings(wrong)
	return wrong
}

func TestEndpointLimitersMatchContractTable(t *testing.T) {
	doc := loadContractLimiters(t)
	cases := append(endpointsACases(t), endpointsBCases(t)...)
	cases = append(cases, docEndpointCases(t)...)
	cases = append(cases, workspaceEndpointCases(t)...)
	covered := map[string]bool{}
	var wrong []string
	for _, c := range cases {
		method, path := routeParts(t, c.route)
		key := method + " " + contractNormPath(path)
		covered[key] = true
		listed, ok := doc[key]
		if !ok {
			wrong = append(wrong, c.name+": "+key+" has no row in api.md 6")
			continue
		}
		var want []string
		for _, l := range listed {
			if !realtimeLimits[l] {
				want = append(want, l)
			}
		}
		switch {
		case len(want) > 1:
			wrong = append(wrong, c.name+": api.md 6 lists several limiters "+strings.Join(want, ",")+"; request.Limiter holds one")
		case len(want) == 0 && c.limiter != "":
			wrong = append(wrong, c.name+": harness says "+c.limiter+", api.md 6 says none")
		case len(want) == 1 && c.limiter != want[0]:
			got := c.limiter
			if got == "" {
				got = "none"
			}
			wrong = append(wrong, c.name+": harness says "+got+", api.md 6 says "+want[0])
		}
	}
	wrong = append(wrong, uncoveredLimiterRows(doc, covered, pendingLimiterRows)...)
	sort.Strings(wrong)
	for _, w := range wrong {
		t.Error(w)
	}
}

func TestUncoveredLimiterRows(t *testing.T) {
	doc := map[string][]string{
		"POST /v1/a":   {"RL_X"},
		"POST /v1/b":   {"RL_X"},
		"GET /v1/ws":   {"RL_WS"},
		"POST /v1/c":   {"RL_X"},
		"DELETE /v1/d": {},
	}
	covered := map[string]bool{"POST /v1/a": true, "POST /v1/c": true}
	pending := map[string]string{"POST /v1/b": "Task 30", "POST /v1/c": "Task 30", "POST /v1/gone": "Task 30"}
	got := uncoveredLimiterRows(doc, covered, pending)
	want := []string{
		"POST /v1/c: covered by a case now; remove it from pendingLimiterRows (Task 30)",
		"POST /v1/gone: pendingLimiterRows names a row api.md 6 does not have",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := uncoveredLimiterRows(doc, map[string]bool{}, nil); len(got) != 3 {
		t.Fatalf("without pending: %v, want 3 uncovered rows", got)
	}
}

func TestParseContractLimiters(t *testing.T) {
	got := parseContractLimiters(t, "x\n## 6. 엔드포인트 요약표\n\n| 메서드 | 경로 | 인증 | 추가 리미터 |\n|---|---|---|---|\n"+
		"| PUT/DELETE/GET | /v1/m/{id}/a, /b | ✓ | RL_REACT(PUT/DELETE) |\n| GET | /v1/x, /v1/y | 없음 | — |\n\n## 7. next\n| GET | /v1/z | ✓ | RL_WS |\n")
	want := map[string][]string{
		"PUT /v1/m/*/a": {"RL_REACT"}, "DELETE /v1/m/*/a": {"RL_REACT"}, "GET /v1/m/*/a": {},
		"PUT /v1/m/*/b": {"RL_REACT"}, "DELETE /v1/m/*/b": {"RL_REACT"}, "GET /v1/m/*/b": {},
		"GET /v1/x": {}, "GET /v1/y": {},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %v, want %v", got, want)
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok || strings.Join(g, ",") != strings.Join(w, ",") {
			t.Errorf("%s = %v (present %v), want %v", k, g, ok, w)
		}
	}
}
