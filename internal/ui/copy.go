package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/clipboard"
	"github.com/patrache/masil/internal/textwidth"
)

// CopyHint is the toast when the system clipboard could not be written (OSC
// 52 was still sent, but many terminals ignore it): the terminal's own
// selection is the way out.
const CopyHint = "클립보드에 못 넣었어요 — ⌥·fn·Shift 누른 채 드래그"

// Copy is the one way UI code copies text (CA 5.6 clipboard): the text is
// sanitized (textwidth.SanitizeInput: newlines kept, escapes and controls
// dropped) and capped at clipboard.MaxBytes, then sent both as OSC 52
// (tea.SetClipboard, which reaches the terminal across SSH) and to the system
// clipboard program (ctx.Clipboard, run inside the Cmd, off the Update
// goroutine). The toast says okText when one path is believed to work — the
// program succeeded, or the session is remote (SSH: OSC 52 is the path) — and
// CopyHint otherwise. Without a Writer (unit tests) it is OSC 52 only.
func Copy(ctx *Context, text, okText string) tea.Cmd {
	text = clipboard.Clamp(textwidth.SanitizeInput(text), clipboard.MaxBytes)
	var w clipboard.Writer
	if ctx != nil {
		w = ctx.Clipboard
	}
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		if w != nil {
			if err := w.Write(text); err != nil && !errors.Is(err, clipboard.ErrRemote) {
				return ToastMsg{Text: CopyHint, Kind: ToastInfo}
			}
		}
		return ToastMsg{Text: okText, Kind: ToastSuccess}
	})
}
