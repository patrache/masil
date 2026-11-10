// Package share is the workspace share engine (v0.1.3 B10): the share socket
// transport (Conn), and on top of it the owner Host and the participant Guest.
// Frames are docs/contract/workspace.md frames (internal/workspace/wire).
package share

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Conn is one share socket. Frames are whole binary messages (header and
// body, at most wire.MaxFrame bytes).
type Conn interface {
	// Send writes one frame; it fails once the conn is done.
	Send(frame []byte) error
	// Recv delivers received frames in order; it is closed after Done.
	Recv() <-chan []byte
	// Done is closed when the socket ends (either side, or a read error).
	Done() <-chan struct{}
	// Err is why the conn ended (nil while it is open); CloseCodeOf reads
	// the close code out of it.
	Err() error
	// Close ends the socket with code and reason.
	Close(code int, reason string)
}

// DialFunc opens the share socket of shareID.
type DialFunc func(ctx context.Context, shareID string) (Conn, error)

// CloseCode is a share socket close code (workspace.md 1).
type CloseCode int

const (
	CloseProtocol     CloseCode = 4000 // bad frame, From not my seat
	CloseUnauthorized CloseCode = 4001 // my session was revoked (logout, password change)
	CloseSuspended    CloseCode = 4003 // my account was suspended
	CloseRateLimited  CloseCode = 4008 // over the per-connection limits for 3 seconds
	CloseEnded        CloseCode = 4100 // the share ended
	CloseKicked       CloseCode = 4101 // the owner removed me
	CloseReplaced     CloseCode = 4102 // my newer socket took over
)

// CloseCodeOf is the close code err carries, or 0 when it carries none (a
// dropped connection, a read error, nil).
func CloseCodeOf(err error) CloseCode {
	if err == nil {
		return 0
	}
	if code := websocket.CloseStatus(err); code != -1 {
		return CloseCode(code)
	}
	return 0
}

// recvBuffer is how many received frames wait for the reader before the
// socket read loop blocks.
const recvBuffer = 256

// ErrClosed is Send's error on a conn that is done but has no other reason.
var ErrClosed = errors.New("share: connection closed")

// Dialer dials the share socket through c: the URL from
// c.WorkspaceShareWSURL, the headers from c.AuthHeader, a read limit of
// wire.MaxFrame. A refused upgrade is an *api.Error (404 = the share ended,
// or is not mine any more). ctx bounds only the handshake.
func Dialer(c *api.Client) DialFunc {
	return func(ctx context.Context, shareID string) (Conn, error) {
		u := c.WorkspaceShareWSURL(shareID)
		if u == "" {
			return nil, fmt.Errorf("share: bad share id %q", shareID)
		}
		ws, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: c.AuthHeader()})
		if err != nil {
			if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
				return nil, realtime.UpgradeError(resp)
			}
			return nil, err
		}
		ws.SetReadLimit(wire.MaxFrame)
		return newWSConn(ws), nil
	}
}

// wsConn is Conn over coder/websocket: binary messages only, text messages
// from the server are skipped.
type wsConn struct {
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	recv   chan []byte
	done   chan struct{}

	mu  sync.Mutex
	err error
}

func newWSConn(ws *websocket.Conn) *wsConn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &wsConn{ws: ws, ctx: ctx, cancel: cancel, recv: make(chan []byte, recvBuffer), done: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *wsConn) readLoop() {
	var err error
	for {
		var typ websocket.MessageType
		var b []byte
		typ, b, err = c.ws.Read(c.ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageBinary {
			continue
		}
		select {
		case c.recv <- b:
		case <-c.ctx.Done():
		}
		if c.ctx.Err() != nil {
			break
		}
	}
	c.finish(err)
	c.ws.CloseNow()
	c.cancel()
	close(c.recv)
	close(c.done)
}

// finish records why the conn ended; the first reason wins.
func (c *wsConn) finish(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		if err == nil {
			err = ErrClosed
		}
		c.err = err
	}
}

func (c *wsConn) Send(frame []byte) error {
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	if err := c.ws.Write(c.ctx, websocket.MessageBinary, frame); err != nil {
		if e := c.Err(); e != nil {
			return e
		}
		return err
	}
	return nil
}

func (c *wsConn) Recv() <-chan []byte   { return c.recv }
func (c *wsConn) Done() <-chan struct{} { return c.done }

func (c *wsConn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close sends the close frame (waiting briefly for the server's echo, as
// coder/websocket does) and ends the conn; Err then carries code.
func (c *wsConn) Close(code int, reason string) {
	c.finish(websocket.CloseError{Code: websocket.StatusCode(code), Reason: reason})
	c.ws.Close(websocket.StatusCode(code), reason)
	c.cancel()
	<-c.done
}
