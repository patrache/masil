package apitool

import (
	"context"
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Sharing a result as a chat card (flow ⑤, C3.11, P4 Task 20): s on a
// response (or on a finished run) builds the card data, shows it drawn by
// the registered renderer — exactly the bytes that will be posted — and
// sends it to the picked channel. Options (host, headers, body) are off by
// default (JC20); toggling one rebuilds the data and the preview in place.

// dkShare is the share preview dialog.
const dkShare dialogKind = "share"

// sharePreviewW is the preview's card width.
const sharePreviewW = 58

// sharePlan is a card waiting in the preview.
type sharePlan struct {
	typ   string // ui.CardAPIResult or ui.CardAPIRun
	st    *respState
	run   *apitool.RunSummary
	doc   string
	opts  apitool.CardOptions
	data  json.RawMessage // what will be posted, byte for byte
	body  string          // the fallback body
	chans []string        // the channel ids offered, in choice order
}

// cardRef points a result card at the edited collection's request.
func (t *Tool) cardRef(reqID string) apitool.CardRef {
	if sd := t.curShared(); sd != nil {
		return apitool.CardRef{Collection: sd.title, RequestID: reqID, DocID: sd.id}
	}
	return apitool.CardRef{Collection: t.colName, RequestID: reqID}
}

// shareChannels are the channels a card may go to: each room's text
// channels (rooms in my order), then the DMs.
func shareChannels(ctx *ui.Context) []string {
	if ctx.Store == nil {
		return nil
	}
	var out []string
	for _, sp := range ctx.Store.SpaceOrder {
		for _, ch := range ctx.Store.SpaceChannels(sp) {
			out = append(out, ch.ID)
		}
	}
	for _, id := range ctx.Store.DMOrder {
		if ch := ctx.Store.Channels[id]; ch != nil {
			out = append(out, id)
		}
	}
	return out
}

// channelChoice is "노바 개발방 #일반" for a room channel, the DM's name else.
func channelChoice(ctx *ui.Context, id string) string {
	label := ctx.Store.ChannelLabel(id)
	if ch := ctx.Store.Channels[id]; ch != nil && ch.SpaceID != nil {
		if sp := ctx.Store.Spaces[*ch.SpaceID]; sp != nil {
			label = sp.Name + " " + label
		}
	}
	return textwidth.Sanitize(label)
}

// openShareResult previews the shown response as an api.result card.
func (t *Tool) openShareResult(ctx *ui.Context) tea.Cmd {
	st := t.rv.current()
	if st == nil || st.resp == nil || st.prep == nil || ctx.API == nil {
		return nil
	}
	return t.openShare(ctx, &sharePlan{typ: ui.CardAPIResult, st: st})
}

// openShareRun previews a finished run as an api.run card.
func (t *Tool) openShareRun(ctx *ui.Context, rs *runState) tea.Cmd {
	if rs.sum == nil || ctx.API == nil {
		return nil
	}
	return t.openShare(ctx, &sharePlan{typ: ui.CardAPIRun, run: rs.sum, doc: rs.doc})
}

func (t *Tool) openShare(ctx *ui.Context, p *sharePlan) tea.Cmd {
	a := ctx.T.Apitest
	p.chans = shareChannels(ctx)
	if len(p.chans) == 0 {
		return ui.Toast(a.ShareNoChannel(), ui.ToastInfo)
	}
	if err := t.buildShare(ctx, p); err != nil {
		return ui.Toast(a.ShareFailed(describeErr(ctx, err)), ui.ToastError)
	}
	t.share = p
	return ui.OpenOverlay(t.shareDialog(ctx, p, 0))
}

// buildShare makes the card data and the fallback body for p's options.
func (t *Tool) buildShare(ctx *ui.Context, p *sharePlan) error {
	a := ctx.T.Apitest
	var err error
	switch p.typ {
	case ui.CardAPIResult:
		st := p.st
		p.data, err = apitool.ResultCard(st.req, st.prep, st.resp, st.checks, st.env, st.ref, p.opts)
		passed := st.passed()
		p.body = a.CardResultBody(st.method, apitool.ResultPath(st.prep), st.resp.Status, ms(st.resp.Timing.Total), passed, len(st.checks))
	case ui.CardAPIRun:
		s := p.run
		p.data, err = apitool.RunCard(*s, p.doc)
		p.body = a.CardRunBody(s.Collection, s.Requests, s.Passed, s.Failed, ms(s.Total))
	}
	return err
}

// shareCard is p as the message the card renderer draws.
func shareCard(ctx *ui.Context, p *sharePlan) *api.Message {
	m := &api.Message{Kind: api.MessageKindCard, Body: p.body, Card: &api.Card{Type: p.typ, V: cardV, Data: p.data}}
	if me := ctx.Me(); me != nil {
		m.Author = me.UserSummary
	}
	return m
}

// shareDialog is the preview: the card drawn by its renderer, then the
// channels, the options (result cards) and cancel.
func (t *Tool) shareDialog(ctx *ui.Context, p *sharePlan, sel int) *dialog {
	a := ctx.T.Apitest
	var pre []string
	if r, ok := ui.CardFor(p.typ); ok {
		pre = r.Lines(ctx, shareCard(ctx, p), sharePreviewW)
	}
	d := &dialog{inst: t.route.ID, kind: dkShare, title: a.ShareTitle(), prefW: sharePreviewW + 6,
		pre: pre, body: []string{a.SharePick()}, sel: sel}
	for _, id := range p.chans {
		d.choices = append(d.choices, channelChoice(ctx, id))
	}
	opts := len(d.choices)
	if p.typ == ui.CardAPIResult {
		onOff := func(on bool) string {
			if on {
				return a.StateOn()
			}
			return a.StateOff()
		}
		d.choices = append(d.choices, a.ShareOptHost(onOff(p.opts.ShowHost)), a.ShareOptHeaders(onOff(p.opts.IncludeHeaders)), a.ShareOptBody(onOff(p.opts.IncludeBody)))
	}
	d.choices = append(d.choices, a.Cancel())
	d.chain = func(choice int) *dialog {
		if p.typ != ui.CardAPIResult || choice < opts || choice >= opts+3 {
			return nil
		}
		switch choice - opts {
		case 0:
			p.opts.ShowHost = !p.opts.ShowHost
		case 1:
			p.opts.IncludeHeaders = !p.opts.IncludeHeaders
		case 2:
			p.opts.IncludeBody = !p.opts.IncludeBody
		}
		if err := t.buildShare(ctx, p); err != nil {
			return nil
		}
		return t.shareDialog(ctx, p, choice)
	}
	return d
}

// shareAnswer sends the previewed card to the picked channel.
func (t *Tool) shareAnswer(ctx *ui.Context, m dialogMsg) tea.Cmd {
	p := t.share
	if p == nil || m.Choice < 0 || m.Choice >= len(p.chans) {
		t.share = nil
		return nil
	}
	ch := p.chans[m.Choice]
	label := channelChoice(ctx, ch)
	req := api.SendMessageRequest{Kind: api.MessageKindCard, Body: p.body, Card: &api.Card{Type: p.typ, V: cardV, Data: p.data}}
	cl := ctx.API
	_, cmd := t.startJob(ctx, jobCard, func(c context.Context) func(*ui.Context) tea.Cmd {
		_, _, err := cl.SendMessage(c, ch, req)
		return func(ctx *ui.Context) tea.Cmd {
			if t.share == p {
				t.share = nil
			}
			if err != nil {
				return ui.Toast(ctx.T.Apitest.ShareFailed(describeErr(ctx, err)), ui.ToastError)
			}
			return ui.Toast(ctx.T.Apitest.ShareSent(label), ui.ToastSuccess)
		}
	})
	return cmd
}
