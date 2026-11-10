// Package api is the REST client for every endpoint in api.md (CA 4.3).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrache/masil/internal/version"
)

const (
	defaultTimeout = 15 * time.Second  // ordinary calls without a caller deadline
	mediaTimeout   = 120 * time.Second // uploads and media fetches
	maxErrorBody   = 1 << 20           // error envelopes are tiny; never buffer more
	maxRetryWait   = 10 * time.Second  // SendMessage/ReplyThread auto-retry ceiling
	maxMediaBody   = 32 << 20          // media bodies (uploads are <= 5 MiB); never buffer more
)

// Client talks to the masil REST API. It is safe for concurrent use.
type Client struct {
	base      string
	hc        *http.Client
	ua        string
	onUnauth  func()
	onUpgrade func(min string)
	now       func() time.Time

	mu          sync.Mutex
	token       string
	lang        string               // Accept-Language ("" = none)
	unauthFired map[string]bool      // tokens whose OnUnauthorized already ran
	cooldown    map[string]time.Time // limiter -> cooldown end
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the bearer token.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient overrides the HTTP client (default &http.Client{Timeout: 0}; per-call timeouts via ctx).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.hc = h
		}
	}
}

// WithUserAgent overrides the User-Agent (default version.UserAgent()).
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.ua = ua }
}

// WithOnUnauthorized is called (once per token) on 401 unauthorized.
//
// It fires only for a request that carried the token and got 401 with code
// "unauthorized" — never for 401 invalid_credentials (wrong password on login
// or ChangePassword) or an unparsable 401 body.
func WithOnUnauthorized(fn func()) Option {
	return func(c *Client) { c.onUnauth = fn }
}

// WithOnUpgradeRequired is called on 426 with details.min_client_version.
func WithOnUpgradeRequired(fn func(min string)) Option {
	return func(c *Client) { c.onUpgrade = fn }
}

// WithClock injects the clock for rate-limit cooldown tests.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// New creates a client for baseURL.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		base:        strings.TrimRight(baseURL, "/"),
		hc:          &http.Client{Timeout: 0},
		ua:          version.UserAgent(),
		now:         time.Now,
		unauthFired: map[string]bool{},
		cooldown:    map[string]time.Time{},
	}
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	return c
}

// BaseURL returns the server base URL.
func (c *Client) BaseURL() string { return c.base }

// SetToken replaces the bearer token.
func (c *Client) SetToken(token string) {
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

// SetLanguage sets the Accept-Language of every later REST request and of
// AuthHeader (the realtime dial). It is the effective UI language ("ko" in
// 0.1.3); "" sends no header.
func (c *Client) SetLanguage(tag string) {
	c.mu.Lock()
	c.lang = tag
	c.mu.Unlock()
}

func (c *Client) language() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lang
}

// Token returns the current bearer token.
func (c *Client) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// MediaURL turns "/v1/media/<id>" into an absolute URL (absolute URLs pass through).
func (c *Client) MediaURL(rel string) string {
	if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
		return rel
	}
	return c.base + "/" + strings.TrimLeft(rel, "/")
}

