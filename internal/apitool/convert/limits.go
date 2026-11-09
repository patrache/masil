// Package convert imports and exports API test collections: curl (both
// ways), OpenAPI 3.0/3.1 JSON (import) and Postman v2.1 (both ways, plus
// environments) — v0.1.3 C3.9. Every input is untrusted: size and depth are
// capped, nothing is fetched (remote $ref are reported, not followed), and
// secret-looking values are reported as suggestions to become variables.
package convert

import (
	"errors"
	"fmt"

	"github.com/patrache/masil/internal/apitool"
)

// Input limits (C3.9).
const (
	MaxImportBytes = 10 << 20 // an imported file or pasted command
	MaxDepth       = 64       // JSON nesting
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
// "query:<name>", "header:<name>", "auth:pass|token|value", "body" or
// "form:<name>"; RequestID names the request in a collection import.
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
