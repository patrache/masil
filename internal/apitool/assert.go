package apitool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/textproto"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/patrache/masil/internal/secrets"
)

// Assertion failure reasons (AssertResult.Err): stable codes; the screen
// turns them into catalog text.
const (
	ErrBodyTruncated = "body truncated"
	ErrNotJSON       = "not json"
	ErrBadRegex      = "bad regex"
	ErrRegexTooLong  = "regex too long"
	ErrPathSyntax    = "bad path"
	ErrUnknownOp     = "unknown op"
	ErrUnknownTarget = "unknown target"
	ErrBadValue      = "bad value"
	ErrMissing       = "missing"
)

// MaxRegex is the longest pattern accepted (RE2, never backtracks).
const MaxRegex = 1000

// AssertResult is the outcome of one assertion; Got is the actual value as
// text, Err a reason code when it could not be evaluated.
type AssertResult struct {
	ID   string
	Pass bool
	Got  string
	Err  string
}

// respDoc decodes the body as JSON once.
type respDoc struct {
	r    *Response
	done bool
	doc  any
	err  string
}

func (d *respDoc) get() (any, string) {
	if d.done {
		return d.doc, d.err
	}
	d.done = true
	if d.r.Truncated {
		d.err = ErrBodyTruncated
		return nil, d.err
	}
	dec := json.NewDecoder(bytes.NewReader(d.r.Body))
	dec.UseNumber()
	if err := dec.Decode(&d.doc); err != nil || dec.More() {
		d.doc, d.err = nil, ErrNotJSON
	}
	return d.doc, d.err
}

// Check evaluates the assertions against r (C3.7 table): status eq|in,
// time lt|gt, size lt|gt, header exists|eq|contains|regex, json
// exists|eq|type|contains|regex|len, body contains|regex.
func Check(as []Assertion, r *Response) []AssertResult {
	d := &respDoc{r: r}
	out := make([]AssertResult, 0, len(as))
	for _, a := range as {
		res := checkOne(a, r, d)
		res.ID = a.ID
		res.Got = clip(res.Got, 256)
		out = append(out, res)
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func fail(code string) AssertResult { return AssertResult{Err: code} }

func checkOne(a Assertion, r *Response, d *respDoc) AssertResult {
	switch a.Target {
	case "status":
		got := strconv.Itoa(r.Status)
		switch a.Op {
		case "eq":
			n, err := strconv.Atoi(strings.TrimSpace(a.Value))
			if err != nil {
				return AssertResult{Got: got, Err: ErrBadValue}
			}
			return AssertResult{Pass: r.Status == n, Got: got}
		case "in":
			ok, err := statusIn(r.Status, a.Value)
			if err != nil {
				return AssertResult{Got: got, Err: ErrBadValue}
			}
			return AssertResult{Pass: ok, Got: got}
		}
		return AssertResult{Got: got, Err: ErrUnknownOp}
	case "time":
		got := strconv.FormatInt(r.Timing.Total.Milliseconds(), 10) + "ms"
		lim, err := parseDurationValue(a.Value)
		if err != nil {
			return AssertResult{Got: got, Err: ErrBadValue}
		}
		return compare(a.Op, r.Timing.Total < lim, r.Timing.Total > lim, got)
	case "size":
		got := strconv.FormatInt(r.Size, 10) + "B"
		lim, err := parseSize(a.Value)
		if err != nil {
			return AssertResult{Got: got, Err: ErrBadValue}
		}
		return compare(a.Op, r.Size < lim, r.Size > lim, got)
	case "header":
		vals := r.Header.Values(textproto.CanonicalMIMEHeaderKey(a.Path))
		if len(vals) == 0 {
			vals = r.Header.Values(a.Path)
		}
		got := strings.Join(vals, ", ")
		switch a.Op {
		case "exists":
			return AssertResult{Pass: len(vals) > 0, Got: got}
		case "eq":
			return AssertResult{Pass: len(vals) > 0 && slices.Contains(vals, a.Value), Got: got}
		case "contains":
			return AssertResult{Pass: len(vals) > 0 && strings.Contains(got, a.Value), Got: got}
		case "regex":
			re, code := compileRegex(a.Value)
			if re == nil {
				return AssertResult{Got: got, Err: code}
			}
			return AssertResult{Pass: len(vals) > 0 && re.MatchString(got), Got: got}
		}
		return AssertResult{Got: got, Err: ErrUnknownOp}
	case "json":
		return checkJSON(a, d)
	case "body":
		body := string(r.Body)
		var hit bool
		switch a.Op {
		case "contains":
			hit = strings.Contains(body, a.Value)
		case "regex":
			re, code := compileRegex(a.Value)
			if re == nil {
				return fail(code)
			}
			hit = re.MatchString(body)
		default:
			return fail(ErrUnknownOp)
		}
		if !hit && r.Truncated {
			return fail(ErrBodyTruncated)
		}
		return AssertResult{Pass: hit}
	}
	return fail(ErrUnknownTarget)
}

func compare(op string, lt, gt bool, got string) AssertResult {
	switch op {
	case "lt":
		return AssertResult{Pass: lt, Got: got}
	case "gt":
		return AssertResult{Pass: gt, Got: got}
	}
	return AssertResult{Got: got, Err: ErrUnknownOp}
}

// statusIn: a comma list of codes ("201") and classes ("2xx").
func statusIn(st int, list string) (bool, error) {
	seen := false
	for _, part := range strings.Split(list, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" {
			continue
		}
		seen = true
		if len(p) == 3 && strings.HasSuffix(p, "xx") && p[0] >= '1' && p[0] <= '5' {
			if st/100 == int(p[0]-'0') {
				return true, nil
			}
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return false, err
		}
		if st == n {
			return true, nil
		}
	}
	if !seen {
		return false, errors.New("empty")
	}
	return false, nil
}

// parseDurationValue: "500ms", "1.5s", or a bare number of milliseconds.
func parseDurationValue(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return time.Duration(f * float64(time.Millisecond)), nil
	}
	if !strings.HasSuffix(v, "ms") && !strings.HasSuffix(v, "s") {
		return 0, errors.New("unit")
	}
	return time.ParseDuration(v)
}

