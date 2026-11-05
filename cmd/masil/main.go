// Command masil is the terminal chat + SNS client: `masil` opens the TUI,
// other words dispatch registered commands (CA 2.4).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/patrache/masil/internal/config"
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
	// TODO(core): Task 35
	os.Exit(2)
}
