package share

import (
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// HostBroadcast sends pt to every member as a sealed, signed broadcast.
func HostBroadcast(h *Host, kind wire.Kind, pt []byte) error {
	h.mu.Lock()
	_, err := h.broadcast(kind, 0, pt)
	h.mu.Unlock()
	h.flushManual()
	return err
}

// GuestSessionKey is a copy of the guest's current K_s.
func GuestSessionKey(g *Guest) wscrypto.SessionKey {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sess
}

// GuestSealToOwner seals pt as one ToOwner frame with the guest's P2O key
// without sending it (for frames injected past the relay's rules).
func GuestSealToOwner(g *Guest, kind wire.Kind, pt []byte) []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, err := g.p2o.Seal(wire.Header{Kind: kind, From: g.seat, To: wire.SeatOwner}, pt, 0)
	if err != nil {
		panic(err)
	}
	return f
}
