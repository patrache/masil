// Command masil is the terminal chat + SNS client: `masil` opens the TUI,
// other words dispatch registered commands (CA 2.4).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// command is one `masil <name>` subcommand. Each package-main file registers its own from init().
type command struct {
	Name string // "daemon", "admin", "login", ...
	// Usage is the one-line usage in t's language ("masil <cmd> — <설명>");
	// nil lists "masil <name>".
	Usage  func(t *i18n.T) string
	Hidden bool // not listed in `masil help`
	Run    func(ctx context.Context, env *Env, args []string) error
}

// Env is what every command gets.
type Env struct {
	Paths  config.Paths
	Config *config.Config
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// T is the CLI's words in the resolved language and Lang how it was
	// chosen (localize; `MASIL_LANG=en masil …` for one run). A nil T is ko.
	T    *i18n.T
	Lang i18n.Resolution
}

// localize resolves the UI language for env (MASIL_LANG, [ui] language, the
// OS; i18n spec 6.2) and sets env.Lang and env.T.
func localize(env *Env) {
	env.Lang = resolveLocale(env)
	env.T = i18n.For(env.Lang.Effective)
}

// cat is env's catalog (ko when localize has not run, as in most tests).
func (e *Env) cat() *i18n.T {
	if e == nil || e.T == nil {
		return i18n.KoT()
	}
	return e.T
}

// commands is the registry filled by register during init().
var commands = map[string]command{}

// register adds c; panics on duplicate Name. Call from init().
func register(c command) {
	if _, dup := commands[c.Name]; dup {
		panic(fmt.Sprintf("masil: duplicate command %q", c.Name))
	}
	commands[c.Name] = c
}

// errUsage marks a usage error: run maps errors.Is(err, errUsage) to exit code 2 (CR-D).
var errUsage = errors.New("usage")

// usageErr marks err as a usage error: errors.Is(usageErr(x), errUsage) &&
// errors.Is(usageErr(x), x); usageErr(nil) == errUsage.
func usageErr(err error) error {
	if err == nil {
		return errUsage
	}
	return fmt.Errorf("%w: %w", errUsage, err)
}

// main: `masil` (no args) or `masil tui` -> app.Run; else dispatch commands; exit code 0/1/2(usage).
func main() {
	os.Exit(realMain())
}

// realMain prepares the Env (config dir, config.toml, log file) and runs the
// command; split from main so deferred cleanups run before os.Exit.
func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() { // after the first interrupt a second one kills the process as usual
		<-ctx.Done()
		stop()
	}()

	// Before config.toml is read the language comes from MASIL_LANG and the OS only.
	env := &Env{Config: config.Defaults(), Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	localize(env)
	paths, err := config.ResolvePaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, env.T.Cli.ConfigDirNotFound(errText(err)))
		return 1
	}
	if err := config.EnsureDir(paths); err != nil {
		fmt.Fprintln(os.Stderr, env.T.Cli.ConfigDirCreateFailed(errText(err)))
		return 1
	}
	cfg, loadErr := config.Load(paths)
	if cfg == nil {
		cfg = config.Defaults()
	}
	env.Paths, env.Config = paths, cfg
	localize(env) // now with [ui] language
	if loadErr != nil {
		// Load always returns a usable Config (defaults for the bad parts).
		fmt.Fprintln(os.Stderr, env.T.Cli.ConfigPartlyUnread(errText(loadErr)))
	}
	logger, closeLog, err := openLog(paths, "masil")
	if err != nil {
		fmt.Fprintln(os.Stderr, env.T.Cli.LogOpenFailed(errText(err)))
		logger, closeLog = slog.New(slog.DiscardHandler), func() {}
	}
	defer closeLog()
	slog.SetDefault(logger)
	defer setupLSP(cfg)() // runs before closeLog: the servers' last lines reach the log

	return run(ctx, os.Args[1:], env)
}

// run dispatches args to a registered command and maps its error to the exit
// code (CA 2.4, CR-D): nil => 0, errors.Is(err, errUsage) => 2, other => 1.
// No args means "tui"; "-h"/"--help" mean "help". run never prints the
// returned error (the command already told the user on env.Stderr); it only logs it.
func run(ctx context.Context, args []string, env *Env) int {
	name := "tui"
	if len(args) > 0 {
		name, args = args[0], args[1:]
	}
	switch name {
	case "-h", "-help", "--help":
		name = "help"
	}
	c, ok := commands[name]
	if !ok {
		t := env.cat()
		fmt.Fprintln(env.Stderr, t.Cli.UnknownCommand(textwidth.Sanitize(name)))
		fmt.Fprintln(env.Stderr, t.Cli.UnknownCommandHint())
		return 2
	}
	err := c.Run(ctx, env, args)
	if err == nil {
		return 0
	}
	slog.Error("command failed", "cmd", name, "err", err)
	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

// logRotateSize: a log file bigger than this is moved to "<name>.log.1" once at start (CA 3.2).
const logRotateSize = 5 << 20

// openLog opens <LogDir>/<name>.log for appending (0600), rotating it to
// ".1" first when it is over 5MB, and returns a slog text logger on it.
func openLog(p config.Paths, name string) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(p.LogDir, 0o700); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(p.LogDir, name+".log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > logRotateSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	_ = f.Chmod(0o600) // an existing file may be wider
	h := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), func() { _ = f.Close() }, nil
}
