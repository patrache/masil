package ot

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// The convergence fuzz of spec B16 #3: one Sequencer (the owner's) and three
// Clients ("0" = the owner's own editor, "1", "2") over FIFO links with random
// delay; client 2 drops off and comes back via Resync/Reset.

type msgKind int

const (
	mOp     msgKind = iota // client -> sequencer
	mReopen                // client -> sequencer: resync, carrying the pending id
	mAck                   // sequencer -> client
	mRemote                // sequencer -> client
	mState                 // sequencer -> client: full state after a reopen
)

type msg struct {
	kind msgKind
	at   int // delivery step
	p    Pending
	id   string
	seq  int64
	op   Op
	text []rune
	// mState: the pending id named in the reopen was already sequenced
	pendingApplied bool
}

type link struct{ q []msg }

func (l *link) push(r *rand.Rand, now int, m msg) {
	m.at = now + r.Intn(21)
	if n := len(l.q); n > 0 && l.q[n-1].at > m.at {
		m.at = l.q[n-1].at // FIFO: never overtake
	}
	l.q = append(l.q, m)
}

func (l *link) pop(now int) (msg, bool) {
	if len(l.q) == 0 || l.q[0].at > now {
		return msg{}, false
	}
	m := l.q[0]
	l.q = l.q[1:]
	return m, true
}

type peer struct {
	c          *Client
	out, in    link
	online     bool
	subscribed bool // sequencer sends remotes only after the reopen is handled
	backAt     int
}

const markerBase = 0xE000

type fuzzStats struct{ markers, reconnects, resets, acked, remotes int }

type fuzzWorld struct {
	stats   fuzzStats
	t       *testing.T
	r       *rand.Rand
	seqr    *Sequencer
	peers   []*peer
	now     int
	markers int
	deleted map[rune]bool
}

func (w *fuzzWorld) send(p *peer, pend *Pending) {
	if pend == nil {
		return
	}
	if p.online {
		p.out.push(w.r, w.now, msg{kind: mOp, p: *pend})
	} // offline: the frame is lost with the socket
}

func (w *fuzzWorld) localEdit(i int) {
	p := w.peers[i]
	text := p.c.Text()
	var op Op
	if len(text) > 0 && w.r.Intn(10) < 4 {
		at := w.pos(len(text))
		n := 1 + w.r.Intn(min(3, len(text)-at))
		if at+n > len(text) {
			n = len(text) - at
		}
		if n <= 0 {
			return
		}
		for _, ru := range text[at : at+n] {
			if ru >= markerBase {
				w.deleted[ru] = true
			}
		}
		op = Delete(at, n, len(text))
	} else {
		at := w.pos(len(text) + 1)
		s := randString(w.r, 2) + string(rune(markerBase+w.markers))
		w.markers++
		op = Insert(min(at, len(text)), s, len(text))
	}
	pend, err := p.c.Local(op)
	if err != nil {
		w.t.Fatalf("local %d: %v", i, err)
	}
	w.send(p, pend)
}

// pos favours the first line so edits collide on the same line.
func (w *fuzzWorld) pos(n int) int {
	if n <= 0 {
		return 0
	}
	if w.r.Intn(3) > 0 {
		return w.r.Intn(min(n, 8))
	}
	return w.r.Intn(n)
}

func (w *fuzzWorld) serve() {
	for i, p := range w.peers {
		for {
			m, ok := p.out.pop(w.now)
			if !ok {
				break
			}
			switch m.kind {
			case mOp:
				seq, applied, err := w.seqr.SubmitID(i, m.p.ClientOpID, m.p.BaseSeq, m.p.Op)
				if errors.Is(err, ErrDuplicate) {
					w.deliver(p, msg{kind: mAck, id: m.p.ClientOpID, seq: seq})
					continue
				}
				if err != nil {
					w.t.Fatalf("submit from %d: %v", i, err)
				}
				w.deliver(p, msg{kind: mAck, id: m.p.ClientOpID, seq: seq})
				for j, q := range w.peers {
					if j != i && q.subscribed {
						w.deliver(q, msg{kind: mRemote, seq: seq, op: applied})
					}
				}
			case mReopen:
				_, applied := w.seqr.AppliedID(i, m.id)
				p.subscribed = true
				w.deliver(p, msg{kind: mState, seq: w.seqr.Seq(), text: w.seqr.Text(), pendingApplied: m.id != "" && applied})
			}
		}
	}
}

