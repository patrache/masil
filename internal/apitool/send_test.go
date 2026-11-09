package apitool

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// echo is a handler writing back what it received.
type seen struct {
	Method, Path, RawQuery, Body string
	Header                       http.Header
}

func echoServer(t *testing.T) (*httptest.Server, *[]seen) {
	t.Helper()
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Clone()})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func send(t *testing.T, s *Sender, r *Request, sc *Scope) (*Response, error) {
	t.Helper()
	p, err := Prepare(r, "col1", sc, fixedDyn())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return s.Send(context.Background(), p)
}

func newSender(t *testing.T) *Sender {
	t.Helper()
	return NewSender(t.TempDir(), slog.New(slog.DiscardHandler))
}

func TestSendMethodsHeadersQuery(t *testing.T) {
	srv, got := echoServer(t)
	s := newSender(t)
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "PURGE"} {
		r := &Request{Method: m, URL: srv.URL + "/p?z=0",
			Query:   []KV{{Key: "a", Value: "1"}, {Key: "off", Value: "x", Off: true}, {Key: "b", Value: "x y&"}, {Key: "c", Value: "{{v}}"}},
			Headers: []KV{{Key: "X-One", Value: "1"}, {Key: "X-Off", Value: "no", Off: true}, {Key: "X-Two", Value: "{{v}}"}},
		}
		resp, err := send(t, s, r, &Scope{Collection: []Var{{Name: "v", Value: "3"}}})
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if resp.Status != 200 || resp.StatusText != "OK" {
			t.Errorf("%s: status %d %q", m, resp.Status, resp.StatusText)
		}
	}
	if len(*got) != 8 {
		t.Fatalf("server saw %d requests", len(*got))
	}
	for i, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "PURGE"} {
		g := (*got)[i]
		if g.Method != m || g.Path != "/p" {
			t.Errorf("request %d = %s %s", i, g.Method, g.Path)
		}
		if g.RawQuery != "z=0&a=1&b=x+y%26&c=3" {
			t.Errorf("%s query = %q", m, g.RawQuery)
		}
		if g.Header.Get("X-One") != "1" || g.Header.Get("X-Two") != "3" || g.Header.Get("X-Off") != "" {
			t.Errorf("%s headers = %v", m, g.Header)
		}
	}
	// lower-case method is sent upper-case; empty is GET
	if _, err := send(t, s, &Request{Method: "", URL: srv.URL}, nil); err != nil {
		t.Fatal(err)
	}
	if (*got)[8].Method != "GET" {
		t.Errorf("empty method sent as %s", (*got)[8].Method)
	}
	if _, err := Prepare(&Request{Method: "BAD METHOD", URL: srv.URL}, "", nil, fixedDyn()); err == nil {
		t.Error("method with a space accepted")
	}
}

