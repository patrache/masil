// Package convert imports and exports API test collections: curl (both
// ways), OpenAPI 3.0/3.1 JSON (import) and Postman v2.1 (both ways, plus
// environments) — v0.1.3 C3.9. Every input is untrusted: size and depth are
// capped, nothing is fetched (remote $ref are reported, not followed), and
// secret-looking values are reported as suggestions to become variables.
package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/patrache/masil/internal/apitool"
)

// Input limits (C3.9).
const (
	MaxImportBytes = 10 << 20 // an imported file or pasted command
	MaxDepth       = 64       // JSON nesting
	// MaxImportOutput caps the text an import produces: $ref re-use (one
	// path item or body referenced many times) must not turn a small file
	// into gigabytes; over it the import is ErrTooLarge.
	MaxImportOutput = MaxImportBytes
	// rowCost is what a produced row or request counts beyond its text.
	rowCost = 16
)

var (
	// ErrTooLarge: the input is over MaxImportBytes.
	ErrTooLarge = errors.New("convert: input too large")
	// ErrTooDeep: the JSON input nests deeper than MaxDepth.
	ErrTooDeep = errors.New("convert: input nested too deep")
)

// Report tells the user what an import or export left out: Ignored options
// or parts it does not read, Dropped content it could not keep (scripts,
// checks) and Suggestions — secret-looking values that should become
// {{variables}} (C3.9, C3.10 layer 2).
type Report struct {
	Ignored     []string
	Dropped     []string
	Suggestions []Suggestion
}

// Suggestion is a detector finding in one field: Where is "url",
// "query:<name>", "header:<name>", "auth:pass|token|value", "body",
// "form:<name>" or "variable:<name>" (a collection or environment variable,
// no RequestID); RequestID names the request in a collection import.
type Suggestion struct {
	RequestID string
	Where     string
	Finding   apitool.Finding
}

func checkSize(n int) error {
	if n > MaxImportBytes {
		return fmt.Errorf("%w (%d bytes, max %d)", ErrTooLarge, n, MaxImportBytes)
	}
	return nil
}

// suggestVars runs the secret detector over the values of the non-secret
// variables vs (C3.9: secret-looking values already in an imported file).
func suggestVars(vs []apitool.Var) []Suggestion {
	var out []Suggestion
	for _, v := range vs {
		if v.Secret || v.Value == "" {
			continue
		}
		for _, f := range apitool.Detect(v.Name, v.Value) {
			out = append(out, Suggestion{Where: "variable:" + v.Name, Finding: f})
		}
	}
	return out
}

// suggest runs the secret detector over every field of r a credential can
// sit in.
func suggest(r *apitool.Request) []Suggestion {
	var out []Suggestion
	add := func(where, name, value string) {
		for _, f := range apitool.Detect(name, value) {
			out = append(out, Suggestion{RequestID: r.ID, Where: where, Finding: f})
		}
	}
	add("url", "", r.URL)
	for _, kv := range r.Query {
		add("query:"+kv.Key, kv.Key, kv.Value)
	}
	for _, kv := range r.Headers {
		add("header:"+kv.Key, kv.Key, kv.Value)
	}
	add("auth:pass", "password", r.Auth.Pass)
	add("auth:token", "token", r.Auth.Token)
	name := r.Auth.Name
	if name == "" {
		name = "key"
	}
	add("auth:value", name, r.Auth.Value)
	add("body", "", r.Body.Text)
	for _, f := range r.Body.Form {
		add("form:"+f.Key, f.Key, f.Value)
	}
	return out
}

// object is a JSON object that keeps its key order (imports keep the order
// of the source document).
type object struct {
	keys []string
	vals map[string]any
}

func (o *object) get(k string) any {
	if o == nil {
		return nil
	}
	return o.vals[k]
}

