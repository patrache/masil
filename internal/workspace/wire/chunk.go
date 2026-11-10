package wire

// MaxReassembly caps the bytes buffered per sending seat while chunks are
// being reassembled.
const MaxReassembly = 64 << 20

// Chunk splits payload into frames of at most max bytes (header included).
// Every frame copies h with Len set to its piece; all but the last carry
// FlagMore. An empty payload yields one empty frame. Seq is copied as is
// (sealed frames are sealed per chunk, which sets Seq and Len again).
func Chunk(h Header, payload []byte, max int) [][]byte {
	piece := max - HeaderLen
	if piece < 1 {
		piece = 1
	}
	var out [][]byte
	for first := true; first || len(payload) > 0; first = false {
		n := min(piece, len(payload))
		ch := h
		ch.Flags &^= FlagMore
		if n < len(payload) {
			ch.Flags |= FlagMore
		}
		if !first {
			ch.Flags &^= FlagGroupStart
		}
		ch.Len = uint32(n)
		hb := ch.Marshal()
		f := make([]byte, 0, HeaderLen+n)
		f = append(f, hb[:]...)
		f = append(f, payload[:n]...)
		out = append(out, f)
		payload = payload[n:]
	}
	return out
}

type reassemblyKey struct {
	from uint8
	kind Kind
}

// Reassembler joins chunked messages, per (sending seat, kind).
// The zero value is ready to use. Not safe for concurrent use.
type Reassembler struct {
	bufs  map[reassemblyKey][]byte
	total map[uint8]int
}

// Add feeds one chunk's body. It returns the whole message with done=true on
// the last chunk. Over MaxReassembly buffered bytes for one seat, that
// seat's partial messages are dropped and ErrTooLarge is returned.
func (r *Reassembler) Add(h Header, p []byte) (full []byte, done bool, err error) {
	key := reassemblyKey{h.From, h.Kind}
	buf, pending := r.bufs[key]
	if !pending && h.Flags&FlagMore == 0 {
		return p, true, nil
	}
	if r.bufs == nil {
		r.bufs = map[reassemblyKey][]byte{}
		r.total = map[uint8]int{}
	}
	if r.total[h.From]+len(p) > MaxReassembly {
		r.dropSeat(h.From)
		return nil, false, ErrTooLarge
	}
	buf = append(buf, p...)
	r.total[h.From] += len(p)
	if h.Flags&FlagMore != 0 {
		r.bufs[key] = buf
		return nil, false, nil
	}
	delete(r.bufs, key)
	r.total[h.From] -= len(buf)
	return buf, true, nil
}

// Reset drops every partial message from seat (e.g. when it leaves).
func (r *Reassembler) Reset(seat uint8) { r.dropSeat(seat) }

func (r *Reassembler) dropSeat(seat uint8) {
	for k := range r.bufs {
		if k.from == seat {
			delete(r.bufs, k)
		}
	}
	delete(r.total, seat)
}