func TestBodyKinds(t *testing.T) {
	srv, got := echoServer(t)
	s := newSender(t)
	sc := &Scope{Collection: []Var{{Name: "n", Value: "7"}}}
	cases := []struct {
		body   Body
		header []KV
		ct     string
		want   string
	}{
		{Body{Kind: BodyJSON, Text: `{"a":{{n}}}`}, nil, "application/json", `{"a":7}`},
		{Body{Kind: BodyJSON, Text: `{}`, ContentType: "application/vnd.x+json"}, nil, "application/vnd.x+json", `{}`},
		{Body{Kind: BodyJSON, Text: `{}`}, []KV{{Key: "Content-Type", Value: "text/x-mine"}}, "text/x-mine", `{}`},
		{Body{Kind: BodyURLEncoded, Form: []FormField{{KV: KV{Key: "a b", Value: "1&2"}}, {KV: KV{Key: "x", Value: "y", Off: true}}, {KV: KV{Key: "n", Value: "{{n}}"}}}}, nil, "application/x-www-form-urlencoded", "a+b=1%262&n=7"},
		{Body{Kind: BodyRaw, Text: "<x>{{n}}</x>", ContentType: "application/xml"}, nil, "application/xml", "<x>7</x>"},
		{Body{Kind: BodyRaw, Text: "plain"}, nil, "text/plain; charset=utf-8", "plain"},
	}
	for i, c := range cases {
		if _, err := send(t, s, &Request{Method: "POST", URL: srv.URL, Body: c.body, Headers: c.header}, sc); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		g := (*got)[i]
		if g.Header.Get("Content-Type") != c.ct || g.Body != c.want {
			t.Errorf("case %d: ct %q body %q, want %q %q", i, g.Header.Get("Content-Type"), g.Body, c.ct, c.want)
		}
	}

	// multipart: text + file fields
	file := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(file, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	var fname, fdata, text string
	mp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		text = r.FormValue("name")
		f, h, err := r.FormFile("avatar")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		b, _ := io.ReadAll(f)
		fname, fdata = h.Filename, string(b)
	}))
	defer mp.Close()
	r := &Request{Method: "POST", URL: mp.URL, Body: Body{Kind: BodyMultipart, Form: []FormField{
		{KV: KV{Key: "name", Value: "v{{n}}"}},
		{KV: KV{Key: "avatar"}, File: file, FileName: "me.png"},
	}}}
	resp, err := send(t, s, r, sc)
	if err != nil || resp.Status != 200 {
		t.Fatalf("multipart: %v %+v", err, resp)
	}
	if text != "v7" || fname != "me.png" || fdata != "PNGDATA" {
		t.Errorf("multipart got name=%q file=%q %q", text, fname, fdata)
	}
	// a missing file fails at Prepare
	r.Body.Form[1].File = filepath.Join(t.TempDir(), "nope")
	if _, err := Prepare(r, "", sc, fixedDyn()); err == nil {
		t.Error("missing multipart file accepted")
	}
}

func TestAuthHelpers(t *testing.T) {
	srv, got := echoServer(t)
	s := newSender(t)
	sc := &Scope{Collection: []Var{{Name: "u", Value: "user"}, {Name: "p", Value: "pass"}, {Name: "t", Value: "tok"}}}
	for _, a := range []Auth{
		{Kind: AuthBasic, User: "{{u}}", Pass: "{{p}}"},
		{Kind: AuthBearer, Token: "{{t}}"},
		{Kind: AuthAPIKey, Name: "X-Api-Key", Value: "{{t}}"},
		{Kind: AuthAPIKey, Name: "api_key", Value: "{{t}}", In: "query"},
	} {
		if _, err := send(t, s, &Request{Method: "GET", URL: srv.URL + "/a", Auth: a}, sc); err != nil {
			t.Fatal(err)
		}
	}
	g := *got
	if g[0].Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")) {
		t.Errorf("basic = %q", g[0].Header.Get("Authorization"))
	}
	if g[1].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("bearer = %q", g[1].Header.Get("Authorization"))
	}
	if g[2].Header.Get("X-Api-Key") != "tok" {
		t.Errorf("apikey header = %v", g[2].Header)
	}
	if g[3].RawQuery != "api_key=tok" || g[3].Header.Get("Api_key") != "" {
		t.Errorf("apikey query = %q", g[3].RawQuery)
	}
}

