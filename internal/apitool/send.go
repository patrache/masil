package apitool

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Body limits (C3.7).
const (
	MaxViewBody = 10 << 20 // bytes kept in memory for the screen
	SpillMax    = 1 << 30  // the whole body on disk (spill file) at most
)

var (
	// ErrTooManyRedirects: more redirects than Settings.MaxRedirects.
	ErrTooManyRedirects = errors.New("apitool: too many redirects")
	// ErrTooLarge: the response body is over SpillMax.
	ErrTooLarge = errors.New("apitool: response too large")
)

// Timing is the phase timing of one exchange (httptrace). With redirects the
// DNS/Connect/TLS phases add up; FirstByte is from the start to the final
// response's first byte, Total to the end of the body.
type Timing struct {
	DNS, Connect, TLS, FirstByte, Total time.Duration
}

// Response is what came back. Body holds at most MaxViewBody bytes
// (decompressed); a longer body is Truncated and wholly in the Spill file
// (0600, the caller removes it). Size is the decompressed size, WireSize the
// bytes received.
type Response struct {
	Status                         int
	StatusText, Proto, ContentType string
	Header                         http.Header
	Body                           []byte
	Size, WireSize                 int64
	Truncated                      bool
	Spill                          string
	Binary                         bool
	Timing                         Timing
}

// Sender is the only HTTP client to target servers (requests leave from this
// PC only; it never shares anything with the masil API client).
type Sender struct {
	Jars     *Jars
	Log      *slog.Logger
	SpillDir string
	TLS      *tls.Config // base TLS config (tests: RootCAs)
	// Resolver resolves a host name for the dial-time guard (nil = system).
	Resolver func(ctx context.Context, host string) ([]netip.Addr, error)
}

// NewSender is a sender spilling big bodies into spillDir ("" = os.TempDir)
// and logging to log (nil = slog.Default).
func NewSender(spillDir string, log *slog.Logger) *Sender {
	return &Sender{Jars: NewJars(), Log: log, SpillDir: spillDir}
}

func (s *Sender) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Send performs p. Errors never carry the URL (a secret may be in the query);
// a cancel or timeout is ctx's error. The log line holds the method, host,
// status and time only (C3.10 layer 5).
func (s *Sender) Send(ctx context.Context, p *Prepared) (*Response, error) {
	start := time.Now()
	resp, err := s.send(ctx, p, start)
	host := ""
	if u, perr := url.Parse(p.URL); perr == nil {
		host = u.Hostname()
	}
	status := 0
	if resp != nil {
		status = resp.Status
	}
	s.logger().Info("api send", "method", p.Method, "host", host, "status", status, "ms", time.Since(start).Milliseconds())
	return resp, err
}

func (s *Sender) send(parent context.Context, p *Prepared, start time.Time) (*Response, error) {
	set := p.Settings.Effective()
	ctx, cancel := context.WithTimeout(parent, set.Timeout())
	defer cancel()

	var body io.ReadCloser
	if p.Body != nil {
		b, err := p.Body()
		if err != nil {
			return nil, fmt.Errorf("apitool: body: %w", err)
		}
		body = b
	}
	req, err := http.NewRequestWithContext(ctx, p.Method, p.URL, body)
	if err != nil {
		if body != nil {
			body.Close()
		}
		return nil, errors.New("apitool: bad request")
	}
	if p.Body != nil {
		req.GetBody = p.Body
		req.ContentLength = p.bodyLen
		if p.bodyLen == 0 {
			req.Body = http.NoBody
		}
	}
	req.Header = p.Header.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	if h := req.Header.Get("Host"); h != "" {
		req.Host = h
		req.Header.Del("Host")
	}

	var tm Timing
	var dnsAt, connAt, tlsAt time.Time
	trace := &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { dnsAt = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { tm.DNS += time.Since(dnsAt) },
		ConnectStart:         func(string, string) { connAt = time.Now() },
		ConnectDone:          func(string, string, error) { tm.Connect += time.Since(connAt) },
		TLSHandshakeStart:    func() { tlsAt = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { tm.TLS += time.Since(tlsAt) },
		GotFirstResponseByte: func() { tm.FirstByte = time.Since(start) },
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))

	client := s.client(p, set)
	defer client.CloseIdleConnections()
	hr, err := client.Do(req)
	if err != nil {
		return nil, s.cleanErr(ctx, err)
	}
	defer hr.Body.Close()

	r := &Response{
		Status:      hr.StatusCode,
		StatusText:  strings.TrimSpace(strings.TrimPrefix(hr.Status, strconv.Itoa(hr.StatusCode))),
		Proto:       hr.Proto,
		ContentType: hr.Header.Get("Content-Type"),
		Header:      hr.Header,
	}
	if err := s.readBody(ctx, hr, r); err != nil {
		return nil, err
	}
	tm.Total = time.Since(start)
	if tm.FirstByte == 0 || tm.FirstByte > tm.Total {
		tm.FirstByte = tm.Total
	}
	r.Timing = tm
	r.Binary = isBinary(r.ContentType, r.Body)
	return r, nil
}

