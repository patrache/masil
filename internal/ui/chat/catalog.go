package chat

import (
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// cat is the screen's catalog: ctx.T, or ko without a Context (unit tests).
func cat(ctx *ui.Context) *i18n.T {
	if ctx == nil || ctx.T == nil {
		return i18n.KoT()
	}
	return ctx.T
}

// orKo is t, or ko when t is nil (callers handed tOf(ctx)).
func orKo(t *i18n.T) *i18n.T {
	if t == nil {
		return i18n.KoT()
	}
	return t
}

// hz is the time/number formatter of the screen's language.
func hz(ctx *ui.Context) humanize.Formatter { return humanize.For(cat(ctx).Locale()) }
