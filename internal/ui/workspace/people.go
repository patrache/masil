package workspace

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The people cell (spec B6.6, B8): who is in, at which level, and the
// owner's controls over them; and the check-number dialog that lets them in.

// newSASDialog is the owner's check-number comparison for one join request.
func newSASDialog(s *Surface, ctx *ui.Context, j share.JoinRequest) *form {
	p := s.peer(ctx, j.Seat, j.UserID)
	level := wire.LevelViewer
	done := func() {
		s.dlg = nil
		if s.own != nil {
			s.own.sasSeat = 0
			s.dropAsked(j.Seat)
		}
	}
	host := s.own.host
	f := &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.SasTitle(p.name) }}
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		on := func(b bool) *bool { return &b }
		rows := []row{{kind: rowText}, {kind: rowBig, text: formatSAS(j.SAS)}, {kind: rowText},
			{kind: rowText, text: t.SasCompareHint()}}
		for _, q := range host.Pending() {
			if q.Seat == j.Seat && q.PeerMatched != nil && *q.PeerMatched {
				rows = append(rows, row{kind: rowText, text: t.SasPeerMatched(p.name), dim: true})
			}
		}
		rows = append(rows, row{kind: rowText}, row{kind: rowHead, text: t.SasLevel()})
		for _, lv := range []wire.Level{wire.LevelViewer, wire.LevelPrompter, wire.LevelEditor} {
			rows = append(rows, row{kind: rowOption, text: levelName(ctx, lv), checked: on(level == lv), act: func(*ui.Context) tea.Cmd {
				level = lv
				return nil
			}})
		}
		rows = append(rows, row{kind: rowText, text: t.SasKeyboardLater(), dim: true}, row{kind: rowText},
			row{kind: rowOption, text: t.SasAllow(), act: func(ctx *ui.Context) tea.Cmd {
				err := host.Approve(j.Seat, level, true)
				done()
				return approveErr(ctx, err)
			}},
			row{kind: rowOption, text: t.SasAllowUnverified(), disabled: level > wire.LevelPrompter, act: func(ctx *ui.Context) tea.Cmd {
				err := host.Approve(j.Seat, level, false)
				done()
				return approveErr(ctx, err)
			}},
			row{kind: rowOption, text: t.SasMismatch(), warn: true, act: func(*ui.Context) tea.Cmd {
				done()
				host.Decline(j.Seat, true)
				return nil
			}},
			row{kind: rowOption, text: t.SasDecline(), act: func(*ui.Context) tea.Cmd {
				done()
				host.Decline(j.Seat, false)
				return nil
			}},
		)
		return rows
	}
	f.cancel = func(*ui.Context) tea.Cmd { // esc: decide later (the request waits)
		if s.own != nil {
			s.own.sasSeat = 0
		}
		return nil
	}
	return f
}

func approveErr(ctx *ui.Context, err error) tea.Cmd {
	switch {
	case err == nil:
		return nil
	case err == share.ErrNeedsVerify:
		return ui.Toast(ctx.T.Workspace.KeyboardNeedsVerify(), ui.ToastError)
	}
	return ui.Toast(ctx.T.Workspace.ShareFailed(), ui.ToastError)
}

// levelName is a level's catalog name.
func levelName(ctx *ui.Context, l wire.Level) string {
	t := ctx.T.Workspace
	switch l {
	case wire.LevelViewer:
		return t.LevelViewer()
	case wire.LevelPrompter:
		return t.LevelPrompter()
	case wire.LevelEditor:
		return t.LevelEditor()
	case wire.LevelKeyboard:
		return t.LevelKeyboard()
	}
	return ""
}

// selectedMember is the people list's selected participant.
func (s *Surface) selectedMember() (share.Member, bool) {
	if s.own == nil {
		return share.Member{}, false
	}
	ms := s.own.host.Members()
	if len(ms) == 0 {
		return share.Member{}, false
	}
	s.own.people = max(0, min(s.own.people, len(ms)-1))
	return ms[s.own.people], true
}

// peopleKey handles a key in the people cell.
func (s *Surface) peopleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.own == nil {
		return false, nil
	}
	o := s.own
	m, ok := s.selectedMember()
	switch msg.String() {
	case "down", "j":
		o.people++
		s.selectedMember()
		return true, nil
	case "up":
		o.people = max(0, o.people-1)
		return true, nil
	case "enter":
		s.nextJoinDialog(ctx)
		return true, nil
	}
	if !ok {
		return false, nil
	}
	p := s.peer(ctx, m.Seat, m.UserID)
	switch msg.String() {
	case "+", "=":
		if m.Level >= wire.LevelEditor {
			return true, s.keyboardKey(ctx, m, p)
		}
		return true, approveErr(ctx, o.host.SetLevel(m.Seat, m.Level+1))
	case "-":
		if m.Level > wire.LevelViewer {
			return true, approveErr(ctx, o.host.SetLevel(m.Seat, m.Level-1))
		}
		return true, nil
	case "k":
		return true, s.keyboardKey(ctx, m, p)
	case "K":
		if m.Level == wire.LevelKeyboard {
			s.dlg = newExtendDialog(s, p)
		}
		return true, nil
	case "x":
		s.dlg = newKickDialog(s, p)
		return true, nil
	}
	return false, nil
}

