package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// applyLocale switches the shown language (ui.SetLocaleMsg, P5 Task 2). The
// choice is resolved like [ui] language: "auto" (or an unknown value) is the
// OS language again, a closed language falls back through the open gate. A
// choice made in the running app wins over a start-time MASIL_LANG. The
// effective language goes to ctx.T, ctx.Lang, the API's Accept-Language, OS
// notifications and the keymap's help texts; then every live component and
// overlay gets ui.LocaleChangedMsg. Nothing is rebuilt (like a layout mode
// switch), so drafts, scroll, selection and open overlays stay. Saving the
// choice and the toast are the language UI's (P5 Task 24).
func (m *Model) applyLocale(choice string) tea.Cmd {
	in := m.detectOS()
	in.Env = ""
	in.Config = choice
	res := i18n.Resolve(in)
	from := m.ctx.Lang
	m.locale = res
	if res.Effective == from {
		return nil
	}
	m.ctx.T = i18n.For(res.Effective)
	m.ctx.Lang = res.Effective
	m.ctx.API.SetLanguage(string(res.Effective))
	m.notifier.SetLocale(res.Effective)
	m.ctx.Keys.Localize(m.ctx.T)
	return m.broadcast(ui.LocaleChangedMsg{From: from, To: res.Effective})
}

// Locale is the current language decision (the start one, or the last switch).
func (m *Model) Locale() i18n.Resolution {
	return m.locale
}
