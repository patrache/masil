package workspace

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/wsstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane"
	"github.com/patrache/masil/internal/ui/workspace/editor"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The participant's side of a share (spec B6.3, B6.7, B8.7, P2 Task 23): the
// same surface as the owner's — tree, code, terminal — but every cell is the
// owner's, read through share.Guest. Nothing of it touches the disk: the
// remote documents and the terminal replays live in memory and go when the
// share ends.

// guestPhase is where the participant is in the join.
type guestPhase int

const (
	guestConnecting guestPhase = iota // the share is looked up, the socket and key exchange run
	guestSAS                          // the check number is shown, my answer is due
	guestWaiting                      // I said "match"; the owner decides
	guestIn                           // let in
	guestOver                         // ended, declined or left
)

// presenceEvery: my cursor goes out at most this often (B6.7).
const presenceEvery = 100 * time.Millisecond

// guestShare is one share this instance takes part in.
type guestShare struct {
	shareID  string
	info     api.WorkspaceShare
	eng      *share.Guest
	start    *guestStart
	phase    guestPhase
	sas      string
	answered bool // my match/mismatch went out
	level    wire.Level
	verified bool
	// reconnecting: the socket dropped and the engine redials (band)
	reconnecting bool
	// over: why the share is over for me (the full-cover notice)
	overText func(ctx *ui.Context) string
	roster   []share.Member
	// tree: the remote folders asked for, by path ("." = top)
	asked map[string]*treeNode
	// terms: the owner's shared terminals, replayed here
	terms   map[uint8]*replayTerm
	termCur uint8
	unsaved int
	compose composer
	// cursors: the others' cursors by document
	cursors map[string][]peerCursor
	// presence: my cursor waiting to go out, and when it last did
	presence     *wire.DocPresence
	sentPresence wire.DocPresence
	presenceAt   time.Time
}

// guestStart hands the started engine from the start Cmd to the instance
// (any goroutine); a start that finishes after the instance closed leaves
// the share itself.
type guestStart struct {
	mu     sync.Mutex
	closed bool
	eng    *share.Guest
}

func (p *guestStart) hand(g *share.Guest) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.eng = g
	return true
}

// close marks the instance closed and returns the engine to leave, if any.
func (p *guestStart) close() *share.Guest {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	g := p.eng
	p.eng = nil
	return g
}

// guestStartedMsg ends the join start (the share lookup, the socket).
type guestStartedMsg struct {
	surface string
	start   *guestStart
	eng     *share.Guest
	info    api.WorkspaceShare
	err     error
}

// joinInstanceID is the tool instance that joins share id.
func joinInstanceID(id string) string { return ui.ToolWorkspace + ":join:" + id }

// joinCmd opens (or shows again) the participant screen of share id.
func joinCmd(id string) tea.Cmd {
	return ui.OpenTool(ui.ToolWorkspace, joinInstanceID(id), map[string]string{"join": id})
}

// newGuest prepares the participant side of share id (Init starts it).
func newGuest(s *Surface, id string) *guestShare {
	return &guestShare{shareID: id, start: &guestStart{}, asked: map[string]*treeNode{},
		terms: map[uint8]*replayTerm{}, cursors: map[string][]peerCursor{}}
}

// startGuest looks the share up and connects (a Cmd: both wait on the
// network).
func (s *Surface) startGuest(ctx *ui.Context) tea.Cmd {
	g := s.guest
	me := ctx.Me()
	if g == nil || g.eng != nil || me == nil || ctx.API == nil {
		return nil
	}
	client, surface, shareID, userID := ctx.API, s.id, g.shareID, me.ID
	var known *api.WorkspaceShare
	if st := wsstore.Get(ctx.Store); st != nil {
		if sh, ok := st.Share(shareID); ok && sh.Status == "live" {
			known = &sh
		}
	}
	dial := dialFor
	if dial == nil {
		dial = share.Dialer(client)
	}
	manual, clock, start := manualTick, nowClock(ctx), g.start
	return func() tea.Msg {
		bg := context.Background()
		info := known
		if info == nil {
			sh, err := client.GetWorkspaceShare(bg, shareID)
			if err != nil {
				return guestStartedMsg{surface: surface, start: start, err: err}
			}
			info = sh
		}
		eng := share.NewGuest(share.GuestConfig{ShareID: shareID, OwnerID: info.Owner.ID, UserID: userID,
			Dial: dial, Clock: clock, ManualTick: manual})
		if err := eng.Start(bg); err != nil {
			return guestStartedMsg{surface: surface, start: start, info: *info, err: err}
		}
		if !start.hand(eng) { // the screen closed meanwhile
			eng.Leave()
			return nil
		}
		return guestStartedMsg{surface: surface, start: start, eng: eng, info: *info}
	}
}

