package leet

import (
	"context"
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/ui"
)

// Remote run and submit (A10.4): the saved solution file goes to LeetCode
// (never what is only in an editor buffer), paced 10s (1s premium) per
// account; the result is polled once a second with ui.After, at most 60
// times. A missing or expired sign-in opens the login dialog and the run
// goes on after it; a block page or a rate limit is shown, never retried on
// its own.

// judgeKind is run (examples) or submit.
type judgeKind int

const (
	kindNone judgeKind = iota
	kindRun
	kindSubmit
)

// maxChecks is how many times a result is polled (once a second).
const maxChecks = 60

// judgePhase is where the last run or submit is.
type judgePhase int

const (
	jSending judgePhase = iota
	jChecking
	jDone
	jFailed
)

// judgeState is the instance's last run or submit.
type judgeState struct {
	kind     judgeKind
	phase    judgePhase
	seq      int
	id       string // run id / submission id
	tries    int
	res      *leetcode.CheckResult
	err      error
	timedOut bool
	toggled  bool // v: an error's full text folded / unfolded (compact starts folded)

	// what was sent (the share dialog uses it)
	q    *leetcode.Question
	date string
	lang leetcode.Lang
	code string
}

// busy: a request or its polling is under way.
func (j *judgeState) busy() bool { return j != nil && (j.phase == jSending || j.phase == jChecking) }

// acceptedMsg is broadcast when a submit is Accepted (Task 22 records it).
type acceptedMsg struct {
	id   string // the instance
	date string
	q    *leetcode.Question
	lang leetcode.Lang
	code string
	res  *leetcode.CheckResult
	sub  string // the submission id
}

// judge msgs (seq = the judge attempt of instance id).
type (
	credsLoadedMsg struct {
		id    string
		kind  judgeKind
		creds *leetcode.Credentials
		err   error
	}
	judgeSentMsg struct {
		id      string
		seq     int
		judgeID string
		err     error
	}
	checkTickMsg struct {
		id  string
		seq int
	}
	checkedMsg struct {
		id  string
		seq int
		res *leetcode.CheckResult
		err error
	}
)

// startJudge is r / s: the cookies first (memory, else the store, else the
// login dialog), then the pacing, then the saved file.
func (in *instance) startJudge(ctx *ui.Context, kind judgeKind) tea.Cmd {
	if !in.canEdit() || in.judge.busy() {
		return nil
	}
	if c := knownCreds(ctx); c != nil {
		return in.send(ctx, kind, c)
	}
	ns, ok := credNS(ctx)
	store, id := ctx.Secrets, in.id
	return func() tea.Msg {
		if !ok {
			c, found := leetcode.CredentialsFromEnv(envCookie)
			if !found {
				return credsLoadedMsg{id: id, kind: kind, err: secrets.ErrNotFound}
			}
			return credsLoadedMsg{id: id, kind: kind, creds: c}
		}
		c, err := leetcode.LoadCredentials(context.Background(), store, ns, envCookie)
		return credsLoadedMsg{id: id, kind: kind, creds: c, err: err}
	}
}

// credsLoaded continues a judge whose cookies were read from the store.
func (in *instance) credsLoaded(ctx *ui.Context, m credsLoadedMsg) tea.Cmd {
	if m.err != nil || m.creds == nil {
		in.pending = m.kind
		return ui.OpenRegisteredOverlay(OverlayLogin, "")
	}
	rememberCreds(ctx, m.creds, lcAccountOf(ctx).Premium)
	return in.send(ctx, m.kind, m.creds)
}

// send posts the saved file unless the pacer says wait.
func (in *instance) send(ctx *ui.Context, kind judgeKind, c *leetcode.Credentials) tea.Cmd {
	now := nowFunc(ctx)()
	p := pacerOf(ctx)
	if rem := waitBeforeSend(ctx, now); rem > 0 {
		in.waitUntil = now.Add(rem)
		return nil
	}
	path, err := in.solutionFile(ctx)
	if err != nil {
		return ui.Toast(ctx.T.Leetcode.FileFailed(), ui.ToastError)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ui.Toast(ctx.T.Leetcode.FileFailed(), ui.ToastError)
	}
	p.Mark(now)
	in.waitUntil = time.Time{}
	in.judgeSeq++
	lang := currentLang(ctx)
	if splitFor(ctx.Mode, in.zoomed, in.size.W).Tabbed {
		in.tab = tabResult // the widget shows where the answer comes
	}
	j := &judgeState{kind: kind, phase: jSending, seq: in.judgeSeq, q: in.q, date: in.date, lang: lang, code: string(b)}
	in.judge = j
	client, q, code, id, seq := newClient(c), in.q, j.code, in.id, j.seq
	return func() tea.Msg {
		var jid string
		var err error
		if kind == kindSubmit {
			jid, err = client.Submit(context.Background(), q, lang.Slug, code)
		} else {
			jid, err = client.Interpret(context.Background(), q, lang.Slug, code)
		}
		return judgeSentMsg{id: id, seq: seq, judgeID: jid, err: err}
	}
}

