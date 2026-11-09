package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The owner's side of a share (spec B6–B8, P2 Task 21–22): the engine
// (share.Host) runs beside the solo surface; this file starts it, pumps its
// events into the cells and draws the sharing band.

// The engine seams (export_test.go swaps them).
var (
	// dialFor is how the host connects (nil = share.Dialer on the API client).
	dialFor share.DialFunc
	// manualTick: the pump drives the host's Tick (tests).
	manualTick bool
	// ownerActivity reports an owner key press to the host.
	ownerActivity = func(h *share.Host, now time.Time) { h.OwnerActivity(now) }
	// shareTerminal adapts a running terminal for sharing (nil = cannot).
	shareTerminal = func(t *term) share.Terminal {
		if t.pane == nil {
			return nil
		}
		return newPaneTerm(t.pane)
	}
	// formatSAS shows a check number (goldens pin it).
	formatSAS = func(s string) string { return s }
	// ringBell sounds the terminal bell (BEL straight to the terminal; P0
	// toasts have no sound of their own).
	ringBell = func() tea.Cmd { return tea.Raw("\a") }
)

// ownerShare is one running share of this folder.
type ownerShare struct {
	host    *share.Host
	id      string
	title   string
	members []api.WorkspaceShareMember
	files   bool
	terms   map[string]uint8 // terminal instance id -> shared terminal id
	paused  bool
	asked   []share.JoinRequest // join requests not answered yet
	sasSeat uint8               // the seat whose check-number dialog is open
	people  int                 // selected row of the people list
	tinySel int                 // selected proposal in the tiny summary
	audit   *audit.Log
}

// peerInfo is a participant as the screens name them.
type peerInfo struct {
	seat         uint8
	userID       string
	handle, name string // sanitized
	own          bool   // the owner's other device
}

// shareStartedMsg ends the share start (API, engine).
type shareStartedMsg struct {
	surface string
	host    *share.Host
	info    *api.WorkspaceShare
	log     *audit.Log
	opts    shareOpts
	err     error
}

// launchTermMsg starts argv in a surface's terminal cell.
type launchTermMsg struct {
	surface string
	argv    []string
	mode    termhost.Mode
}

func (s *Surface) ownerSharing() bool { return s.own != nil }
func (s *Surface) ownerActive() bool  { return s.own != nil }
func (s *Surface) ownerPeople() int {
	if s.own == nil {
		return 0
	}
	return len(s.own.host.Members())
}

// peer names seat's participant: the share's member list, then friends.
func (s *Surface) peer(ctx *ui.Context, seat uint8, userID string) peerInfo {
	p := peerInfo{seat: seat, userID: userID, handle: userID, name: userID}
	var u *api.UserSummary
	if s.own != nil {
		for i := range s.own.members {
			if m := &s.own.members[i]; m.User.ID == userID {
				u = &m.User
			}
		}
	}
	if u == nil && ctx.Store != nil {
		if f, ok := ctx.Store.Friends[userID]; ok {
			u = &f.User
		}
	}
	if u != nil {
		p.handle = textwidth.Sanitize(u.Handle)
		p.name = textwidth.Sanitize(u.DisplayName)
		if p.name == "" {
			p.name = p.handle
		}
	}
	if me := ctx.Me(); me != nil && me.ID == userID {
		p.own = true
	}
	return p
}

// ownerName is the owner as the warnings name them.
func ownerName(ctx *ui.Context) string {
	if me := ctx.Me(); me != nil {
		if n := textwidth.Sanitize(me.DisplayName); n != "" {
			return n
		}
		return textwidth.Sanitize(me.Handle)
	}
	return ""
}

