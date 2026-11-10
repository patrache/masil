package feed

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

// hz is the time/number formatter of the screen's language.
func hz(ctx *ui.Context) humanize.Formatter { return humanize.For(cat(ctx).Locale()) }