// guestStarted installs the engine (or shows why it could not start).
func (s *Surface) guestStarted(ctx *ui.Context, m guestStartedMsg) tea.Cmd {
	g := s.guest
	if g == nil || m.start != g.start {
		return nil
	}
	g.info = m.info
	if m.err != nil || m.eng == nil {
		slog.Warn("workspace: join", "err", m.err)
		code := share.CloseCodeOf(m.err)
		g.over(func(ctx *ui.Context) string {
			if api.CodeOf(m.err) == "not_found" || code == share.CloseCode(4100) {
				return ctx.T.Workspace.Ended()
			}
			return ctx.T.Workspace.OpenFailed()
		})
		s.dropRemote()
		return nil
	}
	g.eng = m.eng
	return s.armPump(ctx)
}

// over ends my part (the full-cover notice says why).
func (g *guestShare) over(text func(ctx *ui.Context) string) {
	g.phase = guestOver
	g.overText = text
}

// guestUpdate takes the msgs of the participant screen.
func (s *Surface) guestUpdate(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(guestStartedMsg); ok && m.surface == s.id {
		return s.guestStarted(ctx, m)
	}
	return nil
}

// guestPump drives the engine one beat and applies its events.
func (s *Surface) guestPump(ctx *ui.Context) tea.Cmd {
	g := s.guest
	if g.eng == nil || g.phase == guestOver {
		return nil
	}
	now := nowClock(ctx).Now()
	if manualTick {
		g.eng.Tick(now)
	}
	var cmds []tea.Cmd
	for _, ev := range g.eng.Drain() {
		cmds = append(cmds, s.guestEvent(ctx, ev))
		if g.phase == guestOver {
			break
		}
	}
	if g.phase == guestIn && g.presence != nil && now.Sub(g.presenceAt) >= presenceEvery {
		p := *g.presence
		g.presence, g.presenceAt, g.sentPresence = nil, now, p
		g.eng.Presence(p.Doc, p.Cursor, p.SelFrom, p.SelTo)
	}
	return tea.Batch(cmds...)
}

