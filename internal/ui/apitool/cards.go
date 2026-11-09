package apitool

import (
	"encoding/json"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Chat cards api.result and api.run (C3.11, P4 Task 20). The renderer
// draws card data (every string sanitized); a card it cannot read (another
// v, broken data) and every card while the tool's flag is off are drawn
// from the fallback body, like an older client. ⏎ on a card that points at
// a shared collection opens that request when I am in the card's room.

func init() {
	ui.RegisterCard(ui.CardAPIResult, ui.CardRenderer{Lines: resultCardLines, Open: openCard})
	ui.RegisterCard(ui.CardAPIRun, ui.CardRenderer{Lines: runCardLines, Open: openCard})
}

// cardV is the card schema version this client draws.
const cardV = 1

// resultData reads an api.result card (ok=false: draw the body).
func resultData(m *api.Message) (apitool.ResultData, bool) {
	var d apitool.ResultData
	if m == nil || m.Card == nil || m.Card.V != cardV || json.Unmarshal(m.Card.Data, &d) != nil || d.Method == "" {
		return d, false
	}
	return d, true
}

// runData reads an api.run card (ok=false: draw the body).
func runData(m *api.Message) (apitool.RunData, bool) {
	var d apitool.RunData
	if m == nil || m.Card == nil || m.Card.V != cardV || json.Unmarshal(m.Card.Data, &d) != nil || d.Collection == "" {
		return d, false
	}
	return d, true
}

// bodyLines is the fallback: the message body, wrapped.
func bodyLines(m *api.Message, width int) []string {
	if m == nil {
		return nil
	}
	return textwidth.Wrap(textwidth.Sanitize(m.Body), max(width, 1))
}

func cardCut(s string, w int) string { return textwidth.Truncate(s, max(w, 1), "…") }

// resultCardLines: "🧪 POST {{base_url}}/v1/payments", "201 · 142ms · 98B ·
// ✓4/4 · staging", the headers and body lines it carries, the open hint.
func resultCardLines(ctx *ui.Context, m *api.Message, width int) []string {
	d, ok := resultData(m)
	if !ok || !features.On(ui.ToolAPI) {
		return bodyLines(m, width)
	}
	a, th := ctx.T.Apitest, ctx.Theme
	out := []string{cardCut("🧪 "+bold(th, textwidth.Sanitize(d.Method))+" "+textwidth.Sanitize(d.URL), width)}
	meta := a.CardResultMeta(d.Status, int(d.TimeMS), humanSize(d.Size), d.Asserts.Passed, d.Asserts.Total)
	if d.Env != "" {
		meta += " · " + textwidth.Sanitize(d.Env)
	}
	out = append(out, cardCut(statusStyle(th, d.Status, meta), width))
	for _, h := range d.ReqHeaders {
		out = append(out, cardCut(dim(th, "  "+textwidth.Sanitize(h[0])+": "+textwidth.Sanitize(h[1])), width))
	}
	for _, l := range d.BodyHead {
		out = append(out, cardCut(dim(th, "  │ "+textwidth.Sanitize(l)), width))
	}
	if d.Ref.DocID != "" && canOpenCard(ctx, m) {
		out = append(out, cardCut(dim(th, a.CardOpenHint()), width))
	}
	return out
}

// runCardLines: "🧪 Payments 실행 · 6개", "✓2 ✕4 · 1500ms", the failures.
func runCardLines(ctx *ui.Context, m *api.Message, width int) []string {
	d, ok := runData(m)
	if !ok || !features.On(ui.ToolAPI) {
		return bodyLines(m, width)
	}
	a, th := ctx.T.Apitest, ctx.Theme
	out := []string{
		cardCut(a.CardRunTitle(textwidth.Sanitize(d.Collection), d.Requests), width),
		cardCut(a.CardRunMeta(d.Passed, d.Failed, int(d.TimeMS)), width),
	}
	for _, f := range d.Failures {
		result := strconv.Itoa(f.Status)
		if f.Status == 0 {
			result = a.RunFailedNet(textwidth.Sanitize(f.Reason))
		}
		out = append(out, cardCut(dim(th, "  ✕ "+textwidth.Sanitize(f.Method)+" "+textwidth.Sanitize(f.Name)+" · "+result), width))
	}
	return out
}

// cardDoc is the shared collection (and request) a card points at.
func cardDoc(m *api.Message) (doc, req string) {
	if m == nil || m.Card == nil {
		return "", ""
	}
	switch m.Card.Type {
	case ui.CardAPIResult:
		if d, ok := resultData(m); ok {
			return d.Ref.DocID, d.Ref.RequestID
		}
	case ui.CardAPIRun:
		if d, ok := runData(m); ok {
			return d.Ref.DocID, ""
		}
	}
	return "", ""
}

// canOpenCard: the flag is on and I am a member of the room the card was
// posted in (a DM or another room's card shows only).
func canOpenCard(ctx *ui.Context, m *api.Message) bool {
	if !features.On(ui.ToolAPI) || ctx.Store == nil || m == nil {
		return false
	}
	ch := ctx.Store.Channels[m.ChannelID]
	if ch == nil || ch.SpaceID == nil {
		return false
	}
	return ctx.Store.Spaces[*ch.SpaceID] != nil
}

// openCard opens the card's shared request in its instance; nil (enter does
// nothing more) for a member-less, personal or unreadable card.
func openCard(ctx *ui.Context, m *api.Message) tea.Cmd {
	doc, req := cardDoc(m)
	if doc == "" || !canOpenCard(ctx, m) {
		return nil
	}
	q := map[string]string{queryDoc: doc}
	if req != "" {
		q[queryReq] = req
	}
	return ui.OpenTool(ui.ToolAPI, ui.ToolAPI+":doc:"+doc, q)
}
