// Package wscrypto is the workspace share's end-to-end crypto
// (docs/contract/workspace.md 5-6): the committed X25519 key exchange with a
// 6-digit check number, directional AES-256-GCM sealing with counter nonces
// and replay rejection, and the owner's Ed25519 broadcast signatures.
//
// Standard library only. Keys live in memory and are never logged.
package wscrypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	// ErrWeakKey: the peer's public value is a low-order point (shared secret all zero).
	ErrWeakKey = errors.New("wscrypto: weak peer key")
	// ErrBadKey: a public value is not 32 bytes.
	ErrBadKey = errors.New("wscrypto: bad public key")
	// ErrTranscript: the transcript's public values are not this exchange's.
	ErrTranscript = errors.New("wscrypto: transcript does not match the keys")
)

const (
	commitLabel = "masil-ws-commit-v1"
	deriveLabel = "masil-ws-v1"
	okmLen      = 68
)

// KeyPair is an ephemeral X25519 key pair, one per join attempt.
type KeyPair struct {
	priv *ecdh.PrivateKey
}

// NewKeyPair makes a fresh X25519 key pair from crypto/rand.
func NewKeyPair() (*KeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{priv: priv}, nil
}

func keyPairFromPrivate(b []byte) (*KeyPair, error) {
	priv, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return nil, err
	}
	return &KeyPair{priv: priv}, nil
}

// Public is the 32-byte public value.
func (k *KeyPair) Public() []byte { return k.priv.PublicKey().Bytes() }

// Commit is SHA-256("masil-ws-commit-v1" ‖ 0 ‖ shareID ‖ 0 ‖ pub).
func Commit(shareID string, pub []byte) []byte {
	h := sha256.New()
	h.Write([]byte(commitLabel))
	h.Write([]byte{0})
	h.Write([]byte(shareID))
	h.Write([]byte{0})
	h.Write(pub)
	return h.Sum(nil)
}

// VerifyCommit checks commit against pub in constant time.
func VerifyCommit(shareID string, commit, pub []byte) bool {
	return subtle.ConstantTimeCompare(Commit(shareID, pub), commit) == 1
}

// Transcript is what both sides bind into the derived keys.
type Transcript struct {
	ShareID, OwnerID, ParticipantID string
	OwnerPub, ParticipantPub        []byte
}

func (t Transcript) info() string {
	var b bytes.Buffer
	b.WriteString(deriveLabel)
	b.WriteByte(0)
	b.WriteString(t.ShareID)
	b.WriteByte(0)
	b.WriteString(t.OwnerID)
	b.WriteByte(0)
	b.WriteString(t.ParticipantID)
	b.WriteByte(0)
	b.Write(t.OwnerPub)
	b.Write(t.ParticipantPub)
	return b.String()
}

// PairKeys are one owner-participant pair's directional keys and check number.
type PairKeys struct {
	O2P, P2O [32]byte
	SAS      string // "123 456"
}

// Derive computes the pair keys from the local key pair and the peer's public
// value. Both sides pass the same transcript (owner and participant public
// values in their fixed roles); local and peer must be its two public values.
func Derive(local *KeyPair, peerPub []byte, t Transcript) (PairKeys, error) {
	if len(peerPub) != 32 || len(t.OwnerPub) != 32 || len(t.ParticipantPub) != 32 {
		return PairKeys{}, ErrBadKey
	}
	mine := local.Public()
	switch {
	case bytes.Equal(mine, t.OwnerPub) && bytes.Equal(peerPub, t.ParticipantPub):
	case bytes.Equal(mine, t.ParticipantPub) && bytes.Equal(peerPub, t.OwnerPub):
	default:
		return PairKeys{}, ErrTranscript
	}
	pub, err := ecdh.X25519().NewPublicKey(peerPub)
	if err != nil {
		return PairKeys{}, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	shared, err := local.priv.ECDH(pub)
	if err != nil {
		// crypto/ecdh refuses low-order points (all-zero shared secret).
		return PairKeys{}, ErrWeakKey
	}
	if subtle.ConstantTimeCompare(shared, make([]byte, len(shared))) == 1 {
		return PairKeys{}, ErrWeakKey
	}
	okm, err := hkdf.Key(sha256.New, shared, nil, t.info(), okmLen)
	if err != nil {
		return PairKeys{}, err
	}
	var k PairKeys
	copy(k.O2P[:], okm[0:32])
	copy(k.P2O[:], okm[32:64])
	n := binary.BigEndian.Uint32(okm[64:68]) % 1_000_000
	k.SAS = fmt.Sprintf("%03d %03d", n/1000, n%1000)
	return k, nil
}
