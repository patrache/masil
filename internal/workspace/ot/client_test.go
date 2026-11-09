package ot

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestClientOnePendingAtATime(t *testing.T) {
	c := NewClient("2", 5, []rune("abc"))
	a := Insert(3, "d", 3)
	p, err := c.Local(a)
	if err != nil || p == nil {
		t.Fatalf("first local: %v %v", p, err)
	}
	if !strings.HasPrefix(p.ClientOpID, "2-") || !strings.HasSuffix(p.ClientOpID, "-1") || p.BaseSeq != 5 || !reflect.DeepEqual(p.Op, a) {
		t.Fatalf("pending %+v", p)
	}
	b := Insert(4, "e", 4)
	if p2, err := c.Local(b); err != nil || p2 != nil {
		t.Fatalf("second local should buffer: %v %v", p2, err)
	}
	b2 := Insert(0, "Z", 5)
	if p3, err := c.Local(b2); err != nil || p3 != nil {
		t.Fatalf("third local should buffer: %v %v", p3, err)
	}
	if string(c.Text()) != "Zabcde" || !c.Busy() {
		t.Fatalf("text %q", string(c.Text()))
	}
	if _, err := c.Ack(p.ClientOpID+"9", 6); !errors.Is(err, ErrUnexpectedAck) {
		t.Fatalf("wrong ack: %v", err)
	}
	next, err := c.Ack(p.ClientOpID, 6)
	if err != nil || next == nil {
		t.Fatalf("ack: %v %v", next, err)
	}
	want, _ := Compose(b, b2)
	if next.ClientOpID != strings.TrimSuffix(p.ClientOpID, "1")+"2" || next.BaseSeq != 6 || !reflect.DeepEqual(next.Op, want) {
		t.Fatalf("next %+v want op %v", next, want)
	}
	if c.Seq() != 6 {
		t.Fatalf("seq %d", c.Seq())
	}
	if last, err := c.Ack(next.ClientOpID, 7); err != nil || last != nil || c.Busy() {
		t.Fatalf("final ack: %v %v", last, err)
	}
}

func TestClientRemoteTransforms(t *testing.T) {
	c := NewClient("1", 0, []rune("abc"))
	first, _ := c.Local(Insert(0, "X", 3)) // pending
	c.Local(Insert(4, "Y", 4))             // buffer: "XabcY"
	m, err := c.Remote(1, Insert(1, "R", 3))
	if err != nil {
		t.Fatal(err)
	}
	if string(c.Text()) != "XaRbcY" {
		t.Fatalf("text %q (applied %v)", string(c.Text()), m)
	}
	if c.Seq() != 1 {
		t.Fatalf("seq %d", c.Seq())
	}
	// the sequencer agrees once our ops arrive
	s := NewSequencer([]rune("abc"))
	s.Submit(0, Insert(1, "R", 3))
	_, _, err = s.Submit(0, Insert(0, "X", 3))
	if err != nil {
		t.Fatal(err)
	}
	next, _ := c.Ack(first.ClientOpID, 2)
	if _, _, err := s.Submit(next.BaseSeq, next.Op); err != nil {
		t.Fatal(err)
	}
	if string(s.Text()) != string(c.Text()) {
		t.Fatalf("seq %q client %q", string(s.Text()), string(c.Text()))
	}
}

func TestDropAfterReject(t *testing.T) {
	c := NewClient("1", 3, []rune("abc"))
	c.Local(Insert(0, "X", 3))
	c.Local(Insert(0, "Y", 4))
	c.Drop(9, []rune("server"))
	if string(c.Text()) != "server" || c.Busy() || c.Seq() != 9 {
		t.Fatalf("after drop: %q busy=%v seq=%d", string(c.Text()), c.Busy(), c.Seq())
	}
	p, err := c.Local(Insert(6, "!", 6))
	if err != nil || p == nil || p.BaseSeq != 9 {
		t.Fatalf("local after drop: %+v %v", p, err)
	}
}

func TestResetRelaysUnconfirmed(t *testing.T) {
	c := NewClient("2", 0, []rune("hello world"))
	first, _ := c.Local(Insert(5, ",", 11)) // pending, lost with the socket
	c.Local(Insert(12, "!", 12))
	if id := c.Resync(); id != first.ClientOpID {
		t.Fatalf("resync id %q", id)
	}
	// while resyncing, local edits buffer and inbound acks/remotes are ignored
	if p, _ := c.Local(Insert(0, ">", 13)); p != nil {
		t.Fatal("sent while resyncing")
	}
	if p, err := c.Ack(first.ClientOpID, 1); p != nil || err != nil {
		t.Fatalf("ack while resyncing: %v %v", p, err)
	}
	p, err := c.Reset(1, []rune("hello brave world"))
	if err != nil || p == nil || p.BaseSeq != 1 {
		t.Fatalf("reset: %+v %v", p, err)
	}
	if got := string(c.Text()); got != ">hello, brave world!" {
		t.Fatalf("text %q", got)
	}
	s := NewSequencer([]rune("hello world"))
	s.Submit(0, Insert(6, "brave ", 11))
	if _, _, err := s.Submit(p.BaseSeq, p.Op); err != nil || string(s.Text()) != string(c.Text()) {
		t.Fatalf("server %q client %q %v", string(s.Text()), string(c.Text()), err)
	}
}

