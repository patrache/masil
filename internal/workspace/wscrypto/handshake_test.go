package wscrypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
)

func mustKP(t *testing.T) *KeyPair {
	t.Helper()
	k, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func seqBytes(from byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = from + byte(i)
	}
	return b
}

var sasRE = regexp.MustCompile(`^\d{3} \d{3}$`)

func TestDeriveAgrees(t *testing.T) {
	o, p := mustKP(t), mustKP(t)
	if len(o.Public()) != 32 {
		t.Fatalf("public len %d", len(o.Public()))
	}
	tr := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: p.Public()}
	ko, err := Derive(o, p.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := Derive(p, o.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if ko != kp {
		t.Fatalf("owner and participant derived different keys")
	}
	if len(ko.SAS) != 7 || !sasRE.MatchString(ko.SAS) {
		t.Fatalf("SAS %q", ko.SAS)
	}
	if ko.O2P == ko.P2O {
		t.Fatal("O2P == P2O")
	}
}

func TestDeriveVector(t *testing.T) {
	o, err := keyPairFromPrivate(seqBytes(1))
	if err != nil {
		t.Fatal(err)
	}
	p, err := keyPairFromPrivate(seqBytes(33))
	if err != nil {
		t.Fatal(err)
	}
	tr := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: p.Public()}
	k, err := Derive(o, p.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/vector.txt")
	if err != nil {
		t.Fatalf("%v (got SAS=%s O2P=%s)", err, k.SAS, hex.EncodeToString(k.O2P[:]))
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("vector.txt needs 2 lines")
	}
	if k.SAS != strings.TrimSpace(lines[0]) {
		t.Errorf("SAS = %q, vector %q", k.SAS, lines[0])
	}
	if got := hex.EncodeToString(k.O2P[:]); got != strings.TrimSpace(lines[1]) {
		t.Errorf("O2P = %s, vector %s", got, lines[1])
	}
}

func TestMITMChangesSAS(t *testing.T) {
	same := 0
	for range 1000 {
		o, m1, m2, p := mustKP(t), mustKP(t), mustKP(t), mustKP(t)
		// the server (mitm) pretends to be the participant to the owner and the owner to the participant
		trO := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: m1.Public()}
		trP := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: m2.Public(), ParticipantPub: p.Public()}
		ko, err := Derive(o, m1.Public(), trO)
		if err != nil {
			t.Fatal(err)
		}
		kp, err := Derive(p, m2.Public(), trP)
		if err != nil {
			t.Fatal(err)
		}
		if ko.SAS == kp.SAS {
			same++
		}
	}
	if same > 1 {
		t.Fatalf("SAS matched under MITM %d times out of 1000", same)
	}
}

func TestTranscriptBindsIDs(t *testing.T) {
	o, p := mustKP(t), mustKP(t)
	tr := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: p.Public()}
	a, err := Derive(o, p.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	tr.ParticipantID = "U_Q"
	b, err := Derive(o, p.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if a.SAS == b.SAS && a.O2P == b.O2P {
		t.Fatal("participant id not bound into the keys")
	}
	tr.ParticipantID, tr.ShareID = "U_P", "S2"
	c, err := Derive(o, p.Public(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if a.O2P == c.O2P {
		t.Fatal("share id not bound into the keys")
	}
}

func TestDeriveRejectsForeignTranscript(t *testing.T) {
	o, p, q := mustKP(t), mustKP(t), mustKP(t)
	tr := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: q.Public()}
	if _, err := Derive(o, p.Public(), tr); !errors.Is(err, ErrTranscript) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommit(t *testing.T) {
	a, b := mustKP(t), mustKP(t)
	c := Commit("S1", a.Public())
	if len(c) != 32 {
		t.Fatalf("commit len %d", len(c))
	}
	if !VerifyCommit("S1", c, a.Public()) {
		t.Fatal("own commit rejected")
	}
	if VerifyCommit("S1", c, b.Public()) {
		t.Fatal("other pub accepted")
	}
	if VerifyCommit("S2", c, a.Public()) {
		t.Fatal("other share accepted")
	}
	if VerifyCommit("S1", c[:31], a.Public()) {
		t.Fatal("short commit accepted")
	}
	if bytes.Equal(Commit("S1", a.Public()), Commit("S", append([]byte("1"), a.Public()...))) {
		t.Fatal("commit input is ambiguous")
	}
}

func TestWeakKey(t *testing.T) {
	o := mustKP(t)
	zero := make([]byte, 32)
	tr := Transcript{ShareID: "S1", OwnerID: "U_O", ParticipantID: "U_P", OwnerPub: o.Public(), ParticipantPub: zero}
	if _, err := Derive(o, zero, tr); !errors.Is(err, ErrWeakKey) {
		t.Fatalf("err = %v", err)
	}
}
