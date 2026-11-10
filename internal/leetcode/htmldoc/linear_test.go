package htmldoc

import (
	"runtime"
	"strings"
	"testing"
)

// allocated is the bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// Convert stays linear when many short runs of one style are split by tags
// that change nothing (self-closing or unknown tags, empty <b></b>, <wbr>,
// spans that sanitize to nothing): joining them must not copy the growing
// run each time.
func TestConvertLinearInShortRuns(t *testing.T) {
	plain := allocated(func() { Convert(strings.Repeat("a", MaxInput)) })
	budget := 8*plain + 16<<20
	for name, unit := range map[string]string{
		"self-closing": "a<x/>",
		"empty bold":   "ab<b></b>",
		"wbr":          "a<wbr>",
		"empty span":   "a<b>\x01</b>",
	} {
		src := strings.Repeat(unit, MaxInput/len(unit))
		var out []Block
		got := allocated(func() { out = Convert(src) })
		if got > budget {
			t.Errorf("%s: Convert allocated %d MiB, want at most %d MiB (plain text: %d MiB)", name, got>>20, budget>>20, plain>>20)
		}
		if len(out) != 1 || len(out[0].Inline) == 0 || !strings.HasPrefix(out[0].Inline[0].Text, "a") {
			t.Errorf("%s: blocks = %d", name, len(out))
		}
	}
}
