package workspace

import (
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The owner watches what keyboard participants type (spec B8.4, threat
// model): while anyone holds the keyboard the status line shows who typed
// last and when, and the input band — their keys as they went in.

// inputBandCells is how much of the input band is kept (the latest cells).
const inputBandCells = 40

// appendInputBand adds typed bytes to the band: escape sequences and other
// controls dropped, Enter as ⏎, the rest sanitized; the last cells kept.
func appendInputBand(band string, data []byte) string {
	var sb strings.Builder
	for _, r := range ansi.Strip(string(data)) {
		switch {
		case r == '\r' || r == '\n':
			sb.WriteString("⏎ ")
		case unicode.IsPrint(r):
			sb.WriteRune(r)
		}
	}
	band += textwidth.Sanitize(sb.String())
	if textwidth.Width(band) > inputBandCells {
		band = textwidth.TruncateLeft(band, inputBandCells, "")
	}
	return band
}

// keyboardStatus is the status line while someone holds the keyboard.
func (s *Surface) keyboardStatus(ctx *ui.Context) (string, bool) {
	o := s.own
	if o == nil {
		return "", false
	}
	t := ctx.T.Workspace
	now := nowClock(ctx).Now()
	held := false
	var last share.Member
	for _, m := range o.host.Members() {
		if m.Level == wire.LevelKeyboard {
			held = true
		}
		if m.LastInput.After(last.LastInput) {
			last = m
		}
	}
	if !held {
		return "", false
	}
	var parts []string
	if !last.LastInput.IsZero() {
		parts = append(parts, t.LastInput(s.peer(ctx, last.Seat, last.UserID).handle, ago(ctx, max(now.Sub(last.LastInput), time.Duration(0)))))
	}
	if o.inputBand != "" {
		parts = append(parts, "⌨ "+o.inputBand)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " · "), true
}
