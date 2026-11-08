// Package pathbuild is a fixture for TestPathBuildScanFlagsConcatenation:
// each exported method builds its request path either the allowed way
// (string literal or escPath, directly or through a helper) or not.
package pathbuild

import "fmt"

type request struct{ Method, Path, URL string }

type Client struct{}

func escPath(segs ...string) string { return "" }

func (c *Client) call(r request) error { return nil }

// ---- allowed ----

func (c *Client) Literal() error { return c.call(request{Method: "GET", Path: "/v1/me"}) }

func (c *Client) Escaped(id string) error { return c.call(request{Path: escPath("v1", "x", id)}) }

func (c *Client) LocalEscaped(id string) error {
	p := escPath("v1", "x", id)
	return c.call(request{Path: p})
}

func (c *Client) ViaHelperLiteral() error { return c.helper("/v1/a") }

func (c *Client) ViaHelperEscaped(id string) error { return c.helper(escPath("v1", "b", id)) }

func (c *Client) helper(path string) error { return c.call(request{Path: path}) }

// FetchMedia is the one method that takes a whole URL (checked by checkMediaPath).
func (c *Client) FetchMedia(mediaURL string) error {
	return c.call(request{Path: mediaURL, URL: mediaURL})
}

// ---- not allowed ----

func (c *Client) Concat(id string) error { return c.call(request{Path: "/v1/x/" + id}) }

func (c *Client) Sprintf(id string) error {
	return c.call(request{Path: fmt.Sprintf("/v1/x/%s", id)})
}

func (c *Client) LocalConcat(id string) error {
	p := "/v1/y/" + id
	return c.call(request{Path: p})
}

func (c *Client) AppendLater(id string) error {
	p := "/v1/w/"
	p += id
	return c.call(request{Path: p})
}

func (c *Client) RawParam(path string) error { return c.call(request{Path: path}) }

func (c *Client) ViaBadHelper(id string) error { return c.badHelper(id) }

func (c *Client) badHelper(path string) error { return c.call(request{Path: path}) }

func (c *Client) WithURL(u string) error { return c.call(request{Path: "/v1/q", URL: u}) }

func (c *Client) SetLater(id string) error {
	r := request{Method: "GET", Path: "/v1/s"}
	r.Path = r.Path + "/" + id
	return c.call(r)
}