func TestResetAckedDoesNotDuplicate(t *testing.T) {
	c := NewClient("2", 0, []rune("ab"))
	c.Local(Insert(2, "c", 2)) // pending, applied by the server but the ack was lost
	c.Local(Insert(3, "d", 3)) // buffer
	c.Resync()
	p, err := c.ResetAcked(2, []rune("Xabc"))
	if err != nil || p == nil {
		t.Fatalf("%+v %v", p, err)
	}
	if got := string(c.Text()); got != "Xabcd" {
		t.Fatalf("text %q", got)
	}
}

func TestSequencer(t *testing.T) {
	s := NewSequencer([]rune("ab"))
	if s.Seq() != 0 {
		t.Fatal("seq starts at 0")
	}
	seq, applied, err := s.SubmitID(1, "1-1", 0, Insert(0, "X", 2))
	if err != nil || seq != 1 || string(s.Text()) != "Xab" || !reflect.DeepEqual(applied, Insert(0, "X", 2)) {
		t.Fatalf("%d %v %v", seq, applied, err)
	}
	// concurrent op on base 0 is transformed; same-position tie: the earlier one first
	seq, applied, err = s.Submit(0, Insert(0, "Y", 2))
	if err != nil || seq != 2 || string(s.Text()) != "XYab" {
		t.Fatalf("%d %v %q %v", seq, applied, string(s.Text()), err)
	}
	if seq, _, err := s.SubmitID(1, "1-1", 0, Insert(0, "X", 2)); !errors.Is(err, ErrDuplicate) || seq != 1 {
		t.Fatalf("duplicate: %d %v", seq, err)
	}
	if seq, ok := s.AppliedID(1, "1-1"); !ok || seq != 1 {
		t.Fatal("AppliedID")
	}
	if _, _, err := s.Submit(3, Noop(4)); !errors.Is(err, ErrFutureBase) {
		t.Fatalf("future: %v", err)
	}
	if _, _, err := s.Submit(2, Noop(3)); !errors.Is(err, ErrBaseLen) {
		t.Fatalf("base len: %v", err)
	}
	ops, err := s.Since(0)
	if err != nil || len(ops) != 2 {
		t.Fatalf("since: %v %v", ops, err)
	}
	if _, err := s.Since(5); !errors.Is(err, ErrFutureBase) {
		t.Fatalf("since future: %v", err)
	}
}

func TestSequencerStaleBase(t *testing.T) {
	s := NewSequencer(nil)
	for i := range HistoryLimit + 3 {
		if _, _, err := s.Submit(int64(i), Insert(0, "a", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Submit(2, Noop(3)); !errors.Is(err, ErrStaleBase) {
		t.Fatalf("stale: %v", err)
	}
	if _, err := s.Since(2); !errors.Is(err, ErrStaleBase) {
		t.Fatalf("since stale: %v", err)
	}
	if _, _, err := s.Submit(3, Insert(0, "b", 3)); err != nil {
		t.Fatalf("oldest retained base: %v", err)
	}
}

// A reopened document builds a new Client with the same prefix; its ids must
// not collide with the old Client's ids still in the sequencer's history,
// or the new edit is re-acked and never applied (P2 review).
func TestNewClientIDsDoNotCollide(t *testing.T) {
	s := NewSequencer([]rune("ab"))
	old := NewClient("1", 0, []rune("ab"))
	p1, _ := old.Local(Insert(0, "X", 2))
	if !strings.HasPrefix(p1.ClientOpID, "1-") {
		t.Fatalf("id %q keeps the prefix", p1.ClientOpID)
	}
	if _, _, err := s.SubmitID(1, p1.ClientOpID, p1.BaseSeq, p1.Op); err != nil {
		t.Fatal(err)
	}
	reopened := NewClient("1", s.Seq(), s.Text())
	p2, _ := reopened.Local(Insert(3, "Y", 3))
	if p2.ClientOpID == p1.ClientOpID {
		t.Fatalf("same id %q from two clients", p1.ClientOpID)
	}
	if _, _, err := s.SubmitID(1, p2.ClientOpID, p2.BaseSeq, p2.Op); err != nil {
		t.Fatalf("second client's edit: %v", err)
	}
	if got := string(s.Text()); got != "XabY" {
		t.Fatalf("text %q", got)
	}
}

// Duplicate detection is per sender: another seat reusing an id is a new edit.
func TestSequencerIDsScopedBySeat(t *testing.T) {
	s := NewSequencer([]rune("abc"))
	if _, _, err := s.SubmitID(2, "U-1", 0, Insert(0, "X", 3)); err != nil {
		t.Fatal(err)
	}
	seq, _, err := s.SubmitID(1, "U-1", 0, Insert(3, "VICTIM", 3))
	if err != nil || seq != 2 || string(s.Text()) != "XabcVICTIM" {
		t.Fatalf("victim: %d %v %q", seq, err, string(s.Text()))
	}
	if seq, _, err := s.SubmitID(1, "U-1", 0, Insert(3, "VICTIM", 3)); !errors.Is(err, ErrDuplicate) || seq != 2 {
		t.Fatalf("same seat retransmit: %d %v", seq, err)
	}
	if seq, ok := s.AppliedID(2, "U-1"); !ok || seq != 1 {
		t.Fatalf("AppliedID seat 2: %d %v", seq, ok)
	}
	if _, ok := s.AppliedID(3, "U-1"); ok {
		t.Fatal("AppliedID leaks across seats")
	}
}
