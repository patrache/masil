package palette

// RememberForTest records it as a route chosen in the palette (recent list).
func RememberForTest(it Item) {
	remember(it)
}

// ResetRecentForTest empties the recently chosen routes; returns a restore func.
func ResetRecentForTest() (restore func()) {
	recentMu.Lock()
	saved := append([]Item(nil), recent...)
	recent = nil
	recentMu.Unlock()
	return func() {
		recentMu.Lock()
		recent = saved
		recentMu.Unlock()
	}
}
