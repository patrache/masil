package leetcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Origin is the only LeetCode site masil talks to (A10.1 JA10).
const Origin = "https://leetcode.com"

// maxBody caps one answer; a larger one is ErrUnexpected.
const maxBody = 4 << 20

// defaultTimeout bounds one request when Options.HTTP is nil.
const defaultTimeout = 30 * time.Second

// Options configure a Client.
type Options struct {
	BaseURL string       // "" => Origin (tests point it at leetcodetest)
	HTTP    *http.Client // nil => a client with a 30s timeout
	Creds   *Credentials // nil => signed out (reading works, judging does not)
	Log     *slog.Logger // debug lines: method, path, status, length — never header values
}

// Client talks to LeetCode. It never retries and never polls: the UI calls
// again when the user asks or a ui.After fires.
type Client struct {
	base  *url.URL
	http  *http.Client
	creds *Credentials
	log   *slog.Logger
}

// Question is one problem as masil uses it.
type Question struct {
	QuestionID, FrontendID, Slug, Title, Difficulty string
	IsPaidOnly                                      bool
	Tags                                            []string
	ContentHTML                                     string            // "" for premium (never requested)
	Snippets                                        map[string]string // langSlug -> starting code
	ExampleTestcases                                []string
}

// UserStatus is who the cookies sign in as.
type UserStatus struct {
	SignedIn bool
	Username string
	Premium  bool
}

// New makes a client. A BaseURL that does not parse falls back to Origin.
func New(o Options) *Client {
	base, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	if o.BaseURL == "" || err != nil || base.Host == "" {
		base, _ = url.Parse(Origin)
	}
	hc := &http.Client{Timeout: defaultTimeout}
	if o.HTTP != nil {
		cp := *o.HTTP
		hc = &cp
	}
	// never follow a redirect: the cookies and x-csrftoken must not reach another host
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Client{base: base, http: hc, creds: o.Creds, log: log}
}

// WithCreds is a copy of c that signs in with cr (nil = signed out).
func (c *Client) WithCreds(cr *Credentials) *Client {
	cp := *c
	cp.creds = cr
	return &cp
}

// signedIn reports whether c has usable cookies.
func (c *Client) signedIn() bool { return c.creds.usable() }

// do sends one request to the LeetCode host and decodes a JSON answer into
// out. slug ("" = none) sets the Referer to that problem's page.
func (c *Client) do(ctx context.Context, method, path, slug string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	ref := c.base.String() + "/"
	if slug != "" {
		ref = c.base.String() + "/problems/" + slug + "/"
	}
	req.Header.Set("Referer", ref)
	if c.signedIn() && req.URL.Host == c.base.Host {
		req.Header.Set("Cookie", c.creds.cookieHeader())
		req.Header.Set("x-csrftoken", c.creds.CSRF.Reveal())
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Debug("leetcode request failed", "method", method, "path", path, "err", err)
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	c.log.Debug("leetcode request", "method", method, "path", path, "status", resp.StatusCode, "len", len(data),
		"cookie", redactedIf(req.Header.Get("Cookie")), "x-csrftoken", redactedIf(req.Header.Get("x-csrftoken")))
	if err != nil {
		return fmt.Errorf("%w: read: %v", ErrUnexpected, err)
	}
	if len(data) > maxBody {
		return fmt.Errorf("%w: answer over %d bytes", ErrUnexpected, maxBody)
	}
	if err := classify(resp, data); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %v", ErrUnexpected, err)
	}
	return nil
}

// redactedIf is how a header value appears in a debug line.
func redactedIf(v string) string {
	if v == "" {
		return ""
	}
	return "(redacted)"
}

