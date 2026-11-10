package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui/workspace"
)

// `masil workspace open <path>` / `masil workspace stop [share_id]` (v0.1.3
// spec B6, P2 Task 24). Hidden from `masil help` until the feature ships; with
// the flag off it is a usage error, like an unknown command.

// workspaceStopTimeout bounds the whole stop (the list and every end).
const workspaceStopTimeout = 30 * time.Second

func init() {
	register(command{Name: "workspace", Usage: func(t *i18n.T) string { return t.Workspace.CliUsage() },
		Hidden: !features.On("workspace"), Run: cmdWorkspace})
}

// wsT is the CLI's workspace words in env's language.
func wsT(env *Env) i18n.WorkspaceT { return env.cat().Workspace }

func cmdWorkspace(ctx context.Context, env *Env, args []string) error {
	t := wsT(env)
	if !features.On("workspace") || len(args) == 0 {
		return usageError(env, t.CliUsage(), "", nil)
	}
	switch args[0] {
	case "open":
		return cmdWorkspaceOpen(ctx, env, args[1:])
	case "stop":
		return cmdWorkspaceStop(ctx, env, args[1:])
	}
	return usageError(env, t.CliUsage(), "", nil)
}

// cmdWorkspaceOpen starts the TUI on the folder.
func cmdWorkspaceOpen(ctx context.Context, env *Env, args []string) error {
	t := wsT(env)
	pos, done, err := parseArgs(env, "workspace open", t.CliOpenUsage(), args, nil)
	if done {
		return err
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return usageError(env, t.CliOpenUsage(), "", nil)
	}
	abs, err := filepath.Abs(pos[0])
	if err != nil {
		return usageError(env, t.CliOpenUsage(), t.CliNotDir(pos[0]), err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return usageError(env, t.CliOpenUsage(), t.CliNotDir(abs), err)
	}
	o := tuiOptions(env)
	r := workspace.OpenRoute(abs)
	o.InitialRoute = &r
	return startTUI(ctx, env, o)
}

// cmdWorkspaceStop ends one share of mine, or every live one.
func cmdWorkspaceStop(ctx context.Context, env *Env, args []string) error {
	t := wsT(env)
	pos, done, err := parseArgs(env, "workspace stop", t.CliStopUsage(), args, nil)
	if done {
		return err
	}
	if len(pos) > 1 {
		return usageError(env, t.CliStopUsage(), "", nil)
	}
	cr, err := auth.Load(env.Paths)
	if err != nil {
		fmt.Fprintln(env.Stderr, t.CliNotLoggedIn())
		return err
	}
	c, cancel := context.WithTimeout(ctx, workspaceStopTimeout)
	defer cancel()
	client := newClient(env, cr.Server, api.WithToken(cr.Token))
	var ids []string
	if len(pos) == 1 {
		ids = []string{pos[0]}
	} else {
		shares, err := client.ListWorkspaceShares(c, "owner", "live")
		if err != nil {
			fmt.Fprintln(env.Stderr, t.CliStopFailed(errtext.Describe(env.cat(), err)))
			return err
		}
		for _, s := range shares {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		fmt.Fprintln(env.Stdout, t.CliNoShares())
		return nil
	}
	var firstErr error
	n := 0
	for _, id := range ids {
		if err := client.EndWorkspaceShare(c, id); err != nil {
			fmt.Fprintln(env.Stderr, t.CliStopFailed(errtext.Describe(env.cat(), err)))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	if n > 0 {
		fmt.Fprintln(env.Stdout, t.CliStopped(n))
	}
	return firstErr
}