// sent starts polling.
func (in *instance) sent(ctx *ui.Context, m judgeSentMsg) tea.Cmd {
	j := in.judge
	if j == nil || m.seq != j.seq || j.phase != jSending {
		return nil
	}
	if m.err != nil {
		return in.judgeErr(ctx, m.err)
	}
	j.phase, j.id = jChecking, m.judgeID
	return ui.After(ctx, time.Second, checkTickMsg{id: in.id, seq: j.seq})
}

// tick asks for the result once.
func (in *instance) tick(ctx *ui.Context, m checkTickMsg) tea.Cmd {
	j := in.judge
	if j == nil || m.seq != j.seq || j.phase != jChecking {
		return nil
	}
	c := knownCreds(ctx)
	if c == nil {
		return in.judgeErr(ctx, leetcode.ErrNotSignedIn)
	}
	client, jid, id, seq := newClient(c), j.id, in.id, j.seq
	return func() tea.Msg {
		res, err := client.Check(context.Background(), jid)
		return checkedMsg{id: id, seq: seq, res: res, err: err}
	}
}

// checked takes one poll's answer.
func (in *instance) checked(ctx *ui.Context, m checkedMsg) tea.Cmd {
	j := in.judge
	if j == nil || m.seq != j.seq || j.phase != jChecking {
		return nil
	}
	if m.err != nil {
		return in.judgeErr(ctx, m.err)
	}
	j.tries++
	if m.res == nil || !m.res.Done {
		if j.tries >= maxChecks {
			j.phase, j.timedOut = jFailed, true
			return nil
		}
		return ui.After(ctx, time.Second, checkTickMsg{id: in.id, seq: j.seq})
	}
	j.phase, j.res = jDone, m.res
	if j.kind == kindSubmit && m.res.StatusMsg == "Accepted" {
		return emit(acceptedMsg{id: in.id, date: j.date, q: j.q, lang: j.lang, code: j.code, res: m.res, sub: j.id})
	}
	return nil
}

// judgeErr maps a LeetCode error (A10.4): sign-in → the login dialog again
// (the same action goes on after it); a block page or anything else is
// shown; a rate limit holds the account's runs and submits 10 seconds.
func (in *instance) judgeErr(ctx *ui.Context, err error) tea.Cmd {
	j := in.judge
	switch {
	case errors.Is(err, leetcode.ErrAuthExpired), errors.Is(err, leetcode.ErrNotSignedIn):
		forgetCreds(ctx)
		if j != nil {
			in.pending = j.kind
			in.judge = nil
		}
		return ui.OpenRegisteredOverlay(OverlayLogin, "")
	case errors.Is(err, leetcode.ErrRateLimited): // 10 seconds, premium or not
		now := nowFunc(ctx)()
		pacerOf(ctx).Mark(now)
		holdUntil(ctx, now.Add(leetcode.FreeInterval))
		in.waitUntil = now.Add(leetcode.FreeInterval)
	}
	if j != nil {
		j.phase, j.err = jFailed, err
	}
	return nil
}

// loginDone goes on with the action that asked for the login.
func (in *instance) loginDone(ctx *ui.Context) tea.Cmd {
	k := in.pending
	in.pending = kindNone
	if k == kindNone {
		return nil
	}
	return in.startJudge(ctx, k)
}

// waitLeft is the seconds still to wait before sending (0 = none).
func (in *instance) waitLeft(ctx *ui.Context) int {
	if in.waitUntil.IsZero() {
		return 0
	}
	d := in.waitUntil.Sub(nowFunc(ctx)())
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}