// guestEvent applies one engine event.
func (s *Surface) guestEvent(ctx *ui.Context, ev share.Event) tea.Cmd {
	g := s.guest
	t := ctx.T.Workspace
	switch e := ev.(type) {
	case share.EventSASReady:
		g.sas, g.answered = e.SAS, false
		if g.phase < guestIn {
			g.phase = guestSAS
			s.dlg = newJoinSASDialog(s)
		}
	case share.EventGranted:
		g.phase, g.level, g.verified = guestIn, e.Level, e.Verified
		if s.dlg != nil && s.dlg.shareOnly {
			s.dlg = nil
		}
		s.tree.root = &treeNode{rel: ".", dir: true, open: true, depth: -1}
		s.tree.rows = nil
		s.guestList(s.tree.root)
		s.relayout(ctx)
	case share.EventDeclined:
		g.over(declinedText(e.Reason))
		s.dropRemote()
	case share.EventEnded:
		g.over(func(ctx *ui.Context) string { return ctx.T.Workspace.Ended() })
		s.dropRemote()
	case share.EventReconnecting:
		g.reconnecting = true
	case share.EventResumed:
		g.reconnecting = false
	case share.EventRoster:
		g.roster = e.Members
		g.level = g.eng.Level()
		for _, m := range e.Members {
			if m.UserID == g.me(ctx) {
				g.verified = m.Verified
			}
		}
		s.guestSyncReadOnly()
	case share.EventTree:
		s.guestTree(e.TreeList)
	case share.EventDocState:
		return s.guestDocState(ctx, e.DocState)
	case share.EventDocRemote:
		if d := s.code.find(e.Doc); d != nil {
			d.ed.ApplyRemote(e.Op)
			s.guestShiftCursors(e.Doc, e.Op)
		}
	case share.EventDocReset:
		if d := s.code.find(e.Doc); d != nil {
			d.ed.TakeOps()
			d.ed.Reset(e.Text)
		}
	case share.EventDocRejected:
		return ui.Toast(t.SaveFailed(), ui.ToastError)
	case share.EventPresence:
		s.guestPresence(ctx, e.DocPresence)
	case share.EventNotice:
		if e.Code == share.NoticeSaved && e.Seat != 0 && e.Seat == g.mySeat(ctx) {
			return ui.Toast(t.Saved(textwidth.Sanitize(baseName(e.Arg))), ui.ToastSuccess)
		}
	case share.EventPropResult:
		return propResultToast(ctx, e.Action)
	case share.EventTermKeyframe:
		rt := g.term(e.Term)
		rt.keyframe(e.Bytes)
	case share.EventTermOut:
		g.term(e.Term).write(e.Bytes)
	case share.EventTermStatus:
		g.term(e.Term).setStatus(e.TermStatus)
		g.unsaved = e.Unsaved
	case share.EventTamperWarning:
		return ui.Toast(t.TamperWarning(), ui.ToastAlert)
	}
	return nil
}

// me is my user id.
func (g *guestShare) me(ctx *ui.Context) string {
	if me := ctx.Me(); me != nil {
		return me.ID
	}
	return ""
}

// mySeat is my seat in the share (0 when unknown).
func (g *guestShare) mySeat(ctx *ui.Context) uint8 {
	me := g.me(ctx)
	for _, m := range g.info.Members {
		if m.User.ID == me && m.Seat > 0 && m.Seat <= 8 {
			return uint8(m.Seat)
		}
	}
	return 0
}

// declinedText is why a join attempt ended.
func declinedText(reason string) func(ctx *ui.Context) string {
	return func(ctx *ui.Context) string {
		t := ctx.T.Workspace
		switch reason {
		case share.ReasonExpired:
			return t.DeclinedExpired()
		case share.ReasonMismatch, share.ReasonCommit:
			return t.SasMismatchWarning()
		case share.ReasonNotFound, share.ReasonEnded:
			return t.Ended()
		}
		return t.DeclinedOwner()
	}
}

// propResultToast tells what the owner did with my proposal.
func propResultToast(ctx *ui.Context, action string) tea.Cmd {
	t := ctx.T.Workspace
	switch action {
	case "insert", "insert_send", "edit":
		return ui.Toast(t.PropDone(), ui.ToastInfo)
	case "reject":
		return ui.Toast(t.PropRejected(), ui.ToastInfo)
	case "too_long", "full", "rate_limited":
		return ui.Toast(t.PropFailed(), ui.ToastError)
	}
	return nil
}

// dropRemote forgets everything the owner showed (the share is over): the
// documents, the terminal replays, the tree.
func (s *Surface) dropRemote() {
	g := s.guest
	if s.dlg != nil && s.dlg.shareOnly {
		s.dlg = nil
	}
	for _, d := range s.code.docs {
		d.ed.Reset("")
	}
	s.code.docs, s.code.cur, s.code.msg = nil, 0, ""
	clear(g.terms)
	clear(g.cursors)
	clear(g.asked)
	g.compose = composer{}
	s.tree.root, s.tree.rows = nil, nil
}

// guestPeerName names a seat for the screens (sanitized handle).
func (s *Surface) guestPeerName(seat uint8, userID string) string {
	g := s.guest
	if seat == wire.SeatOwner || userID == g.info.Owner.ID {
		return textwidth.Sanitize(g.info.Owner.Handle)
	}
	for _, m := range g.info.Members {
		if m.User.ID == userID {
			return textwidth.Sanitize(m.User.Handle)
		}
	}
	return textwidth.Sanitize(userID)
}

