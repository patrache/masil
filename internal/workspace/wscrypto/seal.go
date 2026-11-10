package wscrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	"github.com/patrache/masil/internal/workspace/wire"
)

// Direction is the first nonce byte; it keeps the three uses of a key apart.
type Direction uint8

const (
	DirBroadcast Direction = 0 // owner -> all, K_s
	DirO2P       Direction = 1 // owner -> one participant
	DirP2O       Direction = 2 // one participant -> owner
)

const (
	// RekeyAfter is the counter value at which the sender should switch keys.
	RekeyAfter uint64 = 1 << 40
	// TagLen is the GCM tag size.
	TagLen = 16
	// SignatureLen is the Ed25519 signature size appended to broadcasts.
	SignatureLen = 64
)

var (
	ErrReplay    = errors.New("wscrypto: replayed or reordered frame")
	ErrAuth      = errors.New("wscrypto: frame failed authentication")
	ErrExhausted = errors.New("wscrypto: nonce counter exhausted")
)

// MaxPlaintext is the largest plaintext that fits in one sealed frame with
// extra trailing bytes (SignatureLen for broadcasts, else 0).
func MaxPlaintext(extra int) int { return wire.MaxFrame - wire.HeaderLen - TagLen - extra }

// SessionKey is the broadcast key K_s and its epoch.
type SessionKey struct {
	Epoch uint32
	Key   [32]byte
}

// NewSessionKey makes a random K_s for epoch.
func NewSessionKey(epoch uint32) (SessionKey, error) {
	k := SessionKey{Epoch: epoch}
	if _, err := rand.Read(k.Key[:]); err != nil {
		return SessionKey{}, err
	}
	return k, nil
}

func nonce(dir Direction, seq uint64) [12]byte {
	var n [12]byte
	n[0] = byte(dir)
	binary.BigEndian.PutUint64(n[4:], seq)
	return n
}

func newGCM(key [32]byte) cipher.AEAD {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err) // 32-byte keys never fail
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return gcm
}

// Sealer seals one direction of one key with a counter nonce starting at 1.
// Safe for concurrent use.
type Sealer struct {
	mu      sync.Mutex
	aead    cipher.AEAD
	dir     Direction
	counter uint64 // last used
}

func NewSealer(key [32]byte, dir Direction) *Sealer {
	return &Sealer{aead: newGCM(key), dir: dir}
}

// Seal fills h.Seq with the next counter and h.Len with len(plaintext)+16+
// extra, then returns header ‖ ciphertext with the 20 header bytes as AAD.
// extra is the length of what the caller appends afterwards (SignatureLen
// for broadcasts, else 0). Frames over wire.MaxFrame fail with
// wire.ErrTooLarge without using a counter value.
func (s *Sealer) Seal(h wire.Header, plaintext []byte, extra int) ([]byte, error) {
	if extra < 0 || len(plaintext) > MaxPlaintext(extra) {
		return nil, wire.ErrTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counter == math.MaxUint64 {
		return nil, ErrExhausted
	}
	s.counter++
	h.Seq = s.counter
	h.Len = uint32(len(plaintext) + TagLen + extra)
	hb := h.Marshal()
	n := nonce(s.dir, h.Seq)
	out := make([]byte, wire.HeaderLen, wire.HeaderLen+len(plaintext)+TagLen+extra)
	copy(out, hb[:])
	return s.aead.Seal(out, n[:], plaintext, hb[:]), nil
}

// NeedsRekey reports whether the counter has reached RekeyAfter.
func (s *Sealer) NeedsRekey() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counter >= RekeyAfter
}

// Opener opens one direction of one key and rejects replayed or reordered
// counters. Safe for concurrent use.
type Opener struct {
	mu   sync.Mutex
	aead cipher.AEAD
	dir  Direction
	last uint64 // last opened seq
}

func NewOpener(key [32]byte, dir Direction) *Opener {
	return &Opener{aead: newGCM(key), dir: dir}
}

// Open authenticates and decrypts a sealed frame. extra trailing bytes
// (a signature) are counted in the header's Len; the frame may still carry
// them (they are ignored here — check them first with Verify) or already have
// them stripped. A failure leaves the replay window unchanged.
func (o *Opener) Open(frame []byte, extra int) (wire.Header, []byte, error) {
	h, body, err := parseSealed(frame, extra)
	if err != nil {
		return wire.Header{}, nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if h.Seq <= o.last {
		return wire.Header{}, nil, ErrReplay
	}
	n := nonce(o.dir, h.Seq)
	pt, err := o.aead.Open(nil, n[:], body, frame[:wire.HeaderLen])
	if err != nil {
		return wire.Header{}, nil, ErrAuth
	}
	o.last = h.Seq
	return h, pt, nil
}

// parseSealed returns the header and the ciphertext (tag included) of a
// frame whose Len counts extra trailing bytes that may or may not be present.
func parseSealed(frame []byte, extra int) (wire.Header, []byte, error) {
	if extra < 0 {
		return wire.Header{}, nil, wire.ErrLength
	}
	h, err := wire.ParseHeader(frame)
	if err != nil {
		return wire.Header{}, nil, err
	}
	have := len(frame) - wire.HeaderLen
	var ct []byte
	switch int(h.Len) {
	case have: // trailing bytes still attached
		if have < TagLen+extra {
			return wire.Header{}, nil, wire.ErrLength
		}
		ct = frame[wire.HeaderLen : len(frame)-extra]
	case have + extra: // already stripped
		ct = frame[wire.HeaderLen:]
	default:
		return wire.Header{}, nil, wire.ErrLength
	}
	if len(ct) < TagLen {
		return wire.Header{}, nil, wire.ErrLength
	}
	return h, ct, nil
}
