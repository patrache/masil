package wscrypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/workspace/wire"
)

func pairKeys(t *testing.T) PairKeys {
	t.Helper()
	o, p := mustKP(t), mustKP(t)
	tr := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: p.Public()}
	k, err := Derive(o, p.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustSeal(t *testing.T, s *Sealer, h wire.Header, pt string, extra int) []byte {
	t.Helper()
	f, err := s.Seal(h, []byte(pt), extra)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSealOpenRoundTrip(t *testing.T) {
	k := pairKeys(t)
	s := NewSealer(k.P2O, DirP2O)
	o := NewOpener(k.P2O, DirP2O)
	for i := 1; i <= 3; i++ {
		pt := fmt.Sprintf(`{"n":%d}`, i)
		f := mustSeal(t, s, wire.Header{Kind: wire.KindDocOp, From: 2, To: wire.SeatOwner}, pt, 0)
		// the sealed frame is a valid wire frame
		if _, _, err := wire.Parse(f); err != nil {
			t.Fatal(err)
		}
		h, got, err := o.Open(f, 0)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != pt || h.Seq != uint64(i) || h.From != 2 || h.Kind != wire.KindDocOp {
			t.Fatalf("frame %d: %q %+v", i, got, h)
		}
	}
	// nonce layout: dir ‖ 0,0,0 ‖ seq
	n := nonce(DirP2O, 0x0102)
	if !bytes.Equal(n[:], []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2}) {
		t.Fatalf("nonce % x", n)
	}
}

func TestOpenRejectsReplayAndReorder(t *testing.T) {
	k := pairKeys(t)
	s := NewSealer(k.O2P, DirO2P)
	o := NewOpener(k.O2P, DirO2P)
	h := wire.Header{Kind: wire.KindDocAck, From: wire.SeatOwner, To: 1}
	f1, f2, f3 := mustSeal(t, s, h, "1", 0), mustSeal(t, s, h, "2", 0), mustSeal(t, s, h, "3", 0)
	for _, f := range [][]byte{f1, f2} {
		if _, _, err := o.Open(f, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := o.Open(f2, 0); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay f2: %v", err)
	}
	if _, _, err := o.Open(f1, 0); !errors.Is(err, ErrReplay) {
		t.Fatalf("reorder f1: %v", err)
	}
	if _, pt, err := o.Open(f3, 0); err != nil || string(pt) != "3" {
		t.Fatalf("f3: %q %v", pt, err)
	}
}

func TestTamperedHeaderOrBody(t *testing.T) {
	k := pairKeys(t)
	s := NewSealer(k.P2O, DirP2O)
	o := NewOpener(k.P2O, DirP2O)
	h := wire.Header{Kind: wire.KindTermInput, From: 1, To: wire.SeatOwner}
	f1 := mustSeal(t, s, h, "ls", 0)
	f2 := mustSeal(t, s, h, "rm", 0)
	f3 := mustSeal(t, s, h, "ok", 0)

	bad := bytes.Clone(f1)
	bad[3] ^= 1 // From
	if _, _, err := o.Open(bad, 0); !errors.Is(err, ErrAuth) {
		t.Fatalf("header flip: %v", err)
	}
	bad = bytes.Clone(f2)
	bad[wire.HeaderLen] ^= 0x80
	if _, _, err := o.Open(bad, 0); !errors.Is(err, ErrAuth) {
		t.Fatalf("body flip: %v", err)
	}
	// failures did not move the replay window: f1 (seq 1) still opens
	if _, pt, err := o.Open(f1, 0); err != nil || string(pt) != "ls" {
		t.Fatalf("f1 after tamper: %q %v", pt, err)
	}
	if _, pt, err := o.Open(f3, 0); err != nil || string(pt) != "ok" {
		t.Fatalf("f3: %q %v", pt, err)
	}
	// truncated frames are rejected, not panicked on
	if _, _, err := o.Open(f3[:wire.HeaderLen+3], 0); err == nil {
		t.Fatal("truncated frame opened")
	}
}

func TestDirectionKeysDiffer(t *testing.T) {
	k := pairKeys(t)
	f := mustSeal(t, NewSealer(k.O2P, DirO2P), wire.Header{Kind: wire.KindTreeList, To: 1}, "x", 0)
	if _, _, err := NewOpener(k.P2O, DirP2O).Open(f, 0); !errors.Is(err, ErrAuth) {
		t.Fatalf("P2O key: %v", err)
	}
	if _, _, err := NewOpener(k.O2P, DirBroadcast).Open(f, 0); !errors.Is(err, ErrAuth) {
		t.Fatalf("broadcast dir: %v", err)
	}
	if _, _, err := NewOpener(k.O2P, DirO2P).Open(f, 0); err != nil {
		t.Fatalf("right key: %v", err)
	}
}

func broadcast(t *testing.T, s *Sealer, sg *Signer, pt string) []byte {
	t.Helper()
	f := mustSeal(t, s, wire.Header{Kind: wire.KindTermOut, From: wire.SeatOwner, To: wire.SeatAll}, pt, SignatureLen)
	return sg.Sign(f)
}

func TestBroadcastSignature(t *testing.T) {
	ks, err := NewSessionKey(1)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	ownerSealer := NewSealer(ks.Key, DirBroadcast)
	signed := broadcast(t, ownerSealer, owner, "screen")
	if _, _, err := wire.Parse(signed); err != nil {
		t.Fatalf("signed frame is not a wire frame: %v", err)
	}
	frame, err := Verify(owner.Public(), signed)
	if err != nil {
		t.Fatal(err)
	}
	rx := NewOpener(ks.Key, DirBroadcast)
	if _, pt, err := rx.Open(frame, SignatureLen); err != nil || string(pt) != "screen" {
		t.Fatalf("open verified: %q %v", pt, err)
	}
	// Open also accepts the frame with the signature still attached (it ignores it)
	signed2 := broadcast(t, ownerSealer, owner, "screen2")
	if _, pt, err := rx.Open(signed2, SignatureLen); err != nil || string(pt) != "screen2" {
		t.Fatalf("open signed: %q %v", pt, err)
	}

	// participant A knows K_s but not the owner's signing key
	forger, _ := NewSigner()
	aSealer := NewSealer(ks.Key, DirBroadcast)
	setCounterForTest(aSealer, 100)
	forged := broadcast(t, aSealer, forger, "rm -rf")
	if _, err := Verify(owner.Public(), forged); !errors.Is(err, ErrSignature) {
		t.Fatalf("forged: %v", err)
	}
	// a stripped or truncated signature also fails
	unsigned := mustSeal(t, aSealer, wire.Header{Kind: wire.KindTermOut, To: wire.SeatAll}, "x", SignatureLen)
	if _, err := Verify(owner.Public(), unsigned); !errors.Is(err, ErrSignature) {
		t.Fatalf("unsigned: %v", err)
	}
	if _, err := Verify(owner.Public(), signed[:10]); !errors.Is(err, ErrSignature) {
		t.Fatalf("short: %v", err)
	}
	// tampering with a signed frame breaks the signature
	bad := bytes.Clone(signed2)
	bad[wire.HeaderLen+1] ^= 1
	if _, err := Verify(owner.Public(), bad); !errors.Is(err, ErrSignature) {
		t.Fatalf("tampered: %v", err)
	}
}

func TestRekeyExcludesKicked(t *testing.T) {
	owner, _ := NewSigner()
	k1, _ := NewSessionKey(1)
	tx1 := NewSealer(k1.Key, DirBroadcast)
	a1, b1 := NewOpener(k1.Key, DirBroadcast), NewOpener(k1.Key, DirBroadcast)
	for i := range 2 {
		f := broadcast(t, tx1, owner, fmt.Sprint(i))
		for _, o := range []*Opener{a1, b1} {
			if _, _, err := o.Open(f, SignatureLen); err != nil {
				t.Fatal(err)
			}
		}
	}
	// kick B: new epoch key goes to A only
	k2, _ := NewSessionKey(2)
	if k2.Epoch != 2 || k2.Key == k1.Key {
		t.Fatalf("rekey: %+v", k2)
	}
	tx2 := NewSealer(k2.Key, DirBroadcast)
	a2 := NewOpener(k2.Key, DirBroadcast)
	authFails := 0
	for i := range 5 {
		f := broadcast(t, tx2, owner, fmt.Sprint("after", i))
		if _, _, err := a2.Open(f, SignatureLen); err != nil {
			t.Fatalf("A cannot open epoch 2: %v", err)
		}
		_, _, err := b1.Open(f, SignatureLen)
		switch {
		case errors.Is(err, ErrAuth):
			authFails++
		case errors.Is(err, ErrReplay):
		default:
			t.Fatalf("kicked B opened an epoch-2 frame: %v", err)
		}
	}
	if authFails == 0 {
		t.Fatal("expected ErrAuth for epoch-2 frames beyond B's window")
	}
}

func TestNeedsRekey(t *testing.T) {
	var key [32]byte
	rand.Read(key[:])
	s := NewSealer(key, DirO2P)
	setCounterForTest(s, RekeyAfter-1)
	if s.NeedsRekey() {
		t.Fatal("NeedsRekey before the limit")
	}
	f := mustSeal(t, s, wire.Header{Kind: wire.KindDocAck, To: 1}, "x", 0)
	if !s.NeedsRekey() {
		t.Fatal("NeedsRekey after reaching the limit")
	}
	h, _, _ := wire.Parse(f)
	if h.Seq != RekeyAfter {
		t.Fatalf("seq = %d", h.Seq)
	}
}

func TestSealTooLarge(t *testing.T) {
	k := pairKeys(t)
	s := NewSealer(k.O2P, DirO2P)
	_, err := s.Seal(wire.Header{Kind: wire.KindTreeList, To: 1}, make([]byte, wire.MaxFrame), 0)
	if !errors.Is(err, wire.ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	// the failed seal did not burn a counter value
	f := mustSeal(t, s, wire.Header{Kind: wire.KindTreeList, To: 1}, "x", 0)
	if h, _, _ := wire.Parse(f); h.Seq != 1 {
		t.Fatalf("seq = %d", h.Seq)
	}
	if MaxPlaintext(0) != wire.MaxFrame-wire.HeaderLen-16 || MaxPlaintext(SignatureLen) != wire.MaxFrame-wire.HeaderLen-16-64 {
		t.Fatal("MaxPlaintext")
	}
}