// decodeJSON decodes one JSON value: objects as *object (ordered), arrays
// as []any, numbers as json.Number. Nesting deeper than MaxDepth is
// ErrTooDeep; data after the value is an error.
func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("convert: data after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder, depth int) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("convert: bad JSON: %w", err)
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if depth+1 > MaxDepth {
		return nil, ErrTooDeep
	}
	switch d {
	case '{':
		o := &object{vals: map[string]any{}}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("convert: bad JSON: %w", err)
			}
			k, _ := kt.(string)
			v, err := decodeValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			if _, dup := o.vals[k]; !dup {
				o.keys = append(o.keys, k)
			}
			o.vals[k] = v
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("convert: bad JSON: %w", err)
		}
		return o, nil
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := decodeValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("convert: bad JSON: %w", err)
		}
		return arr, nil
	}
	return nil, fmt.Errorf("convert: bad JSON")
}

// encodeJSON writes a decoded value back as JSON, keys in their order,
// indented by two spaces when indent is set (json.Indent's layout); HTML is
// not escaped.
func encodeJSON(v any, indent bool) string {
	s, _ := encodeCapped(v, indent, -1)
	return s
}

// encodeCapped is encodeJSON that never holds more than max bytes (max < 0 =
// no cap): past it, it stops and returns ErrTooLarge. Indenting multiplies a
// deeply nested value, so the cap applies while writing, not after.
func encodeCapped(v any, indent bool, max int) (string, error) {
	c := &capBuf{max: max}
	writeJSON(c, v, indent, 0)
	if c.err != nil {
		return "", fmt.Errorf("%w (output over %d bytes)", ErrTooLarge, max)
	}
	return c.b.String(), nil
}

// capBuf is a buffer refusing to grow past max bytes (max < 0 = no cap);
// the first refusal sticks in err.
type capBuf struct {
	b   bytes.Buffer
	max int
	err error
}

func (c *capBuf) fits(n int) bool {
	if c.err == nil && c.max >= 0 && c.b.Len()+n > c.max {
		c.err = ErrTooLarge
	}
	return c.err == nil
}

func (c *capBuf) Write(p []byte) (int, error) {
	if !c.fits(len(p)) {
		return 0, c.err
	}
	return c.b.Write(p)
}

func (c *capBuf) str(s string) {
	if c.fits(len(s)) {
		c.b.WriteString(s)
	}
}

const spaces = "                                                                "

// newline starts a line indented depth levels (two spaces each).
func (c *capBuf) newline(depth int) {
	c.str("\n")
	for n := 2 * depth; n > 0 && c.err == nil; n -= len(spaces) {
		c.str(spaces[:min(n, len(spaces))])
	}
}

func writeJSON(c *capBuf, v any, indent bool, depth int) {
	if c.err != nil {
		return
	}
	switch t := v.(type) {
	case *object:
		c.str("{")
		for i, k := range t.keys {
			if c.err != nil {
				return
			}
			if i > 0 {
				c.str(",")
			}
			if indent {
				c.newline(depth + 1)
			}
			writeJSON(c, k, indent, depth+1)
			c.str(":")
			if indent {
				c.str(" ")
			}
			writeJSON(c, t.vals[k], indent, depth+1)
		}
		if indent && len(t.keys) > 0 {
			c.newline(depth)
		}
		c.str("}")
	case []any:
		c.str("[")
		for i, e := range t {
			if c.err != nil {
				return
			}
			if i > 0 {
				c.str(",")
			}
			if indent {
				c.newline(depth + 1)
			}
			writeJSON(c, e, indent, depth+1)
		}
		if indent && len(t) > 0 {
			c.newline(depth)
		}
		c.str("]")
	case json.Number:
		c.str(t.String())
	default:
		// a string is at least its length plus quotes: refuse before the
		// encoder builds it
		if s, ok := t.(string); ok && !c.fits(len(s)+2) {
			return
		}
		var e bytes.Buffer
		enc := json.NewEncoder(&e)
		enc.SetEscapeHTML(false)
		if enc.Encode(t) != nil {
			return
		}
		b := bytes.TrimSuffix(e.Bytes(), []byte("\n"))
		_, _ = c.Write(b)
	}
}
