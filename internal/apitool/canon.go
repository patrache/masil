package apitool

import (
	"bytes"
	"encoding/json"
)

// canonicalJSON is v as JSON with sorted keys, two-space indent and a final
// newline, so a file diffs cleanly in git and the same value always gives the
// same bytes. HTML characters are not escaped; numbers keep their text.
func canonicalJSON(v any) ([]byte, error) {
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(&raw)
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	enc = json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(generic); err != nil { // Encode ends with "\n"
		return nil, err
	}
	return out.Bytes(), nil
}
