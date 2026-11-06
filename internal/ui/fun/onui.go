package fun

import (
	"context"
	"strconv"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
)

// A palette command run with arguments ("/status 🎧 듣는 중") opens no dialog,
// so no live ui/fun component receives its ui.Do result — and a tea.Cmd runs off
// the UI goroutine, where it must not touch the Store. overlayApply hands such a
// result back to the UI goroutine: the Cmd parks the Store update under a token
// and returns ui.OpenOverlayMsg{ID: overlayApply, Args: token}; the app calls
// the registered factory on the UI goroutine with the Context, the factory runs
// the parked update and returns nil, so nothing is opened (an unknown or
// already-used token is a no-op).
const overlayApply = "fun.apply"

var parked = struct {
	sync.Mutex
	next int
	fns  map[string]func(*ui.Context)
}{fns: map[string]func(*ui.Context){}}

func init() {
	ui.RegisterOverlay(overlayApply, func(ctx *ui.Context, token string) ui.Overlay {
		parked.Lock()
		fn := parked.fns[token]
		delete(parked.fns, token)
		parked.Unlock()
		if fn != nil && ctx != nil && ctx.Store != nil {
			fn(ctx)
		}
		return nil
	})
}

// onUI returns the msg that runs fn on the UI goroutine.
func onUI(fn func(*ui.Context)) tea.Msg {
	parked.Lock()
	parked.next++
	token := strconv.Itoa(parked.next)
	parked.fns[token] = fn
	parked.Unlock()
	return ui.OpenOverlayMsg{ID: overlayApply, Args: token}
}

// doApply runs call off the UI goroutine. On success apply(ctx, value) runs on
// the UI goroutine and the success toast shows; an error becomes ui.ErrorMsg
// (api.Describe toast; a 401 ends the session instead).
func doApply[T any](ctx *ui.Context, call func(context.Context, *api.Client) (T, error), apply func(*ui.Context, T), success string) tea.Cmd {
	var client *api.Client
	if ctx != nil {
		client = ctx.API
	}
	return func() tea.Msg {
		if client == nil {
			return ui.ErrorMsg{Err: &api.Error{Code: "network"}}
		}
		v, err := call(context.Background(), client)
		if err != nil {
			return ui.ErrorMsg{Err: err}
		}
		return tea.BatchMsg{
			func() tea.Msg { return onUI(func(c *ui.Context) { apply(c, v) }) },
			ui.Toast(success, ui.ToastSuccess),
		}
	}
}

// meTag is "<prefix><my user id>" (the ui.Do tag of a request about me).
func meTag(ctx *ui.Context, prefix string) string {
	if me := ctx.Me(); me != nil {
		return prefix + me.ID
	}
	return prefix + string(api.MeRef)
}
