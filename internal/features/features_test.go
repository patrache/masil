package features_test

import (
	"os"
	"slices"
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

// TestFeaturesNoneAndEmpty: a MASIL_FEATURES that is set wins over the
// release set — "none" (or an empty value) turns every tool off.
func TestFeaturesNoneAndEmpty(t *testing.T) {
	for _, env := range []string{"none", "", " NONE "} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("MASIL_FEATURES", env)
			for _, id := range features.Known() {
				if features.On(id) {
					t.Errorf("MASIL_FEATURES=%q: On(%q) is on", env, id)
				}
			}
			if features.Any() {
				t.Errorf("MASIL_FEATURES=%q: Any() is true", env)
			}
		})
	}
	if len(features.Known()) != 5 {
		t.Errorf("Known() = %v, want the five tool ids", features.Known())
	}
}

// TestReleaseFlagsDefault (P0 Task 31, JC21): without MASIL_FEATURES the
// release set is on — every one of the five 0.1.3 tools — and nothing else.
func TestReleaseFlagsDefault(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "") // restored at cleanup
	if err := os.Unsetenv("MASIL_FEATURES"); err != nil {
		t.Fatal(err)
	}
	want := []string{"leet", "workspace", "arch", "git", "api"}
	if got := features.Released(); !slices.Equal(got, want) {
		t.Fatalf("Released() = %v, want %v", got, want)
	}
	for _, id := range features.Known() {
		if !features.On(id) {
			t.Errorf("On(%q) is off without MASIL_FEATURES", id)
		}
	}
	if features.On("nope") || !features.Any() {
		t.Errorf("On(nope) = %v, Any() = %v", features.On("nope"), features.Any())
	}
	// A set MASIL_FEATURES wins: only what it names.
	t.Setenv("MASIL_FEATURES", "arch")
	for _, id := range features.Known() {
		if got := features.On(id); got != (id == "arch") {
			t.Errorf("MASIL_FEATURES=arch: On(%q) = %v", id, got)
		}
	}
}
