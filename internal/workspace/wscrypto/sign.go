package wscrypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"

	"github.com/patrache/masil/internal/workspace/wire"
)

var ErrSignature = errors.New("wscrypto: bad owner signature")

// Signer is the owner's per-share ephemeral Ed25519 key.
type Signer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func NewSigner() (*Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Signer{priv: priv, pub: pub}, nil
}

func (s *Signer) Public() ed25519.PublicKey { return s.pub }

// Sign returns frame ‖ signature over the whole frame (header ‖ ciphertext).
// frame must have been sealed with extra = SignatureLen.
func (s *Signer) Sign(frame []byte) []byte {
	sig := ed25519.Sign(s.priv, frame)
	out := make([]byte, 0, len(frame)+SignatureLen)
	out = append(out, frame...)
	return append(out, sig...)
}

// Verify checks a signed broadcast frame and returns it without the
// signature. The signed frame must be a whole wire frame (its Len counts the
// signature); anything else fails with ErrSignature.
func Verify(pub ed25519.PublicKey, signed []byte) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, ErrSignature
	}
	if _, _, err := wire.Parse(signed); err != nil || len(signed) < wire.HeaderLen+TagLen+SignatureLen {
		return nil, ErrSignature
	}
	cut := len(signed) - SignatureLen
	if !ed25519.Verify(pub, signed[:cut], signed[cut:]) {
		return nil, ErrSignature
	}
	return signed[:cut], nil
}
