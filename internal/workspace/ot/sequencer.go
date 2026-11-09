package ot

import "errors"

// HistoryLimit is how many sequenced operations a Sequencer keeps for
// transforming late submissions (older bases get ErrStaleBase).
const HistoryLimit = 5000

var (
	ErrStaleBase  = errors.New("ot: base sequence no longer retained")
	ErrFutureBase = errors.New("ot: base sequence is in the future")
	// ErrDuplicate: the client op id was already sequenced; the returned seq
	// is the original one (re-ack, change nothing).
	ErrDuplicate = errors.New("ot: operation already sequenced")
)

type histEntry struct {
	op Op
	id string
}

// Sequencer is the single place that orders one document's edits (the
// owner's PC). Not safe for concurrent use.
type Sequencer struct {
	text []rune
	seq  int64
	hist []histEntry // hist[i] has seq = s.seq - len(hist) + 1 + i
	ids  map[string]int64
}

func NewSequencer(text []rune) *Sequencer {
	return &Sequencer{text: append([]rune(nil), text...), ids: map[string]int64{}}
}

// Seq is the last assigned sequence number (0 before any edit).
func (s *Sequencer) Seq() int64 { return s.seq }

// Text is a copy of the current document.
func (s *Sequencer) Text() []rune { return append([]rune(nil), s.text...) }

func (s *Sequencer) after(baseSeq int64) ([]histEntry, error) {
	if baseSeq > s.seq {
		return nil, ErrFutureBase
	}
	oldest := s.seq - int64(len(s.hist))
	if baseSeq < oldest {
		return nil, ErrStaleBase
	}
	return s.hist[baseSeq-oldest:], nil
}

// Submit transforms op (made against baseSeq) over everything sequenced
// since, applies it and assigns the next sequence number. applied is the
// transformed operation to broadcast.
func (s *Sequencer) Submit(baseSeq int64, op Op) (seq int64, applied Op, err error) {
	return s.SubmitID("", baseSeq, op)
}

// SubmitID is Submit that also remembers clientOpID (when not empty): a
// second submission of the same id returns its original seq with
// ErrDuplicate and changes nothing (retransmission after a reconnect).
func (s *Sequencer) SubmitID(clientOpID string, baseSeq int64, op Op) (seq int64, applied Op, err error) {
	if clientOpID != "" {
		if seq, ok := s.ids[clientOpID]; ok {
			return seq, nil, ErrDuplicate
		}
	}
	later, err := s.after(baseSeq)
	if err != nil {
		return 0, nil, err
	}
	if err := op.validate(); err != nil {
		return 0, nil, err
	}
	for _, h := range later {
		if op, _, err = Transform(op, h.op); err != nil {
			return 0, nil, err
		}
	}
	text, err := op.Apply(s.text)
	if err != nil {
		return 0, nil, err
	}
	s.text = text
	s.seq++
	s.hist = append(s.hist, histEntry{op: op, id: clientOpID})
	if clientOpID != "" {
		s.ids[clientOpID] = s.seq
	}
	if over := len(s.hist) - HistoryLimit; over > 0 {
		for _, h := range s.hist[:over] {
			if h.id != "" {
				delete(s.ids, h.id)
			}
		}
		s.hist = append([]histEntry(nil), s.hist[over:]...)
	}
	return s.seq, op, nil
}

// AppliedID reports the seq a client op id was sequenced at, while retained.
func (s *Sequencer) AppliedID(clientOpID string) (int64, bool) {
	if clientOpID == "" {
		return 0, false
	}
	seq, ok := s.ids[clientOpID]
	return seq, ok
}

// Since returns the operations sequenced after seq, in order.
func (s *Sequencer) Since(seq int64) ([]Op, error) {
	later, err := s.after(seq)
	if err != nil {
		return nil, err
	}
	out := make([]Op, len(later))
	for i, h := range later {
		out[i] = h.op
	}
	return out, nil
}