// ownerDisplay is the owner as the participant screens name them.
func (g *guestShare) ownerDisplay() string {
	if n := textwidth.Sanitize(g.info.Owner.DisplayName); n != "" {
		return n
	}
	return textwidth.Sanitize(g.info.Owner.Handle)
}

// title is the share's name (sanitized).
func (g *guestShare) title() string { return textwidth.Sanitize(g.info.Title) }

// ---- tree ----

// guestList asks the owner for folder n.
func (s *Surface) guestList(n *treeNode) {
	g := s.guest
	if g.eng == nil {
		return
	}
	n.open = true
	if n.loaded {
		return
	}
	g.asked[n.rel] = n
	_ = g.eng.ListDir(n.rel)
}

// guestTree fills the folder the owner listed.
func (s *Surface) guestTree(l wire.TreeList) {
	g := s.guest
	dir := l.Dir
	if dir == "" {
		dir = "."
	}
	n := g.asked[dir]
	if n == nil {
		return
	}
	delete(g.asked, dir)
	n.loaded = true
	n.children = n.children[:0]
	if l.Error != "" {
		n.children = []*treeNode{{errRow: true, depth: n.depth + 1}}
		s.tree.flatten()
		return
	}
	for _, e := range l.Entries {
		rel := e.Name
		if n.rel != "." {
			rel = n.rel + "/" + e.Name
		}
		n.children = append(n.children, &treeNode{name: e.Name, rel: rel, dir: e.Dir, depth: n.depth + 1})
	}
	if l.More > 0 {
		n.children = append(n.children, &treeNode{moreRow: true, more: l.More, depth: n.depth + 1})
	}
	s.tree.flatten()
}

// guestTreeKey handles a key in the remote tree.
func (s *Surface) guestTreeKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	c := s.tree
	if c.nav(msg.String()) {
		return true, nil
	}
	n := c.selected()
	switch msg.String() {
	case "enter", "l", "right", "space":
		if n == nil || n.moreRow || n.errRow {
			return true, nil
		}
		if n.dir {
			if n.open && msg.String() == "enter" {
				n.open = false
			} else {
				s.guestList(n)
			}
			c.flatten()
			return true, nil
		}
		if msg.String() == "l" || msg.String() == "right" {
			return true, nil
		}
		return true, s.guestOpen(ctx, n.rel)
	case "h", "left":
		if n == nil {
			return true, nil
		}
		if n.dir && n.open {
			n.open = false
			c.flatten()
		} else if p := c.parent(c.sel); p >= 0 {
			c.sel = p
		}
		return true, nil
	}
	return false, nil
}

// ---- documents ----

// guestOpen asks the owner for rel (or shows it again).
func (s *Surface) guestOpen(ctx *ui.Context, rel string) tea.Cmd {
	g := s.guest
	if d := s.code.find(rel); d != nil {
		s.code.cur = slices.Index(s.code.docs, d)
		s.setFocus(ctx, PaneCode)
		return nil
	}
	if g.eng == nil || g.eng.OpenDoc(rel) != nil {
		s.code.msg = ctx.T.Workspace.CodeCannotOpen()
	}
	return nil
}

// guestDocState shows a document the owner sent (or why it did not open).
func (s *Surface) guestDocState(ctx *ui.Context, m wire.DocState) tea.Cmd {
	g := s.guest
	t := ctx.T.Workspace
	if m.Error != "" {
		switch m.Error {
		case share.DocErrTooLarge:
			s.code.msg = t.CodeCannotOpenLarge()
		case share.DocErrBinary:
			s.code.msg = t.CodeCannotOpenBinary()
		default:
			s.code.msg = t.CodeCannotOpen()
		}
		s.code.cur = -1
		return nil
	}
	text, ok := g.eng.DocText(m.Doc)
	if !ok {
		text = m.Text
	}
	d := &openDoc{rel: m.Doc, readOnly: m.ReadOnly, reason: m.Reason}
	d.ed = editor.New(ctx.Theme, text, m.ReadOnly || g.level < wire.LevelEditor)
	s.code.docs = append(s.code.docs, d)
	s.code.cur = len(s.code.docs) - 1
	s.code.msg = ""
	s.guestShowPeers(d.rel)
	s.setFocus(ctx, PaneCode)
	return nil
}

