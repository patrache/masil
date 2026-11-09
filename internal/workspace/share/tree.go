package share

import (
	"github.com/patrache/masil/internal/workspace/wire"
)

// onTreeReq lists one folder for a participant (deny list and ignore rules
// applied; refused paths are recorded).
func (h *Host) onTreeReq(seat uint8, m wire.TreeReq) {
	mb := h.members[seat]
	out := wire.TreeList{Dir: m.Dir, Entries: []wire.TreeEntry{}}
	if h.docs == nil {
		out.Error = DocErrNotFound
		h.sendToPeer(mb, wire.KindTreeList, mustJSON(out))
		return
	}
	dir := m.Dir
	if dir == "" {
		dir = "."
	}
	entries, more, err := h.docs.root.List(dir, true)
	if err != nil {
		out.Error = h.pathViolation(seat, m.Dir, err)
		h.sendToPeer(mb, wire.KindTreeList, mustJSON(out))
		return
	}
	for _, e := range entries {
		if e.Denied || e.Ignored {
			continue // List(forPeer) leaves them out already; never send one
		}
		out.Entries = append(out.Entries, wire.TreeEntry{Name: e.Name, Dir: e.Dir, Size: e.Size})
	}
	out.More = more
	h.sendToPeer(mb, wire.KindTreeList, mustJSON(out))
}

// ListDir asks the owner for one folder of the shared tree ("." = top);
// the answer arrives as EventTree.
func (g *Guest) ListDir(rel string) error {
	g.mu.Lock()
	err := g.sendLevel(wire.LevelViewer, wire.KindTreeReq, mustJSON(wire.TreeReq{Dir: rel}))
	g.mu.Unlock()
	g.flushManual()
	return err
}

// sendLevel sends body when my level is at least min.
func (g *Guest) sendLevel(min wire.Level, kind wire.Kind, body []byte) error {
	if g.over {
		return ErrEnded
	}
	if g.phase != phaseGranted || g.level < min {
		return ErrLevel
	}
	return g.sendOwner(kind, body)
}
