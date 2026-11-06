// Package pathscan is a fixture for TestPathMethodsScanSeesMethodHelpers:
// each exported method either does or does not put a caller value into a
// request path through escPath, directly or via helpers.
package pathscan

type Client struct{}

func escPath(segs ...string) string { return "" }

func (c *Client) get(path string) error { return nil }

// Direct: escPath with a caller value.
func (c *Client) Direct(id string) error { return c.get(escPath("v1", "x", id)) }

// ViaFunc: through a package-level helper.
func (c *Client) ViaFunc(id string) error { return c.get(funcHelper(id)) }

func funcHelper(id string) string { return escPath("v1", "f", id) }

// ViaMethod: through an unexported method helper on the receiver.
func (c *Client) ViaMethod(id string) error { return c.methodHelper(id) }

func (c *Client) methodHelper(id string) error { return c.get(escPath("v1", "m", id)) }

// ViaChain: method helper -> method helper -> escPath.
func (c *Client) ViaChain(id string) error { return c.outer(id) }

func (c *Client) outer(id string) error { return c.inner(id) }

func (c *Client) inner(id string) error { return c.get(escPath("v1", "c", id)) }

// ViaGeneric: through a generic package helper.
func (c *Client) ViaGeneric(id string) error { _, err := generic[int](c, id); return err }

func generic[T any](c *Client, id string) (T, error) {
	var z T
	return z, c.get(escPath("v1", "g", id))
}

// Constant: escPath with literals only.
func (c *Client) Constant() error { return c.get(escPath("v1", "const")) }

// NoPath: no escPath at all.
func (c *Client) NoPath() error { return c.get("/v1/plain") }
