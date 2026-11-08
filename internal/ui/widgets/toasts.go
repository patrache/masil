package widgets

import (
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// toastIcon is the 1-cell icon and its style per kind:
// info ○ (acc4), success ● (Success), error ✕ (Error), alert ★ (Warning).
func toastIcon(t *theme.Theme, k ui.ToastKind) (string, lipgloss.Style) {
	switch k {
	case ui.ToastSuccess:
		return "●", t.S.Success
	case ui.ToastError:
		return "✕", t.S.Error
	case ui.ToastAlert:
		return "★", t.S.Warning
	default:
		return "○", t.S.Base.Foreground(t.P.Acc4)
	}
}

// Toasts renders toast boxes' inner lines (the app draws the border around them).
//
// Each toast is "<icon> <text>" with the sanitized text word-wrapped to width-2 and
// continuation lines indented by two blanks; every line is padded to exactly width
// cells with Base (width < 3 degrades to a truncated single line per toast).
func Toasts(t *theme.Theme, items []ui.ToastMsg, width int) []string {
	if width <= 0 || len(items) == 0 {
		return nil
	}
	var out []string
	for _, it := range items {
		icon, ist := toastIcon(t, it.Kind)
		text := textwidth.Sanitize(it.Text)
		if width < 3 {
			out = append(out, fitLine(t, ist.Render(icon)+t.S.Base.Render(" "+text), width))
			continue
		}
		for i, l := range textwidth.Wrap(text, width-2) {
			lead := pad(t, 2)
			if i == 0 {
				lead = ist.Render(icon) + pad(t, 1)
			}
			out = append(out, lead+t.S.Base.Render(l)+pad(t, width-2-textwidth.Width(l)))
		}
	}
	return out
}
