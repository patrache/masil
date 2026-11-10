package lazygit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/theme"
)

// UserConfigFiles returns the user's lazygit config files, to put before the
// masil overlay in --use-config-file (A11.3: that flag replaces lazygit's own
// config path). LG_CONFIG_FILE, when set, is used as is (empty items
// dropped); otherwise "<bin> -cd" names the folder and its config.yml is used
// when it exists ([] when it does not). ok is false when "-cd" fails: the
// caller then skips the overlay rather than hide the user's settings.
func UserConfigFiles(ctx context.Context, env Env, bin string) (files []string, ok bool) {
	if env.Getenv != nil {
		if v := strings.TrimSpace(env.Getenv("LG_CONFIG_FILE")); v != "" {
			files = []string{}
			for _, f := range strings.Split(v, ",") {
				if f = strings.TrimSpace(f); f != "" {
					files = append(files, f)
				}
			}
			return files, true
		}
	}
	out, err := env.Run.Output(ctx, "", bin, "-cd")
	if err != nil {
		return nil, false
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || strings.ContainsAny(dir, "\n\x00") {
		return nil, false
	}
	p := filepath.Join(dir, "config.yml")
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
		return []string{p}, true
	}
	return []string{}, true
}

// OverlayColors are the four lazygit theme colors masil sets ("#rrggbb").
type OverlayColors struct {
	ActiveBorder, InactiveBorder, SelectedLineBg, OptionsText string
}

// OverlayColorsFrom takes the colors from a masil theme: accent, line,
// selection background and the second accent.
func OverlayColorsFrom(t *theme.Theme) OverlayColors {
	if t == nil {
		t = theme.Get(theme.Default)
	}
	return OverlayColors{
		ActiveBorder:   hexOf(t.P.Acc),
		InactiveBorder: hexOf(t.P.Line),
		SelectedLineBg: hexOf(t.P.SelBG),
		OptionsText:    hexOf(t.P.Acc2),
	}
}

func hexOf(c color.Color) string {
	if c == nil {
		return "#000000"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// WriteOverlay writes the color-only overlay to
// <cacheDir>/lazygit/theme-<hash>.yml (folder 0700, file 0600) and returns
// its path. The YAML holds gui.theme's four keys and nothing else (JA29).
func WriteOverlay(cacheDir string, c OverlayColors) (string, error) {
	for _, v := range []string{c.ActiveBorder, c.InactiveBorder, c.SelectedLineBg, c.OptionsText} {
		if !colorRe.MatchString(v) {
			return "", fmt.Errorf("lazygit: overlay color %q", v)
		}
	}
	if cacheDir == "" {
		return "", errors.New("lazygit: no cache folder")
	}
	body := fmt.Sprintf("# masil theme overlay (colors only); generated, safe to delete\n"+
		"gui:\n  theme:\n"+
		"    activeBorderColor:\n      - '%s'\n"+
		"    inactiveBorderColor:\n      - '%s'\n"+
		"    selectedLineBgColor:\n      - '%s'\n"+
		"    optionsTextColor:\n      - '%s'\n",
		strings.ToLower(c.ActiveBorder), strings.ToLower(c.InactiveBorder), strings.ToLower(c.SelectedLineBg), strings.ToLower(c.OptionsText))
	sum := sha256.Sum256([]byte(body))
	dir := filepath.Join(cacheDir, "lazygit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "theme-"+hex.EncodeToString(sum[:6])+".yml")
	if old, err := os.ReadFile(p); err == nil && string(old) == body {
		return p, nil
	}
	if err := config.WriteFileAtomic(p, []byte(body), 0o600); err != nil {
		return "", err
	}
	return p, nil
}