// guestSyncReadOnly makes the editors read-only below the editor level.
func (s *Surface) guestSyncReadOnly() {
	for _, d := range s.code.docs {
		d.ed.SetReadOnly(d.readOnly || s.guest.level < wire.LevelEditor)
	}
}

// guestCodeKey runs a key in a remote document: my edits go to the owner,
// undo is my own edits only, :w asks the owner to save.
func (s *Surface) guestCodeKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	g := s.guest
	d := s.code.current()
	if d == nil {
		return false, nil
	}
	handled, cmd := d.ed.HandleKey(msg)
	s.guestFlushOps(d)
	if !handled {
		return msg.String() == "esc", nil
	}
	if cmd == nil {
		return true, nil
	}
	switch m := cmd().(type) {
	case editor.UndoMsg:
		if op, ok := g.eng.Undo(d.rel); ok {
			d.ed.ApplyRemote(op)
		}
	case editor.RedoMsg:
		if op, ok := g.eng.Redo(d.rel); ok {
			d.ed.ApplyRemote(op)
		}
	case editor.SaveMsg:
		var c tea.Cmd
		if err := g.eng.Save(d.rel); err != nil {
			c = ui.Toast(ctx.T.Workspace.SaveFailed(), ui.ToastError)
		}
		if m.Close {
			s.guestCloseDoc(ctx, d)
		}
		return true, c
	case editor.CloseMsg:
		s.guestCloseDoc(ctx, d)
	}
	return true, nil
}

// guestFlushOps sends my new edits of d to the owner (a refused one puts
// my copy back in the editor) and queues my cursor.
func (s *Surface) guestFlushOps(d *openDoc) {
	g := s.guest
	for _, op := range d.ed.TakeOps() {
		if op.IsNoop() {
			continue
		}
		if err := g.eng.Edit(d.rel, op); err != nil {
			if text, ok := g.eng.DocText(d.rel); ok {
				d.ed.TakeOps()
				d.ed.Reset(text)
			}
			break
		}
	}
	s.guestNotePresence(d)
}

// guestCloseDoc closes a remote document.
func (s *Surface) guestCloseDoc(ctx *ui.Context, d *openDoc) {
	i := slices.Index(s.code.docs, d)
	if i < 0 {
		return
	}
	_ = s.guest.eng.CloseDoc(d.rel)
	delete(s.guest.cursors, d.rel)
	s.code.docs = slices.Delete(s.code.docs, i, i+1)
	if s.code.cur >= len(s.code.docs) {
		s.code.cur = len(s.code.docs) - 1
	}
	if len(s.code.docs) == 0 {
		s.setFocus(ctx, PaneTree)
	}
}

// guestNotePresence queues my cursor when it moved (sent at most every
// presenceEvery by the pump).
func (s *Surface) guestNotePresence(d *openDoc) {
	g := s.guest
	cur := d.ed.CursorIndex()
	from, to, ok := d.ed.Selection()
	if !ok {
		from, to = 0, 0
	}
	p := wire.DocPresence{Doc: d.rel, Cursor: cur, SelFrom: from, SelTo: to}
	if p == g.sentPresence && g.presence == nil {
		return
	}
	g.presence = &p
}

// guestPresence keeps someone's cursor (the owner's or another participant's).
func (s *Surface) guestPresence(ctx *ui.Context, p wire.DocPresence) {
	g := s.guest
	user := g.info.Owner.ID
	for _, m := range g.roster {
		if m.Seat == p.Seat {
			user = m.UserID
		}
	}
	if p.Seat != wire.SeatOwner && user == g.info.Owner.ID {
		return // a seat not on the roster
	}
	pc := peerCursor{seat: p.Seat, peer: editor.Peer{
		Name: s.guestPeerName(p.Seat, user), Color: ctx.Theme.UserColor(user, false),
		Cursor: max(p.Cursor, 0), SelFrom: max(p.SelFrom, 0), SelTo: max(p.SelTo, 0),
	}}
	list := slices.DeleteFunc(g.cursors[p.Doc], func(c peerCursor) bool { return c.seat == p.Seat })
	list = append(list, pc)
	slices.SortFunc(list, func(a, b peerCursor) int { return int(a.seat) - int(b.seat) })
	g.cursors[p.Doc] = list
	s.guestShowPeers(p.Doc)
}

