package share

import (
	"context"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

const (
	// killSendWait bounds how long Kill tries to tell the server.
	killSendWait = 200 * time.Millisecond
	// restTimeout bounds the background REST calls (kick, end).
	restTimeout = 15 * time.Second
)

// Kick removes a participant: its level goes at once, a new K_s (epoch+1)
// goes to the others only, every keyboard holder drops to editor, the
// server closes its socket (ctl_kick) and the REST removal runs in the
// background (the owner-side block stays even if it fails).
func (h *Host) Kick(seat uint8) error {
	h.mu.Lock()
	err := h.kickLocked(seat)
	h.mu.Unlock()
	h.flushManual()
	return err
}

func (h *Host) kickLocked(seat uint8) error {
	if h.over || h.killed.Load() {
		return ErrEnded
	}
	mb := h.members[seat]
	if mb == nil {
		return ErrNotMember
	}
	h.lastOwner = h.now()
	delete(h.members, seat)
	delete(h.joins, seat)
	h.reasm.Reset(seat)
	h.memberRemoved(mb)
	h.record(audit.EvKick, int(seat), mb.user, nil)
	if err := h.rekeyLocked(true); err != nil {
		return err
	}
	_ = h.sendPlain(wire.KindCtlKick, wire.SeatOwner, wire.SeatServer, wire.CtlKick{Seat: seat})
	h.sendLevels()
	h.membersChanged()
	h.emit(EventPeerLeft{Seat: seat, UserID: mb.user})
	if f := h.cfg.RemoveParticipant; f != nil {
		user := mb.user
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
			defer cancel()
			_ = f(ctx, user)
		}()
	}
	return nil
}

// rekeyLocked makes K_s epoch+1, sends it to every remaining member (O2P)
// and switches the broadcast sealer right after those frames. With
// demoteKeyboard (a removal) keyboard holders drop to editor (spec B8.4).
func (h *Host) rekeyLocked(demoteKeyboard bool) error {
	next, err := wscrypto.NewSessionKey(h.sess.Epoch + 1)
	if err != nil {
		return err
	}
	old := h.sess
	h.sess = next
	body := mustJSON(wire.Rekey{Epoch: next.Epoch, Key: next.Key[:]})
	h.outMu.Lock()
	for _, s := range h.seatsLocked() {
		if mb := h.members[s]; mb.o2p != nil {
			_, _ = h.sealLocked(mb.o2p, nil, wire.KindRekey, 0, wire.SeatOwner, s, body)
		}
	}
	h.bseal = wscrypto.NewSealer(next.Key, wscrypto.DirBroadcast)
	h.outMu.Unlock()
	clear(old.Key[:])
	h.rekeyDue = false
	if demoteKeyboard {
		for _, s := range h.seatsLocked() {
			h.demote(h.members[s], DemoteRekey)
		}
	}
	return nil
}

// rekeyIfDue rekeys after the keyboard was taken back (spec B9) or when the
// broadcast counter reached RekeyAfter, once the owner is connected (a
// rekey sent while away would reach nobody; members away get the current
// key in the resume grant). Keyboard holders keep the keyboard: the
// participants stay the same.
func (h *Host) rekeyIfDue() {
	if h.over || h.killed.Load() || h.curConn() == nil {
		return
	}
	h.outMu.Lock()
	s := h.bseal
	h.outMu.Unlock()
	if !h.rekeyDue && (s == nil || !needsRekey(s)) {
		return
	}
	_ = h.rekeyLocked(false)
}

// Kill is the kill switch. It does not wait for anything: participant input
// is refused from this instant (the killed flag is checked right before
// every terminal write), then in the background the server is told
// (ctl_end, at most 0.2 s), the socket closes, the keys are overwritten and
// the REST end runs.
func (h *Host) Kill() {
	if !h.killed.CompareAndSwap(false, true) {
		return
	}
	go func() { _ = h.shutdown(context.Background(), ReasonKill, false) }()
}

// Killed reports whether the kill switch (or an end) was used.
func (h *Host) Killed() bool { return h.killed.Load() }

// End ends the share normally: like Kill, but the participants hear
// "ended" first and the REST end is waited for.
func (h *Host) End(ctx context.Context) error {
	if !h.killed.CompareAndSwap(false, true) {
		return ErrEnded
	}
	h.mu.Lock()
	if !h.over {
		_, _ = h.broadcast(wire.KindEnded, 0, mustJSON(wire.Ended{Reason: ReasonEnded}))
	}
	h.mu.Unlock()
	h.flush()
	return h.shutdown(ctx, ReasonEnded, true)
}

func (h *Host) shutdown(ctx context.Context, reason string, wait bool) error {
	if c := h.curConn(); c != nil {
		f := ctlEndFrame()
		sent := make(chan struct{})
		go func() {
			_ = c.Send(f)
			close(sent)
		}()
		select {
		case <-sent:
		case <-time.After(killSendWait):
		}
		go c.Close(1000, "")
	}
	h.mu.Lock()
	h.setConn(nil)
	h.down = false
	h.finishLocked(reason, "owner")
	h.mu.Unlock()
	end := h.cfg.EndShare
	if end == nil {
		return nil
	}
	if !wait {
		go func() {
			c, cancel := context.WithTimeout(context.Background(), restTimeout)
			defer cancel()
			_ = end(c)
		}()
		return nil
	}
	return end(ctx)
}

func ctlEndFrame() []byte {
	p := []byte("{}")
	h := wire.Header{Kind: wire.KindCtlEnd, From: wire.SeatOwner, To: wire.SeatServer, Len: uint32(len(p))}
	hb := h.Marshal()
	return append(hb[:], p...)
}
