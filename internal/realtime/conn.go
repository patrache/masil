package realtime

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
)

// Conn abstracts the socket for tests.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close(code int, reason string) error
}

const (
	readLimit     = 1 << 20 // server frames can carry large objects (CA 4.4 decision)
	maxUpgradeErr = 1024    // coder/websocket keeps at most 1 KiB of a failed handshake body
)

// coderConn adapts *websocket.Conn to Conn (text frames only).
type coderConn struct{ c *websocket.Conn }

// Read returns the next text frame; binary frames are skipped (the server
// never sends them). A close frame surfaces as an error carrying
// websocket.CloseError (see websocket.CloseStatus).
func (c coderConn) Read(ctx context.Context) ([]byte, error) {
	for {
		typ, b, err := c.c.Read(ctx)
		if err != nil {
			return nil, err
		}
		if typ == websocket.MessageText {
			return b, nil
		}
	}
}

func (c coderConn) Write(ctx context.Context, data []byte) error {
	return c.c.Write(ctx, websocket.MessageText, data)
}

func (c coderConn) Close(code int, reason string) error {
	return c.c.Close(websocket.StatusCode(code), reason)
}

// coderDial opens the socket with coder/websocket. ctx bounds only the
// handshake. A non-101 handshake response is decoded from the api.md error
// envelope into *api.Error.
func coderDial(ctx context.Context, url string, h http.Header) (Conn, error) {
	return DialConn(ctx, url, h, readLimit)
}

// DialConn opens a text-frame socket like the realtime client's own dial,
// reading frames of up to limit bytes (a bigger one closes the socket with
// 1009). The room document socket (internal/collab, collab.md 4) uses it with
// its own limit. A non-101 handshake response is an *api.Error.
func DialConn(ctx context.Context, url string, h http.Header, limit int64) (Conn, error) {
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return nil, upgradeError(resp)
		}
		return nil, err
	}
	c.SetReadLimit(limit)
	return coderConn{c}, nil
}

// upgradeError mirrors the api client's envelope decoding for the handshake
// response (whose body coder/websocket truncates to 1 KiB).
func upgradeError(resp *http.Response) *api.Error {
	e := &api.Error{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}
	var raw []byte
	if resp.Body != nil {
		raw, _ = io.ReadAll(io.LimitReader(resp.Body, maxUpgradeErr))
	}
	var env struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
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
		if json.Unmarshal(e.Details, &d) == nil && d.RetryAfterSec != nil && *d.RetryAfterSec > 0 {
			e.RetryAfter = time.Duration(math.Ceil(*d.RetryAfterSec * float64(time.Second)))
		} else if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
			e.RetryAfter = time.Duration(s) * time.Second
		}
	}
	return e
}
