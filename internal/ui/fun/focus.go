package fun

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// Focus mode limits (copy: the fun catalog area).
const (
	focusMinSec = 60    // api.md POST /v1/me/focus: duration_sec 60..43200
	focusMaxSec = 43200 // 12 hours
)

var (
	errFocusRange  = errors.New("focus duration out of range")
	errFocusFormat = errors.New("focus duration not understood")
)

func init() {
	palette.Register(palette.Command{
		ID: "focus.start", Label: func(t *i18n.T) string { return t.Fun.CmdFocusStart() }, Group: palette.GroupCommand,
		Keywords:   []string{"focus", "dnd"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Fun.KwFocusStart()) },
		Available:  func(ctx *ui.Context) bool { return ctx.Store != nil && ctx.Store.Focus == nil },
		Run:        runFocusStart,
	})
	palette.Register(palette.Command{
		ID: "focus.stop", Label: func(t *i18n.T) string { return t.Fun.CmdFocusStop() }, Group: palette.GroupCommand,
		Keywords:   []string{"focus", "stop", "off"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Fun.KwFocusStop()) },
		Available:  func(ctx *ui.Context) bool { return ctx.Store != nil && ctx.Store.Focus != nil },
		Run:        runFocusStop,
	})
}

// parseFocusArgs is the focus.start duration: "" => unset (the server uses
// settings.focus_default_min), a bare number => minutes, otherwise a Go
// duration ("25m", "1h30m"). The result is whole seconds in 60..43200
// (errFocusRange outside, errFocusFormat when unparsable).
func parseFocusArgs(args string) (api.Opt[int], error) {
	s := strings.TrimSpace(oneLine(args))
	if s == "" {
		return api.Opt[int]{}, nil
	}
	var d time.Duration
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > focusMaxSec/60 {
			return api.Opt[int]{}, errFocusRange
		}
		d = time.Duration(n) * time.Minute
	} else {
		pd, err := time.ParseDuration(s)
		if err != nil {
			return api.Opt[int]{}, errFocusFormat
		}
		d = pd
	}
	if d < focusMinSec*time.Second || d > focusMaxSec*time.Second {
		return api.Opt[int]{}, errFocusRange
	}
	return api.Some(int(d / time.Second)), nil
}

// runFocusStart is focus.start: parse the duration, POST, store the session and
// toast how long alerts are held.
func runFocusStart(ctx *ui.Context, args string) tea.Cmd {
	sec, err := parseFocusArgs(args)
	c, f := cat(ctx), hz(ctx)
	switch {
	case errors.Is(err, errFocusFormat):
		return ui.Toast(c.Fun.FocusErrFormat(), ui.ToastError)
	case err != nil:
		return ui.Toast(c.Fun.FocusErrRange(), ui.ToastError)
	}
	client := clientOf(ctx)
	return func() tea.Msg {
		if client == nil {
			return ui.ErrorMsg{Err: &api.Error{Code: "network"}}
		}
		fs, err := client.StartFocus(context.Background(), sec)
		if err != nil {
			return ui.ErrorMsg{Err: err}
		}
		if fs == nil {
			return ui.ErrorMsg{Err: &api.Error{Code: "network"}}
		}
		text := c.Fun.FocusStarted(f.Duration(fs.EndsAt.Sub(fs.StartedAt).Round(time.Second)))
		return tea.BatchMsg{
			func() tea.Msg {
				return onUI(func(c *ui.Context) {
					if c.Store != nil {
						c.Store.Focus = fs
					}
				})
			},
			ui.Toast(text, ui.ToastSuccess),
		}
	}
}

// runFocusStop is focus.stop: DELETE, then Store.FocusEnded only — the summary
// overlay opens solely for alert{kind:"focus_summary"} (CA 9.1). A 409 (not
// focusing) is an info toast.
func runFocusStop(ctx *ui.Context, _ string) tea.Cmd {
	client, notFocusing := clientOf(ctx), cat(ctx).Fun.FocusNotFocusing()
	return func() tea.Msg {
		if client == nil {
			return ui.ErrorMsg{Err: &api.Error{Code: "network"}}
		}
		sum, err := client.EndFocus(context.Background())
		if err != nil {
			var ae *api.Error
			if errors.As(err, &ae) && ae.Code == "conflict" { // not focusing: Store.Focus was stale
				return tea.BatchMsg{
					func() tea.Msg {
						return onUI(func(c *ui.Context) {
							if c.Store != nil {
								c.Store.Focus = nil
							}
						})
					},
					ui.Toast(notFocusing, ui.ToastInfo),
				}
			}
			return ui.ErrorMsg{Err: err}
		}
		if sum == nil {
			return nil
		}
		return onUI(func(c *ui.Context) {
			if c.Store != nil {
				c.Store.FocusEnded(*sum)
			}
		})
	}
}

// clientOf is ctx.API (nil without a context).
func clientOf(ctx *ui.Context) *api.Client {
	if ctx == nil {
		return nil
	}
	return ctx.API
}
