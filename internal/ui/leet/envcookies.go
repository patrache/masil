package leet

import (
	"os"
	"sync"

	"github.com/patrache/masil/internal/leetcode"
)

// The environment sign-in (LEETCODE_SESSION, LEETCODE_CSRFTOKEN,
// LEETCODE_CFCLEARANCE) is for this run only and never reaches a program
// masil starts (A9.1 rule 4): termhost hands every child os.Environ(), so the
// values are moved out of the process environment into memory when masil
// starts, before any program can be launched.

var envCookieNames = [...]string{leetcode.EnvSession, leetcode.EnvCSRF, leetcode.EnvCFClearance}

var (
	envCookieMu sync.Mutex
	envCookies  = map[string]string{}
)

func init() { takeEnvCookies() }

// takeEnvCookies moves the LEETCODE_* variables from the process environment
// into memory (an empty or missing one is just dropped).
func takeEnvCookies() {
	got := map[string]string{}
	for _, k := range envCookieNames {
		if v, ok := os.LookupEnv(k); ok {
			if v != "" {
				got[k] = v
			}
			_ = os.Unsetenv(k)
		}
	}
	envCookieMu.Lock()
	envCookies = got
	envCookieMu.Unlock()
}

// envCookie is the getenv of leetcode.CredentialsFromEnv: the LEETCODE_*
// values masil was started with.
func envCookie(name string) string {
	envCookieMu.Lock()
	defer envCookieMu.Unlock()
	return envCookies[name]
}
