package leet

import "github.com/patrache/masil/internal/ui"

// SetLeetcodeForTest points every LeetCode client this package makes at
// baseURL (a leetcodetest fake) instead of leetcode.com; the returned func
// restores it. Production code has no way to change the address.
func SetLeetcodeForTest(baseURL string) (restore func()) {
	clientMu.Lock()
	saved := leetcodeURL
	leetcodeURL = baseURL
	clientMu.Unlock()
	return func() {
		clientMu.Lock()
		leetcodeURL = saved
		clientMu.Unlock()
	}
}

// SetOpenURLForTest replaces the browser opener; the returned func restores it.
func SetOpenURLForTest(f func(url string) error) (restore func()) {
	openMu.Lock()
	saved := openURL
	openURL = f
	openMu.Unlock()
	return func() {
		openMu.Lock()
		openURL = saved
		openMu.Unlock()
	}
}

// MismatchForTest is masil's slug for date when it differs from this device's.
func MismatchForTest(ctx *ui.Context, date string) string { return mismatchOn(ctx, date) }
