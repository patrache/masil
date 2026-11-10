package wire

import "fmt"

// Kind is the frame kind byte.
type Kind uint8

// Class says how a kind's body is protected and routed.
type Class uint8

const (
	ClassServer    Class = iota + 1 // plaintext JSON for/from the server
	ClassHandshake                  // plaintext public values
	ClassBroadcast                  // sealed with K_s + owner signature
	ClassToOwner                    // sealed with the sender's P2O key
	ClassToPeer                     // sealed with the recipient's O2P key
)

var classNames = map[Class]string{
	ClassServer: "Server", ClassHandshake: "Handshake", ClassBroadcast: "Broadcast",
	ClassToOwner: "ToOwner", ClassToPeer: "ToPeer",
}

func (c Class) String() string {
	if n, ok := classNames[c]; ok {
		return n
	}
	return fmt.Sprintf("class(%d)", uint8(c))
}

const (
	KindSrvHello     Kind = 0x01
	KindSrvPeer      Kind = 0x02
	KindCtlLevels    Kind = 0x03
	KindCtlKick      Kind = 0x04
	KindCtlEnd       Kind = 0x05
	KindCtlAttention Kind = 0x06

	KindHsCommit   Kind = 0x10
	KindHsOwnerPub Kind = 0x11
	KindHsReveal   Kind = 0x12
	KindHsGrant    Kind = 0x13
	KindHsDecline  Kind = 0x14
	KindHsResume   Kind = 0x15
	KindHsConfirm  Kind = 0x16

	KindTermOut    Kind = 0x20
	KindTermKey    Kind = 0x21
	KindTermInput  Kind = 0x22
	KindTermStatus Kind = 0x23

	KindPropSubmit Kind = 0x30
	KindPropResult Kind = 0x31

	KindDocOpen     Kind = 0x40
	KindDocState    Kind = 0x41
	KindDocOp       Kind = 0x42
	KindDocAck      Kind = 0x43
	KindDocReject   Kind = 0x44
	KindDocBcast    Kind = 0x45
	KindDocSave     Kind = 0x46
	KindDocPresence Kind = 0x47
	KindDocClose    Kind = 0x48

	KindTreeReq  Kind = 0x50
	KindTreeList Kind = 0x51
	KindFsReq    Kind = 0x52
	KindFsResult Kind = 0x53

	KindRoster    Kind = 0x60
	KindRekey     Kind = 0x61
	KindEnded     Kind = 0x62
	KindSelfLevel Kind = 0x63
	KindNotice    Kind = 0x64
)

type kindInfo struct {
	kind     Kind
	name     string
	class    Class // class when a participant sends it (or the only class)
	minLevel Level // LevelNoPeer: participants never send it
}

// kindTable is the single source of the frame kind table; it must match
// docs/contract/workspace.md section 3 and server/src/do/workspace-frame.ts.
var kindTable = [...]kindInfo{
	{KindSrvHello, "srv_hello", ClassServer, LevelNoPeer},
	{KindSrvPeer, "srv_peer", ClassServer, LevelNoPeer},
	{KindCtlLevels, "ctl_levels", ClassServer, LevelNoPeer},
	{KindCtlKick, "ctl_kick", ClassServer, LevelNoPeer},
	{KindCtlEnd, "ctl_end", ClassServer, LevelNoPeer},
	{KindCtlAttention, "ctl_attention", ClassServer, LevelNoPeer},
	{KindHsCommit, "hs_commit", ClassHandshake, LevelNone},
	{KindHsOwnerPub, "hs_owner_pub", ClassHandshake, LevelNoPeer},
	{KindHsReveal, "hs_reveal", ClassHandshake, LevelNone},
	{KindHsGrant, "hs_grant", ClassToPeer, LevelNoPeer},
	{KindHsDecline, "hs_decline", ClassHandshake, LevelNoPeer},
	{KindHsResume, "hs_resume", ClassToOwner, LevelNone},
	{KindHsConfirm, "hs_confirm", ClassToOwner, LevelNone},
	{KindTermOut, "term_out", ClassBroadcast, LevelNoPeer},
	{KindTermKey, "term_key", ClassBroadcast, LevelNoPeer},
	{KindTermInput, "term_input", ClassToOwner, LevelKeyboard},
	{KindTermStatus, "term_status", ClassBroadcast, LevelNoPeer},
	{KindPropSubmit, "prop_submit", ClassToOwner, LevelPrompter},
	{KindPropResult, "prop_result", ClassToPeer, LevelNoPeer},
	{KindDocOpen, "doc_open", ClassToOwner, LevelViewer},
	{KindDocState, "doc_state", ClassToPeer, LevelNoPeer},
	{KindDocOp, "doc_op", ClassToOwner, LevelEditor},
	{KindDocAck, "doc_ack", ClassToPeer, LevelNoPeer},
	{KindDocReject, "doc_reject", ClassToPeer, LevelNoPeer},
	{KindDocBcast, "doc_bcast", ClassBroadcast, LevelNoPeer},
	{KindDocSave, "doc_save", ClassToOwner, LevelEditor},
	{KindDocPresence, "doc_presence", ClassToOwner, LevelViewer},
	{KindDocClose, "doc_close", ClassToOwner, LevelViewer},
	{KindTreeReq, "tree_req", ClassToOwner, LevelViewer},
	{KindTreeList, "tree_list", ClassToPeer, LevelNoPeer},
	{KindFsReq, "fs_req", ClassToOwner, LevelEditor},
	{KindFsResult, "fs_result", ClassToPeer, LevelNoPeer},
	{KindRoster, "roster", ClassBroadcast, LevelNoPeer},
	{KindRekey, "rekey", ClassToPeer, LevelNoPeer},
	{KindEnded, "ended", ClassBroadcast, LevelNoPeer},
	{KindSelfLevel, "self_level", ClassToOwner, LevelViewer},
	{KindNotice, "notice", ClassBroadcast, LevelNoPeer},
}

var kindIndex = func() map[Kind]*kindInfo {
	m := make(map[Kind]*kindInfo, len(kindTable))
	for i := range kindTable {
		m[kindTable[i].kind] = &kindTable[i]
	}
	return m
}()

// Kinds lists every known kind in table order.
func Kinds() []Kind {
	out := make([]Kind, len(kindTable))
	for i, k := range kindTable {
		out[i] = k.kind
	}
	return out
}

// Valid reports whether k is in the kind table.
func (k Kind) Valid() bool { return kindIndex[k] != nil }

func (k Kind) String() string {
	if ki := kindIndex[k]; ki != nil {
		return ki.name
	}
	return fmt.Sprintf("kind(0x%02x)", uint8(k))
}

// Class is the kind's class when a participant sends it (for kinds only the
// owner or server sends, its only class). Unknown kinds return 0.
func (k Kind) Class() Class {
	if ki := kindIndex[k]; ki != nil {
		return ki.class
	}
	return 0
}

// MinLevel is the lowest level at which a participant may send k to the
// owner; LevelNoPeer when participants never send it (and for unknown kinds).
func (k Kind) MinLevel() Level {
	if ki := kindIndex[k]; ki != nil {
		return ki.minLevel
	}
	return LevelNoPeer
}

// ClassFor is the class of k sent from seat from: doc_presence is a
// broadcast when the owner sends it and ToOwner otherwise.
func ClassFor(k Kind, from uint8) Class {
	if k == KindDocPresence && from == SeatOwner {
		return ClassBroadcast
	}
	return k.Class()
}
