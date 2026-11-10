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
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/textwidth"
)

const (
	usageDaemonLine = "masil daemon run|install|uninstall|status"
	usageDaemonRun  = "masil daemon run [--foreground] [--verbose]"
)

// usageDaemon follows core's `masil help` line format ("masil <cmd> — <설명>").
func usageDaemon(t *i18n.T) string { return usageDaemonLine + " — " + t.Daemon.UsageDesc() }

// statusLabels are the `masil daemon status` row names in t's language.
func statusLabels(t *i18n.T) []string {
	return []string{t.Daemon.LabelAutostart(), t.Daemon.LabelRunning(), t.Daemon.LabelAccount(), t.Daemon.LabelNotify(), t.Daemon.LabelSync()}
}

// statusLabelWidth is the display width every status label is padded to: the
// widest label + 2 (ko "자동 시작" => 11).
func statusLabelWidth(t *i18n.T) int { return textwidth.MaxWidth(statusLabels(t)...) + 2 }

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
	t := env.cat()
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(env.Stdout, t.Cli.UsageLine(usageDaemonLine))
		return nil
	case "run":
		return daemonRun(ctx, env, args)
	case "install", "uninstall", "status":
		fs := newDaemonFlags(args[0])
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintln(env.Stdout, t.Cli.UsageLine(usageDaemonLine))
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
	fmt.Fprintln(env.Stderr, env.cat().Cli.UsageLine(usageDaemonLine))
	err := fmt.Errorf("%w: masil daemon %s", errUsage, strings.Join(args, " "))
	if cause != nil {
		err = fmt.Errorf("%w (%w)", err, cause)
	}
	return err
}

func newDaemonFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("masil daemon "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the catalog's usage line replaces flag's own messages
	return fs
}

// daemonRun is `masil daemon run [--foreground] [--verbose]`.
func daemonRun(ctx context.Context, env *Env, args []string) error {
	t := env.cat()
	fs := newDaemonFlags("run")
	foreground := fs.Bool("foreground", false, "do not re-launch hidden (Windows)")
	verbose := fs.Bool("verbose", false, "also write the log to stderr")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, t.Cli.UsageLine(usageDaemonRun))
			return nil
		}
		return daemonUsage(env, args, err)
	}
	logPath := filepath.Join(env.Paths.LogDir, "daemon.log")
	if !*foreground {
		// The child gets the same command line; Detach marks it so it runs here.
		detached, err := detachFn(append([]string{"daemon"}, args...))
		if err != nil {
			fmt.Fprintln(env.Stderr, t.Daemon.DetachFailed(errText(err)))
			return err
		}
		if detached {
			fmt.Fprintln(env.Stdout, t.Daemon.Detached(logPath))
			return nil
		}
	}

	var extra io.Writer
	if *verbose {
		extra = env.Stderr
	}
	logger, closer, err := daemon.NewLogger(env.Paths, extra)
	if err != nil {
		fmt.Fprintln(env.Stderr, t.Daemon.LogOpenFailed(textwidth.Sanitize(logPath), errText(err)))
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
		fmt.Fprintln(env.Stdout, t.Daemon.AlreadyRunning())
		return nil
	case errors.Is(err, daemon.ErrRestartRequired):
		// Non-zero on purpose: launchd KeepAlive{SuccessfulExit:false} and
		// systemd Restart=on-failure relaunch the new binary.
		fmt.Fprintln(env.Stderr, t.Daemon.RestartRequired())
		return err
	default:
		fmt.Fprintln(env.Stderr, t.Daemon.StoppedError(errText(err)))
		return err
	}
}

// daemonInstall registers the stable executable path with the OS supervisor and
// warns about what would keep alerts from arriving (not logged in, credentials
// for another server, desktop off, a shell MASIL_SERVER the autostarted daemon
// cannot see).
func daemonInstall(env *Env) error {
	t := env.cat()
	exe, err := executableFn()
	if err != nil {
		fmt.Fprintln(env.Stderr, t.Daemon.ExePathFailed(errText(err)))
		return err
	}
	exe, err = autostart.StableExecutable(exe)
	if err != nil {
		fmt.Fprintln(env.Stderr, textwidth.Sanitize(autostart.Message(t, err)))
		return err
	}
	if err := autostartInstallFn(exe, env.Paths); err != nil {
		fmt.Fprintln(env.Stderr, t.Daemon.InstallFailed(textwidth.Sanitize(autostart.Message(t, err))))
		return err
	}
	detail := exe
	if st, err := autostartQueryFn(); err == nil && st.Detail != "" {
		detail = autostartDetail(t, st)
	}
	fmt.Fprintln(env.Stdout, t.Daemon.Installed(textwidth.Sanitize(detail)))
	daemonServer := daemon.AutostartServer(env.Config)
	creds, err := auth.Load(env.Paths)
	switch {
	case err != nil:
		fmt.Fprintln(env.Stdout, t.Daemon.InstallNotLoggedIn())
	case !creds.Matches(daemonServer):
		// e.g. `MASIL_SERVER=… masil login` (inline): the daemon would never connect.
		writeServerMismatch(env.Stdout, t, "", creds.Server, daemonServer)
	}
	if !env.Config.Notify.Desktop {
		fmt.Fprintln(env.Stdout, t.Daemon.InstallDesktopOff())
	}
	if shell := strings.TrimRight(strings.TrimSpace(os.Getenv("MASIL_SERVER")), "/"); shell != "" && shell != daemonServer {
		fmt.Fprintln(env.Stdout, t.Daemon.InstallShellServer(textwidth.Sanitize(shell), textwidth.Sanitize(daemonServer)))
	}
	return nil
}

