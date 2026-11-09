package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/version"
)

// runTUI starts the full-screen app; tests replace it.
var runTUI = app.Run

// Core command usage lines (Korean, one line each; shown by `masil help`).
const (
	usageTUI     = "masil [tui] — 터미널 마실을 열어요"
	usageLogin   = "masil login [--handle <아이디> --password-stdin] — 로그인해요"
	usageSignup  = "masil signup — 가입해요"
	usageLogout  = "masil logout — 이 기기에서 로그아웃해요"
	usageJoin    = "masil join <초대 코드> — 초대받은 방에 들어가요"
	usageVersion = "masil version — 버전을 보여 줘요"
	usageConfig  = "masil config path — 설정 폴더 위치를 보여 줘요"
	usageHelp    = "masil help — 도움말"
)

// coreOrder is the fixed `masil help` order of the core commands; commands of
// other plans follow, sorted by name.
var coreOrder = []string{"tui", "login", "signup", "logout", "join", "version", "config", "help"}

const (
	loginTimeout  = 20 * time.Second // one login request
	logoutTimeout = 10 * time.Second // the best-effort server logout
	maxPassword   = 64 << 10         // stdin bytes read for --password-stdin
	deviceNameMax = 64               // api.md 5.1: device_name 0..64
	clientName    = "tui"            // sessions made by `masil login` are TUI sessions
)

func init() {
	register(command{Name: "tui", Usage: usageTUI, Run: cmdTUI})
	register(command{Name: "login", Usage: usageLogin, Run: cmdLogin})
	register(command{Name: "signup", Usage: usageSignup, Run: cmdSignup})
	register(command{Name: "logout", Usage: usageLogout, Run: cmdLogout})
	register(command{Name: "join", Usage: usageJoin, Run: cmdJoin})
	register(command{Name: "version", Usage: usageVersion, Run: cmdVersion})
	register(command{Name: "config", Usage: usageConfig, Run: cmdConfig})
	register(command{Name: "help", Usage: usageHelp, Run: cmdHelp})
}

// parseArgs parses a core command's flags with ContinueOnError. done=true means
// the command must return err right away: -h/--help printed the usage on stdout
// (err nil), or a parse error printed the usage on stderr (err is a usage error).
func parseArgs(env *Env, name, usage string, args []string, setup func(fs *flag.FlagSet)) (pos []string, done bool, err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // flag's own (English) messages are replaced below
	if setup != nil {
		setup(fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(env.Stdout, "사용법: %s\n", usage)
			return nil, true, nil
		}
		return nil, true, usageError(env, usage, "인자가 잘못됐어요: "+err.Error(), err)
	}
	return fs.Args(), false, nil
}

// usageError writes an optional reason and the usage line to stderr and returns
// a usage error (exit code 2) wrapping cause.
// errText is err as one line safe for the terminal: an error can wrap text
// from the server, a file name or a peer, so it is sanitized before printing
// (pending-fixes 0-b).
func errText(err error) string {
	if err == nil {
		return ""
	}
	return textwidth.Sanitize(err.Error())
}

func usageError(env *Env, usage, reason string, cause error) error {
	if reason != "" {
		fmt.Fprintln(env.Stderr, textwidth.Sanitize(reason))
	}
	fmt.Fprintf(env.Stderr, "사용법: %s\n", usage)
	return usageErr(cause)
}

// noArgs is parseArgs for a command that takes no flags and no positional args.
func noArgs(env *Env, name, usage string, args []string) (done bool, err error) {
	pos, done, err := parseArgs(env, name, usage, args, nil)
	if done {
		return true, err
	}
	if len(pos) > 0 {
		return true, usageError(env, usage, "필요 없는 인자가 있어요: "+strings.Join(pos, " "), nil)
	}
	return false, nil
}

// tuiOptions builds the app options with the saved credentials, used only when
// they were issued by the configured server (a token is never sent elsewhere).
func tuiOptions(env *Env) app.Options {
	o := app.Options{Paths: env.Paths, Config: env.Config}
	cr, err := auth.Load(env.Paths)
	switch {
	case err != nil:
		if !errors.Is(err, auth.ErrNoCredentials) || errors.Unwrap(err) != nil {
			slog.Warn("credentials not loaded", "err", err)
		}
	case !cr.Matches(env.Config.ServerURL()):
		slog.Info("credentials are for another server; showing login", "saved", cr.Server, "server", env.Config.ServerURL())
	default:
		o.Credentials = cr
	}
	return o
}

