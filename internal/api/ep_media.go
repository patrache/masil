package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ---- 5.14 uploads / media ----

// Upload: POST /v1/uploads?purpose=avatar|post|story. The body is the raw
// image bytes (not JSON) sent with the given Content-Type and Content-Length
// = size; size must be > 0 (api.md requires Content-Length), otherwise a
// client-side invalid_request with no request sent. Default timeout 120s.
func (c *Client) Upload(ctx context.Context, purpose, contentType string, body io.Reader, size int64) (*Upload, error) {
	if size <= 0 || body == nil {
		return nil, invalidRequest("size", "invalid", fmt.Sprintf("upload needs a body with a known size (got %d)", size))
	}
	req := request{
		Method: "POST", Path: "/v1/uploads", Query: url.Values{"purpose": {purpose}},
		RawBody: body, ContentType: contentType, ContentLength: size,
		Limiter: rlUpload, Auth: true, Timeout: mediaTimeout,
	}
	v, _, err := one[Upload](ctx, c, req, "upload")
	return v, err
}

// DeleteUpload: DELETE /v1/uploads/{id} (owner, pending only; 204).
func (c *Client) DeleteUpload(ctx context.Context, uploadID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "uploads", uploadID), Auth: true}, nil)
	return err
}

// FetchMedia: GET /v1/media/{id}; 304 => NotModified (Data nil).
//
// mediaURL is either relative ("/v1/media/<id>", as in Media.URL) or
// absolute. The bearer token is sent only when the URL is on the API origin
// (scheme+host of BaseURL), never to a foreign host. ifNoneMatch ("" = none)
// is sent as If-None-Match. Default timeout 120s.
//
// On the API origin the path must be <BaseURL path>/v1/media/<one segment>
// (api.md: Media.URL / avatar_url are "/v1/media/<id>"); anything else is a
// client-side 400 invalid_request {"fields":{"path":"invalid"}} with nothing
// sent, so a hostile Media.URL ("/v1/me", "/v1/auth/sessions") cannot have
// the bearer token GET another API endpoint.
func (c *Client) FetchMedia(ctx context.Context, mediaURL, ifNoneMatch string) (*MediaBlob, error) {
	abs := c.MediaURL(mediaURL)
	same := c.sameOrigin(abs)
	if same {
		if e := c.checkMediaPath(abs); e != nil {
			return nil, e
		}
	}
	req := request{Method: "GET", Path: mediaURL, URL: abs, Auth: same, Timeout: mediaTimeout}
	if ifNoneMatch != "" {
		req.Header = http.Header{"If-None-Match": {ifNoneMatch}}
	}
	var raw rawResponse
	st, err := c.call(ctx, req, &raw)
	if err != nil {
		return nil, err
	}
	blob := &MediaBlob{ETag: raw.Header.Get("ETag"), ContentType: raw.Header.Get("Content-Type")}
	if st == http.StatusNotModified {
		blob.NotModified = true
		if blob.ETag == "" {
			blob.ETag = ifNoneMatch
		}
		return blob, nil
	}
	blob.Data = raw.Data
	return blob, nil
}

// sameOrigin reports whether abs has the scheme and host of the base URL.
func (c *Client) sameOrigin(abs string) bool {
	b, err1 := url.Parse(c.base)
	u, err2 := url.Parse(abs)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(b.Scheme, u.Scheme) && strings.EqualFold(b.Host, u.Host)
}

// checkMediaPath reports a client-side invalid_request unless abs (on the API
// origin) has the path <base path>/v1/media/<id>: exactly one non-empty
// segment after the prefix that does not decode to anything holding "/" or
// a backslash. The query is not looked at.
func (c *Client) checkMediaPath(abs string) error {
	bad := func(p string) error {
		return invalidRequest("path", "invalid", fmt.Sprintf("api: GET %s: refusing request path (not a media path on the API origin)", p))
	}
	b, err1 := url.Parse(c.base)
	u, err2 := url.Parse(abs)
	if err1 != nil || err2 != nil {
		return bad(abs)
	}
	p := u.EscapedPath()
	id, ok := strings.CutPrefix(p, strings.TrimRight(b.EscapedPath(), "/")+"/v1/media/")
	if !ok || id == "" || strings.Contains(id, "/") {
		return bad(p)
	}
	dec, err := url.PathUnescape(id)
	if err != nil || strings.ContainsAny(dec, "/\\") {
		return bad(p)
	}
	return nil
}