// graphql runs one query and decodes its data into out.
func (c *Client) graphql(ctx context.Context, op, query string, vars map[string]any, slug string, out any) error {
	if vars == nil {
		vars = map[string]any{}
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := c.do(ctx, "POST", "/graphql/", slug, gqlRequest{OperationName: op, Query: query, Variables: vars}, &env); err != nil {
		return err
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return fmt.Errorf("%w: graphql %s: no data (%d errors)", ErrUnexpected, op, len(env.Errors))
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("%w: graphql %s: %v", ErrUnexpected, op, err)
	}
	return nil
}

// checkMeta rejects a question whose slug or number cannot be used.
func checkMeta(g *gqlQuestion) error {
	if !ValidSlug(g.TitleSlug) || !FrontendIDRe.MatchString(g.QuestionFrontendID) {
		return fmt.Errorf("%w: %q %q", ErrBadSlug, g.TitleSlug, g.QuestionFrontendID)
	}
	return nil
}

// Daily is today's problem and its UTC day. The first query asks for facts
// only; a non-premium problem's body comes from one more query. A premium
// problem comes back with its facts and no body (nil error: the UI shows the
// lock).
func (c *Client) Daily(ctx context.Context) (q *Question, date string, err error) {
	var d struct {
		Active *struct {
			Date     string       `json:"date"`
			Question *gqlQuestion `json:"question"`
		} `json:"activeDailyCodingChallengeQuestion"`
	}
	if err := c.graphql(ctx, "questionOfToday", dailyQuery, nil, "", &d); err != nil {
		return nil, "", err
	}
	if d.Active == nil || d.Active.Question == nil {
		return nil, "", fmt.Errorf("%w: no daily problem", ErrUnexpected)
	}
	if !validDate(d.Active.Date) {
		return nil, "", fmt.Errorf("%w: daily date %q", ErrUnexpected, d.Active.Date)
	}
	if err := checkMeta(d.Active.Question); err != nil {
		return nil, "", err
	}
	meta := d.Active.Question.question()
	if meta.IsPaidOnly {
		return meta, d.Active.Date, nil
	}
	full, err := c.body(ctx, meta.Slug)
	if err != nil {
		return nil, "", err
	}
	return full, d.Active.Date, nil
}

// Question is the problem slug. A premium problem comes back with its facts
// only and ErrPremiumOnly (its body is never requested).
func (c *Client) Question(ctx context.Context, slug string) (*Question, error) {
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("%w: %q", ErrBadSlug, slug)
	}
	meta, err := c.question(ctx, "questionMeta", questionMetaQuery, slug)
	if err != nil {
		return nil, err
	}
	if meta.IsPaidOnly {
		return meta, ErrPremiumOnly
	}
	return c.body(ctx, slug)
}

// body is a non-premium problem with its body, snippets and examples.
func (c *Client) body(ctx context.Context, slug string) (*Question, error) {
	q, err := c.question(ctx, "questionData", questionBodyQuery, slug)
	if err != nil {
		return nil, err
	}
	if q.IsPaidOnly { // turned premium between the two queries: drop what came
		q.ContentHTML, q.Snippets, q.ExampleTestcases = "", map[string]string{}, []string{}
		return q, ErrPremiumOnly
	}
	return q, nil
}

func (c *Client) question(ctx context.Context, op, query, slug string) (*Question, error) {
	var d struct {
		Question *gqlQuestion `json:"question"`
	}
	if err := c.graphql(ctx, op, query, map[string]any{"titleSlug": slug}, slug, &d); err != nil {
		return nil, err
	}
	if d.Question == nil {
		return nil, fmt.Errorf("%w: no problem %q", ErrUnexpected, slug)
	}
	if err := checkMeta(d.Question); err != nil {
		return nil, err
	}
	if d.Question.TitleSlug != slug {
		return nil, fmt.Errorf("%w: asked %q, got %q", ErrUnexpected, slug, d.Question.TitleSlug)
	}
	return d.Question.question(), nil
}

// UserStatus asks who the cookies sign in as (signed out without cookies).
func (c *Client) UserStatus(ctx context.Context) (*UserStatus, error) {
	var d struct {
		UserStatus *struct {
			IsSignedIn bool    `json:"isSignedIn"`
			Username   *string `json:"username"`
			IsPremium  *bool   `json:"isPremium"`
		} `json:"userStatus"`
	}
	if err := c.graphql(ctx, "globalData", userStatusQuery, nil, "", &d); err != nil {
		return nil, err
	}
	if d.UserStatus == nil {
		return nil, fmt.Errorf("%w: no userStatus", ErrUnexpected)
	}
	st := &UserStatus{SignedIn: d.UserStatus.IsSignedIn}
	if d.UserStatus.Username != nil {
		st.Username = *d.UserStatus.Username
	}
	if d.UserStatus.IsPremium != nil {
		st.Premium = *d.UserStatus.IsPremium
	}
	return st, nil
}
