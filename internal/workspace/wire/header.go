// Package wire is the byte format of the workspace share socket
// (docs/contract/workspace.md): the 20-byte outer header, the frame kind
// table, participation levels, chunking and the inner JSON messages.
//
// It holds no keys and does no crypto; sealing lives in wscrypto.
package wire

import (
	"encoding/binary"
	"errors"
)

const (
	// Version is the header version byte.
	Version = 1
	// HeaderLen is the size of the outer header.
	HeaderLen = 20
	// MaxFrame is the largest frame, header included.
	MaxFrame = 256 << 10
)

// Seats.
const (
	SeatOwner  uint8 = 0
	SeatServer uint8 = 254
	SeatAll    uint8 = 255
	// MaxSeat is the highest participant seat (participants are 1..MaxSeat).
	MaxSeat uint8 = 8
)

// Header flags.
const (
	FlagMore       uint8 = 1 << 0 // more chunks of this message follow
	FlagGroupStart uint8 = 1 << 1 // first frame of a keyframe group
)

var (
	ErrVersion  = errors.New("wire: unsupported version")
	ErrLength   = errors.New("wire: bad length")
	ErrTooLarge = errors.New("wire: frame too large")
	ErrReserved = errors.New("wire: reserved bytes not zero")
)

// Header is the unencrypted outer header. Len is the number of bytes after
// the header (ciphertext, GCM tag and signature included).
type Header struct {
	Kind     Kind
	Flags    uint8
	From, To uint8
	Seq      uint64
	Len      uint32
}

// Marshal lays the header out as [0]=Version [1]=Kind [2]=Flags [3]=From
// [4]=To [5:8]=0 [8:16]=Seq [16:20]=Len (big endian).
func (h Header) Marshal() [HeaderLen]byte {
	var b [HeaderLen]byte
	b[0] = Version
	b[1] = byte(h.Kind)
	b[2] = h.Flags
	b[3] = h.From
	b[4] = h.To
	binary.BigEndian.PutUint64(b[8:16], h.Seq)
	binary.BigEndian.PutUint32(b[16:20], h.Len)
	return b
}

// Parse splits a frame into its header and the bytes after it.
// The returned payload aliases frame.
func Parse(frame []byte) (Header, []byte, error) {
	if len(frame) < HeaderLen {
		return Header{}, nil, ErrLength
	}
	if frame[0] != Version {
		return Header{}, nil, ErrVersion
	}
	if len(frame) > MaxFrame {
		return Header{}, nil, ErrTooLarge
	}
	if frame[5] != 0 || frame[6] != 0 || frame[7] != 0 {
		return Header{}, nil, ErrReserved
	}
	h := Header{
		Kind:  Kind(frame[1]),
		Flags: frame[2],
		From:  frame[3],
		To:    frame[4],
		Seq:   binary.BigEndian.Uint64(frame[8:16]),
		Len:   binary.BigEndian.Uint32(frame[16:20]),
	}
	if uint64(h.Len) != uint64(len(frame)-HeaderLen) {
		return Header{}, nil, ErrLength
	}
	return h, frame[HeaderLen:], nil
}
