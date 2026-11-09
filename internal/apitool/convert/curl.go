package convert

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ids"
)

var (
	// ErrNotCurl: the pasted text is not a curl command.
	ErrNotCurl = errors.New("convert: not a curl command")
	// ErrShellSyntax: an unterminated quote or a flag without its value.
	ErrShellSyntax = errors.New("convert: bad shell syntax")
	// ErrNoURL: the command has no URL.
	ErrNoURL = errors.New("convert: no URL")
)

const formContentType = "application/x-www-form-urlencoded"

// --- shell tokenizer (POSIX quoting, no expansion) ---

// shellSplit splits s like a POSIX shell without expanding anything:
// single quotes are literal, double quotes unescape only \" \\ \$ \` and a
// backslash-newline; outside quotes a backslash escapes the next character
// and a backslash-newline joins lines. $'…' (ANSI-C quoting, as browsers'
// "copy as cURL" writes) unescapes \n \r \t \\ \' \" \xHH and friends.
func shellSplit(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	in := false
	flush := func() {
		if in {
			out = append(out, cur.String())
			cur.Reset()
			in = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case c == '\\':
			switch {
			case strings.HasPrefix(s[i+1:], "\n"):
				i++
			case strings.HasPrefix(s[i+1:], "\r\n"):
				i += 2
			case i+1 < len(s):
				in = true
				cur.WriteByte(s[i+1])
				i++
			default:
				in = true
				cur.WriteByte('\\')
			}
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("%w: unterminated '", ErrShellSyntax)
			}
			in = true
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
		case c == '"':
			in = true
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					switch s[j+1] {
					case '"', '\\', '$', '`':
						cur.WriteByte(s[j+1])
						j++
						continue
					case '\n':
						j++
						continue
					}
				}
				cur.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, fmt.Errorf("%w: unterminated \"", ErrShellSyntax)
			}
			i = j
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			in = true
			j, err := ansiQuote(s, i+2, &cur)
			if err != nil {
				return nil, err
			}
			i = j
		default:
			in = true
			cur.WriteByte(c)
		}
	}
	flush()
	return out, nil
}

// ansiQuote reads a $'…' body from s[i:] into b; it returns the index of
// the closing quote.
func ansiQuote(s string, i int, b *strings.Builder) (int, error) {
	for ; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			return i, nil
		}
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch e := s[i]; e {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case 'e', 'E':
			b.WriteByte(0x1b)
		case '\\', '\'', '"', '?':
			b.WriteByte(e)
		case 'x':
			n, v := 0, 0
			for n < 2 && i+1 < len(s) && isHex(s[i+1]) {
				v = v*16 + hexVal(s[i+1])
				i++
				n++
			}
			if n == 0 {
				b.WriteString(`\x`)
			} else {
				b.WriteByte(byte(v))
			}
		default:
			b.WriteByte('\\')
			b.WriteByte(e)
		}
	}
	return 0, fmt.Errorf("%w: unterminated $'", ErrShellSyntax)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return int(c-'A') + 10
}

// --- flags ---

// shortLong maps the short flags we read to their long names.
var shortLong = map[byte]string{
	'X': "--request", 'H': "--header", 'd': "--data", 'F': "--form", 'u': "--user",
	'G': "--get", 'k': "--insecure", 'L': "--location", 'b': "--cookie", 'm': "--max-time",
}

// argShort are short flags taking a value (ours and ignored ones).
const argShort = "XHdFubmoAexUTcDwErKYyzCQPt"

