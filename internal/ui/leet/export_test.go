package leet

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
