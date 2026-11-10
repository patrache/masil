package e2e

import (
	"bufio"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// The x-pseudo region check (P5 Task 4, i18n spec 9.3): under x-pseudo every
// catalog string is wrapped in ⟦⟧, so a region showing other letters (beyond
// user data, key names and slash command names) draws a string that skipped
// the catalog. testdata/pseudo_pending.txt lists the regions not migrated yet
// (one key per line, sorted); the migration tasks delete their keys, and a
// listed key that turns clean fails TestPseudoPendingNoStale.

const pseudoPendingFile = "testdata/pseudo_pending.txt"

// pseudoSizes are the sizes the check draws each state at.
var pseudoSizes = []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}, uitest.Tiny}

// pseudoStates are sweepStates plus login, settings and help.
func pseudoStates() []pseudoState {
	var out []pseudoState
	for _, st := range sweepStates {
		out = append(out, pseudoState{name: st.name, nav: st.nav})
	}
	return append(out,
		pseudoState{name: "login", loggedOut: true},
		pseudoState{name: "settings", nav: openSettings},
		pseudoState{name: "help", nav: func(h *uitest.Harness) { h.Press("?") }},
	)
}

// openSettings serves the settings screen's two reads (my settings, no other
// sessions) and opens it.
func openSettings(h *uitest.Harness) {
	h.API.JSON("GET /v1/me/settings", http.StatusOK, map[string]any{"settings": h.Context().Store.Settings})
	h.API.JSON("GET /v1/auth/sessions", http.StatusOK, map[string]any{"items": []any{}, "next_cursor": nil})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSettings}})
}

type pseudoState struct {
	name      string
	nav       func(h *uitest.Harness)
	loggedOut bool
}

func (st pseudoState) open(t *testing.T, sz uitest.Size, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	if st.loggedOut {
		opts = append(opts, uitest.WithCredentials(nil))
	}
	h := newHarness(t, sz, productSeed, opts...)
	if st.nav != nil {
		st.nav(h)
	}
	return h
}

// regionErrs records AssertPseudoWrapped's reports without failing.
type regionErrs struct {
	testing.TB
	errs []string
}

func (r *regionErrs) Helper() {}
func (r *regionErrs) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

// pseudoRun draws every state at every size and returns region key => the
// reports of its unwrapped text ("" entries never appear: clean regions map
// to nil). switched: open in ko, then SetLocale("x-pseudo") (old text cached
// before the switch shows up here).
func pseudoRun(t *testing.T, switched bool) map[string][]string {
	t.Helper()
	if switched {
		i18n.SetOpenForTest(t, i18n.Ko, i18n.Pseudo)
		t.Setenv("MASIL_LANG", "") // MASIL_LANG beats a switch
	}
	seen := map[string][]string{}
	for _, st := range pseudoStates() {
		for _, sz := range pseudoSizes {
			var h *uitest.Harness
			if switched {
				h = st.open(t, sz)
				h.SetLocale(string(i18n.Pseudo))
				if h.Context().Lang != i18n.Pseudo {
					t.Fatalf("%s: SetLocale(x-pseudo) => %q", st.name, h.Context().Lang)
				}
			} else {
				h = st.open(t, sz, uitest.WithLocale(i18n.Pseudo))
			}
			h.Check()
			allow := uitest.PseudoAllow(h)
			for key, text := range uitest.PseudoRegions(h) {
				rec := &regionErrs{TB: t}
				uitest.AssertPseudoWrapped(rec, key, text, allow)
				if _, ok := seen[key]; !ok {
					seen[key] = nil
				}
				for _, e := range rec.errs {
					seen[key] = append(seen[key], fmt.Sprintf("%s %dx%d: %s", st.name, sz.W, sz.H, e))
				}
			}
		}
	}
	return seen
}

// pseudoPending reads testdata/pseudo_pending.txt (sorted, no duplicates).
func pseudoPending(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(pseudoPendingFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]bool{}
	prev := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k := strings.TrimSpace(sc.Text())
		if k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		if k <= prev {
			t.Fatalf("%s: %q must sort after %q (sorted, no duplicates)", pseudoPendingFile, k, prev)
		}
		out[k], prev = true, k
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func checkPseudo(t *testing.T, seen map[string][]string) {
	t.Helper()
	pending := pseudoPending(t)
	for _, key := range slices.Sorted(maps.Keys(seen)) {
		if errs := seen[key]; len(errs) > 0 && !pending[key] {
			t.Errorf("%s: text outside the catalog (or add the key to %s while it is migrated):\n  %s",
				key, pseudoPendingFile, strings.Join(errs[:min(len(errs), 5)], "\n  "))
		}
	}
}

func TestPseudoSweep(t *testing.T) {
	checkPseudo(t, pseudoRun(t, false))
}

func TestPseudoAfterSwitch(t *testing.T) {
	checkPseudo(t, pseudoRun(t, true))
}

func TestPseudoPendingNoStale(t *testing.T) {
	direct, switched := pseudoRun(t, false), pseudoRun(t, true)
	for _, key := range slices.Sorted(maps.Keys(pseudoPending(t))) {
		_, d := direct[key]
		_, s := switched[key]
		switch {
		case !d && !s:
			t.Errorf("%s is not drawn by any pseudo state: remove %s from %s", key, key, pseudoPendingFile)
		case len(direct[key]) == 0 && len(switched[key]) == 0:
			t.Errorf("%s is clean now: remove %s from %s", key, key, pseudoPendingFile)
		}
	}
}
