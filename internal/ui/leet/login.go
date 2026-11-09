package leet

import (
	"context"
	"errors"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/secretsui"
	"github.com/patrache/masil/internal/ui/settings"
)

// The cookie login (A10.4): LEETCODE_SESSION and csrftoken (required) and
// cf_clearance (optional) pasted from the browser. They are checked with
// LeetCode's userStatus, then kept in ctx.Secrets only (the OS store, or
// after a consent the file / this run) — never shown back except as
// "••••abcd", never sent to masil. Only the LeetCode user name (not a
// secret) goes to tools-state.json.

// OverlayLogin is the login dialog (args unused).
const OverlayLogin = "leet.login"

const loginW, loginH = 66, 20

// loginDoneMsg: a login was saved (instances waiting on it go on).
type loginDoneMsg struct{}

// cookie field order.
var cookieNames = [3]string{leetcode.NameSession, leetcode.NameCSRF, leetcode.NameCFClearance}

type loginDlg struct {
	vals [3][]rune
	sel  int
	size ui.Size

	busy    bool
	seq     int
	errText string
	creds   *leetcode.Credentials // checked, being saved
	status  *leetcode.UserStatus
}

var (
	_ ui.Overlay    = (*loginDlg)(nil)
	_ ui.KeyHandler = (*loginDlg)(nil)
	_ ui.Titled     = (*loginDlg)(nil)
)

func init() {
	ui.RegisterOverlay(OverlayLogin, func(*ui.Context, string) ui.Overlay { return &loginDlg{} })
	settings.RegisterToolLogin(toolID, describeLogin, forgetLogin)
}

// describeLogin is the settings row's account: the LeetCode user name this
// device signed in as (cheap: no store access).
func describeLogin(ctx *ui.Context) (string, bool) {
	u := loadState(ctx).Username
	return textwidth.Sanitize(u), u != ""
}

// forgetLogin deletes this account's cookies and the user name.
func forgetLogin(ctx *ui.Context) tea.Cmd {
	st := loadState(ctx)
	st.Username, st.Premium = "", false
	saveState(ctx, st)
	forgetCreds(ctx)
	ns, ok := credNS(ctx)
	store := ctx.Secrets
	if !ok || store == nil {
		return nil
	}
	return func() tea.Msg {
		if err := leetcode.DeleteCredentials(context.Background(), store, ns); err != nil {
			return ui.ErrorMsg{Err: err}
		}
		return nil
	}
}

// credNS is this masil account's cookie namespace.
func credNS(ctx *ui.Context) (leetcode.CredNS, bool) {
	server, user, ok := ctx.SecretScope()
	return leetcode.CredNS{ServerHost: server, UserID: user}, ok
}

func (d *loginDlg) Init(*ui.Context) tea.Cmd        { return nil }
func (d *loginDlg) Resize(_ *ui.Context, s ui.Size) { d.size = s }
func (d *loginDlg) Modal() bool                     { return true }
func (d *loginDlg) Bounds(body layout.Rect) layout.Rect {
	return centered(body, loginW, loginH)
}
func (d *loginDlg) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Leetcode.LoginTitle(), ui.Badge{}
}

// login dialog msgs (seq = the dialog's attempt).
type (
	loginCheckedMsg struct {
		seq    int
		status *leetcode.UserStatus
		err    error
	}
	loginSavedMsg struct {
		seq int
		err error
	}
)

func (d *loginDlg) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if !d.busy {
			d.vals[d.sel] = []rune(leetcode.NormalizeCookie(cookieNames[d.sel], m.Content))
			d.errText = ""
		}
	case loginCheckedMsg:
		if m.seq != d.seq || !d.busy {
			return nil
		}
		switch {
		case errors.Is(m.err, leetcode.ErrBlocked):
			return d.fail(ctx.T.Leetcode.Blocked())
		case m.err != nil:
			return d.fail(ctx.T.Leetcode.LoginFailed())
		case m.status == nil || !m.status.SignedIn:
			return d.fail(ctx.T.Leetcode.LoginRejected())
		}
		d.status = m.status
		return d.save(ctx)
	case loginSavedMsg:
		if m.seq != d.seq || !d.busy {
			return nil
		}
		if errors.Is(m.err, secrets.ErrNeedConsent) {
			return ui.OpenRegisteredOverlay(secretsui.OverlayConsent, toolID)
		}
		if m.err != nil {
			return d.fail(ctx.T.Leetcode.LoginSaveFailed())
		}
		return d.saved(ctx)
	case secretsui.ConsentMsg:
		if m.Tool != toolID || !d.busy || d.creds == nil {
			return nil
		}
		if m.Choice == secrets.FallbackUnset {
			return d.fail(ctx.T.Leetcode.LoginSaveFailed())
		}
		return d.save(ctx)
	}
	return nil
}

// fail shows why and lets the user edit again.
func (d *loginDlg) fail(text string) tea.Cmd {
	d.busy, d.errText, d.creds, d.status = false, text, nil, nil
	return nil
}

// save stores the checked cookies (a Cmd: the OS store may be slow).
func (d *loginDlg) save(ctx *ui.Context) tea.Cmd {
	ns, ok := credNS(ctx)
	store, c, seq := ctx.Secrets, *d.creds, d.seq
	if !ok || store == nil {
		return d.fail(ctx.T.Leetcode.LoginSaveFailed())
	}
	return func() tea.Msg {
		return loginSavedMsg{seq: seq, err: leetcode.SaveCredentials(context.Background(), store, ns, c)}
	}
}

