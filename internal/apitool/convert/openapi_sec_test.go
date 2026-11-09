package convert

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// A path key that does not start with "/" (or carries '@' or '\') would
// change the host after {{base_url}}: it is skipped and reported.
func TestOpenAPIPathKeyMustBeAPath(t *testing.T) {
	doc := `{"openapi":"3.0.3","servers":[{"url":"https://api.example.com"}],"paths":{
		"@evil.com/x":{"get":{"summary":"at"}},
		"evil.com/y":{"get":{"summary":"rel"}},
		"/a@evil.com":{"get":{"summary":"at2"}},
		"/a\\b":{"get":{"summary":"bs"}},
		"/ok/{id}":{"get":{"summary":"ok"}}}}`
	col, rep, err := ImportOpenAPI([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(col.Requests) != 1 {
		var urls []string
		for _, r := range col.Requests {
			urls = append(urls, r.URL)
		}
		t.Fatalf("requests = %v, want only /ok/{id}", urls)
	}
	for _, r := range col.Requests {
		if r.URL != "{{base_url}}/ok/{{id}}" {
			t.Errorf("url = %q", r.URL)
		}
	}
	for _, k := range []string{"@evil.com/x", "evil.com/y", "/a@evil.com", `/a\b`} {
		if !hasPrefix(rep.Ignored, "path: "+k) {
			t.Errorf("Ignored %v lacks %q", rep.Ignored, k)
		}
	}
}

// $ref re-use multiplies what one input byte produces: the import has an
// output budget and stops with ErrTooLarge instead of building gigabytes.
func TestOpenAPIRefExpansionBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","components":{"requestBodies":{"big":{"content":{"application/json":{"example":"`)
	b.WriteString(strings.Repeat("a", 256<<10))
	b.WriteString(`"}}}},"pathItems":{"p":{`)
	for i, m := range httpMethods {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `%q:{"requestBody":{"$ref":"#/components/requestBodies/big"}}`, m)
	}
	b.WriteString(`}}},"paths":{`)
	for i := range 30 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"/p%d":{"$ref":"#/components/pathItems/p"}`, i)
	}
	b.WriteString(`}}`)
	if _, _, err := ImportOpenAPI([]byte(b.String())); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// re-use within the budget still imports
	small := strings.Replace(b.String(), strings.Repeat("a", 256<<10), "tiny", 1)
	if col, _, err := ImportOpenAPI([]byte(small)); err != nil || len(col.Requests) != 30*len(httpMethods) {
		t.Fatalf("small: %v", err)
	}
}

// One example alone whose indented form passes the budget (deep nesting:
// every element gets a long indent) stops with ErrTooLarge, and the import
// never holds that output: what it allocates stays near the input's decode
// cost plus the budget, far below the full expansion.
func TestOpenAPISingleExampleBudgetBounded(t *testing.T) {
	const depth, n = 50, 500_000
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","paths":{"/x":{"post":{"requestBody":{"content":{"application/json":{"example":`)
	b.WriteString(strings.Repeat("[", depth))
	b.WriteString(strings.Repeat("1,", n-1) + "1")
	b.WriteString(strings.Repeat("]", depth))
	b.WriteString(`}}}}}}}`)
	doc := []byte(b.String())
	full := n * (2*depth + 2) // the indented example's size
	if full < 4*MaxImportOutput {
		t.Fatalf("fixture too small: %d", full)
	}
	allocs := func(f func()) uint64 {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		f()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	// the decode is linear in the input and not the point: subtract it
	decode := allocs(func() { _, _ = decodeJSON(doc) })
	var err error
	got := allocs(func() { _, _, err = ImportOpenAPI(doc) })
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if extra := int64(got) - int64(decode); extra > 4*MaxImportOutput {
		t.Fatalf("the import allocated %d bytes beyond the decode (budget %d, full expansion %d)", extra, MaxImportOutput, full)
	}
}

// The capped encoder stops at its cap without building the rest.
func TestEncodeCappedStopsEarly(t *testing.T) {
	v := any([]any{})
	for range 40 {
		v = []any{v, json.Number("1")}
	}
	if s, err := encodeCapped(v, true, -1); err != nil || s != encodeJSON(v, true) {
		t.Fatalf("uncapped: %v", err)
	}
	if _, err := encodeCapped(v, true, 64); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// same layout as json.Indent
	o := &object{keys: []string{"a", "b", "e"}, vals: map[string]any{"a": json.Number("1"), "b": []any{"<x>", true, nil}, "e": &object{vals: map[string]any{}}}}
	if got := encodeJSON(o, true); got != "{\n  \"a\": 1,\n  \"b\": [\n    \"<x>\",\n    true,\n    null\n  ],\n  \"e\": {}\n}" {
		t.Fatalf("indent = %q", got)
	}
}
