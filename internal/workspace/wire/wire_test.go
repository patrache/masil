package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	h := Header{Kind: KindDocOp, Flags: FlagMore, From: 3, To: SeatOwner, Seq: 1<<40 + 7, Len: 5}
	b := h.Marshal()
	got, payload, err := Parse(append(b[:], "hello"...))
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("header = %+v, want %+v", got, h)
	}
	if string(payload) != "hello" {
		t.Fatalf("payload = %q", payload)
	}
	if b[0] != 1 || b[1] != byte(KindDocOp) || b[2] != FlagMore || b[3] != 3 || b[4] != SeatOwner {
		t.Fatalf("layout = % x", b)
	}
	if !bytes.Equal(b[5:8], []byte{0, 0, 0}) {
		t.Fatalf("reserved = % x", b[5:8])
	}
	if binary.BigEndian.Uint64(b[8:16]) != h.Seq || binary.BigEndian.Uint32(b[16:20]) != 5 {
		t.Fatalf("seq/len = % x", b[8:20])
	}
}

func frameOf(h Header, payload []byte) []byte {
	b := h.Marshal()
	return append(b[:], payload...)
}

func TestParseRejects(t *testing.T) {
	good := frameOf(Header{Kind: KindTermOut, Len: 5}, []byte("hello"))

	v2 := bytes.Clone(good)
	v2[0] = 2
	if _, _, err := Parse(v2); !errors.Is(err, ErrVersion) {
		t.Errorf("version 2: %v", err)
	}

	short := frameOf(Header{Kind: KindTermOut, Len: 5}, []byte("hell"))
	if _, _, err := Parse(short); !errors.Is(err, ErrLength) {
		t.Errorf("len mismatch: %v", err)
	}

	big := frameOf(Header{Kind: KindTermOut, Len: MaxFrame - HeaderLen + 1}, make([]byte, MaxFrame-HeaderLen+1))
	if _, _, err := Parse(big); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}

	res := bytes.Clone(good)
	res[6] = 1
	if _, _, err := Parse(res); !errors.Is(err, ErrReserved) {
		t.Errorf("reserved: %v", err)
	}

	if _, _, err := Parse(good[:19]); !errors.Is(err, ErrLength) {
		t.Errorf("short header: %v", err)
	}

	exact := frameOf(Header{Kind: KindTermOut, Len: MaxFrame - HeaderLen}, make([]byte, MaxFrame-HeaderLen))
	if _, _, err := Parse(exact); err != nil {
		t.Errorf("exactly MaxFrame: %v", err)
	}
}

func TestChunkReassemble(t *testing.T) {
	payload := make([]byte, 600<<10)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	h := Header{Kind: KindTreeList, From: SeatOwner, To: 2}
	chunks := Chunk(h, payload, MaxFrame)
	if len(chunks) < 3 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	var r Reassembler
	doneCount := 0
	var full []byte
	for i, c := range chunks {
		if len(c) > MaxFrame {
			t.Fatalf("chunk %d len %d", i, len(c))
		}
		ch, p, err := Parse(c)
		if err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
		more := ch.Flags&FlagMore != 0
		if more != (i < len(chunks)-1) {
			t.Fatalf("chunk %d FlagMore=%v", i, more)
		}
		got, done, err := r.Add(ch, p)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			doneCount++
			full = got
		}
	}
	if doneCount != 1 || !bytes.Equal(full, payload) {
		t.Fatalf("done %d times, equal=%v", doneCount, bytes.Equal(full, payload))
	}

	// a single small message passes straight through
	one := Chunk(Header{Kind: KindRoster}, []byte("{}"), MaxFrame)
	if len(one) != 1 {
		t.Fatalf("small chunks = %d", len(one))
	}
	ch, p, _ := Parse(one[0])
	if got, done, err := r.Add(ch, p); err != nil || !done || string(got) != "{}" {
		t.Fatalf("small: %q %v %v", got, done, err)
	}
	// empty payload still yields one frame
	if n := len(Chunk(Header{Kind: KindHsResume}, nil, MaxFrame)); n != 1 {
		t.Fatalf("empty chunks = %d", n)
	}
}

func TestReassemblerLimit(t *testing.T) {
	var r Reassembler
	piece := make([]byte, 1<<20)
	h := Header{Kind: KindTermKey, Flags: FlagMore, From: 1}
	var err error
	for i := 0; i <= 64 && err == nil; i++ {
		_, _, err = r.Add(h, piece)
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	// the buffer was dropped: a fresh message works
	h.Flags = 0
	if got, done, err := r.Add(h, []byte("x")); err != nil || !done || string(got) != "x" {
		t.Fatalf("after limit: %q %v %v", got, done, err)
	}
}

func TestLevel(t *testing.T) {
	for _, l := range []Level{LevelNone, LevelViewer, LevelPrompter, LevelEditor, LevelKeyboard} {
		got, ok := ParseLevel(l.String())
		if !ok || got != l {
			t.Errorf("ParseLevel(%q) = %v %v", l.String(), got, ok)
		}
	}
	if LevelViewer.String() != "viewer" || LevelKeyboard.String() != "keyboard" || LevelNone.String() != "" {
		t.Fatal("level names")
	}
	if _, ok := ParseLevel("admin"); ok {
		t.Fatal("unknown level accepted")
	}
	if !(LevelNone < LevelViewer && LevelViewer < LevelPrompter && LevelPrompter < LevelEditor && LevelEditor < LevelKeyboard) {
		t.Fatal("level order")
	}
}

func TestClassFor(t *testing.T) {
	if ClassFor(KindDocPresence, SeatOwner) != ClassBroadcast || ClassFor(KindDocPresence, 2) != ClassToOwner {
		t.Fatal("doc_presence direction")
	}
	if ClassFor(KindTermOut, 3) != ClassBroadcast || ClassFor(KindDocOp, SeatOwner) != ClassToOwner {
		t.Fatal("ClassFor should equal Class for other kinds")
	}
}

func TestTermPayload(t *testing.T) {
	p := TermPayload(2, []byte("ls\r"))
	id, b, err := SplitTerm(p)
	if err != nil || id != 2 || string(b) != "ls\r" {
		t.Fatalf("%d %q %v", id, b, err)
	}
	if _, _, err := SplitTerm(nil); !errors.Is(err, ErrLength) {
		t.Fatalf("empty: %v", err)
	}
}
