//go:build darwin

package i18n

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// preferredUILanguages reads AppleLanguages ("defaults read -g AppleLanguages",
// at most 300ms). Any failure returns nil.
func preferredUILanguages() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return nil
	}
	return parseAppleLanguages(string(out))
}

// parseAppleLanguages reads the plist array text: ( "ko-KR", en, ... ).
func parseAppleLanguages(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		v := strings.Trim(strings.TrimSpace(line), `(),"`)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