// parseSize: "1MB", "512KB", "100B" (1024 multiples) or a bare byte count.
func parseSize(v string) (int64, error) {
	v = strings.ToUpper(strings.TrimSpace(v))
	mult := 1.0
	for _, u := range []struct {
		s string
		m float64
	}{{"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		if num, ok := strings.CutSuffix(v, u.s); ok {
			v, mult = strings.TrimSpace(num), u.m
			break
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, errors.New("size")
	}
	return int64(f * mult), nil
}

func compileRegex(p string) (*regexp.Regexp, string) {
	if len(p) > MaxRegex {
		return nil, ErrRegexTooLong
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, ErrBadRegex
	}
	return re, ""
}

func checkJSON(a Assertion, d *respDoc) AssertResult {
	if _, err := parsePath(a.Path); err != nil {
		return fail(ErrPathSyntax)
	}
	doc, code := d.get()
	if code != "" {
		return fail(code)
	}
	res, _ := EvalPath(doc, a.Path)
	if a.Op == "exists" {
		return AssertResult{Pass: len(res) > 0, Got: strconv.Itoa(len(res))}
	}
	var v any
	switch {
	case pathHasWildcard(a.Path):
		v = res
	case len(res) == 0:
		if isJSONOp(a.Op) {
			return AssertResult{Err: ErrMissing}
		}
		return fail(ErrUnknownOp)
	default:
		v = res[0]
	}
	got := jsonText(v)
	switch a.Op {
	case "eq":
		return AssertResult{Pass: jsonEq(v, a.Value), Got: got}
	case "type":
		return AssertResult{Pass: jsonType(v) == strings.TrimSpace(a.Value), Got: jsonType(v)}
	case "contains":
		switch t := v.(type) {
		case []any:
			return AssertResult{Pass: slices.ContainsFunc(t, func(e any) bool { return jsonEq(e, a.Value) }), Got: got}
		case map[string]any:
			_, ok := t[a.Value]
			return AssertResult{Pass: ok, Got: got}
		}
		return AssertResult{Pass: strings.Contains(got, a.Value), Got: got}
	case "regex":
		re, code := compileRegex(a.Value)
		if re == nil {
			return AssertResult{Got: got, Err: code}
		}
		return AssertResult{Pass: re.MatchString(got), Got: got}
	case "len":
		want, err := strconv.Atoi(strings.TrimSpace(a.Value))
		if err != nil {
			return AssertResult{Got: got, Err: ErrBadValue}
		}
		n := -1
		switch t := v.(type) {
		case []any:
			n = len(t)
		case map[string]any:
			n = len(t)
		case string:
			n = utf8.RuneCountInString(t)
		}
		return AssertResult{Pass: n == want, Got: strconv.Itoa(n)}
	}
	return AssertResult{Got: got, Err: ErrUnknownOp}
}

func isJSONOp(op string) bool {
	switch op {
	case "eq", "type", "contains", "regex", "len":
		return true
	}
	return false
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// jsonText is a value as text: strings raw, the rest compact JSON.
func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// jsonEq compares a JSON value with the assertion's text: strings as text
// (or a quoted JSON string), numbers numerically, true/false/null literally,
// arrays and objects as JSON.
func jsonEq(v any, want string) bool {
	switch t := v.(type) {
	case string:
		if t == want {
			return true
		}
		var s string
		return json.Unmarshal([]byte(want), &s) == nil && s == t
	case json.Number:
		a, ok1 := new(big.Rat).SetString(t.String())
		b, ok2 := new(big.Rat).SetString(strings.TrimSpace(want))
		return ok1 && ok2 && a.Cmp(b) == 0
	case bool:
		return strconv.FormatBool(t) == strings.TrimSpace(want)
	case nil:
		return strings.TrimSpace(want) == "null"
	}
	dec := json.NewDecoder(strings.NewReader(want))
	dec.UseNumber()
	var w any
	if dec.Decode(&w) != nil {
		return false
	}
	a, err1 := canonicalJSON(v)
	b, err2 := canonicalJSON(w)
	return err1 == nil && err2 == nil && bytes.Equal(a, b)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ExtractError is an extraction that found no value.
type ExtractError struct {
	ID, Var, Reason string
}

func (e *ExtractError) Error() string {
	return "apitool: extract {{" + e.Var + "}}: " + e.Reason
}

// ExtractAll reads every extraction from r: From "json" (JSON path; strings
// raw, others compact JSON) or "header" (name). Misses are errors.
func ExtractAll(ex []Extract, r *Response) (map[string]string, []error) {
	d := &respDoc{r: r}
	out := map[string]string{}
	var errs []error
	for _, e := range ex {
		if strings.TrimSpace(e.Var) == "" {
			errs = append(errs, &ExtractError{ID: e.ID, Var: e.Var, Reason: ErrBadValue})
			continue
		}
		switch e.From {
		case "json":
			if _, err := parsePath(e.Path); err != nil {
				errs = append(errs, &ExtractError{ID: e.ID, Var: e.Var, Reason: ErrPathSyntax})
				continue
			}
			doc, code := d.get()
			if code != "" {
				errs = append(errs, &ExtractError{ID: e.ID, Var: e.Var, Reason: code})
				continue
			}
			res, _ := EvalPath(doc, e.Path)
			if len(res) == 0 {
				errs = append(errs, &ExtractError{ID: e.ID, Var: e.Var, Reason: ErrMissing})
				continue
			}
			v := res[0]
			if pathHasWildcard(e.Path) {
				v = res
			}
			out[e.Var] = jsonText(v)
		case "header":
			v := r.Header.Get(e.Path)
			if v == "" && len(r.Header.Values(e.Path)) == 0 {
				errs = append(errs, &ExtractError{ID: e.ID, Var: e.Var, Reason: ErrMissing})
				continue
			}
			out[e.Var] = v
		default:
			errs = append(errs, &ExtractError{ID: e.ID, Var: e.Var, Reason: ErrUnknownTarget})
		}
	}
	return out, errs
}

// ApplyExtracts stores extracted values: a secret variable goes to the Vault
// (never to a file or document); a shared collection keeps plain values as my
// value (Mine, never synced); otherwise the environment, or my globals
// without one. changed lists the names written (sorted). A Vault error such
// as secrets.ErrNeedConsent stops it.
func ApplyExtracts(vals map[string]string, s *Scope) (changed []string, err error) {
	names := make([]string, 0, len(vals))
	for n := range vals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		v := vals[name]
		if key, _, secret := s.SecretSlot(name); secret {
			if s.Vault == nil {
				return changed, fmt.Errorf("apitool: no vault for {{%s}}", name)
			}
			if err := s.Vault.Put(key, secrets.New(v)); err != nil {
				return changed, err
			}
			changed = append(changed, name)
			continue
		}
		if s.DocID != "" {
			if s.Mine == nil {
				s.Mine = &Mine{}
			}
			if s.Mine.Values == nil {
				s.Mine.Values = map[string]string{}
			}
			slot := ""
			if s.Env != nil {
				slot = envSlot(s.Env)
			}
			if old, ok := s.Mine.Values[slot+"/"+name]; !ok || old != v {
				s.Mine.Values[slot+"/"+name] = v
				changed = append(changed, name)
			}
			continue
		}
		target := &s.Globals
		if s.Env != nil {
			target = &s.Env.Vars
		}
		if setVar(target, name, v) {
			changed = append(changed, name)
		}
	}
	return changed, nil
}

func setVar(vs *[]Var, name, value string) bool {
	for i := range *vs {
		if (*vs)[i].Name == name {
			if (*vs)[i].Value == value {
				return false
			}
			(*vs)[i].Value = value
			return true
		}
	}
	*vs = append(*vs, Var{Name: name, Value: value})
	return true
}