// startShare creates the share on the server and starts the engine (a Cmd:
// both may wait on the network).
func (s *Surface) startShare(ctx *ui.Context, o shareOpts) tea.Cmd {
	me := ctx.Me()
	if me == nil || ctx.API == nil || s.own != nil {
		return nil
	}
	client, id, ownerID := ctx.API, s.id, me.ID
	dial := dialFor
	if dial == nil {
		dial = share.Dialer(client)
	}
	manual, clock := manualTick, nowClock(ctx)
	root, docs := s.root, s.docs
	if !o.files {
		root, docs = nil, nil
	}
	auditDir := ""
	if ctx.Paths.Cache != "" {
		auditDir = filepath.Join(ctx.Paths.Cache, "workspace-audit")
	}
	users := make([]api.UserRef, len(o.invite))
	for i, h := range o.invite {
		users[i] = api.UserRef("@" + h)
	}
	title := o.title
	return func() tea.Msg {
		bg := context.Background()
		info, err := client.CreateWorkspaceShare(bg, title, users)
		if err != nil {
			return shareStartedMsg{surface: id, err: err}
		}
		var log *audit.Log
		if auditDir != "" {
			if l, err := audit.Open(auditDir, info.ID); err == nil {
				log = l
			}
		}
		shareID := info.ID
		host := share.NewHost(share.HostConfig{
			ShareID: shareID, OwnerID: ownerID, Title: title, Dial: dial, Audit: log, Clock: clock,
			ManualTick: manual, Root: root, Docs: docs,
			RemoveParticipant: func(ctx context.Context, userID string) error {
				return client.RemoveWorkspaceParticipant(ctx, shareID, api.UserRef(userID))
			},
			EndShare: func(ctx context.Context) error { return client.EndWorkspaceShare(ctx, shareID) },
		})
		if err := host.Start(bg); err != nil {
			_ = client.EndWorkspaceShare(bg, shareID)
			if log != nil {
				_ = log.Close()
			}
			return shareStartedMsg{surface: id, err: err}
		}
		return shareStartedMsg{surface: id, host: host, info: info, log: log, opts: o}
	}
}

// shareStarted installs a started share: the chosen terminals, the size,
// autosave.
func (s *Surface) shareStarted(ctx *ui.Context, m shareStartedMsg) tea.Cmd {
	if m.err != nil || m.host == nil {
		slog.Warn("workspace: share start", "err", m.err)
		return ui.Toast(ctx.T.Workspace.ShareFailed(), ui.ToastError)
	}
	if s.closed || s.own != nil {
		_ = m.host.End(context.Background())
		return nil
	}
	s.own = &ownerShare{host: m.host, id: m.info.ID, title: m.info.Title, members: m.info.Members, files: m.opts.files,
		terms: map[string]uint8{}, audit: m.log}
	if m.opts.files {
		s.docs.SetAutosave(m.opts.autosave)
	}
	size := share.FixedSizes[min(max(m.opts.size, 0), len(share.FixedSizes)-1)]
	next := uint8(1)
	for _, t := range s.term.terms {
		if !m.opts.terms[t.id] {
			continue
		}
		st := shareTerminal(t)
		if st == nil {
			continue
		}
		if err := m.host.ShareTerminal(next, st); err != nil {
			continue
		}
		if size[0] > 0 {
			_ = m.host.SetTermSize(next, size[0], size[1])
		}
		s.own.terms[t.id] = next
		next++
	}
	s.relayout(ctx)
	return s.armPump(ctx)
}

// ownerUpdate takes the msgs of the share.
func (s *Surface) ownerUpdate(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case shareStartedMsg:
		if m.surface == s.id {
			return s.shareStarted(ctx, m)
		}
	case launchTermMsg:
		if m.surface == s.id {
			return s.launchTerm(ctx, m.argv, m.mode)
		}
	case wsCommandMsg:
		if m.surface == s.id {
			return s.wsCommand(ctx, m.action)
		}
	case shareEndMsg:
		if m.surface == s.id {
			cmd := s.shareEnded(ctx)
			return tea.Batch(cmd, ui.Toast(ctx.T.Workspace.ShareEnded(), ui.ToastInfo))
		}
	}
	return nil
}

// ownerPump drives the engine one beat and applies what it reported.
func (s *Surface) ownerPump(ctx *ui.Context) tea.Cmd {
	o := s.own
	if manualTick {
		o.host.Tick(nowClock(ctx).Now())
	}
	if s.docs != nil {
		o.host.SetUnsaved(len(s.docs.Dirty()))
	}
	var cmds []tea.Cmd
	for _, ev := range o.host.Drain() {
		cmds = append(cmds, s.ownerEvent(ctx, ev))
		if s.own == nil {
			break
		}
	}
	return tea.Batch(cmds...)
}

