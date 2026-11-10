package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Typing indicator constants.
const (
	typingNameCells = 6                     // each name is cut to this many cells
	typingGrace     = 50 * time.Millisecond // the redraw lands just after the expiry
)

// typingTickMsg redraws a conversation right after a typing indicator expired
// (ch = its channel; the views re-read chatstore.Typing in View).
type typingTickMsg struct{ ch string }

// typingText is the typing line of ids (already without me and blocked users):
// "민지 입력 중…", "민지, 준호 입력 중…", "여러 명이 입력 중…" (3 or more), "" for none.
// Names are sanitized and cut to typingNameCells each.
func typingText(ctx *ui.Context, ids []string) string {
	switch {
	case len(ids) == 0:
		return ""
	case len(ids) >= 3:
		return cat(ctx).Chat.TypingMany()
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := cat(ctx).Chat.TypingSomeone()
		if ctx != nil && ctx.Store != nil {
			if u := ctx.Store.User(id); u != nil {
				if n := strings.TrimSpace(textwidth.Sanitize(displayName(*u))); n != "" {
					name = n
				}
			}
		}
		names = append(names, strings.TrimRight(textwidth.Truncate(name, typingNameCells, "…"), " ")) // Truncate pads a split wide char
	}
	if len(names) == 2 {
		return cat(ctx).Chat.TypingTwo(names[0], names[1])
	}
	return cat(ctx).Chat.TypingOne(names[0])
}

// typingLine is the typing text of one scope (rootID "" = the channel's own list).
func typingLine(ctx *ui.Context, channelID, rootID string) string {
	c := chatOf(ctx)
	if c == nil || ctx.Now == nil {
		return ""
	}
	var root *string
	if rootID != "" {
		root = &rootID
	}
	return typingText(ctx, c.Typing(channelID, root, ctx.Now()))
}

// renderTypingLine draws the typing line in exactly w cells (S.Dim, italic).
func renderTypingLine(ctx *ui.Context, text string, w int) string {
	st := themeOf(ctx).S.Dim.Italic(true)
	return cell(st.Render, text, w)
}

// typingTick arms the redraw of a typing frame of this scope (nil for other
// events or scopes): expires_in_ms + typingGrace later.
func typingTick(ctx *ui.Context, ev realtime.Event, channelID, rootID string) tea.Cmd {
	if ev.Type != "typing" {
		return nil
	}
	d, err := ev.Decode()
	if err != nil {
		return nil
	}
	ty, ok := d.(realtime.Typing)
	if !ok || ty.ChannelID != channelID {
		return nil
	}
	got := ""
	if ty.ThreadRootID != nil {
		got = *ty.ThreadRootID
	}
	if got != rootID {
		return nil
	}
	return ui.After(ctx, time.Duration(max(ty.ExpiresInMS, 0))*time.Millisecond+typingGrace, typingTickMsg{ch: channelID})
}