// guestShiftCursors moves rel's cursors over an edit.
func (s *Surface) guestShiftCursors(rel string, op ot.Op) {
	list := s.guest.cursors[rel]
	for i := range list {
		p := &list[i].peer
		p.Cursor = ot.TransformIndex(op, p.Cursor)
		p.SelFrom = ot.TransformIndex(op, p.SelFrom)
		p.SelTo = ot.TransformIndex(op, p.SelTo)
	}
	s.guestShowPeers(rel)
}

func (s *Surface) guestShowPeers(rel string) {
	d := s.code.find(rel)
	if d == nil {
		return
	}
	var peers []editor.Peer
	for _, c := range s.guest.cursors[rel] {
		peers = append(peers, c.peer)
	}
	d.ed.SetPeers(peers)
}

// ---- keys ----

// guestKey handles a key on the participant screen (no dialog open).
func (s *Surface) guestKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	g := s.guest
	if g.phase == guestOver {
		// any key closes the notice and shows the tool hub (either order of
		// the two msgs ends there)
		return true, tea.Batch(ui.Navigate(ui.Route{Kind: ui.RouteTools}), ui.CloseTool(s.id))
	}
	if s.plan.Tiny || g.phase != guestIn {
		if r := s.prefix.Key(ctx, msg); r.Action != termpane.PrefixNone {
			if r.Action == termpane.PrefixExtra && r.Key == "enter" && g.phase == guestSAS && !g.answered {
				s.dlg = newJoinSASDialog(s) // the check number again after esc
				return true, nil
			}
			return s.guestPrefix(ctx, r)
		}
		if msg.String() == "q" {
			return true, s.guestLeave()
		}
		return g.phase != guestIn, nil
	}
	// a terminal that has my keyboard takes every key but the prefix
	if s.guestGrabs() {
		r := s.prefix.Key(ctx, msg)
		if r.Action == termpane.PrefixNone {
			if rt := g.current(); rt != nil {
				_ = g.eng.Input(g.termCur, keyBytes(msg))
			}
			return true, nil
		}
		return s.guestPrefix(ctx, r)
	}
	if rt := g.current(); s.focus == PaneTerm && rt != nil && rt.pan && !s.prefix.Active() && !ctx.Keys.MatchesEscape(msg) {
		if rt.panKey(msg.String()) {
			return true, nil
		}
	}
	if s.focus == PaneTerm && g.composing() && !s.prefix.Active() && !ctx.Keys.MatchesEscape(msg) {
		if ok, cmd := s.composeKey(ctx, msg); ok {
			return true, cmd
		}
	}
	r := s.prefix.Key(ctx, msg)
	if r.Action != termpane.PrefixNone {
		return s.guestPrefix(ctx, r)
	}
	switch s.focus {
	case PaneTree:
		return s.guestTreeKey(ctx, msg)
	case PaneCode:
		return s.guestCodeKey(ctx, msg)
	case PaneTerm:
		return s.guestTermKey(ctx, msg)
	}
	return false, nil
}

