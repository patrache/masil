package wire

import (
	"encoding/json"
	"time"
)

// Inner messages: the JSON bodies of frames after opening (or of plaintext
// Server/Handshake frames). Field names follow docs/contract/workspace.md 3.1.

type SeatInfo struct {
	Seat   uint8  `json:"seat"`
	UserID string `json:"user_id"`
}

type SrvHello struct {
	ShareID  string     `json:"share_id"`
	Seat     uint8      `json:"seat"`
	OwnerID  string     `json:"owner_id"`
	Seats    []SeatInfo `json:"seats"`
	MaxFrame int        `json:"max_frame"`
}

// SrvPeer State values.
const (
	PeerJoined = "joined"
	PeerLeft   = "left"
)

type SrvPeer struct {
	Seat   uint8  `json:"seat"`
	UserID string `json:"user_id"`
	State  string `json:"state"`
}

// CtlLevels keys are seat numbers as decimal strings.
type CtlLevels struct {
	Levels   map[string]string `json:"levels"`
	Verified map[string]bool   `json:"verified"`
}

type CtlKick struct {
	Seat uint8 `json:"seat"`
}

type HsCommit struct {
	Commit []byte `json:"commit"`
}

type HsPub struct {
	Pub []byte `json:"pub"`
}

type HsConfirm struct {
	Match bool `json:"match"`
}

type HsGrant struct {
	Epoch         uint32     `json:"epoch"`
	SessionKey    []byte     `json:"session_key"`
	SignPub       []byte     `json:"sign_pub"`
	Level         string     `json:"level"`
	KeyboardUntil *time.Time `json:"keyboard_until,omitempty"`
	Verified      bool       `json:"verified"`
}

type HsDecline struct {
	Reason string `json:"reason"`
}

type HsResume struct{}

type RosterEntry struct {
	Seat          uint8      `json:"seat"`
	UserID        string     `json:"user_id"`
	Level         string     `json:"level"`
	Verified      bool       `json:"verified"`
	KeyboardUntil *time.Time `json:"keyboard_until,omitempty"`
	Connected     bool       `json:"connected"`
}

type Roster struct {
	Members []RosterEntry `json:"members"`
}

type Rekey struct {
	Epoch uint32 `json:"epoch"`
	Key   []byte `json:"key"`
}

type Ended struct {
	Reason string `json:"reason"`
}

type Notice struct {
	Code string `json:"code"`
	Seat uint8  `json:"seat"`
	Arg  string `json:"arg"`
}

type TermStatus struct {
	Term      uint8  `json:"term"`
	Title     string `json:"title"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	Attention bool   `json:"attention"`
	Unsaved   int    `json:"unsaved"`
}

type PropSubmit struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type PropResult struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type TreeReq struct {
	Dir string `json:"dir"`
}

type TreeEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

type TreeList struct {
	Dir     string      `json:"dir"`
	Entries []TreeEntry `json:"entries"`
	More    int         `json:"more"`
	Error   string      `json:"error,omitempty"`
}

type DocOpen struct {
	Doc string `json:"doc"`
}

type DocState struct {
	Doc      string `json:"doc"`
	Seq      int64  `json:"seq"`
	Text     string `json:"text"`
	ReadOnly bool   `json:"read_only"`
	Reason   string `json:"reason,omitempty"`
	Error    string `json:"error,omitempty"`
}

// DocOp.Ops is an ot.Op in ot.js JSON form (docs/contract/workspace.md 7).
type DocOp struct {
	Doc        string          `json:"doc"`
	ClientOpID string          `json:"client_op_id"`
	BaseSeq    int64           `json:"base_seq"`
	Ops        json.RawMessage `json:"ops"`
}

type DocAck struct {
	Doc        string `json:"doc"`
	ClientOpID string `json:"client_op_id"`
	Seq        int64  `json:"seq"`
}

type DocReject struct {
	Doc        string `json:"doc"`
	ClientOpID string `json:"client_op_id"`
	Code       string `json:"code"`
}

// AuthorDisk is DocBcast.Author for edits that came from the disk.
const AuthorDisk = -1

type DocBcast struct {
	Doc    string          `json:"doc"`
	Seq    int64           `json:"seq"`
	Author int             `json:"author"`
	Ops    json.RawMessage `json:"ops"`
}

type DocSave struct {
	Doc string `json:"doc"`
}

type DocPresence struct {
	Doc     string `json:"doc"`
	Seat    uint8  `json:"seat"`
	Cursor  int    `json:"cursor"`
	SelFrom int    `json:"sel_from"`
	SelTo   int    `json:"sel_to"`
}

type DocClose struct {
	Doc string `json:"doc"`
}

// FsReq Op values.
const (
	FsCreate = "create"
	FsRename = "rename"
	FsDelete = "delete"
)

type FsReq struct {
	Op   string `json:"op"`
	From string `json:"from"`
	To   string `json:"to"`
}

type FsResult struct {
	Op    string `json:"op"`
	From  string `json:"from"`
	To    string `json:"to"`
	Error string `json:"error,omitempty"`
}

// SelfLevel lowers one's own level; "" leaves.
type SelfLevel struct {
	Level string `json:"level"`
}

// TermPayload builds the plaintext of term_out/term_key/term_input:
// [1 byte terminal id][raw bytes].
func TermPayload(term uint8, b []byte) []byte {
	out := make([]byte, 0, 1+len(b))
	out = append(out, term)
	return append(out, b...)
}

// SplitTerm is the inverse of TermPayload. The bytes alias p.
func SplitTerm(p []byte) (term uint8, b []byte, err error) {
	if len(p) < 1 {
		return 0, nil, ErrLength
	}
	return p[0], p[1:], nil
}
