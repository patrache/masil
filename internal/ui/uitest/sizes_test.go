package uitest_test

import (
	"reflect"
	"testing"

	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

func TestAllSizes(t *testing.T) {
	wantReq := []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}, {W: 50, H: 20}}
	if !reflect.DeepEqual(uitest.Required, wantReq) {
		t.Fatalf("Required = %v, want %v", uitest.Required, wantReq)
	}
	wantModes := map[layout.Mode][]uitest.Size{
		layout.ModeCommand:  {{200, 60}, {200, 50}, {240, 70}, {320, 90}},
		layout.ModeStandard: {{120, 40}, {100, 40}, {199, 50}, {200, 49}, {160, 45}},
		layout.ModeCompact:  {{120, 30}, {100, 39}, {199, 39}, {160, 16}},
		layout.ModeWidget:   {{80, 24}, {99, 60}, {60, 60}, {44, 24}},
		layout.ModeTiny:     {{50, 20}, {99, 23}, {43, 40}, {120, 15}, {44, 16}},
		layout.ModeTooSmall: {{19, 10}, {30, 5}},
	}
	if !reflect.DeepEqual(uitest.ModeSizes, wantModes) {
		t.Fatalf("ModeSizes = %v, want %v", uitest.ModeSizes, wantModes)
	}

	// Required first, then ModeSizes in mode order Command..TooSmall, deduped.
	var want []uitest.Size
	seen := map[uitest.Size]bool{}
	add := func(s uitest.Size) {
		if !seen[s] {
			seen[s] = true
			want = append(want, s)
		}
	}
	for _, s := range wantReq {
		add(s)
	}
	for _, m := range []layout.Mode{layout.ModeCommand, layout.ModeStandard, layout.ModeCompact, layout.ModeWidget, layout.ModeTiny, layout.ModeTooSmall} {
		for _, s := range wantModes[m] {
			add(s)
		}
	}
	got := uitest.AllSizes()
	if len(got) != 24 {
		t.Fatalf("len(AllSizes()) = %d, want 24", len(got))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllSizes() = %v, want %v", got, want)
	}
	got[0] = uitest.Size{W: 1, H: 1}
	if uitest.AllSizes()[0] != (uitest.Size{W: 200, H: 60}) || uitest.Required[0] != (uitest.Size{W: 200, H: 60}) {
		t.Fatal("AllSizes() must return a fresh slice")
	}
}