// guestPrefix runs a ctrl+\ action on the participant screen.
func (s *Surface) guestPrefix(ctx *ui.Context, r termpane.PrefixResult) (bool, tea.Cmd) {
	switch r.Action {
	case termpane.PrefixArm:
		return true, r.Cmd
	case termpane.PrefixExtra:
		switch r.Key {
		case "1":
			s.setFocus(ctx, PaneTree)
		case "2":
			s.setFocus(ctx, PaneCode)
		case "3":
			s.setFocus(ctx, PaneTerm)
		case "4":
			s.setFocus(ctx, PanePeople)
		case "f":
			if rt := s.guest.current(); rt != nil {
				rt.followOn()
			}
		}
		return true, nil
	case termpane.PrefixScroll:
		if rt := s.guest.current(); rt != nil && s.focus == PaneTerm {
			rt.pan = true
		}
		return true, nil
	case termpane.PrefixZoom:
		s.cellZoom = !s.cellZoom
		s.relayout(ctx)
		return true, nil
	case termpane.PrefixFocus:
		return true, s.moveFocus(ctx, r.Dir)
	case termpane.PrefixPalette:
		return true, func() tea.Msg { return ui.ShellActionMsg{Action: ui.ShellPalette} }
	case termpane.PrefixSurface:
		return true, func() tea.Msg { return ui.ToolSurfaceMsg{Action: ui.ToolSurfaceToggle} }
	case termpane.PrefixKill: // my own share elsewhere is cut
		return true, killNow(ctx)
	case termpane.PrefixLiteral:
		if s.guestGrabs() {
			_ = s.guest.eng.Input(s.guest.termCur, []byte{0x1c})
		}
		return true, nil
	}
	return true, nil
}

// guestTermKey: [ and ] switch the shared terminals.
func (s *Surface) guestTermKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	g := s.guest
	ids := g.termIDs()
	if len(ids) < 2 {
		return false, nil
	}
	i := slices.Index(ids, g.termCur)
	switch msg.String() {
	case "[":
		g.termCur = ids[(i+len(ids)-1)%len(ids)]
	case "]":
		g.termCur = ids[(i+1)%len(ids)]
	default:
		return false, nil
	}
	return true, nil
}

// guestGrabs: my keyboard level and the focus on a shared terminal (not
// panning).
func (s *Surface) guestGrabs() bool {
	g := s.guest
	if g == nil || g.phase != guestIn || s.dlg != nil || s.plan.Tiny || s.focus != PaneTerm || g.level < wire.LevelKeyboard {
		return false
	}
	rt := g.current()
	return rt != nil && !rt.pan
}

// guestLeave leaves the share and closes the screen.
func (s *Surface) guestLeave() tea.Cmd {
	return ui.CloseTool(s.id)
}

// guestClose leaves the share when the screen closes.
func (s *Surface) guestClose() tea.Cmd {
	g := s.guest
	if g == nil {
		return nil
	}
	eng := g.start.close()
	if eng == nil {
		eng = g.eng
	}
	g.eng = nil
	s.dropRemote()
	if eng == nil {
		return nil
	}
	if manualTick {
		eng.Leave()
		return nil
	}
	return func() tea.Msg { eng.Leave(); return nil }
}

// ---- drawing ----

// guestBand is the participant's top line.
func (s *Surface) guestBand(ctx *ui.Context, w int) string {
	g := s.guest
	t := ctx.T.Workspace
	var parts []string
	if g.phase == guestIn {
		parts = append(parts, t.GuestBand(g.title(), g.ownerDisplay(), levelName(ctx, g.level)))
		if !g.verified {
			parts = append(parts, t.Unverified())
		}
		if g.level >= wire.LevelPrompter && g.unsaved > 0 {
			parts = append(parts, t.TermTitleUnsaved(g.unsaved))
		}
	} else {
		parts = append(parts, g.title(), t.JoinConnecting())
	}
	if g.reconnecting {
		parts = append(parts, t.GuestReconnecting())
	}
	st := lipgloss.NewStyle().Foreground(ctx.Theme.P.Acc2).Bold(true)
	return st.Render(textwidth.Truncate(" "+strings.Join(parts, " · "), w, "…"))
}