func TestTimingPhases(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		fmt.Fprint(w, "hi")
	}))
	defer srv.Close()
	s := newSender(t)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	s.TLS = &tls.Config{RootCAs: pool}
	resp, err := send(t, s, &Request{Method: "GET", URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tm := resp.Timing
	if tm.Connect <= 0 || tm.TLS <= 0 || tm.FirstByte <= 0 || tm.Total < tm.FirstByte {
		t.Errorf("timing = %+v", tm)
	}
	if string(resp.Body) != "hi" {
		t.Errorf("body = %q", resp.Body)
	}
}

func TestTimeoutSetting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	s := newSender(t)
	start := time.Now()
	_, err := send(t, s, &Request{Method: "GET", URL: srv.URL, Settings: Settings{TimeoutMS: 50}}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
	p, err := Prepare(&Request{Method: "GET", URL: srv.URL}, "", nil, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if p.Settings.TimeoutMS != 30000 || p.Settings.Timeout() != 30*time.Second || p.Settings.MaxRedirects != 10 {
		t.Errorf("effective settings = %+v", p.Settings)
	}
}

func TestRedirectLimitAndNoFollow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n int
		fmt.Sscanf(r.URL.Path, "/r/%d", &n)
		if n > 0 {
			http.Redirect(w, r, fmt.Sprintf("/r/%d", n-1), http.StatusFound)
			return
		}
		fmt.Fprint(w, "end")
	}))
	defer srv.Close()
	s := newSender(t)
	resp, err := send(t, s, &Request{Method: "GET", URL: srv.URL + "/r/10"}, nil)
	if err != nil || string(resp.Body) != "end" {
		t.Fatalf("10 redirects: %v %+v", err, resp)
	}
	if _, err := send(t, s, &Request{Method: "GET", URL: srv.URL + "/r/11"}, nil); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("11 redirects err = %v", err)
	}
	if _, err := send(t, s, &Request{Method: "GET", URL: srv.URL + "/r/3", Settings: Settings{MaxRedirects: 2}}, nil); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("MaxRedirects 2 err = %v", err)
	}
	resp, err = send(t, s, &Request{Method: "GET", URL: srv.URL + "/r/1", Settings: Settings{NoFollow: true}}, nil)
	if err != nil || resp.Status != 302 || resp.Header.Get("Location") != "/r/0" {
		t.Fatalf("NoFollow: %v %+v", err, resp)
	}
}

func TestInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	s := newSender(t)
	_, err := send(t, s, &Request{Method: "GET", URL: srv.URL}, nil)
	var uerr x509.UnknownAuthorityError
	var cerr *tls.CertificateVerificationError
	if !errors.As(err, &uerr) && !errors.As(err, &cerr) {
		t.Fatalf("self-signed err = %v", err)
	}
	resp, err := send(t, s, &Request{Method: "GET", URL: srv.URL, Settings: Settings{Insecure: true}}, nil)
	if err != nil || resp.Status != 200 {
		t.Fatalf("insecure: %v %+v", err, resp)
	}
}

func TestLargeBodySpills(t *testing.T) {
	const n = 12 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(bytes.Repeat([]byte("abcdefgh"), n/8))
	}))
	defer srv.Close()
	s := newSender(t)
	resp, err := send(t, s, &Request{Method: "GET", URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Body) != MaxViewBody || !resp.Truncated || resp.Size != n || resp.Spill == "" {
		t.Fatalf("len=%d truncated=%v size=%d spill=%q", len(resp.Body), resp.Truncated, resp.Size, resp.Spill)
	}
	fi, err := os.Stat(resp.Spill)
	if err != nil || fi.Size() != n {
		t.Fatalf("spill = %v %v", fi, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("spill perm = %v", fi.Mode().Perm())
	}
	if filepath.Dir(resp.Spill) != s.SpillDir {
		t.Errorf("spill outside SpillDir: %s", resp.Spill)
	}
}

func TestCancelMidBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()
	s := newSender(t)
	p, err := Prepare(&Request{Method: "GET", URL: srv.URL}, "", nil, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err = s.Send(ctx, p)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("cancel took %v", d)
	}
	ents, _ := os.ReadDir(s.SpillDir)
	if len(ents) != 0 {
		t.Errorf("spill left behind: %v", ents)
	}
}

