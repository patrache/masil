package arch

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// dragPresence is a drag in progress as presence (collab Presence.Dragging):
// node or box ID shown at world (X, Y), Into = the box it would drop into.
type dragPresence struct {
	ID   string `json:"id"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Into string `json:"into,omitempty"`
}

// dragEvery is the shortest gap between two drag presence frames (at most 10
// a second; the drop itself is one op).
const dragEvery = 100 * time.Millisecond

// shared is the room document sink, or nil.
func (m *canvasModel) shared() *sharedSink {
	sh, _ := m.sink.(*sharedSink)
	return sh
}

// live reports whether the room session may send (not kicked or read-only).
func (sh *sharedSink) live() bool { return sh != nil && sh.s != nil && !sh.kicked }

// syncPresence sends my selection when it changed since the last frame.
func (m *canvasModel) syncPresence() {
	sh := m.shared()
	if !sh.live() {
		return
	}
	sel := append([]string{}, m.selection()...)
	slices.Sort(sel)
	if sh.sentSelOK && slices.Equal(sel, sh.sentSel) {
		return
	}
	sh.sentSel, sh.sentSelOK = slices.Clone(sel), true
	sh.s.SetPresence(collab.Presence{Selection: sel})
}

// sendDrag shares the drag in progress, at most every dragEvery of ctx's
// clock; ghost nil ends it (sent at once).
func (m *canvasModel) sendDrag(ctx *ui.Context, ghost *archdraw.Ghost) {
	sh := m.shared()
	if !sh.live() || ctx == nil || ctx.Now == nil {
		return
	}
	if ghost == nil {
		m.clearDragPresence()
		return
	}
	now := ctx.Now()
	if m.dragShared && now.Sub(m.dragSentAt) < dragEvery {
		return
	}
	b, err := json.Marshal(dragPresence{ID: ghost.ID, X: ghost.X, Y: ghost.Y, Into: ghost.IntoBox})
	if err != nil {
		return
	}
	m.dragShared, m.dragSentAt = true, now
	sh.presenceSent++
	sh.s.SetPresence(collab.Presence{Dragging: b})
}

// clearDragPresence ends a shared drag at once (Dragging = null); it needs no
// clock, so any path that drops the gesture can call it.
func (m *canvasModel) clearDragPresence() {
	if !m.dragShared {
		return
	}
	m.dragShared = false
	if sh := m.shared(); sh.live() {
		sh.s.SetPresence(collab.Presence{Dragging: json.RawMessage("null")})
	}
}

// remoteMarks are the other people's selections and drags for the canvas.
func (m *canvasModel) remoteMarks(ctx *ui.Context) []archdraw.RemoteMark {
	sh := m.shared()
	if sh == nil || sh.kicked {
		return nil
	}
	var out []archdraw.RemoteMark
	for i, p := range sh.peers {
		mk := archdraw.RemoteMark{User: personName(ctx, p.User), Index: i % 3}
		for _, id := range p.Selection {
			if m.exists(id) {
				mk.Selected = append(mk.Selected, id)
			}
		}
		if g := peerGhost(p); g != nil && m.exists(g.ID) {
			mk.Ghost = g
		}
		if len(mk.Selected) > 0 || mk.Ghost != nil {
			out = append(out, mk)
		}
	}
	return out
}

// peerGhost is a peer's drag, or nil.
func peerGhost(p collab.Presence) *archdraw.Ghost {
	if len(p.Dragging) == 0 || string(p.Dragging) == "null" {
		return nil
	}
	var d dragPresence
	if json.Unmarshal(p.Dragging, &d) != nil || d.ID == "" {
		return nil
	}
	return &archdraw.Ghost{ID: d.ID, X: d.X, Y: d.Y, IntoBox: d.Into}
}

// drawRemoteTags writes each dragging peer's name above their ghost (the
// canvas cell rect r; the selection tags come with archdraw).
func (m *canvasModel) drawRemoteTags(ctx *ui.Context, b *buf, marks []archdraw.RemoteMark) {
	r := m.regs.Canvas
	for _, mk := range marks {
		if mk.Ghost == nil {
			continue
		}
		sx, sy := m.vp.ToScreen(mk.Ghost.X, mk.Ghost.Y)
		sy--
		if sx < 0 || sy < 0 || sx >= r.W || sy >= r.H {
			continue
		}
		name := textwidth.Truncate(mk.User, r.W-sx, "…")
		b.text(r.X+sx, r.Y+sy, name, r.W-sx, sRemote+sty(mk.Index%3))
	}
}

// personName is a user's display name (handle, else a short ID).
func personName(ctx *ui.Context, id string) string {
	if ctx != nil && ctx.Store != nil {
		if me := ctx.Me(); me != nil && me.ID == id {
			if n := cleanText(me.DisplayName); n != "" {
				return n
			}
			return cleanText(me.Handle)
		}
		if u := ctx.Store.User(id); u != nil {
			if n := cleanText(u.DisplayName); n != "" {
				return n
			}
			if n := cleanText(u.Handle); n != "" {
				return n
			}
		}
	}
	if len(id) > 6 {
		return id[len(id)-6:]
	}
	return id
}

// peopleLine is everyone on the document: me first, then the others.
func (m *canvasModel) peopleLine(ctx *ui.Context, sh *sharedSink) string {
	names := []string{personName(ctx, myID(ctx))}
	for _, p := range sh.peers {
		if p.User != myID(ctx) {
			names = append(names, personName(ctx, p.User))
		}
	}
	return strings.Join(names, "·")
}