// argLong are long flags taking a value.
var argLong = map[string]bool{
	"--request": true, "--header": true, "--data": true, "--data-ascii": true, "--data-raw": true,
	"--data-binary": true, "--data-urlencode": true, "--form": true, "--form-string": true,
	"--user": true, "--url": true, "--cookie": true, "--max-time": true, "--max-redirs": true,
	// ignored, but their value must be skipped
	"--output": true, "--user-agent": true, "--referer": true, "--proxy": true, "--proxy-user": true,
	"--upload-file": true, "--cookie-jar": true, "--dump-header": true, "--write-out": true,
	"--cert": true, "--cert-type": true, "--key": true, "--key-type": true, "--cacert": true,
	"--capath": true, "--connect-timeout": true, "--retry": true, "--retry-delay": true,
	"--retry-max-time": true, "--limit-rate": true, "--range": true, "--continue-at": true,
	"--config": true, "--speed-limit": true, "--speed-time": true, "--time-cond": true,
	"--resolve": true, "--connect-to": true, "--interface": true, "--dns-servers": true,
	"--proxy-header": true, "--json": true, "--variable": true, "--trace": true, "--trace-ascii": true,
	"--stderr": true, "--unix-socket": true, "--abstract-unix-socket": true, "--quote": true,
	"--ciphers": true, "--tls-max": true, "--pinnedpubkey": true, "--request-target": true,
	"--pass": true, "--local-port": true, "--max-filesize": true, "--netrc-file": true,
	"--aws-sigv4": true, "--oauth2-bearer": true, "--preproxy": true, "--socks5": true,
	"--socks5-hostname": true, "--doh-url": true, "--output-dir": true, "--rate": true,
	"--url-query": true, "--ftp-port": true, "--telnet-option": true, "--mail-from": true,
	"--mail-rcpt": true, "--keepalive-time": true, "--happy-eyeballs-timeout-ms": true,
	"--create-file-mode": true, "--parallel-max": true, "--expand-url": true, "--expand-data": true,
	"--expand-header": true, "--hostpubmd5": true, "--service-name": true, "--sasl-authzid": true,
}

type flag struct{ name, value, text string }

// flags turns tokens (after "curl") into flags; positional values have an
// empty name.
func flags(toks []string) ([]flag, error) {
	var out []flag
	rest := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case rest || t == "-" || !strings.HasPrefix(t, "-"):
			out = append(out, flag{value: t, text: t})
		case t == "--":
			rest = true
		case strings.HasPrefix(t, "--"):
			if !argLong[t] {
				out = append(out, flag{name: t, text: t})
				continue
			}
			if i+1 >= len(toks) {
				return nil, fmt.Errorf("%w: %s needs a value", ErrShellSyntax, t)
			}
			i++
			out = append(out, flag{name: t, value: toks[i], text: t + " " + toks[i]})
		default:
			for j := 1; j < len(t); j++ {
				c := t[j]
				short := "-" + string(c)
				name := shortLong[c]
				if name == "" {
					name = short
				}
				if strings.IndexByte(argShort, c) < 0 {
					out = append(out, flag{name: name, text: short})
					continue
				}
				v := t[j+1:]
				if v == "" {
					if i+1 >= len(toks) {
						return nil, fmt.Errorf("%w: %s needs a value", ErrShellSyntax, short)
					}
					i++
					v = toks[i]
				}
				out = append(out, flag{name: name, value: v, text: short + " " + v})
				break
			}
		}
	}
	return out, nil
}

// --- ParseCurl ---

type curlState struct {
	r        apitool.Request
	rep      Report
	url      string
	method   string
	data     []string // pieces as sent (already encoded)
	form     []apitool.FormField
	get      bool
	location bool
	cookies  []string
}

// ParseCurl reads a pasted curl command (C3.9): -X -H -d --data --data-raw
// --data-binary --data-urlencode -F -u -G -k -L --url --compressed -b, plus
// --form-string --max-time --max-redirs. Everything else is listed in
// Report.Ignored; nothing is read from files and no variable is expanded.
// The URL's query becomes query rows when it decodes cleanly.
func ParseCurl(cmd string) (*apitool.Request, Report, error) {
	if err := checkSize(len(cmd)); err != nil {
		return nil, Report{}, err
	}
	toks, err := shellSplit(cmd)
	if err != nil {
		return nil, Report{}, err
	}
	if len(toks) == 0 || (toks[0] != "curl" && path.Base(toks[0]) != "curl" && toks[0] != "curl.exe") {
		return nil, Report{}, ErrNotCurl
	}
	fl, err := flags(toks[1:])
	if err != nil {
		return nil, Report{}, err
	}
	st := &curlState{}
	for _, f := range fl {
		st.apply(f)
	}
	if st.url == "" {
		return nil, st.rep, ErrNoURL
	}
	st.finish()
	r := st.r
	r.ID = ids.New()
	st.rep.Suggestions = suggest(&r)
	return &r, st.rep, nil
}