// WebSocketURL returns ws(s)://.../v1/ws?mode=..&device_id=..
// (query encoded with url.Values.Encode, so keys are sorted: device_id, mode).
func (c *Client) WebSocketURL(mode, deviceID string) string {
	u, err := url.Parse(c.base)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/ws"
	u.RawPath = ""
	q := url.Values{}
	if deviceID != "" {
		q.Set("device_id", deviceID)
	}
	if mode != "" {
		q.Set("mode", mode)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// AuthHeader returns Authorization + User-Agent (+ Accept-Language when set),
// for the realtime dial.
func (c *Client) AuthHeader() http.Header {
	h := http.Header{}
	if t := c.Token(); t != "" {
		h.Set("Authorization", "Bearer "+t)
	}
	h.Set("User-Agent", c.ua)
	if l := c.language(); l != "" {
		h.Set("Accept-Language", l)
	}
	return h
}

// request is one REST call, built by the endpoint methods (ep_*.go).
type request struct {
	Method, Path  string     // Path is already escaped: build it with escPath
	URL           string     // absolute URL used instead of base+Path (FetchMedia); Path still names the call in errors
	Query         url.Values // encoded with Encode (sorted keys)
	Body          any        // JSON body (Content-Type application/json; charset=utf-8)
	RawBody       io.Reader  // non-JSON body (uploads); wins over Body
	ContentType   string     // for RawBody
	ContentLength int64      // for RawBody; 0 = unknown
	Limiter       string     // dedicated limiter (rl*), "" if only RL_API applies
	Auth          bool       // send the bearer token; also subject to RL_API
	Timeout       time.Duration
	Header        http.Header // extra request headers (e.g. If-None-Match)
}

// escPath joins segments into "/seg/seg", escaping each with url.PathEscape.
//
// url.PathEscape leaves "." and ".." as they are and an empty value leaves
// "//", so a caller value like a pasted invite code ".." would build
// /v1/invites/.. — which WHATWG URL parsing (Workers, Hono) and Cloudflare URL
// normalisation turn into /v1/, another endpoint (pending-fixes 5). do()
// rejects such a path with checkPath before anything is sent.
func escPath(segs ...string) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	return b.String()
}

// checkPath returns a client-side 400 invalid_request {"fields":{"path":
// "invalid"}} when the request URL's path is one a URL normaliser on the way
// would rewrite, so the request could land on a different endpoint: an empty
// segment ("//", a trailing "/"; Cloudflare merges successive slashes), a
// "." or ".." segment, also percent-encoded (".%2e", "%2E"; WHATWG and
// RFC 3986 normalisation decode them as dot segments), or a raw backslash
// (WHATWG and Cloudflare read it as "/"). Every path this package builds
// passes; only bad caller or server-supplied values (IDs, codes, Media.URL)
// trip it.
func checkPath(method, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil // http.NewRequest reports it
	}
	bad := func(why string) error {
		return invalidRequest("path", "invalid", fmt.Sprintf("api: %s %s: refusing request path (%s)", method, u.EscapedPath(), why))
	}
	if strings.Contains(rawURL, `\`) { // escPath sends a value's "\" as %5C; a raw one only comes from a server URL
		return bad("backslash")
	}
	p := u.EscapedPath()
	if p == "" {
		return nil
	}
	if !strings.HasPrefix(p, "/") {
		return bad("relative path")
	}
	for _, seg := range strings.Split(p[1:], "/") {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			dec = seg
		}
		switch dec {
		case "":
			return bad("empty segment")
		case ".", "..":
			return bad("dot segment")
		}
	}
	return nil
}

// rawResponse is an out value for call that receives a non-JSON body
// (GET /v1/media/{id}): a 2xx body is read as bytes (at most maxMediaBody)
// and a 304 Not Modified counts as success. Other statuses are *Error as usual.
type rawResponse struct {
	Status int
	Header http.Header
	Data   []byte // nil for 204/304
}

// errEnvelope is the api.md 0.5 error body.
type errEnvelope struct {
	Error struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details"`
	} `json:"error"`
}

// call performs r and decodes a 2xx JSON body into out (if non-nil; 204 is not read).
// It returns the HTTP status (0 when nothing was received).
//
// Errors: *Error for any non-2xx answer (or a client-side cooldown 429); for
// transport failures the original error wrapped with %w (CodeOf => "network"),
// and caller ctx cancellation/deadline (CodeOf => "canceled").
func (c *Client) call(ctx context.Context, r request, out any) (status int, err error) {
	return c.do(ctx, r, out, true)
}

// callRetryOnce is call plus the single SendMessage/ReplyThread auto-retry
// (CA 4.3.1): on rate_limited with RetryAfter <= 10s it waits that long
// (respecting ctx) and sends the identical request again — same body, so the
// same client_nonce.
func (c *Client) callRetryOnce(ctx context.Context, r request, out any) (int, error) {
	if r.RawBody != nil {
		return 0, fmt.Errorf("api: %s %s: retry needs a JSON body", r.Method, r.Path)
	}
	st, err := c.do(ctx, r, out, true)
	var e *Error
	if !errors.As(err, &e) || e.Status != http.StatusTooManyRequests || e.Code != "rate_limited" || e.RetryAfter > maxRetryWait {
		return st, err
	}
	if e.RetryAfter > 0 {
		t := time.NewTimer(e.RetryAfter)
		select {
		case <-ctx.Done():
			t.Stop()
			return 0, fmt.Errorf("api: %s %s: retry wait: %w", r.Method, r.Path, ctx.Err())
		case <-t.C:
		}
	}
	// r.Body is re-marshaled unchanged, so the retry carries the same
	// client_nonce. The wait covered the cooldown we just learned; do not
	// re-check it (an injected clock would otherwise never let the retry out).
	return c.do(ctx, r, out, false)
}

