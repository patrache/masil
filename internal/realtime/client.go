// Package realtime is the WebSocket client shared by the TUI and the daemon (CA 4.4).
package realtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/patrache/masil/internal/api"
)

// Mode is the connection mode.
type Mode string

// Connection modes.
const (
	ModeFull   Mode = "full"
	ModeNotify Mode = "notify"
)

// ResumePoint is what the client sends in `resume`.
type ResumePoint struct {
	Epoch        *string // nil => first connect
	LastSeq      *int64
	UseDeviceAck bool // daemon restart without disk state
}

// Phase is the connection phase.
type Phase int

// Connection phases.
const (
	PhaseIdle       Phase = iota
	PhaseConnecting       // dialing
	PhaseSyncing          // got hello, sent resume, waiting for ready (UI: "동기화 중")
	PhaseLive             // ready received
	PhaseBackoff          // waiting before reconnect
	PhaseStopped          // terminal: unauthorized/suspended/upgrade_required/ctx done
)

// State is reported through Options.OnState.
type State struct {
	Phase       Phase
	Attempt     int       // consecutive failures (n in backoff formula)
	NextRetry   time.Time // PhaseBackoff only
	LastClose   int       // last close code (0 if none)
	Err         error
	Epoch       string
	Seq         int64 // last processed seq
	TUIOnDevice *bool // notify mode: hello.local_tui_connected / device.tui_state
}

// Options configures a Client.
type Options struct {
	API      *api.Client // base URL, token, UA
	DeviceID string
	Mode     Mode
	Resume   ResumePoint
	OnEvent  func(Event) // called on the client goroutine, in order; must not block long
	OnState  func(State)
	Logger   *slog.Logger
	Now      func() time.Time
	Rand     func() float64                                                     // jitter, tests inject
	Dial     func(ctx context.Context, url string, h http.Header) (Conn, error) // nil => coder/websocket
}

// Terminal errors returned by Run.
var (
	ErrUnauthorized    = errors.New("realtime: unauthorized (4001/401)")
	ErrSuspended       = errors.New("realtime: account suspended (4003/403)")
	ErrUpgradeRequired = errors.New("realtime: upgrade required (4010/426)")
)

// Client is the connect/resume/heartbeat/reconnect loop.
type Client struct { /* unexported */
}

// New creates a client.
func New(opts Options) *Client {
	// TODO(core): Task 13
	return nil
}

// Run is the connect+reconnect loop; returns nil on ctx done, or one of the Err* terminal errors.
func (c *Client) Run(ctx context.Context) error {
	// TODO(core): Task 13
	return nil
}

// Position returns the current epoch + last processed seq.
func (c *Client) Position() ResumePoint {
	// TODO(core): Task 13
	return ResumePoint{}
}

// MarkProcessed records that the caller finished applying seq; acks are sent at most every 5s.
func (c *Client) MarkProcessed(seq int64) {
	// TODO(core): Task 13
}

// Typing sends a typing frame (full only); throttled to 1 per 3s per (channel,thread).
func (c *Client) Typing(channelID string, threadRootID *string) {
	// TODO(core): Task 13
}

// SetIdle sends idle state (full only); sends only on change, re-sent after reconnect.
func (c *Client) SetIdle(idle bool) {
	// TODO(core): Task 13
}

// Watch replaces the watched topics (full only); capped at 50, re-sent every 120s and after reconnect.
func (c *Client) Watch(topics []string) {
	// TODO(core): Task 13
}

// Close sends 1000 and stops.
func (c *Client) Close() {
	// TODO(core): Task 13
}