func (st *curlState) ignore(f flag) { st.rep.Ignored = append(st.rep.Ignored, f.text) }

func (st *curlState) apply(f flag) {
	switch f.name {
	case "":
		if st.url == "" {
			st.url = f.value
		} else {
			st.ignore(f)
		}
	case "--url":
		if st.url == "" {
			st.url = f.value
		} else {
			st.ignore(f)
		}
	case "--request":
		st.method = strings.ToUpper(f.value)
	case "--header":
		st.header(f)
	case "--data", "--data-ascii", "--data-binary":
		if strings.HasPrefix(f.value, "@") {
			st.ignore(f) // from a file: never read
			return
		}
		st.data = append(st.data, f.value)
	case "--data-raw":
		st.data = append(st.data, f.value)
	case "--data-urlencode":
		st.dataURLEncode(f)
	case "--form":
		st.formField(f, false)
	case "--form-string":
		st.formField(f, true)
	case "--user":
		user, pass, _ := strings.Cut(f.value, ":")
		st.r.Auth = apitool.Auth{Kind: apitool.AuthBasic, User: user, Pass: pass}
	case "--get":
		st.get = true
	case "--insecure":
		st.r.Settings.Insecure = true
	case "--location":
		st.location = true
	case "--compressed":
		// accepted: the sender decompresses on its own
	case "--cookie":
		if !strings.Contains(f.value, "=") {
			st.ignore(f) // a cookie file
			return
		}
		st.cookies = append(st.cookies, f.value)
	case "--max-time":
		sec, err := strconv.ParseFloat(f.value, 64)
		if err != nil || sec <= 0 || sec > 1e6 {
			st.ignore(f)
			return
		}
		st.r.Settings.TimeoutMS = int(sec*1000 + 0.5)
	case "--max-redirs":
		n, err := strconv.Atoi(f.value)
		if err != nil || n <= 0 || n > 1000 {
			st.ignore(f)
			return
		}
		st.r.Settings.MaxRedirects = n
	default:
		st.ignore(f)
	}
}

func (st *curlState) header(f flag) {
	v := f.value
	if strings.HasPrefix(v, "@") {
		st.ignore(f)
		return
	}
	if name, val, ok := strings.Cut(v, ":"); ok && strings.TrimSpace(name) != "" {
		val = strings.Trim(val, " \t")
		if val == "" {
			st.ignore(f) // "Name:" removes a curl default header
			return
		}
		st.r.Headers = append(st.r.Headers, apitool.KV{Key: strings.TrimSpace(name), Value: val})
		return
	}
	if name, ok := strings.CutSuffix(strings.TrimSpace(v), ";"); ok && name != "" && !strings.ContainsAny(name, ":;") {
		st.r.Headers = append(st.r.Headers, apitool.KV{Key: name})
		return
	}
	st.ignore(f)
}

// dataURLEncode follows curl: "content", "=content", "name=content" are
// encoded; "@file" and "name@file" read a file and are ignored.
func (st *curlState) dataURLEncode(f flag) {
	v := f.value
	if eq := strings.IndexByte(v, '='); eq >= 0 {
		name := v[:eq]
		piece := url.QueryEscape(v[eq+1:])
		if name != "" {
			piece = name + "=" + piece
		}
		st.data = append(st.data, piece)
		return
	}
	if strings.Contains(v, "@") {
		st.ignore(f)
		return
	}
	st.data = append(st.data, url.QueryEscape(v))
}

// formParams are the -F parameters after ';' curl understands.
var formParams = []string{"type=", "filename=", "headers=", "encoder="}

