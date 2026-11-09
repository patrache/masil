package convert

import (
	"errors"
	"fmt"
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
