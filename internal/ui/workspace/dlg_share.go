package workspace

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/launch"
	"github.com/patrache/masil/internal/workspace/share"
)

// shareOpts is what the share dialog chose.
type shareOpts struct {
	title    string
	invite   []string // handles, seat order
	files    bool
	terms    map[string]bool // terminal instance ids
	size     int             // index into share.FixedSizes
	autosave bool
}

// newShareDialog is the share start (spec B6.5): one screen that wraps and
// scrolls, never cuts. Sharing starts only with its enter.
func newShareDialog(s *Surface, ctx *ui.Context) *form {
	handle := ""
	if me := ctx.Me(); me != nil {
		handle = textwidth.Sanitize(me.Handle)
	}
	title := &field{label: ctx.T.Workspace.ShareTitleLabel(), r: []rune(ctx.T.Workspace.ShareTitleDefault(handle))}
	pick := &invitePick{}
	sp := &spacePick{}
	s.spacePick = sp
	files, autosave := true, false
	size := 0
	terms := map[string]bool{}
	type termCheck struct {
		t       *term
		verdict launch.Verdict
	}
	var checks []termCheck
	for _, t := range s.term.terms {
		v := launch.Check(t.Argv())
		if !paneSupported() {
			v.Allowed = false
		}
		checks = append(checks, termCheck{t, v})
		terms[t.id] = v.Allowed && shareTerminal(t) != nil
	}
	f := &form{
		title: func(ctx *ui.Context) string { return ctx.T.Workspace.ShareDialogTitle() },
		hint:  func(ctx *ui.Context) string { return ctx.T.Workspace.ShareDialogHint() },
	}
	f.keys = func(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
		if msg.String() != "enter" {
			return false, nil
		}
		t := []rune(textwidth.SanitizeInput(title.value()))
		if len(t) == 0 {
			t = []rune(ctx.T.Workspace.ShareTitleDefault(handle))
		}
		if len(t) > 40 {
			t = t[:40]
		}
		o := shareOpts{title: string(t), invite: append([]string(nil), pick.handles...), files: files, terms: map[string]bool{},
			size: size, autosave: autosave && files}
		for id, on := range terms {
			o.terms[id] = on
		}
		s.dlg, s.spacePick = nil, nil
		return true, s.startShare(ctx, o)
	}
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		on := func(b bool) *bool { return &b }
		rows := []row{
			{kind: rowInput, input: title},
			{kind: rowText, text: t.ShareTitleServerVisible(), dim: true},
			{kind: rowText},
			{kind: rowHead, text: t.ShareInviteFriends(maxInvite)},
		}
		cands := inviteCandidates(ctx)
		if len(cands) == 0 {
			rows = append(rows, row{kind: rowText, text: t.ShareInviteNone(), dim: true})
		}
		person := func(u api.UserSummary) row {
			h := u.Handle
			return row{kind: rowOption, text: inviteLabel(u), checked: on(pick.has(h)), act: func(ctx *ui.Context) tea.Cmd {
				if !pick.toggle(h) {
					return ui.Toast(ctx.T.Workspace.ShareInviteFull(maxInvite), ui.ToastInfo)
				}
				return nil
			}}
		}
		for _, u := range cands {
			rows = append(rows, person(u))
		}
		// JB18: or the members of one space
		if spaces := spaceChoices(ctx); len(spaces) > 0 {
			rows = append(rows, row{kind: rowText}, row{kind: rowHead, text: t.ShareInviteSpace()})
			for _, sd := range spaces {
				id := sd.ID
				rows = append(rows, row{kind: rowOption, text: textwidth.Sanitize(sd.Name), checked: on(sp.id == id), act: func(ctx *ui.Context) tea.Cmd {
					return sp.choose(ctx, s.id, id)
				}})
				if sp.id != id {
					continue
				}
				switch {
				case sp.loading:
					rows = append(rows, row{kind: rowText, text: t.ShareSpaceLoading(), dim: true})
				case sp.failed:
					rows = append(rows, row{kind: rowText, text: t.ShareSpaceFailed(), warn: true})
				}
				for _, u := range spaceInvitees(ctx, sp.members, cands) {
					rows = append(rows, person(u))
				}
			}
		}
		rows = append(rows, row{kind: rowText}, row{kind: rowHead, text: t.ShareWhat()},
			row{kind: rowOption, text: t.ShareFiles(), checked: on(files), act: func(*ui.Context) tea.Cmd {
				files = !files
				return nil
			}})
		for _, c := range checks {
			c := c
			r := row{kind: rowOption, text: t.ShareTerminal(c.t.n, textwidth.Sanitize(c.t.name)), checked: on(terms[c.t.id])}
			switch {
			case !paneSupported():
				r.disabled = true
			case !c.verdict.Allowed:
				r.disabled = true
			case shareTerminal(c.t) == nil:
				r.disabled = true
			default:
				r.act = func(*ui.Context) tea.Cmd {
					terms[c.t.id] = !terms[c.t.id]
					return nil
				}
			}
			rows = append(rows, r)
			switch {
			case !paneSupported():
				rows = append(rows, row{kind: rowText, text: t.WindowsTermShareDisabled(), warn: true})
			case !c.verdict.Allowed:
				rows = append(rows, row{kind: rowText, text: blockReason(ctx, c.verdict.Reason), warn: true})
			default:
				for _, w := range c.verdict.Warnings {
					rows = append(rows, row{kind: rowText, text: warningText(ctx, w), warn: true})
				}
			}
		}
		rows = append(rows, row{kind: rowText}, row{kind: rowHead, text: t.ShareTermSize()})
		for i, sz := range share.FixedSizes {
			label := t.ShareTermSizeFit()
			if sz[0] > 0 {
				label = strconv.Itoa(sz[0]) + "x" + strconv.Itoa(sz[1])
			}
			rows = append(rows, row{kind: rowOption, text: label, checked: on(size == i), act: func(*ui.Context) tea.Cmd {
				size = i
				return nil
			}})
		}
		rows = append(rows, row{kind: rowText},
			row{kind: rowOption, text: t.ShareAutosave(), checked: on(autosave && files), disabled: !files, act: func(*ui.Context) tea.Cmd {
				autosave = !autosave
				return nil
			}},
			row{kind: rowText},
			row{kind: rowText, text: t.ShareNoticeScreen(), warn: true},
			row{kind: rowText},
			row{kind: rowText, text: t.LegalNotice(ownerName(ctx))},
			row{kind: rowText},
			row{kind: rowText, text: t.CompanyCodeNotice()},
		)
		return rows
	}
	return f
}

// inviteLabel is "display @handle" (sanitized), or the handle alone.
func inviteLabel(u api.UserSummary) string {
	h := textwidth.Sanitize(u.Handle)
	label := textwidth.Sanitize(u.DisplayName)
	if label == "" || label == h {
		return h
	}
	return label + " @" + h
}

// blockReason is why a terminal cannot be shared.
func blockReason(ctx *ui.Context, r launch.Reason) string {
	t := ctx.T.Workspace
	switch r {
	case launch.ReasonBypassFlag:
		return t.CheckBlockedBypass()
	case launch.ReasonUserSettings:
		return t.CheckBlockedSettings()
	}
	return t.CheckBlockedMode()
}

// warningText is a shared program's warning as a sentence.
func warningText(ctx *ui.Context, w launch.Warning) string {
	t := ctx.T.Workspace
	if w == launch.WarnAcceptEdits {
		return t.CheckAcceptEdits()
	}
	return t.CheckShellWarning()
}
