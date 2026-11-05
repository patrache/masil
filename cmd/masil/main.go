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
	"github.com/patrache/masil/internal/textwidth"
)

// command is one `masil <name>` subcommand. Each package-main file registers its own from init().
type command struct {
	Name   string // "daemon", "admin", "login", ...
	Usage  string // one line, Korean
	Hidden bool   // not listed in `masil help`
	Run    func(ctx context.Context, env *Env, args []string) error
}

// Env is what every command gets.
type Env struct {
	Paths  config.Paths
	Config *config.Config
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
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

	paths, err := config.ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "설정 폴더를 찾지 못했어요: %v\n", err)
		return 1
	}
	if err := config.EnsureDir(paths); err != nil {
		fmt.Fprintf(os.Stderr, "설정 폴더를 만들지 못했어요: %v\n", err)
		return 1
	}
	cfg, err := config.Load(paths)
	if err != nil {
		// Load always returns a usable Config (defaults for the bad parts).
		fmt.Fprintf(os.Stderr, "config.toml 일부를 읽지 못해 기본값을 썼어요: %v\n", err)
	}
	if cfg == nil {
		cfg = config.Defaults()
	}
	logger, closeLog, err := openLog(paths, "masil")
	if err != nil {
		fmt.Fprintf(os.Stderr, "로그 파일을 열지 못했어요: %v\n", err)
		logger, closeLog = slog.New(slog.DiscardHandler), func() {}
	}
	defer closeLog()
	slog.SetDefault(logger)

	env := &Env{Paths: paths, Config: cfg, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
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
		fmt.Fprintf(env.Stderr, "알 수 없는 명령이에요: %s\n`masil help` 로 명령 목록을 볼 수 있어요\n", textwidth.Sanitize(name))
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