// ownerEvent applies one engine event.
func (s *Surface) ownerEvent(ctx *ui.Context, ev share.Event) tea.Cmd {
	t := ctx.T.Workspace
	o := s.own
	switch e := ev.(type) {
	case share.EventJoinRequest:
		// The request waits (toast, band, people list) until the owner opens
		// its dialog: a dialog that popped up by itself would take the keys
		// the owner was typing and could let someone in unchecked.
		o.asked = append(o.asked, e.JoinRequest)
		name := s.peer(ctx, e.Seat, e.UserID).name
		return tea.Batch(ui.Toast(t.JoinRequesting(name), ui.ToastAlert), ringBell())
	case share.EventJoinExpired:
		s.dropAsked(e.Seat)
	case share.EventPeerJoined:
		s.dropAsked(e.Seat)
		return ui.Toast(t.PeerJoined(s.peer(ctx, e.Seat, e.UserID).name), ui.ToastInfo)
	case share.EventPeerLeft:
		return ui.Toast(t.PeerLeft(s.peer(ctx, e.Seat, e.UserID).name), ui.ToastInfo)
	case share.EventMITMWarning:
		s.dropAsked(e.Seat)
		s.dlg = noticeDialog(func(ctx *ui.Context) string { return ctx.T.Workspace.SasMismatch() },
			func(ctx *ui.Context) string { return ctx.T.Workspace.SasMismatchWarning() })
	case share.EventTamperWarning:
		s.dlg = tamperDialog(s)
	case share.EventKeyboardPaused:
		o.paused = true
	case share.EventKeyboardResumed:
		o.paused = false
	case share.EventDocApplied:
		if d := s.code.find(e.Doc); d != nil {
			s.applyRemote(d, e.Seq, e.Op)
		}
	case share.EventDoc:
		return s.docEvents(ctx, []share.DocEvent{e.DocEvent})
	case share.EventProposal:
		return s.onProposal(ctx, e.Proposal)
	case share.EventEnded:
		return s.shareEnded(ctx)
	}
	return nil
}

// shareEnded goes back to the solo surface; the programs keep running.
func (s *Surface) shareEnded(ctx *ui.Context) tea.Cmd {
	if s.own == nil {
		return nil
	}
	if s.own.audit != nil {
		_ = s.own.audit.Close()
	}
	s.own = nil
	if s.docs != nil {
		s.docs.SetAutosave(false)
	}
	if s.focus == PanePeople {
		s.focus = PaneTree
	}
	if s.dlg != nil && s.dlg.shareOnly {
		s.dlg = nil
	}
	s.relayout(ctx)
	return nil
}

func (s *Surface) dropAsked(seat uint8) {
	o := s.own
	for i, j := range o.asked {
		if j.Seat == seat {
			o.asked = append(o.asked[:i], o.asked[i+1:]...)
			break
		}
	}
	if o.sasSeat == seat && s.dlg != nil && s.dlg.shareOnly {
		s.dlg, o.sasSeat = nil, 0
	}
}

// nextJoinDialog opens the check-number dialog of the oldest waiting
// request when no dialog is open (only on an owner action: ctrl+\ enter, enter
// in the people list); false when nothing waits.
func (s *Surface) nextJoinDialog(ctx *ui.Context) bool {
	o := s.own
	if o == nil || s.dlg != nil || len(o.asked) == 0 {
		return false
	}
	j := o.asked[0]
	o.sasSeat = j.Seat
	s.dlg = newSASDialog(s, ctx, j)
	return true
}

// openKey is the key that opens what waits for the owner: ctrl+\ enter.
func openKey(ctx *ui.Context) string {
	return ctx.Keys.Binding(keymap.TermhostEscape).Help().Key + " enter"
}

// ownerBand is the sharing band: 🔴 공유 중 · n명 · who and how (B6.7).
func (s *Surface) ownerBand(ctx *ui.Context) (string, lipgloss.Style, bool) {
	o := s.own
	if o == nil {
		return "", lipgloss.Style{}, false
	}
	t := ctx.T.Workspace
	ms := o.host.Members()
	parts := []string{t.BandSharing(len(ms))}
	var tags []string
	now := nowClock(ctx).Now()
	for _, m := range ms {
		p := s.peer(ctx, m.Seat, m.UserID)
		tags = append(tags, p.handle+"["+levelTag(ctx, m, now)+"]")
	}
	if len(tags) > 0 {
		parts = append(parts, strings.Join(tags, " "))
	}
	if o.paused {
		parts = append(parts, t.KeyboardPaused())
	}
	if n := len(o.asked); n > 0 {
		parts = append(parts, t.JoinPending(n, openKey(ctx)))
	}
	if n := len(o.host.Proposals()); n > 0 {
		parts = append(parts, t.ProposalPending(n, openKey(ctx)))
	}
	st := lipgloss.NewStyle().Foreground(ctx.Theme.P.Acc2).Bold(true)
	return strings.Join(parts, " · "), st, true
}

