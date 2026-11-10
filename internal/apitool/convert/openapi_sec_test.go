package convert

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
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

// pathItemRefDoc builds a document whose paths all $ref one path item
// (components.pathItems.p = item): the fan-out is paths × the item's size.
func pathItemRefDoc(item string, paths int) []byte {
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","components":{"pathItems":{"p":`)
	b.WriteString(item)
	b.WriteString(`}},"paths":{`)
	for i := range paths {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"/p%d":{"$ref":"#/components/pathItems/p"}`, i)
	}
	b.WriteString(`}}`)
	return []byte(b.String())
}

func repeatJoin(n int, f func(i int) string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = f(i)
	}
	return strings.Join(parts, ",")
}

// $ref fan-out that produces no output (non-method keys, nameless
// parameters, examples without a value) is still work: the import has a
// work budget and stops with ErrTooLarge quickly instead of running for
// minutes on a small file.
func TestOpenAPIRefFanOutWorkBudget(t *testing.T) {
	cases := map[string][]byte{
		// many non-method keys in a path item every path refers to
		"junk keys": pathItemRefDoc(`{"get":{},`+repeatJoin(200_000, func(i int) string { return fmt.Sprintf(`"x%d":0`, i) })+`}`, 400),
		// a shared parameters array of non-objects
		"non-object params": pathItemRefDoc(`{"get":{},"parameters":[`+repeatJoin(200_000, func(int) string { return "0" })+`]}`, 400),
		// nameless parameters, each with its own "in" (no dedup, no output)
		"nameless params": pathItemRefDoc(`{"get":{},"parameters":[`+repeatJoin(20_000, func(i int) string { return fmt.Sprintf(`{"in":"%d"}`, i) })+`]}`, 1_000),
		// a named parameter whose examples carry no value
		"examples without value": pathItemRefDoc(`{"get":{},"parameters":[{"name":"q","in":"query","examples":{`+repeatJoin(100_000, func(i int) string { return fmt.Sprintf(`"e%d":{}`, i) })+`}}]}`, 400),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if len(doc) > MaxImportBytes {
				t.Fatalf("fixture %d bytes over the input limit", len(doc))
			}
			start := time.Now()
			_, _, err := ImportOpenAPI(doc)
			if el := time.Since(start); el > 5*time.Second {
				t.Errorf("import took %v", el)
			}
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
		})
	}
}

// A long $ref string followed again and again is work in proportion to its
// length.
func TestOpenAPILongRefWorkBudget(t *testing.T) {
	ref := "#/components/x/" + strings.Repeat("a/", 1<<20)
	doc := pathItemRefDoc(`{"get":{"requestBody":{"$ref":"`+ref+`"}}}`, 400)
	if len(doc) > MaxImportBytes {
		t.Fatalf("fixture %d bytes over the input limit", len(doc))
	}
	start := time.Now()
	_, _, err := ImportOpenAPI(doc)
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("import took %v", el)
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// Many path parameters in one operation become collection variables in
// linear time (no scan of every variable per parameter).
func TestOpenAPIManyPathParamsLinear(t *testing.T) {
	const n = 80_000
	params := repeatJoin(n, func(i int) string { return fmt.Sprintf(`{"name":"v%d","in":"path"}`, i) })
	doc := []byte(`{"openapi":"3.0.3","paths":{"/x":{"get":{"parameters":[` + params + `]}}}}`)
	start := time.Now()
	col, _, err := ImportOpenAPI(doc)
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("import took %v", el)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(col.Vars) != n {
		t.Fatalf("vars = %d, want %d", len(col.Vars), n)
	}
}

// A path item shared by many paths, with a realistic amount of shared
// parameters and a form body, still imports under the work budget.
func TestOpenAPIRealisticReuseImports(t *testing.T) {
	var comps strings.Builder
	for i := range 30 {
		if i > 0 {
			comps.WriteString(",")
		}
		fmt.Fprintf(&comps, `"q%d":{"name":"q%d","in":"query","schema":{"$ref":"#/components/schemas/s"}}`, i, i)
	}
	refs := repeatJoin(30, func(i int) string { return fmt.Sprintf(`{"$ref":"#/components/parameters/q%d"}`, i) })
	props := repeatJoin(30, func(i int) string { return fmt.Sprintf(`"f%d":{"$ref":"#/components/schemas/s"}`, i) })
	op := `{"parameters":[` + refs + `],"requestBody":{"content":{"application/x-www-form-urlencoded":{"schema":{"properties":{` + props + `}}}}}}`
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","components":{"schemas":{"s":{"type":"string","example":"x"}},"parameters":{` + comps.String() + `},"pathItems":{"p":{`)
	b.WriteString(repeatJoin(4, func(i int) string { return fmt.Sprintf(`%q:%s`, httpMethods[i], op) }))
	b.WriteString(`}}},"paths":{`)
	const paths = 1_000
	b.WriteString(repeatJoin(paths, func(i int) string { return fmt.Sprintf(`"/p%d":{"$ref":"#/components/pathItems/p"}`, i) }))
	b.WriteString(`}}`)
	col, _, err := ImportOpenAPI([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(col.Requests) != 4*paths {
		t.Fatalf("requests = %d", len(col.Requests))
	}
}
