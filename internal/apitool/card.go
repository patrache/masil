package apitool

import (
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// Chat cards api.result and api.run (C3.11, api.md 2): what a card carries
// is decided here — data, then redaction (every secret value used), then
// the 16KB fit. The screen draws the preview from these exact bytes.

// MaxCardBytes is the server's card data limit (api.md 2 Card).
const MaxCardBytes = 16_384

// Card body limits (C3.11).
const (
	cardMaxHeaders  = 20
	cardMaxLines    = 20
	cardMaxLineRune = 200
	cardMaxFailures = 3
)

// ErrCardTooLarge: even the smallest card is over MaxCardBytes.
var ErrCardTooLarge = errors.New("apitool: card over 16KB")

// CardOptions are what a result card may carry beyond the summary; all off
// by default (JC20).
type CardOptions struct {
	ShowHost       bool // expand the non-secret variables of the URL
	IncludeHeaders bool // the sent request headers (sensitive ones masked)
	IncludeBody    bool // the response body's first 20 lines
}

// CardRef points a card at its request: a personal collection's name, or a
// shared collection's document id.
type CardRef struct {
	Collection, RequestID, DocID string
}

type cardAsserts struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

type cardRef struct {
	Collection string `json:"collection"`
	RequestID  string `json:"request_id"`
	DocID      string `json:"doc_id,omitempty"`
}

// ResultData is api.result v1.
type ResultData struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	Status     int         `json:"status"`
	TimeMS     int64       `json:"time_ms"`
	Size       int64       `json:"size"`
	Asserts    cardAsserts `json:"asserts"`
	Env        string      `json:"env,omitempty"`
	Ref        cardRef     `json:"ref"`
	ReqHeaders [][2]string `json:"req_headers,omitempty"`
	BodyHead   []string    `json:"body_head,omitempty"`
}

type runFailure struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

type runRef struct {
	DocID string `json:"doc_id,omitempty"`
}

// RunData is api.run v1.
type RunData struct {
	Collection string       `json:"collection"`
	Requests   int          `json:"requests"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	TimeMS     int64        `json:"time_ms"`
	Failures   []runFailure `json:"failures"`
	Ref        runRef       `json:"ref"`
}

// ResultCard is the api.result data of one exchange. The URL is the
// template ({{base_url}}/v1/payments); ShowHost expands only non-secret
// variables. Every secret value p used is masked wherever it shows.
func ResultCard(r *Request, p *Prepared, resp *Response, as []AssertResult, envName string, ref CardRef, o CardOptions) (json.RawMessage, error) {
	sv := secretTexts(p)
	red := newRedactor(sv)
	d := ResultData{Env: envName, Ref: cardRef{Collection: ref.Collection, RequestID: ref.RequestID, DocID: ref.DocID}}
	if r != nil {
		d.Method, d.URL = methodOf(r), r.URL
	}
	if p != nil {
		if d.Method == "" {
			d.Method = p.Method
		}
		if o.ShowHost {
			d.URL = expandPlain(d.URL, p.Used)
		}
	}
	d.URL = red.replace(d.URL)
	if resp != nil {
		d.Status, d.TimeMS, d.Size = resp.Status, resp.Timing.Total.Milliseconds(), resp.Size
	}
	for _, a := range as {
		d.Asserts.Total++
		if a.Pass {
			d.Asserts.Passed++
		}
	}
	if o.IncludeHeaders && p != nil {
		h := RedactHeader(p.Header, sv)
		names := make([]string, 0, len(h))
		for k := range h {
			names = append(names, k)
		}
		slices.Sort(names)
		for _, k := range names {
			if len(d.ReqHeaders) == cardMaxHeaders {
				break
			}
			d.ReqHeaders = append(d.ReqHeaders, [2]string{k, strings.Join(h[k], ", ")})
		}
	}
	if o.IncludeBody && resp != nil && !resp.Binary && len(resp.Body) > 0 {
		d.BodyHead = bodyHead(red.replace(string(resp.Body)))
	}
	for {
		b := rawJSON(d)
		if len(b) <= MaxCardBytes {
			return b, nil
		}
		switch {
		case len(d.BodyHead) > 0:
			d.BodyHead = d.BodyHead[:len(d.BodyHead)-1]
		case len(d.ReqHeaders) > 0:
			d.ReqHeaders = d.ReqHeaders[:len(d.ReqHeaders)-1]
		default:
			return nil, ErrCardTooLarge
		}
	}
}

// methodOf is the request's method (GET when blank).
func methodOf(r *Request) string {
	if m := strings.TrimSpace(r.Method); m != "" {
		return strings.ToUpper(m)
	}
	return "GET"
}

// expandPlain fills in the non-secret variables of used; secret ones and
// unknown names stay {{name}}.
func expandPlain(tmpl string, used []Value) string {
	plain := map[string]string{}
	for _, u := range used {
		if !u.Secret {
			plain[u.Name] = u.Text
		}
	}
	return placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		if v, ok := plain[name]; ok {
			return v
		}
		return m
	})
}

// bodyHead is the body's first lines, each cut to cardMaxLineRune runes.
func bodyHead(s string) []string {
	s = strings.ToValidUTF8(s, "�")
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > cardMaxLines {
		lines = lines[:cardMaxLines]
	}
	for i, l := range lines {
		if utf8.RuneCountInString(l) > cardMaxLineRune {
			lines[i] = string([]rune(l)[:cardMaxLineRune])
		}
	}
	return lines
}

// ResultPath is the fallback body's path: the sent URL's path, secrets
// masked ("/v1/payments").
func ResultPath(p *Prepared) string {
	if p == nil {
		return ""
	}
	path := p.URL
	if u, err := url.Parse(p.URL); err == nil {
		path = u.EscapedPath()
		if path == "" {
			path = "/"
		}
	}
	return RedactString(path, secretTexts(p))
}

// RunCard is the api.run data of a runner summary (the first three
// failures).
func RunCard(s RunSummary, docID string) (json.RawMessage, error) {
	d := RunData{Collection: s.Collection, Requests: s.Requests, Passed: s.Passed, Failed: s.Failed,
		TimeMS: s.Total.Milliseconds(), Failures: []runFailure{}, Ref: runRef{DocID: docID}}
	for _, f := range s.Failures {
		if len(d.Failures) == cardMaxFailures {
			break
		}
		d.Failures = append(d.Failures, runFailure{Name: f.Name, Method: f.Method, Status: f.Status, Reason: f.Reason})
	}
	b := rawJSON(d)
	if len(b) > MaxCardBytes {
		return nil, ErrCardTooLarge
	}
	return b, nil
}