// client builds a client for one send: its own transport (no shared pool, no
// automatic decompression, proxy from the environment) and redirect policy.
func (s *Sender) client(p *Prepared, set Settings) *http.Client {
	tc := &tls.Config{}
	if s.TLS != nil {
		tc = s.TLS.Clone()
	}
	if set.Insecure {
		tc.InsecureSkipVerify = true
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DisableCompression:  true,
		TLSClientConfig:     tc,
		DialContext:         s.dialContext(p),
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: set.Timeout(),
		MaxIdleConns:        1,
	}
	var jar http.CookieJar
	if s.Jars != nil {
		jar = s.Jars.For(p.CollectionID)
	}
	return &http.Client{
		Transport: tr,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if set.NoFollow {
				return http.ErrUseLastResponse
			}
			if len(via) > set.MaxRedirects {
				return ErrTooManyRedirects
			}
			return nil
		},
	}
}

// dialContext is the plain dialer.
func (s *Sender) dialContext(p *Prepared) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{}
	return d.DialContext
}

// cleanErr drops the *url.Error wrapper (it prints the URL) and prefers the
// context's error.
func (s *Sender) cleanErr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("apitool: send: %w", cerr)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("apitool: send: %w", err)
}

// countReader counts the bytes read through it.
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// readBody reads up to MaxViewBody into memory and spills the whole body to
// a file beyond that; a gzip body is decompressed.
func (s *Sender) readBody(ctx context.Context, hr *http.Response, r *Response) (err error) {
	wire := &countReader{r: hr.Body}
	var src io.Reader = wire
	if enc := strings.ToLower(strings.TrimSpace(hr.Header.Get("Content-Encoding"))); enc == "gzip" || enc == "x-gzip" {
		zr, zerr := gzip.NewReader(wire)
		if zerr != nil && !errors.Is(zerr, io.EOF) {
			return s.cleanErr(ctx, fmt.Errorf("gzip: %w", zerr))
		}
		if zerr == nil {
			defer zr.Close()
			src = zr
		}
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(src, MaxViewBody+1))
	if err != nil {
		return s.cleanErr(ctx, err)
	}
	if n <= MaxViewBody {
		r.Body, r.Size, r.WireSize = buf.Bytes(), n, wire.n
		return nil
	}
	dir := s.SpillDir
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "masil-api-*.body")
	if err != nil {
		return fmt.Errorf("apitool: spill: %w", err)
	}
	defer func() {
		cerr := f.Close()
		if err == nil && cerr != nil {
			err = fmt.Errorf("apitool: spill: %w", cerr)
		}
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if err := os.Chmod(f.Name(), filePerm); err != nil {
		return fmt.Errorf("apitool: spill: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("apitool: spill: %w", err)
	}
	rest, err := io.Copy(f, io.LimitReader(src, SpillMax-n+1))
	if err != nil {
		return s.cleanErr(ctx, err)
	}
	total := n + rest
	if total > SpillMax {
		return ErrTooLarge
	}
	r.Body = buf.Bytes()[:MaxViewBody]
	r.Size, r.WireSize, r.Truncated, r.Spill = total, wire.n, true, f.Name()
	return nil
}

// isBinary: not a text-like type and the first 8KB are not UTF-8.
func isBinary(contentType string, body []byte) bool {
	mt, _, _ := mime.ParseMediaType(contentType)
	mt = strings.ToLower(mt)
	if strings.HasPrefix(mt, "text/") || strings.HasSuffix(mt, "json") || strings.HasSuffix(mt, "xml") ||
		strings.HasSuffix(mt, "javascript") || mt == "application/x-www-form-urlencoded" {
		return false
	}
	head := body[:min(len(body), 8<<10)]
	// a rune cut at the 8KB edge is not a reason to call it binary
	if len(body) > len(head) {
		for i := 0; i < utf8.UTFMax-1 && len(head) > 0 && !utf8.Valid(head); i++ {
			head = head[:len(head)-1]
		}
	}
	return !utf8.Valid(head)
}

// Jars keeps one in-memory cookie jar per collection (never on disk, C3.7).
type Jars struct {
	mu   sync.Mutex
	jars map[string]*cookiejar.Jar
}

// NewJars is an empty set of jars.
func NewJars() *Jars { return &Jars{jars: map[string]*cookiejar.Jar{}} }

func (j *Jars) jar(colID string, create bool) *cookiejar.Jar {
	j.mu.Lock()
	defer j.mu.Unlock()
	if jar, ok := j.jars[colID]; ok || !create {
		return jar
	}
	jar, _ := cookiejar.New(nil) // never fails without options
	j.jars[colID] = jar
	return jar
}

// For is the collection's jar (created on first use).
func (j *Jars) For(colID string) http.CookieJar { return j.jar(colID, true) }

// List is the cookies the collection would send to u.
func (j *Jars) List(colID string, u *url.URL) []*http.Cookie {
	if jar := j.jar(colID, false); jar != nil {
		return jar.Cookies(u)
	}
	return nil
}

// Clear forgets the collection's cookies.
func (j *Jars) Clear(colID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.jars, colID)
}
