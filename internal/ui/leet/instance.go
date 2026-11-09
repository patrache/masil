package leet

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// One leet instance: "leet:today" (today's problem) or "leet:<date>/<slug>"
// (a problem opened from a #daily card). It shows the first-run notice the
// first time and asks masil for my streaks (the hub remembers them).

// meTag is the ui.Do tag of GET /v1/leet/me.
const meTag = "leet.me"

type instance struct {
	id     string
	target string // "" = today, else "<date>/<slug>"
	size   ui.Size
}

var (
	_ ui.Component = (*instance)(nil)
	_ ui.Titled    = (*instance)(nil)
)

func newInstance(_ *ui.Context, r ui.Route) ui.Component {
	in := &instance{id: r.ID}
	if r.ID != todayInstance {
		in.target = r.Query[queryTarget]
		if in.target == "" {
			in.target = strings.TrimPrefix(r.ID, instancePrefix)
		}
	}
	return in
}

func (in *instance) Init(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	if !loadState(ctx).FirstRunDone {
		cmds = append(cmds, ui.OpenRegisteredOverlay(OverlayFirstRun, ""))
	}
	cmds = append(cmds, fetchMe(ctx))
	return tea.Batch(cmds...)
}

// fetchMe asks masil for my streaks, recent solves and #daily spaces.
func fetchMe(ctx *ui.Context) tea.Cmd {
	return ui.Do(ctx, meTag, func(c context.Context, a *api.Client) (*api.LeetMe, error) {
		if a == nil {
			return nil, nil
		}
		return a.LeetMe(c)
	})
}

func (in *instance) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ui.ResultMsg[*api.LeetMe]); ok && m.Tag == meTag && m.Err == nil {
		rememberMe(ctx, m.Value)
	}
	return nil
}

func (in *instance) Resize(_ *ui.Context, size ui.Size) { in.size = size }

func (in *instance) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Leetcode.Name(), ui.Badge{}
}

func (in *instance) View(ctx *ui.Context, _ bool) string {
	w, h := in.size.W, in.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th := themeOf(ctx)
	out := make([]string, h)
	for i := range out {
		out[i] = th.S.Base.Render(strings.Repeat(" ", w))
	}
	out[0] = fitStyled(th, th.S.Dim.Render(" "+hubIcon+" "+ctx.T.Leetcode.Name()), w)
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}