// saved: remembered for this run, the user name kept, the dialog closed.
func (d *loginDlg) saved(ctx *ui.Context) tea.Cmd {
	rememberCreds(ctx, d.creds, d.status.Premium)
	st := loadState(ctx)
	st.Username, st.Premium = d.status.Username, d.status.Premium
	saveState(ctx, st)
	user := textwidth.Sanitize(d.status.Username)
	text := ctx.T.Leetcode.SignedIn(user)
	if d.status.Premium {
		text = ctx.T.Leetcode.SignedInPremium(user)
	}
	d.wipe()
	return tea.Batch(closeOverlay, ui.Toast(text, ui.ToastInfo), emit(loginDoneMsg{}))
}

// wipe clears the typed values.
func (d *loginDlg) wipe() {
	for i := range d.vals {
		clear(d.vals[i])
		d.vals[i] = nil
	}
	d.creds, d.status = nil, nil
}

func (d *loginDlg) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	switch k {
	case "ctrl+c":
		return false, nil
	case "esc":
		d.busy = false
		d.seq++
		d.wipe()
		return true, closeOverlay
	}
	if d.busy {
		return true, nil
	}
	switch k {
	case "tab", "down":
		d.sel = (d.sel + 1) % len(d.vals)
	case "shift+tab", "up":
		d.sel = (d.sel + len(d.vals) - 1) % len(d.vals)
	case "backspace", "ctrl+h":
		if n := len(d.vals[d.sel]); n > 0 {
			d.vals[d.sel][n-1] = 0
			d.vals[d.sel] = d.vals[d.sel][:n-1]
		}
	case "ctrl+u":
		clear(d.vals[d.sel])
		d.vals[d.sel] = nil
	case "enter":
		return true, d.confirm(ctx)
	default:
		for _, r := range msg.Text {
			if !unicode.IsControl(r) && len(d.vals[d.sel]) < 8192 {
				d.vals[d.sel] = append(d.vals[d.sel], r)
			}
		}
	}
	d.errText = ""
	return true, nil
}

// value is field i as a cookie value.
func (d *loginDlg) value(i int) string {
	return leetcode.NormalizeCookie(cookieNames[i], string(d.vals[i]))
}

// confirm asks LeetCode who these cookies are.
func (d *loginDlg) confirm(ctx *ui.Context) tea.Cmd {
	c := leetcode.Credentials{Session: secrets.New(d.value(0)), CSRF: secrets.New(d.value(1)), CFClearance: secrets.New(d.value(2))}
	if c.Session.IsZero() || c.CSRF.IsZero() {
		d.errText = ctx.T.Leetcode.LoginMissing()
		return nil
	}
	d.busy, d.errText, d.creds = true, "", &c
	d.seq++
	seq, client := d.seq, newClient(&c)
	return func() tea.Msg {
		st, err := client.UserStatus(context.Background())
		return loginCheckedMsg{seq: seq, status: st, err: err}
	}
}

func (d *loginDlg) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	cw := contentWidth(w)
	var head []string
	for _, l := range textwidth.Wrap(t.Leetcode.CookieWarning(), cw) {
		head = append(head, th.S.Warning.Render(l))
	}
	keep := len(head)
	head = append(head, "")
	for _, s := range []string{t.Leetcode.LoginStep1(), t.Leetcode.LoginStep2(), t.Leetcode.LoginStep3()} {
		for _, l := range textwidth.Wrap(s, cw) {
			head = append(head, th.S.Base.Render(l))
		}
	}
	head = append(head, "")
	nameW := 0
	for _, n := range cookieNames {
		nameW = max(nameW, textwidth.Width(n))
	}
	nameW = min(nameW, max(4, cw/2))
	var mid []string
	for i, n := range cookieNames {
		var val string
		st := th.S.Base
		switch v := d.value(i); {
		case v != "":
			val = leetcode.Mask(secrets.New(v))
		case i == 2:
			val, st = t.Leetcode.LoginOptional(), th.S.Dim
		default:
			val, st = t.Leetcode.LoginRequired(), th.S.Dim
		}
		mark, nameSt := "  ", th.S.Base
		if i == d.sel {
			mark, nameSt = "› ", th.S.Selected
		}
		label := textwidth.PadRight(textwidth.Truncate(n, nameW, "…"), nameW)
		room := max(1, cw-2-nameW-2)
		mid = append(mid, nameSt.Render(mark+label+"  ")+st.Render(textwidth.Truncate(val, room, "…")))
	}
	status := ""
	switch {
	case d.busy:
		status = th.S.Dim.Render(textwidth.Truncate(t.Leetcode.LoginChecking(), cw, "…"))
	case d.errText != "":
		status = th.S.Error.Render(textwidth.Truncate(textwidth.Sanitize(d.errText), cw, "…"))
	}
	foot := []string{"", status, th.S.Dim.Render(textwidth.Truncate(t.Leetcode.LoginFooter(), cw, "…"))}
	rows, _ := stack(head, mid, foot, d.sel, h, keep)
	return renderRows(th, rows, d.size)
}