// guestView draws the participant screen.
func (s *Surface) guestView(ctx *ui.Context, w, h int, focused bool) string {
	g := s.guest
	t := ctx.T.Workspace
	th := ctx.Theme
	if g.phase == guestOver {
		var lines []string
		for range max((h-4)/2, 0) {
			lines = append(lines, "")
		}
		for _, l := range textwidth.Wrap(g.overText(ctx), max(w-2, 1)) {
			lines = append(lines, textwidth.Center(th.S.Title.Render(l), w))
		}
		lines = append(lines, "", textwidth.Center(lipgloss.NewStyle().Foreground(th.P.Dim).Render(t.EndedHint()), w))
		return strings.Join(textwidth.Fit(lines, w, h), "\n")
	}
	if s.plan.Tiny {
		return strings.Join(textwidth.Fit(s.guestTiny(ctx, w, h), w, h), "\n")
	}
	if g.phase != guestIn {
		lines := []string{s.guestBand(ctx, w), ""}
		msg := t.JoinConnecting()
		if g.phase == guestWaiting {
			msg = t.JoinWaitingOwner(g.ownerDisplay())
		}
		for _, l := range textwidth.Wrap(msg, max(w-2, 1)) {
			lines = append(lines, " "+lipgloss.NewStyle().Foreground(th.P.Dim).Render(l))
		}
		return strings.Join(textwidth.Fit(lines, w, h), "\n")
	}
	p := s.plan
	var blocks []block
	add := func(r rect, lines []string) {
		if r.W > 0 && r.H > 0 {
			blocks = append(blocks, block{r, textwidth.Fit(lines, r.W, r.H)})
		}
	}
	add(p.Band, []string{s.guestBand(ctx, p.Band.W)})
	add(p.SubTabs, []string{s.subTabsLine(ctx, p.SubTabs.W)})
	add(p.Tree, s.tree.view(ctx, s, p.Tree.W, p.Tree.H, focused && s.focus == PaneTree))
	add(p.Code, s.code.view(ctx, s, p.Code.W, p.Code.H, focused && s.focus == PaneCode))
	tr := s.termRect()
	if tr != p.Term {
		add(rect{X: p.Term.X, Y: p.Term.Y, W: 1, H: p.Term.H}, separatorColumn(ctx, p.Term.H))
	}
	add(tr, s.guestTermView(ctx, tr.W, tr.H, focused && s.focus == PaneTerm))
	add(p.People, s.peopleView(ctx, p.People.W, p.People.H, focused && s.focus == PanePeople))
	return strings.Join(drawBlocks(blocks, w, h), "\n")
}

// guestTiny is the tiny participant summary: what is watched, and leave.
func (s *Surface) guestTiny(ctx *ui.Context, w, h int) []string {
	g := s.guest
	t := ctx.T.Workspace
	dim := lipgloss.NewStyle().Foreground(ctx.Theme.P.Dim)
	lines := []string{s.guestBand(ctx, w)}
	for _, l := range textwidth.Wrap(t.GuestTiny(g.title()), max(w-1, 1)) {
		lines = append(lines, " "+l)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	return append(lines, " "+dim.Render(t.GuestTinyKeys()))
}

// termIDs are the shared terminals in id order.
func (g *guestShare) termIDs() []uint8 { return slices.Sorted(maps.Keys(g.terms)) }

// current is the shown shared terminal (nil when none).
func (g *guestShare) current() *replayTerm {
	if rt, ok := g.terms[g.termCur]; ok {
		return rt
	}
	ids := g.termIDs()
	if len(ids) == 0 {
		return nil
	}
	g.termCur = ids[0]
	return g.terms[g.termCur]
}

// term is the replay of shared terminal id (made on first use).
func (g *guestShare) term(id uint8) *replayTerm {
	rt := g.terms[id]
	if rt == nil {
		rt = newReplayTerm(id)
		g.terms[id] = rt
	}
	return rt
}

// guestPaste puts pasted text where my keys go: the proposal line, the
// owner's terminal (keyboard level) or the open document (editor level).
func (s *Surface) guestPaste(ctx *ui.Context, text string) {
	g := s.guest
	if g.phase != guestIn || s.plan.Tiny {
		return
	}
	switch {
	case s.focus == PaneTerm && g.composing():
		g.compose.add(text)
	case s.guestGrabs():
		_ = g.eng.Input(g.termCur, []byte(textwidth.SanitizeInput(text)))
	case s.focus == PaneCode:
		d := s.code.current()
		if d == nil || d.ed.ReadOnly() || d.ed.Mode() != "insert" {
			return
		}
		for _, r := range textwidth.SanitizeInput(text) {
			if r == '\n' {
				d.ed.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				d.ed.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
		s.guestFlushOps(d)
	}
}
