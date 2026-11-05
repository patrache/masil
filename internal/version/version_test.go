package version_test

import (
	"regexp"
	"testing"

	"github.com/patrache/masil/internal/version"
)

func TestUserAgent(t *testing.T) {
	ua := version.UserAgent()
	if !regexp.MustCompile(`^masil/0\.0\.0-dev \((darwin|linux|windows); [a-z0-9]+\)$`).MatchString(ua) {
		t.Fatal(ua)
	}
}

func TestSemver(t *testing.T) {
	orig := version.Version
	t.Cleanup(func() { version.Version = orig })
	for in, want := range map[string]string{"v0.2.0-dev": "0.2.0", "0.1.0": "0.1.0", "v1.2.3": "1.2.3", "0.0.0-dev": "0.0.0"} {
		version.Version = in
		if got := version.Semver(); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
