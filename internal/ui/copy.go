package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/clipboard"
	"github.com/patrache/masil/internal/textwidth"
)

// CopyHint is the toast when the clipboard program failed (OSC 52 was still
// sent, but many terminals ignore it): the terminal's own selection is the
// way out.
const CopyHint = "클립보드에 못 넣었어요 — ⌥·fn·Shift 누른 채 드래그"

// CopySentHint is the toast when there is no clipboard program (Wayland
// without wl-clipboard, a console): only OSC 52 was sent, which many
// terminals honor (kitty, foot, WezTerm, Ghostty, Windows Terminal) and
// others ignore, so the toast claims neither success nor failure.
const CopySentHint = "터미널로 복사를 보냈어요 — 안 붙으면 Shift 누른 채 드래그 (또는 wl-clipboard·xclip 설치)"

// Copy is the one way UI code copies text (CA 5.6 clipboard): the text is
// sanitized (textwidth.SanitizeInput: newlines kept, escapes and controls
// dropped) and capped at clipboard.MaxBytes, then sent both as OSC 52
// (tea.SetClipboard, which reaches the terminal across SSH) and to the system
// clipboard program (ctx.Clipboard, run inside the Cmd, off the Update
// goroutine). The toast says okText when one path is believed to work — the
// program succeeded, or the session is remote (SSH: OSC 52 is the path) —
// CopySentHint when there is no program (OSC 52 alone, unknown) and CopyHint
// when the program failed. Without a Writer (unit tests) it is OSC 52 only.
func Copy(ctx *Context, text, okText string) tea.Cmd {
	text = clipboard.Clamp(textwidth.SanitizeInput(text), clipboard.MaxBytes)
	var w clipboard.Writer
	if ctx != nil {
		w = ctx.Clipboard
	}
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		if w != nil {
			switch err := w.Write(text); {
			case err == nil, errors.Is(err, clipboard.ErrRemote):
			case errors.Is(err, clipboard.ErrUnavailable):
				return ToastMsg{Text: CopySentHint, Kind: ToastInfo}
			default:
				return ToastMsg{Text: CopyHint, Kind: ToastInfo}
			}
		}
		return ToastMsg{Text: okText, Kind: ToastSuccess}
	})
}