// resolveLocale decides the UI language (i18n spec 6.2): MASIL_LANG, [ui]
// language, the OS; closed languages fall back (0.1.3 shows ko).
func resolveLocale(env *Env) i18n.Resolution {
	in := i18n.DetectOS()
	in.Config = env.Config.UI.Language
	return i18n.Resolve(in)
}

// startTUI runs the app and explains a failure on stderr.
func startTUI(ctx context.Context, env *Env, o app.Options) error {
	if o.Locale == nil {
		res := resolveLocale(env)
		o.Locale = &res
	}
	if err := runTUI(ctx, o); err != nil {
		fmt.Fprintf(env.Stderr, "masil을 실행하지 못했어요: %s\n", errText(err))
		return err
	}
	return nil
}

func cmdTUI(ctx context.Context, env *Env, args []string) error {
	if done, err := noArgs(env, "tui", usageTUI, args); done {
		return err
	}
	return startTUI(ctx, env, tuiOptions(env))
}

func cmdJoin(ctx context.Context, env *Env, args []string) error {
	pos, done, err := parseArgs(env, "join", usageJoin, args, nil)
	if done {
		return err
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return usageError(env, usageJoin, "초대 코드 하나를 적어 주세요", nil)
	}
	o := tuiOptions(env)
	o.InitialRoute = &ui.Route{Kind: ui.RouteInvite, ID: strings.TrimSpace(pos[0])}
	return startTUI(ctx, env, o)
}

// cmdLogin: with --handle and --password-stdin it logs in without the TUI;
// with no flags it opens the TUI on the login form.
func cmdLogin(ctx context.Context, env *Env, args []string) error {
	var handle string
	var fromStdin bool
	pos, done, err := parseArgs(env, "login", usageLogin, args, func(fs *flag.FlagSet) {
		fs.StringVar(&handle, "handle", "", "handle")
		fs.BoolVar(&fromStdin, "password-stdin", false, "read the password from stdin")
	})
	if done {
		return err
	}
	if len(pos) > 0 {
		return usageError(env, usageLogin, "필요 없는 인자가 있어요: "+strings.Join(pos, " "), nil)
	}
	handleSet := handle != ""
	if !handleSet && !fromStdin {
		o := app.Options{Paths: env.Paths, Config: env.Config, InitialRoute: &ui.Route{Kind: ui.RouteLogin, Arg: "login"}}
		return startTUI(ctx, env, o)
	}
	if !handleSet || !fromStdin {
		return usageError(env, usageLogin, "--handle 과 --password-stdin 은 함께 써요", nil)
	}
	handle = normalizeHandle(handle)
	if handle == "" {
		return usageError(env, usageLogin, "아이디를 적어 주세요", nil)
	}
	password, err := readPassword(ctx, env.Stdin)
	if err != nil {
		fmt.Fprintln(env.Stderr, "비밀번호를 읽지 못했어요")
		return err
	}
	if password == "" {
		return usageError(env, usageLogin, "stdin 으로 비밀번호를 넣어 주세요", nil)
	}

	server := env.Config.ServerURL()
	client := api.New(server)
	client.SetLanguage(string(resolveLocale(env).Effective))
	c, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	res, err := client.Login(c, api.LoginRequest{Handle: handle, Password: password, Client: clientName, DeviceName: deviceName()})
	if err != nil {
		fmt.Fprintln(env.Stderr, textwidth.Sanitize(api.Describe(err)))
		return err
	}
	cr := &auth.Credentials{
		Version: 1, Server: client.BaseURL(), Token: res.Token, SessionID: res.Session.ID,
		UserID: res.User.ID, Handle: res.User.Handle, SavedAt: time.Now(),
	}
	if err := auth.Save(env.Paths, cr); err != nil {
		fmt.Fprintln(env.Stderr, "로그인 정보를 저장하지 못했어요")
		// Do not leave an unusable session behind on the server.
		lc, lcancel := context.WithTimeout(ctx, logoutTimeout)
		defer lcancel()
		_ = api.New(cr.Server, api.WithToken(cr.Token)).Logout(lc)
		return err
	}
	slog.Info("logged in", "handle", cr.Handle, "token", cr.Redacted())
	fmt.Fprintf(env.Stdout, "로그인했어요: @%s\n", textwidth.Sanitize(cr.Handle))
	return nil
}