// autostartDetail is where autostart is registered, with a note in t's
// language when the registered executable is gone (sanitized by the caller).
func autostartDetail(t *i18n.T, st autostart.Status) string {
	if st.ExeMissing != "" || st.ExeUnknown {
		return t.Daemon.DetailExeMissing(st.Detail, st.ExeMissing)
	}
	return st.Detail
}

// writeServerMismatch tells that the credentials belong to another server than
// the one the daemon uses, and how to fix it (shared by install and status).
func writeServerMismatch(w io.Writer, t *i18n.T, indent, credServer, daemonServer string) {
	s := textwidth.Sanitize
	fmt.Fprintln(w, indent+t.Daemon.ServerMismatch(s(credServer), s(daemonServer)))
	fmt.Fprintln(w, indent+"  "+t.Daemon.ServerMismatchFix())
}

// daemonUninstall removes the autostart registration.
func daemonUninstall(env *Env) error {
	t := env.cat()
	if err := autostartUninstallFn(); err != nil {
		fmt.Fprintln(env.Stderr, t.Daemon.UninstallFailed(textwidth.Sanitize(autostart.Message(t, err))))
		return err
	}
	fmt.Fprintln(env.Stdout, t.Daemon.Uninstalled())
	if daemonGOOS == "windows" { // no supervisor to stop: the running daemon lives until logout
		fmt.Fprintln(env.Stdout, t.Daemon.UninstallWindowsNote())
	}
	return nil
}

// daemonStatus prints the daemon's own view (never this shell's MASIL_SERVER as its server).
func daemonStatus(env *Env) error {
	r := daemon.Inspect(env.Paths, env.Config)
	as, asErr := autostartQueryFn()
	renderStatus(env.Stdout, env.cat(), r, as, asErr, nowFn(), time.Local)
	return nil
}

// renderStatus writes the `masil daemon status` report in t's language (nil =
// ko). Every value that comes from disk (handle, servers, log lines, paths) is
// sanitized for the terminal.
func renderStatus(w io.Writer, t *i18n.T, r daemon.Report, as autostart.Status, asErr error, now time.Time, loc *time.Location) {
	if t == nil {
		t = i18n.KoT()
	}
	d := t.Daemon
	width := statusLabelWidth(t)
	row := func(label, value string) {
		fmt.Fprintf(w, "  %s%s\n", textwidth.PadRight(label, width), value)
	}
	s := textwidth.Sanitize
	fmt.Fprintln(w, d.StatusTitle())

	switch {
	case errors.Is(asErr, autostart.ErrUnsupported):
		row(d.LabelAutostart(), d.AutostartUnsupported())
	case asErr != nil:
		row(d.LabelAutostart(), d.AutostartCheckFailed(s(autostart.Message(t, asErr))))
	case as.Installed:
		row(d.LabelAutostart(), d.AutostartInstalled(s(autostartDetail(t, as))))
	default:
		row(d.LabelAutostart(), d.AutostartNotInstalled())
	}

	if r.Running {
		row(d.LabelRunning(), d.Running())
	} else {
		row(d.LabelRunning(), d.Stopped())
	}

	if r.LoggedIn {
		row(d.LabelAccount(), "@"+s(r.Handle)+" · "+s(r.CredServer))
	} else {
		row(d.LabelAccount(), d.NotLoggedIn())
	}

	if r.DesktopEnabled {
		row(d.LabelNotify(), d.NotifyOn())
	} else {
		row(d.LabelNotify(), d.NotifyOff())
	}

	if r.State != nil {
		when := humanize.For(t.Locale()).MessageTime(now, r.State.SavedAt, loc)
		row(d.LabelSync(), d.SyncSaved(when, strconv.FormatInt(r.State.LastSeq, 10)))
	} else {
		row(d.LabelSync(), d.SyncNone())
	}

	if r.ServerMismatch {
		writeServerMismatch(w, t, "  ", r.CredServer, r.ConfigServer)
	}
	if r.ShellServer != "" {
		fmt.Fprintln(w, "  "+d.StatusShellServer(s(r.ShellServer)))
	}
	if len(r.Problems) > 0 {
		fmt.Fprintln(w, "  "+d.RecentWarnings())
		for _, line := range r.Problems {
			fmt.Fprintf(w, "    %s\n", s(line))
		}
	}
}
