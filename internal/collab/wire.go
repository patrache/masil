package collab

import (
	"encoding/json"
	"sort"
)

// Document socket frames (collab.md 3). Every frame is one JSON text with its
// kind in "t".

// Frame limits (collab.md 4).
const (
	// MaxClientFrame is the largest frame the client sends; the server
	// closes the socket with 1009 above it.
	MaxClientFrame = 65_536
	// ReadLimit is the largest frame the client reads (the biggest
	// maxDocBytes + 65,536): one hello snapshot or restore reset.
	ReadLimit = 2_162_688
)

// ---- client → server ----

type helloOut struct {
	T     string `json:"t"`
	Since *int64 `json:"since"`
}

type opOut struct {
	T          string `json:"t"`
	ClientOpID string `json:"client_op_id"`
	BaseSeq    int64  `json:"base_seq"`
	Ops        []Op   `json:"ops"`
}

func encodeHello(since *int64) []byte {
	b, _ := json.Marshal(helloOut{T: "hello", Since: since})
	return b
}

func encodeOp(id string, base int64, ops []Op) ([]byte, error) {
	return json.Marshal(opOut{T: "op", ClientOpID: id, BaseSeq: base, Ops: ops})
}

// encodePresence sends the fields p sets (nil = not changed); an empty,
// non-nil Selection clears it.
func encodePresence(p Presence) []byte {
	m := map[string]any{"t": "presence"}
	if p.Cursor != nil {
		m["cursor"] = json.RawMessage(p.Cursor)
	}
	if p.Selection != nil {
		m["selection"] = p.Selection
	}
	if p.Dragging != nil {
		m["dragging"] = json.RawMessage(p.Dragging)
	}
	b, _ := json.Marshal(m)
	return b
}

// ---- server → client ----

// frameIn is any server frame; which fields count depends on T.
type frameIn struct {
	T          string          `json:"t"`
	Seq        int64           `json:"seq"`
	Snapshot   json.RawMessage `json:"snapshot"`
	Ops        json.RawMessage `json:"ops"` // hello: []helloOp, op: []Op
	Author     string          `json:"author"`
	ClientOpID string          `json:"client_op_id"`
	Code       string          `json:"code"`
	Detail     string          `json:"detail"`
	User       string          `json:"user"`
	Cursor     json.RawMessage `json:"cursor"`
	Selection  []string        `json:"selection"`
	Dragging   json.RawMessage `json:"dragging"`
	Reason     string          `json:"reason"`
}

// helloOp is one sequenced frame in hello.ops.
type helloOp struct {
	Seq    int64  `json:"seq"`
	Author string `json:"author"`
	Ops    []Op   `json:"ops"`
}

func decodeFrame(b []byte) (frameIn, error) {
	var f frameIn
	err := json.Unmarshal(b, &f)
	return f, err
}

func sortedPeers(m map[string]Presence) []Presence {
	out := make([]Presence, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User < out[j].User })
	return out
}