func cmdSignup(ctx context.Context, env *Env, args []string) error {
	if done, err := noArgs(env, "signup", usageSignup, args); done {
		return err
	}
	o := app.Options{Paths: env.Paths, Config: env.Config, InitialRoute: &ui.Route{Kind: ui.RouteLogin, Arg: "signup"}}
	return startTUI(ctx, env, o)
}

// cmdLogout revokes the session on the server that issued it (best effort) and
// always removes the local credentials.
func cmdLogout(ctx context.Context, env *Env, args []string) error {
	if done, err := noArgs(env, "logout", usageLogout, args); done {
		return err
	}
	cr, err := auth.Load(env.Paths)
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			if errors.Unwrap(err) != nil { // a corrupt file: remove it all the same
				if cerr := auth.Clear(env.Paths); cerr != nil {
					fmt.Fprintln(env.Stderr, "로그인 정보를 지우지 못했어요")
					return cerr
				}
			}
			fmt.Fprintln(env.Stdout, "로그인돼 있지 않아요")
			return nil
		}
		fmt.Fprintln(env.Stderr, "로그인 정보를 읽지 못했어요")
		return err
	}
	c, cancel := context.WithTimeout(ctx, logoutTimeout)
	defer cancel()
	if err := api.New(cr.Server, api.WithToken(cr.Token)).Logout(c); err != nil && api.CodeOf(err) != "unauthorized" {
		slog.Warn("server logout failed", "err", err)
		fmt.Fprintf(env.Stderr, "서버에 로그아웃을 알리지 못했어요 (%s). 이 기기의 로그인 정보는 지울게요\n", textwidth.Sanitize(api.Describe(err)))
	}
	if err := auth.Clear(env.Paths); err != nil {
		fmt.Fprintln(env.Stderr, "로그인 정보를 지우지 못했어요")
		return err
	}
	fmt.Fprintln(env.Stdout, "로그아웃했어요")
	return nil
}

func cmdVersion(_ context.Context, env *Env, args []string) error {
	if done, err := noArgs(env, "version", usageVersion, args); done {
		return err
	}
	fmt.Fprintf(env.Stdout, "masil %s (%s, %s)\n", version.Version, version.Commit, version.Date)
	return nil
}

func cmdConfig(_ context.Context, env *Env, args []string) error {
	pos, done, err := parseArgs(env, "config", usageConfig, args, nil)
	if done {
		return err
	}
	if len(pos) != 1 || pos[0] != "path" {
		return usageError(env, usageConfig, "", nil)
	}
	fmt.Fprintln(env.Stdout, env.Paths.Dir)
	return nil
}

func cmdHelp(_ context.Context, env *Env, _ []string) error {
	fmt.Fprint(env.Stdout, helpText())
	return nil
}

// helpText lists every visible command: core ones in coreOrder, then the rest by name.
func helpText() string {
	rank := make(map[string]int, len(coreOrder))
	for i, n := range coreOrder {
		rank[n] = i
	}
	var cs []command
	for _, c := range commands {
		if !c.Hidden {
			cs = append(cs, c)
		}
	}
	sort.Slice(cs, func(i, j int) bool {
		ri, iCore := rank[cs[i].Name]
		rj, jCore := rank[cs[j].Name]
		switch {
		case iCore && jCore:
			return ri < rj
		case iCore != jCore:
			return iCore
		default:
			return cs[i].Name < cs[j].Name
		}
	})
	var b strings.Builder
	b.WriteString("masil — 터미널에서 마실 가요\n\n명령:\n")
	for _, c := range cs {
		u := c.Usage
		if u == "" {
			u = "masil " + c.Name
		}
		b.WriteString("  " + u + "\n")
	}
	return b.String()
}

// normalizeHandle trims, drops a leading "@" and lowercases (handles are lowercase).
func normalizeHandle(h string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "@"))
}

// readPassword reads the first line of r (at most maxPassword bytes) without its
// line ending; an interrupt (ctx) stops the wait.
func readPassword(ctx context.Context, r io.Reader) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(r, maxPassword)).ReadString('\n')
		if errors.Is(err, io.EOF) {
			err = nil
		}
		ch <- result{strings.TrimRight(line, "\r\n"), err}
	}()
	select {
	case res := <-ch:
		return res.s, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// deviceName is the session's device name: the host name, at most 64 runes.
func deviceName() string {
	n, err := os.Hostname()
	if err != nil {
		return ""
	}
	n = textwidth.Sanitize(strings.TrimSpace(n))
	if rs := []rune(n); len(rs) > deviceNameMax {
		n = string(rs[:deviceNameMax])
	}
	return n
}