// levelTag is "편집", "키보드 28:41", "보기·미확인".
func levelTag(ctx *ui.Context, m share.Member, now time.Time) string {
	t := ctx.T.Workspace
	var s string
	switch m.Level {
	case wire.LevelViewer:
		s = t.LevelViewer()
	case wire.LevelPrompter:
		s = t.LevelPrompter()
	case wire.LevelEditor:
		s = t.LevelEditor()
	case wire.LevelKeyboard:
		s = t.KeyboardRemaining(mmss(m.KeyboardUntil.Sub(now)))
	}
	if !m.Verified {
		s += "·" + t.Unverified()
	}
	return s
}

// mmss is a remaining time as minutes:seconds (rounded up to the second).
func mmss(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int((d + time.Second - 1) / time.Second)
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func (s *Surface) ownerBadge() ui.Badge {
	if s.own != nil {
		return ui.Badge{Dot: true}
	}
	return ui.Badge{}
}

// ownerClose ends the share when the workspace closes.
func (s *Surface) ownerClose(*ui.Context) tea.Cmd {
	o := s.own
	if o == nil {
		return nil
	}
	s.own = nil
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := o.host.End(ctx); err != nil && !errors.Is(err, share.ErrEnded) {
			o.host.Kill()
		}
		if o.audit != nil {
			_ = o.audit.Close()
		}
		return nil
	}
}

// shareKey is ctrl+\ S.
func (s *Surface) shareKey(ctx *ui.Context) tea.Cmd {
	if s.own != nil || s.root == nil {
		return nil
	}
	s.dlg = newShareDialog(s, ctx)
	return nil
}

func (s *Surface) onTermStarted(*ui.Context, *term) {}

// onTermEnded stops sharing an ended terminal.
func (s *Surface) onTermEnded(_ *ui.Context, t *term) {
	if s.own == nil {
		return
	}
	if id, ok := s.own.terms[t.id]; ok {
		s.own.host.UnshareTerminal(id)
		delete(s.own.terms, t.id)
	}
}

// noteOwnerKey tells the engine the owner is at the keyboard.
func (s *Surface) noteOwnerKey(ctx *ui.Context) {
	if s.own != nil {
		ownerActivity(s.own.host, nowClock(ctx).Now())
	}
}

// noticeDialog is a warning with one way out.
func noticeDialog(title, text func(*ui.Context) string) *form {
	return &form{title: title, shareOnly: true, build: func(ctx *ui.Context) []row {
		return []row{{kind: rowText, text: text(ctx), warn: true}, {kind: rowText},
			{kind: rowOption, text: ctx.T.Workspace.Cancel(), act: func(*ui.Context) tea.Cmd { return nil }}}
	}}
}

// tamperDialog asks whether to stop a share whose frames keep failing.
func tamperDialog(s *Surface) *form {
	f := &form{shareOnly: true}
	f.title = func(ctx *ui.Context) string { return ctx.T.Workspace.BandSharing(s.ownerPeople()) }
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		return []row{{kind: rowText, text: t.TamperWarning(), warn: true}, {kind: rowText},
			{kind: rowOption, text: t.TamperStop(), act: func(*ui.Context) tea.Cmd {
				s.dlg = nil
				if s.own != nil {
					s.own.host.Kill()
				}
				return nil
			}},
			{kind: rowOption, text: t.TamperContinue(), act: func(*ui.Context) tea.Cmd { s.dlg = nil; return nil }},
		}
	}
	return f
}

// wsCommand runs a palette command on this instance.
func (s *Surface) wsCommand(ctx *ui.Context, action string) tea.Cmd {
	switch action {
	case "share.start":
		return s.shareKey(ctx)
	case "share.end":
		if s.own != nil {
			s.dlg = newEndDialog(s)
		}
	case "proposals":
		return s.openProposals(ctx)
	}
	return nil
}