func TestBinaryAndDecompress(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0xff, 0x00, 0xfe}, 100)...)
	plain := strings.Repeat(`{"k":"value"}`, 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(png)
		case "/gz":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			zw.Write([]byte(plain))
			zw.Close()
		case "/utf8":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("hello world"))
		}
	}))
	defer srv.Close()
	s := newSender(t)
	resp, err := send(t, s, &Request{Method: "GET", URL: srv.URL + "/png"}, nil)
	if err != nil || !resp.Binary || resp.ContentType != "image/png" {
		t.Fatalf("png: %v %+v", err, resp)
	}
	resp, err = send(t, s, &Request{Method: "GET", URL: srv.URL + "/gz"}, nil)
	if err != nil || resp.Binary {
		t.Fatalf("gz: %v binary=%v", err, resp.Binary)
	}
	if string(resp.Body) != plain || resp.Size != int64(len(plain)) || resp.WireSize >= resp.Size || resp.WireSize <= 0 {
		t.Fatalf("gz: size=%d wire=%d body ok=%v", resp.Size, resp.WireSize, string(resp.Body) == plain)
	}
	resp, err = send(t, s, &Request{Method: "GET", URL: srv.URL + "/utf8"}, nil)
	if err != nil || resp.Binary {
		t.Fatalf("utf8 octet-stream: %v binary=%v", err, resp.Binary)
	}
}

func TestCookieJarMemoryOnly(t *testing.T) {
	var cookies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc", Path: "/"})
		}
	}))
	defer srv.Close()
	cfg := t.TempDir()
	s := NewSender(cfg, slog.New(slog.DiscardHandler))
	for _, c := range []struct{ col, path string }{{"A", "/login"}, {"A", "/me"}, {"B", "/me"}} {
		p, err := Prepare(&Request{Method: "GET", URL: srv.URL + c.path}, c.col, nil, fixedDyn())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Send(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	if cookies[1] != "sid=abc" || cookies[2] != "" {
		t.Fatalf("cookies = %q", cookies)
	}
	u := mustURL(t, srv.URL+"/")
	if l := s.Jars.List("A", u); len(l) != 1 || l[0].Name != "sid" {
		t.Errorf("List(A) = %v", l)
	}
	s.Jars.Clear("A")
	if l := s.Jars.List("A", u); len(l) != 0 {
		t.Errorf("after Clear: %v", l)
	}
	ents, _ := os.ReadDir(cfg)
	if len(ents) != 0 {
		t.Errorf("files written: %v", ents)
	}
}

func TestSendLogsNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		fmt.Fprint(w, "resp-body-canary")
	}))
	defer srv.Close()
	var buf bytes.Buffer
	s := NewSender(t.TempDir(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	r := &Request{Method: "POST", URL: srv.URL + "/path-canary?q=query-canary",
		Headers: []KV{{Key: "X-Token", Value: "header-canary"}},
		Body:    Body{Kind: BodyRaw, Text: "body-canary"}}
	if _, err := send(t, s, r, nil); err != nil {
		t.Fatal(err)
	}
	// a failing send logs no URL either
	if _, err := send(t, s, &Request{Method: "GET", URL: "http://127.0.0.1:1/path-canary?q=query-canary"}, nil); err == nil {
		t.Fatal("send to port 1 succeeded")
	} else if strings.Contains(err.Error(), "query-canary") {
		t.Errorf("error carries the URL: %v", err)
	}
	log := buf.String()
	for _, want := range []string{"method=POST", "host=127.0.0.1", "status=201", "ms="} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q: %s", want, log)
		}
	}
	for _, bad := range []string{"path-canary", "query-canary", "header-canary", "body-canary", "resp-body-canary", "X-Token"} {
		if strings.Contains(log, bad) {
			t.Errorf("log contains %q: %s", bad, log)
		}
	}
}

func TestIsBinaryEdge(t *testing.T) {
	// a multi-byte rune cut at the 8KB edge is still text
	body := append(bytes.Repeat([]byte("a"), 8<<10-1), "한글"...)
	if isBinary("application/octet-stream", body) {
		t.Error("text cut at 8KB called binary")
	}
	if !isBinary("application/octet-stream", []byte{0xff, 0xfe, 0x00}) {
		t.Error("invalid UTF-8 not binary")
	}
	if isBinary("text/html", []byte{0xff}) {
		t.Error("text/* called binary")
	}
}
