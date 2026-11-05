package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/patrache/masil/internal/admincli"
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/textwidth"
)

// usageAdmin is the `admin` entry of the command registry (hidden from `masil help`).
const usageAdmin = "운영자 명령 (reports | report | resolve | remove | suspend | unsuspend | users | actions)"

func init() {
	register(command{Name: "admin", Usage: usageAdmin, Hidden: true, Run: adminCmd})
}

// adminClientFn builds the API client for `masil admin`; tests replace it.
var adminClientFn = adminClient

// errAdminClient is returned after adminClient explained the problem on stderr.
var errAdminClient = errors.New("admin: no usable credentials")

// adminCmd runs `masil admin …`. A usage error from admincli is marked with
// the package-main usage marker so `run` exits with code 2 (CA 2.4, CR-D).
func adminCmd(ctx context.Context, env *Env, args []string) error {
	c, err := adminClientFn(env)
	if err != nil {
		return err
	}
	err = admincli.Run(ctx, c, args, admincli.IO{Stdout: env.Stdout, Stderr: env.Stderr, Now: time.Now(), Loc: time.Local})
	if errors.Is(err, admincli.ErrUsage) {
		return usageErr(err)
	}
	return err
}

// adminClient makes a client with the saved credentials, only when they were
// issued by the configured server (the token is never sent elsewhere).
func adminClient(env *Env) (*api.Client, error) {
	cr, err := auth.Load(env.Paths)
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			fmt.Fprintln(env.Stderr, "로그인하지 않았습니다 — 먼저 'masil login'")
		} else {
			fmt.Fprintf(env.Stderr, "로그인 정보를 읽지 못했습니다: %s\n", textwidth.Sanitize(err.Error()))
		}
		return nil, fmt.Errorf("%w: %w", errAdminClient, err)
	}
	server := env.Config.ServerURL()
	if !cr.Matches(server) {
		fmt.Fprintf(env.Stderr, "로그인한 서버(%s)와 설정 서버(%s)가 다릅니다\n", textwidth.Sanitize(cr.Server), textwidth.Sanitize(server))
		return nil, fmt.Errorf("%w: server mismatch", errAdminClient)
	}
	return api.New(server, api.WithToken(cr.Token)), nil
}
