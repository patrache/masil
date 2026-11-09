package lazygit

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/theme"
)

// Args returns lazygit's arguments: "-p <absolute repo>", then
// "--use-config-file <user files…>,<overlay>" when overlay is set (the user's
// files first so their keys and editor stay).
func Args(repo string, userFiles []string, overlay string) []string {
	args := []string{"-p", absRepo(repo)}
	if overlay != "" {
		files := append(slices.Clone(userFiles), overlay)
		args = append(args, "--use-config-file", strings.Join(files, ","))
	}
	return args
}

// absRepo makes repo absolute so it can never read as a flag.
func absRepo(repo string) string {
	if abs, err := filepath.Abs(repo); err == nil {
		return abs
	}
	if strings.HasPrefix(repo, "-") {
		return "." + string(filepath.Separator) + repo
	}
	return repo
}

// LaunchArgs returns the arguments for one launch. The color overlay is
// added only in the pane mode (mode is the mode termhost.Choose settled on),
// with overlayOn, and when the user's config files are known; any failure
// launches without it.
func LaunchArgs(ctx context.Context, env Env, bin, repo string, mode termhost.Mode, overlayOn bool, th *theme.Theme, cacheDir string) []string {
	if mode != termhost.ModePane || !overlayOn || cacheDir == "" {
		return Args(repo, nil, "")
	}
	files, ok := UserConfigFiles(ctx, env, bin)
	if !ok {
		return Args(repo, nil, "")
	}
	overlay, err := WriteOverlay(cacheDir, OverlayColorsFrom(th))
	if err != nil || strings.Contains(overlay, ",") {
		return Args(repo, nil, "")
	}
	return Args(repo, files, overlay)
}
