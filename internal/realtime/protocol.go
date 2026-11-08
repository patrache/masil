package realtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Event is one server frame (events.md 2).
type Event struct {
	Type     string // events.md 8장 name
	Seq      *int64 // nil for ephemeral
	TS       time.Time
	Data     json.RawMessage
	Replayed bool // received before `ready` on this connection
}

// Decode returns the typed payload (CA 4.4 table); unknown type => (nil, nil).
// Payloads are returned as values (e.g. Hello, not *Hello). A known type whose
// data is missing, null or malformed is an error.
func (e Event) Decode() (any, error) {
	fn, ok := decoders[e.Type]
	if !ok {
		return nil, nil
	}
	raw := bytes.TrimSpace(e.Data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("realtime: decode %s: missing data", e.Type)
	}
	v, err := fn(raw)
	if err != nil {
		return nil, fmt.Errorf("realtime: decode %s: %w", e.Type, err)
	}
	return v, nil
}

// wireFrame is the server frame shape `{type, seq, ts, data}` (events.md 2).
type wireFrame struct {
	Type string          `json:"type"`
	Seq  *int64          `json:"seq"`
	TS   time.Time       `json:"ts"`
	Data json.RawMessage `json:"data"`
}

// parseFrame parses one server text frame. `{"type":"pong"}` yields an Event
// with nil Seq and zero TS. A frame without a type is an error.
func parseFrame(b []byte) (Event, error) {
	var w wireFrame
	if err := json.Unmarshal(b, &w); err != nil {
		return Event{}, fmt.Errorf("realtime: bad frame: %w", err)
	}
	if w.Type == "" {
		return Event{}, errors.New("realtime: bad frame: missing type")
	}
	return Event{Type: w.Type, Seq: w.Seq, TS: w.TS, Data: w.Data}, nil
}

// Client → server frames (events.md 5). Keys are emitted in a fixed order
// (struct field order); `ref` is never sent, so the server never replies on success.

// pingFrame is exactly the 15-byte heartbeat string (events.md 3.4).
var pingFrame = []byte(`{"type":"ping"}`)

type clientFrame[T any] struct {
	Type string `json:"type"`
	Data T      `json:"data"`
}

func encodeFrame[T any](typ string, data T) []byte {
	b, err := json.Marshal(clientFrame[T]{Type: typ, Data: data})
	if err != nil {
		// Only strings, bools, ints and pointers to them: cannot fail.
		panic("realtime: encode " + typ + ": " + err.Error())
	}
	return b
}

// encodeResume builds `resume` (events.md 3.3). UseDeviceAck takes precedence
// over Epoch/LastSeq.
func encodeResume(p ResumePoint) []byte {
	if p.UseDeviceAck {
		return encodeFrame("resume", struct {
			UseDeviceAck bool `json:"use_device_ack"`
		}{true})
	}
	return encodeFrame("resume", struct {
		Epoch   *string `json:"epoch"`
		LastSeq *int64  `json:"last_seq"`
	}{p.Epoch, p.LastSeq})
}

// encodeAck builds `ack` (events.md 3.5).
func encodeAck(seq int64) []byte {
	return encodeFrame("ack", struct {
		Seq int64 `json:"seq"`
	}{seq})
}

// encodeTyping builds `typing` (events.md 5).
func encodeTyping(channelID string, threadRootID *string) []byte {
	return encodeFrame("typing", struct {
		ChannelID    string  `json:"channel_id"`
		ThreadRootID *string `json:"thread_root_id"`
	}{channelID, threadRootID})
}

// encodeIdle builds `idle` (events.md 5).
func encodeIdle(idle bool) []byte {
	return encodeFrame("idle", struct {
		Idle bool `json:"idle"`
	}{idle})
}

// encodeWatch builds `watch` (events.md 5); nil means "unwatch all" and is
// sent as an empty array. The caller applies the 50-topic cap.
func encodeWatch(topics []string) []byte {
	if topics == nil {
		topics = []string{}
	}
	return encodeFrame("watch", struct {
		Topics []string `json:"topics"`
	}{topics})
}
