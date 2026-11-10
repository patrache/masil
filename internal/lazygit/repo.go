package lazygit

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// RepoRoot returns the top level of the git repository holding dir
// ("git -C <dir> rev-parse --show-toplevel", symlinks resolved), or ErrNotRepo.
func RepoRoot(ctx context.Context, env Env, dir string) (string, error) {
	dir = expandHome(env, strings.TrimSpace(dir))
	if dir == "" {
		return "", ErrNotRepo
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotRepo, err)
	}
	out, err := env.Run.Output(ctx, "", "git", "-C", abs, "rev-parse", "--show-toplevel")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrNotRepo
	}
	top := strings.TrimRight(string(out), "\r\n")
	if strings.TrimSpace(top) == "" || strings.ContainsAny(top, "\n\x00") {
		return "", ErrNotRepo
	}
	top = filepath.Clean(filepath.FromSlash(top))
	if real, err := filepath.EvalSymlinks(top); err == nil {
		top = real
	}
	return top, nil
}

// ResolveRepo picks the repository to open (A11.2): the configured repo when
// it is a repository, else the start folder's repository, else needPick (the
// picker). err is only the context's error.
func ResolveRepo(ctx context.Context, env Env, cfgRepo, startDir string) (repo string, needPick bool, err error) {
	for _, dir := range []string{cfgRepo, startDir} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		top, err := RepoRoot(ctx, env, dir)
		if err == nil {
			return top, false, nil
		}
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
	}
	return "", true, nil
}
