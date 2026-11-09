package workspace

import (
	"slices"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/workspace/editor"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Cursors while sharing (spec B6.7): the participants' cursors drawn in the
// owner's editor with their names, and the owner's cursor sent to them.

// peerCursor is one participant's cursor in one document.
type peerCursor struct {
	seat uint8
	peer editor.Peer
}

// onPresence keeps a participant's cursor and redraws the document's peers.
func (s *Surface) onPresence(ctx *ui.Context, p wire.DocPresence) {
	o := s.own
	if o == nil || p.Seat == wire.SeatOwner || p.Seat == 0 {
		return
	}
	if o.cursors == nil {
		o.cursors = map[string][]peerCursor{}
	}
	user := s.seatUser(p.Seat)
	pc := peerCursor{seat: p.Seat, peer: editor.Peer{
		Name: s.peer(ctx, p.Seat, user).handle, Color: ctx.Theme.UserColor(user, false),
		Cursor: max(p.Cursor, 0), SelFrom: max(p.SelFrom, 0), SelTo: max(p.SelTo, 0),
	}}
	list := slices.DeleteFunc(o.cursors[p.Doc], func(c peerCursor) bool { return c.seat == p.Seat })
	list = append(list, pc)
	slices.SortFunc(list, func(a, b peerCursor) int { return int(a.seat) - int(b.seat) })
	o.cursors[p.Doc] = list
	s.showPeers(p.Doc)
}

// dropCursors forgets seat's cursors (they left).
func (s *Surface) dropCursors(seat uint8) {
	if s.own == nil {
		return
	}
	for rel, list := range s.own.cursors {
		s.own.cursors[rel] = slices.DeleteFunc(list, func(c peerCursor) bool { return c.seat == seat })
		s.showPeers(rel)
	}
}

// shiftCursors moves rel's cursors over an edit applied to it.
func (s *Surface) shiftCursors(rel string, op ot.Op) {
	if s.own == nil || len(s.own.cursors[rel]) == 0 {
		return
	}
	for i := range s.own.cursors[rel] {
		p := &s.own.cursors[rel][i].peer
		p.Cursor = ot.TransformIndex(op, p.Cursor)
		p.SelFrom = ot.TransformIndex(op, p.SelFrom)
		p.SelTo = ot.TransformIndex(op, p.SelTo)
	}
	s.showPeers(rel)
}

// showPeers hands rel's cursors to its editor (none when not sharing).
func (s *Surface) showPeers(rel string) {
	d := s.code.find(rel)
	if d == nil {
		return
	}
	var peers []editor.Peer
	if s.own != nil {
		for _, c := range s.own.cursors[rel] {
			peers = append(peers, c.peer)
		}
	}
	d.ed.SetPeers(peers)
}

// clearPeers takes every participant cursor off the editors (the share ended).
func (s *Surface) clearPeers() {
	for _, d := range s.code.docs {
		d.ed.SetPeers(nil)
	}
}

// sendPresence tells the participants where the owner's cursor is in d
// (only when it moved; the engine sends it only for documents they see).
func (s *Surface) sendPresence(d *openDoc) {
	o := s.own
	if o == nil || d == nil {
		return
	}
	cur := d.ed.CursorIndex()
	from, to, ok := d.ed.Selection()
	if !ok {
		from, to = 0, 0
	}
	p := wire.DocPresence{Doc: d.rel, Seat: wire.SeatOwner, Cursor: cur, SelFrom: from, SelTo: to}
	if o.sentCursor == p {
		return
	}
	o.sentCursor = p
	o.host.Presence(d.rel, cur, from, to)
}
