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

// statusHold is the account's one "solving" status: the instances that
// asked for it and the status from before the first of them. Two open
// problems (leet:today and a card's) share it, so the status from before
// comes back once, when the last of them lets go.
type statusHold struct {
	holders map[string]bool // instance ids
	prev    savedStatus
}

// startStatus shows "solving <id>" when auto_status is on (once per
// instance; only the first instance of the account saves my status and
// sends it).
func (in *instance) startStatus(ctx *ui.Context) tea.Cmd {
	if in.statusOn || in.q == nil || !toolConfig(ctx).AutoStatus || ctx.API == nil {
		return nil
	}
	in.statusOn = true
	prev := savedStatus{}
	if me := ctx.Me(); me != nil && me.Status != nil {
		st := me.Status
		prev = savedStatus{had: true, text: st.Text, expiresAt: st.ExpiresAt}
		if st.Emoji != nil {
			e := *st.Emoji
			prev.emoji = &e
		}
	}
	first := withMemo(ctx, func(m *memo) bool {
		if m.status.holders == nil {
			m.status.holders = map[string]bool{}
		}
		first := len(m.status.holders) == 0
		if first {
			m.status.prev = prev
		}
		m.status.holders[in.id] = true
		return first
	})
	if !first {
		return nil
	}
	req := api.SetStatusRequest{Emoji: api.Some(solvingEmoji), Text: ctx.T.Leetcode.SolvingStatus(in.q.FrontendID)}
	return ui.Do(ctx, statusSetTag, func(c context.Context, a *api.Client) (*api.UserStatus, error) {
		return a.SetStatus(c, req)
	})
}

// restoreStatus lets go of the status (once per instance); the last
// instance of the account puts the status from before back.
func (in *instance) restoreStatus(ctx *ui.Context) tea.Cmd {
	if !in.statusOn || ctx.API == nil {
		return nil
	}
	in.statusOn = false
	type release struct {
		prev savedStatus
		last bool
	}
	r := withMemo(ctx, func(m *memo) release {
		delete(m.status.holders, in.id)
		return release{prev: m.status.prev, last: len(m.status.holders) == 0}
	})
	if !r.last {
		return nil
	}
	prev := r.prev
	now := nowFunc(ctx)()
	if !prev.had || prev.expiresAt != nil && !prev.expiresAt.After(now) {
		applyMyStatus(ctx, nil)
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
	// The store has it now: the answer may land after the last instance is
	// gone (closing it is what restores), with nobody left to take it.
	applyMyStatus(ctx, &api.UserStatus{Emoji: prev.emoji, Text: prev.text, ExpiresAt: prev.expiresAt})
	return ui.Do(ctx, statusSetTag, func(c context.Context, a *api.Client) (*api.UserStatus, error) {
		return a.SetStatus(c, req)
	})
}

// applyMyStatus puts st (nil = none) in the store's copy of me (UI goroutine).
func applyMyStatus(ctx *ui.Context, st *api.UserStatus) {
	if ctx.Store == nil || ctx.Store.Me == nil {
		return
	}
	ctx.Store.Me.Status = st
	ctx.Store.ObserveUser(ctx.Store.Me.UserSummary)
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