// keyboardKey opens the keyboard warning (a verified participant only).
func (s *Surface) keyboardKey(ctx *ui.Context, m share.Member, p peerInfo) tea.Cmd {
	if !m.Verified {
		return ui.Toast(ctx.T.Workspace.KeyboardNeedsVerify(), ui.ToastError)
	}
	if m.Level == wire.LevelKeyboard {
		s.dlg = newExtendDialog(s, p)
		return nil
	}
	s.dlg = newKeyboardDialog(s, ctx, p)
	return nil
}

// newKickDialog asks before removing a participant.
func newKickDialog(s *Surface, p peerInfo) *form {
	return &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.KickConfirm(p.name) },
		build: func(ctx *ui.Context) []row {
			t := ctx.T.Workspace
			return []row{{kind: rowText},
				{kind: rowOption, text: t.KickYes(), warn: true, act: func(ctx *ui.Context) tea.Cmd {
					s.dlg = nil
					if s.own != nil {
						return approveErr(ctx, s.own.host.Kick(p.seat))
					}
					return nil
				}},
				{kind: rowOption, text: t.Cancel(), act: func(*ui.Context) tea.Cmd { s.dlg = nil; return nil }},
			}
		}}
}

// peopleView draws the people cell: one row per participant, the last
// input, the keys.
func (s *Surface) peopleView(ctx *ui.Context, w, h int, focused bool) []string {
	if s.own == nil || w <= 0 {
		return nil
	}
	th := ctx.Theme
	t := ctx.T.Workspace
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	sep := lipgloss.NewStyle().Foreground(th.P.Line).Render("│")
	inner := max(w-1, 1)
	if s.plan.Single == PanePeople { // the widget shows the list alone
		sep, inner = "", w
	}
	now := nowClock(ctx).Now()
	ms := s.own.host.Members()
	lines := []string{th.S.Title.Render(textwidth.Truncate(" "+t.PeopleTitle(len(ms)), inner, "…"))}
	var last share.Member
	for i, m := range ms {
		p := s.peer(ctx, m.Seat, m.UserID)
		text := " " + p.handle + " " + levelTag(ctx, m, now)
		if !m.Connected {
			text += " · " + t.PeopleOffline()
		}
		text = textwidth.Truncate(text, inner, "…")
		if i == s.own.people && focused {
			text = th.S.Selected.Render(textwidth.PadRight(text, inner))
		} else if i == s.own.people {
			text = th.S.Cursorline.Render(textwidth.PadRight(text, inner))
		}
		lines = append(lines, text)
		if m.LastInput.After(last.LastInput) {
			last = m
		}
	}
	for _, j := range s.own.asked {
		p := s.peer(ctx, j.Seat, j.UserID)
		lines = append(lines, th.S.Warning.Render(textwidth.Truncate(" "+p.handle+" · "+t.PeopleWaiting(), inner, "…")))
	}
	var foot []string
	if !last.LastInput.IsZero() {
		p := s.peer(ctx, last.Seat, last.UserID)
		for _, l := range textwidth.Wrap(t.LastInput(p.handle, ago(ctx, now.Sub(last.LastInput))), max(inner-1, 1)) {
			foot = append(foot, " "+dim.Render(l))
		}
	}
	for _, l := range textwidth.Wrap(t.PeopleKeys(), max(inner-1, 1)) {
		foot = append(foot, " "+dim.Render(l))
	}
	for len(lines)+len(foot) < h {
		lines = append(lines, "")
	}
	lines = append(lines[:max(0, min(len(lines), h-len(foot)))], foot...)
	out := make([]string, 0, h)
	for _, l := range lines {
		if len(out) == h {
			break
		}
		out = append(out, sep+textwidth.PadRight(l, inner))
	}
	return out
}

// ago is a short "how long ago".
func ago(ctx *ui.Context, d time.Duration) string {
	if d < time.Minute {
		return ctx.T.Workspace.AgoNow()
	}
	return ctx.T.Workspace.AgoMinutes(int(d / time.Minute))
}
