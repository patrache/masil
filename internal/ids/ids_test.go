package ids_test

import (
	"testing"

	"github.com/patrache/masil/internal/ids"
)

func TestIDsMonotonicAndValid(t *testing.T) {
	prev := ""
	for i := 0; i < 2000; i++ {
		id := ids.New()
		if len(id) != 26 || !ids.Valid(id) || (prev != "" && !ids.Less(prev, id)) {
			t.Fatalf("%d %q after %q", i, id, prev)
		}
		prev = id
	}
}

func TestValidRejects(t *testing.T) {
	for _, s := range []string{"", "01JA000000000000000000SAM", "01JA0000000000000000000SAMX", "01ja0000000000000000000sam", "01JA00000000000000000000IL", "81JA0000000000000000000SAM"} {
		if ids.Valid(s) {
			t.Error(s)
		}
	}
	if !ids.Valid("01JA0000000000000000000SAM") {
		t.Fatal("fixture id must be valid")
	}
}
