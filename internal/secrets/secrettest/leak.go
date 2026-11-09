package secrettest

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api/apitest"
)

// AssertNoLeak fails tb when any of values (empty ones are skipped) appears in
// a request srv recorded — method, path, query, header names and values, body —
// or in any file under logDir ("" = no log check), raw or URL/JSON-escaped
// (v0.1.3 A9.5). Reports name the place and the value's index, never the value.
func AssertNoLeak(tb testing.TB, srv *apitest.Server, logDir string, values ...string) {
	tb.Helper()
	type needle struct {
		i     int
		forms []string
	}
	var needles []needle
	for i, v := range values {
		if v != "" {
			needles = append(needles, needle{i, encodings(v)})
		}
	}
	if len(needles) == 0 {
		return
	}
	check := func(where, hay string) {
		tb.Helper()
		for _, n := range needles {
			for _, f := range n.forms {
				if strings.Contains(hay, f) {
					tb.Errorf("secrettest: value #%d leaked in %s", n.i, where)
					break
				}
			}
		}
	}
	if srv != nil {
		for ri, r := range srv.Requests() {
			// Only the request's index is named: its method or path may hold the value.
			at := func(part string) string { return "request " + strconv.Itoa(ri) + " " + part }
			check(at("method"), r.Method)
			check(at("path"), r.Path)
			check(at("query"), r.Query)
			check(at("body"), string(r.Body))
			names := make([]string, 0, len(r.Header))
			for k := range r.Header {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				check(at("header name"), k)
				for _, v := range r.Header[k] {
					check(at("header "+k), v)
				}
			}
		}
	}
	if logDir != "" {
		_ = filepath.WalkDir(logDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				tb.Errorf("secrettest: read %s: %v", p, rerr)
				return nil
			}
			rel, _ := filepath.Rel(logDir, p)
			check("log file "+rel, string(b))
			return nil
		})
	}
}

// encodings are the forms a value can take on the wire: raw, URL query and
// path escaped, JSON string escaped.
func encodings(v string) []string {
	forms := []string{v, url.QueryEscape(v), url.PathEscape(v)}
	if b, err := json.Marshal(v); err == nil {
		forms = append(forms, strings.Trim(string(b), `"`))
	}
	out := forms[:0]
	seen := map[string]bool{}
	for _, f := range forms {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}