func (st *curlState) formField(f flag, literal bool) {
	name, val, ok := strings.Cut(f.value, "=")
	if !ok || name == "" {
		st.ignore(f)
		return
	}
	if literal {
		st.form = append(st.form, apitool.FormField{KV: apitool.KV{Key: name, Value: val}})
		return
	}
	if strings.HasPrefix(val, "<") {
		st.ignore(f) // content from a file
		return
	}
	file := strings.HasPrefix(val, "@")
	if file {
		val = val[1:]
	}
	// Split off ";type=…" style parameters.
	var params []string
	for {
		i := -1
		for _, p := range formParams {
			if j := strings.LastIndex(val, ";"+p); j > i {
				i = j
			}
		}
		if i < 0 {
			break
		}
		params = append(params, val[i+1:])
		val = val[:i]
	}
	ff := apitool.FormField{KV: apitool.KV{Key: name}}
	if !file {
		ff.Value = val
	} else {
		ff.File = val
		ff.FileName = path.Base(strings.ReplaceAll(val, `\`, "/"))
	}
	for _, p := range params {
		if fn, ok := strings.CutPrefix(p, "filename="); ok && file {
			ff.FileName = strings.Trim(fn, `"`)
		}
	}
	st.form = append(st.form, ff)
}

// finish builds the request from what the flags collected.
func (st *curlState) finish() {
	r := &st.r
	r.Settings.NoFollow = !st.location
	if len(st.cookies) > 0 {
		r.Headers = append(r.Headers, apitool.KV{Key: "Cookie", Value: strings.Join(st.cookies, "; ")})
	}
	u := st.url
	hasBody := false
	switch {
	case len(st.form) > 0:
		hasBody = true
		r.Body = apitool.Body{Kind: apitool.BodyMultipart, Form: st.form}
		for _, d := range st.data {
			st.rep.Ignored = append(st.rep.Ignored, "-d "+d)
		}
	case st.get && len(st.data) > 0:
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + strings.Join(st.data, "&")
	case len(st.data) > 0:
		hasBody = true
		r.Body = st.dataBody()
	}
	r.URL, r.Query = splitQuery(u)
	switch {
	case st.method != "":
		r.Method = st.method
	case hasBody:
		r.Method = "POST"
	default:
		r.Method = "GET"
	}
}

func (st *curlState) contentType() (int, string) {
	for i, h := range st.r.Headers {
		if strings.EqualFold(h.Key, "Content-Type") {
			return i, strings.TrimSpace(h.Value)
		}
	}
	return -1, ""
}

func (st *curlState) dataBody() apitool.Body {
	joined := strings.Join(st.data, "&")
	ctIdx, ct := st.contentType()
	t := strings.TrimSpace(joined)
	if len(st.data) == 1 && t != "" && (t[0] == '{' || t[0] == '[') && json.Valid([]byte(t)) {
		// The json kind sets the type; a plain matching header is redundant.
		if ctIdx >= 0 && strings.EqualFold(ct, "application/json") {
			st.r.Headers = append(st.r.Headers[:ctIdx], st.r.Headers[ctIdx+1:]...)
			if len(st.r.Headers) == 0 {
				st.r.Headers = nil
			}
		}
		return apitool.Body{Kind: apitool.BodyJSON, Text: joined}
	}
	if form, ok := decodeForm(joined); ok && (ctIdx < 0 || strings.EqualFold(ct, formContentType)) {
		return apitool.Body{Kind: apitool.BodyURLEncoded, Form: form}
	}
	b := apitool.Body{Kind: apitool.BodyRaw, Text: joined}
	if ctIdx < 0 {
		b.ContentType = formContentType // what curl sends
	}
	return b
}

// decodeForm decodes "a=1&b=2" when every piece is name=value and decodes.
func decodeForm(s string) ([]apitool.FormField, bool) {
	if s == "" {
		return nil, false
	}
	var out []apitool.FormField
	for _, p := range strings.Split(s, "&") {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, false
		}
		dk, err1 := url.QueryUnescape(k)
		dv, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil {
			return nil, false
		}
		out = append(out, apitool.FormField{KV: apitool.KV{Key: dk, Value: dv}})
	}
	return out, true
}

// splitQuery moves a cleanly decoding query into rows; otherwise (bad
// escapes, a fragment, a piece without '=') the URL is kept whole.
func splitQuery(u string) (string, []apitool.KV) {
	base, q, ok := strings.Cut(u, "?")
	if !ok || q == "" || strings.Contains(q, "#") {
		return u, nil
	}
	form, ok := decodeForm(q)
	if !ok {
		return u, nil
	}
	rows := make([]apitool.KV, len(form))
	for i, f := range form {
		rows[i] = f.KV
	}
	return base, rows
}