func (c *Client) do(ctx context.Context, r request, out any, checkCooldown bool) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("api: %s %s: %w", r.Method, r.Path, err)
	}
	u := c.base + r.Path
	if r.URL != "" {
		u = r.URL
	}
	if e := checkPath(r.Method, u); e != nil {
		return 0, e
	}
	if checkCooldown {
		ls := []string{r.Limiter}
		if r.Auth {
			ls = append(ls, rlAPI)
		}
		if e := c.cooldownError(ls...); e != nil {
			return e.Status, e
		}
	}

	parent := ctx
	if _, ok := ctx.Deadline(); !ok {
		d := r.Timeout
		if d <= 0 {
			d = defaultTimeout
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	if len(r.Query) > 0 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + r.Query.Encode()
	}
	var body io.Reader
	contentType := ""
	switch {
	case r.RawBody != nil:
		body, contentType = r.RawBody, r.ContentType
	case r.Body != nil:
		b, err := json.Marshal(r.Body)
		if err != nil {
			return 0, fmt.Errorf("api: encode %s %s: %w", r.Method, r.Path, err)
		}
		body, contentType = bytes.NewReader(b), "application/json; charset=utf-8"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		return 0, fmt.Errorf("api: build %s %s: %w", r.Method, r.Path, err)
	}
	if r.RawBody != nil && r.ContentLength > 0 {
		req.ContentLength = r.ContentLength
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.ua)
	if l := c.language(); l != "" {
		req.Header.Set("Accept-Language", l)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	token := ""
	if r.Auth {
		token = c.Token()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		if parent.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			// Our default timeout, not the caller's: treat as a network failure.
			return 0, fmt.Errorf("api: %s %s: %w", r.Method, r.Path, errRequestTimeout)
		}
		return 0, fmt.Errorf("api: %s %s: %w", r.Method, r.Path, err)
	}
	defer resp.Body.Close()

	if rr, ok := out.(*rawResponse); ok && (resp.StatusCode == http.StatusNotModified || resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return c.readRaw(parent, r, resp, rr)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
			return resp.StatusCode, nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			if parent.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				return resp.StatusCode, fmt.Errorf("api: %s %s: %w", r.Method, r.Path, errRequestTimeout)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return resp.StatusCode, fmt.Errorf("api: %s %s: %w", r.Method, r.Path, err)
			}
			return resp.StatusCode, &Error{
				Status:    resp.StatusCode,
				Code:      "internal_error",
				Message:   "decode response: " + err.Error(),
				Details:   json.RawMessage(`{}`),
				RequestID: resp.Header.Get("X-Request-Id"),
			}
		}
		return resp.StatusCode, nil
	}

	e := c.parseError(resp)
	switch {
	case e.Status == http.StatusTooManyRequests:
		limiter := e.Limiter()
		if limiter == "" {
			limiter = r.Limiter
		}
		if limiter == "" && r.Auth {
			limiter = rlAPI
		}
		c.noteCooldown(limiter, e.RetryAfter)
	case e.Status == http.StatusUnauthorized && e.Code == "unauthorized" && r.Auth && token != "":
		c.fireUnauthorized(token)
	case e.Status == http.StatusUpgradeRequired:
		var d struct {
			Min string `json:"min_client_version"`
		}
		e.details(&d)
		if c.onUpgrade != nil {
			c.onUpgrade(d.Min)
		}
	}
	return e.Status, e
}

// readRaw fills rr from a 2xx/304 response (see rawResponse).
func (c *Client) readRaw(parent context.Context, r request, resp *http.Response, rr *rawResponse) (int, error) {
	rr.Status = resp.StatusCode
	rr.Header = resp.Header.Clone()
	rr.Data = nil
	if resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBody+1))
	if err != nil {
		if parent.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return resp.StatusCode, fmt.Errorf("api: %s %s: %w", r.Method, r.Path, errRequestTimeout)
		}
		return resp.StatusCode, fmt.Errorf("api: %s %s: read body: %w", r.Method, r.Path, err)
	}
	if len(data) > maxMediaBody {
		e := decodeError(resp.StatusCode, fmt.Sprintf("response body exceeds %d bytes", maxMediaBody))
		e.RequestID = resp.Header.Get("X-Request-Id")
		return resp.StatusCode, e
	}
	rr.Data = data
	return resp.StatusCode, nil
}

// parseError builds *Error from a non-2xx response. A body that is not the
// envelope falls back to internal_error (5xx) or invalid_request (4xx).
func (c *Client) parseError(resp *http.Response) *Error {
	e := &Error{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	var env errEnvelope
	if json.Unmarshal(raw, &env) == nil && env.Error.Code != "" {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
		e.Details = env.Error.Details
	} else {
		if resp.StatusCode >= 500 {
			e.Code = "internal_error"
		} else {
			e.Code = "invalid_request"
		}
		e.Message = strings.TrimSpace(http.StatusText(resp.StatusCode))
	}
	if len(e.Details) == 0 || string(e.Details) == "null" {
		e.Details = json.RawMessage(`{}`)
	}
	if e.Status == http.StatusTooManyRequests {
		var d struct {
			RetryAfterSec *float64 `json:"retry_after_sec"`
		}
		if e.details(&d) && d.RetryAfterSec != nil && *d.RetryAfterSec > 0 {
			e.RetryAfter = time.Duration(math.Ceil(*d.RetryAfterSec * float64(time.Second)))
		} else if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
			e.RetryAfter = time.Duration(s) * time.Second
		}
	}
	return e
}

// fireUnauthorized runs OnUnauthorized once per token, and only while that
// token is still the current one (a newer login must not be logged out by a
// late 401 for the old token).
func (c *Client) fireUnauthorized(token string) {
	c.mu.Lock()
	fire := c.onUnauth != nil && c.token == token && !c.unauthFired[token]
	if fire {
		c.unauthFired[token] = true
	}
	c.mu.Unlock()
	if fire {
		c.onUnauth()
	}
}