func (w *fuzzWorld) deliver(p *peer, m msg) {
	if p.online {
		p.in.push(w.r, w.now, m)
	}
}

func (w *fuzzWorld) receive() {
	for i, p := range w.peers {
		for {
			m, ok := p.in.pop(w.now)
			if !ok {
				break
			}
			var pend *Pending
			var err error
			switch m.kind {
			case mAck:
				pend, err = p.c.Ack(m.id, m.seq)
			case mRemote:
				w.stats.remotes++
				_, err = p.c.Remote(m.seq, m.op)
			case mState:
				w.stats.resets++
				if m.pendingApplied {
					w.stats.acked++
					pend, err = p.c.ResetAcked(m.seq, m.text)
				} else {
					pend, err = p.c.Reset(m.seq, m.text)
				}
			}
			if err != nil {
				w.t.Fatalf("client %d kind %d: %v", i, m.kind, err)
			}
			w.send(p, pend)
		}
	}
}

func (w *fuzzWorld) quiet() bool {
	for _, p := range w.peers {
		if !p.online || len(p.in.q) > 0 || len(p.out.q) > 0 || p.c.Busy() {
			return false
		}
	}
	return true
}

func runFuzz(t *testing.T, seed int64) fuzzStats {
	start := []rune("fn main() {\n\tprintln(\"한\")\n}\n")
	w := &fuzzWorld{t: t, r: rand.New(rand.NewSource(seed)), seqr: NewSequencer(start), deleted: map[rune]bool{}}
	for i := range 3 {
		w.peers = append(w.peers, &peer{c: NewClient(fmt.Sprint(i), 0, start), online: true, subscribed: true})
	}
	reconnect := func(p *peer) {
		w.stats.reconnects++
		p.online = true
		p.out.push(w.r, w.now, msg{kind: mReopen, id: p.c.Resync()})
	}
	const active = 250
	for w.now = 0; w.now < active+5000; w.now++ {
		if w.now < active {
			for i := range w.peers {
				if w.peers[i].online || i == 2 {
					if w.r.Intn(10) < 3 {
						w.localEdit(i)
					}
				}
			}
			if p := w.peers[2]; p.online && w.r.Intn(100) < 5 {
				p.online, p.subscribed = false, false
				p.in.q = nil  // inbound frames are lost
				p.out.q = nil // so is anything not yet delivered
				p.backAt = w.now + 1 + w.r.Intn(30)
			}
		}
		if p := w.peers[2]; !p.online && (w.now >= p.backAt || w.now >= active) {
			reconnect(p)
		}
		w.serve()
		w.receive()
		if w.now >= active && w.quiet() {
			break
		}
	}
	if !w.quiet() {
		t.Fatalf("seed %d: did not quiesce", seed)
	}
	want := string(w.seqr.Text())
	for i, p := range w.peers {
		if got := string(p.c.Text()); got != want {
			t.Fatalf("seed %d: client %d diverged\n got %q\nwant %q", seed, i, got, want)
		}
	}
	count := map[rune]int{}
	for _, ru := range want {
		if ru >= markerBase {
			count[ru]++
		}
	}
	for m := range w.markers {
		ru := rune(markerBase + m)
		if count[ru] > 1 {
			t.Fatalf("seed %d: marker %d duplicated", seed, m)
		}
		if !w.deleted[ru] && count[ru] == 0 {
			t.Fatalf("seed %d: marker %d lost (text %q)", seed, m, strings.ToValidUTF8(want, "?"))
		}
	}
	w.stats.markers = w.markers
	return w.stats
}

func TestFuzzConvergence(t *testing.T) {
	var total fuzzStats
	for seed := int64(0); seed < 300; seed++ {
		s := runFuzz(t, seed)
		total.markers += s.markers
		total.reconnects += s.reconnects
		total.resets += s.resets
		total.acked += s.acked
		total.remotes += s.remotes
	}
	t.Logf("inserts %d, remote ops %d, reconnects %d, resets %d (ack lost %d)",
		total.markers, total.remotes, total.reconnects, total.resets, total.acked)
	if total.resets == 0 || total.acked == 0 || total.resets == total.acked || total.remotes == 0 {
		t.Fatalf("fuzz did not exercise reconnects: %+v", total)
	}
}
