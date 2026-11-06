package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/autostart"
	"github.com/patrache/masil/internal/daemon"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/textwidth"
)

const (
	usageDaemonLine = "masil daemon run|install|uninstall|status"
	// usageDaemon follows core's `masil help` line format ("masil <cmd> — <설명>").
	usageDaemon = usageDaemonLine + " — 알림 데몬 (실행 · 자동 시작 등록/해제 · 상태)"
	// statusLabelWidth is the display width every `masil daemon status` label is padded to.
	statusLabelWidth = 11
)

// Seams tests replace (see stubDaemonFns).
var (
	daemonRunFn          = daemon.Run
	autostartInstallFn   = autostart.Install
	autostartUninstallFn = autostart.Uninstall
	autostartQueryFn     = autostart.Query
	executableFn         = os.Executable
	detachFn             = daemon.Detach
	nowFn                = time.Now
	daemonGOOS           = runtime.GOOS
)

func init() {
	register(command{Name: "daemon", Usage: usageDaemon, Run: daemonCmd})
}

// daemonCmd is `masil daemon run|install|uninstall|status`. Usage errors print
// the usage line on stderr and wrap errUsage (exit code 2, CA 2.4); runtime
// failures are told on stderr and returned plain (exit code 1).
func daemonCmd(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return daemonUsage(env, args, nil)
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprintf(env.Stdout, "사용법: %s\n", usageDaemonLine)
		return nil
	case "run":
		return daemonRun(ctx, env, args)
	case "install", "uninstall", "status":
		fs := newDaemonFlags(args[0])
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintf(env.Stdout, "사용법: %s\n", usageDaemonLine)
				return nil
			}
			return daemonUsage(env, args, err)
		}
		switch args[0] {
		case "install":
			return daemonInstall(env)
		case "uninstall":
			return daemonUninstall(env)
		default:
			return daemonStatus(env)
		}
	default:
		return daemonUsage(env, args, nil)
	}
}

// daemonUsage prints the usage line and returns a usage error naming the args.
func daemonUsage(env *Env, args []string, cause error) error {
	fmt.Fprintf(env.Stderr, "사용법: %s\n", usageDaemonLine)
	err := fmt.Errorf("%w: masil daemon %s", errUsage, strings.Join(args, " "))
	if cause != nil {
		err = fmt.Errorf("%w (%w)", err, cause)
	}
	return err
}

func newDaemonFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("masil daemon "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the Korean usage line replaces flag's own messages
	return fs
}

// daemonRun is `masil daemon run [--foreground] [--verbose]`.
func daemonRun(ctx context.Context, env *Env, args []string) error {
	fs := newDaemonFlags("run")
	foreground := fs.Bool("foreground", false, "do not re-launch hidden (Windows)")
	verbose := fs.Bool("verbose", false, "also write the log to stderr")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(env.Stdout, "사용법: masil daemon run [--foreground] [--verbose]\n")
			return nil
		}
		return daemonUsage(env, args, err)
	}
	logPath := filepath.Join(env.Paths.LogDir, "daemon.log")
	if !*foreground {
		// The child gets the same command line; Detach marks it so it runs here.
		detached, err := detachFn(append([]string{"daemon"}, args...))
		if err != nil {
			fmt.Fprintf(env.Stderr, "데몬을 백그라운드로 띄우지 못했습니다: %v\n", err)
			return err
		}
		if detached {
			fmt.Fprintf(env.Stdout, "백그라운드에서 데몬을 시작했습니다 (로그: %s)\n", logPath)
			return nil
		}
	}

	var extra io.Writer
	if *verbose {
		extra = env.Stderr
	}
	logger, closer, err := daemon.NewLogger(env.Paths, extra)
	if err != nil {
		fmt.Fprintf(env.Stderr, "데몬 로그 파일(%s)을 열지 못했습니다: %v\n", logPath, err)
		return err
	}
	defer closer.Close()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = daemonRunFn(ctx, daemon.Options{Paths: env.Paths, Config: env.Config, Logger: logger})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, instance.ErrAlreadyRunning):
		fmt.Fprintln(env.Stdout, "데몬이 이미 실행 중입니다.")
		return nil
	case errors.Is(err, daemon.ErrRestartRequired):
		// Non-zero on purpose: launchd KeepAlive{SuccessfulExit:false} and
		// systemd Restart=on-failure relaunch the new binary.
		fmt.Fprintln(env.Stderr, "실행 파일이 바뀌어 종료합니다 — 자동 시작이 새 버전으로 다시 띄웁니다.")
		return err
	default:
		fmt.Fprintf(env.Stderr, "데몬이 오류로 멈췄습니다: %v\n", err)
		return err
	}
}

