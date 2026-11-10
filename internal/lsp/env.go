package lsp

import (
	"os"
	"strings"
)

// scrubbedEnv are variables a language server never gets. masil already moves
// the LeetCode sign-in (leetcode.EnvSession, EnvCSRF, EnvCFClearance) out of
// its environment at start (A9.1 rule 4, internal/ui/leet/envcookies.go) and
// termhost hands programs os.Environ() after that; dropping them here again
// keeps a server safe even if it starts before that move.
var scrubbedEnv = []string{"LEETCODE_SESSION", "LEETCODE_CSRFTOKEN", "LEETCODE_CFCLEARANCE"}

// childEnv is masil's environment minus scrubbedEnv.
func childEnv() []string { return scrubEnv(os.Environ()) }

func scrubEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, s := range scrubbedEnv {
			if strings.EqualFold(name, s) { // Windows names are case-insensitive
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}
