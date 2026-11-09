package apitool

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Prepared is a request with every variable expanded, ready for Sender.
type Prepared struct {
	RequestID, Method, URL string
	Header                 http.Header
	// Body opens a fresh body (nil = no body); it may be called again for a
	// redirect that keeps the body.
	Body         func() (io.ReadCloser, error)
	Used         []Value  // every variable used (secrets included)
	Settings     Settings // effective (defaults filled in)
	CollectionID string   // cookie jar
	Foreign      bool     // last changed by someone else (shared collection)
	LastBy       string   // who changed it last (display name)
	PrivateOK    bool     // the user allowed a private target for this version

	bodyLen int64 // -1 = unknown
}

// expander expands templates and collects the used values once each.
type expander struct {
	s    *Scope
	d    Dyn
	used []Value
	err  error
}

func (e *expander) x(tmpl string) string {
	if e.err != nil {
		return ""
	}
	out, used, err := Expand(tmpl, e.s, e.d)
	if err != nil {
		e.err = err
		return ""
	}
	for _, u := range used {
		if !containsValue(e.used, u.Name) || u.Source == SourceDynamic {
			e.used = append(e.used, u)
		}
	}
	return out
}

func containsValue(vs []Value, name string) bool {
	for _, v := range vs {
		if v.Name == name {
			return true
		}
	}
	return false
}

// validMethod is an HTTP token (RFC 9110 5.6.2).
func validMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		c := m[i]
		if c <= ' ' || c >= 0x7f || strings.IndexByte(`"(),/:;<=>?@[\]{}`, c) >= 0 {
			return false
		}
	}
	return true
}

// Prepare's errors the screen names (they never carry the URL).
var (
	// ErrBadMethod: the method is not an HTTP token.
	ErrBadMethod = errors.New("apitool: bad method")
	// ErrBadURL: the expanded URL is not http(s) with a host.
	ErrBadURL = errors.New("apitool: bad URL")
	// ErrFormFile: a multipart file field's file cannot be read.
	ErrFormFile = errors.New("apitool: form file not readable")
)

// Prepare expands r in scope s: URL (+ query rows and an apikey in the
// query), header rows, the auth helper and the body. It reads nothing from
// the network; a multipart file must exist.
func Prepare(r *Request, colID string, s *Scope, d Dyn) (*Prepared, error) {
	e := &expander{s: s, d: d}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !validMethod(method) {
		return nil, fmt.Errorf("%w %q", ErrBadMethod, r.Method)
	}

	rawURL := strings.TrimSpace(e.x(r.URL))
	var qs []string
	for _, kv := range r.Query {
		if kv.Off || kv.Key == "" {
			continue
		}
		qs = append(qs, url.QueryEscape(e.x(kv.Key))+"="+url.QueryEscape(e.x(kv.Value)))
	}
	h := http.Header{}
	for _, kv := range r.Headers {
		if kv.Off || strings.TrimSpace(kv.Key) == "" {
			continue
		}
		h.Add(strings.TrimSpace(e.x(kv.Key)), e.x(kv.Value))
	}
	switch r.Auth.Kind {
	case AuthBasic:
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(e.x(r.Auth.User)+":"+e.x(r.Auth.Pass))))
	case AuthBearer:
		h.Set("Authorization", "Bearer "+e.x(r.Auth.Token))
	case AuthAPIKey:
		name, val := e.x(r.Auth.Name), e.x(r.Auth.Value)
		if name != "" {
			if r.Auth.In == "query" {
				qs = append(qs, url.QueryEscape(name)+"="+url.QueryEscape(val))
			} else {
				h.Set(name, val)
			}
		}
	}
	body, n, ct, err := e.body(r.Body)
	if err == nil {
		err = e.err
	}
	if err != nil {
		return nil, err
	}
	if ct != "" && h.Get("Content-Type") == "" {
		h.Set("Content-Type", ct)
	}

	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, ErrBadURL
	}
	if len(qs) > 0 {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += strings.Join(qs, "&")
	}
	return &Prepared{
		RequestID:    r.ID,
		Method:       method,
		URL:          u.String(),
		Header:       h,
		Body:         body,
		Used:         e.used,
		Settings:     r.Settings.Effective(),
		CollectionID: colID,
		bodyLen:      n,
	}, nil
}

func bytesBody(b []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
}

// body builds the body opener, its length (-1 unknown) and default type.
func (e *expander) body(b Body) (func() (io.ReadCloser, error), int64, string, error) {
	switch b.Kind {
	case "", BodyNone:
		return nil, 0, "", nil
	case BodyJSON:
		t := []byte(e.x(b.Text))
		return bytesBody(t), int64(len(t)), or(b.ContentType, "application/json"), nil
	case BodyRaw:
		t := []byte(e.x(b.Text))
		return bytesBody(t), int64(len(t)), or(b.ContentType, "text/plain; charset=utf-8"), nil
	case BodyURLEncoded:
		var parts []string
		for _, f := range b.Form {
			if f.Off || f.Key == "" {
				continue
			}
			parts = append(parts, url.QueryEscape(e.x(f.Key))+"="+url.QueryEscape(e.x(f.Value)))
		}
		t := []byte(strings.Join(parts, "&"))
		return bytesBody(t), int64(len(t)), or(b.ContentType, "application/x-www-form-urlencoded"), nil
	case BodyMultipart:
		return e.multipart(b)
	}
	return nil, 0, "", fmt.Errorf("apitool: unknown body kind %q", b.Kind)
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type mpField struct {
	name, value, file, fileName string
}

// multipart streams text and file fields; files are opened at send time.
func (e *expander) multipart(b Body) (func() (io.ReadCloser, error), int64, string, error) {
	var fields []mpField
	for _, f := range b.Form {
		if f.Off || f.Key == "" {
			continue
		}
		mf := mpField{name: e.x(f.Key)}
		if f.File != "" {
			st, err := os.Stat(f.File)
			if err != nil || !st.Mode().IsRegular() {
				return nil, 0, "", fmt.Errorf("%w: %q", ErrFormFile, f.Key)
			}
			mf.file = f.File
			mf.fileName = or(f.FileName, filepath.Base(f.File))
		} else {
			mf.value = e.x(f.Value)
		}
		fields = append(fields, mf)
	}
	boundary := multipart.NewWriter(io.Discard).Boundary()
	open := func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			mw := multipart.NewWriter(pw)
			_ = mw.SetBoundary(boundary)
			pw.CloseWithError(writeFields(mw, fields))
		}()
		return pr, nil
	}
	return open, -1, "multipart/form-data; boundary=" + boundary, nil
}

func writeFields(mw *multipart.Writer, fields []mpField) error {
	for _, f := range fields {
		if f.file == "" {
			if err := mw.WriteField(f.name, f.value); err != nil {
				return err
			}
			continue
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(f.name), escapeQuotes(f.fileName)))
		h.Set("Content-Type", or(mime.TypeByExtension(filepath.Ext(f.fileName)), "application/octet-stream"))
		w, err := mw.CreatePart(h)
		if err != nil {
			return err
		}
		fh, err := os.Open(f.file)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, fh)
		fh.Close()
		if err != nil {
			return err
		}
	}
	return mw.Close()
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "", "\n", "")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }
