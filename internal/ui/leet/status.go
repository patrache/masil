package leet

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
)

// The status message (A10.4, [tools.leet] auto_status — off by default,
// A10.5): opening a problem remembers my status and shows "🧩 1606 푸는
// 중" to friends; an Accepted submit or closing the tool puts the old one
// back (or clears it when there was none). Off: no status request at all.

const (
	statusSetTag   = "leet.status:set"
	statusClearTag = "leet.status:clear"
	solvingEmoji   = "🧩"
)

// savedStatus is the status before the tool changed it.
type savedStatus struct {
	had       bool
	emoji     *string
	text      string
	expiresAt *time.Time
}

// startStatus shows "solving <id>" when auto_status is on (once per instance).
func (in *instance) startStatus(ctx *ui.Context) tea.Cmd {
	if in.statusOn || in.q == nil || !toolConfig(ctx).AutoStatus || ctx.API == nil {
		return nil
	}
	in.statusOn = true
	in.prevStatus = savedStatus{}
	if me := ctx.Me(); me != nil && me.Status != nil {
		st := me.Status
		in.prevStatus = savedStatus{had: true, text: st.Text, expiresAt: st.ExpiresAt}
		if st.Emoji != nil {
			e := *st.Emoji
			in.prevStatus.emoji = &e
		}
	}
	req := api.SetStatusRequest{Emoji: api.Some(solvingEmoji), Text: ctx.T.Leetcode.SolvingStatus(in.q.FrontendID)}
	return ui.Do(ctx, statusSetTag, func(c context.Context, a *api.Client) (*api.UserStatus, error) {
		return a.SetStatus(c, req)
	})
}

// restoreStatus puts the status from before back (once).
func (in *instance) restoreStatus(ctx *ui.Context) tea.Cmd {
	if !in.statusOn || ctx.API == nil {
		return nil
	}
	in.statusOn = false
	prev := in.prevStatus
	now := nowFunc(ctx)()
	if !prev.had || prev.expiresAt != nil && !prev.expiresAt.After(now) {
		return ui.Do(ctx, statusClearTag, func(c context.Context, a *api.Client) (struct{}, error) {
			return struct{}{}, a.ClearStatus(c)
		})
	}
	req := api.SetStatusRequest{Text: prev.text}
	if prev.emoji != nil {
		req.Emoji = api.Some(*prev.emoji)
	}
	if prev.expiresAt != nil {
		req.ExpiresInSec = api.Some(max(1, int(prev.expiresAt.Sub(now)/time.Second)))
	}
	return ui.Do(ctx, statusSetTag, func(c context.Context, a *api.Client) (*api.UserStatus, error) {
		return a.SetStatus(c, req)
	})
}

// statusResult keeps the store's copy of my status in step.
func statusResult(ctx *ui.Context, msg tea.Msg) {
	if ctx.Store == nil || ctx.Store.Me == nil {
		return
	}
	switch m := msg.(type) {
	case ui.ResultMsg[*api.UserStatus]:
		if m.Tag == statusSetTag && m.Err == nil && m.Value != nil {
			ctx.Store.Me.Status = m.Value
		}
	case ui.ResultMsg[struct{}]:
		if m.Tag == statusClearTag && m.Err == nil {
			ctx.Store.Me.Status = nil
		}
	}
}
