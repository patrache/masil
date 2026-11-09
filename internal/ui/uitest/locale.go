package uitest

import "github.com/patrache/masil/internal/ui"

// SetLocale switches the running app's language like the language UI does:
// Send(ui.SetLocaleMsg{Choice: choice}) ("auto" or a locale code). Unlike
// WithLocale it goes through the open-language gate — open the language first
// with i18n.SetOpenForTest. A MASIL_LANG in the process env still wins.
func (h *Harness) SetLocale(choice string) {
	h.T.Helper()
	h.Send(ui.SetLocaleMsg{Choice: choice})
}