// --- ToCurl ---

// CurlMode chooses how ToCurl treats variables.
type CurlMode int

const (
	// CurlTemplate keeps every {{variable}} as is (default).
	CurlTemplate CurlMode = iota
	// CurlNonSecret expands non-secret and dynamic variables; secret ones
	// stay {{…}} (a secret value is never written).
	CurlNonSecret
)

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.$-]+)\s*\}\}`)

// ToCurl writes r as one curl command. Exports hide typed-in credentials
// (sensitive header, query and form values, the auth password/token/key)
// and the value of every secret variable s can reach, wherever it appears,
// as apitool.Mask; {{placeholders}} stay.
func ToCurl(r *apitool.Request, s *apitool.Scope, mode CurlMode, d apitool.Dyn) (string, error) {
	src := *r
	if mode == CurlNonSecret {
		src = expandNonSecret(r, s, d)
	}
	red := apitool.RedactRequest(&src, scopeSecretValues(s))
	return curlCommand(&red), nil
}

// scopeSecretValues are the vault values of the secret variables declared
// in s (env, collection, globals); one that cannot be read is skipped.
func scopeSecretValues(s *apitool.Scope) []string {
	if s == nil || s.Vault == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	collect := func(vs []apitool.Var) {
		for _, v := range vs {
			if v.Secret && !seen[v.Name] {
				seen[v.Name] = true
				names = append(names, v.Name)
			}
		}
	}
	if s.Env != nil {
		collect(s.Env.Vars)
	}
	collect(s.Collection)
	collect(s.Globals)
	var out []string
	for _, n := range names {
		if v, err := s.Lookup(n); err == nil && v.Secret && v.Text != "" {
			out = append(out, v.Text)
		}
	}
	return out
}

func expandNonSecret(r *apitool.Request, s *apitool.Scope, d apitool.Dyn) apitool.Request {
	if s == nil {
		s = &apitool.Scope{}
	}
	x := func(t string) string {
		return placeholderRe.ReplaceAllStringFunc(t, func(m string) string {
			name := placeholderRe.FindStringSubmatch(m)[1]
			if _, _, secret := s.SecretSlot(name); secret && !strings.HasPrefix(name, "$") {
				return m
			}
			v, _, err := apitool.Expand("{{"+name+"}}", s, d)
			if err != nil {
				return m
			}
			return v
		})
	}
	out := *r
	out.URL = x(r.URL)
	out.Query = mapKV(r.Query, x)
	out.Headers = mapKV(r.Headers, x)
	out.Body.Text = x(r.Body.Text)
	out.Body.Form = make([]apitool.FormField, len(r.Body.Form))
	for i, f := range r.Body.Form {
		f.Key, f.Value = x(f.Key), x(f.Value)
		out.Body.Form[i] = f
	}
	if r.Body.Form == nil {
		out.Body.Form = nil
	}
	a := &out.Auth
	a.User, a.Pass, a.Token, a.Name, a.Value = x(a.User), x(a.Pass), x(a.Token), x(a.Name), x(a.Value)
	return out
}

func mapKV(kvs []apitool.KV, x func(string) string) []apitool.KV {
	if kvs == nil {
		return nil
	}
	out := make([]apitool.KV, len(kvs))
	for i, kv := range kvs {
		kv.Key, kv.Value = x(kv.Key), x(kv.Value)
		out[i] = kv
	}
	return out
}

func curlCommand(r *apitool.Request) string {
	var args []string
	add := func(a ...string) { args = append(args, a...) }

	var body []string
	switch r.Body.Kind {
	case apitool.BodyJSON, apitool.BodyRaw:
		body = append(body, "--data-raw", r.Body.Text)
	case apitool.BodyURLEncoded:
		for _, f := range r.Body.Form {
			if !f.Off && f.Key != "" {
				body = append(body, "-d", exportEscape(f.Key)+"="+exportEscape(f.Value))
			}
		}
	case apitool.BodyMultipart:
		for _, f := range r.Body.Form {
			if f.Off || f.Key == "" {
				continue
			}
			if f.File != "" || f.FileName != "" {
				p := f.File
				if p == "" {
					p = f.FileName
				}
				v := f.Key + "=@" + p
				if f.FileName != "" && f.FileName != path.Base(strings.ReplaceAll(p, `\`, "/")) {
					v += ";filename=" + f.FileName
				}
				body = append(body, "-F", v)
			} else if strings.HasPrefix(f.Value, "@") || strings.HasPrefix(f.Value, "<") || strings.Contains(f.Value, ";") {
				body = append(body, "--form-string", f.Key+"="+f.Value)
			} else {
				body = append(body, "-F", f.Key+"="+f.Value)
			}
		}
	}

	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = "GET"
	}
	implied := "GET"
	if len(body) > 0 {
		implied = "POST"
	}
	if method != implied {
		add("-X", method)
	}

	u := r.URL
	var qs []string
	for _, kv := range r.Query {
		if !kv.Off && kv.Key != "" {
			qs = append(qs, exportEscape(kv.Key)+"="+exportEscape(kv.Value))
		}
	}
	if r.Auth.Kind == apitool.AuthAPIKey && r.Auth.In == "query" && r.Auth.Name != "" {
		qs = append(qs, exportEscape(r.Auth.Name)+"="+exportEscape(r.Auth.Value))
	}
	if len(qs) > 0 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + strings.Join(qs, "&")
	}
	if strings.HasPrefix(u, "-") {
		add("--url", u) // curl would read a bare "-o…" as an option
	} else {
		add(u)
	}

	hasCT := false
	for _, h := range r.Headers {
		if h.Off || strings.TrimSpace(h.Key) == "" {
			continue
		}
		if strings.EqualFold(h.Key, "Content-Type") {
			hasCT = true
		}
		if h.Value == "" {
			add("-H", h.Key+";")
		} else {
			add("-H", h.Key+": "+h.Value)
		}
	}
	switch r.Auth.Kind {
	case apitool.AuthBasic:
		add("-u", r.Auth.User+":"+r.Auth.Pass)
	case apitool.AuthBearer:
		add("-H", "Authorization: Bearer "+r.Auth.Token)
	case apitool.AuthAPIKey:
		if r.Auth.In != "query" && r.Auth.Name != "" {
			add("-H", r.Auth.Name+": "+r.Auth.Value)
		}
	}
	if !hasCT {
		switch {
		case r.Body.Kind == apitool.BodyJSON:
			add("-H", "Content-Type: application/json")
		case r.Body.Kind == apitool.BodyRaw && r.Body.ContentType != "" && !strings.EqualFold(r.Body.ContentType, formContentType):
			add("-H", "Content-Type: "+r.Body.ContentType)
		case r.Body.Kind == apitool.BodyRaw && r.Body.ContentType == "":
			add("-H", "Content-Type: text/plain; charset=utf-8")
		}
	}
	add(body...)
	set := r.Settings
	if set.Insecure {
		add("-k")
	}
	if !set.NoFollow {
		add("-L")
		if set.MaxRedirects > 0 {
			add("--max-redirs", strconv.Itoa(set.MaxRedirects))
		}
	}
	if set.TimeoutMS > 0 {
		add("--max-time", strconv.FormatFloat(float64(set.TimeoutMS)/1000, 'f', -1, 64))
	}

	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shellQuote(a)
	}
	return "curl " + strings.Join(q, " ")
}

// exportEscape percent-encodes a query or form part, keeping {{…}} and
// other harmless characters readable; '+' and '%' are always encoded so
// decoding gives the text back.
func exportEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.IndexByte("-._~{}$!*(),:/'", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// shellQuote quotes a for a POSIX shell: bare when it is only safe
// characters, else in single quotes where each quote inside is closed,
// backslash-escaped and reopened.
func shellQuote(a string) string {
	if a != "" {
		safe := true
		for i := 0; i < len(a); i++ {
			c := a[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_./:=,@%+", c) >= 0) {
				safe = false
				break
			}
		}
		if safe {
			return a
		}
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}
