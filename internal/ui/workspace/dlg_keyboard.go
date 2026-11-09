package workspace

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
)

// durationLabel is "10분", "1시간".
func durationLabel(ctx *ui.Context, d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return ctx.T.Workspace.KeyboardHours(int(d / time.Hour))
	}
	return ctx.T.Workspace.KeyboardMinutes(int(d / time.Minute))
}

// handleMatches is the keyboard confirmation (spec B8.4): the participant's
// handle typed as is — ASCII, any case. A display name, or anything with a
// rune the screen would not show, never matches.
func handleMatches(typed, handle string) bool {
	return typed != "" && typed == textwidth.SanitizeInput(typed) && strings.EqualFold(typed, handle)
}

// newKeyboardDialog is the keyboard warning (B8.4, B13): the whole warning,
// the short terms notice, the duration and the typed handle.
func newKeyboardDialog(s *Surface, ctx *ui.Context, p peerInfo) *form {
	d := share.DefaultKeyboard
	typed := &field{label: ctx.T.Workspace.KeyboardHandleLabel(), raw: true}
	mismatch := false
	f := &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.KeyboardTitle(p.name) }}
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		on := func(b bool) *bool { return &b }
		owner := ownerName(ctx)
		warning := t.KeyboardWarning(p.name, owner)
		if p.own {
			warning = t.KeyboardWarningOwnDevice(owner)
		}
		rows := []row{{kind: rowText, text: warning, warn: true}, {kind: rowText},
			{kind: rowText, text: t.LegalNoticeShort(owner)}, {kind: rowText},
			{kind: rowHead, text: t.KeyboardDuration()}}
		for _, x := range share.KeyboardDurations {
			rows = append(rows, row{kind: rowOption, text: durationLabel(ctx, x), checked: on(d == x), act: func(*ui.Context) tea.Cmd {
				d = x
				return nil
			}})
		}
		rows = append(rows, row{kind: rowText}, row{kind: rowText, text: t.KeyboardTypeHandle(p.handle)},
			row{kind: rowInput, input: typed, act: func(ctx *ui.Context) tea.Cmd {
				if !handleMatches(typed.value(), p.handle) {
					mismatch = true
					return nil
				}
				s.dlg = nil
				if s.own == nil {
					return nil
				}
				return approveErr(ctx, s.own.host.GrantKeyboard(p.seat, d))
			}})
		if mismatch {
			rows = append(rows, row{kind: rowText, text: t.KeyboardHandleMismatch(), warn: true})
		}
		return rows
	}
	return f
}

// newExtendDialog moves a running keyboard grant's end (K).
func newExtendDialog(s *Surface, p peerInfo) *form {
	f := &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.KeyboardExtend(p.name) }}
	f.build = func(ctx *ui.Context) []row {
		rows := []row{{kind: rowHead, text: ctx.T.Workspace.KeyboardDuration()}}
		for _, x := range share.KeyboardDurations {
			rows = append(rows, row{kind: rowOption, text: durationLabel(ctx, x), act: func(ctx *ui.Context) tea.Cmd {
				s.dlg = nil
				if s.own == nil {
					return nil
				}
				return approveErr(ctx, s.own.host.ExtendKeyboard(p.seat, x))
			}})
		}
		return rows
	}
	return f
}
