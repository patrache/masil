package features_test

import (
	"testing"

	"github.com/patrache/masil/internal/features"
)

func TestFeaturesEnv(t *testing.T) {
	for _, tc := range []struct {
		env  string
		on   map[string]bool
		want bool // Any()
	}{
		{"", map[string]bool{"git": false, "leet": false, "api": false}, false},
		{"git", map[string]bool{"git": true, "leet": false, "api": false}, true},
		{"leet,arch", map[string]bool{"leet": true, "arch": true, "git": false}, true},
		{" leet , GIT ", map[string]bool{"leet": true, "git": true, "api": false}, true},
		{"all", map[string]bool{"git": true, "leet": true, "workspace": true, "arch": true, "api": true}, true},
		{"nope", map[string]bool{"git": false}, false},
	} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("MASIL_FEATURES", tc.env)
			for id, want := range tc.on {
				if got := features.On(id); got != want {
					t.Errorf("On(%q) = %v, want %v", id, got, want)
				}
			}
			if got := features.Any(); got != tc.want {
				t.Errorf("Any() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Nothing is released in 0.1.3 until the release PR (P0 Task 31): with no
// MASIL_FEATURES every flag is off.
func TestFeaturesDefaultOff(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "")
	for _, id := range features.Known() {
		if features.On(id) {
			t.Errorf("On(%q) is on by default", id)
		}
	}
	if len(features.Known()) != 5 {
		t.Errorf("Known() = %v, want the five tool ids", features.Known())
	}
}