// daemonInstall registers the stable executable path with the OS supervisor and
// warns about what would keep alerts from arriving (not logged in, credentials
// for another server, desktop off, a shell MASIL_SERVER the autostarted daemon
// cannot see).
func daemonInstall(env *Env) error {
	exe, err := executableFn()
	if err != nil {
		fmt.Fprintf(env.Stderr, "실행 파일 경로를 찾지 못했습니다: %v\n", err)
		return err
	}
	exe, err = autostart.StableExecutable(exe)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return err
	}
	if err := autostartInstallFn(exe, env.Paths); err != nil {
		fmt.Fprintf(env.Stderr, "자동 시작을 등록하지 못했습니다: %v\n", err)
		return err
	}
	detail := exe
	if st, err := autostartQueryFn(); err == nil && st.Detail != "" {
		detail = st.Detail
	}
	fmt.Fprintf(env.Stdout, "자동 시작을 등록했습니다: %s\n", textwidth.Sanitize(detail))
	daemonServer := daemon.AutostartServer(env.Config)
	creds, err := auth.Load(env.Paths)
	switch {
	case err != nil:
		fmt.Fprintln(env.Stdout, "아직 로그인하지 않았습니다 — 'masil login' 뒤부터 알림이 옵니다.")
	case !creds.Matches(daemonServer):
		// e.g. `MASIL_SERVER=… masil login` (inline): the daemon would never connect.
		writeServerMismatch(env.Stdout, "", creds.Server, daemonServer)
	}
	if !env.Config.Notify.Desktop {
		fmt.Fprintln(env.Stdout, "config.toml 의 [notify] desktop 이 꺼져 있어 알림이 뜨지 않습니다.")
	}
	if shell := strings.TrimRight(strings.TrimSpace(os.Getenv("MASIL_SERVER")), "/"); shell != "" && shell != daemonServer {
		fmt.Fprintf(env.Stdout, "⚠ 셸의 MASIL_SERVER(%s)는 자동 시작된 데몬에 전달되지 않습니다 — 데몬은 %s 에 접속합니다. config.toml 의 server 를 같은 주소로 맞추세요.\n",
			textwidth.Sanitize(shell), textwidth.Sanitize(daemonServer))
	}
	return nil
}

// writeServerMismatch tells that the credentials belong to another server than
// the one the daemon uses, and how to fix it (shared by install and status).
func writeServerMismatch(w io.Writer, indent, credServer, daemonServer string) {
	s := textwidth.Sanitize
	fmt.Fprintf(w, "%s⚠ 로그인한 서버(%s)와 데몬이 쓰는 서버(%s)가 달라 알림을 받지 않습니다.\n", indent, s(credServer), s(daemonServer))
	fmt.Fprintf(w, "%s  같은 서버로 다시 로그인하거나 config.toml 의 server 를 맞추세요.\n", indent)
}

// daemonUninstall removes the autostart registration.
func daemonUninstall(env *Env) error {
	if err := autostartUninstallFn(); err != nil {
		fmt.Fprintf(env.Stderr, "자동 시작을 해제하지 못했습니다: %v\n", err)
		return err
	}
	fmt.Fprintln(env.Stdout, "자동 시작을 해제했습니다.")
	if daemonGOOS == "windows" { // no supervisor to stop: the running daemon lives until logout
		fmt.Fprintln(env.Stdout, "지금 실행 중인 데몬은 다음 로그인부터 뜨지 않습니다.")
	}
	return nil
}

// daemonStatus prints the daemon's own view (never this shell's MASIL_SERVER as its server).
func daemonStatus(env *Env) error {
	r := daemon.Inspect(env.Paths, env.Config)
	as, asErr := autostartQueryFn()
	renderStatus(env.Stdout, r, as, asErr, nowFn(), time.Local)
	return nil
}

// renderStatus writes the `masil daemon status` report. Every value that comes
// from disk (handle, servers, log lines, paths) is sanitized for the terminal.
func renderStatus(w io.Writer, r daemon.Report, as autostart.Status, asErr error, now time.Time, loc *time.Location) {
	row := func(label, value string) {
		fmt.Fprintf(w, "  %s%s\n", textwidth.PadRight(label, statusLabelWidth), value)
	}
	s := textwidth.Sanitize
	fmt.Fprintln(w, "masil 알림 데몬")

	switch {
	case errors.Is(asErr, autostart.ErrUnsupported):
		row("자동 시작", "이 OS 는 지원하지 않음")
	case asErr != nil:
		row("자동 시작", "확인 실패 — "+s(asErr.Error()))
	case as.Installed:
		row("자동 시작", "등록됨 · "+s(as.Detail))
	default:
		row("자동 시작", "등록 안 됨 — masil daemon install")
	}

	if r.Running {
		row("실행", "실행 중")
	} else {
		row("실행", "멈춤")
	}

	if r.LoggedIn {
		row("계정", "@"+s(r.Handle)+" · "+s(r.CredServer))
	} else {
		row("계정", "로그인 안 됨 — masil login")
	}

	if r.DesktopEnabled {
		row("알림", "켜짐")
	} else {
		row("알림", "꺼짐 (config.toml [notify] desktop = false)")
	}

	if r.State != nil {
		row("동기화", humanize.MessageTime(now, r.State.SavedAt, loc)+" 저장 · seq "+strconv.FormatInt(r.State.LastSeq, 10))
	} else {
		row("동기화", "기록 없음")
	}

	if r.ServerMismatch {
		writeServerMismatch(w, "  ", r.CredServer, r.ConfigServer)
	}
	if r.ShellServer != "" {
		fmt.Fprintf(w, "  ⚠ 이 셸의 MASIL_SERVER(%s)는 자동 시작된 데몬에 전달되지 않습니다.\n", s(r.ShellServer))
	}
	if len(r.Problems) > 0 {
		fmt.Fprintln(w, "  최근 경고")
		for _, line := range r.Problems {
			fmt.Fprintf(w, "    %s\n", s(line))
		}
	}
}
